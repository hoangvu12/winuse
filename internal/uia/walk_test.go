package uia

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/wstate"
)

// fakeNode is a synthetic tree node; fakeSource serves it to Walk with an
// injectable per-chunk cost (the fake clock advances by step on every
// Children call, so Budget can be exercised deterministically).
type fakeNode struct {
	data NodeData
	kids []*fakeNode
}

func node(controlType int32, name string, kids ...*fakeNode) *fakeNode {
	return &fakeNode{data: NodeData{ControlType: controlType, Name: name, Enabled: true}, kids: kids}
}

type fakeSource struct {
	root          *fakeNode
	rootErr       error
	childrenErr   func(n *fakeNode) error
	elapsed       time.Duration
	step          time.Duration
	base          time.Time
	childrenCalls int
}

func newFakeSource(root *fakeNode) *fakeSource {
	return &fakeSource{root: root, base: time.Now()}
}

func (s *fakeSource) now() time.Time { return s.base.Add(s.elapsed) }

func (s *fakeSource) Root() (*fakeNode, NodeData, error) {
	if s.rootErr != nil {
		return nil, NodeData{}, s.rootErr
	}
	return s.root, s.root.data, nil
}

func (s *fakeSource) Children(n *fakeNode, remaining time.Duration) ([]*fakeNode, []NodeData, error) {
	s.childrenCalls++
	s.elapsed += s.step
	if s.childrenErr != nil {
		if err := s.childrenErr(n); err != nil {
			return nil, nil, err
		}
	}
	kids := make([]*fakeNode, len(n.kids))
	data := make([]NodeData, len(n.kids))
	for i, k := range n.kids {
		kids[i] = k
		data[i] = k.data
	}
	return kids, data, nil
}

// A three-level tree: root is index 0, children follow depth-first.
func sampleTree() *fakeNode {
	return node(wstate.ControlTypeWindow, "Sample",
		node(wstate.ControlTypeMenuBar, "Menu Bar",
			node(wstate.ControlTypeMenuItem, "File"),
			node(wstate.ControlTypeMenuItem, "Edit"),
		),
		node(wstate.ControlTypeEdit, "Name"),
		node(wstate.ControlTypeButton, "OK"),
	)
}

func fullConfig() WalkConfig {
	return WalkConfig{Now: func() time.Time { return time.Now() }}
}

func TestWalkDepthFirstOrderAndIndexes(t *testing.T) {
	src := newFakeSource(sampleTree())
	res, err := Walk[*fakeNode](src, fullConfig())
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	want := []struct {
		depth int
		role  string
		name  string
	}{
		{0, "window", "Sample"},
		{1, "menu bar", "Menu Bar"},
		{2, "menu item", "File"},
		{2, "menu item", "Edit"},
		{1, "edit", "Name"},
		{1, "button", "OK"},
	}
	if len(res.Elements) != len(want) {
		t.Fatalf("got %d elements, want %d", len(res.Elements), len(want))
	}
	for i, w := range want {
		e := res.Elements[i]
		if e.Depth != w.depth || e.Role != w.role || e.Name != w.name {
			t.Errorf("element %d = (depth %d, %s, %q), want (depth %d, %s, %q)", i, e.Depth, e.Role, e.Name, w.depth, w.role, w.name)
		}
	}
	// Live parallels Elements; nothing dropped, no notes.
	if len(res.Live) != len(res.Elements) {
		t.Errorf("Live has %d handles, want %d", len(res.Live), len(res.Elements))
	}
	if len(res.Dropped) != 0 {
		t.Errorf("Dropped = %d, want 0", len(res.Dropped))
	}
	if len(res.Notes) != 0 {
		t.Errorf("Notes = %v, want none", res.Notes)
	}
	if res.FocusedIndex != -1 {
		t.Errorf("FocusedIndex = %d, want -1", res.FocusedIndex)
	}
}

func TestWalkFocusedElement(t *testing.T) {
	tree := sampleTree()
	tree.kids[1].data.Focused = true // the edit
	res, err := Walk[*fakeNode](newFakeSource(tree), fullConfig())
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if res.FocusedIndex != 4 {
		t.Fatalf("FocusedIndex = %d, want 4 (the edit)", res.FocusedIndex)
	}
}

