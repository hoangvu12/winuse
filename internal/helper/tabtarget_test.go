package helper

// Offline tests for Tab targets (background-tabs, ADR 0017) through the
// fake winuse extension (bridgetest): the user looks at Inbox in window 1
// (Window 301) while agents read and act in Docs and Shop, Tabs of the same
// window in the background. Each Tab's Page is a small form answered by
// fakePage. The user's selected Tab never changes, nothing is activated, no
// real input is sent, and the Agent Cursor stays still. No browser runs.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/bridge"
	"github.com/hoangvu12/winuse/internal/bridge/bridgetest"
	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// The Tabs of window 1: the user's Inbox (shown), and two agents' Tabs.
const (
	bgInbox = "E1000000000000000000000000000001"
	bgDocs  = "E2000000000000000000000000000002"
	bgShop  = "E3000000000000000000000000000003"
)

// The Page's Elements in a Tab target's Window State: 0 is the document.
const (
	elSignIn = 1 // button, node 70
	elEmail  = 2 // edit, node 80
	elLink   = 3 // link "Open report", node 90: a click opens a new Tab
	elColour = 4 // <select>, node 100
	elWhen   = 5 // <input type=date>, node 110
)

// bgRig is the user's Chrome through the fake extension, with a fake Page
// in every Tab.
type bgRig struct {
	*tabRig
	ext *bridgetest.Extension

	mu       sync.Mutex
	calls    []string          // "<4 chars of the session> <method>[ <detail>]"
	selected map[string]int    // the <select>'s option per session
	date     map[string]string // the date input's value per session
	value    map[string]string // the text field's value per session
	gate     map[string]chan struct{}
	shot     func() (any, error)
}

func newBgRig(t *testing.T) *bgRig {
	t.Helper()
	r := &bgRig{selected: map[string]int{}, date: map[string]string{}, value: map[string]string{}, gate: map[string]chan struct{}{}}
	r.ext = &bridgetest.Extension{
		Tabs: []bridgetest.Tab{
			{TargetID: bgInbox, Title: "Inbox", URL: "https://mail.test/", TabID: 11, WindowID: 1, Active: true},
			{TargetID: bgDocs, Title: "Docs", URL: "https://docs.test/", TabID: 12, WindowID: 1, Group: bridgetest.TabGroup{Title: "claude", Color: "orange"}},
			{TargetID: bgShop, Title: "Shop", URL: "https://shop.test/", TabID: 13, WindowID: 1, Group: bridgetest.TabGroup{Title: "codex", Color: "green"}},
		},
		Windows: []bridgetest.Window{{WindowID: 1, Left: 100, Top: 60, Width: 1200, Height: 800, State: "normal"}},
		Page:    r.page,
	}
	bridgetest.Start(t, t.TempDir(), r.ext)
	bc, err := bridge.Dial(context.Background(), r.ext.Info.Addr(), r.ext.Info.Token)
	if err != nil {
		t.Fatal(err)
	}
	c := cdp.NewClient(bc)
	t.Cleanup(func() { c.Close() })
	conn := &browser.Connection{Browser: "chrome", Mode: browser.ModeExtension, PID: 4242, Client: c}
	r.tabRig = newTabRig(t, conn, nil, nil, &stripActions{})
	r.svc.Windows = func() ([]win.WindowInfo, error) {
		w := tabTestWindows[0]
		w.Title = "Inbox - Google Chrome"
		return []win.WindowInfo{w, tabTestWindows[3]}, nil
	}
	r.svc.Pages.(*livePages).reader.Settle = time.Millisecond
	r.svc.PageContent = func(uint64) (win.Rect, bool) { return win.Rect{Left: 100, Top: 160, Right: 1300, Bottom: 860}, true }
	r.svc.tabHolds = newTabHolds()
	r.cmds = CommandsWith(r.svc)
	return r
}

func (r *bgRig) record(s string) {
	r.mu.Lock()
	r.calls = append(r.calls, s)
	r.mu.Unlock()
}

// sent lists the recorded calls of one Tab whose method starts with prefix.
func (r *bgRig) sent(target, prefix string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, c := range r.calls {
		if strings.HasPrefix(c, strings.ToLower(target[:4])+" "+prefix) {
			out = append(out, c)
		}
	}
	return out
}

// as runs a command as an Agent.
func (r *bgRig) as(t *testing.T, id, cmd string, args any) (string, *Error) {
	t.Helper()
	return runAs(t, r.cmds, id, cmd, args)
}

