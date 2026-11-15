package page

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/wstate"
)

var update = flag.Bool("update", false, "rewrite the golden Window State files")

// loadRaw reads a recorded (hand-authored) Page read.
func loadRaw(t *testing.T, name string) *Raw {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw Raw
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return &raw
}

// frameFor is the faked UI Automation frame of a Chrome Window showing
// the Page: chrome-frame.json with its title, tab and document named after
// the Page.
func frameFor(t *testing.T, title string) *wstate.Tree {
	t.Helper()
	frame, err := wstate.LoadFixture(filepath.Join("testdata", "chrome-frame.json"))
	if err != nil {
		t.Fatal(err)
	}
	frame.Title = strings.ReplaceAll(frame.Title, "TITLE", title)
	for i := range frame.Elements {
		frame.Elements[i].Name = strings.ReplaceAll(frame.Elements[i].Name, "TITLE", title)
	}
	return frame
}

// TestGoldenWindowStates renders each recorded Page merged into the faked
// frame and compares it with its golden text (go test -update rewrites
// them).
func TestGoldenWindowStates(t *testing.T) {
	for _, name := range []string{"login-form", "clickable-div", "cross-origin-iframe", "password-field"} {
		t.Run(name, func(t *testing.T) {
			raw := loadRaw(t, name)
			m, err := Merge(frameFor(t, raw.Title), raw)
			if err != nil {
				t.Fatal(err)
			}
			got := wstate.Render(m.Tree)
			golden := filepath.Join("testdata", name+".txt")
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Errorf("Window State differs from %s:\n got:\n%s\nwant:\n%s", golden, got, want)
			}
			if len(m.Origins) != len(m.Tree.Elements)-(len(frameFor(t, raw.Title).Elements)-(m.End-m.Doc-1)) {
				t.Errorf("%d origins for the Page's Elements", len(m.Origins))
			}
		})
	}
}

// TestGoldensFromTheCutFrame: the frame as the UI Automation walk reads it
// for a browser Window, the document's own line without its descendants
// (uia.DocumentCut), merges into the same goldens, with the same indexes
// and origins.
func TestGoldensFromTheCutFrame(t *testing.T) {
	for _, name := range []string{"login-form", "clickable-div", "cross-origin-iframe", "password-field"} {
		raw := loadRaw(t, name)
		whole, err := Merge(frameFor(t, raw.Title), raw)
		if err != nil {
			t.Fatal(err)
		}
		cut := frameFor(t, raw.Title)
		cut.Elements = append(cut.Elements[:9:9], cut.Elements[12:]...)
		if !NamedAfter(cut.Elements[8].Name, raw.Title) {
			t.Fatalf("%s: the document is not named after the Tab", name)
		}
		m, err := Merge(cut, raw)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := wstate.Render(m.Tree), wstate.Render(whole.Tree); got != want {
			t.Errorf("%s: the cut frame merges differently:\n%s\nwant:\n%s", name, got, want)
		}
		if m.Doc != whole.Doc || m.End != m.Doc+1 || len(m.Origins) != len(whole.Origins) {
			t.Errorf("%s: doc %d end %d, %d origins; want doc %d, %d origins", name, m.Doc, m.End, len(m.Origins), whole.Doc, len(whole.Origins))
		}
		if !reflect.DeepEqual(m.Origins, whole.Origins) {
			t.Errorf("%s: origins differ:\n%+v\nwant:\n%+v", name, m.Origins, whole.Origins)
		}
	}
	if NamedAfter("", "") || NamedAfter("Sign in", "") || !NamedAfter("Sign​  in", "Sign in") {
		t.Error("NamedAfter: an empty title names nothing; spaces and zero-width characters do not count")
	}
}

