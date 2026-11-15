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
	lines := []PrintedLine{{0, "\t0 window W\n"}, {1, "\t\t1 button B\n"}}
	out := Cut("Window: \"W\", App: a.exe.\n", lines, "Note: tail\n", 10, "Narrow it")
	want := "Window: \"W\", App: a.exe.\n\nNote: tail\nNote: --max-chars 10 stopped the output after the header: 2 more Element lines (from Element 0 on) are not printed; their Element Indexes still work. Narrow it\n"
	if out != want {
		t.Errorf("got:\n%q\nwant:\n%q", out, want)
	}
}