// noUserImpact fails when anything reached the user: their Tab no longer
// shown, a Tab or window activated, real input, the cursor moved, or a
// foreground CDP method.
func (r *bgRig) noUserImpact(t *testing.T) {
	t.Helper()
	if got := r.ext.ActiveTab(1); got != bgInbox {
		t.Errorf("window 1 shows %s, not the user's Inbox", got)
	}
	if got := r.ext.Activated(); len(got) != 0 {
		t.Errorf("tabs activated: %v", got)
	}
	for _, s := range r.log() {
		if s == "activate" || strings.HasPrefix(s, "press ") || strings.HasPrefix(s, "cursor-") {
			t.Errorf("the user saw %q: %v", s, r.log())
		}
	}
	noForegroundCalls(t, extMethods(r.ext))
	for _, c := range r.sent(bgInbox, "") {
		t.Errorf("the user's Tab got %q", c)
	}
}

var markerRE = regexp.MustCompile(`/\* (winuse:[a-z]+) \*/`)

// page is the fake Pages: a form of a button, a text field, a link that
// opens a new Tab, a <select> and a date input.
func (r *bgRig) page(sid, method string, params json.RawMessage) (any, error) {
	var p map[string]any
	json.Unmarshal(params, &p)
	detail := ""
	if typ, ok := p["type"].(string); ok {
		detail = " " + typ
	}
	if m := markerRE.FindStringSubmatch(fmt.Sprint(p["functionDeclaration"])); m != nil {
		detail = " " + m[1]
	}
	if e, ok := p["enabled"].(bool); ok {
		detail = fmt.Sprintf(" %v", e)
	}
	if st, ok := p["state"].(string); ok {
		detail = " " + st
	}
	r.record(strings.ToLower(sid[:4]) + " " + method + detail)
	switch method {
	case "Page.getFrameTree":
		return map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "F" + sid, "loaderId": "L1"}}}, nil
	case "Accessibility.getFullAXTree":
		return map[string]any{"nodes": axForm(sid)}, nil
	case "DOM.resolveNode":
		return map[string]any{"object": map[string]any{"type": "object", "objectId": fmt.Sprintf("%s/%v", sid, p["backendNodeId"])}}, nil
	case "Runtime.callFunctionOn":
		return map[string]any{"result": map[string]any{"type": "object", "value": r.call(sid, p)}}, nil
	case "Runtime.evaluate":
		if strings.Contains(fmt.Sprint(p["expression"]), "winuse:focus") {
			return map[string]any{"result": map[string]any{"value": map[string]any{"kind": "editable", "x": 10, "y": 20, "w": 50, "h": 10, "dpr": 1}}}, nil
		}
		return map[string]any{"result": map[string]any{"type": "number", "value": 0}}, nil
	case "Input.dispatchMouseEvent":
		r.mu.Lock()
		gate := r.gate[sid]
		r.mu.Unlock()
		if gate != nil && p["type"] == "mousePressed" {
			<-gate
		}
		if p["type"] == "mouseReleased" && p["x"] == float64(90) {
			r.ext.OpenFromTab(sid, "https://report.test/", false) // target=_blank
		}
	case "Input.insertText":
		r.mu.Lock()
		r.value[sid] = fmt.Sprint(p["text"])
		r.mu.Unlock()
	case "Page.captureScreenshot":
		if r.shot != nil {
			return r.shot()
		}
	}
	return map[string]any{}, nil
}

