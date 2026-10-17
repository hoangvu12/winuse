package uia

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/wstate"
)

// startFakeWorker returns a worker whose jobs are served by a plain
// goroutine (no COM); it mimics the real worker's lifecycle: jobs run
// one at a time, stop is the wind-down signal, and a delivered job
// always runs before the wind-down.
func startFakeWorker() *worker {
	w := &worker{jobs: make(chan func()), stop: make(chan struct{}), exited: make(chan struct{})}
	go func() {
		for {
			select {
			case job := <-w.jobs:
				job()
			case <-w.stop:
				for {
					select {
					case job := <-w.jobs:
						job()
					default:
						close(w.exited)
						return
					}
				}
			}
		}
	}()
	return w
}

// hangWorker returns a worker that never serves jobs (the wedged-thread
// scenario: a provider stuck inside a call).
func hangWorker() *worker {
	return &worker{jobs: make(chan func()), stop: make(chan struct{}), exited: make(chan struct{})}
}

func testClient(factory func() (*worker, error)) *Client {
	return &Client{factory: factory, store: newStore(func(comPtr) {}, func(comPtr) {})}
}

func TestClientRunExecutesJobOnWorker(t *testing.T) {
	ran := make(chan struct{}, 1)
	c := testClient(func() (*worker, error) { return startFakeWorker(), nil })
	defer c.Close()
	if err := c.Run(context.Background(), func(w *worker, _ *run) {
		if w.thread != nil {
			t.Error("fake workers carry no thread")
		}
		ran <- struct{}{}
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	select {
	case <-ran:
	default:
		t.Fatal("the job never ran")
	}
}

func TestClientRunAbandonsWedgedWorkerAndReplacesIt(t *testing.T) {
	var mu sync.Mutex
	started := 0
	var wedged *worker
	c := testClient(func() (*worker, error) {
		mu.Lock()
		defer mu.Unlock()
		started++
		if started == 1 {
			wedged = hangWorker() // the first worker never answers
			return wedged, nil
		}
		return startFakeWorker(), nil
	})
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := c.Run(ctx, func(w *worker, _ *run) { t.Error("the job must never reach the wedged worker") }); err == nil {
		t.Fatal("run against a wedged worker must fail with the context error")
	}

	// The wedged worker was abandoned: stop is closed (senders learn the
	// worker is gone) and the next run is served by a replacement.
	select {
	case <-wedged.stop:
	default:
		t.Fatal("the wedged worker must be winding down (stop closed)")
	}
	if wedged.send(func() {}) {
		t.Fatal("a send to the abandoned worker must refuse the job")
	}
	ran := false
	if err := c.Run(context.Background(), func(w *worker, _ *run) { ran = true }); err != nil {
		t.Fatalf("run on the replacement: %v", err)
	}
	if !ran {
		t.Fatal("the replacement worker did not run the job")
	}
	mu.Lock()
	defer mu.Unlock()
	if started != 2 {
		t.Errorf("factory called %d times, want 2", started)
	}
}

func TestClientRunAbandonsWorkerOnSlowJob(t *testing.T) {
	// A worker whose current job never finishes: the run must give up,
	// abandon it, and the next job goes to a fresh worker.
	c := testClient(func() (*worker, error) { return startFakeWorker(), nil })
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := c.Run(ctx, func(w *worker, _ *run) { time.Sleep(10 * time.Second) }); err == nil {
		t.Fatal("run must fail with the context error")
	}
	ran := make(chan struct{})
	if err := c.Run(context.Background(), func(w *worker, _ *run) { close(ran) }); err != nil {
		t.Fatalf("run on the replacement: %v", err)
	}
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("the replacement worker did not run the job")
	}
}

func TestClientRunRecoversFromPanickingJob(t *testing.T) {
	c := testClient(func() (*worker, error) { return startFakeWorker(), nil })
	defer c.Close()
	if err := c.Run(context.Background(), func(w *worker, _ *run) { panic("boom") }); err == nil {
		t.Fatal("a panicking job must surface as an error")
	}
	ran := false
	if err := c.Run(context.Background(), func(w *worker, _ *run) { ran = true }); err != nil {
		t.Fatalf("run after the panic: %v", err)
	}
	if !ran {
		t.Fatal("the client must serve later jobs after a job panic")
	}
}

func TestClientRunAfterCloseFails(t *testing.T) {
	c := testClient(func() (*worker, error) { return startFakeWorker(), nil })
	c.Close()
	if err := c.Run(context.Background(), func(w *worker, _ *run) {}); err == nil {
		t.Fatal("run after Close must fail")
	}
}

// TestClientRunConcurrentTimeoutsNeverPanic reproduces finding 1's race:
// concurrent requests against one worker plus a timeout that abandons it.
// The old design closed w.jobs from abandon while other Runs were sending
// on it, so the helper could panic with a send on a closed channel. The
// channel is now never closed (stop is the wind-down signal senders
// watch), so every Run must return an orderly error — the context error,
// ErrWorkerGone or success — never a panic — and a replacement worker must
// serve the next request.
func TestClientRunConcurrentTimeoutsNeverPanic(t *testing.T) {
	factory := func() (*worker, error) {
		w := &worker{jobs: make(chan func()), stop: make(chan struct{}), exited: make(chan struct{})}
		go func() {
			for {
				select {
				case job := <-w.jobs:
					time.Sleep(20 * time.Millisecond) // busy: senders queue up
					job()
				case <-w.stop:
					close(w.exited)
					return
				}
			}
		}()
		return w, nil
	}
	c := testClient(factory)
	defer c.Close()

	const n = 8
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
			defer cancel()
			_ = c.Run(ctx, func(w *worker, _ *run) {}) // the error is checked below
		}()
	}
	wg.Wait()

	// After the storm, a fresh request is served (by a replacement worker
	// or a surviving one) without hanging.
	ran := make(chan error, 1)
	go func() { ran <- c.Run(context.Background(), func(w *worker, _ *run) {}) }()
	select {
	case err := <-ran:
		if err != nil && !errors.Is(err, ErrWorkerGone) {
			t.Errorf("run after the concurrent storm: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a run after the concurrent storm never returned")
	}
}

