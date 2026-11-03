package uia

// The UIA worker pool (multi-agent ticket 07, ADR 0009 as amended by ADR
// 0014): several UIA MTA workers instead of one, each App bound to one of
// them by its process id, so every UIA call for an App's Windows runs on
// that App's worker. A hung App wedges only its own worker: other Apps'
// reads and Actions keep running on theirs (probes round 3: with one App
// hung, other threads clicked a healthy App in 17 ms), and the wedged
// worker is abandoned and replaced on its own, as the one worker was.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/hoangvu12/winuse/internal/win"
)

// PoolSize is how many UIA workers the helper runs at most. Each starts
// on first use, so a helper that reads one App runs one.
const PoolSize = 4

// affinityIdle is how long an App's binding to its worker outlives its
// last job; past it, with no job in flight, the binding is dropped when a
// new App is bound, and the App is bound afresh at its next call.
const affinityIdle = 10 * time.Minute

// binding is one App's place in the pool.
type binding struct {
	slot     int
	inflight int       // jobs of this App now running or waiting for the worker
	last     time.Time // when its last job ended
}

// Client is the helper's UI Automation client: it owns the pool of worker
// threads and the live-Element store. All COM work goes through RunFor,
// which sends one job to the worker of the Window's App and waits for it
// under the request's context.
//
// A worker wedged past the request's budget (a provider ignoring even the
// UIA timeouts) is abandoned — its thread is leaked, never killed — and the
// next job for its slot starts a fresh worker (uia-go notes §5: the
// belt-and-braces layer on top of the walk's own limits). The other slots
// carry on.
type Client struct {
	mu      sync.Mutex
	slots   [PoolSize]*worker
	apps    map[uint32]*binding // App process id → its slot
	closed  bool
	factory func() (*worker, error)  // startWorker; faked in tests
	pidOf   func(hwnd uint64) uint32 // the App behind a Window (win.AppPID); nil binds every job to one worker
	store   *store
	thin    *thinWindows // Windows whose settle ran out thin (retry.go); nil remembers nothing
}

// New creates a client. The worker threads start lazily on first use, so
// a helper that only ever lists windows never initialises COM.
func New() *Client {
	return &Client{factory: startWorker, pidOf: win.AppPID, store: newStore(addRef, release), thin: newThinWindows()}
}

// Close releases the workers and the stored live Elements. Best effort: a
// wedged worker is leaked rather than waited on; the process exit reclaims
// it.
func (c *Client) Close() {
	c.mu.Lock()
	var ws []*worker
	for i, w := range c.slots {
		if w != nil {
			ws = append(ws, w)
			c.slots[i] = nil
		}
	}
	c.closed = true
	c.mu.Unlock()
	if len(ws) == 0 {
		return
	}
	// Release stored elements on a COM thread (Release on MTA objects from
	// an uninitialized thread is not allowed); every worker is in the one
	// process MTA, so any of them can. Delivery is bounded: a worker still
	// busy past the bound is wedged, and it is leaked with whatever
	// elements remain rather than hanging the shutdown.
	delivered := make(chan struct{})
	go func() {
		if ws[0].send(func() { c.store.releaseAll() }) {
			close(delivered)
		}
	}()
	select {
	case <-delivered:
	case <-ws[0].exited:
	case <-time.After(2 * time.Second):
	}
	for _, w := range ws {
		w.close()
	}
	deadline := time.After(2 * time.Second)
	for _, w := range ws {
		select {
		case <-w.exited:
		case <-deadline:
			return // a wedged worker: leak it; the process exit reclaims it
		}
	}
}

// acquire returns the worker of hwnd's App, starting it if needed, and
// counts a job in flight for the App until done is called. hwnd 0 (a call
// that names no Window) has an App of its own, process 0.
func (c *Client) acquire(hwnd uint64) (w *worker, done func(), err error) {
	var pid uint32
	if c.pidOf != nil && hwnd != 0 {
		pid = c.pidOf(hwnd)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, nil, errors.New("the UIA client is closed")
	}
	b := c.bind(pid)
	if c.slots[b.slot] == nil {
		w, err := c.factory()
		if err != nil {
			return nil, nil, err
		}
		c.slots[b.slot] = w
	}
	b.inflight++
	return c.slots[b.slot], func() {
		c.mu.Lock()
		b.inflight--
		b.last = time.Now()
		c.mu.Unlock()
	}, nil
}

