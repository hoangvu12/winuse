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

// workerInit serializes the workers' start-up: the first UI Automation
// call is not re-entrant across threads (multi-agent ticket 07's
// research), so each pool worker initialises COM and creates its
// IUIAutomation2 alone, and takes work only after that.
var workerInit sync.Mutex

// startWorker starts a fresh worker thread. It returns only after the
// thread has initialised COM and created IUIAutomation2 (or failed), one
// worker at a time (workerInit).
func startWorker() (*worker, error) {
	workerInit.Lock()
	defer workerInit.Unlock()
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
		// Provider-connection setup is bounded to 1s. Transactions get a
		// fixed timeout; the walk re-puts it before every cache chunk and
		// restores it after (capture.go, Children).
		call(auto, uia2PutConnectionTimeout, 1000)
		putTransactionTimeout(auto, workerTransactionTimeout)
		// AutoSetFocus defaults to TRUE: UIA then focuses the target Element
		// before every control pattern call (Invoke, Toggle, SetValue, ...),
		// and focusing moves the desktop foreground (agent-desktop A3-4) —
		// Headless Actions would steal it (ADR 0010; measured live on the
		// fixture and on Chrome). Off: a Headed set-value focuses explicitly.
		call(auto, uia2PutAutoSetFocus, 0)
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
// wait for a busy worker — finding 2's lesson, from wgc's worker, where
// a parked send holding the lock blocked close (and with it the
// watchdog's abandon), deadlocking every later request; instead senders
// park in a select that also watches stop, so closing stop releases
// them. jobs is never closed, so no send can panic (finding 1).
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

// Capture reads hwnd's Window State within budget and stores the live
// Elements behind it. A new read of a Window replaces the previous one
// (ticket 03): the old live Elements are released, so Element Indexes from
// an earlier Window State can no longer resolve — they fail as stale in
// later tickets instead of pointing at the wrong Element.
//
// Chromium/Electron enable their accessibility tree lazily, at the first
// UI Automation contact (see retry.go): a Chromium Window whose web content
// reads thin, and is not read over a Browser Connection, is read again
// until it settles, within DefaultChromiumSettle (or the wider budget
// WithSettle put on ctx, still bounded by its deadline), and one that stays thin
// carries a note saying so (a Window that already stayed thin once gets one
// short retry instead, until a read finds it built); any other nearly empty first read is retried
// once after a short delay, and a tree that stays nearly empty carries a
// note pointing the agent at a screenshot.
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
	tree, _, err := c.CaptureFrame(ctx, hwnd, title, app, budget, nil)
	return tree, err
}

// CaptureFrame is Capture for a browser Window whose Page is read over CDP
// (browser merge ticket 04): the walk leaves out the descendants of the
// Document Element cut names (WalkConfig.Cut), so the content the Page
// replaces is never read through UI Automation. at is that Document's
// Element Index, or -1 when every Document was read whole. A nil cut is
// Capture.
func (c *Client) CaptureFrame(ctx context.Context, hwnd uint64, title, app string, budget time.Duration, cut DocumentCut) (tree *wstate.Tree, at int, err error) {
	// One read is one job on the COM thread; the retry delay is spent on
	// the caller's goroutine so the worker stays free. Each successful read
	// replaces the Window's live Elements, so the store ends up holding the
	// Elements behind the Window State that is returned (the last read's,
	// which at follows).
	at = -1
	read := func(cfg WalkConfig) (*readResult, error) {
		cfg.Cut = cut
		seq := c.store.beginRead()
		var res *captureResult
		var jobErr error
		runErr := c.Run(ctx, func(w *worker, r *run) {
			res, jobErr = w.thread.capture(uintptr(hwnd), title, app, cfg)
			if jobErr == nil {
				c.installRead(storeAgent(ctx), hwnd, seq, res, r)
			}
		})
		if runErr != nil {
			return nil, runErr
		}
		if jobErr != nil {
			return nil, jobErr
		}
		at = res.cut
		return &readResult{tree: res.tree, rootClass: res.rootClass, paged: res.cut >= 0}, nil
	}
	tree, err = readTree(ctx, app, budget, SettleOf(ctx), c.thin.window(hwnd), read, sleepCtx)
	if err != nil {
		return nil, -1, err
	}
	return tree, at, nil
}

// installRead installs a completed read's Elements as hwnd's live Window
// State — unless the run was abandoned between the read and this point
// (finding 5): an abandoned worker is winding down and will leave COM, and
// Elements created on its apartment would dangle after CoUninitialize, so
// the read releases its own Elements instead, whatever the stored
// generation. The abandoned flag and the install decision share the run's
// mutex, so the mark cannot land between the check and the install.
func (c *Client) installRead(agent string, hwnd uint64, seq uint64, res *captureResult, r *run) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.abandoned {
		for _, p := range res.live {
			c.store.release(p)
		}
		return
	}
	c.store.install(agent, hwnd, seq, res.live, res.tree.Elements, res.partial)
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
	if skeleton(ctx) {
		cfg.MaxDepth, cfg.CountHidden = SkeletonDepth, true
	}
	return cfg
}
