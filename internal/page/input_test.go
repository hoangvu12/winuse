package page

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
	"github.com/hoangvu12/winuse/internal/keys"
)

// TestToScreenMatchesTheProbe replays probe-results.md section 3: the UIA
// document Element's top-left plus the CSS rect x devicePixelRatio lands
// within 1 px of UIA's own BoundingRectangle at every display scale and
// page zoom measured (probe-run-chrome-*.md).
func TestToScreenMatchesTheProbe(t *testing.T) {
	type row struct {
		name       string
		docL, docT int32
		dpr        float64
		css        [4]float64 // left, top, right, bottom
		uia        [4]int32
	}
	rows := []row{
		{"100% zoom 100 link", 26, 146, 1.5, [4]float64{24, 24, 144, 48}, [4]int32{62, 182, 242, 218}},
		{"150% zoom 100 corner", 26, 146, 1.5, [4]float64{360, 220, 440, 250}, [4]int32{566, 476, 686, 521}},
		{"150% zoom 125 button", 26, 146, 1.875, [4]float64{200, 60, 340, 96}, [4]int32{401, 258, 664, 326}},
		{"150% zoom 125 input", 26, 146, 1.875, [4]float64{40, 140, 260, 168}, [4]int32{101, 408, 514, 461}},
		{"175% zoom 100 corner", 30, 170, 1.75, [4]float64{360, 220, 440, 250}, [4]int32{660, 555, 800, 608}},
		{"175% zoom 125 link", 30, 170, 2.1875, [4]float64{24, 24, 144, 48}, [4]int32{82, 222, 345, 275}},
		{"175% zoom 150 button", 30, 170, 2.625, [4]float64{200, 60, 340, 96}, [4]int32{555, 327, 923, 422}},
		{"175% zoom 150 corner", 30, 170, 2.625, [4]float64{360, 220, 440, 250}, [4]int32{975, 747, 1185, 827}},
	}
	for _, r := range rows {
		rect := Rect{Left: r.css[0], Top: r.css[1], Width: r.css[2] - r.css[0], Height: r.css[3] - r.css[1], DPR: r.dpr}
		l, tp, rt, b := ToScreen(r.docL, r.docT, rect)
		got := [4]int32{l, tp, rt, b}
		for i := range got {
			if d := got[i] - r.uia[i]; d < -1 || d > 1 {
				t.Errorf("%s: screen %v, UIA %v", r.name, got, r.uia)
				break
			}
		}
		// The inverse transform brings the screen centre back to the CSS
		// centre, within half a physical pixel.
		cx, cy := PointToScreen(r.docL, r.docT, r.dpr, Point{X: (r.css[0] + r.css[2]) / 2, Y: (r.css[1] + r.css[3]) / 2})
		p := ToCSS(r.docL, r.docT, r.dpr, cx, cy)
		if math.Abs(p.X-(r.css[0]+r.css[2])/2) > 0.5/r.dpr+1e-9 || math.Abs(p.Y-(r.css[1]+r.css[3])/2) > 0.5/r.dpr+1e-9 {
			t.Errorf("%s: inverse of the centre = %+v", r.name, p)
		}
	}
	// The monitor scale alone would be off by hundreds of px once zoomed:
	// the transform must use the page's devicePixelRatio.
	l, _, _, _ := ToScreen(30, 170, Rect{Left: 360, Top: 220, Width: 80, Height: 30, DPR: 1.75})
	if l == 975 {
		t.Error("the monitor scale cannot reproduce the zoomed page")
	}
	if l, tp, _, _ := ToScreen(10, 20, Rect{Left: 5, Top: 5, Width: 1, Height: 1}); l != 15 || tp != 25 {
		t.Errorf("a missing DPR counts as 1: %d,%d", l, tp)
	}
}

