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
	"time"
	"unsafe"

	"github.com/hoangvu12/winuse/internal/wstate"
)

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
		el, jobErr = c.lookup(hwnd, index)
	})
	if err != nil {
		return nil, err
	}
	if jobErr != nil {
		return nil, jobErr
	}
	return &heldElement{c: c, el: el}, nil
}

// heldElement is a Held over the store's AddRef'd hand-out.
type heldElement struct {
	c  *Client
	el comPtr
}

func (h *heldElement) State(ctx context.Context) (ElementState, error) {
	var st ElementState
	var jobErr error
	err := h.c.Run(ctx, func(w *worker, _ *run) {
		if w.thread == nil {
			jobErr = errNoCOMThread
			return
		}
		st, jobErr = readElementState(h.el)
	})
	if err != nil {
		return ElementState{}, err
	}
	return st, jobErr
}

// Release releases the Element on the UIA worker, off the caller's
// goroutine: a worker busy with another request's read must not hold up
// the wait's answer. A worker that winds down first leaks the reference.
func (h *heldElement) Release() {
	el := h.el
	h.el = nil
	if el == nil {
		return
	}
	go h.c.Run(context.Background(), func(_ *worker, _ *run) { h.c.store.release(el) })
}

// propIsEnabled is UIA_IsEnabledPropertyId.
const propIsEnabled = 30010

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
