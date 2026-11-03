package helper

// Page Actions' offline tests (browser merge ticket 05): the routing table
// per command, origin and --headed; the actionability refusals, each with
// "nothing was sent"; the dialog race; the Agent Cursor through the
// CSS-to-screen transform; set-value's read-back and password non-echo;
// the keyboard's focus decision; the scroll deltas and the inverse
// transform; the Batch's scope; the Esc checkpoints. The Page is a
// cdptest fake; every Win32 seam is the headless spy's. No browser runs.

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/shot"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// pageActor is the headless spy's element side plus the live-Element
// store's Page origins, as *uia.Client is both.
type pageActor struct {
	*headlessSpy
	trees *pageTrees
}

func (a pageActor) SplicePage(ctx context.Context, hwnd uint64, base, merged []wstate.Element, from []int, origins map[int]any) error {
	return a.trees.SplicePage(ctx, hwnd, base, merged, from, origins)
}

func (a pageActor) ElementOrigin(hwnd uint64, index int) (any, error) {
	return a.trees.ElementOrigin(hwnd, index)
}

func (a pageActor) StoredElements(hwnd uint64) ([]wstate.Element, bool) {
	a.trees.mu.Lock()
	defer a.trees.mu.Unlock()
	els, ok := a.trees.state[hwnd]
	return append([]wstate.Element(nil), els...), ok
}

// tabPages is a PageReader whose active Tab the test sets.
type tabPages struct {
	fakePages
	mu  sync.Mutex
	tab string
}

func (p *tabPages) PageActiveTab(ctx context.Context, w win.WindowInfo) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tab, nil
}

// pageRig is the browser Window 301 whose latest Window State holds a
// Page: Element 2 is a UIA Element of the frame, 4 the document Element,
// 7 a button and 8 a text field of the Page.
type pageRig struct {
	*headlessSpy
	srv    *cdptest.Server
	client *cdp.Client
	trees  *pageTrees
	pages  *tabPages

	mu        sync.Mutex
	aim       map[string]any // aimJS answer overrides
	field     map[string]any
	value     string
	focus     map[string]any
	loader    string
	holdPress bool
	inputs    []string
	scrolled  int
	scrolls   [][5]int32
	textField map[string]any // select-text's textFieldJS answer
	selection map[string]any // selectTextJS answer override; nil echoes the span
}