func chord(t *testing.T, s string) keys.Chord {
	t.Helper()
	c, err := keys.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// brief is the part of a key event the mapping tests compare.
func brief(evs []map[string]any) []string {
	var out []string
	for _, e := range evs {
		s := e["type"].(string) + " " + e["key"].(string)
		if c, ok := e["code"]; ok {
			s += " " + c.(string)
		}
		if tx, ok := e["text"]; ok {
			s += " text=" + strings.ReplaceAll(tx.(string), "\r", `\r`)
		}
		if m := e["modifiers"].(int); m != 0 {
			s += " mods=" + strconv.Itoa(m)
		}
		if l, ok := e["location"]; ok {
			s += " loc=" + strconv.Itoa(l.(int))
		}
		out = append(out, s)
	}
	return out
}

func TestKeyEventsMapping(t *testing.T) {
	cases := map[string][]string{
		"Return":   {"keyDown Enter Enter text=\\r", "keyUp Enter Enter"},
		"KP_Enter": {"keyDown Enter NumpadEnter text=\\r loc=3", "keyUp Enter NumpadEnter loc=3"},
		"Tab":      {"rawKeyDown Tab Tab", "keyUp Tab Tab"},
		"space":    {"keyDown   Space text= ", "keyUp   Space"},
		"Escape":   {"rawKeyDown Escape Escape", "keyUp Escape Escape"},
		"a":        {"keyDown a KeyA text=a", "keyUp a KeyA"},
		"F5":       {"rawKeyDown F5 F5", "keyUp F5 F5"},
		"Prior":    {"rawKeyDown PageUp PageUp", "keyUp PageUp PageUp"},
		"KP_7":     {"keyDown 7 Numpad7 text=7 loc=3", "keyUp 7 Numpad7 loc=3"},
		"period":   {"keyDown . Period text=.", "keyUp . Period"},
		// Control holds: no text, so Control+a selects all.
		"Control_L+a": {
			"rawKeyDown Control ControlLeft mods=2 loc=1",
			"rawKeyDown a KeyA mods=2",
			"keyUp a KeyA mods=2",
			"keyUp Control ControlLeft loc=1",
		},
		// Shift picks the shifted character, as Codex's names ask.
		"Shift_L+a": {
			"rawKeyDown Shift ShiftLeft mods=8 loc=1",
			"keyDown A KeyA text=A mods=8",
			"keyUp A KeyA mods=8",
			"keyUp Shift ShiftLeft loc=1",
		},
		"Shift_L+1": {
			"rawKeyDown Shift ShiftLeft mods=8 loc=1",
			"keyDown ! Digit1 text=! mods=8",
			"keyUp ! Digit1 mods=8",
			"keyUp Shift ShiftLeft loc=1",
		},
		"Control_L+Shift_L+period": {
			"rawKeyDown Control ControlLeft mods=2 loc=1",
			"rawKeyDown Shift ShiftLeft mods=10 loc=1",
			"rawKeyDown > Period mods=10",
			"keyUp > Period mods=10",
			"keyUp Shift ShiftLeft mods=2 loc=1",
			"keyUp Control ControlLeft loc=1",
		},
		"Alt_R+F4": {
			"rawKeyDown Alt AltRight mods=1 loc=2",
			"rawKeyDown F4 F4 mods=1",
			"keyUp F4 F4 mods=1",
			"keyUp Alt AltRight loc=2",
		},
	}
	for name, want := range cases {
		got := brief(KeyEvents(chord(t, name)))
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s:\n got %q\nwant %q", name, got, want)
		}
	}
	// The virtual key travels with every event.
	for _, e := range KeyEvents(chord(t, "Return")) {
		if e["windowsVirtualKeyCode"] != 0x0D || e["nativeVirtualKeyCode"] != 0x0D {
			t.Errorf("vk = %v", e)
		}
	}
	// Every name keys.Parse accepts maps to a DOM key.
	for _, name := range []string{"BackSpace", "Delete", "Insert", "Home", "End", "Next", "Up", "Down", "Left", "Right", "Menu", "Caps_Lock", "Num_Lock", "KP_Multiply", "KP_Divide", "KP_Delete", "grave", "bracketleft", "apostrophe", "F20"} {
		for _, e := range KeyEvents(chord(t, name)) {
			if e["key"] == "" || e["code"] == nil {
				t.Errorf("%s maps to %v", name, e)
			}
		}
	}
}

