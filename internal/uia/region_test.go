package uia

// find, region reads and the skeleton (desktop-parity 04): the skeleton
// counts the children past its depth instead of noting a depth cut; find
// keeps every match or the first one; added Elements take the indexes
// after the Window State's own, which keep resolving to the same Elements,
// and name:/id: references prefer the read's own Elements; a full read
// replaces the added ones. Fake sources and pointers, no COM.

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/wstate"
)

func TestWalkSkeletonCountsHiddenChildren(t *testing.T) {
	tree := node(wstate.ControlTypeWindow, "W",
		node(wstate.ControlTypePane, "A",
			node(wstate.ControlTypeGroup, "B",
				node(wstate.ControlTypeList, "C",
					node(wstate.ControlTypeListItem, "1"), node(wstate.ControlTypeListItem, "2")),
				node(wstate.ControlTypeButton, "Leaf"))))
	src := newFakeSource(tree)
	cfg := fullConfig()
	cfg.MaxDepth, cfg.CountHidden = SkeletonDepth, true
	res, err := Walk[*fakeNode](src, cfg)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range res.Elements {
		names = append(names, e.Name)
	}
	if !slices.Equal(names, []string{"W", "A", "B", "C", "Leaf"}) {
		t.Fatalf("read %v", names)
	}
	if res.Elements[3].MoreChildren != 2 || res.Elements[4].MoreChildren != 0 {
		t.Errorf("counts: C %d, Leaf %d", res.Elements[3].MoreChildren, res.Elements[4].MoreChildren)
	}
	if len(res.Notes) != 0 || len(res.Dropped) != 2 {
		t.Errorf("notes %v, dropped %d: a skeleton is no depth cut, and the counted children are released", res.Notes, len(res.Dropped))
	}
	// Without CountHidden the same depth is a cap with its note.
	res, _ = Walk[*fakeNode](newFakeSource(tree), WalkConfig{MaxDepth: SkeletonDepth})
	if res.Elements[3].MoreChildren != 0 || len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "depth cap") {
		t.Errorf("capped walk: %+v %v", res.Elements[3], res.Notes)
	}
}

func TestPickMatches(t *testing.T) {
	els := []wstate.Element{{Role: "button", Name: "A"}, {Role: "text"}, {Role: "button", Name: "B"}}
	isButton := wstate.Query{Role: "button"}.Match
	count := 0
	if keep := pickMatches(els, isButton, false, false, &count); !slices.Equal(keep, []bool{true, false, true}) || count != 2 {
		t.Errorf("all: %v, %d", keep, count)
	}
	count = 0
	if keep := pickMatches(els, isButton, true, false, &count); !slices.Equal(keep, []bool{true, false, false}) || count != 1 {
		t.Errorf("first: %v, %d", keep, count)
	}
	count = 0
	if keep := pickMatches(els, isButton, false, true, &count); !slices.Equal(keep, []bool{false, false, false}) || count != 2 {
		t.Errorf("count: %v, %d", keep, count)
	}
}

func TestStoreAddsAfterTheWindowState(t *testing.T) {
	f := newFakeStore()
	c := &Client{store: f.store}
	read := ptrs(3)
	f.install("A", 101, f.beginRead(), read, []wstate.Element{{Name: "Window"}, {Name: "Save", AutomationId: "save"}, {Name: "Open"}})

	found := ptrs(2)
	first := f.add("A", 101, found, []wstate.Element{{Name: "Save", AutomationId: "save"}, {Name: "Deep", AutomationId: "deep"}})
	if first != 3 {
		t.Fatalf("first added index %d, want 3", first)
	}
	// The read's indexes keep their Elements; the added ones follow.
	for i, want := range []comPtr{read[0], read[1], read[2], found[0], found[1]} {
		if el, err := c.lookup(agentCtx("A"), 101, i); err != nil || el != want {
			t.Errorf("element %d = %v, %v", i, el, err)
		}
	}
	// name:/id: prefer the read's own Element over its added repeat, and
	// reach an added one only when the read has none.
	for ref, want := range map[string]int{"name:Save": 1, "id:save": 1, "name:Deep": 4, "id:deep": 4} {
		if got, err := c.ResolveElementRef(agentCtx("A"), 101, ref); err != nil || got != want {
			t.Errorf("%s = %d, %v, want %d", ref, got, err, want)
		}
	}
	if _, err := c.ResolveElementRef(agentCtx("A"), 101, "name:None"); !errors.As(err, new(*wstate.NoMatchRefError)) {
		t.Errorf("no match: %v", err)
	}
	// Another Agent's store is untouched.
	if _, err := c.lookup(agentCtx("B"), 101, 3); err == nil {
		t.Error("B sees A's added Elements")
	}
	// A full read replaces everything, the added Elements too.
	f.install("A", 101, f.beginRead(), ptrs(1), []wstate.Element{{Name: "Window"}})
	if f.released[found[0]] != 1 || f.released[found[1]] != 1 {
		t.Errorf("the added Elements were not released: %v", f.released)
	}
	if _, err := c.lookup(agentCtx("A"), 101, 3); !errors.Is(err, ErrStale) {
		t.Errorf("index 3 after a new read: %v, want stale", err)
	}
}

func TestStoreAddWithoutAWindowState(t *testing.T) {
	f := newFakeStore()
	c := &Client{store: f.store}
	found := ptrs(2)
	if first := f.add("A", 101, found, []wstate.Element{{Name: "X"}, {Name: "X"}}); first != 0 {
		t.Fatalf("first = %d, want 0", first)
	}
	var several *wstate.AmbiguousRefError
	if _, err := c.ResolveElementRef(agentCtx("A"), 101, "name:X"); !errors.As(err, &several) || !slices.Equal(several.Matches, []int{0, 1}) {
		t.Errorf("two added X: %v", err)
	}
}

func TestResolveBaseFirstOffsetsAddedCandidates(t *testing.T) {
	els := []wstate.Element{{Name: "W"}, {Name: "Item"}, {Name: "Item"}}
	var several *wstate.AmbiguousRefError
	if _, err := resolveBaseFirst(els, 1, "name:Item"); !errors.As(err, &several) || !slices.Equal(several.Matches, []int{1, 2}) ||
		!strings.HasPrefix(several.Candidates[0], "1 ") || !strings.HasPrefix(several.Candidates[1], "2 ") {
		t.Errorf("ambiguous among added: %v %+v", err, several)
	}
}

func TestPartialOnly(t *testing.T) {
	if got := partialOnly([]string{nearEmptyNote, partialNotePrefix + " x"}); !slices.Equal(got, []string{partialNotePrefix + " x"}) {
		t.Errorf("notes %v", got)
	}
}