func newPageRig(t *testing.T) *pageRig {
	t.Helper()
	r := &pageRig{headlessSpy: newHeadlessSpy(), srv: cdptest.NewServer(), aim: map[string]any{},
		field: map[string]any{"kind": "text"}, textField: map[string]any{"kind": "text", "length": 14, "selectable": true}, focus: map[string]any{"kind": "editable", "x": 10, "y": 20, "w": 50, "h": 10, "dpr": 1.5}, loader: "L1"}
	t.Cleanup(r.srv.Close)
	r.bounds = win.Rect{Left: 100, Top: 200, Right: 1100, Bottom: 900} // the document Element
	r.handlers()
	ws, err := cdp.DialWS(context.Background(), r.srv.URL(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	r.client = cdp.NewClient(ws)
	t.Cleanup(func() { r.client.Close() })

	r.trees = newPageTrees(t)
	els := make([]wstate.Element, 10)
	els[2].Name, els[4].Name, els[7].Name, els[8].Name = "Reload", "Sign in", "Sign in", "Email"
	r.trees.state[browserWindowID] = els
	origin := func(node int64) *page.Origin {
		return &page.Origin{Browser: "chrome", Client: r.client, TargetID: "T1", SessionID: "S", FrameID: "F1", LoaderID: "L1", BackendNodeID: node, DocIndex: 4}
	}
	r.trees.origins[browserWindowID] = map[int]any{7: origin(70), 8: origin(80)}
	r.pages = &tabPages{tab: "T1"}

	w := browserWindow()
	w.Bounds = win.Rect{Left: 0, Top: 0, Right: 1200, Bottom: 1000}
	r.svc.Windows = func() ([]win.WindowInfo, error) { return append(fakeWindows(), w), nil }
	r.svc.Actions = pageActor{r.headlessSpy, r.trees}
	r.svc.Pages = r.pages
	r.svc.Shots = shot.NewStore()
	r.svc.FrameOrigin = func(hwnd uint64) (image.Point, error) { return image.Point{}, nil }
	r.svc.Scroll = func(target uint64, x, y, dx, dy int32) error {
		r.step("wheel")
		r.mu.Lock()
		r.scrolls = append(r.scrolls, [5]int32{int32(target), x, y, dx, dy})
		r.mu.Unlock()
		return nil
	}
	return r
}

func (r *pageRig) handlers() {
	r.srv.Handle("Page.getFrameTree", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		return map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "F1", "loaderId": r.loader}}}, nil
	})
	r.srv.Handle("DOM.scrollIntoViewIfNeeded", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		r.mu.Lock()
		r.scrolled++
		r.mu.Unlock()
		return nil, nil
	})
	r.srv.Handle("DOM.resolveNode", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		return map[string]any{"object": map[string]any{"type": "object", "objectId": "obj"}}, nil
	})
	r.srv.Handle("DOM.focus", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		r.record("focus")
		return nil, nil
	})
	r.srv.Handle("Runtime.callFunctionOn", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p struct {
			Fn   string `json:"functionDeclaration"`
			Args []struct {
				Value any `json:"value"`
			} `json:"arguments"`
		}
		json.Unmarshal(m.Params, &p)
		r.mu.Lock()
		defer r.mu.Unlock()
		var v any
		switch {
		case strings.Contains(p.Fn, "winuse:textfield"):
			v = r.textField
		case strings.Contains(p.Fn, "winuse:selecttext"):
			// The span as sent: start and end, or "all" for an undefined end.
			start, end := p.Args[0].Value, p.Args[1].Value
			if end == nil {
				r.inputs = append(r.inputs, fmt.Sprintf("selecttext %v all", start))
				end = r.textField["length"]
			} else {
				r.inputs = append(r.inputs, fmt.Sprintf("selecttext %v %v", start, end))
			}
			v = map[string]any{"start": start, "end": end, "length": r.textField["length"]}
			if r.selection != nil {
				v = r.selection
			}
		case strings.Contains(p.Fn, "winuse:aim"):
			st := map[string]any{"state": "ok", "l": 10.0, "t": 20.0, "w": 50.0, "h": 10.0, "x": 35.0, "y": 25.0, "chain": true, "dpr": 1.5, "name": "page name"}
			for k, x := range r.aim {
				st[k] = x
			}
			v = st
		case strings.Contains(p.Fn, "winuse:field"):
			v = r.field
		case strings.Contains(p.Fn, "winuse:select"):
			r.inputs = append(r.inputs, "select")
			v = true
		case strings.Contains(p.Fn, "winuse:read"):
			v = r.value
		case strings.Contains(p.Fn, "isConnected"):
			v = true
		}
		return map[string]any{"result": map[string]any{"type": "object", "value": v}}, nil
	})
	r.srv.Handle("Runtime.evaluate", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p struct {
			Expression string `json:"expression"`
		}
		json.Unmarshal(m.Params, &p)
		r.mu.Lock()
		defer r.mu.Unlock()
		if strings.Contains(p.Expression, "winuse:focus") {
			return map[string]any{"result": map[string]any{"value": r.focus}}, nil
		}
		return map[string]any{"result": map[string]any{"value": 1.5}}, nil
	})
	input := func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p map[string]any
		json.Unmarshal(m.Params, &p)
		desc := strings.TrimPrefix(m.Method, "Input.")
		for _, k := range []string{"type", "button", "key", "text"} {
			if v, ok := p[k]; ok {
				desc += fmt.Sprintf(" %v", v)
			}
		}
		for _, k := range []string{"x", "y", "clickCount", "deltaX", "deltaY"} {
			if v, ok := p[k]; ok {
				desc += fmt.Sprintf(" %s=%v", k, v)
			}
		}
		r.mu.Lock()
		r.inputs = append(r.inputs, desc)
		if m.Method == "Input.insertText" {
			r.value = p["text"].(string)
		}
		hold := r.holdPress && p["type"] == "mousePressed"
		r.mu.Unlock()
		if hold {
			c.Event(m.SessionID, "Page.javascriptDialogOpening", map[string]any{"type": "confirm", "message": "Leave this page?"})
			return nil, cdptest.ErrNoReply
		}
		return nil, nil
	}
	r.srv.Handle("Input.dispatchMouseEvent", input)
	r.srv.Handle("Input.dispatchKeyEvent", input)
	r.srv.Handle("Input.insertText", input)
}