func TestWalkDisabledState(t *testing.T) {
	tree := sampleTree()
	tree.kids[2].data.Enabled = false // the button
	res, _ := Walk[*fakeNode](newFakeSource(tree), fullConfig())
	if !res.Elements[5].Disabled {
		t.Error("the disabled button must render as disabled")
	}
}

func TestWalkUnknownControlTypeKeepsElement(t *testing.T) {
	tree := node(60000, "Odd", node(60001, "Odder"))
	res, err := Walk[*fakeNode](newFakeSource(tree), fullConfig())
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if res.Elements[0].Role != "control(60000)" || res.Elements[1].Role != "control(60001)" {
		t.Errorf("unknown roles = %q, %q", res.Elements[0].Role, res.Elements[1].Role)
	}
}

func TestWalkChildCapTruncatesWithMarker(t *testing.T) {
	kids := make([]*fakeNode, 5)
	for i := range kids {
		kids[i] = node(wstate.ControlTypeText, string(rune('a'+i)))
	}
	root := node(wstate.ControlTypeList, "List", kids...)
	cfg := fullConfig()
	cfg.MaxChildren = 2
	res, err := Walk[*fakeNode](newFakeSource(root), cfg)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	// Root + two of five children.
	if len(res.Elements) != 3 {
		t.Fatalf("got %d elements, want 3", len(res.Elements))
	}
	if len(res.Dropped) != 3 {
		t.Fatalf("Dropped = %d, want 3", len(res.Dropped))
	}
	tr := res.Elements[0].Truncated
	if tr == nil {
		t.Fatal("root must carry the truncation marker")
	}
	if tr.Limit != "2" || tr.OmittedChildren != 3 {
		t.Errorf("marker = (truncated: %s, omitted %d children), want (2, 3)", tr.Limit, tr.OmittedChildren)
	}
	// The child cap is named inline; no partial note for it.
	for _, n := range res.Notes {
		if strings.Contains(n, "partial") {
			t.Errorf("child cap alone must not add a partial note: %q", n)
		}
	}
}

func TestWalkNodeCapStopsWithNoteAndMarker(t *testing.T) {
	// A wide tree: 4 nodes of 3 children each under the root.
	kid := func(name string) *fakeNode { return node(wstate.ControlTypeText, name) }
	root := node(wstate.ControlTypeWindow, "W",
		kid("a0"), kid("a1"), kid("a2"),
		kid("b0"), kid("b1"), kid("b2"),
	)
	cfg := fullConfig()
	cfg.MaxNodes = 5 // root + 4 children
	res, err := Walk[*fakeNode](newFakeSource(root), cfg)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(res.Live) != 5 {
		t.Fatalf("visited %d elements, want 5", len(res.Live))
	}
	if len(res.Dropped) != 2 {
		t.Fatalf("Dropped = %d, want 2 (the remaining b children)", len(res.Dropped))
	}
	tr := res.Elements[0].Truncated
	if tr == nil || tr.Limit != "5" || tr.OmittedChildren != 2 {
		t.Errorf("root marker = %+v, want (5, omitted 2)", tr)
	}
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "the element cap (5) was reached") {
		t.Errorf("Notes = %v, want the element-cap note", res.Notes)
	}
}

func TestWalkDepthCapNotesWithoutMarker(t *testing.T) {
	// A chain 5 deep.
	var root *fakeNode = node(wstate.ControlTypeWindow, "W")
	tail := root
	for i := 0; i < 5; i++ {
		leaf := node(wstate.ControlTypeText, "t")
		tail.kids = []*fakeNode{leaf}
		tail = leaf
	}
	cfg := fullConfig()
	cfg.MaxDepth = 2 // root (0) and depth 1 are expanded; depth 2 rendered, not expanded
	res, err := Walk[*fakeNode](newFakeSource(root), cfg)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(res.Elements) != 3 {
		t.Fatalf("got %d elements, want 3 (depths 0,1,2)", len(res.Elements))
	}
	if res.Elements[2].Truncated != nil {
		t.Error("the depth cut has no omitted-children count and must not carry a marker")
	}
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "the depth cap (2) was reached") {
		t.Errorf("Notes = %v, want the depth-cap note", res.Notes)
	}
}

