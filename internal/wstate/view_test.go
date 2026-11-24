package wstate

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// viewTree is a small Window State with the shapes the views fold: an
// unnamed pane, a shared ID, a deep subtree and a walk's cut mark.
func viewTree() *Tree {
	return &Tree{
		Title: "Views", App: "app.exe", FocusedIndex: 4,
		Elements: []Element{
			{Depth: 0, Role: "window", Name: "Views", SecondaryActions: []string{"Raise"}, AutomationId: "w"}, // 0
			{Depth: 1, Role: "pane"},                                                           // 1: bare, folds
			{Depth: 2, Role: "group", Name: "Toolbar"},                                         // 2
			{Depth: 3, Role: "button", Name: "Open", AutomationId: "b"},                        // 3: shared ID
			{Depth: 3, Role: "button", Name: "Save", AutomationId: "b"},                        // 4: shared ID, focused
			{Depth: 3, Role: "text", Name: "Ready", AutomationId: "msg"},                       // 5: not interactive
			{Depth: 1, Role: "pane", AutomationId: "side"},                                     // 6: unnamed, but its ID is unique
			{Depth: 2, Role: "custom", Truncated: &Truncation{Limit: "7", OmittedChildren: 3}}, // 7: cut mark
			{Depth: 2, Role: "edit", Settable: "string", Name: "Find"},                         // 8
		},
		Notes: []string{"a walk note"},
	}
}

// lineIndexes are the Element Indexes the tree part of out prints, in
// order, each with its line's text after the index.
func lineIndexes(t *testing.T, out string) map[int]string {
	t.Helper()
	got := map[int]string{}
	re := regexp.MustCompile(`^\t+(\d+) (.*)$`)
	for _, l := range strings.Split(out, "\n") {
		if m := re.FindStringSubmatch(l); m != nil {
			i, _ := strconv.Atoi(m[1])
			got[i] = m[2]
		}
	}
	return got
}

func TestRenderViewFullIsRender(t *testing.T) {
	tree := viewTree()
	if got := RenderView(tree, View{Full: true, Interactive: true, MaxChars: 10, Ref: "7"}); got != Render(tree) {
		t.Errorf("--full must print what Render prints, byte for byte:\n%s\nwant:\n%s", got, Render(tree))
	}
}

// TestRenderViewDropsSharedIDs: the zero view (the default, speed 13)
// prints every Element and leaves out only the IDs several Elements
// share, the focused Element's sentence included; a tree without one
// prints exactly what Render prints.
func TestRenderViewDropsSharedIDs(t *testing.T) {
	tree := viewTree()
	want := strings.NewReplacer(" ID: b\n", "\n", " ID: b.\n", ".\n").Replace(Render(tree))
	if got := RenderView(tree, View{Ref: "7"}); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	tree.Elements[4].AutomationId = "save"
	if got := RenderView(tree, View{Ref: "7"}); got != Render(tree) || !strings.Contains(got, "3 button Open ID: b\n") {
		t.Errorf("unique IDs always print:\n%s", got)
	}
}

