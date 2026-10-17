package uia

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/hoangvu12/winuse/internal/wstate"
)

// ErrInit reports that UI Automation could not be initialised in the helper
// process (COM or the CUIAutomation8 class failed to create).
var ErrInit = errors.New("UI Automation is unavailable")

// ErrWindowGone reports that a Window's root element could not be read: the
// Window closed or exposes no accessibility tree.
var ErrWindowGone = errors.New("window is gone")

// thread owns the COM state of one worker: created on the worker's locked
// OS thread, used only from that thread.
type thread struct {
	auto comPtr // IUIAutomation2, with the connection timeout already set
}

// worker is one OS-locked, COM-initialised (MTA) thread serving UIA jobs.
// The thread must own no window and pump no messages: MTA needs no message
// pump, and UIA calls should come from a thread that owns no HWND (uia-go
// notes §6).
type worker struct {
	jobs   chan func()   // unbuffered: one job in flight at a time; never closed
	stop   chan struct{} // closed to wind the thread down; senders watch it
	exited chan struct{}
	thread *thread // set before the worker answers ready; read by jobs

	mu     sync.Mutex // guards closed; never held across the job wait
	closed bool
}

// ErrWorkerGone reports that the UIA worker is winding down — it was
// abandoned after a wedged provider call outlasted a request's budget, or
// the client is closing — so a job cannot be delivered to it. The next
// request starts a replacement worker; the recovery is to retry the
// command.
var ErrWorkerGone = errors.New("the UIA worker is winding down after being abandoned; retry the command")

// startWorker starts a fresh worker thread. It returns only after the
// thread has initialised COM and created IUIAutomation2 (or failed).
func startWorker() (*worker, error) {
	w := &worker{jobs: make(chan func()), stop: make(chan struct{}), exited: make(chan struct{})}
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		hr := coInitializeEx()
		if hr.Failed() {
			ready <- fmt.Errorf("%w: CoInitializeEx failed (HRESULT %s)", ErrInit, hr)
			close(w.exited)
			return
		}
		// CoInitializeEx S_OK/S_FALSE both require one CoUninitialize.
		auto, hr := coCreateAutomation()
		if hr.Failed() || auto == nil {
			coUninitialize()
			ready <- fmt.Errorf("%w: CoCreateInstance failed (HRESULT %s)", ErrInit, hr)
			close(w.exited)
			return
		}
		// Provider-connection setup is bounded to 1s; per-chunk transaction
		// timeouts are re-put before every cache chunk by the walk.
		call(auto, uia2PutConnectionTimeout, 1000)
		w.thread = &thread{auto: auto}
		ready <- nil
		w.serve()
	}()
	if err := <-ready; err != nil {
		return nil, err
	}
	return w, nil
}

// serve is the worker thread's job loop. The wind-down signal is stop,
// never a close of jobs: a sender can be parked on jobs the moment an
// abandon fires, and a send on a closed channel would panic the helper
// (finding 1 of the review). A job whose send completed in the same
// instant stop closed still runs — its sender already returned true and
// is waiting for it — so the stop branch drains what was delivered before
// releasing the COM state.
func (w *worker) serve() {
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
					// Order: release the automation object while the MTA is
					// alive, then leave COM. Stored elements live in the
					// client-level store and are only ever used from a live
					// worker (run ensures one exists), so the MTA is never
					// uninitialised under them.
					release(w.thread.auto)
					coUninitialize()
					close(w.exited)
					return
				}
			}
		}
	}
}

// send delivers one job to the worker thread. It reports false when the
// worker is winding down or already gone. w.mu is never held across the
// wait for a busy worker (finding 2, fixed here in its wgc form first: a
// parked send holding the lock would block close — and with it the
// watchdog's abandon — deadlocking every later request); instead senders
// park in a select that also watches stop, so closing stop releases them.
// jobs is never closed, so no send can panic.
func (w *worker) send(job func()) bool {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return false
	}
	w.mu.Unlock()
	select {
	case w.jobs <- job:
		return true
	case <-w.stop:
		return false
	case <-w.exited:
		return false
	}
}