// TestWorkerDeliveredJobRunsDespiteConcurrentClose pins the drain rule: a
// job whose send completed in the same instant close fired still runs —
// its sender already returned true and is waiting on it.
func TestWorkerDeliveredJobRunsDespiteConcurrentClose(t *testing.T) {
	w := &worker{jobs: make(chan func()), stop: make(chan struct{}), exited: make(chan struct{})}
	go func() {
		for {
			select {
			case job := <-w.jobs:
				job()
			case <-w.stop:
				close(w.exited)
				return
			}
		}
	}()
	// Send and close at the same time; the worker may take either branch.
	w.close()
	ran := make(chan struct{})
	if w.send(func() { close(ran) }) {
		select {
		case <-ran:
		case <-time.After(5 * time.Second):
			t.Fatal("the delivered job never ran after the concurrent close")
		}
	}
	select {
	case <-w.exited:
	case <-time.After(5 * time.Second):
		t.Fatal("the worker never wound down")
	}
}

func TestClientFactoryErrorSurfaces(t *testing.T) {
	c := testClient(func() (*worker, error) { return nil, ErrInit })
	if err := c.Run(context.Background(), func(w *worker, _ *run) {}); err == nil {
		t.Fatal("the factory error must surface")
	}
	// Not closed by the failure: a later factory success still works.
	c.factory = func() (*worker, error) { return startFakeWorker(), nil }
	if err := c.Run(context.Background(), func(w *worker, _ *run) {}); err != nil {
		t.Fatalf("run after a transient factory failure: %v", err)
	}
}

func TestStoreInstallReplacesPreviousWindowState(t *testing.T) {
	// Fake element pointers: distinct real addresses; the injected release
	// never calls COM on them.
	a, b, cc := 1, 2, 3
	p1, p2, p3 := comPtr(&a), comPtr(&b), comPtr(&cc)
	var mu sync.Mutex
	released := map[comPtr]int{}
	s := newStore(func(p comPtr) {}, func(p comPtr) {
		mu.Lock()
		released[p]++
		mu.Unlock()
	})

	// A newer read's install replaces the previous state's Elements.
	s.install(101, s.beginRead(), []comPtr{p1}, nil)
	if got := released[p1]; got != 0 {
		t.Fatalf("p1 released %d times before any replacement, want 0", got)
	}
	// A different Window does not disturb the first.
	s.install(202, s.beginRead(), []comPtr{p2}, nil)
	s.install(101, s.beginRead(), []comPtr{p3}, nil)
	if got := released[p1]; got != 1 {
		t.Fatalf("the previous state's element was released %d times, want 1", got)
	}
	if got := released[p2]; got != 0 {
		t.Fatalf("window 202's element released %d times, want 0", got)
	}
	if e, ok := s.element(101, 0); !ok || e != p3 {
		t.Fatalf("element(101, 0) = %v, %v; want the new state's element", e, ok)
	}
	if _, ok := s.element(101, 1); ok {
		t.Fatal("index 1 must not resolve: the new state has one element")
	}
	if _, ok := s.element(303, 0); ok {
		t.Fatal("an unread Window must have no stored state")
	}

	s.releaseAll()
	if got := released[p2]; got != 1 || released[p3] != 1 {
		t.Fatalf("releaseAll released p2 %d times and p3 %d times, want 1 each", released[p2], released[p3])
	}
}

