package page

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/wstate"
)

// hintPage is a shop page with the controls agents mix up: an icon button
// and a "Delete" in every row, an image button with a test id, a field
// with a generated id, a card's icon button, and one control that needs no
// hint.
func hintPage() *tn {
	inline := map[string]string{"display": "inline-block"}
	row := func(name string, icon *tn) *tn {
		return el("LI", nil, block,
			el("SPAN", nil, block, txt(name)),
			el("BUTTON", nil, inline, icon), txt(" "),
			el("BUTTON", nil, inline, txt("Delete")))
	}
	return page(
		el("UL", nil, block,
			row("Blue mug", el("svg", nil, nil, el("use", []string{"href", "/sprite.svg#trash"}, nil))),
			row("Red mug", el("I", []string{"class", "fa-solid fa-heart"}, nil)),
		),
		el("BUTTON", []string{"data-testid", "close-dialog"}, inline, el("IMG", []string{"src", "https://x.test/img/close-x.png?v=2"}, nil)),
		el("INPUT", []string{"id", "field-12345", "name", "email"}, inline),
		el("DIV", []string{"class", "product-card"}, block,
			el("SPAN", nil, nil, txt("Green mug")),
			el("BUTTON", []string{"id", ":r3:"}, inline, el("svg", []string{"data-icon", "cart"}, nil))),
		el("BUTTON", nil, inline, txt("Checkout")),
	)
}

// axOf is a getFullAXTree for the page's controls: each BUTTON a button
// named by its text, each INPUT a textbox, under one root.
func axOf(doc *tn) []AXNode {
	nodes := []AXNode{{NodeID: "root", Role: axRole("RootWebArea"), BackendDOMNodeID: doc.id}}
	var walk func(n *tn)
	walk = func(n *tn) {
		role := ""
		switch n.name {
		case "BUTTON":
			role = "button"
		case "INPUT":
			role = "textbox"
		}
		if role != "" {
			name := ""
			if len(n.kids) > 0 && n.kids[0].name == "#text" {
				name = n.kids[0].value
			}
			id := fmt.Sprint(n.id)
			nodes[0].ChildIDs = append(nodes[0].ChildIDs, id)
			nodes = append(nodes, AXNode{NodeID: id, ParentID: "root", Role: axRole(role), Name: axString(name), BackendDOMNodeID: n.id})
		}
		for _, k := range n.kids {
			walk(k)
		}
	}
	walk(doc)
	return nodes
}

// hintRead is the snapshot's marks of the hint page and its tree.
func hintRead(t *testing.T) (*tn, []AXNode, *Marks) {
	t.Helper()
	doc := hintPage()
	doc.number(1)
	b, _ := json.Marshal(snapshotOf([]string{"F1"}, doc))
	s, err := decodeSnapshot(b)
	if err != nil {
		t.Fatal(err)
	}
	nodes := axOf(doc)
	return doc, nodes, marksOf(s, s.doc("F1"), nodes)
}

// TestSnapshotHintsTellControlsApart: the snapshot gives each unnamed or
// duplicate control its icon, image, test id or field name, and its row's
// text without its own; a generated id is skipped; a unique name gets
// nothing.
func TestSnapshotHintsTellControlsApart(t *testing.T) {
	doc, _, m := hintRead(t)
	got := map[int64]NodeHint{}
	for _, h := range m.Hints {
		got[h.BackendNodeID] = h
	}
	var buttons []*tn
	var collect func(n *tn)
	collect = func(n *tn) {
		if n.name == "BUTTON" || n.name == "INPUT" {
			buttons = append(buttons, n)
		}
		for _, k := range n.kids {
			collect(k)
		}
	}
	collect(doc)
	want := []NodeHint{
		{Hint: "icon trash", Row: "Blue mug Delete"},
		{Row: "Blue mug"},
		{Hint: "icon fa-heart", Row: "Red mug Delete"},
		{Row: "Red mug"},
		{Hint: "img close-x.png, testid close-dialog"},
		{Hint: "name email"},
		{Hint: "icon cart", Row: "Green mug"},
		{}, // Checkout: unique, no hint
	}
	if len(buttons) != len(want) {
		t.Fatalf("%d controls", len(buttons))
	}
	for i, b := range buttons {
		w := want[i]
		if w != (NodeHint{}) {
			w.BackendNodeID = b.id
		}
		if g := got[b.id]; g != w {
			t.Errorf("control %d: %+v, want %+v", i, g, w)
		}
	}
	if len(m.Hints) != 7 {
		t.Errorf("%d hints, want 7", len(m.Hints))
	}
}

