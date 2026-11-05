package uia

// Point forms resolved inside the target Window (headless-max ticket 11,
// ADR 0013's reversal of ADR 0010 Decision 1). An @x,y point is matched
// against the bounds of the Elements behind the Window's latest Window
// State, the read's own checked live where they decide the hit
// (pointBounds), never against the desktop: a point over a covering Window
// still names the target's own Element underneath. The smallest Element
// that contains the point wins, then the nearest Element up its ancestor
// chain that can take the Action. The Window's root never counts: a point
// that only the root contains names no Element.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/hoangvu12/winuse/internal/wstate"
)

// PointNeed is what an Action at a point needs from the Element there.
type PointNeed int

const (
	NeedClick  PointNeed = iota // a click route: Invoke, Toggle, SelectionItem, Expand/Collapse or a default action
	NeedValue                   // set-value: a settable value, a toggle, or a combo box or list to pick from
	NeedAction                  // perform-secondary-action: the named Secondary Action
	NeedScroll                  // scroll: any Element; ScrollElement climbs to the nearest scroller
)

// PointHit is the Element a point resolved to.
type PointHit struct {
	Index int    // its Element Index in the Window's latest Window State
	Role  string // its role, as the Window State prints it
	Name  string // its name ("" when unnamed)
}

// PointError is a point that names no Element the Action can use. Nothing
// was sent.
type PointError struct {
	Kind  PointErrorKind
	Index int // the Element under the point (PointNoRoute), or -1
	Other int // the overlapping Element (PointAmbiguous), or -1
	Role  string
}

// PointErrorKind classifies a PointError.
type PointErrorKind int

const (
	PointWindowOnly PointErrorKind = iota // no Element but the Window's root contains the point
	PointAmbiguous                        // overlapping Elements that are not nested differ in role or actions
	PointNoRoute                          // the Element there and its ancestors offer nothing for the Action
)

func (e *PointError) Error() string {
	switch e.Kind {
	case PointAmbiguous:
		return fmt.Sprintf("the point is on two overlapping Elements, %d and %d, that act differently", e.Index, e.Other)
	case PointNoRoute:
		return fmt.Sprintf("the point is on element %d (%s), and neither it nor an Element around it can take this Action without the pointer", e.Index, e.Role)
	}
	return "the point is on no Element of the Window, only on the Window itself"
}

// pointCand is one Element of the Window State with its live bounds.
type pointCand struct {
	depth    int
	bounds   rect
	readable bool   // the bounds were read; an unreadable Element never contains a point
	sig      string // role and actions: overlapping Elements with different signatures are ambiguous
	route    bool   // it can take the Action itself
}

// pointSig is an Element's signature for the overlap check: its role and
// what it offers.
func pointSig(e wstate.Element) string {
	return strings.Join([]string{e.Role, e.Settable, e.Toggle, e.ExpandState, strings.Join(e.SecondaryActions, ",")}, "|")
}

// contains reports whether r holds the point, right and bottom edges
// excluded (Windows rectangles).
func (r rect) contains(x, y int32) bool {
	return r.Right > r.Left && r.Bottom > r.Top && x >= r.Left && x < r.Right && y >= r.Top && y < r.Bottom
}

func (r rect) area() int64 {
	return int64(r.Right-r.Left) * int64(r.Bottom-r.Top)
}

// parents is each Element's parent index in a depth-first pre-order list
// (-1 for the root).
func parents(cands []pointCand) []int {
	out := make([]int, len(cands))
	var stack []int
	for i, c := range cands {
		for len(stack) > 0 && cands[stack[len(stack)-1]].depth >= c.depth {
			stack = stack[:len(stack)-1]
		}
		out[i] = -1
		if len(stack) > 0 {
			out[i] = stack[len(stack)-1]
		}
		stack = append(stack, i)
	}
	return out
}

// isAncestor reports whether a is an ancestor of i.
func isAncestor(parent []int, a, i int) bool {
	for p := parent[i]; p >= 0; p = parent[p] {
		if p == a {
			return true
		}
	}
	return false
}

