package uia

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// poolClient is a test Client whose Windows belong to the Apps pids
// names (a Window ID absent from it has no readable process: App 0).
func poolClient(factory func() (*worker, error), pids map[uint64]uint32) *Client {
	c := testClient(factory)
	c.pidOf = func(hwnd uint64) uint32 { return pids[hwnd] }
	return c
}

// workerOf runs an empty job for hwnd and returns the worker it ran on.
func workerOf(t *testing.T, c *Client, hwnd uint64) *worker {
	t.Helper()
	var got *worker
	if err := c.RunFor(context.Background(), hwnd, func(w *worker, _ *run) { got = w }); err != nil {
		t.Fatalf("run for window %d: %v", hwnd, err)
	}
	return got
}

func TestPoolBindsEachAppToOneWorker(t *testing.T) {
	started := 0
	c := poolClient(func() (*worker, error) { started++; return startFakeWorker(), nil },
		map[uint64]uint32{101: 7, 102: 7, 201: 8, 301: 9})
	defer c.Close()

	a1, a2 := workerOf(t, c, 101), workerOf(t, c, 102)
	if a1 != a2 {
		t.Error("two Windows of one App ran on different workers")
	}
	b, d := workerOf(t, c, 201), workerOf(t, c, 301)
	if b == a1 || d == a1 || b == d {
		t.Error("three Apps in a pool of four must get a worker each")
	}
	if workerOf(t, c, 101) != a1 {
		t.Error("an App moved to another worker between calls")
	}
	if started != 3 {
		t.Errorf("started %d workers for three Apps, want 3", started)
	}
}

func TestPoolSharesWorkersPastItsSize(t *testing.T) {
	pids := map[uint64]uint32{}
	for i := uint64(1); i <= 2*PoolSize; i++ {
		pids[i] = uint32(i)
	}
	started := 0
	c := poolClient(func() (*worker, error) { started++; return startFakeWorker(), nil }, pids)
	defer c.Close()
	per := map[*worker]int{}
	for i := uint64(1); i <= 2*PoolSize; i++ {
		per[workerOf(t, c, i)]++
	}
	if started != PoolSize || len(per) != PoolSize {
		t.Fatalf("%d Apps started %d workers over %d, want %d", 2*PoolSize, started, len(per), PoolSize)
	}
	for _, n := range per {
		if n != 2 {
			t.Errorf("a worker serves %d Apps; the Apps must spread evenly (2 each)", n)
		}
	}
}

func TestPoolWithoutAppLookupUsesOneWorker(t *testing.T) {
	c := testClient(func() (*worker, error) { return startFakeWorker(), nil }) // pidOf nil
	defer c.Close()
	if workerOf(t, c, 101) != workerOf(t, c, 202) {
		t.Error("a Client that cannot tell Apps apart must keep one worker")
	}
}

// A hung App wedges its own worker only: a job for another App runs at
// once on its worker meanwhile, the wedged worker is abandoned at the
// hung job's deadline and replaced for its slot, and the healthy App's
// worker is untouched.
func TestPoolOneWedgedWorkerLeavesTheOthersServing(t *testing.T) {
	c := poolClient(func() (*worker, error) { return startFakeWorker(), nil }, map[uint64]uint32{101: 7, 201: 8})
	defer c.Close()
	healthy := workerOf(t, c, 201)

	stuck := make(chan struct{})
	defer close(stuck)
	inJob := make(chan struct{})
	hung := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
		defer cancel()
		hung <- c.RunFor(ctx, 101, func(*worker, *run) { close(inJob); <-stuck })
	}()
	<-inJob

	start := time.Now()
	if w := workerOf(t, c, 201); w != healthy {
		t.Error("the healthy App moved to another worker")
	}
	if took := time.Since(start); took > 200*time.Millisecond {
		t.Errorf("the healthy App's job took %s while another App was hung", took)
	}
	// A second call for the hung App waits behind its wedged job, not
	// behind the healthy App.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := c.RunFor(ctx, 101, func(*worker, *run) { t.Error("a job reached the wedged worker") }); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a job behind the hung App's job: %v, want the deadline", err)
	}

	if err := <-hung; !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the hung job: %v, want the deadline", err)
	}
	replaced := workerOf(t, c, 101)
	if replaced == healthy {
		t.Error("the hung App's replacement worker must not be the healthy App's")
	}
	if workerOf(t, c, 201) != healthy {
		t.Error("abandoning the hung App's worker replaced the healthy App's")
	}
}

// A new App avoids a worker with a job in flight (a hung App's wedged job
// counts) when another worker is idle.
func TestPoolBindsANewAppAwayFromABusyWorker(t *testing.T) {
	pids := map[uint64]uint32{}
	for i := uint64(1); i <= PoolSize+1; i++ {
		pids[i] = uint32(i)
	}
	c := poolClient(func() (*worker, error) { return startFakeWorker(), nil }, pids)
	defer c.Close()
	ws := map[uint64]*worker{}
	for i := uint64(1); i <= PoolSize; i++ {
		ws[i] = workerOf(t, c, i)
	}
	// App 1 hangs on its worker; Apps 2..PoolSize are idle.
	stuck, inJob := make(chan struct{}), make(chan struct{})
	defer close(stuck)
	go c.RunFor(context.Background(), 1, func(*worker, *run) { close(inJob); <-stuck })
	<-inJob

	if w := workerOf(t, c, PoolSize+1); w == ws[1] {
		t.Error("a new App was bound to the worker of a hung App while others were idle")
	}
}

func TestPoolDropsIdleBindings(t *testing.T) {
	c := poolClient(func() (*worker, error) { return startFakeWorker(), nil }, map[uint64]uint32{101: 7, 201: 8})
	defer c.Close()
	workerOf(t, c, 101)
	c.mu.Lock()
	c.apps[7].last = time.Now().Add(-2 * affinityIdle)
	c.mu.Unlock()
	workerOf(t, c, 201) // binding a new App sweeps the idle ones
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.apps[7]; ok {
		t.Error("an App idle past affinityIdle kept its binding")
	}
	if _, ok := c.apps[8]; !ok {
		t.Error("the new App has no binding")
	}
}

func TestPoolCloseWindsEveryWorkerDown(t *testing.T) {
	var mu sync.Mutex
	var started []*worker
	c := poolClient(func() (*worker, error) {
		w := startFakeWorker()
		mu.Lock()
		started = append(started, w)
		mu.Unlock()
		return w, nil
	}, map[uint64]uint32{101: 7, 201: 8, 301: 9})
	for _, h := range []uint64{101, 201, 301} {
		workerOf(t, c, h)
	}
	c.Close()
	mu.Lock()
	defer mu.Unlock()
	for i, w := range started {
		select {
		case <-w.exited:
		case <-time.After(2 * time.Second):
			t.Errorf("worker %d did not exit on Close", i)
		}
	}
	if err := c.RunFor(context.Background(), 101, func(*worker, *run) {}); err == nil {
		t.Error("a run after Close must fail")
	}
}