// sendCtx is send bounded by ctx: it reports the delivery, ctx's error
// when the deadline fired first (the caller abandons the worker), or
// ErrWorkerGone when the worker is winding down.
func (w *worker) sendCtx(ctx context.Context, job func()) (bool, error) {
	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	if closed {
		return false, ErrWorkerGone
	}
	select {
	case w.jobs <- job:
		return true, nil
	case <-ctx.Done():
		return false, ctx.Err()
	case <-w.stop:
		return false, ErrWorkerGone
	case <-w.exited:
		return false, ErrWorkerGone
	}
}

// close winds the worker down: senders learn through stop and the thread
// leaves its loop after its current job (or is leaked if that job never
// returns). Idempotent; jobs is never closed.
func (w *worker) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.closed {
		w.closed = true
		close(w.stop)
	}
}

// Client is the helper's UI Automation client: it owns the current worker
// thread and the live-Element store. All COM work goes through Run, which
// sends one job to the worker and waits for it under the request's context.
//
// A worker wedged past the request's budget (a provider ignoring even the
// UIA timeouts) is abandoned — its thread is leaked, never killed — and the
// next job starts a fresh worker (uia-go notes §5: the belt-and-braces
// layer on top of the walk's own limits).
type Client struct {
	mu      sync.Mutex
	w       *worker
	closed  bool
	factory func() (*worker, error) // startWorker; faked in tests
	store   *store
}

// New creates a client. The worker thread starts lazily on first use, so a
// helper that only ever lists windows never initialises COM.
func New() *Client {
	return &Client{factory: startWorker, store: newStore(addRef, release)}
}

// Close releases the worker and the stored live Elements. Best effort: a
// wedged worker is leaked rather than waited on; the process exit reclaims
// it.
func (c *Client) Close() {
	c.mu.Lock()
	w := c.w
	c.w = nil
	c.closed = true
	c.mu.Unlock()
	if w == nil {
		return
	}
	// Release stored elements on a COM thread (Release on MTA objects from
	// an uninitialized thread is not allowed). Delivery is bounded: a
	// worker still busy past the bound is wedged, and it is leaked with
	// whatever elements remain rather than hanging the shutdown.
	delivered := make(chan struct{})
	go func() {
		if w.send(func() { c.store.releaseAll() }) {
			close(delivered)
		}
	}()
	select {
	case <-delivered:
	case <-w.exited:
	case <-time.After(2 * time.Second):
	}
	w.close()
	select {
	case <-w.exited:
	case <-time.After(2 * time.Second):
		// Worker wedged: leak it; the process exit reclaims it.
	}
}

// ensureWorker returns the current worker, starting one if needed.
func (c *Client) ensureWorker() (*worker, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, errors.New("the UIA client is closed")
	}
	if c.w == nil {
		w, err := c.factory()
		if err != nil {
			return nil, err
		}
		c.w = w
	}
	return c.w, nil
}

// abandon gives up on w: it is detached so the next Run starts a
// replacement, and stop is closed so the thread winds down after its
// current job (or is leaked if that job never returns; the abandoned
// job's results are discarded).
func (c *Client) abandon(w *worker) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.w == w {
		c.w = nil
		w.close()
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

// Run executes job on the worker's COM thread, bounded by ctx. A job that
// does not finish before ctx expires abandons the worker (its result is
// discarded) and returns ctx's error — the run is marked abandoned first,
// so a job still executing on the abandoned worker refuses to install its
// results anywhere shared. A worker already winding down (an earlier
// abandon) answers ErrWorkerGone: the next Run starts a replacement, so
// the caller can retry.
func (c *Client) Run(ctx context.Context, job func(w *worker, r *run)) error {
	w, err := c.ensureWorker()
	if err != nil {
		return err
	}
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
		if errors.Is(serr, ErrWorkerGone) {
			return serr
		}
		// ctx expired while waiting for the worker (another request's job
		// holds it): give up on the worker, like a job that overruns.
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
		r.markAbandoned()
		c.abandon(w)
		return ctx.Err()
	}
}

