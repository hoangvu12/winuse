package uia

// The point hit-test (headless-max ticket 11), pure: which Element of a
// Window State a point names, and when it names none the Action can use.

import (
	"errors"
	"testing"

	"github.com/hoangvu12/winuse/internal/wstate"
)

// cand builds one candidate: depth, bounds, signature and route.
func cand(depth int, l, t, r, b int32, sig string, route bool) pointCand {
	return pointCand{depth: depth, bounds: rect{l, t, r, b}, readable: true, sig: sig, route: route}
}

func TestHitTestPicksTheSmallestElementThenClimbsToARoute(t *testing.T) {
	cands := []pointCand{
		cand(0, 0, 0, 800, 600, "window", false),       // 0 the Window's root
		cand(1, 0, 0, 800, 600, "pane", false),         // 1 the client area
		cand(2, 100, 100, 300, 140, "button", true),    // 2 Save
		cand(3, 110, 105, 200, 135, "text", false),     // 3 Save's label
		cand(2, 100, 200, 300, 400, "list", false),     // 4
		cand(3, 100, 200, 300, 220, "list item", true), // 5
	}
	cases := []struct {
		name       string
		x, y       int32
		under, act int
	}{
		{"the label climbs to its button", 150, 120, 3, 2},
		{"the button's own edge", 250, 120, 2, 2},
		{"a list item", 150, 210, 5, 5},
		{"the right edge is outside", 299, 219, 5, 5},
	}
	for _, tc := range cases {
		under, act, err := hitTest(cands, tc.x, tc.y)
		if err != nil || under != tc.under || act != tc.act {
			t.Errorf("%s: hitTest(%d,%d) = %d, %d, %v; want %d, %d", tc.name, tc.x, tc.y, under, act, err, tc.under, tc.act)
		}
	}
	// x 300 is past the list's and its item's right edge (right edges are
	// excluded): only the pane, which offers nothing, holds the point.
	var pe *PointError
	if _, _, err := hitTest(cands, 300, 210); !errors.As(err, &pe) || pe.Kind != PointNoRoute || pe.Index != 1 {
		t.Errorf("a point past the list's right edge = %v, want PointNoRoute on the pane (1)", err)
	}
}

func TestHitTestRefusesTheWindowOnly(t *testing.T) {
	cands := []pointCand{
		cand(0, 0, 0, 800, 600, "window", true),
		cand(1, 100, 100, 200, 200, "button", true),
	}
	_, _, err := hitTest(cands, 500, 500)
	var pe *PointError
	if !errors.As(err, &pe) || pe.Kind != PointWindowOnly {
		t.Fatalf("a point only the root contains = %v, want PointWindowOnly", err)
	}
}

func TestHitTestRefusesACanvasWithNoRoute(t *testing.T) {
	cands := []pointCand{
		cand(0, 0, 0, 800, 600, "window", true), // the root's route never counts
		cand(1, 0, 0, 800, 600, "pane", false),
		cand(2, 50, 50, 750, 550, "custom", false),
	}
	under, _, err := hitTest(cands, 400, 300)
	var pe *PointError
	if !errors.As(err, &pe) || pe.Kind != PointNoRoute || pe.Index != 2 || under != 2 {
		t.Fatalf("a canvas = %d, %v; want PointNoRoute on element 2", under, err)
	}
}

func TestHitTestOverlap(t *testing.T) {
	base := []pointCand{
		cand(0, 0, 0, 800, 600, "window", false),
		cand(1, 0, 0, 800, 600, "pane", false),
		cand(2, 100, 100, 300, 300, "button|||", true),              // 2
		cand(2, 200, 200, 400, 400, "check box||off||Toggle", true), // 3 overlaps 2 at 250,250
	}
	_, _, err := hitTest(base, 250, 250)
	var pe *PointError
	if !errors.As(err, &pe) || pe.Kind != PointAmbiguous {
		t.Fatalf("overlapping siblings that act differently = %v, want PointAmbiguous", err)
	}
	if !(pe.Index == 2 && pe.Other == 3) && !(pe.Index == 3 && pe.Other == 2) {
		t.Errorf("ambiguous between %d and %d, want 2 and 3", pe.Index, pe.Other)
	}

	same := append([]pointCand(nil), base...)
	same[3] = cand(2, 200, 200, 400, 400, "button|||", true)
	_, act, err := hitTest(same, 250, 250)
	if err != nil || (act != 2 && act != 3) {
		t.Errorf("overlapping siblings that act alike = %d, %v; want one of them", act, err)
	}

	// A child that sticks out of its parent is nested, not an overlap.
	nested := []pointCand{
		cand(0, 0, 0, 800, 600, "window", false),
		cand(1, 100, 100, 300, 300, "group", false),
		cand(2, 250, 250, 350, 350, "button", true),
	}
	if _, act, err := hitTest(nested, 260, 260); err != nil || act != 2 {
		t.Errorf("a child overlapping its own parent = %d, %v; want 2", act, err)
	}
}

func TestHitTestSkipsUnreadableBounds(t *testing.T) {
	cands := []pointCand{
		cand(0, 0, 0, 800, 600, "window", false),
		cand(1, 100, 100, 300, 300, "button", true),
		{depth: 2, sig: "text"}, // unreadable: never contains the point
	}
	if _, act, err := hitTest(cands, 150, 150); err != nil || act != 1 {
		t.Errorf("hitTest = %d, %v; want 1", act, err)
	}
}

func TestParentsFromDepths(t *testing.T) {
	cands := []pointCand{{depth: 0}, {depth: 1}, {depth: 2}, {depth: 2}, {depth: 1}, {depth: 2}}
	want := []int{-1, 0, 1, 1, 0, 4}
	got := parents(cands)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parents = %v, want %v", got, want)
		}
	}
}

func TestMetaRoute(t *testing.T) {
	cases := []struct {
		e      wstate.Element
		need   PointNeed
		action string
		want   bool
	}{
		{wstate.Element{Role: "edit", Settable: "string"}, NeedValue, "", true},
		{wstate.Element{Role: "check box", Toggle: "off"}, NeedValue, "", true},
		{wstate.Element{Role: "combo box"}, NeedValue, "", true},
		{wstate.Element{Role: "text"}, NeedValue, "", false},
		{wstate.Element{Role: "tree item", SecondaryActions: []string{"Expand"}}, NeedAction, "Expand", true},
		{wstate.Element{Role: "tree item", SecondaryActions: []string{"Expand"}}, NeedAction, "Toggle", false},
		{wstate.Element{Role: "text"}, NeedScroll, "", true},
		{wstate.Element{Role: "button"}, NeedClick, "", false}, // read live
	}
	for _, tc := range cases {
		if got := metaRoute(tc.e, tc.need, tc.action); got != tc.want {
			t.Errorf("metaRoute(%+v, %d, %q) = %v, want %v", tc.e, tc.need, tc.action, got, tc.want)
		}
	}
}
