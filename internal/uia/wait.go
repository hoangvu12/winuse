package uia

// The UI Automation side of the desktop waits (desktop-parity ticket 03):
// a tree read that leaves the stored Window State alone, for a text wait
// that polls the tree, and a held Element whose live state an element
// wait polls. Both poll; winuse subscribes to no UI Automation events
// (ADR 0004).

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/hoangvu12/winuse/internal/wstate"
)

// waitPollKey marks the context of a wait's poll (WithWaitPoll).
type waitPollKey struct{}

// WithWaitPoll marks ctx as the context of a wait's poll (desktop-parity
// 03). When it ends, Run gives the job up without abandoning the worker:
// a wait ends at its deadline as an ordinary outcome, so a poll still
// queued behind another request's job, or still reading, says nothing
// about a wedged worker, and abandoning it would fail the requests queued
// on it. A poll installs nothing, so a job left running is harmless; a
// worker that is really wedged is abandoned by the next request that
// overruns on it.
func WithWaitPoll(ctx context.Context) context.Context {
	return context.WithValue(ctx, waitPollKey{}, true)
}

func isWaitPoll(ctx context.Context) bool {
	poll, _ := ctx.Value(waitPollKey{}).(bool)
	return poll
}

// errNoCOMThread is a wait job on a fake worker (offline tests), which
// carries no COM thread to read with.
var errNoCOMThread = errors.New("the UIA worker has no COM thread")

// PeekTree reads hwnd's tree once within budget, as Capture does, but
// keeps the Window's stored Window State: the read's live Elements are
// released at once instead of replacing the stored ones, so the Element
// Indexes the agent holds keep resolving to the Elements they named. It
// skips Capture's Chromium settle: a wait polls, and its later reads see
// a lazily built tree as it grows.
func (c *Client) PeekTree(ctx context.Context, hwnd uint64, title, app string, budget time.Duration) (*wstate.Tree, error) {
	var res *captureResult
	var jobErr error
	err := c.Run(ctx, func(w *worker, _ *run) {
		if w.thread == nil {
			jobErr = errNoCOMThread
			return
		}
		res, jobErr = w.thread.capture(uintptr(hwnd), title, app, walkConfig(ctx, budget))
		if jobErr == nil {
			for _, p := range res.live {
				c.store.release(p)
			}
		}
	})
	if err != nil {
		return nil, err
	}
	if jobErr != nil {
		return nil, jobErr
	}
	return res.tree, nil
}

// ElementState is a held Element's live state, as an element wait reads
// it on every poll.
type ElementState struct {
	Enabled    bool
	HasValue   bool   // the Element offers the Value pattern
	Value      string // its value (compare it, never print it, when Secret)
	Secret     bool   // a password field
	Toggle     string // "on", "off" or "indeterminate"; "" without the Toggle pattern
	Selectable bool   // the Element offers the SelectionItem pattern
	Selected   bool
	Expand     string // "collapsed", "expanded", "partially expanded" or "a leaf"; "" without the ExpandCollapse pattern
}

// Held is one live Element an element wait holds from its start to its
// end (HoldElement): it keeps naming that Element whatever later reads
// of the Window store.
type Held interface {
	// State reads the Element's live state; an Element that is gone fails
	// with ErrStale.
	State(ctx context.Context) (ElementState, error)
	// Release gives the Element back. It does not wait for the UIA worker.
	Release()
}

// HoldElement resolves an Element Index of hwnd's latest Window State
// once, to the live Element behind it, and holds it for a wait: ErrNoState
// and ErrStale as for an Action, ErrPageElement for a Page Element. A hold
// whose job finishes after ctx expired is lost with its one reference.
func (c *Client) HoldElement(ctx context.Context, hwnd uint64, index int) (Held, error) {
	var el comPtr
	var jobErr error
	err := c.Run(ctx, func(_ *worker, _ *run) {
		el, jobErr = c.lookup(ctx, hwnd, index)
	})
	if err != nil {
		return nil, err
	}
	if jobErr != nil {
		return nil, jobErr
	}
	return &heldElement{c: c, el: el}, nil
}