func TestPasswordValuesNeverRender(t *testing.T) {
	for _, name := range []string{"login-form", "password-field"} {
		raw := loadRaw(t, name)
		m, err := Merge(frameFor(t, raw.Title), raw)
		if err != nil {
			t.Fatal(err)
		}
		out := wstate.Render(m.Tree)
		for _, secret := range []string{"hunter2", "correct horse", "battery staple"} {
			if strings.Contains(out, secret) {
				t.Errorf("%s: the password value %q was rendered:\n%s", name, secret, out)
			}
		}
	}
	// Without the cursor-pointer pass, password fields cannot be told
	// apart: no text field shows its value.
	raw := loadRaw(t, "password-field")
	raw.Main.Marks = nil
	res := Build(raw, testLimits())
	for _, e := range res.Elements {
		if e.Role == "edit" && e.Value != "" {
			t.Errorf("with the pass failed, edit %q shows value %q", e.Name, e.Value)
		}
	}
	// A value the browser masked is a password too, even unmarked.
	raw = loadRaw(t, "login-form")
	raw.Main.Marks = &Marks{}
	for i := range raw.Main.Nodes {
		if raw.Main.Nodes[i].NodeID == "17" {
			raw.Main.Nodes[i].Value = &AXValue{Type: "string", Value: json.RawMessage(`"\u2022\u2022\u2022\u2022"`)}
		}
	}
	for _, e := range Build(raw, testLimits()).Elements {
		if e.Name == "Password" && e.Role == "edit" && e.Value != "" {
			t.Errorf("a masked value rendered: %q", e.Value)
		}
	}
}

func testLimits() Limits {
	return Limits{MaxNodes: MaxNodes, MaxDepth: MaxDepth, MaxChildren: MaxChildren, NodeLabel: MaxNodes, DepthLabel: MaxDepth}
}

func TestOriginsRecordSessionFrameAndDocument(t *testing.T) {
	raw := loadRaw(t, "cross-origin-iframe")
	m, err := Merge(frameFor(t, raw.Title), raw)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Origin{}
	for i, o := range m.Origins {
		byName[m.Tree.Elements[m.Doc+1+i].Name] = o
		if o.DocIndex != m.Doc {
			t.Errorf("origin %d: DocIndex %d, want %d", i, o.DocIndex, m.Doc)
		}
	}
	if o := byName["Checkout"]; o.SessionID != "S3" || o.FrameID != "F1" || o.LoaderID != "L1" || o.BackendNodeID != 2 || o.Owner != nil {
		t.Errorf("main frame heading origin = %+v", o)
	}
	if o := byName["Shipping questions"]; o.SessionID != "S3" || o.FrameID != "F3" || o.LoaderID != "L3" || o.BackendNodeID != 312 {
		t.Errorf("same-origin iframe link origin = %+v", o)
	}
	o := byName["Pay now"]
	if o.SessionID != "CHILD1" || o.FrameID != "F2" || o.LoaderID != "L2" || o.BackendNodeID != 40 || o.TargetID != "T3" {
		t.Errorf("cross-origin iframe button origin = %+v", o)
	}
	if o.Owner == nil || o.Owner.SessionID != "S3" || o.Owner.BackendNodeID != 300 || o.Owner.LoaderID != "L1" {
		t.Errorf("cross-origin iframe owner = %+v", o.Owner)
	}
}

func TestMergeFocus(t *testing.T) {
	raw := loadRaw(t, "login-form")
	// The frame's focus in the document (or nowhere) gives way to the
	// Page's focused field.
	for _, f := range []int{-1, 8, 10} {
		frame := frameFor(t, raw.Title)
		frame.FocusedIndex = f
		m, err := Merge(frame, raw)
		if err != nil {
			t.Fatal(err)
		}
		if el := m.Tree.Elements[m.Tree.FocusedIndex]; el.Name != "Email" || el.Role != "edit" {
			t.Errorf("frame focus %d: focused %+v", f, el)
		}
	}
	// The frame's focus outside the document stays, shifted past the Page.
	frame := frameFor(t, raw.Title)
	frame.FocusedIndex = 13 // the infobar's Cancel button
	m, err := Merge(frame, raw)
	if err != nil {
		t.Fatal(err)
	}
	if el := m.Tree.Elements[m.Tree.FocusedIndex]; el.Name != "Cancel" {
		t.Errorf("frame focus after the document moved to %+v", el)
	}
	frame.FocusedIndex = 7 // the address bar, before the document
	m, _ = Merge(frame, raw)
	if m.Tree.FocusedIndex != 7 {
		t.Errorf("address bar focus moved to %d", m.Tree.FocusedIndex)
	}
	// A Page with only the document focused focuses the document Element.
	raw = loadRaw(t, "clickable-div")
	m, _ = Merge(frameFor(t, raw.Title), raw)
	if m.Tree.FocusedIndex != m.Doc {
		t.Errorf("root focus: focused %d, want the document %d", m.Tree.FocusedIndex, m.Doc)
	}
}