// bind returns pid's binding, binding a new App to the slot that is least
// busy (fewest Apps with a job in flight: a wedged job counts, so a new
// App avoids a hung App's worker), then serves the fewest Apps, then comes
// first. Called with c.mu held.
func (c *Client) bind(pid uint32) *binding {
	if c.apps == nil {
		c.apps = map[uint32]*binding{}
	}
	if b, ok := c.apps[pid]; ok {
		return b
	}
	var busy, bound [PoolSize]int
	for p, b := range c.apps {
		if b.inflight == 0 && time.Since(b.last) > affinityIdle {
			delete(c.apps, p)
			continue
		}
		bound[b.slot]++
		if b.inflight > 0 {
			busy[b.slot]++
		}
	}
	slot := 0
	for i := 1; i < PoolSize; i++ {
		if busy[i] < busy[slot] || busy[i] == busy[slot] && bound[i] < bound[slot] {
			slot = i
		}
	}
	b := &binding{slot: slot, last: time.Now()}
	c.apps[pid] = b
	return b
}

// ensureWorker returns the worker of the calls that name no Window,
// starting it if needed.
func (c *Client) ensureWorker() (*worker, error) {
	w, done, err := c.acquire(0)
	if err != nil {
		return nil, err
	}
	done()
	return w, nil
}

// abandon gives up on w: it is detached so the next job for its slot
// starts a replacement, and stop is closed so the thread winds down after
// its current job (or is leaked if that job never returns; the abandoned
// job's results are discarded). The other slots are untouched.
func (c *Client) abandon(w *worker) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, s := range c.slots {
		if s == w {
			c.slots[i] = nil
			w.close()
		}
	}
}

// run tracks one Run: whether the worker it ran on was abandoned while
// the job was still in flight. A job that completes after its run was
// abandoned — a wedged provider call eventually returning on a worker
// already winding down — must not install its results anywhere shared:
// Run marks the run before abandoning, and the job's tail checks it under
// the same mutex (finding 5 of the review).
type run struct {
	mu        sync.Mutex
	abandoned bool
}

// wasAbandoned reports whether Run gave up on this run's worker. It is
// read on the worker thread together with the decision that depends on
// it, under mu, so the flag cannot flip between the check and the
// decision.
func (r *run) wasAbandoned() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.abandoned
}

// markAbandoned is called by Run before it abandons the worker — never
// after a job that already finished keeps its results.
func (r *run) markAbandoned() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.abandoned = true
}

// Run is RunFor with no Window: the job runs on the worker of the calls
// that name none.
func (c *Client) Run(ctx context.Context, job func(w *worker, r *run)) error {
	return c.RunFor(ctx, 0, job)
}

// RunFor executes job on the COM thread of hwnd's App's worker, bounded by
// ctx. A job that does not finish before ctx expires abandons that worker
// (its result is discarded) and returns ctx's error — the run is marked
// abandoned first, so a job still executing on the abandoned worker
// refuses to install its results anywhere shared. A worker already winding
// down (an earlier abandon) answers ErrWorkerGone: the next job starts a
// replacement, so the caller can retry.
func (c *Client) RunFor(ctx context.Context, hwnd uint64, job func(w *worker, r *run)) error {
	w, release, err := c.acquire(hwnd)
	if err != nil {
		return err
	}
	defer release()
	r := &run{}
	done := make(chan struct{})
	var perr error
	j := func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				perr = fmt.Errorf("the UIA worker crashed: %v", r)
				c.abandon(w) // its COM state is suspect; replace it
			}
		}()
		job(w, r)
	}
	delivered, serr := w.sendCtx(ctx, j)
	if !delivered {
		if errors.Is(serr, ErrWorkerGone) || isWaitPoll(ctx) {
			return serr // a wait's poll gives up without abandoning (WithWaitPoll)
		}
		// ctx expired while waiting for the worker (another request's job
		// for this App holds it): give up on the worker, like a job that
		// overruns.
		c.abandon(w)
		return serr
	}
	select {
	case <-done:
		return perr
	case <-ctx.Done():
		// The job may have finished in the same instant the deadline fired;
		// its result wins over the timeout (the server delivers output
		// produced past the budget).
		select {
		case <-done:
			return perr
		default:
		}
		if isWaitPoll(ctx) {
			return ctx.Err() // left to finish: a poll installs nothing (WithWaitPoll)
		}
		r.markAbandoned()
		c.abandon(w)
		return ctx.Err()
	}
}
