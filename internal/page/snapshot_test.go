package page

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/bridge"
	"github.com/hoangvu12/winuse/internal/bridge/bridgetest"
	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// marksOfPage runs marksOf over one test page, numbered from 1.
func marksOfPage(t *testing.T, doc *tn) *Marks {
	t.Helper()
	doc.number(1)
	b, _ := json.Marshal(snapshotOf([]string{"F1"}, doc))
	s, err := decodeSnapshot(b)
	if err != nil {
		t.Fatal(err)
	}
	d := s.doc("F1")
	if d == nil {
		t.Fatal("no document F1")
	}
	return marksOf(s, d)
}

var (
	pointer = map[string]string{"cursor": "pointer", "display": "block"}
	block   = map[string]string{"display": "block"}
)

// TestSnapshotMarksFollowTheClickablesRules: each rule of clickables.js,
// applied to a snapshot. The comment on each case is the rule.
func TestSnapshotMarksFollowTheClickablesRules(t *testing.T) {
	long := strings.Repeat("abcdefghij", 10)
	upper := "BUY NOW"
	shout := txt("Buy now")
	shout.text = &upper
	doc := page(
		// cursor:pointer with no interactive role: its innerText.
		el("DIV", []string{"id", "plain"}, pointer, txt("Open settings")),
		// The outermost pointer only: a child that inherits it is not
		// another clickable.
		el("DIV", []string{"id", "outer"}, pointer, el("DIV", []string{"id", "inner"}, pointer, txt("Inner"))),
		// onclick or a tabindex other than -1 counts without the pointer.
		el("SPAN", []string{"id", "onclick", "onclick", "go()"}, nil, txt("Close")),
		el("DIV", []string{"id", "tab0", "tabindex", "0"}, block, txt("Focusable")),
		el("DIV", []string{"id", "tabneg", "tabindex", "-1"}, block, txt("Not focusable")),
		// Hidden, not rendered, or no box: left out.
		el("DIV", []string{"id", "hidden"}, map[string]string{"cursor": "pointer", "visibility": "hidden"}, txt("Hidden")),
		el("DIV", []string{"id", "none"}, map[string]string{"cursor": "pointer", "display": "none"}, txt("None")),
		el("DIV", []string{"id", "zero"}, map[string]string{"cursor": "pointer", "box": "zero"}, txt("Zero")),
		// Inside a widget: part of it, not its own Element.
		el("BUTTON", nil, nil, el("SPAN", []string{"id", "inbutton"}, pointer, txt("Buy"))),
		el("A", []string{"href", "/x"}, nil, el("DIV", []string{"id", "inlink"}, pointer, txt("Link card"))),
		el("A", []string{"id", "anchor"}, nil, el("DIV", []string{"id", "inanchor"}, pointer, txt("Anchor card"))),
		el("DIV", []string{"role", "tab"}, nil, el("SPAN", []string{"id", "intab"}, pointer, txt("Tab text"))),
		// Native tags and widget roles have their own Elements.
		el("LABEL", []string{"id", "label"}, pointer, txt("Label")),
		el("DIV", []string{"id", "rolebutton", "role", " Button "}, pointer, txt("Role button")),
		el("DIV", []string{"id", "roleregion", "role", "region"}, pointer, txt("Region")),
		// An editable region is a field.
		el("DIV", []string{"contenteditable", ""}, block, el("DIV", []string{"id", "editable", "tabindex", "0"}, block, txt("Editable"))),
		el("DIV", []string{"contenteditable", "true"}, block, el("DIV", []string{"contenteditable", "false"}, block, el("DIV", []string{"id", "noneditable", "tabindex", "0"}, block, txt("Not editable")))),
		// innerText: a break around blocks and at <br>, none inside inline
		// runs, hidden and generated text left out, text-transform kept.
		el("DIV", []string{"id", "card"}, pointer, el("H3", nil, block, txt("Blue mug")), txt("12 EUR")),
		el("DIV", []string{"id", "inline"}, pointer, txt("Foo"), el("B", nil, nil, txt("bar"))),
		el("DIV", []string{"id", "br"}, pointer, txt("Line one"), el("BR", nil, nil), txt("Line two")),
		el("DIV", []string{"id", "mixed"}, pointer, el("SPAN", nil, nil, txt("Shown")), el("SPAN", nil, map[string]string{"visibility": "hidden"}, txt(" Secret"))),
		el("DIV", []string{"id", "pseudo"}, pointer, el("::before", nil, nil, txt("*")), txt("Star")),
		el("DIV", []string{"id", "shout"}, pointer, shout),
		el("DIV", []string{"id", "upper"}, map[string]string{"cursor": "pointer", "display": "block", "text-transform": "uppercase"}, txt("Go now")),
		el("DIV", []string{"id", "select"}, pointer, el("SELECT", nil, map[string]string{"display": "inline-block"},
			el("OPTION", nil, map[string]string{"box": "none"}, txt("One")),
			el("OPTGROUP", nil, map[string]string{"box": "none"}, el("OPTION", nil, map[string]string{"box": "none"}, txt("Two")))), txt(" pick")),
		// An SVG element has no innerText: its labels only.
		el("svg", nil, nil, el("g", []string{"id", "svg", "aria-label", "Chart"}, map[string]string{"cursor": "pointer"}, el("text", nil, nil, txt("Label")))),
		el("DIV", []string{"id", "spaces"}, pointer, txt("  a  b \n\t c  ")),
		// No text: aria-label, then title.
		el("DIV", []string{"id", "icon", "aria-label", "Search"}, pointer),
		el("DIV", []string{"id", "titled", "aria-label", "", "title", "Help"}, pointer),
		// 80 UTF-16 units of text at most.
		el("DIV", []string{"id", "long"}, pointer, txt(long)),
		// A shadow tree's text is not seen; its elements are not scanned.
		el("DIV", []string{"id", "host", "title", "Host"}, pointer,
			&tn{name: "#shadow", kids: []*tn{el("SPAN", []string{"id", "inshadow", "tabindex", "0"}, nil, txt("Shadow text")), el("INPUT", []string{"type", "PASSWORD", "id", "shadowpw"}, nil)}}),
		el("INPUT", []string{"type", "password", "id", "pw"}, nil),
	)
	m := marksOfPage(t, doc)
	want := []Clickable{
		{doc.find("plain").id, "Open settings"},
		{doc.find("outer").id, "Inner"},
		{doc.find("onclick").id, "Close"},
		{doc.find("tab0").id, "Focusable"},
		{doc.find("inanchor").id, "Anchor card"},
		{doc.find("roleregion").id, "Region"},
		{doc.find("noneditable").id, "Not editable"},
		{doc.find("card").id, "Blue mug 12 EUR"},
		{doc.find("inline").id, "Foobar"},
		{doc.find("br").id, "Line one Line two"},
		{doc.find("mixed").id, "Shown"},
		{doc.find("pseudo").id, "Star"},
		{doc.find("shout").id, "BUY NOW"},
		{doc.find("upper").id, "GO NOW"},
		{doc.find("select").id, "One Two pick"},
		{doc.find("svg").id, "Chart"},
		{doc.find("spaces").id, "a b c"},
		{doc.find("icon").id, "Search"},
		{doc.find("titled").id, "Help"},
		{doc.find("long").id, long[:80]},
		{doc.find("host").id, "Host"},
	}
	if !reflect.DeepEqual(m.Clickables, want) {
		t.Errorf("clickables:\n got %v\nwant %v", m.Clickables, want)
	}
	// Passwords: the light tree's (as clickables.js) and the shadow tree's,
	// any case of the type.
	if wantPw := []int64{doc.find("shadowpw").id, doc.find("pw").id}; !reflect.DeepEqual(m.Passwords, wantPw) {
		t.Errorf("passwords = %v, want %v", m.Passwords, wantPw)
	}
}

