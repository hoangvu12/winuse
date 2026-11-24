package uia

// The ScrollItem route of an Element scroll by steps (recap-trial ticket
// 02): a list with no Scroll pattern on the axis, whose items offer
// ScrollItem. Frameworks that draw their own lists (AccessKit-based Rust
// UIs, many custom toolkits) expose the items and their ScrollItem pattern
// but no Scroll pattern on the list, and often a scroll bar whose
// RangeValue is read-only. Such a list scrolls Headless one item at a
// time: each step brings the item nearest past the list's edge in the
// direction into view through ScrollItem.ScrollIntoView.
//
// The list is the nearest Element at or above the named one whose ScrollItem
// children reach out of its bounds on the axis (listOverflows): a row's own
// parts never do, the list's rows do, so naming a row, a word inside it, the
// list or a scroll bar beside the rows all find the list. A virtualized list
// builds the next rows as it scrolls, so every step reads the items again.
//
// The read-back is the item's bounds: a step that moved nothing tries the
// next item past the edge once; a first step that moved nothing fails as
// delivered ("it may already be at the end"), a later one ends the run as
// a partial move. With no item past the edge at the first step nothing is
// sent (ErrScrollEnd).

import (
	"errors"
	"fmt"
	"sort"
	"time"
	"unsafe"

	"github.com/hoangvu12/winuse/internal/win"
)

// ErrScrollEnd is a scroll by steps through a list's items where no item
// lies past the list's edge in the direction: nothing was sent.
var ErrScrollEnd = errors.New("the list is already at its end")

// edgeSlack is how far an item may cross an edge and still count as
// inside: a pixel of rounding between a list's bounds and its items'.
const edgeSlack = 1

// triesPerStep bounds the ScrollIntoView calls one step makes: the item
// nearest past the edge, then the next one when the first did not move the
// list (a provider that thinks a cut item is shown enough).
const triesPerStep = 2

// emptyRect reports bounds with no area: an Element with no place on
// screen.
func emptyRect(r rect) bool { return r.Right <= r.Left || r.Bottom <= r.Top }

// intersect is the overlap of a and b; empty when they do not overlap.
func intersect(a, b rect) rect {
	return rect{Left: max(a.Left, b.Left), Top: max(a.Top, b.Top), Right: min(a.Right, b.Right), Bottom: min(a.Bottom, b.Bottom)}
}

// listOverflows reports whether any item with bounds reaches out of view
// on the axis, beyond edgeSlack: the list has more than it shows that way.
func listOverflows(view rect, items []rect, vertical bool) bool {
	for _, it := range items {
		if emptyRect(it) {
			continue
		}
		if vertical && (it.Top < view.Top-edgeSlack || it.Bottom > view.Bottom+edgeSlack) {
			return true
		}
		if !vertical && (it.Left < view.Left-edgeSlack || it.Right > view.Right+edgeSlack) {
			return true
		}
	}
	return false
}

// itemsPastEdge lists the items that reach past view's edge in the
// direction (down: below the bottom; up: above the top; likewise right and
// left), nearest the edge first. Items with no bounds are skipped.
func itemsPastEdge(view rect, items []rect, dir ScrollDirection) []int {
	var out []int
	for i, it := range items {
		if emptyRect(it) {
			continue
		}
		past := false
		switch dir {
		case ScrollDown:
			past = it.Bottom > view.Bottom+edgeSlack
		case ScrollUp:
			past = it.Top < view.Top-edgeSlack
		case ScrollRight:
			past = it.Right > view.Right+edgeSlack
		case ScrollLeft:
			past = it.Left < view.Left-edgeSlack
		}
		if past {
			out = append(out, i)
		}
	}
	// The near edge of each item, the way the list goes: the nearest first.
	near := func(i int) int32 {
		it := items[i]
		switch dir {
		case ScrollDown:
			return it.Bottom
		case ScrollUp:
			return -it.Top
		case ScrollRight:
			return it.Right
		}
		return -it.Left
	}
	sort.SliceStable(out, func(a, b int) bool { return near(out[a]) < near(out[b]) })
	return out
}

// itemScroller is the live list scrollItemsBySteps drives: the COM code
// binds it to a list's items, the offline tests to a fake list.
type itemScroller struct {
	read       func() (view rect, items []rect, err error) // the list's bounds and its items' now
	scrollInto func(sent, i int) error                     // ScrollIntoView on item i of the latest read; sent calls went before it
	moved      func(i int, before rect) bool               // whether item i left before (or the list dropped it) within verifyWait
}

