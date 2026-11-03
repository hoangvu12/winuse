package uia

import (
	"context"
	"errors"
	"fmt"
	"unsafe"
)

// The element-side half of Actions (ticket 05): resolving an Element Index
// of a Window's latest Window State to the screen point to inject input at.
//
// The lookup and the COM calls all run as one job on the UIA worker thread
// (Client.Run): the worker serializes them with Capture jobs, so the live
// Elements behind a Window State cannot be replaced and released under a
// running action.

// Point is a point on the screen in physical pixels (the helper is
// per-monitor-v2 DPI aware, so UIA coordinates and SendInput coordinates
// share this space).
type Point struct{ X, Y int32 }

// ErrNoState reports that the Window has no cached Window State: no state
// was read for it in this helper's lifetime (the helper restarted, or the
// agent never read one).
var ErrNoState = errors.New("no cached window state")

// errNoState is ErrNoState for hwnd.
func errNoState(hwnd uint64) error {
	return fmt.Errorf("%w: window %d", ErrNoState, hwnd)
}

// NotReadByAgentError is ErrNoState for an Agent that has not read a
// Window another Agent has read (multi-agent ticket 03, ADR 0014 Decision
// 2): Window States are per Agent, so this Agent reads the Window before
// it acts on it. errors.Is matches ErrNoState.
type NotReadByAgentError struct{ WindowID uint64 }

func (e *NotReadByAgentError) Error() string {
	return fmt.Sprintf("window %d has no Window State read by this agent (another agent read it)", e.WindowID)
}

func (e *NotReadByAgentError) Unwrap() error { return ErrNoState }

// ErrStale reports that an Element Index no longer resolves to a live
// Element: the index is not in the Window's cached Window State, or the
// Element behind it is gone. The recovery is always to read the Window
// State again and use fresh Element Indexes.
var ErrStale = errors.New("stale element index")

// rect is the Win32 RECT (layout-identical to win.Rect; local so the COM
// layer stays self-contained).
type rect struct{ Left, Top, Right, Bottom int32 }

// ElementPoint resolves an Element Index of hwnd's latest Window State to
// the point to click: the Element's clickable point when one is available,
// otherwise the centre of its current bounds — after scrolling it into
// view when the Element offers ScrollItem (spec, Acting details).
func (c *Client) ElementPoint(ctx context.Context, hwnd uint64, index int) (Point, error) {
	var pt Point
	var jobErr error
	err := c.RunFor(ctx, hwnd, func(w *worker, _ *run) {
		el, lerr := c.lookup(ctx, hwnd, index)
		if lerr != nil {
			jobErr = lerr
			return
		}
		// The store AddRef'd the Element on hand-out (finding 4): release
		// our reference after the action, so a newer Window State that
		// replaced the stored one frees the Element exactly when the last
		// user is done with it.
		defer c.store.release(el)
		if w.thread == nil {
			return // fake workers carry no COM thread; tests exercise the miss paths
		}
		pt, jobErr = w.thread.elementPoint(el)
	})
	if err != nil {
		return Point{}, err
	}
	return pt, jobErr
}

// lookup resolves an Element Index of the requesting Agent's latest
// Window State of hwnd to its live element handle, on the worker thread,
// AddRef'd for the caller (the store's own reference can be released by a
// newer install while the caller uses the Element; release it through the
// store when done). A Window with no cached state for the Agent fails as
// ErrNoState (NotReadByAgentError when another Agent read it); an index
// beyond the stored one as ErrStale (both with the read-the-state-again
// recovery, mapped by the helper).
func (c *Client) lookup(ctx context.Context, hwnd uint64, index int) (comPtr, error) {
	who := storeAgent(ctx)
	el, ok := c.store.element(who, hwnd, index)
	if ok && el == nil {
		// A Page Element (splice.go): it has no UIA Element behind it.
		return nil, fmt.Errorf("%w: element %d of window %d", ErrPageElement, index, hwnd)
	}
	if ok {
		return el, nil
	}
	if c.store.has(who, hwnd) {
		return nil, fmt.Errorf("%w: element %d is not in the cached Window State of window %d", ErrStale, index, hwnd)
	}
	return nil, c.store.noState(who, hwnd)
}

// elementPoint computes the point to click for one live element, on the
// worker thread: scroll into view (best effort), then the clickable point
// or the bounds centre.
func (th *thread) elementPoint(el comPtr) (Point, error) {
	if err := scrollIntoView(el); err != nil {
		return Point{}, err
	}
	var pt struct{ X, Y int32 }
	var got int32
	if hr := call(el, elGetClickablePoint, uintptr(unsafe.Pointer(&pt)), uintptr(unsafe.Pointer(&got))); hr.Failed() {
		if hrStale(hr) {
			return Point{}, fmt.Errorf("%w: the Element is no longer available (HRESULT %s)", ErrStale, hr)
		}
		if hr != uiaErrNoClickablePoint {
			return Point{}, fmt.Errorf("reading the Element's clickable point failed (HRESULT %s)", hr)
		}
		// UIA_E_NOCLICKABLEPOINT: fall through to the bounds centre.
	} else if got != 0 {
		return Point{X: pt.X, Y: pt.Y}, nil
	}
	// No clickable point (FALSE or UIA_E_NOCLICKABLEPOINT): the centre of
	// the Element's current bounds.
	var r rect
	if hr := call(el, elCurrentBoundingRect, uintptr(unsafe.Pointer(&r))); hr.Failed() {
		if hrStale(hr) {
			return Point{}, fmt.Errorf("%w: the Element is no longer available (HRESULT %s)", ErrStale, hr)
		}
		return Point{}, fmt.Errorf("reading the Element's bounds failed (HRESULT %s)", hr)
	}
	if r.Left >= r.Right || r.Top >= r.Bottom {
		return Point{}, errors.New("the Element has no clickable point and no bounds")
	}
	return Point{X: (r.Left + r.Right) / 2, Y: (r.Top + r.Bottom) / 2}, nil
}

// elementFromPoint resolves a screen point to the live Element under it
// (the topmost one), on the worker thread — the element-side resolution
// of an @x,y Element Reference (IUIAutomation::ElementFromPoint; the
// POINT travels by value in one register, uia-go notes §2). The caller
// owns the returned Element.
func (th *thread) elementFromPoint(x, y int32) (comPtr, error) {
	var el comPtr
	pt := uintptr(uint32(x)) | uintptr(uint32(y))<<32
	if hr := call(th.auto, uiaElementFromPoint, pt, uintptr(unsafe.Pointer(&el))); hr.Failed() || el == nil {
		return nil, fmt.Errorf("reading the Element at the point failed (HRESULT %s)", hr)
	}
	return el, nil
}

// scrollIntoView asks the Element's ScrollItem pattern to bring it on
// screen (the same call as scroll into-view, scrollItemIntoView), best
// effort: a stale Element surfaces (the caller fails as stale), but any
// other failure, and an Element without the pattern, is ignored (the
// clickable point or the bounds centre may still be usable).
func scrollIntoView(el comPtr) error {
	if err := scrollItemIntoView(el); errors.Is(err, ErrStale) {
		return err
	}
	return nil
}