// TestSnapshotMarksDisplayContents: an element with no box (display:
// contents) has no style in the snapshot, so its child's pointer is
// compared with the nearest boxed ancestor's. clickables.js would read the
// contents element's own cursor; the two differ only when that element
// sets cursor:pointer itself, as here: the child is marked where the
// pass would not mark it.
func TestSnapshotMarksDisplayContents(t *testing.T) {
	doc := page(el("DIV", []string{"id", "contents"}, map[string]string{"cursor": "pointer", "display": "contents"},
		el("DIV", []string{"id", "child"}, block, txt("Inside"))))
	m := marksOfPage(t, doc)
	if want := []Clickable{{doc.find("child").id, "Inside"}}; !reflect.DeepEqual(m.Clickables, want) {
		t.Errorf("clickables = %v, want %v", m.Clickables, want)
	}
}

// TestSnapshotMarksCaps: clickables.js's caps, 200 found and 5000
// elements looked at.
func TestSnapshotMarksCaps(t *testing.T) {
	var many []*tn
	for i := 0; i < 250; i++ {
		many = append(many, el("DIV", nil, pointer, txt(fmt.Sprint(i))))
	}
	if m := marksOfPage(t, page(many...)); len(m.Clickables) != marksFoundCap || m.Clickables[199].Text != "199" {
		t.Errorf("%d clickables, the last %+v", len(m.Clickables), m.Clickables[len(m.Clickables)-1])
	}
	var filler []*tn
	for i := 0; i < marksScanCap-1; i++ {
		filler = append(filler, el("I", nil, nil))
	}
	in := page(append(filler, el("DIV", []string{"id", "last"}, pointer, txt("Last")), el("DIV", nil, pointer, txt("Past the cap")))...)
	m := marksOfPage(t, in)
	if want := []Clickable{{in.find("last").id, "Last"}}; !reflect.DeepEqual(m.Clickables, want) {
		t.Errorf("clickables = %v, want %v", m.Clickables, want)
	}
}