// call answers Runtime.callFunctionOn by the function's winuse marker.
func (r *bgRig) call(sid string, p map[string]any) any {
	node := 0
	fmt.Sscanf(strings.TrimPrefix(fmt.Sprint(p["objectId"]), sid+"/"), "%d", &node)
	var arg any
	if args, ok := p["arguments"].([]any); ok && len(args) > 0 {
		if a, ok := args[0].(map[string]any); ok {
			arg = a["value"]
		}
	}
	m := markerRE.FindStringSubmatch(fmt.Sprint(p["functionDeclaration"]))
	if m == nil {
		return true // isConnected
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	switch m[1] {
	case "winuse:aim":
		return map[string]any{"state": "ok", "l": 10.0, "t": 20.0, "w": 50.0, "h": 10.0, "x": float64(node), "y": 25.0, "chain": true, "dpr": 1.0, "name": "page name"}
	case "winuse:nativepicker":
		switch node {
		case 100:
			return "list"
		case 110:
			return "date"
		}
		return ""
	case "winuse:choice":
		switch node {
		case 100:
			opts := []map[string]any{{"label": "Red", "value": "r"}, {"label": "Blue", "value": "b"}}
			opts[r.selected[sid]]["selected"] = true
			return map[string]any{"kind": "select", "options": opts}
		case 110:
			return map[string]any{"kind": "picker", "type": "date", "value": r.date[sid]}
		}
		return map[string]any{"kind": ""}
	case "winuse:pick":
		r.selected[sid] = int(arg.(float64))
		return true
	case "winuse:picker":
		r.date[sid] = fmt.Sprint(arg)
		return r.date[sid]
	case "winuse:field":
		return map[string]any{"kind": "text"}
	case "winuse:read":
		return r.value[sid]
	}
	return true
}

// axForm is a Tab's accessibility tree.
func axForm(sid string) []map[string]any {
	n := func(id, role, name string, backend int, kids ...string) map[string]any {
		out := map[string]any{"nodeId": id, "role": map[string]any{"type": "role", "value": role}, "name": map[string]any{"type": "computedString", "value": name},
			"backendDOMNodeId": backend, "frameId": "F" + sid}
		if len(kids) > 0 {
			out["childIds"] = kids
		}
		if id != "1" {
			out["parentId"] = "1"
		}
		return out
	}
	return []map[string]any{
		n("1", "RootWebArea", "Form", 1, "2", "3", "4", "5", "6"),
		n("2", "button", "Sign in", 70),
		n("3", "textbox", "Email", 80),
		n("4", "link", "Open report", 90),
		n("5", "combobox", "Colour", 100),
		// <input type=date>: its spinners and picker button are shadow parts.
		n("6", "date", "When", 110, "7", "8"),
		n("7", "spinButton", "Month", 111),
		n("8", "button", "Show date picker", 112),
	}
}

func TestSplitTabRef(t *testing.T) {
	for _, c := range []struct {
		ref, window, tab string
		ok               bool
	}{
		{"301:e2000000", "301", "e2000000", true},
		{"chrome:E200", "chrome", "E200", true},
		{"301", "", "", false},
		{"301:e2", "", "", false}, // too short to be a tab id
		{":e200", "", "", false},  // no Window
		{`C:\x`, "", "", false},   // not a tab id
		{"a:b:cafe", "a:b", "cafe", true},
	} {
		w, tab, ok := splitTabRef(c.ref)
		if w != c.window || tab != c.tab || ok != c.ok {
			t.Errorf("splitTabRef(%q) = %q, %q, %v", c.ref, w, tab, ok)
		}
	}
}

func TestRetargetNamesTheTab(t *testing.T) {
	bt := &bgTab{browserConn: browserConn{w: win.WindowInfo{ID: 301}}}
	bt.tab.TargetID = bgDocs
	for in, want := range map[string]string{
		"window 301 shows a JavaScript dialog":                      "tab e2000000 of window 301 shows a JavaScript dialog",
		"the active Tab of window 301 shows a browser page":         "tab e2000000 of window 301 shows a browser page",
		"run winuse get-window-state 301 again":                     "run winuse get-window-state 301:e2000000 again",
		"run winuse dialog 301 accept or winuse dialog 301 dismiss": "run winuse dialog 301:e2000000 accept or winuse dialog 301:e2000000 dismiss",
		"winuse tab list 301; winuse browser status chrome":         "winuse tab list 301; winuse browser status chrome",
		"window 3012 is another":                                    "window 3012 is another",
		"read winuse get-window-state 301.":                         "read winuse get-window-state 301:e2000000.",
	} {
		if got := bt.retarget(in); got != want {
			t.Errorf("retarget(%q) =\n %q, want\n %q", in, got, want)
		}
	}
}

// TestTabTargetReadsABackgroundTab: the Tab's Page under a document
// Element, read over CDP alone, held (focus emulation, active lifecycle,
// no discarding), while the user's Tab is untouched.
func TestTabTargetReadsABackgroundTab(t *testing.T) {
	r := newBgRig(t)
	out, e := r.as(t, "claude", "get-window-state", protocol.GetWindowArgs{Window: "301:e200"})
	if e != nil {
		t.Fatalf("get-window-state: %+v", e)
	}
	for _, want := range []string{
		`Window: "Docs", App: chrome.exe.`,
		"\t0 document Docs Value: https://docs.test/",
		"\t\t1 button Sign in",
		"\t\t2 edit (settable, string) Email",
		"\t\t3 link Open report",
		"\t\t4 combo box Colour",
		"\t\t5 edit (settable, string) When\n",
		"Note: this is tab e2000000, in the background of window 301, read through its Browser Connection alone: act on it as 301:e2000000",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the Window State lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "spinner") || strings.Contains(out, "Show date picker") {
		t.Errorf("the date input's shadow parts are lines:\n%s", out)
	}
	if !r.ext.Held(bgDocs) || r.ext.Held(bgInbox) || r.ext.Held(bgShop) {
		t.Errorf("holds = %v", r.ext.Holds())
	}
	if got := r.sent(bgDocs, "Emulation.setFocusEmulationEnabled"); len(got) == 0 || got[len(got)-1] != "e200 Emulation.setFocusEmulationEnabled true" {
		t.Errorf("focus emulation: %v", got)
	}
	if got := r.sent(bgDocs, "Page.setWebLifecycleState"); len(got) != 1 || got[0] != "e200 Page.setWebLifecycleState active" {
		t.Errorf("lifecycle: %v", got)
	}
	r.noUserImpact(t)

	// The Window's own Window State is not the Tab's: its indexes stay the
	// Window's, and another Agent has not read the Tab.
	_, e = r.as(t, "codex", "click", protocol.ClickArgs{Window: "301:e200", ElementIndex: idx(elSignIn)})
	if e == nil || e.Msg != "no cached Window State is available for tab e2000000 of window 301; nothing was sent" ||
		e.Hint != "run winuse get-window-state 301:e2000000 to read the Tab's Elements before acting on them" {
		t.Errorf("another Agent: %+v", e)
	}
	if got := r.sent(bgDocs, "Input."); len(got) != 0 {
		t.Errorf("input sent: %v", got)
	}
}

// TestTabTargetActsWithoutTouchingTheUsersTab: every Action reaches the
// background Tab over CDP, and the user's Tab, the foreground and the
// pointer stay as they are.
func TestTabTargetActsWithoutTouchingTheUsersTab(t *testing.T) {
	r := newBgRig(t)
	if _, e := r.as(t, "claude", "get-window-state", protocol.GetWindowArgs{Window: "301:e200"}); e != nil {
		t.Fatal(e.Msg)
	}
	steps := []struct {
		cmd  string
		args any
	}{
		{"click", protocol.ClickArgs{Window: "301:e200", ElementIndex: idx(elSignIn)}},
		{"click", protocol.ClickArgs{Window: "301:e200", Ref: "name:Sign in", ClickCount: 2}},
		{"set-value", protocol.SetValueArgs{Window: "301:e200", ElementIndex: idx(elEmail), Value: "a@b.c"}},
		{"type-text", protocol.TypeTextArgs{Window: "301:e200", Text: "hello"}},
		{"type-text", protocol.TypeTextArgs{Window: "301:e200", Text: "x", IntoIndex: idx(elEmail)}},
		{"press-key", protocol.PressKeyArgs{Window: "301:e200", Key: "Return"}},
		{"scroll", protocol.ScrollArgs{Window: "301:e200", ElementIndex: idx(elSignIn), Direction: "down"}},
		{"hover", protocol.HoverArgs{Window: "301:e200", ElementIndex: idx(elSignIn)}},
		{"drag", protocol.DragArgs{Window: "301:e200", FromIndex: idx(elSignIn), ToIndex: idx(elEmail)}},
		{"eval", protocol.EvalArgs{Window: "301:e200", Expression: "1"}},
	}
	for _, st := range steps {
		if out, e := r.as(t, "claude", st.cmd, st.args); e != nil || !strings.HasPrefix(out, "ok") && st.cmd != "eval" {
			t.Errorf("%s: %q, %+v", st.cmd, out, e)
		}
	}
	if got := r.sent(bgDocs, "Input."); len(got) < 10 {
		t.Errorf("the Tab got too little input: %v", got)
	}
	r.noUserImpact(t)
	for _, b := range r.esc.brackets() {
		if b != "arm-passive" && b != "disarm-passive" {
			t.Errorf("a swallowing Esc scope: %v", r.esc.brackets())
			break
		}
	}

	// A Batch on the Tab target runs every step there, headless.
	out, e := r.as(t, "claude", "batch", protocol.BatchArgs{Window: "301:e200", Steps: []protocol.Step{
		{Kind: "click", Args: json.RawMessage(`{"element_index":1}`)},
		{Kind: "press-key", Args: json.RawMessage(`{"key":"Tab"}`)},
	}})
	if e != nil || !strings.Contains(out, "Action 1 (click): ok") || !strings.Contains(out, "Action 2 (press-key): ok") {
		t.Errorf("batch: %q, %+v", out, e)
	}
	r.noUserImpact(t)
}

// TestTabTargetLinkOpensANewTabBesideTheUsersTab: a target=_blank link
// clicked in a background Tab opens a Tab that the extension keeps from
// taking the user's place; it joins the agent's group, and the Action's
// note names it on the Tab target.
func TestTabTargetLinkOpensANewTabBesideTheUsersTab(t *testing.T) {
	r := newBgRig(t)
	if _, e := r.as(t, "claude", "get-window-state", protocol.GetWindowArgs{Window: "301:e200"}); e != nil {
		t.Fatal(e.Msg)
	}
	out, e := r.as(t, "claude", "click", protocol.ClickArgs{Window: "301:e200", ElementIndex: idx(elLink)})
	if e != nil || !strings.HasPrefix(out, "ok\nNote: a new Tab opened: opened4 \"https://report.test/\" https://report.test/") {
		t.Fatalf("click = %q, %+v", out, e)
	}
	if g := r.ext.Group("OPENED4"); g.Title != "claude" || g.Color != "orange" {
		t.Errorf("the new Tab's group = %+v", g)
	}
	r.noUserImpact(t)

	// The new Tab is a Tab target of the same Window.
	if _, e := r.as(t, "claude", "get-window-state", protocol.GetWindowArgs{Window: "301:opened4"}); e != nil {
		t.Errorf("reading the new Tab: %+v", e)
	}
	r.noUserImpact(t)
}

// TestTabTargetLinkWithoutAnOpener (live check 3 of background-tabs): a
// target=_blank link is noopener, and the browser may give its Tab no
// openerTabId; the extension takes it by the held Tab acting in its
// window, so the user's Tab stays selected and the new Tab joins the
// agent's group.
func TestTabTargetLinkWithoutAnOpener(t *testing.T) {
	r := newBgRig(t)
	r.ext.NoOpener = true
	if _, e := r.as(t, "claude", "get-window-state", protocol.GetWindowArgs{Window: "301:e200"}); e != nil {
		t.Fatal(e.Msg)
	}
	out, e := r.as(t, "claude", "click", protocol.ClickArgs{Window: "301:e200", ElementIndex: idx(elLink)})
	if e != nil || !strings.Contains(out, "Note: a new Tab opened: opened4") {
		t.Fatalf("click = %q, %+v", out, e)
	}
	if g := r.ext.Group("OPENED4"); g.Title != "claude" {
		t.Errorf("group = %+v", g)
	}
	r.noUserImpact(t)
}

// TestUsersTabComesBackWhenTheGuardMisses: an extension whose guard did
// not fire (an older build, a worker that lost its holds) leaves the new
// Tab selected; winuse's own check selects the user's Tab again within a
// moment, and the new Tab joins the agent's group.
func TestUsersTabComesBackWhenTheGuardMisses(t *testing.T) {
	oldPoll := usersTabPoll
	usersTabPoll = 10 * time.Millisecond
	t.Cleanup(func() { usersTabPoll = oldPoll })
	r := newBgRig(t)
	r.ext.GuardOff = true
	if _, e := r.as(t, "claude", "get-window-state", protocol.GetWindowArgs{Window: "301:e200"}); e != nil {
		t.Fatal(e.Msg)
	}
	if _, e := r.as(t, "claude", "click", protocol.ClickArgs{Window: "301:e200", ElementIndex: idx(elLink)}); e != nil {
		t.Fatal(e.Msg)
	}
	deadline := time.Now().Add(3 * time.Second)
	for r.ext.ActiveTab(1) != bgInbox && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := r.ext.ActiveTab(1); got != bgInbox {
		t.Fatalf("window 1 shows %s, not the user's Inbox", got)
	}
	if got := r.ext.Activated(); len(got) != 1 || got[0] != bgInbox {
		t.Errorf("activated %v, want the user's Tab once", got)
	}
	if g := r.ext.Group("OPENED4"); g.Title != "claude" {
		t.Errorf("group = %+v", g)
	}
}

// TestTheUsersOwnSwitchIsLeftAlone: when the user moves to another Tab
// themselves while winuse watches, winuse does not move them back.
func TestTheUsersOwnSwitchIsLeftAlone(t *testing.T) {
	oldPoll := usersTabPoll
	usersTabPoll = 200 * time.Millisecond
	t.Cleanup(func() { usersTabPoll = oldPoll })
	r := newBgRig(t)
	r.ext.GuardOff = true
	if _, e := r.as(t, "claude", "get-window-state", protocol.GetWindowArgs{Window: "301:e200"}); e != nil {
		t.Fatal(e.Msg)
	}
	if _, e := r.as(t, "claude", "click", protocol.ClickArgs{Window: "301:e200", ElementIndex: idx(elLink)}); e != nil {
		t.Fatal(e.Msg)
	}
	r.ext.Select(bgShop) // the user clicks Shop
	time.Sleep(700 * time.Millisecond)
	if got := r.ext.ActiveTab(1); got != bgShop {
		t.Errorf("window 1 shows %s, not the Tab the user chose", got)
	}
	if got := r.ext.Activated(); len(got) != 0 {
		t.Errorf("winuse switched Tabs: %v", got)
	}
}

// TestTheUsersOwnLinksAreNotGuarded: a Tab the user is looking at, held
// once by an agent, still lets the user follow a link into a new Tab.
func TestTheUsersOwnLinksAreNotGuarded(t *testing.T) {
	r := newBgRig(t)
	if _, e := r.as(t, "claude", "get-window-state", protocol.GetWindowArgs{Window: "301:e100"}); e != nil {
		t.Fatal(e.Msg)
	}
	if !r.ext.Held(bgInbox) {
		t.Fatalf("holds = %v", r.ext.Holds())
	}
	id := r.ext.OpenFromTab(bgInbox, "https://user.test/", false)
	if got := r.ext.ActiveTab(1); got != id {
		t.Errorf("window 1 shows %s, not the Tab the user opened (%s)", got, id)
	}
}

// TestTabTargetNativePopups: a click on a <select> or a date input of a
// background Tab is refused (its popup would show on the user's screen),
// and set-value sets them through the DOM.
func TestTabTargetNativePopups(t *testing.T) {
	r := newBgRig(t)
	if _, e := r.as(t, "claude", "get-window-state", protocol.GetWindowArgs{Window: "301:e200"}); e != nil {
		t.Fatal(e.Msg)
	}
	_, e := r.as(t, "claude", "click", protocol.ClickArgs{Window: "301:e200", ElementIndex: idx(elColour)})
	if e == nil || e.Msg != "element 4 of tab e2000000 is a drop-down list, and a click would open its popup on the user's screen while the Tab is in the background; nothing was sent" ||
		e.Hint != "set it instead: winuse set-value 301:e2000000 4 <option label or value>" {
		t.Errorf("select click: %+v", e)
	}
	_, e = r.as(t, "claude", "click", protocol.ClickArgs{Window: "301:e200", ElementIndex: idx(elWhen)})
	if e == nil || !strings.Contains(e.Msg, "is a date input, and a click would open its popup") {
		t.Errorf("date click: %+v", e)
	}
	if got := r.sent(bgDocs, "Input.dispatchMouseEvent"); len(got) != 0 {
		t.Errorf("a refused click sent %v", got)
	}
	if out, e := r.as(t, "claude", "set-value", protocol.SetValueArgs{Window: "301:e200", ElementIndex: idx(elColour), Value: "Blue"}); e != nil || out != okOutput {
		t.Errorf("set-value select: %q, %+v", out, e)
	}
	if out, e := r.as(t, "claude", "set-value", protocol.SetValueArgs{Window: "301:e200", ElementIndex: idx(elWhen), Value: "2026-11-04"}); e != nil || out != okOutput {
		t.Errorf("set-value date: %q, %+v", out, e)
	}
	r.mu.Lock()
	sel, date := r.selected[bgDocs], r.date[bgDocs]
	r.mu.Unlock()
	if sel != 1 || date != "2026-11-04" {
		t.Errorf("select %d, date %q", sel, date)
	}
	if got := r.sent(bgDocs, "Input.dispatchMouseEvent"); len(got) != 0 {
		t.Errorf("set-value clicked: %v", got)
	}
	r.noUserImpact(t)
}

// TestTabTargetRefusals: what a background Tab cannot take is refused
// before anything is resolved, held or sent.
func TestTabTargetRefusals(t *testing.T) {
	r := newBgRig(t)
	for _, c := range []struct {
		cmd  string
		args any
		msg  string
	}{
		{"click", protocol.ClickArgs{Window: "301:e200", ElementIndex: idx(1), Headed: true}, "--headed does not reach a Tab target (301:e200)"},
		{"click", protocol.ClickArgs{Window: "301:e200", Point: &protocol.Point{X: 5, Y: 5}}, "click at an @x,y point does not take a Tab target (301:e200)"},
		{"click", protocol.ClickArgs{Window: "301:e200", ElementIndex: idx(1), Post: true}, "--post does not reach a Tab target (301:e200)"},
		{"scroll", protocol.ScrollArgs{Window: "301:e200", X: 4, Y: 4, ScrollY: 120}, "scroll at an @x,y point does not take a Tab target"},
		{"perform-secondary-action", protocol.SecondaryActionArgs{Window: "301:e200", ElementIndex: idx(1), Action: "Toggle"}, "perform-secondary-action does not take a Tab target (301:e200)"},
		{"find", map[string]any{"window": "301:e200", "name": "x"}, "find does not take a Tab target"},
		{"activate-window", protocol.GetWindowArgs{Window: "301:e200"}, "activate-window does not take a Tab target"},
		{"wait", protocol.WaitArgs{Window: "301:e200", For: "enabled"}, "wait on a Tab target (301:e200) waits for the Page"},
		{"batch", protocol.BatchArgs{Window: "301:e200", Steps: []protocol.Step{{Kind: "click", Window: "302", Args: json.RawMessage(`{"element_index":1}`)}}}, "Action 1 of a Batch on a Tab target names its own Window"},
		{"batch", protocol.BatchArgs{Window: "301:e200", Steps: []protocol.Step{{Kind: "click", Args: json.RawMessage(`{"element_index":1,"headed":true}`)}}}, "--headed does not reach a Tab target"},
		{"click", protocol.ClickArgs{Window: "301:ffff", ElementIndex: idx(1)}, `no tab has the id "ffff" in the Chrome of window 301`},
		{"click", protocol.ClickArgs{Window: "301:301:e200", ElementIndex: idx(1)}, "301:301:e200 names more than one Window: a Tab target is one Window and one tab id, <window>:<tab>"},
	} {
		_, e := r.as(t, "claude", c.cmd, c.args)
		if e == nil || !strings.HasPrefix(e.Msg, c.msg) || !strings.HasSuffix(e.Msg, nothingSentNote) {
			t.Errorf("%s %+v: %+v", c.cmd, c.args, e)
		}
	}
	_, e := r.as(t, "claude", "get-window-state", map[string]any{"window": "301:e200", "root": "1"})
	if e == nil || !strings.Contains(e.Msg, "get-window-state reads a Tab target whole") {
		t.Errorf("--root: %+v", e)
	}
	if got := r.ext.Holds(); len(got) != 0 {
		t.Errorf("a refusal held a Tab: %v", got)
	}
	r.noUserImpact(t)
}

// TestTabTargetTakesTheOutputBudgets: the views shape a Tab target's
// read like a Window's (speed 05); only --root and --skeleton,
// which read UI Automation, are refused.
func TestTabTargetTakesTheOutputBudgets(t *testing.T) {
	r := newBgRig(t)
	whole, e := r.as(t, "claude", "get-window-state", protocol.GetWindowArgs{Window: "301:e200"})
	if e != nil {
		t.Fatal(e.Msg)
	}
	out, e := r.as(t, "claude", "get-window-state", map[string]any{"window": "301:e200", "interactive": true})
	if e != nil || out != whole {
		t.Errorf("--interactive leaves nothing out of an all-interactive Page: %+v\n%s\nwhole:\n%s", e, out, whole)
	}
	out, e = r.as(t, "claude", "get-window-state", map[string]any{"window": "301:e200", "max_chars": 560})
	if e != nil || len(out) > 560 || !strings.Contains(out, "\t\t1 button Sign in\n") ||
		!strings.Contains(out, "are not printed; their Element Indexes still work. Narrow the read with --interactive, --compact or --depth, or write it all with --out <file>\n") {
		t.Errorf("--max-chars 560 (%d characters), with no --root hint: %+v\n%s", len(out), e, out)
	}
}

// TestTabTargetsTakeALaneEach: two Agents in two background Tabs of one
// window run at once; each Tab has its own lane.
func TestTabTargetsTakeALaneEach(t *testing.T) {
	r := newBgRig(t)
	for _, tab := range []string{"301:e200", "301:e300"} {
		if _, e := r.as(t, "claude", "get-window-state", protocol.GetWindowArgs{Window: tab}); e != nil {
			t.Fatal(e.Msg)
		}
	}
	gate := make(chan struct{})
	r.mu.Lock()
	r.gate[bgDocs] = gate
	r.mu.Unlock()
	done := make(chan *Error, 1)
	go func() {
		_, e := r.as(t, "claude", "click", protocol.ClickArgs{Window: "301:e200", ElementIndex: idx(elSignIn)})
		done <- e
	}()
	// The first click holds its Tab's lane, blocked on its press.
	deadline := time.Now().Add(3 * time.Second)
	for len(r.sent(bgDocs, "Input.dispatchMouseEvent mousePressed")) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if _, e := r.as(t, "claude", "click", protocol.ClickArgs{Window: "301:e300", ElementIndex: idx(elSignIn)}); e != nil {
		t.Errorf("the other Tab waited or failed: %+v", e)
	}
	close(gate)
	if e := <-done; e != nil {
		t.Errorf("the first click: %+v", e)
	}
	r.noUserImpact(t)

	// The lanes themselves.
	ctx := withTabTarget(context.Background(), &bgTab{tab: page.Tab{TargetID: bgDocs}})
	if set := r.svc.lanesFor(ctx, "click", json.RawMessage(`{"window":"301"}`)); len(set.keys) != 1 || set.keys[0] != tabLane(bgDocs) {
		t.Errorf("lanes = %v", set.keys)
	}
}

// TestTabTargetScreenshot: a capture of the background Tab, or, when it
// does not come in time, the tree with a note.
func TestTabTargetScreenshot(t *testing.T) {
	r := newBgRig(t)
	var buf bytes.Buffer
	jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 40, 30)), nil)
	r.shot = func() (any, error) {
		return map[string]any{"data": base64.StdEncoding.EncodeToString(buf.Bytes())}, nil
	}
	out, e := r.as(t, "claude", "get-window-state", map[string]any{"window": "301:e200", "screenshot": true})
	if e != nil || !regexp.MustCompile(`(?m)^Screenshot: .*tab-e2000000.*\.jpg \(40x30 px, scale 1, tab e2000000; @x,y points do not apply to a Tab target\)$`).MatchString(out) {
		t.Errorf("screenshot: %q, %+v", out, e)
	}

	old := tabShotWait
	tabShotWait = 50 * time.Millisecond
	t.Cleanup(func() { tabShotWait = old })
	r.shot = func() (any, error) {
		time.Sleep(time.Second)
		return map[string]any{}, nil
	}
	out, e = r.as(t, "claude", "get-window-state", map[string]any{"window": "301:e200", "screenshot": true})
	if e != nil || !strings.Contains(out, "\t\t1 button Sign in") ||
		!strings.Contains(out, "Note: no screenshot of tab e2000000: the browser gave no frame within 50ms, as a hidden Tab sometimes does; the tree above is current") {
		t.Errorf("hung capture: %q, %+v", out, e)
	}
	r.noUserImpact(t)
}