// hitTest picks the Element at a point (pure): the smallest Element other
// than the root (index 0) whose bounds contain it, ties to the deeper and
// then the later one; an overlapping Element that is neither its ancestor
// nor its descendant and differs in signature makes the point ambiguous;
// then the nearest Element from it up to (not including) the root whose
// route holds. It returns the Element under the point and the one to act
// on.
func hitTest(cands []pointCand, x, y int32) (under, act int, err error) {
	best := -1
	for i := 1; i < len(cands); i++ {
		c := cands[i]
		if !c.readable || !c.bounds.contains(x, y) {
			continue
		}
		if best < 0 || c.bounds.area() < cands[best].bounds.area() ||
			(c.bounds.area() == cands[best].bounds.area() && c.depth >= cands[best].depth) {
			best = i
		}
	}
	if best < 0 {
		return -1, -1, &PointError{Kind: PointWindowOnly, Index: -1, Other: -1}
	}
	parent := parents(cands)
	for i := 1; i < len(cands); i++ {
		c := cands[i]
		if i == best || !c.readable || !c.bounds.contains(x, y) {
			continue
		}
		if isAncestor(parent, i, best) || isAncestor(parent, best, i) {
			continue
		}
		if c.sig != cands[best].sig {
			return best, -1, &PointError{Kind: PointAmbiguous, Index: best, Other: i}
		}
	}
	for i := best; i > 0; i = parent[i] {
		if cands[i].route {
			return best, i, nil
		}
	}
	return best, -1, &PointError{Kind: PointNoRoute, Index: best, Other: -1}
}

// metaRoute is the route an Element offers by its Window State line, for
// the needs that line answers: a value to set and a named Secondary
// Action. Scroll takes any Element; a click needs the live patterns.
func metaRoute(e wstate.Element, need PointNeed, action string) bool {
	switch need {
	case NeedValue:
		return e.Settable != "" || e.Toggle != "" || e.Role == "combo box" || e.Role == "list"
	case NeedAction:
		for _, a := range e.SecondaryActions {
			if a == action {
				return true
			}
		}
		return false
	case NeedScroll:
		return true
	}
	return false
}

// pointBatch is how often the bounds walk checks the request's deadline.
const pointBatch = 32

// ElementAtPoint resolves a screen point (physical pixels) to the Element
// of hwnd's latest Window State that an Action with need acts on (action:
// the canonical Secondary Action for NeedAction). It matches the stored
// Elements' bounds on the UIA worker (pointBounds), bounded by ctx, and
// for a click probes the live patterns of the Element under the point and
// its ancestors. Nothing is sent to the Window.
func (c *Client) ElementAtPoint(ctx context.Context, hwnd uint64, x, y int32, need PointNeed, action string) (PointHit, error) {
	var hit PointHit
	var jobErr error
	err := c.RunFor(ctx, hwnd, func(w *worker, _ *run) {
		who := storeAgent(ctx)
		elems, elements, ok := c.store.elements(who, hwnd)
		if !ok {
			jobErr = c.store.noState(who, hwnd)
			return
		}
		defer func() {
			for _, el := range elems {
				if el != nil {
					c.store.release(el)
				}
			}
		}()
		if w.thread == nil {
			jobErr = errors.New("no UI Automation thread")
			return
		}
		cands := make([]pointCand, len(elements))
		for i, e := range elements {
			cands[i] = pointCand{depth: e.Depth, sig: pointSig(e), route: metaRoute(e, need, action)}
		}
		kept := func(i int) bool { return i < len(elems) && elems[i] != nil } // not a Page Element, nor one the walk could not keep
		if err := pointBounds(ctx, cands, kept, func(i int) (rect, bool) {
			return elementRect(elems[i], elCachedBoundingRect)
		}, func(i int) (rect, bool) {
			return elementRect(elems[i], elCurrentBoundingRect)
		}, x, y); err != nil {
			jobErr = err
			return
		}
		if need == NeedClick {
			// The click route is read live, only along the chain that can
			// win: the smallest Element under the point and its ancestors.
			under, _, err := hitTest(cands, x, y)
			var pe *PointError
			if err != nil && !(errors.As(err, &pe) && pe.Kind == PointNoRoute) {
				jobErr = err
				return
			}
			parent := parents(cands)
			for i := under; i > 0; i = parent[i] {
				if i < len(elems) && elems[i] != nil {
					cands[i].route = hasClickRoute(elems[i])
				}
			}
		}
		_, act, err := hitTest(cands, x, y)
		if err != nil {
			var pe *PointError
			if errors.As(err, &pe) && pe.Index >= 0 && pe.Index < len(elements) {
				pe.Role = elements[pe.Index].Role
			}
			jobErr = err
			return
		}
		hit = PointHit{Index: act, Role: elements[act].Role, Name: elements[act].Name}
	})
	if err != nil {
		return PointHit{}, err
	}
	return hit, jobErr
}