// Capture reads hwnd's Window State within budget and stores the live
// Elements behind it. A new read of a Window replaces the previous one
// (ticket 03): the old live Elements are released, so Element Indexes from
// an earlier Window State can no longer resolve — they fail as stale in
// later tickets instead of pointing at the wrong Element.
//
// A nearly empty first read is retried once after a short delay
// (Chromium/Electron enable their accessibility tree lazily, at the first
// UI Automation contact; see retry.go), so the Window State of Chrome,
// Edge and Electron apps includes their web content, and a tree that
// stays nearly empty carries a note pointing the agent at a screenshot.
//
// Replacement is generation-aware (ticket 14's finding): the read takes a
// sequence number when it starts, and its Elements are installed only if
// they are newer than what is stored. A read whose job completes late —
// an abandoned wedged worker whose provider call eventually returns after
// a newer read already stored its Elements — releases its own Elements
// instead of clobbering the newer Window State.
//
// title and app fill the Window State's header (the resolved Window's title
// and executable file name).
func (c *Client) Capture(ctx context.Context, hwnd uint64, title, app string, budget time.Duration) (*wstate.Tree, error) {
	// One read is one job on the COM thread; the retry delay is spent on
	// the caller's goroutine so the worker stays free. Each successful read
	// replaces the Window's live Elements, so the store ends up holding the
	// Elements behind the Window State that is returned.
	read := func(cfg WalkConfig) (*readResult, error) {
		seq := c.store.beginRead()
		var res *captureResult
		var jobErr error
		runErr := c.Run(ctx, func(w *worker, r *run) {
			res, jobErr = w.thread.capture(uintptr(hwnd), title, app, cfg)
			if jobErr == nil {
				c.installRead(hwnd, seq, res, r)
			}
		})
		if runErr != nil {
			return nil, runErr
		}
		if jobErr != nil {
			return nil, jobErr
		}
		return &readResult{tree: res.tree, rootClass: res.rootClass}, nil
	}
	return readTree(ctx, app, budget, read, sleepCtx)
}

// installRead installs a completed read's Elements as hwnd's live Window
// State — unless the run was abandoned between the read and this point
// (finding 5): an abandoned worker is winding down and will leave COM, and
// Elements created on its apartment would dangle after CoUninitialize, so
// the read releases its own Elements instead, whatever the stored
// generation. The abandoned flag and the install decision share the run's
// mutex, so the mark cannot land between the check and the install.
func (c *Client) installRead(hwnd uint64, seq uint64, res *captureResult, r *run) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.abandoned {
		for _, p := range res.live {
			c.store.release(p)
		}
		return
	}
	c.store.install(hwnd, seq, res.live, res.tree.Elements)
}

// responseMargin is kept between the walk's budget and the request deadline
// so a partial tree still renders and answers in time.
const responseMargin = 500 * time.Millisecond

// walkConfig bounds one tree read. The walk's soft limit (budget) also
// respects the request's hard deadline, minus the response margin to
// render and answer within it. It is re-derived per read, so the retry's
// second read gets whatever time the request has left.
func walkConfig(ctx context.Context, budget time.Duration) WalkConfig {
	cfg := WalkConfig{
		MaxNodes:    DefaultMaxNodes,
		MaxDepth:    DefaultMaxDepth,
		MaxChildren: DefaultMaxChildren,
		Budget:      budget,
	}
	if d, ok := ctx.Deadline(); ok {
		if slack := time.Until(d) - responseMargin; slack > 0 && slack < cfg.Budget {
			cfg.Budget = slack
		}
	}
	if cfg.Budget <= 0 {
		cfg.Budget = time.Millisecond // a non-positive budget still walks the root
	}
	return cfg
}