// scrollItemsBySteps scrolls a list by steps through its items' ScrollItem
// pattern: each step reads the items, brings the one nearest past the edge
// into view, and checks it moved, trying the next item once when it did
// not. No item past the edge sends nothing at the first step (ErrScrollEnd)
// and ends the run later; a first step that moved nothing is a VerifyError;
// a call that fails after an earlier one was sent is a WriteError.
func scrollItemsBySteps(steps int, dir ScrollDirection, s itemScroller) error {
	if err := CheckScrollSteps(steps); err != nil {
		return err
	}
	sent := 0
	for step := 0; step < steps; step++ {
		view, items, err := s.read()
		if err != nil {
			if sent == 0 {
				return err
			}
			return nil // the list changed under an earlier step: it moved
		}
		cands := itemsPastEdge(view, items, dir)
		if len(cands) == 0 {
			if sent == 0 {
				return fmt.Errorf("%w: no item lies further %s", ErrScrollEnd, dir)
			}
			return nil // the end, reached part way
		}
		moved := false
		for _, i := range cands[:min(len(cands), triesPerStep)] {
			if err := s.scrollInto(sent, i); err != nil {
				if sent == 0 {
					return err
				}
				return &WriteError{What: fmt.Sprintf("scrolling %s (step %d of %d)", dir, step+1, steps), Err: err}
			}
			sent++
			if s.moved(i, items[i]) {
				moved = true
				break
			}
		}
		if !moved {
			if step == 0 {
				return &VerifyError{Action: "scroll", Expected: "a changed scroll position", Msg: didNotScrollMsg}
			}
			return nil
		}
	}
	return nil
}

// ---- the COM side ----

// propIsScrollItemAvailable is UIA_IsScrollItemPatternAvailablePropertyId.
const propIsScrollItemAvailable = 30035

// liveRect reads an Element's bounds now; ok is false when they cannot be
// read (stale is set when the Element is gone).
func liveRect(el comPtr) (r rect, ok, stale bool) {
	hr := call(el, elCurrentBoundingRect, uintptr(unsafe.Pointer(&r)))
	if hr.Failed() {
		return rect{}, false, hrStale(hr)
	}
	return r, true, false
}

// scrollItems reads node's control-view children that offer ScrollItem,
// with their bounds. The caller releases the Elements.
func (th *thread) scrollItems(node comPtr) ([]comPtr, []rect) {
	var cond comPtr
	if hr := call(th.auto, uiaGetControlViewCondition, uintptr(unsafe.Pointer(&cond))); hr.Failed() || cond == nil {
		return nil, nil
	}
	defer release(cond)
	kids, _ := findChildren(node, cond)
	var els []comPtr
	var rects []rect
	for _, k := range kids {
		r, ok, _ := liveRect(k)
		if !ok || !currentBool(k, propIsScrollItemAvailable) {
			release(k)
			continue
		}
		els = append(els, k)
		rects = append(rects, r)
	}
	return els, rects
}

// itemList finds the list a scroll on the axis drives through ScrollItem:
// el or its nearest ancestor (raw view) whose ScrollItem children reach out
// of its bounds on the axis. The climb ends at hwnd's own root Element and
// never reaches the desktop, whose children are other Apps' Windows. nil
// when none; the caller releases the list.
func (th *thread) itemList(hwnd uint64, el comPtr, vertical bool) comPtr {
	var walker comPtr
	if hr := call(th.auto, uiaGetRawViewWalker, uintptr(unsafe.Pointer(&walker))); hr.Failed() || walker == nil {
		walker = nil
	} else {
		defer release(walker)
	}
	addRef(el)
	cur := el // owned
	for depth := 0; depth <= maxScrollAncestors; depth++ {
		var parent comPtr
		if walker != nil {
			if hr := call(walker, walkerGetParent, uintptr(cur), uintptr(unsafe.Pointer(&parent))); hr.Failed() {
				parent = nil
			}
		}
		if parent == nil && cur != el {
			break // cur is the desktop
		}
		if box, ok, _ := liveRect(cur); ok && !emptyRect(box) {
			kids, rects := th.scrollItems(cur)
			releaseAll(kids)
			if listOverflows(box, rects, vertical) {
				if parent != nil {
					release(parent)
				}
				return cur
			}
		}
		atRoot := nativeWindow(cur) == hwnd
		release(cur)
		cur = parent
		if cur == nil || atRoot {
			break
		}
	}
	if cur != nil {
		release(cur)
	}
	return nil
}

// scrollList runs scrollItemsBySteps on a live list. Its view is its bounds
// within hwnd's Window, so a list taller than the Window still has an edge.
func (th *thread) scrollList(hwnd uint64, list comPtr, dir ScrollDirection, steps int) error {
	var kids []comPtr
	defer func() { releaseAll(kids) }()
	return scrollItemsBySteps(steps, dir, itemScroller{
		read: func() (rect, []rect, error) {
			releaseAll(kids)
			kids = nil
			box, ok, stale := liveRect(list)
			if stale {
				return rect{}, nil, fmt.Errorf("%w: the list is no longer available", ErrStale)
			}
			if !ok {
				return rect{}, nil, errors.New("reading the list's bounds failed")
			}
			if wr, wok := win.WindowRect(hwnd); wok && !win.IsMinimized(hwnd) {
				if in := intersect(box, rect{Left: wr.Left, Top: wr.Top, Right: wr.Right, Bottom: wr.Bottom}); !emptyRect(in) {
					box = in
				}
			}
			var rects []rect
			kids, rects = th.scrollItems(list)
			return box, rects, nil
		},
		scrollInto: func(sent, i int) error {
			return scrollItemIntoView(kids[i])
		},
		moved: func(i int, before rect) bool {
			deadline := time.Now().Add(verifyWait)
			for {
				now, ok, stale := liveRect(kids[i])
				if stale || (ok && now != before) {
					return true
				}
				if time.Now().After(deadline) {
					return false
				}
				time.Sleep(verifyInterval)
			}
		},
	})
}