func TestTextParts(t *testing.T) {
	got := TextParts("ab\ncd\r\ne\tf\r")
	want := []TextPart{{Text: "ab"}, {Key: "Return"}, {Text: "cd"}, {Key: "Return"}, {Text: "e"}, {Key: "Tab"}, {Text: "f"}, {Key: "Return"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parts = %+v", got)
	}
	if got := TextParts("héllo 👋"); len(got) != 1 || got[0].Text != "héllo 👋" {
		t.Errorf("one run = %+v", got)
	}
}

func TestClickEvents(t *testing.T) {
	evs := ClickEvents(Point{X: 10.5, Y: 20}, ButtonLeft, 2)
	var got []string
	for _, e := range evs {
		got = append(got, e["type"].(string)+" "+e["button"].(string))
		if e["x"] != 10.5 || e["y"] != 20.0 {
			t.Errorf("point %v", e)
		}
	}
	want := []string{"mouseMoved none", "mousePressed left", "mouseReleased left", "mousePressed left", "mouseReleased left"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("double click = %v", got)
	}
	if evs[3]["clickCount"] != 2 || evs[1]["clickCount"] != 1 || evs[1]["buttons"] != 1 || evs[2]["buttons"] != 0 {
		t.Errorf("counts and buttons: %v", evs)
	}
	right := ClickEvents(Point{}, ButtonRight, 1)
	if len(right) != 3 || right[1]["button"] != "right" || right[1]["buttons"] != 2 {
		t.Errorf("right click = %v", right)
	}
	if m := ClickEvents(Point{}, ButtonMiddle, 1); m[1]["buttons"] != 4 {
		t.Errorf("middle click = %v", m)
	}
}

func TestValueMatchesNeverShowsAPassword(t *testing.T) {
	if err := ValueMatches(Field{Kind: "text"}, "abc", "abc"); err != nil {
		t.Error(err)
	}
	if err := ValueMatches(Field{Kind: "editable"}, "a b", "a\u00a0b\n"); err != nil {
		t.Errorf("editable text normalizes: %v", err)
	}
	err := ValueMatches(Field{Kind: "text"}, "hello world", "hello")
	var v *VerifyError
	if !errors.As(err, &v) || !strings.Contains(err.Error(), `holds "hello" afterwards`) {
		t.Errorf("mismatch: %v", err)
	}
	err = ValueMatches(Field{Kind: "text", Password: true}, "hunter2secret", "hunter2")
	if !errors.As(err, &v) || strings.Contains(err.Error(), "hunter") || !strings.Contains(err.Error(), "7 characters afterwards, not 13") {
		t.Errorf("password mismatch: %v", err)
	}
}

// A text area stores the value's CRLF line breaks as LF (headless-max
// 24): the same text, not a mismatch. Other differences still are.
func TestValueMatchesIgnoresLineBreakForms(t *testing.T) {
	if err := ValueMatches(Field{Kind: "textarea"}, "one\r\ntwo", "one\ntwo"); err != nil {
		t.Errorf("CRLF read back as LF: %v", err)
	}
	if err := ValueMatches(Field{Kind: "editable"}, "one\rtwo", "one\ntwo\n"); err != nil {
		t.Errorf("editable CR read back as LF: %v", err)
	}
	if err := ValueMatches(Field{Kind: "textarea"}, "one\r\ntwo", "one two"); err == nil {
		t.Error("a missing line break matched")
	}
}

func TestFieldRefusal(t *testing.T) {
	if (Field{Kind: "text"}).Refusal() != nil {
		t.Error("a text field takes a value")
	}
	var na *NotActionableError
	if err := (Field{Kind: "text", Disabled: true}).Refusal(); !errors.As(err, &na) || na.Why != WhyDisabled {
		t.Errorf("disabled: %v", err)
	}
	var ns *NotSettableError
	if err := (Field{Kind: "text", ReadOnly: true}).Refusal(); !errors.As(err, &ns) {
		t.Errorf("read-only: %v", err)
	}
	if err := (Field{Kind: "", Type: "checkbox"}).Refusal(); !errors.As(err, &ns) {
		t.Errorf("checkbox: %v", err)
	}
}

// fakePage is a fake Tab for the input tests: session S (frame F1 at L1),
// a cross-origin iframe session C (its iframe element node 300 in S),
// configurable aim states per node, a recorded input log, and handlers a
// test can hold.
type fakePage struct {
	srv *cdptest.Server
	mu  sync.Mutex
	aim map[int64]map[string]any // aimJS answers per backend node
	// holdPress leaves mousePressed unanswered and opens a dialog instead.
	holdPress bool
	refuse    string // an Input method the browser refuses
	value     string // the field's value, set by insertText
	field     map[string]any
	quads     []float64
	inputs    []string
	scrolled  []int64
	// The click point's settle and re-centre (learn-from 02): moves logs
	// them per node ("settle 7", "recentre 7"), awaited counts settles sent
	// with awaitPromise, and recentred replaces a node's aim answer once
	// it was re-centred.
	moves     []string
	awaited   int
	recentred map[int64]map[string]any
	// Placeholder and remounting editors (bladebro-ideas 09): follow holds,
	// per node, the answers of its winuse:follow calls in order (a node id
	// the focus went to, "lost", or nil for none; nil once they run out);
	// fields and reads override a node's winuse:field and winuse:read
	// answers; labels are winuse:label's. selected and readFrom record the
	// nodes the select and the read-back ran on.
	follow   map[int64][]any
	fields   map[int64]map[string]any
	reads    map[int64]any
	labels   map[int64]map[string]any
	selected []int64
	readFrom []int64
}

func newFakePage(t *testing.T) (*fakePage, *cdp.Client) {
	t.Helper()
	f := &fakePage{srv: cdptest.NewServer(), aim: map[int64]map[string]any{}, field: map[string]any{"kind": "text"}}
	t.Cleanup(f.srv.Close)
	f.srv.Handle("Page.getFrameTree", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		return map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "F1", "loaderId": "L1"}}}, nil
	})
	f.srv.Handle("DOM.scrollIntoViewIfNeeded", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p struct {
			BackendNodeID int64 `json:"backendNodeId"`
		}
		json.Unmarshal(m.Params, &p)
		f.mu.Lock()
		f.scrolled = append(f.scrolled, p.BackendNodeID)
		f.mu.Unlock()
		return nil, nil
	})
	f.srv.Handle("DOM.resolveNode", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p struct {
			BackendNodeID int64 `json:"backendNodeId"`
		}
		json.Unmarshal(m.Params, &p)
		id, _ := json.Marshal(p.BackendNodeID)
		return map[string]any{"object": map[string]any{"type": "object", "objectId": m.SessionID + ":" + string(id)}}, nil
	})
	f.srv.Handle("DOM.getContentQuads", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return map[string]any{"quads": [][]float64{f.quads}}, nil
	})
	f.srv.Handle("Runtime.callFunctionOn", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p struct {
			ObjectID string `json:"objectId"`
			Fn       string `json:"functionDeclaration"`
			Args     []struct {
				Value any `json:"value"`
			} `json:"arguments"`
			Await bool `json:"awaitPromise"`
		}
		json.Unmarshal(m.Params, &p)
		var node int64
		json.Unmarshal([]byte(p.ObjectID[strings.Index(p.ObjectID, ":")+1:]), &node)
		f.mu.Lock()
		defer f.mu.Unlock()
		var v any
		switch {
		case strings.Contains(p.Fn, "winuse:follow"):
			var next any
			if q := f.follow[node]; len(q) > 0 {
				next, f.follow[node] = q[0], q[1:]
			}
			switch n := next.(type) {
			case int:
				return map[string]any{"result": map[string]any{"type": "object", "subtype": "node", "objectId": fmt.Sprintf("follow:%d", n)}}, nil
			case string:
				return map[string]any{"result": map[string]any{"type": "string", "value": n}}, nil
			}
			return map[string]any{"result": map[string]any{"type": "object", "subtype": "null", "value": nil}}, nil
		case strings.Contains(p.Fn, "winuse:label"):
			v = f.labels[node]
		case strings.Contains(p.Fn, "winuse:settle"):
			f.moves = append(f.moves, fmt.Sprintf("settle %d", node))
			if p.Await {
				f.awaited++
			}
			v = 1
		case strings.Contains(p.Fn, "winuse:recentre"):
			f.moves = append(f.moves, fmt.Sprintf("recentre %d", node))
			if st, ok := f.recentred[node]; ok {
				f.aim[node] = st
			}
			v = true
		case strings.Contains(p.Fn, "winuse:aim"):
			st := map[string]any{"state": "ok", "l": 10.0, "t": 20.0, "w": 50.0, "h": 10.0, "x": 35.0, "y": 25.0, "ox": 0.0, "oy": 0.0, "chain": true, "dpr": 1.5, "name": "Save"}
			for k, x := range f.aim[node] {
				st[k] = x
			}
			if len(p.Args) > 2 && p.Args[2].Value != nil {
				st["x"], st["y"] = p.Args[2].Value, p.Args[3].Value
			}
			v = st
		case strings.Contains(p.Fn, "paddingLeft") && !strings.Contains(p.Fn, "winuse:aim"):
			v = []float64{100, 200, 2}
		case strings.Contains(p.Fn, "winuse:field"):
			v = f.field
			if fd, ok := f.fields[node]; ok {
				v = fd
			}
		case strings.Contains(p.Fn, "winuse:select"):
			f.inputs = append(f.inputs, "select")
			f.selected = append(f.selected, node)
			v = true
		case strings.Contains(p.Fn, "winuse:read"):
			f.readFrom = append(f.readFrom, node)
			v = f.value
			if r, ok := f.reads[node]; ok {
				v = r
			}
		case strings.Contains(p.Fn, "isConnected"):
			v = true
		}
		return map[string]any{"result": map[string]any{"type": "object", "value": v}}, nil
	})
	f.srv.Handle("DOM.focus", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		f.mu.Lock()
		f.inputs = append(f.inputs, "focus")
		f.mu.Unlock()
		return nil, nil
	})
	// The node a winuse:follow answer names, by its object id.
	f.srv.Handle("DOM.describeNode", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p struct {
			ObjectID string `json:"objectId"`
		}
		json.Unmarshal(m.Params, &p)
		n, err := strconv.ParseInt(strings.TrimPrefix(p.ObjectID, "follow:"), 10, 64)
		if err != nil {
			return nil, &cdptest.ReplyError{Code: -32000, Message: "Could not find node with given id"}
		}
		return map[string]any{"node": map[string]any{"backendNodeId": n, "nodeName": "DIV"}}, nil
	})
	input := func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p map[string]any
		json.Unmarshal(m.Params, &p)
		f.mu.Lock()
		defer f.mu.Unlock()
		if m.Method == f.refuse {
			return nil, &cdptest.ReplyError{Code: -32000, Message: "refused"}
		}
		desc := m.SessionID + " " + m.Method
		if tp, ok := p["type"].(string); ok {
			desc += " " + tp
		}
		if m.Method == "Input.insertText" {
			desc += " " + p["text"].(string)
			f.value = p["text"].(string)
		}
		f.inputs = append(f.inputs, desc)
		if f.holdPress && p["type"] == "mousePressed" {
			c.Event(m.SessionID, "Page.javascriptDialogOpening", map[string]any{"type": "confirm", "message": "Leave\n  this page?"})
			return nil, cdptest.ErrNoReply
		}
		return nil, nil
	}
	f.srv.Handle("Input.dispatchMouseEvent", input)
	f.srv.Handle("Input.dispatchKeyEvent", input)
	f.srv.Handle("Input.insertText", input)
	ws, err := cdp.DialWS(context.Background(), f.srv.URL(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c := cdp.NewClient(ws)
	t.Cleanup(func() { c.Close() })
	return f, c
}

func (f *fakePage) log() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.inputs...)
}

