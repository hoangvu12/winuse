package uia

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// TestHoldElementHoldsUntilReleased: an element wait resolves its Element
// once and holds its own reference (desktop-parity 03), so a newer read of
// the Window cannot free it mid-wait; Release gives the reference back on
// the worker. The stored Window State stays as it was.
func TestHoldElementHoldsUntilReleased(t *testing.T) {
	c := testClient(func() (*worker, error) { return startFakeWorker(), nil })
	defer c.Close()
	a := 1
	var mu sync.Mutex
	addRefs, releases := 0, 0
	c.store.addRef = func(comPtr) { mu.Lock(); addRefs++; mu.Unlock() }
	c.store.release = func(comPtr) { mu.Lock(); releases++; mu.Unlock() }
	c.store.install(defaultAgent, 101, c.store.beginRead(), []comPtr{comPtr(&a)}, nil, false)

	held, err := c.HoldElement(context.Background(), 101, 0)
	if err != nil {
		t.Fatalf("hold: %v", err)
	}
	mu.Lock()
	if addRefs != 1 || releases != 0 {
		t.Errorf("after the hold: %d AddRefs, %d Releases; want the hand-out only", addRefs, releases)
	}
	mu.Unlock()
	if _, err := held.State(context.Background()); !errors.Is(err, errNoCOMThread) {
		t.Errorf("a fake worker's state read = %v, want errNoCOMThread", err)
	}
	held.Release()
	held.Release() // a second release is a no-op
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := releases
		mu.Unlock()
		if n == 1 {
			break
		}
		if n > 1 || time.Now().After(deadline) {
			t.Fatalf("%d Releases, want exactly 1", n)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !c.store.has(defaultAgent, 101) {
		t.Error("holding an Element changed the stored Window State")
	}
}

func TestHoldElementMisses(t *testing.T) {
	c := testClient(func() (*worker, error) { return startFakeWorker(), nil })
	defer c.Close()
	if _, err := c.HoldElement(context.Background(), 101, 0); !errors.Is(err, ErrNoState) {
		t.Errorf("no Window State: %v", err)
	}
	a := 1
	c.store.install(defaultAgent, 101, c.store.beginRead(), []comPtr{comPtr(&a)}, nil, false)
	if _, err := c.HoldElement(context.Background(), 101, 3); !errors.Is(err, ErrStale) {
		t.Errorf("an index past the Window State: %v", err)
	}
}

func TestPeekTreeNeedsACOMThread(t *testing.T) {
	c := testClient(func() (*worker, error) { return startFakeWorker(), nil })
	defer c.Close()
	if _, err := c.PeekTree(context.Background(), 101, "t", "a.exe", time.Second); !errors.Is(err, errNoCOMThread) {
		t.Errorf("peek on a fake worker = %v", err)
	}
}

// TestHeldElementOutlivesItsWaitSafely: a wait's poll can still be reading
// when the wait releases its Element. The read keeps the Element until it
// ends and then releases it; a read after the release reads nothing.
func TestHeldElementOutlivesItsWaitSafely(t *testing.T) {
	c := testClient(func() (*worker, error) { return startFakeWorker(), nil })
	defer c.Close()
	a := 1
	var mu sync.Mutex
	releases := 0
	c.store.release = func(comPtr) { mu.Lock(); releases++; mu.Unlock() }
	c.store.install(defaultAgent, 101, c.store.beginRead(), []comPtr{comPtr(&a)}, nil, false)
	held, err := c.HoldElement(context.Background(), 101, 0)
	if err != nil {
		t.Fatalf("hold: %v", err)
	}
	h := held.(*heldElement)
	count := func() int { mu.Lock(); defer mu.Unlock(); return releases }

	el := h.use() // a read in flight
	held.Release()
	held.Release()
	time.Sleep(50 * time.Millisecond)
	if n := count(); n != 0 {
		t.Fatalf("%d Releases while a read was in flight, want none", n)
	}
	h.done(el)
	if n := count(); n != 1 {
		t.Fatalf("%d Releases after the read ended, want 1", n)
	}
	if _, err := held.State(context.Background()); !errors.Is(err, errHoldEnded) {
		t.Errorf("a read after the release = %v, want errHoldEnded", err)
	}
	time.Sleep(50 * time.Millisecond)
	if n := count(); n != 1 {
		t.Errorf("%d Releases in all, want exactly 1", n)
	}
}

// TestRunGivesAWaitPollUpWithoutAbandoning: a wait's poll whose context
// ends, queued behind another job or running, returns without abandoning
// the worker (WithWaitPoll), so the requests queued on it keep it.
func TestRunGivesAWaitPollUpWithoutAbandoning(t *testing.T) {
	c := testClient(func() (*worker, error) { return startFakeWorker(), nil })
	defer c.Close()
	busy, release := make(chan struct{}), make(chan struct{})
	go c.Run(context.Background(), func(*worker, *run) { close(busy); <-release })
	<-busy
	w, _ := c.ensureWorker()

	ctx, cancel := context.WithTimeout(WithWaitPoll(context.Background()), 50*time.Millisecond)
	defer cancel()
	if err := c.Run(ctx, func(*worker, *run) {}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a queued poll = %v, want its deadline", err)
	}
	if now, _ := c.ensureWorker(); now != w {
		t.Fatal("a queued poll abandoned the worker")
	}
	close(release)

	stuck := make(chan struct{})
	defer close(stuck)
	ctx2, cancel2 := context.WithTimeout(WithWaitPoll(context.Background()), 50*time.Millisecond)
	defer cancel2()
	if err := c.Run(ctx2, func(*worker, *run) { <-stuck }); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a running poll = %v, want its deadline", err)
	}
	if now, _ := c.ensureWorker(); now != w {
		t.Error("a running poll abandoned the worker")
	}
}