// store keeps the live Elements behind each Window's latest Window State.
// It is client-level (not worker-level): live UIA elements belong to the
// process's MTA, so they stay valid when a wedged worker is replaced. All
// COM calls on them still go through Run, which guarantees a live worker.
//
// Replacements are generation-aware: every read takes a sequence number
// when it starts (beginRead) and may install its Elements only when it is
// newer than what is stored (install). A read whose job completes after a
// newer read stored its Elements — the wedged-worker case, where an
// abandoned job's provider call eventually returns late — releases its own
// Elements instead, so it can neither clobber the newer Window State the
// agent actually saw nor release that state's Elements under a running
// Action resolving an Element Index. Abandoned runs are refused outright
// (installRead, finding 5).
//
// Hand-outs are AddRef'd (element): a newer install releases only the
// stored reference, so an Element an Action is using cannot be freed
// under it; the Action releases its own reference on the worker thread
// when it is done (finding 4).
type store struct {
	mu      sync.Mutex
	byHwnd  map[uint64]*windowState
	next    uint64       // read sequence counter
	addRef  func(comPtr) // addRef, injectable for tests
	release func(comPtr) // release, injectable for tests
}

// windowState is one Window's stored live Elements, the Window State
// they back (its Elements, the name:/id: reference space — ticket 11),
// and the read that produced them.
type windowState struct {
	seq      uint64
	elems    []comPtr
	elements []wstate.Element
}

func newStore(addRef, release func(comPtr)) *store {
	return &store{byHwnd: map[uint64]*windowState{}, addRef: addRef, release: release}
}

// beginRead allocates the sequence number of a tree read that is starting.
func (s *store) beginRead() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	return s.next
}

// install stores elems (and the Window State elements they back) as
// hwnd's live Elements when seq is newer than the stored read's,
// releasing the previous Elements. A superseded read (an abandoned
// worker's job completing after a newer read) has its elems released
// instead; the newer Window State is left untouched. Callers run on a
// COM thread — the worker whose walk produced the Elements.
func (s *store) install(hwnd uint64, seq uint64, elems []comPtr, elements []wstate.Element) {
	s.mu.Lock()
	st := s.byHwnd[hwnd]
	if st != nil && seq <= st.seq {
		s.mu.Unlock()
		for _, p := range elems {
			s.release(p)
		}
		return
	}
	var old []comPtr
	if st != nil {
		old = st.elems
	}
	if len(elems) == 0 {
		delete(s.byHwnd, hwnd)
	} else {
		s.byHwnd[hwnd] = &windowState{seq: seq, elems: elems, elements: elements}
	}
	s.mu.Unlock()
	for _, p := range old {
		s.release(p)
	}
}

// element returns the live element for an Element Index of hwnd's latest
// Window State, AddRef'd for the caller: a newer install releases the
// stored reference, and without a reference of its own the caller's
// Element could be freed while it still uses it. The caller releases it
// when done (on the worker thread, through the same store).
func (s *store) element(hwnd uint64, index int) (comPtr, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.byHwnd[hwnd]
	if st == nil || index < 0 || index >= len(st.elems) {
		return nil, false
	}
	el := st.elems[index]
	if el != nil {
		s.addRef(el)
	}
	return el, true
}

// state returns the Elements of hwnd's latest stored Window State — the
// space name:/id: references resolve against (ticket 11). Pure metadata:
// no COM, no worker; the live Element behind a resolved Index is probed
// by the Action's own step.
func (s *store) state(hwnd uint64) ([]wstate.Element, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.byHwnd[hwnd]
	if st == nil {
		return nil, false
	}
	return st.elements, true
}

// releaseAll drops every stored Window. Called on the worker thread.
func (s *store) releaseAll() {
	s.mu.Lock()
	all := s.byHwnd
	s.byHwnd = map[uint64]*windowState{}
	s.mu.Unlock()
	for _, st := range all {
		for _, p := range st.elems {
			s.release(p)
		}
	}
}