// errHoldEnded is a State read after Release: a wait's poll still running
// when the wait ended (its result is dropped).
var errHoldEnded = errors.New("the wait that held the Element has ended")

// heldElement is a Held over the store's AddRef'd hand-out. A wait's poll
// can outlive the wait (pollUntil leaves a check that does not answer), so
// a State read can meet Release: the Element is taken under mu inside the
// read's job, and the last of Release and the reads in flight releases it,
// on a worker.
type heldElement struct {
	c *Client

	mu       sync.Mutex
	el       comPtr // nil once released
	reading  int    // State reads using el now
	released bool   // Release was called
}

func (h *heldElement) State(ctx context.Context) (ElementState, error) {
	var st ElementState
	var jobErr error
	err := h.c.Run(ctx, func(w *worker, _ *run) {
		el := h.use()
		if el == nil {
			jobErr = errHoldEnded
			return
		}
		defer h.done(el)
		if w.thread == nil {
			jobErr = errNoCOMThread
			return
		}
		st, jobErr = readElementState(el)
	})
	if err != nil {
		return ElementState{}, err
	}
	return st, jobErr
}

// use takes the Element for one read on the worker; nil after Release.
func (h *heldElement) use() comPtr {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.released || h.el == nil {
		return nil
	}
	h.reading++
	return h.el
}

// done ends one read; the last read after Release releases the Element,
// on the worker it ran on.
func (h *heldElement) done(el comPtr) {
	h.mu.Lock()
	h.reading--
	last := h.released && h.reading == 0 && h.el != nil
	if last {
		h.el = nil
	}
	h.mu.Unlock()
	if last {
		h.c.store.release(el)
	}
}

// Release releases the Element on the UIA worker, off the caller's
// goroutine: a worker busy with another request's read must not hold up
// the wait's answer. With a read in flight, that read releases it when it
// ends. A worker that winds down first leaks the reference.
func (h *heldElement) Release() {
	h.mu.Lock()
	el := h.el
	idle := !h.released && h.reading == 0
	h.released = true
	if idle {
		h.el = nil
	}
	h.mu.Unlock()
	if !idle || el == nil {
		return
	}
	go h.c.Run(context.Background(), func(_ *worker, _ *run) { h.c.store.release(el) })
}

// readElementState reads one live Element's state on the worker thread.
// IsEnabled is read first, with its HRESULT: a gone Element fails it as
// stale, and every other property is read only from a live one.
func readElementState(el comPtr) (ElementState, error) {
	var v variant
	if hr := call(el, elGetCurrentProperty, uintptr(propIsEnabled), uintptr(unsafe.Pointer(&v))); hr.Failed() {
		clearVariant(&v)
		if hrStale(hr) {
			return ElementState{}, staleErr(hr)
		}
		return ElementState{}, fmt.Errorf("reading the Element's state failed (HRESULT %s)", hr)
	}
	st := ElementState{Enabled: v.has(vtBool) && v.boolean()}
	clearVariant(&v)
	if st.HasValue = currentBool(el, propIsValueAvailable); st.HasValue {
		st.Value = currentString(el, propValueValue)
		st.Secret = currentBool(el, propIsPassword)
	}
	if currentBool(el, propIsToggleAvailable) {
		if s := currentState(el, propToggleState); s >= 0 {
			st.Toggle = toggleWord(s)
		}
	}
	if st.Selectable = currentBool(el, propIsSelectionItemAvailable); st.Selectable {
		st.Selected = currentBool(el, propSelectionItemIsSelected)
	}
	if currentBool(el, propIsExpandCollapseAvailable) {
		if s := currentState(el, propExpandCollapseState); s >= 0 {
			st.Expand = expandWord(s)
		}
	}
	return st, nil
}
