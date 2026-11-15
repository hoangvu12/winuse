package uia

// One huge list must not end the tree read (learn-from 08): a node whose
// children time out as one chunk is read one child at a time up to
// MaxChildren and cut with a note naming it, and the walk goes on with its
// siblings; the skeleton counts through a property-less count with its own
// short bound. Fake sources, no COM.

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/wstate"
)

// stepSource is a fakeSource that can also step through children one at a
// time (ChildStepper). Each step costs stepCost on the fake clock.
type stepSource struct {
	*fakeSource
	parent   map[*fakeNode]*fakeNode
	pos      map[*fakeNode]int
	stepErr  func(parent *fakeNode, i int) error // i: the child asked for
	after    func(n *fakeNode) (int, bool)
	stepCost time.Duration
	steps    int
}

func newStepSource(root *fakeNode) *stepSource {
	s := &stepSource{fakeSource: newFakeSource(root), parent: map[*fakeNode]*fakeNode{}, pos: map[*fakeNode]int{}}
	var link func(n *fakeNode)
	link = func(n *fakeNode) {
		for i, k := range n.kids {
			s.parent[k], s.pos[k] = n, i
			link(k)
		}
	}
	link(root)
	return s
}

func (s *stepSource) child(p *fakeNode, i int) (*fakeNode, NodeData, bool, error) {
	s.steps++
	s.elapsed += s.stepCost
	if s.stepErr != nil {
		if err := s.stepErr(p, i); err != nil {
			return nil, NodeData{}, false, err
		}
	}
	if i >= len(p.kids) {
		return nil, NodeData{}, false, nil
	}
	return p.kids[i], p.kids[i].data, true, nil
}

func (s *stepSource) FirstChild(n *fakeNode, _ time.Duration) (*fakeNode, NodeData, bool, error) {
	return s.child(n, 0)
}

func (s *stepSource) NextSibling(prev *fakeNode, _ time.Duration) (*fakeNode, NodeData, bool, error) {
	return s.child(s.parent[prev], s.pos[prev]+1)
}

func (s *stepSource) ItemsAfter(n *fakeNode) (int, bool) {
	if s.after == nil {
		return 0, false
	}
	return s.after(n)
}

// countSource is a fakeSource with a property-less count (ChildCounter).
type countSource struct {
	*fakeSource
	slow   map[*fakeNode]bool // counts that time out
	cost   time.Duration      // fake clock per count
	bounds []time.Duration
}

func (s *countSource) CountChildren(n *fakeNode, remaining time.Duration) (int, error) {
	s.bounds = append(s.bounds, remaining)
	s.elapsed += s.cost
	if s.slow[n] {
		return 0, ErrChunkTimedOut
	}
	return len(n.kids), nil
}

// hugeList is a list named Items of n list items, "Item 1" to "Item n".
func hugeList(n int) *fakeNode {
	items := make([]*fakeNode, n)
	for i := range items {
		items[i] = node(wstate.ControlTypeListItem, fmt.Sprintf("Item %d", i+1))
	}
	return node(wstate.ControlTypeList, "Items", items...)
}

// listWindow is a Window holding a list of n items and, after it, a button.
func listWindow(n int) (root, list *fakeNode) {
	list = hugeList(n)
	return node(wstate.ControlTypeWindow, "W", list, node(wstate.ControlTypeButton, "After the list")), list
}

// slowChunk makes list's one-chunk read time out.
func slowChunk(src *fakeSource, list *fakeNode) {
	src.childrenErr = func(n *fakeNode) error {
		if n == list {
			return ErrChunkTimedOut
		}
		return nil
	}
}

func itemNames(from, to int) string {
	var out []string
	for i := from; i <= to; i++ {
		out = append(out, fmt.Sprintf("Item %d", i))
	}
	return strings.Join(out, "|")
}