// TestTabHoldEndsWhenIdle: a hold ends after the Agent's last command on
// the Tab: focus emulation off and the extension's release.
func TestTabHoldEndsWhenIdle(t *testing.T) {
	r := newBgRig(t)
	r.svc.tabHolds.idle = 50 * time.Millisecond
	if _, e := r.as(t, "claude", "get-window-state", protocol.GetWindowArgs{Window: "301:e200"}); e != nil {
		t.Fatal(e.Msg)
	}
	deadline := time.Now().Add(3 * time.Second)
	for r.ext.Held(bgDocs) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if r.ext.Held(bgDocs) {
		t.Fatalf("still held: %v", r.ext.Holds())
	}
	if got := r.sent(bgDocs, "Emulation.setFocusEmulationEnabled"); got[len(got)-1] != "e200 Emulation.setFocusEmulationEnabled false" {
		t.Errorf("focus emulation: %v", got)
	}
	if len(r.svc.tabHolds.heldTabs()) != 0 {
		t.Errorf("the helper still holds %v", r.svc.tabHolds.heldTabs())
	}
	// Coming back holds it again.
	if _, e := r.as(t, "claude", "get-window-state", protocol.GetWindowArgs{Window: "301:e200"}); e != nil || !r.ext.Held(bgDocs) {
		t.Errorf("held again: %v, %+v", r.ext.Holds(), e)
	}
}

// TestTabCommandsTakeATabTargetsWindow: tab list on a Tab target lists its
// Window's Tabs, as on the Window.
func TestTabCommandsTakeATabTargetsWindow(t *testing.T) {
	r := newBgRig(t)
	out, e := r.as(t, "claude", "tab-list", protocol.TabArgs{Window: "301:e200"})
	if e != nil || !strings.Contains(out, "* e1000000 \"Inbox\"") || !strings.Contains(out, "  e2000000 \"Docs\"") {
		t.Errorf("tab list = %q, %+v", out, e)
	}
	r.noUserImpact(t)
}