func TestWalkTimeBudgetReturnsPartialWithNote(t *testing.T) {
	// Every Children call costs 1s on the fake clock; the budget covers the
	// root's chunk plus one child chunk, then the deadline cuts the walk.
	tree := sampleTree()
	src := newFakeSource(tree)
	src.step = time.Second
	cfg := WalkConfig{Budget: 3 * time.Second, Now: src.now}
	res, err := Walk[*fakeNode](src, cfg)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(res.Elements) == 0 || len(res.Elements) == 6 {
		t.Fatalf("got %d elements, want a partial tree", len(res.Elements))
	}
	found := false
	for _, n := range res.Notes {
		if strings.Contains(n, "exceeded the 3s time limit") {
			found = true
		}
	}
	if !found {
		t.Errorf("Notes = %v, want the time-limit note", res.Notes)
	}
}

func TestWalkChunkTimeoutStopsWalk(t *testing.T) {
	tree := sampleTree()
	src := newFakeSource(tree)
	src.childrenErr = func(n *fakeNode) error {
		if n == tree.kids[0] { // menu bar: the second chunk
			return ErrChunkTimedOut
		}
		return nil
	}
	res, err := Walk[*fakeNode](src, fullConfig())
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	// The root and the menu bar (whose chunk timed out) render; the walk
	// stops there.
	if len(res.Elements) > 2 {
		t.Fatalf("walk must stop at the timed-out chunk, got %d elements", len(res.Elements))
	}
	found := false
	for _, n := range res.Notes {
		if strings.Contains(n, "time limit") {
			found = true
		}
	}
	if !found {
		t.Errorf("Notes = %v, want the time-limit note", res.Notes)
	}
}

func TestWalkChildrenErrorBecomesLeafWithNote(t *testing.T) {
	tree := sampleTree()
	src := newFakeSource(tree)
	src.childrenErr = func(n *fakeNode) error {
		if n == tree.kids[1] {
			return errors.New("element gone")
		}
		return nil
	}
	res, err := Walk[*fakeNode](src, fullConfig())
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	// All six elements render; the edit has no children despite the error.
	if len(res.Elements) != 6 {
		t.Fatalf("got %d elements, want 6", len(res.Elements))
	}
	found := false
	for _, n := range res.Notes {
		if strings.Contains(n, "not every Element's children could be read") {
			found = true
		}
	}
	if !found {
		t.Errorf("Notes = %v, want the unreadable-children note", res.Notes)
	}
}

func TestWalkNearEmptyTreeNote(t *testing.T) {
	root := node(wstate.ControlTypeWindow, "W", node(wstate.ControlTypeCustom, "Canvas"))
	res, err := Walk[*fakeNode](newFakeSource(root), fullConfig())
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "nearly empty") {
		t.Errorf("Notes = %v, want the nearly-empty note", res.Notes)
	}
	// The note text is a shared contract: the retry policy (retry.go)
	// detects a sparse read by it, and recorded fixtures carry it.
	if res.Notes[0] != nearEmptyNote {
		t.Errorf("near-empty note = %q, want the exact nearEmptyNote constant", res.Notes[0])
	}
}

func TestWalkRootErrorPropagates(t *testing.T) {
	src := newFakeSource(nil)
	src.rootErr = ErrWindowGone
	res, err := Walk[*fakeNode](src, fullConfig())
	if !errors.Is(err, ErrWindowGone) {
		t.Fatalf("walk error = %v, want ErrWindowGone", err)
	}
	if len(res.Elements) != 0 {
		t.Errorf("no elements expected on a root failure, got %d", len(res.Elements))
	}
}

func TestWalkExpiredBudgetWalksOnlyRoot(t *testing.T) {
	// The clock advances 1s per chunk; a 500ms budget expires during the
	// root's own chunk, so the walk returns just the root with the note.
	src := newFakeSource(sampleTree())
	src.step = time.Second
	cfg := WalkConfig{Budget: 500 * time.Millisecond, Now: src.now}
	res, err := Walk[*fakeNode](src, cfg)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(res.Elements) != 1 || res.Elements[0].Role != "window" {
		t.Fatalf("an expired budget must still walk the root, got %d elements", len(res.Elements))
	}
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "time limit") {
		t.Errorf("Notes = %v, want the time-limit note", res.Notes)
	}
}