func (r *pageRig) record(s string) {
	r.mu.Lock()
	r.inputs = append(r.inputs, s)
	r.mu.Unlock()
}

func (r *pageRig) sent() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.inputs...)
}

func (r *pageRig) set(f func()) {
	r.mu.Lock()
	f()
	r.mu.Unlock()
}

func (r *pageRig) run(t *testing.T, cmd string, args any) (string, *Error) {
	t.Helper()
	return runCommand(t, CommandsWith(r.svc), cmd, args)
}

// noForeground fails when a Page Action reached a Win32 input or
// foreground seam.
func (r *pageRig) noForeground(t *testing.T) {
	t.Helper()
	for _, s := range r.recorded() {
		switch s {
		case "activate", "click", "type-text", "press-key", "integrity", "ready", "headless-click", "headless-set-value", "wheel":
			t.Errorf("a Page Action ran %q: %v", s, r.recorded())
		}
	}
}

func idx(i int) *int { return &i }

// TestPageRoutingTable: the routing per command, origin and --headed.
func TestPageRoutingTable(t *testing.T) {
	type row struct {
		name    string
		cmd     string
		args    any
		wantOut string
		wantErr string   // substring of the failure, "" for ok
		steps   []string // Win32 or UIA spy steps that must appear
		cdp     bool     // CDP input must have been sent
	}
	rows := []row{
		{"click page", "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7)}, okOutput, "", nil, true},
		{"double click page", "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7), ClickCount: 2}, okOutput, "", nil, true},
		{"right click page", "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7), MouseButton: "right"}, okOutput, "", nil, true},
		{"click page by name", "click", protocol.ClickArgs{Window: "301", Ref: "name:Sign in"}, okOutput, "", []string{"resolve"}, true},
		{"click page headed", "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7), Headed: true}, okOutput, "", []string{"activate", "click"}, false},
		{"click UIA", "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(2)}, okOutput, "", []string{"headless-click"}, false},
		{"set-value page", "set-value", protocol.SetValueArgs{Window: "301", ElementIndex: idx(8), Value: "a@b.c"}, okOutput, "", nil, true},
		{"set-value page headed", "set-value", protocol.SetValueArgs{Window: "301", ElementIndex: idx(8), Value: "x", Headed: true}, "", "set-value --headed does not reach element 8", nil, false},
		{"set-value UIA", "set-value", protocol.SetValueArgs{Window: "301", ElementIndex: idx(2), Value: "x"}, okOutput, "", []string{"headless-set-value"}, false},
		{"secondary page", "perform-secondary-action", protocol.SecondaryActionArgs{Window: "301", ElementIndex: idx(7), Action: "Toggle"}, "", "Secondary Actions do not reach Page Elements", nil, false},
		{"scroll page", "scroll", protocol.ScrollArgs{Window: "301", ElementIndex: idx(7), Direction: "down"}, okOutput, "", nil, true},
		{"scroll page headed", "scroll", protocol.ScrollArgs{Window: "301", ElementIndex: idx(7), Direction: "down", Headed: true}, okOutput, "", []string{"activate", "wheel"}, false},
		{"scroll page into-view", "scroll", protocol.ScrollArgs{Window: "301", ElementIndex: idx(7), Direction: "into-view"}, okOutput, "", nil, false},
		{"type-text page", "type-text", protocol.TypeTextArgs{Window: "301", Text: "hello"}, okOutput, "", nil, true},
		{"type-text headed", "type-text", protocol.TypeTextArgs{Window: "301", Text: "hello", Headed: true}, okOutput, "", []string{"activate", "type-text"}, false},
		{"press-key page", "press-key", protocol.PressKeyArgs{Window: "301", Key: "Control_L+a"}, okOutput, "", nil, true},
		{"press-key headed", "press-key", protocol.PressKeyArgs{Window: "301", Key: "Return", Headed: true}, okOutput, "", []string{"activate", "press-key"}, false},
	}
	for _, rw := range rows {
		t.Run(rw.name, func(t *testing.T) {
			r := newPageRig(t)
			r.refIndex = 7
			out, e := r.run(t, rw.cmd, rw.args)
			if rw.wantErr != "" {
				if e == nil || !strings.Contains(e.Msg, rw.wantErr) || !strings.HasSuffix(e.Msg, "nothing was sent") {
					t.Fatalf("want a refusal %q, got %q %+v", rw.wantErr, out, e)
				}
			} else if e != nil || out != rw.wantOut {
				t.Fatalf("out %q, err %+v", out, e)
			}
			steps := strings.Join(r.recorded(), ",")
			for _, s := range rw.steps {
				if !strings.Contains(","+steps+",", ","+s+",") {
					t.Errorf("missing step %q in %s", s, steps)
				}
			}
			hasInput := false
			for _, s := range r.sent() {
				if strings.HasPrefix(s, "dispatch") || strings.HasPrefix(s, "insertText") {
					hasInput = true
				}
			}
			if hasInput != rw.cdp {
				t.Errorf("CDP input sent = %v, want %v: %v", hasInput, rw.cdp, r.sent())
			}
			if rw.cdp {
				r.noForeground(t)
				if got := strings.Join(r.esc.brackets(), ","); got != "arm-passive,disarm-passive" {
					t.Errorf("Esc scope = %s", got)
				}
			}
		})
	}
}

// TestPageClickSendsTrustedInputAndShowsTheCursor: the mouse sequence at
// the box centre, and the Agent Cursor at the transformed rectangle with
// the Element's name from the Window State.
func TestPageClickSendsTrustedInputAndShowsTheCursor(t *testing.T) {
	r := newPageRig(t)
	if _, e := r.run(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7), ClickCount: 2}); e != nil {
		t.Fatal(e.Error())
	}
	want := []string{
		"dispatchMouseEvent mouseMoved none x=35 y=25",
		"dispatchMouseEvent mousePressed left x=35 y=25 clickCount=1",
		"dispatchMouseEvent mouseReleased left x=35 y=25 clickCount=1",
		"dispatchMouseEvent mousePressed left x=35 y=25 clickCount=2",
		"dispatchMouseEvent mouseReleased left x=35 y=25 clickCount=2",
	}
	if got := r.sent(); !reflect.DeepEqual(got, want) {
		t.Errorf("sent:\n%s", strings.Join(got, "\n"))
	}
	// document (100, 200) + CSS (10, 20, 50x10) x 1.5
	rect := win.Rect{Left: 115, Top: 230, Right: 190, Bottom: 245}
	if len(r.cursor.targets) != 1 || r.cursor.targets[0] != rect || len(r.cursor.clicks) != 1 || r.cursor.clicks[0] != rect {
		t.Errorf("cursor %v, clicks %v", r.cursor.targets, r.cursor.clicks)
	}
	if r.cursor.labels[0] != `double click "Sign in"` {
		t.Errorf("label %q", r.cursor.labels[0])
	}
	if r.scrolled != 1 {
		t.Errorf("scrollIntoViewIfNeeded ran %d times", r.scrolled)
	}
}

// TestPageClickRefusals: every actionability refusal names the reason (and
// the blocker), says nothing was sent, and sends nothing.
func TestPageClickRefusals(t *testing.T) {
	cases := []struct {
		aim  map[string]any
		want string
	}{
		{map[string]any{"state": "covered", "by": `<div class="cookie-banner">`}, `element 7 cannot be clicked: it is covered by <div class="cookie-banner"> at its centre; nothing was sent`},
		{map[string]any{"state": "disabled"}, "element 7 cannot be clicked: it is disabled; nothing was sent"},
		{map[string]any{"state": "hidden"}, "element 7 cannot be clicked: it is hidden or has no size; nothing was sent"},
		{map[string]any{"state": "outside"}, "element 7 cannot be clicked: its centre is outside the visible part of the Page, even after scrolling it into view; nothing was sent"},
	}
	for _, c := range cases {
		for _, headed := range []bool{false, true} {
			r := newPageRig(t)
			r.set(func() { r.aim = c.aim })
			_, e := r.run(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7), Headed: headed})
			if e == nil || e.Msg != c.want || e.Hint == "" || strings.ContainsAny(e.Msg+e.Hint, "–—") {
				t.Errorf("headed=%v: %+v", headed, e)
			}
			if got := r.sent(); len(got) != 0 {
				t.Errorf("headed=%v sent %v", headed, got)
			}
			r.noForeground(t) // the Headed form refuses before activating
			if len(r.cursor.clicks) != 0 {
				t.Error("the cursor rippled on a refusal")
			}
		}
	}
}

// TestPageClickStale: a navigated page and a switched Tab both fail with
// the UIA stale message, before anything is sent.
func TestPageClickStale(t *testing.T) {
	stale := "element 7 is not available in the cached Window State for window 301; nothing was sent"
	r := newPageRig(t)
	r.set(func() { r.loader = "L2" })
	if _, e := r.run(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7)}); e == nil || e.Msg != stale || !strings.Contains(e.Hint, "get-window-state 301 again") {
		t.Errorf("navigated: %+v", e)
	}
	r = newPageRig(t)
	r.pages.tab = "T2"
	if _, e := r.run(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7)}); e == nil || e.Msg != stale {
		t.Errorf("another Tab: %+v", e)
	}
	if _, e := r.run(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7), Headed: true}); e == nil || e.Msg != stale {
		t.Errorf("another Tab, headed: %+v", e)
	}
	if len(r.sent()) != 0 {
		t.Errorf("sent %v", r.sent())
	}
	r.noForeground(t)
}