// elementRect reads an Element's bounds through a get_*BoundingRectangle
// slot: the cached one is an in-process read of the walk's cache, the
// current one a round trip to the App.
func elementRect(el comPtr, slot uintptr) (rect, bool) {
	var r rect
	if hr := call(el, slot, uintptr(unsafe.Pointer(&r))); hr.Failed() {
		return rect{}, false
	}
	return r, true
}

// pointBounds fills the candidates' bounds for hitTest (speed ticket 03).
// It takes them from the read that produced the Window State (cached),
// then reads live the ones that decide the hit: every Element they put
// under the point, the chain from the smallest of them up to the root,
// and the root. When all of those are where the read saw them, the hit is
// the one the live bounds give, unless an Element moved onto the point
// since the read; when one moved, or an Element has no cached bounds,
// every Element is read live, one round trip each, as before. kept says
// which candidates have a live Element. It returns ctx's error when the
// request ends meanwhile.
func pointBounds(ctx context.Context, cands []pointCand, kept func(int) bool, cached, live func(int) (rect, bool), x, y int32) error {
	allLive := func() error {
		for i := range cands {
			if i%pointBatch == 0 {
				if err := ctx.Err(); err != nil {
					return err
				}
			}
			cands[i].bounds, cands[i].readable = rect{}, false
			if kept(i) {
				cands[i].bounds, cands[i].readable = live(i)
			}
		}
		return nil
	}
	for i := range cands {
		if !kept(i) {
			continue
		}
		r, ok := cached(i)
		if !ok {
			return allLive()
		}
		cands[i].bounds, cands[i].readable = r, true
	}
	check := []int{0}
	for i := 1; i < len(cands); i++ {
		if cands[i].readable && cands[i].bounds.contains(x, y) {
			check = append(check, i)
		}
	}
	if under, _, _ := hitTest(cands, x, y); under > 0 {
		parent := parents(cands)
		for i := parent[under]; i > 0; i = parent[i] {
			if !cands[i].bounds.contains(x, y) {
				check = append(check, i)
			}
		}
	}
	for _, i := range check {
		if !kept(i) {
			continue
		}
		if r, ok := live(i); !ok || r != cands[i].bounds {
			return allLive()
		}
	}
	return ctx.Err()
}

// hasClickRoute reports whether the Element offers the Headless click
// route's first step (headlessClick's order), without calling it.
func hasClickRoute(el comPtr) bool {
	for _, p := range []struct {
		id  uintptr
		iid *windows.GUID
	}{
		{patternInvoke, &iidInvokePattern},
		{patternToggle, &iidTogglePattern},
		{patternSelectionItem, &iidSelectionItemPattern},
	} {
		if pat, _ := currentPattern(el, p.id, p.iid); pat != nil {
			release(pat)
			return true
		}
	}
	if pat, _ := currentPattern(el, patternExpandCollapse, &iidExpandCollapsePattern); pat != nil {
		state := patternState(pat, ecCurrentState)
		release(pat)
		if state != expandLeafNode {
			return true
		}
	}
	return currentString(el, propLegacyDefaultAction) != ""
}