func TestWalkSlowListIsReadOneAtATimeAndCut(t *testing.T) {
	root, list := listWindow(12)
	src := newStepSource(root)
	slowChunk(src.fakeSource, list)
	src.after = func(n *fakeNode) (int, bool) { return 12 - (src.pos[n] + 1), true } // SizeOfSet - PositionInSet
	cfg := fullConfig()
	cfg.MaxChildren = 5
	res, err := Walk[*fakeNode](src, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := walkNames(res), "W|Items|"+itemNames(1, 5)+"|After the list"; got != want {
		t.Errorf("elements = %q, want %q", got, want)
	}
	want := partialNotePrefix + " the children of Element 1 were too slow to read at once, so the first 5 were read one at a time and 7 more items were not read"
	if len(res.Notes) != 1 || res.Notes[0] != want {
		t.Errorf("Notes = %q, want %q", res.Notes, want)
	}
	if !slices.Equal(res.Slow, []SlowChildren{{Index: 1, Read: 5, More: 7}}) {
		t.Errorf("Slow = %+v", res.Slow)
	}
	// The sixth item, read to learn there were more, is released.
	if len(res.Dropped) != 1 || res.Dropped[0] != list.kids[5] || len(res.Live) != len(res.Elements) {
		t.Errorf("dropped %d, live %d of %d", len(res.Dropped), len(res.Live), len(res.Elements))
	}
	if src.steps != 6 {
		t.Errorf("%d steps, want 6 (five items and the one past the cap)", src.steps)
	}
	if res.Elements[1].Truncated != nil {
		t.Errorf("the count is not exact, so no truncation suffix: %+v", res.Elements[1].Truncated)
	}
}

func TestWalkSlowListWithoutASetSize(t *testing.T) {
	root, list := listWindow(12)
	src := newStepSource(root)
	slowChunk(src.fakeSource, list)
	cfg := fullConfig()
	cfg.MaxChildren = 5
	res, _ := Walk[*fakeNode](src, cfg)
	want := partialNotePrefix + " the children of Element 1 were too slow to read at once, so the first 5 were read one at a time and the rest were not read"
	if len(res.Notes) != 1 || res.Notes[0] != want {
		t.Errorf("Notes = %q, want %q", res.Notes, want)
	}
}

// TestWalkSlowListUnderTheCapIsWhole: a slow chunk whose children all fit
// under MaxChildren is read whole, one at a time, with no note.
func TestWalkSlowListUnderTheCapIsWhole(t *testing.T) {
	root, list := listWindow(4)
	src := newStepSource(root)
	slowChunk(src.fakeSource, list)
	cfg := fullConfig()
	cfg.MaxChildren = 5
	res, _ := Walk[*fakeNode](src, cfg)
	if got, want := walkNames(res), "W|Items|"+itemNames(1, 4)+"|After the list"; got != want {
		t.Errorf("elements = %q, want %q", got, want)
	}
	if len(res.Notes) != 0 || len(res.Slow) != 0 || len(res.Dropped) != 0 || src.steps != 5 {
		t.Errorf("notes %q, slow %+v, dropped %d, steps %d", res.Notes, res.Slow, len(res.Dropped), src.steps)
	}
}

// TestWalkSlowListWhoseFirstStepTimesOutStops: when not even one child
// answers, the provider is not answering at all: the walk stops with the
// time note, as a timed-out chunk always did.
func TestWalkSlowListWhoseFirstStepTimesOutStops(t *testing.T) {
	root, list := listWindow(12)
	src := newStepSource(root)
	slowChunk(src.fakeSource, list)
	src.stepErr = func(*fakeNode, int) error { return ErrChunkTimedOut }
	res, _ := Walk[*fakeNode](src, fullConfig())
	if got := walkNames(res); got != "W|Items" {
		t.Errorf("elements = %q, want the walk stopped at the list", got)
	}
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "time limit") || len(res.Slow) != 0 {
		t.Errorf("Notes = %q, Slow = %+v", res.Notes, res.Slow)
	}
}

// TestWalkSlowListThatBreaksOffGoesOn: a step that fails after some
// children were read cuts the rest, and the walk goes on.
func TestWalkSlowListThatBreaksOffGoesOn(t *testing.T) {
	root, list := listWindow(12)
	src := newStepSource(root)
	slowChunk(src.fakeSource, list)
	src.stepErr = func(p *fakeNode, i int) error {
		if p == list && i == 2 {
			return errors.New("element gone")
		}
		return nil
	}
	res, _ := Walk[*fakeNode](src, fullConfig())
	if got, want := walkNames(res), "W|Items|Item 1|Item 2|After the list"; got != want {
		t.Errorf("elements = %q, want %q", got, want)
	}
	want := partialNotePrefix + " the children of Element 1 were too slow to read at once, so the first 2 were read one at a time and the rest were not read"
	if len(res.Notes) != 1 || res.Notes[0] != want {
		t.Errorf("Notes = %q", res.Notes)
	}
}