func TestMergeNeedsADocument(t *testing.T) {
	raw := loadRaw(t, "login-form")
	frame := frameFor(t, raw.Title)
	frame.Elements = frame.Elements[:8] // no document
	_, err := Merge(frame, raw)
	var u *Unavailable
	if !errors.As(err, &u) || u.Reason != "UI Automation shows no document for the active Tab" {
		t.Fatalf("err = %v", err)
	}
	// Two documents: the one named after the Tab wins.
	frame = frameFor(t, raw.Title)
	frame.Elements = append(frame.Elements, wstate.Element{Depth: 1, Role: "document", Name: "Side panel"})
	m, err := Merge(frame, raw)
	if err != nil || m.Doc != 8 {
		t.Fatalf("doc %v, err %v", m, err)
	}
	frame.Elements[8].Name = "Other"
	if _, err := Merge(frame, raw); !errors.As(err, &u) || !strings.Contains(u.Reason, "several documents") {
		t.Fatalf("err = %v", err)
	}
}

// TestCapsAndPartialNotes: the Page shares the frame's element cap, marks
// the cut Element like the walk does, and joins the partial-tree note.
func TestCapsAndPartialNotes(t *testing.T) {
	raw := &Raw{TargetID: "T", Title: "Long", Main: Doc{SessionID: "S", FrameID: "F", Marks: &Marks{}}}
	root := AXNode{NodeID: "r", Role: &AXValue{Value: json.RawMessage(`"RootWebArea"`)}, BackendDOMNodeID: 1}
	var nodes []AXNode
	for i := 0; i < 10; i++ {
		id := "b" + string(rune('0'+i))
		root.ChildIDs = append(root.ChildIDs, id)
		nodes = append(nodes, AXNode{NodeID: id, ParentID: "r", Role: &AXValue{Value: json.RawMessage(`"button"`)},
			Name: &AXValue{Value: json.RawMessage(`"B` + string(rune('0'+i)) + `"`)}, BackendDOMNodeID: int64(10 + i)})
	}
	raw.Main.Nodes = append([]AXNode{root}, nodes...)

	res := Build(raw, Limits{MaxNodes: 4, MaxDepth: 10, MaxChildren: 1000, NodeLabel: 3000, DepthLabel: 48})
	if len(res.Elements) != 4 || res.DocTruncated == nil || res.DocTruncated.Limit != "3000" || res.DocTruncated.OmittedChildren != 6 {
		t.Fatalf("node cap: %d Elements, doc truncation %+v", len(res.Elements), res.DocTruncated)
	}
	if len(res.Reasons) != 1 || res.Reasons[0] != "the element cap (3000) was reached" {
		t.Errorf("reasons = %q", res.Reasons)
	}
	res = Build(raw, Limits{MaxNodes: 100, MaxDepth: 10, MaxChildren: 3, NodeLabel: 3000, DepthLabel: 48})
	if len(res.Elements) != 3 || res.DocTruncated == nil || res.DocTruncated.Limit != "3" || res.DocTruncated.OmittedChildren != 7 || len(res.Reasons) != 0 {
		t.Fatalf("children cap: %d Elements, %+v, %q", len(res.Elements), res.DocTruncated, res.Reasons)
	}

	// The frame's partial note gains the Page's reason, once.
	notes := mergeNotes([]string{"this Window State is partial: the tree read exceeded the 8s time limit"}, []string{"the element cap (3000) was reached"})
	if len(notes) != 1 || notes[0] != "this Window State is partial: the tree read exceeded the 8s time limit; the element cap (3000) was reached" {
		t.Errorf("notes = %q", notes)
	}
	if notes := mergeNotes(nil, []string{"the depth cap (48) was reached"}); len(notes) != 1 || notes[0] != "this Window State is partial: the depth cap (48) was reached" {
		t.Errorf("notes = %q", notes)
	}

	// The depth cap: an Element at the cap is shown but not expanded.
	deep := &Raw{Title: "Deep", Main: Doc{Marks: &Marks{}, Nodes: []AXNode{
		{NodeID: "r", Role: &AXValue{Value: json.RawMessage(`"RootWebArea"`)}, ChildIDs: []string{"l"}},
		{NodeID: "l", ParentID: "r", Role: &AXValue{Value: json.RawMessage(`"list"`)}, ChildIDs: []string{"i"}},
		{NodeID: "i", ParentID: "l", Role: &AXValue{Value: json.RawMessage(`"listitem"`)}, ChildIDs: []string{"a"}},
		{NodeID: "a", ParentID: "i", Role: &AXValue{Value: json.RawMessage(`"link"`)}, Name: &AXValue{Value: json.RawMessage(`"x"`)}},
	}}}
	res = Build(deep, Limits{MaxNodes: 100, MaxDepth: 1, MaxChildren: 10, NodeLabel: 3000, DepthLabel: 48})
	if len(res.Elements) != 2 || len(res.Reasons) != 1 || res.Reasons[0] != "the depth cap (48) was reached" {
		t.Errorf("depth cap: %d Elements, %q", len(res.Elements), res.Reasons)
	}
}