// TestBuildCarriesTheHintsToTheView: Build puts a hint on its Element, the
// default view prints the control's own part, --interactive its row too,
// and --full neither.
func TestBuildCarriesTheHintsToTheView(t *testing.T) {
	_, nodes, m := hintRead(t)
	raw := &Raw{TargetID: "T1", Main: Doc{SessionID: "S", FrameID: "F1", Nodes: nodes, Marks: m}}
	res := Build(raw, Limits{MaxNodes: 3000, MaxDepth: 48, MaxChildren: 1000, NodeLabel: 3000, DepthLabel: 48})
	tree := &wstate.Tree{Title: "Shop", App: "chrome.exe", Elements: res.Elements, FocusedIndex: -1}
	def := wstate.RenderView(tree, wstate.View{})
	for _, line := range []string{
		"\n\t0 button Hint: icon trash\n",
		"\n\t1 button Delete\n",
		"\n\t4 button Hint: img close-x.png, testid close-dialog\n",
		"\n\t5 edit (settable, string) Hint: name email\n",
		"\n\t7 button Checkout\n",
	} {
		if !strings.Contains(def, line) {
			t.Errorf("default view lacks %q:\n%s", line, def)
		}
	}
	in := wstate.RenderView(tree, wstate.View{Interactive: true})
	if !strings.Contains(in, "1 button Delete Hint: in Blue mug\n") || !strings.Contains(in, "6 button Hint: icon cart, in Green mug\n") {
		t.Errorf("--interactive:\n%s", in)
	}
	if full := wstate.RenderView(tree, wstate.View{Full: true}); strings.Contains(full, "Hint:") {
		t.Errorf("--full:\n%s", full)
	}
}

// TestHintPassCapsAndSkips: a generated value is no hint, an image's data:
// URL has no file name, and the pass stops at hintCap controls.
func TestHintPassCapsAndSkips(t *testing.T) {
	for _, c := range []struct {
		in   string
		want bool
	}{{"save-btn", false}, {"row-1234", true}, {":r1:", true}, {"h2o", false}} {
		if generated(c.in) != c.want {
			t.Errorf("generated(%q) = %v", c.in, !c.want)
		}
	}
	if fileName("data:image/png;base64,AAAA") != "" || fileName("/a/b/logo.svg") != "logo.svg" {
		t.Error("fileName")
	}
	var many []*tn
	for i := 0; i < hintCap+50; i++ {
		many = append(many, el("BUTTON", []string{"class", "icon-x"}, nil))
	}
	doc := page(many...)
	doc.number(1)
	b, _ := json.Marshal(snapshotOf([]string{"F1"}, doc))
	s, _ := decodeSnapshot(b)
	if m := marksOf(s, s.doc("F1"), axOf(doc)); len(m.Hints) != hintCap {
		t.Errorf("%d hints, want the cap %d", len(m.Hints), hintCap)
	}
	if !reflect.DeepEqual(hintTargets(nil, nil), map[int64]bool{}) {
		t.Error("no controls, no targets")
	}
}

// BenchmarkHintPass is the hint pass's own cost next to the marks pass it
// rides on: on the Page read benchmark's page (200 role-less clickables
// with their own text, one unnamed password field: one target), and on
// pages of rows that each hold an icon button and a "Delete" (every one a
// target, the worst case):
//
//	go test ./internal/page -run '^$' -bench HintPass -benchmem
func BenchmarkHintPass(b *testing.B) {
	p := newBenchPage(200, 5)
	raw, _ := json.Marshal(p.snapshot["session-T3"])
	s, _ := decodeSnapshot(raw)
	d := s.doc("F1")
	b.Run("marks/bench-page", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			newDocView(s, d).marks(s, d)
		}
	})
	b.Run("marks+hints/bench-page", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			marksOf(s, d, p.ax["session-T3"])
		}
	})
	for _, rows := range []int{50, 200} {
		var items []*tn
		for i := 0; i < rows; i++ {
			items = append(items, el("LI", nil, block,
				el("SPAN", nil, block, txt(fmt.Sprintf("Item %d", i))),
				el("BUTTON", nil, nil, el("svg", nil, nil, el("use", []string{"href", "#trash"}, nil))),
				el("BUTTON", nil, nil, txt("Delete"))))
		}
		doc := page(el("UL", nil, block, items...))
		doc.number(1)
		raw, _ := json.Marshal(snapshotOf([]string{"F1"}, doc))
		s, _ := decodeSnapshot(raw)
		d := s.doc("F1")
		nodes := axOf(doc)
		b.Run(fmt.Sprintf("marks/rows=%d", rows), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				newDocView(s, d).marks(s, d)
			}
		})
		b.Run(fmt.Sprintf("marks+hints/rows=%d", rows), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				marksOf(s, d, nodes)
			}
		})
	}
}