// TestPageClickDialogRace: a press that opens a dialog is reported at
// once: the click was delivered, so it is `ok` and a note names the
// dialog and the dialog command (desktop-parity 02).
func TestPageClickDialogRace(t *testing.T) {
	r := newPageRig(t)
	r.set(func() { r.holdPress = true })
	start := time.Now()
	out, e := r.run(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7)})
	if e != nil {
		t.Fatalf("dialog: %+v", e)
	}
	want := "ok\nNote: the Page opened a confirm dialog: \"Leave this page?\"; the Page takes no other input until it is answered: " +
		"run winuse dialog 301 accept or winuse dialog 301 dismiss (a prompt takes its text: winuse dialog 301 accept <text>)\n"
	if out != want {
		t.Errorf("output %q, want %q", out, want)
	}
	if time.Since(start) > 3*time.Second {
		t.Error("the click waited for the held press")
	}
	if len(r.cursor.clicks) != 1 {
		t.Error("a delivered click ripples the cursor")
	}
}

// TestPageClickHeadedUsesTheTransform: --headed clicks the v1 way at the
// Element's transformed screen point.
func TestPageClickHeadedUsesTheTransform(t *testing.T) {
	r := newPageRig(t)
	if _, e := r.run(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7), Headed: true, MouseButton: "right"}); e != nil {
		t.Fatal(e.Error())
	}
	// (100 + 35 x 1.5, 200 + 25 x 1.5) = (152.5, 237.5), rounded.
	if r.clickX != 153 || r.clickY != 238 || r.clickButton != win.MouseRight || r.clickTarget != browserWindowID {
		t.Errorf("click at %d,%d button %v target %d", r.clickX, r.clickY, r.clickButton, r.clickTarget)
	}
	steps := strings.Join(r.recorded(), ",")
	if !strings.Contains(steps, "activate") || strings.Index(steps, "activate") > strings.Index(steps, ",click") {
		t.Errorf("steps %s", steps)
	}
}

