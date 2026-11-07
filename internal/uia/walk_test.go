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
	errKeepsKids  bool // a childrenErr comes with the node's children (the ones that did read)
	elapsed       time.Duration
	step          time.Duration
	base          time.Time
	childrenCalls int
	expanded      []*fakeNode       // every node whose children were read, in order
	onChildren    func(n *fakeNode) // runs at every Children call, before it answers
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
	s.expanded = append(s.expanded, n)
	if s.onChildren != nil {
		s.onChildren(n)
	}
	var err error
	if s.childrenErr != nil {
		if err = s.childrenErr(n); err != nil && !s.errKeepsKids {
			return nil, nil, err
		}
	}
	kids := make([]*fakeNode, len(n.kids))
	data := make([]NodeData, len(n.kids))
	for i, k := range n.kids {
		kids[i] = k
		data[i] = k.data
	}
	return kids, data, err
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

// A chunk that read some children and failed on others (reliability-council
// 05): the children that did read render and are expanded, and the read
// carries the partial note, never the nearly-empty one.
func TestWalkChildrenErrorKeepsTheChildrenThatRead(t *testing.T) {
	tree := node(wstate.ControlTypeWindow, "W",
		node(wstate.ControlTypePane, "Panel", node(wstate.ControlTypeButton, "OK")),
	)
	src := newFakeSource(tree)
	src.errKeepsKids = true
	src.childrenErr = func(n *fakeNode) error {
		if n == tree {
			return errors.New("reading cached child 1 of 2 failed")
		}
		return nil
	}
	res, err := Walk[*fakeNode](src, fullConfig())
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(res.Elements) != 3 || res.Elements[1].Name != "Panel" || res.Elements[2].Name != "OK" {
		t.Fatalf("Elements = %+v, want the window, the panel and its button", res.Elements)
	}
	if len(res.Notes) != 1 || !strings.HasPrefix(res.Notes[0], partialNotePrefix) ||
		!strings.Contains(res.Notes[0], "not every Element's children could be read") {
		t.Errorf("Notes = %v, want only the unreadable-children partial note", res.Notes)
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

// browserTree is a browser Window's frame: the address bar, the active
// Tab's document (index 4, its descendants 5 to 7) and the infobar after it.
func browserTree() *fakeNode {
	return node(wstate.ControlTypeWindow, "Page - Google Chrome",
		node(wstate.ControlTypePane, "Google Chrome",
			node(wstate.ControlTypeToolBar, "Address", node(wstate.ControlTypeButton, "Back")),
			node(wstate.ControlTypeDocument, "Page",
				node(wstate.ControlTypeGroup, "", node(wstate.ControlTypeText, "read through UI Automation")),
				node(wstate.ControlTypeButton, "UI Automation button"),
			),
			node(wstate.ControlTypeGroup, "infobar", node(wstate.ControlTypeButton, "Cancel")),
		),
	)
}

// cutNamed is a DocumentCut whose answer is known: the Document named title.
func cutNamed(title string) DocumentCut {
	return func(name, _ string) (bool, bool) { return title != "" && name == title, true }
}

// lateCut is a DocumentCut for the Document named title whose answer is
// known only once *known is set.
func lateCut(title string, known *bool) DocumentCut {
	return func(name, _ string) (bool, bool) { return *known && name == title, *known }
}

func walkNames(res WalkResult[*fakeNode]) string {
	var out []string
	for _, e := range res.Elements {
		out = append(out, e.Name)
	}
	return strings.Join(out, "|")
}

// descendantsOf lists n's descendants, not n itself.
func descendantsOf(n *fakeNode) []*fakeNode {
	var out []*fakeNode
	for _, k := range n.kids {
		out = append(out, k)
		out = append(out, descendantsOf(k)...)
	}
	return out
}

// cutFrame is what every cut walk of browserTree reads: the frame with the
// document's own line and nothing under it.
const cutFrame = "Page - Google Chrome|Google Chrome|Address|Back|Page|infobar|Cancel"

func TestWalkCutNeverReadsTheDocumentsDescendants(t *testing.T) {
	tree := browserTree()
	doc := tree.kids[0].kids[1]
	src := newFakeSource(tree)
	cfg := fullConfig()
	cfg.Cut = cutNamed("Page")
	res, err := Walk[*fakeNode](src, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := walkNames(res); got != cutFrame {
		t.Errorf("elements = %q, want %q", got, cutFrame)
	}
	if res.Cut != 4 || res.Elements[4].Role != "document" {
		t.Errorf("Cut = %d", res.Cut)
	}
	// Not one chunk was read for the document or under it.
	inside := append([]*fakeNode{doc}, descendantsOf(doc)...)
	for _, n := range src.expanded {
		for _, d := range inside {
			if n == d {
				t.Errorf("the walk read the children of %q", n.data.Name)
			}
		}
	}
	if len(src.expanded) != 6 {
		t.Errorf("%d chunks, want 6 (every frame Element but the document)", len(src.expanded))
	}
	if len(res.Live) != len(res.Elements) || len(res.Dropped) != 0 || len(res.Notes) != 0 || res.FocusedIndex != -1 {
		t.Errorf("live %d, dropped %d, notes %q, focus %d", len(res.Live), len(res.Dropped), res.Notes, res.FocusedIndex)
	}
}

// TestWalkLateCutDropsWhatWasReadUnderTheDocument: the answer comes while
// the walk is inside the document. What it read there is dropped (handles
// for the caller to release), its notes and its focus with it, and the
// walk goes on after the document.
func TestWalkLateCutDropsWhatWasReadUnderTheDocument(t *testing.T) {
	tree := browserTree()
	doc := tree.kids[0].kids[1]
	broken, group := node(wstate.ControlTypeGroup, "broken", node(wstate.ControlTypeText, "x")), doc.kids[0]
	doc.kids = append([]*fakeNode{broken}, doc.kids...)
	group.kids[0].data.Focused = true
	known := false
	src := newFakeSource(tree)
	src.childrenErr = func(n *fakeNode) error {
		if n == broken {
			return errors.New("element gone") // an unreadable reason under the document
		}
		return nil
	}
	src.onChildren = func(n *fakeNode) {
		if n == group {
			known = true // the Page read ends while the group is read
		}
	}
	cfg := fullConfig()
	cfg.Cut = lateCut("Page", &known)
	res, err := Walk[*fakeNode](src, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := walkNames(res); got != cutFrame {
		t.Errorf("elements = %q, want %q", got, cutFrame)
	}
	if res.Cut != 4 {
		t.Errorf("Cut = %d", res.Cut)
	}
	// broken, group and its text were read, then dropped; the document's
	// last child was never read and is dropped too.
	if len(res.Dropped) != 4 || len(res.Live) != len(res.Elements) {
		t.Errorf("dropped %d, live %d", len(res.Dropped), len(res.Live))
	}
	if len(res.Notes) != 0 {
		t.Errorf("notes from under the cut document stayed: %q", res.Notes)
	}
	if res.FocusedIndex != 4 {
		t.Errorf("the focus under the document moves to it: %d", res.FocusedIndex)
	}
	for _, n := range src.expanded {
		if n == group.kids[0] || n == doc.kids[2] {
			t.Errorf("read on under the document after the answer: %q", n.data.Name)
		}
	}
}

// TestWalkCutPicksTheFirstDocumentNamedAfterTheTab: the cut is page.Merge's
// pick. A document named otherwise (a side panel) is read whole, a nested
// document goes with its cut parent, and a later document with the same
// name stays whole.
func TestWalkCutPicksTheFirstDocumentNamedAfterTheTab(t *testing.T) {
	tree := browserTree()
	pane := tree.kids[0]
	doc := pane.kids[1]
	doc.kids = append(doc.kids, node(wstate.ControlTypeDocument, "Page", node(wstate.ControlTypeText, "iframe")))
	side := node(wstate.ControlTypeDocument, "Reading list", node(wstate.ControlTypeText, "saved"))
	again := node(wstate.ControlTypeDocument, "Page", node(wstate.ControlTypeText, "again"))
	pane.kids = []*fakeNode{pane.kids[0], side, doc, pane.kids[2], again}
	cfg := fullConfig()
	cfg.Cut = cutNamed("Page")
	res, err := Walk[*fakeNode](newFakeSource(tree), cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := "Page - Google Chrome|Google Chrome|Address|Back|Reading list|saved|Page|infobar|Cancel|Page|again"
	if got := walkNames(res); got != want {
		t.Errorf("elements = %q, want %q", got, want)
	}
	if res.Cut != 6 {
		t.Errorf("Cut = %d, want 6", res.Cut)
	}
}

// TestWalkCutSeesTheDocumentsValue: the cut is asked with each document's
// Value (a browser's document URL), so a stale Tab's document named like
// the Tab is passed over and read whole, and the next one is cut. A
// document with no Value pattern is asked with "".
func TestWalkCutSeesTheDocumentsValue(t *testing.T) {
	tree := browserTree()
	pane := tree.kids[0]
	doc := pane.kids[1]
	doc.data.HasValue, doc.data.ValueValue = true, "https://example.org/"
	stale := node(wstate.ControlTypeDocument, "Page", node(wstate.ControlTypeText, "old"))
	stale.data.HasValue, stale.data.ValueValue = true, "https://example.com/"
	empty := node(wstate.ControlTypeDocument, "")
	empty.data.ValueValue = "ignored without the pattern"
	pane.kids = []*fakeNode{stale, empty, pane.kids[0], doc, pane.kids[2]}
	var asked []string
	cfg := fullConfig()
	cfg.Cut = func(name, value string) (bool, bool) {
		asked = append(asked, name+"="+value)
		return name == "Page" && value == "https://example.org/", true
	}
	res, err := Walk[*fakeNode](newFakeSource(tree), cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := "Page - Google Chrome|Google Chrome|Page|old||Address|Back|Page|infobar|Cancel"
	if got := walkNames(res); got != want || res.Cut != 7 {
		t.Errorf("elements = %q, cut %d; want %q, cut 7", got, res.Cut, want)
	}
	if got := strings.Join(asked, ","); got != "Page=https://example.com/,=,Page=https://example.org/" {
		t.Errorf("asked %q", got)
	}
}

// TestWalkCutNamingNoDocumentIsTheWholeWalk: the answer never comes, names
// none of the documents (several documents, none named after the Tab), or
// comes after the document was read whole: the walk is today's, element
// for element.
func TestWalkCutNamingNoDocumentIsTheWholeWalk(t *testing.T) {
	whole, err := Walk[*fakeNode](newFakeSource(browserTree()), fullConfig())
	if err != nil {
		t.Fatal(err)
	}
	never := false
	for name, cut := range map[string]DocumentCut{
		"never known": lateCut("Page", &never),
		"no match":    cutNamed("Other"),
		"empty title": cutNamed(""),
	} {
		res, err := Walk[*fakeNode](newFakeSource(browserTree()), WalkConfig{Cut: cut})
		if err != nil {
			t.Fatal(err)
		}
		if walkNames(res) != walkNames(whole) || res.Cut != -1 || len(res.Dropped) != 0 || len(res.Notes) != 0 {
			t.Errorf("%s: %q, cut %d", name, walkNames(res), res.Cut)
		}
	}

	// Known once the infobar is read: the document is already whole, and
	// a later document with its name is not cut either.
	tree := browserTree()
	pane := tree.kids[0]
	pane.kids = append(pane.kids, node(wstate.ControlTypeDocument, "Page", node(wstate.ControlTypeText, "again")))
	known := false
	src := newFakeSource(tree)
	src.onChildren = func(n *fakeNode) {
		if n == pane.kids[2] {
			known = true
		}
	}
	res, err := Walk[*fakeNode](src, WalkConfig{Cut: lateCut("Page", &known)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Cut != -1 || len(res.Elements) != 12 {
		t.Errorf("late answer: %q, cut %d", walkNames(res), res.Cut)
	}
}

// TestWalkCutLeavesTheBudgetsToTheFrame: a dense document no longer eats
// the element cap or the time budget, and a cut tree is never called
// nearly empty.
func TestWalkCutLeavesTheBudgetsToTheFrame(t *testing.T) {
	dense := func() *fakeNode {
		tree := browserTree()
		doc := tree.kids[0].kids[1]
		for i := 0; i < 50; i++ {
			doc.kids = append(doc.kids, node(wstate.ControlTypeText, "dense"))
		}
		return tree
	}
	cfg := fullConfig()
	cfg.MaxNodes = 20
	full, _ := Walk[*fakeNode](newFakeSource(dense()), cfg)
	if len(full.Notes) != 1 || !strings.Contains(full.Notes[0], "the element cap (20) was reached") {
		t.Fatalf("the whole walk: %q", full.Notes)
	}
	cfg.Cut = cutNamed("Page")
	res, _ := Walk[*fakeNode](newFakeSource(dense()), cfg)
	if walkNames(res) != cutFrame || len(res.Notes) != 0 {
		t.Errorf("cut walk under the element cap: %q, %q", walkNames(res), res.Notes)
	}

	// One second per chunk on the fake clock, 9 s of budget: the whole walk
	// runs out inside the document; the cut walk does not.
	src := newFakeSource(dense())
	src.step = time.Second
	full, _ = Walk[*fakeNode](src, WalkConfig{Budget: 9 * time.Second, Now: src.now})
	if len(full.Notes) != 1 || !strings.Contains(full.Notes[0], "time limit") {
		t.Fatalf("the whole walk: %q", full.Notes)
	}
	src = newFakeSource(dense())
	src.step = time.Second
	res, _ = Walk[*fakeNode](src, WalkConfig{Budget: 9 * time.Second, Now: src.now, Cut: cutNamed("Page")})
	if walkNames(res) != cutFrame || len(res.Notes) != 0 {
		t.Errorf("cut walk under the time budget: %q, %q", walkNames(res), res.Notes)
	}

	sparse := node(wstate.ControlTypeWindow, "W", node(wstate.ControlTypeDocument, "Page", node(wstate.ControlTypeText, "t")))
	res, _ = Walk[*fakeNode](newFakeSource(sparse), WalkConfig{Cut: cutNamed("Page")})
	if len(res.Elements) != 2 || len(res.Notes) != 0 {
		t.Errorf("a cut tree is not nearly empty: %d elements, %q", len(res.Elements), res.Notes)
	}
}
