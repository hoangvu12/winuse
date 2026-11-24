package uia

// Offline tests for the ScrollItem route of an Element scroll by steps: a
// list with no Scroll pattern whose items offer ScrollItem, scrolled one
// item past its edge at a time. The trees are generic: a view and the
// bounds of the items it lists. No COM is touched.

import (
	"errors"
	"testing"
)

// listView is a 200 px tall, 100 px wide viewport at y 100..300.
var listView = rect{Left: 0, Top: 100, Right: 100, Bottom: 300}

func TestListOverflows(t *testing.T) {
	for _, tc := range []struct {
		name     string
		items    []rect
		vertical bool
		want     bool
	}{
		{"items inside the view", []rect{{0, 100, 100, 150}, {0, 150, 100, 300}}, true, false},
		{"an item past the bottom", []rect{{0, 250, 100, 330}}, true, true},
		{"an item above the top", []rect{{0, 60, 100, 120}}, true, true},
		{"past the bottom is not horizontal overflow", []rect{{0, 250, 100, 330}}, false, false},
		{"an item past the right edge", []rect{{50, 120, 140, 160}}, false, true},
		{"an empty item does not count", []rect{{0, 0, 0, 0}, {0, 900, 100, 900}}, true, false},
		{"a pixel of rounding does not count", []rect{{0, 99, 100, 301}}, true, false},
		{"no items", nil, true, false},
	} {
		if got := listOverflows(listView, tc.items, tc.vertical); got != tc.want {
			t.Errorf("%s: listOverflows = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestItemsPastEdge(t *testing.T) {
	// A list scrolled part way: one item above the top, two in view, one
	// cut by the bottom edge, one wholly below it, one empty.
	items := []rect{
		{0, 40, 100, 90},   // 0: above the top
		{0, 90, 100, 140},  // 1: cut by the top edge
		{0, 140, 100, 280}, // 2: in view
		{0, 280, 100, 330}, // 3: cut by the bottom edge
		{0, 330, 100, 400}, // 4: below the bottom
		{0, 0, 0, 0},       // 5: no bounds
	}
	for _, tc := range []struct {
		dir  ScrollDirection
		want []int
	}{
		{ScrollDown, []int{3, 4}}, // nearest the edge first
		{ScrollUp, []int{1, 0}},
		{ScrollRight, nil},
		{ScrollLeft, nil},
	} {
		got := itemsPastEdge(listView, items, tc.dir)
		if !sameInts(got, tc.want) {
			t.Errorf("%s: itemsPastEdge = %v, want %v", tc.dir, got, tc.want)
		}
	}
	wide := []rect{{-80, 120, -10, 160}, {10, 120, 90, 160}, {90, 120, 170, 160}}
	if got := itemsPastEdge(listView, wide, ScrollRight); !sameInts(got, []int{2}) {
		t.Errorf("right: itemsPastEdge = %v, want [2]", got)
	}
	if got := itemsPastEdge(listView, wide, ScrollLeft); !sameInts(got, []int{0}) {
		t.Errorf("left: itemsPastEdge = %v, want [0]", got)
	}
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// virtualList is a virtualized vertical list: items of the given heights
// stacked from content y 0, shown in listView, scrolled by offset. Only the
// items within overscan of the view are in the tree, as a virtualized list
// builds them. ScrollIntoView brings an item wholly into view with the
// least scroll, unless the list ignores it.
type virtualList struct {
	heights  []int32
	offset   int32
	overscan int32
	ignore   bool // ScrollIntoView answers ok and does nothing
	failAt   int  // the 1-based ScrollIntoView call that fails; 0: none
	calls    int
	shown    []int // the item ids behind the latest read's indexes
}

var errIntoView = errors.New("ScrollIntoView failed")

func (f *virtualList) top(id int) int32 {
	var y int32
	for i := 0; i < id; i++ {
		y += f.heights[i]
	}
	return y
}

func (f *virtualList) total() int32 { return f.top(len(f.heights)) }

func (f *virtualList) viewH() int32 { return listView.Bottom - listView.Top }

// bounds is item id's screen rect now.
func (f *virtualList) bounds(id int) rect {
	y := listView.Top + f.top(id) - f.offset
	return rect{Left: 0, Top: y, Right: 100, Bottom: y + f.heights[id]}
}

// built reports whether item id is in the tree now.
func (f *virtualList) built(id int) bool {
	b := f.bounds(id)
	return b.Bottom > listView.Top-f.overscan && b.Top < listView.Bottom+f.overscan
}

func (f *virtualList) scroller() itemScroller {
	return itemScroller{
		read: func() (rect, []rect, error) {
			f.shown = f.shown[:0]
			var items []rect
			for id := range f.heights {
				if f.built(id) {
					f.shown = append(f.shown, id)
					items = append(items, f.bounds(id))
				}
			}
			return listView, items, nil
		},
		scrollInto: func(sent, i int) error {
			f.calls++
			if f.calls == f.failAt {
				return errIntoView
			}
			if f.ignore {
				return nil
			}
			id := f.shown[i]
			y0, y1 := f.top(id), f.top(id)+f.heights[id]
			switch {
			case y1 > f.offset+f.viewH():
				f.offset = y1 - f.viewH()
			case y0 < f.offset:
				f.offset = y0
			}
			f.offset = max(0, min(f.offset, f.total()-f.viewH()))
			return nil
		},
		moved: func(i int, before rect) bool {
			id := f.shown[i]
			return !f.built(id) || f.bounds(id) != before
		},
	}
}

func newVirtualList(n int, h int32) *virtualList {
	f := &virtualList{overscan: 60}
	for i := 0; i < n; i++ {
		f.heights = append(f.heights, h)
	}
	return f
}

func TestScrollItemsBySteps(t *testing.T) {
	t.Run("down from the top moves one item per step", func(t *testing.T) {
		f := newVirtualList(40, 50) // 2000 px of items in a 200 px view
		if err := scrollItemsBySteps(3, ScrollDown, f.scroller()); err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if f.calls != 3 || f.offset != 150 {
			t.Errorf("calls %d, offset %d; want 3 calls and the view 150 px down", f.calls, f.offset)
		}
	})
	t.Run("up from the middle", func(t *testing.T) {
		f := newVirtualList(40, 50)
		f.offset = 1000
		if err := scrollItemsBySteps(2, ScrollUp, f.scroller()); err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if f.offset != 900 {
			t.Errorf("offset = %d, want 900", f.offset)
		}
	})
	t.Run("an item cut by the edge is shown whole first", func(t *testing.T) {
		f := newVirtualList(40, 50)
		f.offset = 20 // the item at content 200..250 is cut by the bottom edge
		if err := scrollItemsBySteps(1, ScrollDown, f.scroller()); err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if f.offset != 50 {
			t.Errorf("offset = %d, want 50", f.offset)
		}
	})
	t.Run("at the end nothing is sent", func(t *testing.T) {
		f := newVirtualList(40, 50)
		f.offset = f.total() - f.viewH()
		err := scrollItemsBySteps(3, ScrollDown, f.scroller())
		if !errors.Is(err, ErrScrollEnd) {
			t.Fatalf("error = %v, want ErrScrollEnd", err)
		}
		if err.Error() != "the list is already at its end: no item lies further down" {
			t.Errorf("message = %q", err.Error())
		}
		if f.calls != 0 {
			t.Errorf("calls = %d, want none", f.calls)
		}
	})
	t.Run("at the top nothing is sent", func(t *testing.T) {
		f := newVirtualList(40, 50)
		if err := scrollItemsBySteps(1, ScrollUp, f.scroller()); !errors.Is(err, ErrScrollEnd) || f.calls != 0 {
			t.Fatalf("error = %v, calls %d; want ErrScrollEnd and no call", err, f.calls)
		}
	})
	t.Run("reaching the end part way is ok", func(t *testing.T) {
		f := newVirtualList(8, 50) // 400 px: 200 px to scroll
		if err := scrollItemsBySteps(50, ScrollDown, f.scroller()); err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if f.offset != 200 || f.calls != 4 {
			t.Errorf("offset %d, calls %d; want the end (200) after 4 calls", f.offset, f.calls)
		}
	})
	t.Run("a list that ignores ScrollIntoView did not scroll", func(t *testing.T) {
		f := newVirtualList(40, 50)
		f.ignore = true
		err := scrollItemsBySteps(3, ScrollDown, f.scroller())
		var verify *VerifyError
		if !errors.As(err, &verify) || err.Error() != didNotScrollMsg {
			t.Fatalf("error = %v, want the did-not-scroll VerifyError", err)
		}
		if f.calls != 2 {
			t.Errorf("calls = %d, want 2 (the item at the edge, then the next)", f.calls)
		}
	})
	t.Run("the first call failing is its own error", func(t *testing.T) {
		f := newVirtualList(40, 50)
		f.failAt = 1
		if err := scrollItemsBySteps(3, ScrollDown, f.scroller()); !errors.Is(err, errIntoView) {
			t.Fatalf("error = %v, want the call's error", err)
		}
	})
	t.Run("a later call failing may have taken effect", func(t *testing.T) {
		f := newVirtualList(40, 50)
		f.failAt = 2
		err := scrollItemsBySteps(3, ScrollDown, f.scroller())
		var write *WriteError
		if !errors.As(err, &write) {
			t.Fatalf("error = %v, want a WriteError", err)
		}
		if f.offset != 50 {
			t.Errorf("offset = %d, want the first step's 50", f.offset)
		}
	})
	t.Run("steps out of range send nothing", func(t *testing.T) {
		f := newVirtualList(40, 50)
		if err := scrollItemsBySteps(51, ScrollDown, f.scroller()); !errors.Is(err, ErrScrollSteps) || f.calls != 0 {
			t.Fatalf("error = %v, calls %d", err, f.calls)
		}
	})
}