// TestPageSetValue: focus, select all, insertText, read back; a password
// mismatch shows only lengths; a field that takes no text sends nothing.
func TestPageSetValue(t *testing.T) {
	r := newPageRig(t)
	if _, e := r.run(t, "set-value", protocol.SetValueArgs{Window: "301", ElementIndex: idx(8), Value: "me@example.com"}); e != nil {
		t.Fatal(e.Error())
	}
	if got := r.sent(); !reflect.DeepEqual(got, []string{"focus", "select", "insertText me@example.com"}) {
		t.Errorf("sent %v", got)
	}
	if r.cursor.labels[0] != `set value "Email"` {
		t.Errorf("label %q", r.cursor.labels[0])
	}

	r = newPageRig(t)
	r.set(func() { r.field = map[string]any{"kind": "text", "password": true} })
	r.srv.Handle("Input.insertText", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		r.set(func() { r.value = "hunt" })
		return nil, nil
	})
	_, e := r.run(t, "set-value", protocol.SetValueArgs{Window: "301", ElementIndex: idx(8), Value: "hunter22"})
	if e == nil || strings.Contains(e.Msg+e.Hint, "hunt") || !strings.Contains(e.Msg, "4 characters afterwards, not 8") || !strings.Contains(e.Hint, "do not repeat it") {
		t.Errorf("password mismatch: %+v", e)
	}

	r = newPageRig(t)
	r.set(func() { r.field = map[string]any{"kind": "", "type": "checkbox"} })
	_, e = r.run(t, "set-value", protocol.SetValueArgs{Window: "301", ElementIndex: idx(8), Value: "on"})
	if e == nil || !strings.HasPrefix(e.Msg, "cannot set a value for element 8: it takes no typed text") || !strings.HasSuffix(e.Msg, "nothing was sent") || !strings.Contains(e.Hint, "winuse click 301 8") {
		t.Errorf("checkbox: %+v", e)
	}
	if len(r.sent()) != 0 {
		t.Errorf("sent %v", r.sent())
	}
}

