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
	err := c.Run(ctx, func(w *worker) {
		el, lerr := c.lookup(hwnd, index)
		if lerr != nil {
			jobErr = lerr
			return
		}
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

// lookup resolves an Element Index of hwnd's latest Window State to its
// live element handle, on the worker thread. A Window with no cached
// state fails as ErrNoState; an index beyond the stored one as ErrStale
// (both with the read-the-state-again recovery, mapped by the helper).
func (c *Client) lookup(hwnd uint64, index int) (comPtr, error) {
	el, ok := c.store.element(hwnd, index)
	if ok {
		return el, nil
	}
	if c.store.has(hwnd) {
		return nil, fmt.Errorf("%w: element %d is not in the cached Window State of window %d", ErrStale, index, hwnd)
	}
	return nil, fmt.Errorf("%w: window %d", ErrNoState, hwnd)
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

// scrollIntoView asks the Element's ScrollItem pattern to bring it on
// screen, best effort: a stale Element surfaces (the caller fails as
// stale), but any other failure is ignored — the clickable point or the
// bounds centre may still be usable.
func scrollIntoView(el comPtr) error {
	var pat comPtr
	hr := call(el, elGetCurrentPatternAs, patternScrollItem,
		uintptr(unsafe.Pointer(&iidScrollItemPattern)), uintptr(unsafe.Pointer(&pat)))
	if hr.Failed() || pat == nil {
		return nil // not scrollable (or the pattern query failed): not fatal
	}
	defer release(pat)
	if hr := call(pat, scrollItemScrollIntoView); hr.Failed() && hrStale(hr) {
		return fmt.Errorf("%w: the Element is no longer available (HRESULT %s)", ErrStale, hr)
	}
	return nil
}

// has reports whether hwnd has a stored Window State (distinguishing a
// Window with no state at all from an index beyond the stored one).
func (s *store) has(hwnd uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.byHwnd[hwnd]
	return ok
}