// TestSnapshotMarksNoBody: a document without a body (an SVG or XML
// document) has passwords at most, as with clickables.js.
func TestSnapshotMarksNoBody(t *testing.T) {
	doc := &tn{name: "#document", kids: []*tn{el("svg", nil, nil, el("rect", []string{"onclick", "x()"}, nil))}}
	if m := marksOfPage(t, doc); m == nil || len(m.Clickables) != 0 {
		t.Errorf("marks = %+v", m)
	}
}

// TestReadSnapshotMatchesThePass: on the benchmark page (200 role-less
// clickables, a password field, a cross-origin iframe) the snapshot read
// and the clickables.js read give the same marks and the same Elements.
func TestReadSnapshotMatchesThePass(t *testing.T) {
	_, snapRead := benchRead(t, 0, 200, false)
	_, jsRead := benchRead(t, 0, 200, true)
	a, b := snapRead(), jsRead()
	if !reflect.DeepEqual(a.Main.Marks, b.Main.Marks) {
		t.Errorf("main marks differ:\nsnapshot %+v\n    pass %+v", a.Main.Marks, b.Main.Marks)
	}
	if len(a.Main.Marks.Clickables) != 200 || len(a.Main.Marks.Passwords) != 1 {
		t.Errorf("%d clickables, %d passwords", len(a.Main.Marks.Clickables), len(a.Main.Marks.Passwords))
	}
	if len(a.Frames) != 1 || len(b.Frames) != 1 || !reflect.DeepEqual(a.Frames[0].Marks, b.Frames[0].Marks) {
		t.Errorf("frames differ: %+v / %+v", a.Frames, b.Frames)
	}
	lim := Limits{MaxNodes: 100000, MaxDepth: 48, MaxChildren: 1000, NodeLabel: 100000, DepthLabel: 48}
	ra, rb := Build(a, lim), Build(b, lim)
	if !reflect.DeepEqual(ra.Elements, rb.Elements) {
		t.Error("the Elements differ")
	}
	text := wstate.Render(&wstate.Tree{Elements: ra.Elements})
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(l, " clickable ") && !strings.HasPrefix(l, "The focused") {
			lines = append(lines, strings.TrimSpace(l))
		}
	}
	if len(lines) != 200 || lines[199] != "597 clickable Item 199" {
		t.Errorf("%d clickable lines, want 200: %q", len(lines), lines)
	}
}