func TestPageSecondaryActionRefused(t *testing.T) {
	r := newPageRig(t)
	r.refIndex = 7
	_, e := r.run(t, "perform-secondary-action", protocol.SecondaryActionArgs{Window: "301", Ref: "name:Sign in", Action: "expand"})
	if e == nil || e.Msg != "element 7 is in the Page of window 301, and Secondary Actions do not reach Page Elements; nothing was sent" || !strings.Contains(e.Hint, `winuse click 301 "name:Sign in"`) {
		t.Errorf("refusal: %+v", e)
	}
	_, e = r.run(t, "perform-secondary-action", protocol.SecondaryActionArgs{Window: "301", ElementIndex: idx(7), Action: "Raise"})
	if e == nil || !strings.Contains(e.Hint, "activate-window 301") {
		t.Errorf("raise: %+v", e)
	}
	r.noForeground(t)
}

// TestPageScroll: by steps, one wheel at the centre of 100 CSS px a step;
// into-view, the scroll alone, read back.
func TestPageScroll(t *testing.T) {
	r := newPageRig(t)
	if _, e := r.run(t, "scroll", protocol.ScrollArgs{Window: "301", ElementIndex: idx(7), Direction: "up", Steps: idx(2)}); e != nil {
		t.Fatal(e.Error())
	}
	if got := r.sent(); !reflect.DeepEqual(got, []string{"dispatchMouseEvent mouseWheel x=35 y=25 deltaX=0 deltaY=-200"}) {
		t.Errorf("sent %v", got)
	}

	r = newPageRig(t)
	if _, e := r.run(t, "scroll", protocol.ScrollArgs{Window: "301", ElementIndex: idx(7), Direction: "into-view"}); e != nil {
		t.Fatal(e.Error())
	}
	if r.scrolled != 1 || len(r.sent()) != 0 || len(r.cursor.clicks) != 1 {
		t.Errorf("into-view: scrolled %d, sent %v, ripples %d", r.scrolled, r.sent(), len(r.cursor.clicks))
	}
	r.set(func() { r.aim = map[string]any{"state": "outside"} })
	_, e := r.run(t, "scroll", protocol.ScrollArgs{Window: "301", ElementIndex: idx(7), Direction: "into-view"})
	if e == nil || e.Msg != "the Page scrolled, but element 7 is still outside its visible part" || !strings.Contains(e.Hint, "do not repeat it") {
		t.Errorf("still outside: %+v", e)
	}
}

func TestPageWheelDeltas(t *testing.T) {
	r := newPageRig(t)
	for _, c := range []struct {
		dir    string
		dx, dy string
	}{{"down", "0", "300"}, {"up", "0", "-300"}, {"left", "-300", "0"}, {"right", "300", "0"}} {
		r.set(func() { r.inputs = nil })
		if _, e := r.run(t, "scroll", protocol.ScrollArgs{Window: "301", ElementIndex: idx(7), Direction: c.dir}); e != nil {
			t.Fatal(e.Error())
		}
		want := fmt.Sprintf("dispatchMouseEvent mouseWheel x=35 y=25 deltaX=%s deltaY=%s", c.dx, c.dy)
		if got := r.sent(); len(got) != 1 || got[0] != want {
			t.Errorf("%s: %v", c.dir, got)
		}
	}
}

