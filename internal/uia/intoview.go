package uia

// Scroll into view (headless-parity spec, "Element scroll"; recap-trial
// 04): one ScrollItem.ScrollIntoView, read back by geometry. Some
// providers never report an Element offscreen (AccessKit reads
// IsOffscreen false for rows above a list's top), so IsOffscreen alone
// cannot prove the Element moved into view. After the call the Element's
// bounds must lie inside the visible part of the container that scrolls
// it: its nearest ancestor with a Scroll pattern that scrolls, else the
// list whose ScrollItem items reach past its edge (scrollitems.go),
// clipped to the Window. IsOffscreen is an extra signal, and the only one
// when the Element, or the container, has no bounds or none is found.
//
// The verdict and the run (scrollIntoViewChecked) are pure and tested
// offline; the COM code below binds them to live Elements.

import (
	"time"
	"unsafe"

	"github.com/hoangvu12/winuse/internal/win"
)

// The delivered into-view whose Element's bounds are still outside the
// container's visible part: unmoved, or moved short of it.
const (
	intoViewStillMsg = "the Element did not scroll into view; its bounds are still outside the visible part of the list or pane that scrolls it"
	intoViewShortMsg = "the Element moved, but its bounds are still outside the visible part of the list or pane that scrolls it"
)

// intoViewState is one read of an into-view target.
type intoViewState struct {
	view           rect // the visible part of the container that scrolls the Element; empty: none known
	el             rect // the Element's bounds; empty: none
	offscreenKnown bool // IsOffscreen was readable
	offscreen      bool
}

// intoViewer is the live Element scrollIntoViewChecked drives: the COM code
// binds it to an Element and its container, the offline tests to a fake
// list.
type intoViewer struct {
	read   func() intoViewState
	scroll func() error                                      // the one ScrollIntoView call
	settle func(done func(intoViewState) bool) intoViewState // reads until done, or as it reads when verifyWait passes
}

// inView reports whether el lies inside view, edgeSlack allowed. On an axis
// where the Element is larger than the view it must fill the view instead.
// Empty bounds on either side are not judged.
func inView(view, el rect) (judged, in bool) {
	if emptyRect(view) || emptyRect(el) {
		return false, false
	}
	return true, axisIn(view.Top, view.Bottom, el.Top, el.Bottom) && axisIn(view.Left, view.Right, el.Left, el.Right)
}

func axisIn(v0, v1, e0, e1 int32) bool {
	if e1-e0 > v1-v0 {
		return e0 <= v0+edgeSlack && e1 >= v1-edgeSlack
	}
	return e0 >= v0-edgeSlack && e1 <= v1+edgeSlack
}

// intoViewShown is the read-back: the bounds inside the view when they can
// be judged, and IsOffscreen not reading true (intoViewDone).
func intoViewShown(st intoViewState) bool {
	if judged, in := inView(st.view, st.el); judged && !in {
		return false
	}
	return intoViewDone(st.offscreenKnown, st.offscreen)
}

// scrollIntoViewChecked reads the Element, makes the ScrollIntoView call
// and reads it back. A call that fails is its own error. Bounds still
// outside the view are a VerifyError (delivered) saying whether the
// Element moved; with no bounds to judge, IsOffscreen reading true is the
// IsOffscreen VerifyError. already is an Element inside the view before
// the call whose bounds did not change: nothing scrolled.
func scrollIntoViewChecked(s intoViewer) (already bool, err error) {
	before := s.read()
	judged, in := inView(before.view, before.el)
	if err := s.scroll(); err != nil {
		return false, err
	}
	after := s.settle(intoViewShown)
	if intoViewShown(after) {
		return judged && in && after.el == before.el, nil
	}
	if judged, in := inView(after.view, after.el); judged && !in {
		if after.el == before.el {
			return false, &VerifyError{Action: "scroll into-view", Expected: "in view", Msg: intoViewStillMsg}
		}
		return false, &VerifyError{Action: "scroll into-view", Expected: "in view", Msg: intoViewShortMsg}
	}
	return false, &VerifyError{Action: "scroll into-view", Expected: "on screen", Observed: "offscreen"}
}

// ---- the COM side ----

// scrollIntoViewLive runs scrollIntoViewChecked on a live Element.
func (th *thread) scrollIntoViewLive(hwnd uint64, el comPtr) (bool, error) {
	container := th.scrollContainer(hwnd, el)
	if container != nil {
		defer release(container)
	}
	read := func() intoViewState {
		var st intoViewState
		if r, ok, _ := liveRect(el); ok {
			st.el = r
		}
		if container != nil {
			st.view = visiblePart(hwnd, container)
		}
		v := currentProp(el, propIsOffscreen)
		st.offscreenKnown = v.has(vtBool)
		st.offscreen = st.offscreenKnown && v.boolean()
		clearVariant(&v)
		return st
	}
	return scrollIntoViewChecked(intoViewer{
		read:   read,
		scroll: func() error { return scrollItemIntoView(el) },
		settle: func(done func(intoViewState) bool) intoViewState {
			deadline := time.Now().Add(verifyWait)
			for {
				st := read()
				if done(st) || time.Now().After(deadline) {
					return st
				}
				time.Sleep(verifyInterval)
			}
		},
	})
}

// visiblePart is a container's bounds clipped to hwnd's Window; empty when
// the bounds cannot be read or lie outside the Window.
func visiblePart(hwnd uint64, container comPtr) rect {
	box, ok, _ := liveRect(container)
	if !ok || emptyRect(box) {
		return rect{}
	}
	if wr, wok := win.WindowRect(hwnd); wok && !win.IsMinimized(hwnd) {
		box = intersect(box, rect{Left: wr.Left, Top: wr.Top, Right: wr.Right, Bottom: wr.Bottom})
		if emptyRect(box) {
			return rect{}
		}
	}
	return box
}

// scrollContainer finds the container that scrolls el: its nearest
// ancestor (raw view) with a Scroll pattern that scrolls on either axis,
// else the nearest list above it whose ScrollItem items reach past its
// edge (itemList), as an Element scroll by steps picks them. The climbs
// end at hwnd's own root Element and never reach the desktop. nil when
// none; the caller releases it.
func (th *thread) scrollContainer(hwnd uint64, el comPtr) comPtr {
	if nativeWindow(el) == hwnd {
		return nil // el is the Window's root: nothing inside it scrolls it
	}
	var walker comPtr
	if hr := call(th.auto, uiaGetRawViewWalker, uintptr(unsafe.Pointer(&walker))); hr.Failed() || walker == nil {
		return nil
	}
	defer release(walker)
	var parent comPtr
	if hr := call(walker, walkerGetParent, uintptr(el), uintptr(unsafe.Pointer(&parent))); hr.Failed() || parent == nil {
		return nil
	}
	defer release(parent)

	addRef(parent)
	cur := parent // owned
	for depth := 0; depth <= maxScrollAncestors; depth++ {
		if pat, _ := currentPattern(cur, patternScroll, &iidScrollPattern); pat != nil {
			scrolls := patternBool(pat, scrollCurrentV) || patternBool(pat, scrollCurrentH)
			release(pat)
			if scrolls {
				return cur
			}
		}
		if nativeWindow(cur) == hwnd {
			break
		}
		var next comPtr
		if hr := call(walker, walkerGetParent, uintptr(cur), uintptr(unsafe.Pointer(&next))); hr.Failed() || next == nil {
			break
		}
		release(cur)
		cur = next
	}
	release(cur)
	for _, vertical := range []bool{true, false} {
		if list := th.itemList(hwnd, parent, vertical); list != nil {
			return list
		}
	}
	return nil
}