func TestAimChecks(t *testing.T) {
	f, c := newFakePage(t)
	o := Origin{Client: c, SessionID: "S", FrameID: "F1", LoaderID: "L1", BackendNodeID: 7}
	all := AimOptions{Scroll: true, Enabled: true, Visible: true, HitTest: true}
	a, err := o.Aim(context.Background(), all)
	if err != nil {
		t.Fatal(err)
	}
	if a.Point != (Point{X: 35, Y: 25}) || a.Rect != (Rect{Left: 10, Top: 20, Width: 50, Height: 10, DPR: 1.5}) || a.Name != "Save" {
		t.Errorf("aim = %+v", a)
	}
	if len(f.scrolled) != 1 || f.scrolled[0] != 7 {
		t.Errorf("scrolled %v", f.scrolled)
	}
	for _, c := range []struct {
		state map[string]any
		why   string
		msg   string
	}{
		{map[string]any{"state": "disabled"}, WhyDisabled, "it is disabled"},
		{map[string]any{"state": "hidden"}, WhyHidden, "it is hidden or has no size"},
		{map[string]any{"state": "outside"}, WhyOutside, "outside the visible part of the Page"},
		{map[string]any{"state": "covered", "by": `<div class="cookie-banner">`}, WhyCovered, `it is covered by <div class="cookie-banner"> at its centre`},
	} {
		f.mu.Lock()
		f.aim[7] = c.state
		f.mu.Unlock()
		_, err := o.Aim(context.Background(), all)
		var na *NotActionableError
		if !errors.As(err, &na) || na.Why != c.why || !strings.Contains(err.Error(), c.msg) {
			t.Errorf("%s: %v", c.why, err)
		}
		// A check not asked for passes.
		if _, err := o.Aim(context.Background(), AimOptions{}); err != nil {
			t.Errorf("%s without checks: %v", c.why, err)
		}
	}
	f.mu.Lock()
	f.aim[7] = map[string]any{"state": "gone"}
	f.mu.Unlock()
	if _, err := o.Aim(context.Background(), all); !errors.Is(err, ErrStale) {
		t.Errorf("gone: %v", err)
	}
	if len(f.log()) != 0 {
		t.Errorf("aiming sent input: %v", f.log())
	}
}