// TestPageScrollAtAPoint: @x,y inside the Page's document wheels over CDP
// at the inverse-transformed point; outside it, today's Headed wheel.
func TestPageScrollAtAPoint(t *testing.T) {
	r := newPageRig(t)
	r.svc.Shots.Record(browserWindowID, shot.Mapping{Scale: 1, Width: 2000, Height: 2000, Coverage: image.Rect(0, 0, 2000, 2000)})
	if _, e := r.run(t, "scroll", protocol.ScrollArgs{Window: "301", X: 400, Y: 500, ScrollY: 240}); e != nil {
		t.Fatal(e.Error())
	}
	// ((400 - 100) / 1.5, (500 - 200) / 1.5) = (200, 200); 240 units = 2 notches = 200 px.
	if got := r.sent(); !reflect.DeepEqual(got, []string{"dispatchMouseEvent mouseWheel x=200 y=200 deltaX=0 deltaY=200"}) {
		t.Errorf("sent %v", got)
	}
	r.noForeground(t)
	if got := strings.Join(r.esc.brackets(), ","); got != "arm-passive,disarm-passive" {
		t.Errorf("Esc scope = %s", got)
	}

	r.set(func() { r.inputs = nil })
	if _, e := r.run(t, "scroll", protocol.ScrollArgs{Window: "301", X: 50, Y: 50, ScrollY: 120}); e != nil {
		t.Fatal(e.Error())
	}
	if len(r.sent()) != 0 || len(r.scrolls) != 1 || r.scrolls[0] != [5]int32{browserWindowID, 50, 50, 0, 120} {
		t.Errorf("outside the Page: sent %v, wheel %v", r.sent(), r.scrolls)
	}
}

// TestPageKeys: typing and keys reach the Page when it holds the focus,
// newline as Return; with the focus on the body they keep today's route.
func TestPageKeys(t *testing.T) {
	r := newPageRig(t)
	if _, e := r.run(t, "type-text", protocol.TypeTextArgs{Window: "301", Text: "hi\nyo"}); e != nil {
		t.Fatal(e.Error())
	}
	want := []string{"insertText hi", "dispatchKeyEvent keyDown Enter \r", "dispatchKeyEvent keyUp Enter", "insertText yo"}
	if got := r.sent(); !reflect.DeepEqual(got, want) {
		t.Errorf("typed %q", got)
	}
	if r.cursor.labels[0] != "type" || r.cursor.targets[0] != (win.Rect{Left: 115, Top: 230, Right: 190, Bottom: 245}) {
		t.Errorf("cursor %q %v", r.cursor.labels, r.cursor.targets)
	}
	r.noForeground(t)

	r.set(func() { r.inputs = nil })
	if _, e := r.run(t, "press-key", protocol.PressKeyArgs{Window: "301", Key: "ctrl+a"}); e != nil {
		t.Fatal(e.Error())
	}
	want = []string{"dispatchKeyEvent rawKeyDown Control", "dispatchKeyEvent rawKeyDown a", "dispatchKeyEvent keyUp a", "dispatchKeyEvent keyUp Control"}
	if got := r.sent(); !reflect.DeepEqual(got, want) {
		t.Errorf("pressed %q", got)
	}

	for _, kind := range []string{"body", "none"} {
		r = newPageRig(t)
		r.set(func() { r.focus = map[string]any{"kind": kind} })
		if _, e := r.run(t, "type-text", protocol.TypeTextArgs{Window: "301", Text: "x"}); e != nil {
			t.Fatal(e.Error())
		}
		if len(r.sent()) != 0 || !strings.Contains(strings.Join(r.recorded(), ","), "activate,type-text") {
			t.Errorf("focus %s: sent %v, steps %v", kind, r.sent(), r.recorded())
		}
	}
	// Without a Page in the latest Window State: today's route, no probe.
	r = newPageRig(t)
	delete(r.trees.origins, browserWindowID)
	if _, e := r.run(t, "press-key", protocol.PressKeyArgs{Window: "301", Key: "Return"}); e != nil {
		t.Fatal(e.Error())
	}
	if len(r.sent()) != 0 || !strings.Contains(strings.Join(r.recorded(), ","), "press-key") {
		t.Errorf("no Page: %v %v", r.sent(), r.recorded())
	}
}

