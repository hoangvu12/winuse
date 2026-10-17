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
	jobs   chan func() // unbuffered: one job in flight at a time
	exited chan struct{}
	thread *thread // set before the worker answers ready; read by jobs
}

// startWorker starts a fresh worker thread. It returns only after the
// thread has initialised COM and created IUIAutomation2 (or failed).
func startWorker() (*worker, error) {
	w := &worker{jobs: make(chan func()), exited: make(chan struct{})}
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
		for job := range w.jobs {
			job()
		}
		// Order: release the automation object while the MTA is alive, then
		// leave COM. Stored elements live in the client-level store and are
		// only ever used from a live worker (run ensures one exists), so the
		// MTA is never uninitialised under them.
		release(auto)
		coUninitialize()
		close(w.exited)
	}()
	if err := <-ready; err != nil {
		return nil, err
	}
	return w, nil
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
	return &Client{factory: startWorker, store: newStore(release)}
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
	// an uninitialized thread is not allowed).
	select {
	case w.jobs <- func() { c.store.releaseAll() }:
	case <-w.exited:
	}
	close(w.jobs)
	select {
	case <-w.exited:
	case <-time.After(2 * time.Second):
		// Worker wedged: leak it.
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
// replacement, and its job channel is closed so the thread winds down after
// its current job (the abandoned job's results are discarded).
func (c *Client) abandon(w *worker) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.w == w {
		c.w = nil
		close(w.jobs)
	}
}

// Run executes job on the worker's COM thread, bounded by ctx. A job that
// does not finish before ctx expires abandons the worker (its result is
// discarded) and returns ctx's error.
func (c *Client) Run(ctx context.Context, job func(w *worker)) error {
	w, err := c.ensureWorker()
	if err != nil {
		return err
	}
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
		job(w)
	}
	select {
	case w.jobs <- j:
	case <-ctx.Done():
		c.abandon(w)
		return ctx.Err()
	}
	select {
	case <-done:
		return perr
	case <-ctx.Done():
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
	cfg := WalkConfig{
		MaxNodes:    DefaultMaxNodes,
		MaxDepth:    DefaultMaxDepth,
		MaxChildren: DefaultMaxChildren,
		Budget:      budget,
	}
	// The walk's soft limit also respects the request's hard deadline,
	// minus a margin to render and answer within it.
	if d, ok := ctx.Deadline(); ok {
		if slack := time.Until(d) - responseMargin; slack > 0 && slack < cfg.Budget {
			cfg.Budget = slack
		}
	}
	if cfg.Budget <= 0 {
		cfg.Budget = time.Millisecond // a non-positive budget still walks the root
	}
	seq := c.store.beginRead()
	var tree *wstate.Tree
	var jobErr error
	runErr := c.Run(ctx, func(w *worker) {
		var live []comPtr
		tree, live, jobErr = w.thread.capture(uintptr(hwnd), title, app, cfg)
		if jobErr == nil {
			c.store.install(hwnd, seq, live)
		}
	})
	if runErr != nil {
		return nil, runErr
	}
	if jobErr != nil {
		return nil, jobErr
	}
	return tree, nil
}

// Element returns the live element behind (hwnd, elementIndex) from the
// latest Window State, for later Action tickets. ok is false when the
// Window has no stored state or the index is out of range.
func (c *Client) Element(hwnd uint64, elementIndex int) (comPtr, bool) {
	return c.store.element(hwnd, elementIndex)
}

// responseMargin is kept between the walk's budget and the request deadline
// so a partial tree still renders and answers in time.
const responseMargin = 500 * time.Millisecond

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
// Action resolving an Element Index.
type store struct {
	mu      sync.Mutex
	byHwnd  map[uint64]*windowState
	next    uint64       // read sequence counter
	release func(comPtr) // release, injectable for tests
}

// windowState is one Window's stored live Elements and the read that
// produced them.
type windowState struct {
	seq   uint64
	elems []comPtr
}

func newStore(release func(comPtr)) *store {
	return &store{byHwnd: map[uint64]*windowState{}, release: release}
}

// beginRead allocates the sequence number of a tree read that is starting.
func (s *store) beginRead() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	return s.next
}

// install stores elems as hwnd's live Elements when seq is newer than the
// stored read's, releasing the previous Elements. A superseded read (an
// abandoned worker's job completing after a newer read) has its elems
// released instead; the newer Window State is left untouched. Callers run
// on a COM thread — the worker whose walk produced the Elements.
func (s *store) install(hwnd uint64, seq uint64, elems []comPtr) {
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
		s.byHwnd[hwnd] = &windowState{seq: seq, elems: elems}
	}
	s.mu.Unlock()
	for _, p := range old {
		s.release(p)
	}
}

// element returns the live element for an Element Index of hwnd's latest
// Window State.
func (s *store) element(hwnd uint64, index int) (comPtr, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.byHwnd[hwnd]
	if st == nil || index < 0 || index >= len(st.elems) {
		return nil, false
	}
	return st.elems[index], true
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