// TestAimInFrames: a cross-origin iframe's node adds the iframe element's
// content offset and is hit-tested again there; a same-process frame the
// page cannot see into takes its offset from the content quads.
func TestAimInFrames(t *testing.T) {
	f, c := newFakePage(t)
	o := Origin{Client: c, SessionID: "C", FrameID: "F2", BackendNodeID: 40, Owner: &Owner{SessionID: "S", BackendNodeID: 300}}
	a, err := o.Aim(context.Background(), AimOptions{Visible: true, HitTest: true})
	if err != nil {
		t.Fatal(err)
	}
	if a.Point != (Point{X: 135, Y: 225}) || a.Rect.Left != 110 || a.Rect.Top != 220 || a.Rect.DPR != 1.5 {
		t.Errorf("iframe aim = %+v", a)
	}
	f.mu.Lock()
	f.aim[300] = map[string]any{"state": "covered", "by": "<div id=\"modal\">"}
	f.mu.Unlock()
	var na *NotActionableError
	if _, err := o.Aim(context.Background(), AimOptions{Visible: true, HitTest: true}); !errors.As(err, &na) || na.Blocker != `<div id="modal">` {
		t.Errorf("covered iframe: %v", err)
	}

	same := Origin{Client: c, SessionID: "S", FrameID: "F3", BackendNodeID: 41}
	f.mu.Lock()
	f.aim[41] = map[string]any{"chain": false}
	f.quads = []float64{60, 70, 110, 70, 110, 80, 60, 80}
	f.mu.Unlock()
	a, err = same.Aim(context.Background(), AimOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if a.Point != (Point{X: 85, Y: 75}) || a.Rect.Left != 60 {
		t.Errorf("same-process frame aim = %+v", a)
	}
}

// TestAimSettlesAndRecentres (learn-from 02): after the scroll the aim
// waits for the box to stop moving (an awaited settle), and a node still
// outside the viewport or covered is scrolled to the centre once, settled
// and measured again before the refusal. No scroll, no settle; a disabled
// or hidden node is not re-centred.
func TestAimSettlesAndRecentres(t *testing.T) {
	all := AimOptions{Scroll: true, Enabled: true, Visible: true, HitTest: true}
	moves := func(f *fakePage) []string {
		f.mu.Lock()
		defer f.mu.Unlock()
		return append([]string(nil), f.moves...)
	}

	// In view and still: one settle, awaited, and no re-centre.
	f, c := newFakePage(t)
	o := Origin{Client: c, SessionID: "S", FrameID: "F1", LoaderID: "L1", BackendNodeID: 7}
	if _, err := o.Aim(context.Background(), all); err != nil {
		t.Fatal(err)
	}
	if got := moves(f); !reflect.DeepEqual(got, []string{"settle 7"}) || f.awaited != 1 {
		t.Errorf("in view: moves %v, awaited %d", got, f.awaited)
	}

	// Straddling the edge, or under a sticky bar: the re-centre brings it in.
	for _, st := range []map[string]any{{"state": "outside"}, {"state": "covered", "by": `<header class="sticky">`}} {
		f, c = newFakePage(t)
		o = Origin{Client: c, SessionID: "S", FrameID: "F1", LoaderID: "L1", BackendNodeID: 7}
		f.aim[7] = st
		f.recentred = map[int64]map[string]any{7: {"state": "ok", "x": 40.0, "y": 300.0}}
		a, err := o.Aim(context.Background(), all)
		if err != nil {
			t.Fatalf("%v: %v", st["state"], err)
		}
		if a.Point != (Point{X: 40, Y: 300}) {
			t.Errorf("%v: aimed at %+v, want the re-centred point", st["state"], a.Point)
		}
		if got := moves(f); !reflect.DeepEqual(got, []string{"settle 7", "recentre 7", "settle 7"}) {
			t.Errorf("%v: moves %v", st["state"], got)
		}
		if len(f.scrolled) != 1 || len(f.log()) != 0 {
			t.Errorf("%v: scrolled %v, sent %v", st["state"], f.scrolled, f.log())
		}
	}

	// Still covered after the re-centre: the refusal, once.
	f, c = newFakePage(t)
	o = Origin{Client: c, SessionID: "S", FrameID: "F1", LoaderID: "L1", BackendNodeID: 7}
	f.aim[7] = map[string]any{"state": "covered", "by": `<div class="cookie-banner">`}
	var na *NotActionableError
	if _, err := o.Aim(context.Background(), all); !errors.As(err, &na) || na.Why != WhyCovered {
		t.Errorf("still covered: %v", err)
	}
	if got := moves(f); !reflect.DeepEqual(got, []string{"settle 7", "recentre 7", "settle 7"}) {
		t.Errorf("still covered: moves %v", got)
	}

	// Disabled and hidden are not the click point's to fix.
	for _, why := range []string{"disabled", "hidden"} {
		f, c = newFakePage(t)
		o = Origin{Client: c, SessionID: "S", FrameID: "F1", LoaderID: "L1", BackendNodeID: 7}
		f.aim[7] = map[string]any{"state": why}
		if _, err := o.Aim(context.Background(), all); !errors.As(err, &na) || na.Why != why {
			t.Errorf("%s: %v", why, err)
		}
		if got := moves(f); !reflect.DeepEqual(got, []string{"settle 7"}) {
			t.Errorf("%s: moves %v, want no re-centre", why, got)
		}
	}

	// No scroll asked for: nothing moves, nothing settles.
	f, c = newFakePage(t)
	o = Origin{Client: c, SessionID: "S", FrameID: "F1", LoaderID: "L1", BackendNodeID: 7}
	f.aim[7] = map[string]any{"state": "outside"}
	if _, err := o.Aim(context.Background(), AimOptions{Visible: true}); !errors.As(err, &na) || na.Why != WhyOutside {
		t.Errorf("no scroll: %v", err)
	}
	if got := moves(f); len(got) != 0 {
		t.Errorf("no scroll: moves %v", got)
	}

	// A cross-origin iframe's node: its iframe element is re-centred too.
	f, c = newFakePage(t)
	o = Origin{Client: c, SessionID: "C", FrameID: "F2", BackendNodeID: 40, Owner: &Owner{SessionID: "S", BackendNodeID: 300}}
	f.aim[40] = map[string]any{"state": "outside"}
	f.Aim(t, o, all)
	if got := moves(f); !reflect.DeepEqual(got, []string{"settle 40", "recentre 300", "recentre 40", "settle 40"}) {
		t.Errorf("iframe: moves %v", got)
	}
}

// Aim runs an aim whose refusal the test does not check.
func (f *fakePage) Aim(t *testing.T, o Origin, opt AimOptions) {
	t.Helper()
	_, _ = o.Aim(context.Background(), opt)
}

// TestSettleIsBounded: a page whose settle never answers costs at most
// settleTimeout, and the aim still measures.
func TestSettleIsBounded(t *testing.T) {
	f, c := newFakePage(t)
	f.srv.Handle("Runtime.callFunctionOn", func(cn *cdptest.Conn, m cdptest.Message) (any, error) {
		if strings.Contains(string(m.Params), "winuse:settle") {
			return nil, cdptest.ErrNoReply
		}
		return map[string]any{"result": map[string]any{"type": "object", "value": map[string]any{"state": "ok", "w": 5.0, "h": 5.0, "dpr": 1.0, "chain": true}}}, nil
	})
	o := Origin{Client: c, SessionID: "S", FrameID: "F1", LoaderID: "L1", BackendNodeID: 7}
	start := time.Now()
	if _, err := o.Aim(context.Background(), AimOptions{Scroll: true, Visible: true}); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > settleTimeout+time.Second {
		t.Errorf("the aim took %s", took)
	}
}

func TestInputClickAndDialogRace(t *testing.T) {
	f, c := newFakePage(t)
	ctx := context.Background()
	in, err := NewInput(ctx, c, "S", nil, "C")
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err := in.Click(ctx, Point{X: 1, Y: 2}, ButtonLeft, 1); err != nil {
		t.Fatal(err)
	}
	want := []string{"S Input.dispatchMouseEvent mouseMoved", "S Input.dispatchMouseEvent mousePressed", "S Input.dispatchMouseEvent mouseReleased"}
	if got := f.log(); !reflect.DeepEqual(got, want) || in.Sent != 3 {
		t.Errorf("click = %v, sent %d", got, in.Sent)
	}
	if !strings.Contains(strings.Join(f.srv.Methods(), ","), "Page.enable") {
		t.Error("Page.enable was not sent, so no dialog events would flow")
	}

	// The press opens a confirm dialog and never answers: the race reports
	// it at once, and nothing more is sent.
	f.mu.Lock()
	f.holdPress = true
	f.inputs = nil
	f.mu.Unlock()
	start := time.Now()
	err = in.Click(ctx, Point{}, ButtonLeft, 1)
	var d *DialogError
	if !errors.As(err, &d) || d.Type != "confirm" || err.Error() != `the Page opened a confirm dialog: "Leave this page?"` {
		t.Fatalf("dialog: %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Error("the race waited for the press to answer")
	}
	if got := f.log(); len(got) != 2 || in.Sent != 5 {
		t.Errorf("after the dialog: %v, sent %d", got, in.Sent)
	}
}

func TestInputStopsAndRefusals(t *testing.T) {
	f, c := newFakePage(t)
	ctx := context.Background()
	stop := false
	in, err := NewInput(ctx, c, "S", func() bool { return stop })
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	stop = true
	if err := in.Click(ctx, Point{}, ButtonLeft, 1); !errors.Is(err, ErrStopped) || len(f.log()) != 0 {
		t.Errorf("stopped before the first event: %v, %v", err, f.log())
	}
	stop = false
	f.mu.Lock()
	f.refuse = "Input.insertText"
	f.mu.Unlock()
	err = in.InsertText(ctx, "x")
	var se *SendError
	if !errors.As(err, &se) || se.Sent != 0 || se.NoReply {
		t.Errorf("refused: %v", err)
	}
	// Type text: runs inserted, newline and tab as keys.
	f.mu.Lock()
	f.refuse = ""
	f.mu.Unlock()
	if err := in.TypeText(ctx, "hi\nyo"); err != nil {
		t.Fatal(err)
	}
	want := []string{"S Input.insertText hi", "S Input.dispatchKeyEvent keyDown", "S Input.dispatchKeyEvent keyUp", "S Input.insertText yo"}
	if got := f.log(); !reflect.DeepEqual(got, want) {
		t.Errorf("typed %v", got)
	}

	// A Page that never answers times out as NoReply, counted as sent.
	f.srv.Handle("Input.dispatchMouseEvent", func(c *cdptest.Conn, m cdptest.Message) (any, error) { return nil, cdptest.ErrNoReply })
	in.timeout = 100 * time.Millisecond
	before := in.Sent
	err = in.Wheel(ctx, Point{}, 0, 100)
	if !errors.As(err, &se) || !se.NoReply || in.Sent != before+1 {
		t.Errorf("no reply: %v", err)
	}
}

func TestSetValueReadsBack(t *testing.T) {
	f, c := newFakePage(t)
	ctx := context.Background()
	o := Origin{Client: c, SessionID: "S", FrameID: "F1", LoaderID: "L1", BackendNodeID: 7}
	fld, err := o.ReadField(ctx)
	if err != nil || fld.Kind != "text" {
		t.Fatalf("field %+v, %v", fld, err)
	}
	in, err := NewInput(ctx, c, "S", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err := o.SetValue(ctx, in, fld, "new text"); err != nil {
		t.Fatal(err)
	}
	if got := f.log(); !reflect.DeepEqual(got, []string{"focus", "select", "S Input.insertText new text"}) {
		t.Errorf("set-value = %v", got)
	}
	// An empty value deletes the selection with a Backspace.
	f.mu.Lock()
	f.inputs, f.value = nil, ""
	f.mu.Unlock()
	if err := o.SetValue(ctx, in, fld, ""); err != nil {
		t.Fatal(err)
	}
	if got := f.log(); len(got) != 4 || got[2] != "S Input.dispatchKeyEvent rawKeyDown" {
		t.Errorf("empty value = %v", got)
	}
	// A field that limits its length: delivered, read back, never echoing
	// a password.
	f.srv.Handle("Input.insertText", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		f.mu.Lock()
		f.value = "secr"
		f.mu.Unlock()
		return nil, nil
	})
	err = o.SetValue(ctx, in, Field{Kind: "text", Password: true}, "secret-pass")
	var v *VerifyError
	if !errors.As(err, &v) || strings.Contains(err.Error(), "secr") {
		t.Errorf("password read-back: %v", err)
	}
}

// A placeholder ("facade") field mounts the real editor on focus and hands
// it the focus (bladebro-ideas 09): set-value selects, writes and reads back
// in the editor, reads that node's own field (editable text now), and says
// where the text went.
func TestSetValueFollowsAHandedOffFocus(t *testing.T) {
	f, c := newFakePage(t)
	ctx := context.Background()
	o := Origin{Client: c, SessionID: "S", FrameID: "F1", LoaderID: "L1", BackendNodeID: 7}
	f.mu.Lock()
	f.follow = map[int64][]any{7: {8}}
	f.fields = map[int64]map[string]any{8: {"kind": "editable"}}
	f.labels = map[int64]map[string]any{8: {"role": "", "name": "Comment editor"}}
	// innerText of <p>hello</p> with its paragraph newline: editable text
	// normalizes it, a text field would not.
	f.reads = map[int64]any{7: "", 8: "hello\n"}
	f.mu.Unlock()
	in, err := NewInput(ctx, c, "S", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err := o.SetValue(ctx, in, Field{Kind: "text"}, "hello"); err != nil {
		t.Fatalf("set-value through the handed-off focus: %v", err)
	}
	f.mu.Lock()
	selected, readFrom := f.selected, f.readFrom
	f.mu.Unlock()
	if !reflect.DeepEqual(selected, []int64{8}) || !reflect.DeepEqual(readFrom, []int64{8}) {
		t.Errorf("selected %v and read back %v, want the editor 8 for both", selected, readFrom)
	}
	if got := f.log(); !reflect.DeepEqual(got, []string{"focus", "select", "S Input.insertText hello"}) {
		t.Errorf("set-value = %v", got)
	}
	want := []string{`the field handed its focus to edit "Comment editor"; the text went there`}
	if !reflect.DeepEqual(in.Notes, want) {
		t.Errorf("notes = %q, want %q", in.Notes, want)
	}
}

// A field that keeps its focus gets no note and no extra writes.
func TestSetValueWithoutAHandOffAddsNoNote(t *testing.T) {
	f, c := newFakePage(t)
	ctx := context.Background()
	o := Origin{Client: c, SessionID: "S", FrameID: "F1", LoaderID: "L1", BackendNodeID: 7}
	in, err := NewInput(ctx, c, "S", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err := o.SetValue(ctx, in, Field{Kind: "text"}, "plain"); err != nil {
		t.Fatal(err)
	}
	if len(in.Notes) != 0 {
		t.Errorf("notes = %q, want none", in.Notes)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !reflect.DeepEqual(f.selected, []int64{7}) || !reflect.DeepEqual(f.readFrom, []int64{7}) {
		t.Errorf("selected %v and read back %v, want the field 7", f.selected, f.readFrom)
	}
}

// The read-back never reads a node that left the document (an editor that
// replaced itself on the input): it reads the focused editable element
// that took its place and says so, or reports that it could not read back,
// never the stale copy.
func TestCheckValueNeverReadsADetachedNode(t *testing.T) {
	f, c := newFakePage(t)
	ctx := context.Background()
	o := Origin{Client: c, SessionID: "S", FrameID: "F1", LoaderID: "L1", BackendNodeID: 7}
	in, err := NewInput(ctx, c, "S", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	f.mu.Lock()
	f.reads = map[int64]any{7: map[string]any{"replaced": true, "value": "a\nb", "kind": "editable", "role": "", "name": "Remount editor"}}
	f.mu.Unlock()
	if err := o.CheckValue(ctx, in, Field{Kind: "editable"}, "a\nb"); err != nil {
		t.Fatalf("read through the editor that took its place: %v", err)
	}
	want := []string{`the field was replaced after the input; its value was read back from the focused edit "Remount editor"`}
	if !reflect.DeepEqual(in.Notes, want) {
		t.Errorf("notes = %q, want %q", in.Notes, want)
	}

	// The replacement holds other text: a mismatch that names where it read.
	f.mu.Lock()
	f.reads[7] = map[string]any{"replaced": true, "value": "ab", "kind": "editable", "name": "Remount editor"}
	f.mu.Unlock()
	err = o.CheckValue(ctx, nil, Field{Kind: "editable"}, "a\nb")
	var v *VerifyError
	if !errors.As(err, &v) || !strings.Contains(err.Error(), `holds "ab" afterwards`) || !strings.Contains(err.Error(), `edit "Remount editor"`) {
		t.Errorf("mismatch through the replacement: %v", err)
	}

	// Nothing editable holds the focus: no read-back, and no stale copy.
	f.mu.Lock()
	f.reads[7] = map[string]any{"replaced": true, "lost": true}
	f.mu.Unlock()
	err = o.CheckValue(ctx, nil, Field{Kind: "editable"}, "a\nb")
	if !errors.As(err, &v) || !strings.Contains(err.Error(), "could not be read back") || !strings.Contains(err.Error(), "left the document") {
		t.Errorf("lost: %v", err)
	}
}

// type-text --into on an editor that replaces its node on the first input
// (bladebro-ideas 09): before each part after the first, a replaced node
// hands over to the focused editable element, with the caret put at its
// end, and the typing goes on there, once noted.
func TestTypeIntoFollowsARemount(t *testing.T) {
	f, c := newFakePage(t)
	ctx := context.Background()
	o := Origin{Client: c, SessionID: "S", FrameID: "F1", LoaderID: "L1", BackendNodeID: 7}
	f.mu.Lock()
	// After the focus: no hand-off. Before the Return: replaced by 9.
	f.follow = map[int64][]any{7: {nil, 9}}
	f.labels = map[int64]map[string]any{9: {"role": "textbox", "name": "Remount editor"}}
	f.mu.Unlock()
	in, err := NewInput(ctx, c, "S", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err := o.TypeInto(ctx, in, "one\ntwo"); err != nil {
		t.Fatalf("type-text --into across a remount: %v", err)
	}
	want := []string{"focus", "S Input.insertText one", "S Input.dispatchKeyEvent keyDown", "S Input.dispatchKeyEvent keyUp", "S Input.insertText two"}
	if got := f.log(); !reflect.DeepEqual(got, want) {
		t.Errorf("typed %v, want %v", got, want)
	}
	notes := []string{`the field was replaced while typing; the rest of the text went to the focused edit "Remount editor", at its end`}
	if !reflect.DeepEqual(in.Notes, notes) {
		t.Errorf("notes = %q, want %q", in.Notes, notes)
	}
	// Each check after the first part asked for the caret at the end.
	var follows []string
	for _, m := range f.srv.Received() {
		if m.Method != "Runtime.callFunctionOn" || !strings.Contains(string(m.Params), "winuse:follow") {
			continue
		}
		var p struct {
			Args []struct {
				Value any `json:"value"`
			} `json:"arguments"`
		}
		json.Unmarshal(m.Params, &p)
		follows = append(follows, fmt.Sprint(p.Args[0].Value))
	}
	if !reflect.DeepEqual(follows, []string{"focus", "replaced", "replaced"}) {
		t.Errorf("follow calls %v, want the focus check, then a replaced check before each later part", follows)
	}
}

// A field replaced while typing with nothing editable focused stops the
// typing at once, as a delivered failure (the text so far went in), never
// typing the rest somewhere else.
func TestTypeIntoStopsWhenTheFieldIsLost(t *testing.T) {
	f, c := newFakePage(t)
	ctx := context.Background()
	o := Origin{Client: c, SessionID: "S", FrameID: "F1", LoaderID: "L1", BackendNodeID: 7}
	f.mu.Lock()
	f.follow = map[int64][]any{7: {nil, "lost"}}
	f.mu.Unlock()
	in, err := NewInput(ctx, c, "S", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	err = o.TypeInto(ctx, in, "one\ntwo")
	var v *VerifyError
	if err == nil || errors.As(err, &v) || errors.Is(err, ErrStale) || !strings.Contains(err.Error(), "left the document while typing") {
		t.Fatalf("lost field: %v", err)
	}
	if got := f.log(); !reflect.DeepEqual(got, []string{"focus", "S Input.insertText one"}) || in.Sent != 1 {
		t.Errorf("typed %v (sent %d), want only the first part", got, in.Sent)
	}
}

// Editable text drops the zero-width characters editors use as caret
// anchors (Slate's U+FEFF, U+200B), not other text.
func TestValueMatchesEditableZeroWidth(t *testing.T) {
	zwsp, bom := string(rune(0x200B)), string(rune(0xFEFF))
	if err := ValueMatches(Field{Kind: "editable"}, "a\nb", "a"+zwsp+"\n"+bom+"b"); err != nil {
		t.Errorf("zero-width anchors: %v", err)
	}
	if err := ValueMatches(Field{Kind: "editable"}, "a\n\nb", "a\nb"); err == nil {
		t.Error("a missing blank line matched")
	}
	if err := ValueMatches(Field{Kind: "text"}, "ab", "a"+zwsp+"b"); err == nil {
		t.Error("a text field's zero-width character was dropped")
	}
}

func TestReadFocus(t *testing.T) {
	f, c := newFakePage(t)
	f.srv.Handle("Runtime.evaluate", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p struct {
			Expression string `json:"expression"`
		}
		json.Unmarshal(m.Params, &p)
		if strings.Contains(p.Expression, "winuse:focus") {
			return map[string]any{"result": map[string]any{"value": map[string]any{"kind": "editable", "tag": "input", "x": 1, "y": 2, "w": 3, "h": 4, "dpr": 2}}}, nil
		}
		return map[string]any{"result": map[string]any{"value": 1.25}}, nil
	})
	got, err := ReadFocus(context.Background(), c, "S")
	if err != nil || got.Kind != "editable" || got.Rect() != (Rect{Left: 1, Top: 2, Width: 3, Height: 4, DPR: 2}) {
		t.Errorf("focus = %+v, %v", got, err)
	}
	if dpr, err := ReadDPR(context.Background(), c, "S"); err != nil || dpr != 1.25 {
		t.Errorf("dpr = %v, %v", dpr, err)
	}
	c.Close()
	if _, err := ReadFocus(context.Background(), c, "S"); !errors.Is(err, ErrStale) {
		t.Errorf("closed: %v", err)
	}
}