func TestPageHasFocus(t *testing.T) {
	for kind, want := range map[string]bool{"editable": true, "other": true, "frame": true, "body": false, "none": false, "": false} {
		if got := pageHasFocus(page.Focus{Kind: kind}); got != want {
			t.Errorf("%q: %v", kind, got)
		}
	}
}

// TestPageEscStops: a press during the cursor's travel stops before any
// input; a press between events stops what is left.
func TestPageEscStops(t *testing.T) {
	r := newPageRig(t)
	r.cursor.esc = r.esc
	r.esc.pressOn = "travel"
	_, e := r.run(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7)})
	if e == nil || e.Exit != protocol.ExitEsc || !strings.Contains(e.Hint, "nothing was sent") {
		t.Errorf("stop at travel: %+v", e)
	}
	if len(r.sent()) != 0 || len(r.cursor.clicks) != 0 {
		t.Errorf("sent %v", r.sent())
	}

	r = newPageRig(t)
	pressed := false
	r.srv.Handle("Input.dispatchMouseEvent", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		r.record("mouse")
		if !pressed {
			pressed = true
			r.esc.mu.Lock()
			r.esc.pressed = true
			r.esc.mu.Unlock()
		}
		return nil, nil
	})
	_, e = r.run(t, "click", protocol.ClickArgs{Window: "301", ElementIndex: idx(7)})
	if e == nil || e.Exit != protocol.ExitEsc || e.Hint != escStopHint {
		t.Errorf("stop between events: %+v", e)
	}
	if got := r.sent(); len(got) != 1 {
		t.Errorf("events after the stop: %v", got)
	}
}

// TestBatchPageStepsArePassive: Page steps count as Headless, so a Batch
// of them is one passive scope with no banner.
func TestBatchPageStepsArePassive(t *testing.T) {
	r := newPageRig(t)
	out, e := r.run(t, "batch", protocol.BatchArgs{Window: "301", Steps: []protocol.Step{
		stepOf("click", protocol.ClickArgs{ElementIndex: idx(7)}),
		stepOf("type-text", protocol.TypeTextArgs{Text: "x"}),
		stepOf("press-key", protocol.PressKeyArgs{Key: "Return"}),
		stepOf("scroll", protocol.ScrollArgs{ElementIndex: idx(7), Direction: "down"}),
	}})
	if e != nil || out != okOutput {
		t.Fatalf("batch: %q %+v", out, e)
	}
	if got := strings.Join(r.esc.brackets(), ","); strings.Contains(got, "arm,") || !strings.HasPrefix(got, "arm-passive") {
		t.Errorf("Esc scope = %s", got)
	}
	r.noForeground(t)

	// A UIA step that needs the foreground makes it a swallowing scope.
	r = newPageRig(t)
	if _, e := r.run(t, "batch", protocol.BatchArgs{Window: "301", Steps: []protocol.Step{
		stepOf("click", protocol.ClickArgs{ElementIndex: idx(7)}),
		stepOf("click", protocol.ClickArgs{ElementIndex: idx(2), ClickCount: 2}),
	}}); e != nil {
		t.Fatal(e.Error())
	}
	if got := strings.Join(r.esc.brackets(), ","); !strings.HasPrefix(got, "arm,") {
		t.Errorf("mixed Esc scope = %s", got)
	}
	// The step decision itself, per kind.
	s := r.svc
	w := browserWindow()
	for _, c := range []struct {
		kind string
		args any
		want bool
	}{
		{"click", protocol.ClickArgs{ElementIndex: idx(7), ClickCount: 3}, true},
		{"click", protocol.ClickArgs{ElementIndex: idx(7), Headed: true}, false},
		{"click", protocol.ClickArgs{ElementIndex: idx(2), ClickCount: 2}, false},
		{"set-value", protocol.SetValueArgs{ElementIndex: idx(8)}, true},
		{"press-key", protocol.PressKeyArgs{Key: "a"}, true},
		{"press-key", protocol.PressKeyArgs{Key: "a", Post: true}, false},
		{"hover", protocol.HoverArgs{X: 1, Y: 1}, false},
	} {
		raw, _ := json.Marshal(c.args)
		if got := s.pageStep(c.kind, raw, w); got != c.want {
			t.Errorf("%s %s: %v", c.kind, raw, got)
		}
	}
}