// TestStoreInstallRejectsSupersededRead pins ticket 14's finding: an
// abandoned (wedged-worker) read that completes after a newer read has
// stored its Elements must not clobber them — it releases its own
// Elements instead, so the newest Window State (the one the agent saw)
// stays intact and its Elements are never released under a running
// Action.
func TestStoreInstallRejectsSupersededRead(t *testing.T) {
	a, b, cc := 1, 2, 3
	p1, p2, p3 := comPtr(&a), comPtr(&b), comPtr(&cc)
	var mu sync.Mutex
	released := map[comPtr]int{}
	s := newStore(func(p comPtr) {}, func(p comPtr) {
		mu.Lock()
		released[p]++
		mu.Unlock()
	})

	// Read 1 starts (its worker later wedges); read 2 starts after it and
	// completes first — the state the agent actually saw.
	read1 := s.beginRead()
	read2 := s.beginRead()
	s.install(101, read2, []comPtr{p2}, nil)
	if e, ok := s.element(101, 0); !ok || e != p2 {
		t.Fatalf("element(101, 0) = %v, %v; want read 2's element", e, ok)
	}

	// Read 1's job finally completes: superseded, it releases its own
	// Elements and leaves read 2's state untouched.
	s.install(101, read1, []comPtr{p1, p3}, nil)
	if e, ok := s.element(101, 0); !ok || e != p2 {
		t.Fatalf("element(101, 0) = %v, %v; the late read must not clobber it", e, ok)
	}
	if _, ok := s.element(101, 1); ok {
		t.Fatal("the late read's Elements must not extend the stored state")
	}
	if released[p1] != 1 || released[p3] != 1 {
		t.Fatalf("the superseded read's Elements released %d and %d times, want 1 each", released[p1], released[p3])
	}
	if released[p2] != 0 {
		t.Fatalf("the current state's element released %d times, want 0", released[p2])
	}

	// A read starting now is the latest and installs normally.
	s.install(101, s.beginRead(), []comPtr{p1}, nil)
	if e, ok := s.element(101, 0); !ok || e != p1 {
		t.Fatalf("element(101, 0) = %v, %v; want the newest read's element", e, ok)
	}
	if released[p2] != 1 {
		t.Fatalf("the replaced state's element released %d times, want 1", released[p2])
	}
}