// TestResultPartial: every cap makes the Page read partial (reliability
// council 03), the children cap with no reason too; a read within them is
// whole.
func TestResultPartial(t *testing.T) {
	raw := &Raw{Title: "Ten", Main: Doc{Marks: &Marks{}}}
	root := AXNode{NodeID: "r", Role: &AXValue{Value: json.RawMessage(`"RootWebArea"`)}}
	var nodes []AXNode
	for i := 0; i < 10; i++ {
		id := "b" + string(rune('0'+i))
		root.ChildIDs = append(root.ChildIDs, id)
		nodes = append(nodes, AXNode{NodeID: id, ParentID: "r", Role: &AXValue{Value: json.RawMessage(`"button"`)},
			Name: &AXValue{Value: json.RawMessage(`"B` + string(rune('0'+i)) + `"`)}, BackendDOMNodeID: int64(10 + i)})
	}
	nodes[0].ChildIDs = []string{"l"}
	nodes = append(nodes, AXNode{NodeID: "l", ParentID: "b0", Role: &AXValue{Value: json.RawMessage(`"link"`)},
		Name: &AXValue{Value: json.RawMessage(`"x"`)}, BackendDOMNodeID: 30})
	raw.Main.Nodes = append([]AXNode{root}, nodes...)
	for _, c := range []struct {
		name    string
		lim     Limits
		partial bool
	}{
		{"whole", Limits{MaxNodes: 100, MaxDepth: 10, MaxChildren: 100}, false},
		{"element cap", Limits{MaxNodes: 4, MaxDepth: 10, MaxChildren: 100}, true},
		{"children cap", Limits{MaxNodes: 100, MaxDepth: 10, MaxChildren: 3}, true},
		{"depth cap", Limits{MaxNodes: 100, MaxDepth: 0, MaxChildren: 100}, true},
	} {
		c.lim.NodeLabel, c.lim.DepthLabel = 3000, 48
		if res := Build(raw, c.lim); res.Partial() != c.partial {
			t.Errorf("%s: Partial %v, want %v (%d Elements, reasons %q)", c.name, res.Partial(), c.partial, len(res.Elements), res.Reasons)
		}
	}
	nested := Result{Elements: []wstate.Element{{Role: "list", Truncated: &wstate.Truncation{Limit: "1000", OmittedChildren: 5}}}}
	if !nested.Partial() {
		t.Error("a nested children-cap marker must make the read partial")
	}
}

// TestMergeKeepsTheFrameAroundTheDocument: everything outside the
// document's UIA descendants is the frame read, in place.
func TestMergeKeepsTheFrameAroundTheDocument(t *testing.T) {
	raw := loadRaw(t, "login-form")
	frame := frameFor(t, raw.Title)
	m, err := Merge(frame, raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.Doc != 8 || m.End != 12 {
		t.Fatalf("doc %d end %d, want 8 and 12", m.Doc, m.End)
	}
	n := len(m.Origins)
	for i := 0; i <= m.Doc; i++ {
		if m.Tree.Elements[i].Name != frame.Elements[i].Name {
			t.Errorf("frame Element %d changed", i)
		}
	}
	for i := m.End; i < len(frame.Elements); i++ {
		if got := m.Tree.Elements[m.Doc+1+n+(i-m.End)]; got.Name != frame.Elements[i].Name || got.Depth != frame.Elements[i].Depth {
			t.Errorf("frame Element %d after the document = %+v", i, got)
		}
	}
	if frame.Elements[9].Role != "group" {
		t.Error("Merge modified the frame it was given")
	}
}
