package uia

import (
	"context"
	"sync"
	"testing"
	"time"
)

// startFakeWorker returns a worker whose jobs are served by a plain
// goroutine (no COM); it mimics the real worker's lifecycle.
func startFakeWorker() *worker {
	w := &worker{jobs: make(chan func()), exited: make(chan struct{})}
	go func() {
		for job := range w.jobs {
			job()
		}
		close(w.exited)
	}()
	return w
}

// hangWorker returns a worker that never serves jobs (the wedged-thread
// scenario: a provider stuck inside a call).
func hangWorker() *worker {
	return &worker{jobs: make(chan func()), exited: make(chan struct{})}
}

func testClient(factory func() (*worker, error)) *Client {
	return &Client{factory: factory, store: newStore(func(comPtr) {})}
}

func TestClientRunExecutesJobOnWorker(t *testing.T) {
	ran := make(chan struct{}, 1)
	c := testClient(func() (*worker, error) { return startFakeWorker(), nil })
	defer c.Close()
	if err := c.Run(context.Background(), func(w *worker) {
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
	if err := c.Run(ctx, func(w *worker) { t.Error("the job must never reach the wedged worker") }); err == nil {
		t.Fatal("run against a wedged worker must fail with the context error")
	}

	// The wedged worker was abandoned: its job channel is closed (a
	// receive yields immediately with no job) and the next run is served by
	// a replacement.
	select {
	case j, ok := <-wedged.jobs:
		if ok || j != nil {
			t.Fatal("the wedged worker's job channel must be closed and empty")
		}
	default:
		t.Fatal("the wedged worker's job channel must be closed")
	}
	ran := false
	if err := c.Run(context.Background(), func(w *worker) { ran = true }); err != nil {
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
	if err := c.Run(ctx, func(w *worker) { time.Sleep(10 * time.Second) }); err == nil {
		t.Fatal("run must fail with the context error")
	}
	ran := make(chan struct{})
	if err := c.Run(context.Background(), func(w *worker) { close(ran) }); err != nil {
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
	if err := c.Run(context.Background(), func(w *worker) { panic("boom") }); err == nil {
		t.Fatal("a panicking job must surface as an error")
	}
	ran := false
	if err := c.Run(context.Background(), func(w *worker) { ran = true }); err != nil {
		t.Fatalf("run after the panic: %v", err)
	}
	if !ran {
		t.Fatal("the client must serve later jobs after a job panic")
	}
}

func TestClientRunAfterCloseFails(t *testing.T) {
	c := testClient(func() (*worker, error) { return startFakeWorker(), nil })
	c.Close()
	if err := c.Run(context.Background(), func(w *worker) {}); err == nil {
		t.Fatal("run after Close must fail")
	}
}

func TestClientFactoryErrorSurfaces(t *testing.T) {
	c := testClient(func() (*worker, error) { return nil, ErrInit })
	if err := c.Run(context.Background(), func(w *worker) {}); err == nil {
		t.Fatal("the factory error must surface")
	}
	// Not closed by the failure: a later factory success still works.
	c.factory = func() (*worker, error) { return startFakeWorker(), nil }
	if err := c.Run(context.Background(), func(w *worker) {}); err != nil {
		t.Fatalf("run after a transient factory failure: %v", err)
	}
}

func TestStoreReplaceReleasesPreviousWindowState(t *testing.T) {
	// Fake element pointers: distinct real addresses; the injected release
	// never calls COM on them.
	a, b, cc := 1, 2, 3
	p1, p2, p3 := comPtr(&a), comPtr(&b), comPtr(&cc)
	var mu sync.Mutex
	released := map[comPtr]int{}
	s := newStore(func(p comPtr) {
		mu.Lock()
		released[p]++
		mu.Unlock()
	})

	s.replace(101, []comPtr{p1})
	if got := released[p1]; got != 0 {
		t.Fatalf("p1 released %d times before any replacement, want 0", got)
	}
	// A different Window does not disturb the first.
	s.replace(202, []comPtr{p2})
	s.replace(101, []comPtr{p3})
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