// TestRenderViewInteractive: --interactive keeps the Elements an agent
// acts on, a disabled one with its state (reliability-council 11).
func TestRenderViewInteractive(t *testing.T) {
	tree := viewTree()
	tree.Elements[3].Disabled = true
	want := `Window: "Views", App: app.exe.
	0 window Views Secondary Actions: Raise ID: w
		3 button (disabled) Open
		4 button Save
		7 custom (truncated: 7, omitted 3 children)
		8 edit (settable, string) Find

The focused UI element is 4 button Save.
Note: a walk note
Note: --interactive left out 4 of the 9 Elements; their Element Indexes still work
`
	if got := RenderView(tree, View{Interactive: true, Ref: "7"}); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderViewCompact(t *testing.T) {
	want := `Window: "Views", App: app.exe.
	0 window Views Secondary Actions: Raise ID: w
		2 group Toolbar
			3 button Open
			4 button Save
			5 text Ready ID: msg
		6 pane ID: side
			7 custom (truncated: 7, omitted 3 children)
			8 edit (settable, string) Find

The focused UI element is 4 button Save.
Note: a walk note
Note: --compact left out 1 of the 9 Elements; their Element Indexes still work
`
	if got := RenderView(viewTree(), View{Compact: true, Ref: "7"}); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderViewDepth(t *testing.T) {
	out := RenderView(viewTree(), View{Depth: 1, Ref: "7"})
	got := lineIndexes(t, out)
	if len(got) != 3 || got[1] != "pane (+1 children)" || got[6] != "pane ID: side (+2 children)" {
		t.Errorf("--depth 1 prints the root and its two panes with their child counts:\n%s", out)
	}
	if !strings.Contains(out, "Note: --depth 1 left out 6 of the 9 Elements; their Element Indexes still work; read a part whole with winuse get-window-state 7 --root <element>\n") {
		t.Errorf("no note on what --depth left out:\n%s", out)
	}
}

// TestRenderViewKeepsIndexes is the views' promise on real recorded
// trees: every line a view prints is the full read's line for that
// Element Index (the same Element, at most less said about it).
func TestRenderViewKeepsIndexes(t *testing.T) {
	for _, name := range []string{"fixture-app", "word-style"} {
		tree, err := LoadFixture(filepath.Join("testdata", name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		whole := lineIndexes(t, Render(tree))
		for _, v := range []View{{}, {Interactive: true}, {Compact: true}, {Depth: 2}, {Interactive: true, Compact: true}, {MaxChars: 900}} {
			for i, line := range lineIndexes(t, RenderView(tree, v)) {
				full := whole[i]
				// Shared IDs are left out of every view.
				full = regexp.MustCompile(` ID: \S+`).ReplaceAllString(full, "")
				line = regexp.MustCompile(` ID: \S+`).ReplaceAllString(line, "")
				full = strings.TrimSuffix(full, " ")
				if line != full && !strings.HasPrefix(line, full+" (+") {
					t.Errorf("%s %+v: Element %d prints %q, the full read %q", name, v, i, line, whole[i])
				}
			}
		}
	}
}

func TestRenderViewMaxChars(t *testing.T) {
	tree, err := LoadFixture(filepath.Join("testdata", "fixture-app.json"))
	if err != nil {
		t.Fatal(err)
	}
	full := RenderView(tree, View{Ref: "7"}) // the default view, uncut
	for _, max := range []int{900, 1200, 1600} {
		out := RenderView(tree, View{MaxChars: max, Ref: "7"})
		if n := utf8.RuneCountInString(out); n > max {
			t.Errorf("--max-chars %d printed %d characters", max, n)
		}
		if !strings.Contains(out, "Note: --max-chars "+strconv.Itoa(max)+" stopped the output after Element ") ||
			!strings.Contains(out, "winuse get-window-state 7 --root <element>") {
			t.Errorf("--max-chars %d: no note naming what was cut and how to see it:\n%s", max, out)
		}
		if !strings.HasPrefix(full, strings.SplitN(out, "\n\n", 2)[0]) {
			t.Errorf("--max-chars %d: the printed lines must be the full read's first lines:\n%s", max, out)
		}
		for _, n := range tree.Notes {
			if !strings.Contains(out, "Note: "+n+"\n") {
				t.Errorf("--max-chars %d cut the walk's note %q", max, n)
			}
		}
	}
	if out := RenderView(tree, View{MaxChars: len(full) + 1000, Ref: "7"}); out != full {
		t.Errorf("a cap the read fits in must print it whole:\n%s", out)
	}
}

// A read that does not fit --max-chars folds its bare containers first,
// as --compact does, and says so; one that fits prints as before
// (reliability-council 12).
func TestRenderViewFoldsBeforeTheCut(t *testing.T) {
	tree := viewTree()
	for i := 0; i < 40; i++ { // unnamed wrappers around one more named button each
		tree.Elements = append(tree.Elements, Element{Depth: 1, Role: "group"}, Element{Depth: 2, Role: "group"},
			Element{Depth: 3, Role: "button", Name: "Item " + strconv.Itoa(i)})
	}
	whole := RenderView(tree, View{Ref: "7"})
	compact := RenderView(tree, View{Compact: true, Ref: "7"})
	cap := utf8.RuneCountInString(compact) + 500 // the cut keeps room for its own note
	if cap >= utf8.RuneCountInString(whole) {
		t.Fatalf("the test tree must fit the cap only when folded: whole %d, compact %d", utf8.RuneCountInString(whole), utf8.RuneCountInString(compact))
	}
	out := RenderView(tree, View{MaxChars: cap, Ref: "7"})
	if strings.Contains(out, "stopped the output") {
		t.Errorf("folding fits the read, so nothing is cut:\n%s", out)
	}
	got := lineIndexes(t, out)
	if _, ok := got[1]; ok {
		t.Errorf("the bare pane 1 still prints:\n%s", out)
	}
	if _, ok := got[len(tree.Elements)-1]; !ok {
		t.Errorf("the last button is missing:\n%s", out)
	}
	if !strings.Contains(out, "Note: folding unnamed containers to fit --max-chars "+strconv.Itoa(cap)+" (as --compact does) left out 81 of the 129 Elements; their Element Indexes still work\n") {
		t.Errorf("no note on the fold:\n%s", out)
	}
	// Still too long folded: folded, then cut.
	out = RenderView(tree, View{MaxChars: 900, Ref: "7"})
	if !strings.Contains(out, "folding unnamed containers") || !strings.Contains(out, "Note: --max-chars 900 stopped the output") {
		t.Errorf("a read too long even folded folds, then cuts:\n%s", out)
	}
	// A read that fits, and --out (no cap), print every Element as before.
	if out := RenderView(tree, View{MaxChars: utf8.RuneCountInString(whole) + 500, Ref: "7"}); out != whole {
		t.Errorf("a read that fits must print as before:\n%s", out)
	}
	if out := RenderView(tree, View{Ref: "7"}); strings.Contains(out, "folding") {
		t.Errorf("no cap, no fold:\n%s", out)
	}
}

func TestCutTinyCapKeepsHeaderAndTail(t *testing.T) {
	lines := []PrintedLine{{Index: 0, Text: "\t0 window W\n", Role: "window"}, {Index: 1, Text: "\t\t1 button B\n", Role: "button"}}
	out := Cut("Window: \"W\", App: a.exe.\n", lines, "Note: tail\n", 10, "Narrow it")
	want := "Window: \"W\", App: a.exe.\n\nNote: tail\nNote: --max-chars 10 stopped the output after the header: 2 more Element lines (from Element 0 on: 1 button, 1 window) are not printed; their Element Indexes still work. Narrow it\n"
	if out != want {
		t.Errorf("got:\n%q\nwant:\n%q", out, want)
	}
}

// TestCutNoteByRole (bladebro-ideas 08): the cut note says what the lines
// it left out are, by role, the most first and then by name, so a page
// gives the same text every time; past five roles the rest are "other".
// A heading's level is its state, not its role.
func TestCutNoteByRole(t *testing.T) {
	line := func(i int, role string) PrintedLine {
		return PrintedLine{Index: i, Text: "\t" + strconv.Itoa(i) + " " + role + " x\n", Role: role}
	}
	head := "Window: \"W\", App: a.exe.\n"
	lines := []PrintedLine{line(0, "window"), line(1, "link"), line(2, "button"), line(3, "link"), line(4, "edit"), line(5, "button"), line(6, "link"), line(7, "heading")}
	if out := Cut(head, lines, "", 10, "Narrow it"); !strings.Contains(out, " 8 more Element lines (from Element 0 on: 3 link, 2 button, 1 edit, 1 heading, 1 window) are not printed;") {
		t.Errorf("five roles, all named:\n%s", out)
	}
	lines = append(lines, line(8, "text"), line(9, "image"), line(10, "link"))
	if out := Cut(head, lines, "", 10, "Narrow it"); !strings.Contains(out, " 11 more Element lines (from Element 0 on: 4 link, 2 button, 1 edit, 1 heading, 3 other) are not printed;") {
		t.Errorf("seven roles: the four first, then the rest as other:\n%s", out)
	}
	// Only the lines left out count, and the note still fits the cap.
	for _, max := range []int{400, 450, 500} {
		out := Cut(head, lines, "", max, "Narrow it")
		if n := utf8.RuneCountInString(out); n > max {
			t.Errorf("--max-chars %d printed %d characters:\n%s", max, n, out)
		}
	}
	tree := &Tree{Title: "W", App: "a.exe", FocusedIndex: -1, Elements: []Element{
		{Depth: 0, Role: "window", Name: "W"}, {Depth: 1, Role: "heading", Name: "Title", Level: 1}, {Depth: 1, Role: "heading", Name: "Part", Level: 2}}}
	if out := RenderView(tree, View{MaxChars: 1, Ref: "7"}); !strings.Contains(out, "(from Element 0 on: 2 heading, 1 window)") {
		t.Errorf("a heading counts by its role:\n%s", out)
	}
}

// newsTree is a Page's Window State with its chrome around the story: a
// banner holding a navigation of 30 links, the main content, a small
// complementary, and a footer of 30 links.
//
//	0 document; 1 banner, 2 link Home, 3 navigation, 4-33 links;
//	34 main, 35 heading, 36-55 text, 56 button Share;
//	57 complementary, 58-59 links; 60 footer, 61-90 links
func newsTree() *Tree {
	els := []Element{
		{Depth: 0, Role: "document", Name: "News"},
		{Depth: 1, Role: "banner"},
		{Depth: 2, Role: "link", Name: "Home"},
		{Depth: 2, Role: "navigation", Name: "Sections"},
	}
	for i := 0; i < 30; i++ {
		els = append(els, Element{Depth: 3, Role: "link", Name: "Section number " + strconv.Itoa(i) + " of the site"})
	}
	els = append(els, Element{Depth: 1, Role: "main"}, Element{Depth: 2, Role: "heading", Name: "The story", Level: 1})
	for i := 0; i < 20; i++ {
		els = append(els, Element{Depth: 2, Role: "text", Name: "Paragraph " + strconv.Itoa(i) + " of the story, with enough words to cost room in the read."})
	}
	els = append(els, Element{Depth: 2, Role: "button", Name: "Share"},
		Element{Depth: 1, Role: "complementary"}, Element{Depth: 2, Role: "link", Name: "Related one"}, Element{Depth: 2, Role: "link", Name: "Related two"},
		Element{Depth: 1, Role: "footer"})
	for i := 0; i < 30; i++ {
		els = append(els, Element{Depth: 2, Role: "link", Name: "Footer link " + strconv.Itoa(i) + " about the site"})
	}
	return &Tree{Title: "News", App: "chrome.exe", FocusedIndex: -1, Elements: els}
}

// newsCap is a cap newsTree fits only with its chrome folded: the folded
// read and room for the cut's own note. It fails the test when folding
// unnamed containers alone would fit.
func newsCap(t *testing.T, tree *Tree) int {
	t.Helper()
	folded, _ := renderView(tree, View{Compact: true, landmarks: true, Ref: "7"}, SharedIDs(tree.Elements))
	cap := utf8.RuneCountInString(folded) + 700
	if out := RenderView(tree, View{Compact: true, MaxChars: cap, Ref: "7"}); !strings.Contains(out, "stopped the output") && !strings.Contains(out, "Elements folded") {
		t.Fatalf("the test tree must need its landmarks folded to fit %d:\n%s", cap, out)
	}
	return cap
}

// TestRenderViewFoldsLandmarksBeforeTheCut (bladebro-ideas 08): a Page
// read that does not fit even with its unnamed containers folded folds
// its chrome landmarks (navigation, banner, footer, complementary) of 3
// or more Elements into one line each, which names the landmark, counts
// what it holds and says how to read it; the content prints whole, and
// every index keeps working.
func TestRenderViewFoldsLandmarksBeforeTheCut(t *testing.T) {
	tree := newsTree()
	cap := newsCap(t, tree)
	out := RenderView(tree, View{MaxChars: cap, Ref: "7"})
	if strings.Contains(out, "stopped the output") {
		t.Errorf("folding the chrome fits the read, so nothing is cut:\n%s", out)
	}
	for _, want := range []string{
		"\t\t1 banner (+32 Elements folded; --root 1 shows them)\n",
		"\t\t60 footer (+30 Elements folded; --root 60 shows them)\n",
		"\t\t35 heading (level 1) The story\n",
		"\t\t56 button Share\n",
		"\t\t58 link Related one\n", // two links: too few to fold
		"Note: folding unnamed containers (as --compact does) and Page landmarks to fit --max-chars " + strconv.Itoa(cap) +
			" left out 64 of the 91 Elements; their Element Indexes still work; read a part whole with winuse get-window-state 7 --root <element>\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q:\n%s", want, out)
		}
	}
	got := lineIndexes(t, out)
	for _, i := range []int{2, 3, 4, 33, 61, 90} {
		if _, ok := got[i]; ok {
			t.Errorf("Element %d of a folded landmark still prints:\n%s", i, out)
		}
	}
	// A read that fits, or one with no cap, folds nothing.
	whole := RenderView(tree, View{Ref: "7"})
	if strings.Contains(whole, "folded") || !strings.Contains(whole, "\t\t1 banner\n") || !strings.Contains(whole, "\t\t\t3 navigation Sections\n") {
		t.Errorf("no cap, no fold:\n%s", whole)
	}
	if out := RenderView(tree, View{MaxChars: utf8.RuneCountInString(whole) + 500, Ref: "7"}); out != whole {
		t.Errorf("a read that fits must print whole:\n%s", out)
	}
	// A Tab target takes no --root: the line points at --out, which writes
	// the read whole.
	if out := RenderView(tree, View{MaxChars: cap}); !strings.Contains(out, "\t\t1 banner (+32 Elements folded; --out <file> writes them)\n") {
		t.Errorf("without a Window to name:\n%s", out)
	}
}

// TestRenderViewLandmarkFoldRules: only a Page with main content (a main
// or dialog landmark) folds, and never a landmark that holds the focus or
// an Element whose line changed since the Agent's earlier read.
func TestRenderViewLandmarkFoldRules(t *testing.T) {
	tree := newsTree()
	cap := newsCap(t, tree)
	read := func(tree *Tree, v View) string {
		if v.MaxChars == 0 {
			v.MaxChars = cap
		}
		v.Ref = "7"
		return RenderView(tree, v)
	}
	// Room for one landmark left open (about 1,200 characters of links),
	// so the other's fold line is not cut.
	roomy := cap + 1300
	const banner, footer = "\t\t1 banner (+32 Elements folded", "\t\t60 footer (+30 Elements folded"

	noMain := newsTree()
	noMain.Elements[34].Role = "group"
	if out := read(noMain, View{}); strings.Contains(out, "Elements folded") || !strings.Contains(out, "stopped the output") {
		t.Errorf("no main content: nothing folds, the read is cut:\n%s", out)
	}
	dialog := newsTree()
	dialog.Elements[34].Role = "dialog"
	if out := read(dialog, View{}); !strings.Contains(out, banner) || !strings.Contains(out, footer) {
		t.Errorf("a dialog is main content too:\n%s", out)
	}

	focused := newsTree()
	focused.FocusedIndex = 10 // a link in the navigation inside the banner
	out := read(focused, View{MaxChars: roomy})
	if strings.Contains(out, banner) || strings.Contains(out, "3 navigation Sections (+") || !strings.Contains(out, footer) {
		t.Errorf("the focus keeps its landmarks open:\n%s", out)
	}

	before := append([]Element(nil), tree.Elements...)
	before[70].Name = "Footer link that was"
	out = read(tree, View{Before: before, MaxChars: roomy})
	if !strings.Contains(out, banner) || strings.Contains(out, footer) {
		t.Errorf("a changed link keeps its footer open:\n%s", out)
	}
	if out := read(tree, View{Before: append([]Element(nil), tree.Elements...)}); !strings.Contains(out, banner) || !strings.Contains(out, footer) {
		t.Errorf("nothing changed, both fold:\n%s", out)
	}
	// An earlier read of another page: every line changed, so none counts.
	other := []Element{{Role: "document", Name: "Other page"}}
	for i := 0; i < 90; i++ {
		other = append(other, Element{Depth: 1, Role: "text", Name: "Other " + strconv.Itoa(i)})
	}
	if out := read(tree, View{Before: other}); !strings.Contains(out, banner) || !strings.Contains(out, footer) {
		t.Errorf("after another page, both fold:\n%s", out)
	}
}
