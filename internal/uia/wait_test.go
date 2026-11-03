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
	c.store.install(defaultAgent, 101, c.store.beginRead(), []comPtr{comPtr(&a)}, nil)

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
	c.store.install(defaultAgent, 101, c.store.beginRead(), []comPtr{comPtr(&a)}, nil)
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