// TestReadInOneBurst: a warm read sends the Tab's list, its window's
// bounds, then everything else at once: the dialog probe, and for the Tab
// and its iframe the frame tree, the accessibility tree and the snapshot.
// DOM and Accessibility are turned on by the first read of a session only.
func TestReadInOneBurst(t *testing.T) {
	w, read := benchRead(t, time.Millisecond, 200, false)
	for i := 0; i < 2; i++ {
		w.reset()
		read()
		waves, calls, methods := w.stats()
		if waves != 3 {
			t.Errorf("read %d: %d round trips one after another, want 3 (%v)", i, waves, methods)
		}
		if calls != 10 {
			t.Errorf("read %d: %d commands, want 10 (%v)", i, calls, methods)
		}
		for _, m := range []string{"DOM.enable", "Accessibility.enable", "DOMSnapshot.enable", "DOM.describeNode", "Runtime.getProperties"} {
			if methods[m] != 0 {
				t.Errorf("read %d sent %s %d times", i, m, methods[m])
			}
		}
		if methods["DOMSnapshot.captureSnapshot"] != 2 || methods["Runtime.evaluate"] != 1 {
			t.Errorf("read %d: %v", i, methods)
		}
	}
}

// TestReadFallsBackToThePass: a browser without DOMSnapshot (the
// extension forwards chrome.debugger's refusal as a failure naming it)
// gets the clickables.js marks, and its later reads no longer ask.
func TestReadFallsBackToThePass(t *testing.T) {
	p := newBenchPage(20, 5)
	serve := func(m cdptest.Message, emit func(string, string, any)) (any, error) {
		if m.Method == "DOMSnapshot.captureSnapshot" {
			return nil, fmt.Errorf(`{"code":-32601,"message":"'DOMSnapshot.captureSnapshot' wasn't found"}`)
		}
		return p.serve(m, emit)
	}
	w := newSlowWire(0, serve)
	c := cdp.NewClient(w)
	t.Cleanup(func() { c.Close() })
	conn := &browser.Connection{Browser: "chrome", Mode: browser.ModeLaunch, PID: 4242, Client: c}
	r := testReader()
	g := Geometry{Title: "Checkout - Google Chrome", DPI: 144, Rect: Box{Left: 150, Top: 75, Right: 1950, Bottom: 1275}}
	for i := 0; i < 2; i++ {
		w.reset()
		raw, err := r.Read(context.Background(), conn, g)
		if err != nil {
			t.Fatal(err)
		}
		if m := raw.Main.Marks; m == nil || len(m.Clickables) != 20 || len(m.Passwords) != 1 {
			t.Errorf("read %d: marks %+v", i, m)
		}
		_, _, methods := w.stats()
		if i == 1 && methods["DOMSnapshot.captureSnapshot"] != 0 {
			t.Errorf("a refused snapshot was asked for again: %v", methods)
		}
	}
	// A failure that is not a refusal (a navigation under way) is that
	// read's alone.
	r2 := testReader()
	r2.refuse(c, &cdp.Error{Method: "DOMSnapshot.captureSnapshot", Code: -32000, Message: "Document is not available"})
	if r2.refused(c) {
		t.Error("a passing failure was taken for a refusal")
	}
}