// TestWalkSlowListKeepsToTheBudgets: stepping stays within the walk's time
// budget and element cap; either one stops the walk as it always did.
func TestWalkSlowListKeepsToTheBudgets(t *testing.T) {
	root, list := listWindow(50)
	src := newStepSource(root)
	slowChunk(src.fakeSource, list)
	src.stepCost = time.Second
	res, _ := Walk[*fakeNode](src, WalkConfig{Budget: 4 * time.Second, Now: src.now})
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "exceeded the 4s time limit") || len(res.Slow) != 0 {
		t.Errorf("time: Notes = %q, Slow = %+v", res.Notes, res.Slow)
	}
	if strings.Contains(walkNames(res), "After the list") || len(res.Elements) > 2+4 {
		t.Errorf("time: read past the budget: %q", walkNames(res))
	}

	root, list = listWindow(50)
	src = newStepSource(root)
	slowChunk(src.fakeSource, list)
	cfg := fullConfig()
	cfg.MaxNodes = 4
	res, _ = Walk[*fakeNode](src, cfg)
	if got := walkNames(res); got != "W|Items|Item 1|Item 2" {
		t.Errorf("element cap: %q", got)
	}
	// Dropped: the item that would have been the fifth Element, and the button.
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "the element cap (4) was reached") || len(res.Dropped) != 2 {
		t.Errorf("element cap: Notes = %q, dropped %d", res.Notes, len(res.Dropped))
	}
}

// TestWalkTwoSlowListsAreBothNamed: each slow list is named in the one
// partial-tree note, after the other reasons.
func TestWalkTwoSlowListsAreBothNamed(t *testing.T) {
	a, b := hugeList(8), hugeList(3)
	root := node(wstate.ControlTypeWindow, "W", a, node(wstate.ControlTypePane, "P", b))
	src := newStepSource(root)
	src.childrenErr = func(n *fakeNode) error {
		if n == a || n == b {
			return ErrChunkTimedOut
		}
		return nil
	}
	src.stepErr = func(p *fakeNode, i int) error {
		if p == b {
			return errors.New("gone")
		}
		return nil
	}
	cfg := fullConfig()
	cfg.MaxChildren = 2
	res, _ := Walk[*fakeNode](src, cfg)
	// W 0, Items 1, Item 1 2, Item 2 3, P 4, Items 5.
	want := partialNotePrefix + " the children of Element 1 were too slow to read at once, so the first 2 were read one at a time and the rest were not read" +
		"; the children of Element 5 did not answer in time and were not read"
	if len(res.Notes) != 1 || res.Notes[0] != want {
		t.Errorf("Notes = %q\nwant %q", res.Notes, want)
	}
	if got := res.NotesWith(func(i int) string { return fmt.Sprintf("<%d>", i) }); len(got) != 1 ||
		!strings.Contains(got[0], "the children of <1> were") || !strings.Contains(got[0], "the children of <5> did") {
		t.Errorf("NotesWith = %q", got)
	}
}