// TestRunMarksAbandonedBeforeGivingUp pins finding 5's precondition: a
// job that outlives its request's deadline and completes late on the
// abandoned worker must see its run marked abandoned — Run sets the flag
// before it abandons the worker, so the job's tail can refuse to install
// its results.
func TestRunMarksAbandonedBeforeGivingUp(t *testing.T) {
	seen := make(chan bool, 1)
	w := &worker{jobs: make(chan func()), stop: make(chan struct{}), exited: make(chan struct{})}
	go func() {
		// A worker whose current job outlives the request: the job runs
		// well after Run has given up on it.
		job := <-w.jobs
		time.Sleep(50 * time.Millisecond)
		job()
	}()
	c := testClient(func() (*worker, error) { return w, nil })
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := c.Run(ctx, func(w *worker, r *run) {
		seen <- r.wasAbandoned()
	}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("run: %v, want the context error", err)
	}
	select {
	case got := <-seen:
		if !got {
			t.Fatal("the late-completing job must see its run marked abandoned")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the job never completed")
	}
}

// TestInstallReadRefusesAbandonedRuns pins finding 5: an abandoned read
// that completes late installs nothing from the abandoned worker's
// apartment (the thread is winding down and its Elements would dangle
// after CoUninitialize), even when no newer read superseded it — the read
// releases its own Elements instead, and the stored Window State is left
// untouched.
func TestInstallReadRefusesAbandonedRuns(t *testing.T) {
	a, b := 1, 2
	p1, p2 := comPtr(&a), comPtr(&b)
	var mu sync.Mutex
	released := map[comPtr]int{}
	c := testClient(func() (*worker, error) { return startFakeWorker(), nil })
	defer c.Close()
	c.store.release = func(p comPtr) {
		mu.Lock()
		released[p]++
		mu.Unlock()
	}

	// A healthy read installs its Elements as the Window's state.
	res := &captureResult{live: []comPtr{p1}, tree: &wstate.Tree{}}
	c.installRead(101, c.store.beginRead(), res, &run{})
	if e, ok := c.store.element(101, 0); !ok || e != p1 {
		t.Fatalf("element(101, 0) = %v, %v; want the healthy read's element", e, ok)
	}
	if got := released[p1]; got != 0 {
		t.Fatalf("the healthy read's element was released %d times, want 0", got)
	}

	// An abandoned run's late read: its own Elements are released, and the
	// stored state (a newer read's, or the last one) is untouched.
	r := &run{}
	r.markAbandoned()
	res2 := &captureResult{live: []comPtr{p2}, tree: &wstate.Tree{}}
	c.installRead(101, c.store.beginRead(), res2, r)
	if e, ok := c.store.element(101, 0); !ok || e != p1 {
		t.Fatalf("element(101, 0) = %v, %v; the abandoned read must not change it", e, ok)
	}
	if got := released[p2]; got != 1 {
		t.Fatalf("the abandoned read's element was released %d times, want 1", got)
	}
	if got := released[p1]; got != 0 {
		t.Fatalf("the stored state's element was released %d times, want 0", got)
	}
}

// TestStoreElementAddRefsOnHandOut pins finding 4's hand-out contract:
// the store AddRefs the Element it hands an Action, so a newer Window
// State installing and releasing the stored reference cannot free the
// Element while the Action still uses it; the stored reference is the
// only one a replacement releases.
func TestStoreElementAddRefsOnHandOut(t *testing.T) {
	a, b := 1, 2
	p, p2 := comPtr(&a), comPtr(&b)
	var mu sync.Mutex
	addRefs, released := 0, 0
	s := newStore(func(comPtr) { mu.Lock(); addRefs++; mu.Unlock() },
		func(comPtr) { mu.Lock(); released++; mu.Unlock() })

	s.install(101, s.beginRead(), []comPtr{p}, nil)
	if _, ok := s.element(101, 0); !ok {
		t.Fatal("element(101, 0) must resolve")
	}
	mu.Lock()
	if addRefs != 1 {
		t.Fatalf("AddRef called %d times on hand-out, want 1", addRefs)
	}
	if released != 0 {
		t.Fatalf("Release called %d times before any replacement, want 0", released)
	}
	mu.Unlock()

	// A newer read replaces the state: only the stored reference is
	// released — the handed-out one keeps the Element alive for its user.
	s.install(101, s.beginRead(), []comPtr{p2}, nil)
	mu.Lock()
	if released != 1 {
		t.Fatalf("Release called %d times after the replacement, want 1 (the stored reference)", released)
	}
	mu.Unlock()
}

// TestElementPointReleasesItsBorrowedElement pins finding 4's other half:
// an Action releases its borrowed Element reference when it is done —
// through the store, on the worker thread — so the Element is freed
// exactly when the last user finishes.
func TestElementPointReleasesItsBorrowedElement(t *testing.T) {
	c := testClient(func() (*worker, error) { return startFakeWorker(), nil })
	defer c.Close()
	a := 1
	var mu sync.Mutex
	addRefs, releases := 0, 0
	c.store.addRef = func(comPtr) { mu.Lock(); addRefs++; mu.Unlock() }
	c.store.release = func(comPtr) { mu.Lock(); releases++; mu.Unlock() }
	c.store.install(101, c.store.beginRead(), []comPtr{comPtr(&a)}, nil)

	if _, err := c.ElementPoint(context.Background(), 101, 0); err != nil {
		t.Fatalf("element point: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if addRefs != 1 {
		t.Errorf("AddRef called %d times, want 1 (the hand-out)", addRefs)
	}
	if releases != 1 {
		t.Errorf("Release called %d times, want 1 (after the action)", releases)
	}
}