// TestReadOverTheExtension: the same read through the winuse extension's
// bridge. The snapshot goes through chrome.debugger (DOMSnapshot is among
// the domains chrome.debugger allows), and the Tab list and the windows'
// bounds are asked for together.
func TestReadOverTheExtension(t *testing.T) {
	p := newBenchPage(20, 5)
	for _, refuse := range []bool{false, true} {
		ext := bridgetest.Start(t, t.TempDir(), &bridgetest.Extension{
			Tabs:    []bridgetest.Tab{{TargetID: "session-T3", Title: "Checkout", URL: "https://shop.test/checkout", TabID: 11, WindowID: 1, Active: true}},
			Windows: []bridgetest.Window{{WindowID: 1, Left: 100, Top: 50, Width: 1200, Height: 800, State: "normal"}},
			Page: func(sessionID, method string, params json.RawMessage) (any, error) {
				if refuse && method == "DOMSnapshot.captureSnapshot" {
					return nil, fmt.Errorf(`{"code":-32601,"message":"'DOMSnapshot.captureSnapshot' wasn't found"}`)
				}
				return p.serve(cdptest.Message{SessionID: sessionID, Method: method, Params: params}, func(string, string, any) {})
			},
		})
		bc, err := bridge.Dial(context.Background(), ext.Info.Addr(), ext.Info.Token)
		if err != nil {
			t.Fatal(err)
		}
		c := cdp.NewClient(bc)
		t.Cleanup(func() { c.Close() })
		conn := &browser.Connection{Browser: "chrome", Mode: browser.ModeExtension, Client: c}
		g := Geometry{Title: "Checkout - Google Chrome", DPI: 144, Rect: Box{Left: 150, Top: 75, Right: 1950, Bottom: 1275}}
		raw, err := testReader().Read(context.Background(), conn, g)
		if err != nil {
			t.Fatalf("refuse=%v: %v", refuse, err)
		}
		if m := raw.Main.Marks; m == nil || len(m.Clickables) != 20 || len(m.Passwords) != 1 {
			t.Errorf("refuse=%v: marks %+v", refuse, m)
		}
		var snaps, passes int
		for _, cmd := range ext.Commands() {
			switch cmd.Method {
			case "DOMSnapshot.captureSnapshot":
				snaps++
			case "Runtime.getProperties":
				passes++
			}
		}
		if refuse && passes == 0 || !refuse && (snaps != 1 || passes != 0) {
			t.Errorf("refuse=%v: %d snapshots, %d passes", refuse, snaps, passes)
		}
	}
}

// TestReadSaysWhereTheMarksCameFrom: Doc.Snapshot is set for marks from
// the snapshot and not for the pass's, so the live comparison can tell a
// fallback from a match.
func TestReadSaysWhereTheMarksCameFrom(t *testing.T) {
	_, snapRead := benchRead(t, 0, 5, false)
	_, jsRead := benchRead(t, 0, 5, true)
	a, b := snapRead(), jsRead()
	if !a.Main.Snapshot || len(a.Frames) != 1 || !a.Frames[0].Snapshot {
		t.Errorf("snapshot read: main %v, frames %+v", a.Main.Snapshot, a.Frames)
	}
	if b.Main.Snapshot || len(b.Frames) != 1 || b.Frames[0].Snapshot {
		t.Errorf("pass read marked as snapshot")
	}
}