// TestWalkLateCutForgetsASlowListUnderTheDocument: a slow list read under
// the Document a late cut removes leaves no note.
func TestWalkLateCutForgetsASlowListUnderTheDocument(t *testing.T) {
	tree := browserTree()
	doc := tree.kids[0].kids[1]
	group := doc.kids[0]
	known := false
	src := newFakeSource(tree)
	src.childrenErr = func(n *fakeNode) error {
		if n == group {
			known = true // the Page read ends while the slow group is read
			return ErrChunkTimedOut
		}
		return nil
	}
	cfg := fullConfig()
	cfg.Cut = lateCut("Page", &known)
	res, err := Walk[*fakeNode](src, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := walkNames(res); got != cutFrame || res.Cut != 4 {
		t.Errorf("elements = %q, cut %d", got, res.Cut)
	}
	if len(res.Notes) != 0 || len(res.Slow) != 0 {
		t.Errorf("the slow group under the cut Document left notes %q, slow %+v", res.Notes, res.Slow)
	}
}

// TestSkeletonCountsWithoutReadingTheChildren: a source with a count reads
// no child for it, bounds each count by countBudget, and a count that runs
// out of time leaves no number, a note, and the walk going on.
func TestSkeletonCountsWithoutReadingTheChildren(t *testing.T) {
	huge := hugeList(40)
	tree := node(wstate.ControlTypeWindow, "W",
		node(wstate.ControlTypePane, "A",
			node(wstate.ControlTypeGroup, "B",
				huge,
				node(wstate.ControlTypeList, "C", node(wstate.ControlTypeListItem, "1"), node(wstate.ControlTypeListItem, "2")))))
	src := &countSource{fakeSource: newFakeSource(tree), slow: map[*fakeNode]bool{huge: true}}
	cfg := fullConfig()
	cfg.MaxDepth, cfg.CountHidden = SkeletonDepth, true
	res, err := Walk[*fakeNode](src, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := walkNames(res); got != "W|A|B|Items|C" {
		t.Fatalf("read %q", got)
	}
	if res.Elements[3].MoreChildren != 0 || res.Elements[4].MoreChildren != 2 {
		t.Errorf("counts: Items %d, C %d", res.Elements[3].MoreChildren, res.Elements[4].MoreChildren)
	}
	if want := "the children of Element 3 were not counted in time"; len(res.Notes) != 1 || res.Notes[0] != want {
		t.Errorf("Notes = %q, want %q", res.Notes, want)
	}
	if !slices.Equal(res.Uncounted, []int{3}) || len(res.Dropped) != 0 {
		t.Errorf("uncounted %v, dropped %d: a count keeps no child", res.Uncounted, len(res.Dropped))
	}
	for _, n := range src.expanded {
		if n == huge {
			t.Error("the counted list's children were read")
		}
	}
	if len(src.bounds) != 2 || src.bounds[0] > countBudget || src.bounds[1] > countBudget {
		t.Errorf("count bounds %v, want each at most %s", src.bounds, countBudget)
	}

	// A count that runs into the walk's own budget stops the walk.
	src = &countSource{fakeSource: newFakeSource(tree), slow: map[*fakeNode]bool{huge: true}, cost: 10 * time.Second}
	res, _ = Walk[*fakeNode](src, WalkConfig{MaxDepth: SkeletonDepth, CountHidden: true, Budget: 8 * time.Second, Now: src.now})
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "time limit") || walkNames(res) != "W|A|B|Items" {
		t.Errorf("past the budget: %q, %q", walkNames(res), res.Notes)
	}
}

func TestAddedLabel(t *testing.T) {
	els := []wstate.Element{{Role: "window", Name: "W"}, {Role: "list", Name: "  Many \t items "}, {Role: "pane"}, {Role: "list", Name: strings.Repeat("x", 80)}}
	label := addedLabel(els, []int{0, 2}, []int{40, 41})
	for i, want := range []string{"Element 40", `the list "Many items"`, "Element 41", `the list "` + strings.Repeat("x", 57) + `..."`, "an Element"} {
		if got := label(i); got != want {
			t.Errorf("label(%d) = %q, want %q", i, got, want)
		}
	}
	// An abandoned run adds nothing: every Element is named by role and name.
	if got := addedLabel(els, []int{0, 2}, nil)(2); got != "an unnamed pane" {
		t.Errorf("not added: %q", got)
	}
}

// The walk's own cost on a 20,000-item list, without COM: read as one
// chunk and cut at MaxChildren, against one timed-out chunk and 1000 steps.
// The cross-process time this saves or costs is the live check's to
// measure (learn-from 08).
func BenchmarkWalkHugeListBatched(b *testing.B) {
	root, _ := listWindow(20000)
	for b.Loop() {
		if _, err := Walk[*fakeNode](newFakeSource(root), fullConfig()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWalkHugeListStepped(b *testing.B) {
	root, list := listWindow(20000)
	src := newStepSource(root)
	slowChunk(src.fakeSource, list)
	for b.Loop() {
		src.expanded = src.expanded[:0]
		if _, err := Walk[*fakeNode](src, fullConfig()); err != nil {
			b.Fatal(err)
		}
	}
}