// TestReadLeftOutFrameFromTheSnapshot: a same-origin iframe the Tab's tree
// left out takes its marks from its document in the Tab's snapshot, with
// no isolated world and no script; the pass still uses the isolated world.
func TestReadLeftOutFrameFromTheSnapshot(t *testing.T) {
	main := page(el("DIV", []string{"id", "top"}, pointer, txt("Top")), el("IFRAME", []string{"id", "frame"}, block))
	main.number(1)
	inner := page(el("DIV", []string{"id", "pay"}, pointer, txt("Pay now")))
	inner.number(500)
	snap := snapshotOf([]string{"F1", "F3"}, main, inner)
	ax := func(doc *tn, frame string) []AXNode {
		var nodes []AXNode
		var walk func(n *tn, parent string)
		walk = func(n *tn, parent string) {
			node := AXNode{NodeID: fmt.Sprint(n.id), ParentID: parent, BackendDOMNodeID: n.id, Role: axRole("generic")}
			if n.name == "#document" {
				node.Role, node.FrameID = axRole("RootWebArea"), frame
			}
			if n.name == "#text" {
				node.Role, node.Name = axRole("StaticText"), axString(n.value)
			}
			at := len(nodes)
			nodes = append(nodes, node)
			for _, k := range n.kids {
				if k.name != "HEAD" {
					nodes[at].ChildIDs = append(nodes[at].ChildIDs, fmt.Sprint(k.id))
					walk(k, node.NodeID)
				}
			}
		}
		walk(doc, "")
		return nodes
	}
	p := &benchPage{}
	serve := func(m cdptest.Message, emit func(string, string, any)) (any, error) {
		switch m.Method {
		case "Target.getTargets":
			return map[string]any{"targetInfos": []cdptest.Target{{TargetID: "T3", Type: "page", Title: "Checkout", URL: "https://shop.test/checkout"}}}, nil
		case "Target.setAutoAttach":
			return nil, nil
		case "Page.getFrameTree":
			return map[string]any{"frameTree": map[string]any{
				"frame":       map[string]any{"id": "F1", "loaderId": "L1"},
				"childFrames": []any{map[string]any{"frame": map[string]any{"id": "F3", "loaderId": "L3"}}},
			}}, nil
		case "Accessibility.getFullAXTree":
			if strings.Contains(string(m.Params), "F3") {
				return map[string]any{"nodes": ax(inner, "F3")}, nil
			}
			return map[string]any{"nodes": ax(main, "F1")}, nil
		case "DOM.getFrameOwner":
			return map[string]any{"backendNodeId": main.find("frame").id}, nil
		case "DOMSnapshot.captureSnapshot":
			return snap, nil
		case "Page.createIsolatedWorld":
			return map[string]any{"executionContextId": 7}, nil
		case "Runtime.getProperties":
			return map[string]any{"result": []map[string]any{}}, nil
		}
		return p.serve(m, emit)
	}
	for _, js := range []bool{false, true} {
		w := newSlowWire(0, serve)
		c := cdp.NewClient(w)
		t.Cleanup(func() { c.Close() })
		conn := &browser.Connection{Browser: "chrome", Mode: browser.ModeLaunch, PID: 4242, Client: c}
		r := testReader()
		r.JSMarks = js
		raw, err := r.Read(context.Background(), conn, Geometry{Title: "Checkout - Google Chrome", DPI: 144, Rect: Box{Left: 150, Top: 75, Right: 1950, Bottom: 1275}})
		if err != nil {
			t.Fatal(err)
		}
		if len(raw.Frames) != 1 || raw.Frames[0].FrameID != "F3" || raw.Frames[0].Owner != main.find("frame").id || raw.Frames[0].Loaders["F3"] != "L3" {
			t.Fatalf("js=%v: frames %+v", js, raw.Frames)
		}
		_, _, methods := w.stats()
		f := raw.Frames[0]
		if js {
			if f.Snapshot || methods["Page.createIsolatedWorld"] != 1 {
				t.Errorf("pass: snapshot=%v, %v", f.Snapshot, methods)
			}
			continue
		}
		if want := []Clickable{{inner.find("pay").id, "Pay now"}}; !f.Snapshot || f.Marks == nil || !reflect.DeepEqual(f.Marks.Clickables, want) {
			t.Errorf("frame marks %+v (snapshot %v), want %v", f.Marks, f.Snapshot, want)
		}
		if want := []Clickable{{main.find("top").id, "Top"}}; !reflect.DeepEqual(raw.Main.Marks.Clickables, want) {
			t.Errorf("main marks %+v, want %v", raw.Main.Marks, want)
		}
		if methods["Page.createIsolatedWorld"] != 0 || methods["Runtime.evaluate"] != 1 || methods["DOMSnapshot.captureSnapshot"] != 1 {
			t.Errorf("snapshot read sent %v", methods)
		}
	}
}
