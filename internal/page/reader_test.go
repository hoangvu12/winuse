package page

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/bridge"
	"github.com/hoangvu12/winuse/internal/bridge/bridgetest"
	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// fakeChrome is a fake DevTools endpoint serving one Tab, the checkout
// page of cross-origin-iframe.json, with its payment iframe as an
// auto-attached child session. No browser is started.
type fakeChrome struct {
	srv  *cdptest.Server
	raw  *Raw
	mu   sync.Mutex
	gone int // answer this many Tab-session calls with "session not found"
	// props is the cursor-pointer pass's array per session, as
	// Runtime.getProperties returns it, and nodes describeNode's answers.
	props map[string][]map[string]any
	nodes map[string]int64
}

func newFakeChrome(t *testing.T) *fakeChrome {
	t.Helper()
	f := &fakeChrome{srv: cdptest.NewServer(), raw: loadRaw(t, "cross-origin-iframe"), props: map[string][]map[string]any{}, nodes: map[string]int64{}}
	t.Cleanup(f.srv.Close)
	s := f.srv
	s.Targets = []cdptest.Target{
		{TargetID: "T3", Type: "page", Title: "Checkout", URL: "https://shop.test/checkout"},
		{TargetID: "T9", Type: "page", Title: "Checkout", URL: "https://shop.test/checkout?old"}, // a background Tab in another window
		{TargetID: "X1", Type: "page", Title: "New Tab", URL: "chrome://newtab/"},
		{TargetID: "F2", Type: "iframe", Title: "", URL: "https://pay.test/"},
	}
	s.Handle("Browser.getWindowForTarget", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p struct {
			TargetID string `json:"targetId"`
		}
		json.Unmarshal(m.Params, &p)
		b := map[string]any{"left": 100, "top": 50, "width": 1200, "height": 800, "windowState": "normal"}
		id := 1
		if p.TargetID == "T9" {
			b = map[string]any{"left": 22, "top": 22, "width": 1050, "height": 700, "windowState": "normal"}
			id = 2
		}
		return map[string]any{"windowId": id, "bounds": b}, nil
	})
	s.Handle("Target.setAutoAttach", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		// Chrome reports the iframes already there before it answers.
		c.Event(m.SessionID, "Target.attachedToTarget", map[string]any{
			"sessionId":          "CHILD1",
			"targetInfo":         map[string]any{"targetId": "F2", "type": "iframe", "url": "https://pay.test/"},
			"waitingForDebugger": false,
		})
		return nil, nil
	})
	s.Handle("Page.getFrameTree", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		if err := f.maybeGone(m); err != nil {
			return nil, err
		}
		if m.SessionID == "CHILD1" {
			return map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "F2", "loaderId": "L2"}}}, nil
		}
		return map[string]any{"frameTree": map[string]any{
			"frame": map[string]any{"id": "F1", "loaderId": "L1"},
			"childFrames": []any{
				map[string]any{"frame": map[string]any{"id": "F2", "loaderId": "L2"}},
				map[string]any{"frame": map[string]any{"id": "F3", "loaderId": "L3"}},
			},
		}}, nil
	})
	s.Handle("Accessibility.getFullAXTree", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		if m.SessionID == "CHILD1" {
			return map[string]any{"nodes": f.raw.Frames[0].Nodes}, nil
		}
		return map[string]any{"nodes": f.raw.Main.Nodes}, nil
	})
	s.Handle("Runtime.evaluate", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		return map[string]any{"result": map[string]any{"type": "object", "subtype": "array", "objectId": "marks-" + m.SessionID}}, nil
	})
	s.Handle("Runtime.getProperties", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return map[string]any{"result": f.props[m.SessionID]}, nil
	})
	s.Handle("DOM.describeNode", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p struct {
			ObjectID string `json:"objectId"`
		}
		json.Unmarshal(m.Params, &p)
		f.mu.Lock()
		id, ok := f.nodes[p.ObjectID]
		f.mu.Unlock()
		if !ok {
			return nil, &cdptest.ReplyError{Code: -32000, Message: "Could not find node with given id"}
		}
		return map[string]any{"node": map[string]any{"backendNodeId": id}}, nil
	})
	s.Handle("DOM.getFrameOwner", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p struct {
			FrameID string `json:"frameId"`
		}
		json.Unmarshal(m.Params, &p)
		owners := map[string]int64{"F2": 300, "F3": 310}
		return map[string]any{"backendNodeId": owners[p.FrameID]}, nil
	})
	return f
}

// maybeGone answers the Tab session's call as a detached session while
// f.gone lasts.
func (f *fakeChrome) maybeGone(m cdptest.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gone > 0 && m.SessionID != "CHILD1" {
		f.gone--
		return &cdptest.ReplyError{Code: -32001, Message: "Session with given id not found."}
	}
	return nil
}

// propEntry is one Runtime.getProperties entry.
func propEntry(name string, value map[string]any) map[string]any {
	return map[string]any{"name": name, "value": value}
}

func (f *fakeChrome) connection(t *testing.T) *browser.Connection {
	t.Helper()
	ws, err := cdp.DialWS(context.Background(), f.srv.URL(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c := cdp.NewClient(ws)
	t.Cleanup(func() { c.Close() })
	return &browser.Connection{Browser: "chrome", Mode: browser.ModeLaunch, PID: 4242, Client: c}
}

func (f *fakeChrome) count(method string) int {
	n := 0
	for _, m := range f.srv.Methods() {
		if m == method {
			n++
		}
	}
	return n
}

// checkoutWindow is the Window showing T3: its title and its bounds at
// 150%.
func checkoutWindow() Geometry {
	return Geometry{Title: "Checkout - Google Chrome", DPI: 144, Rect: Box{Left: 150, Top: 75, Right: 1950, Bottom: 1275}}
}

func testReader() *Reader {
	r := NewReader()
	r.Settle = 50 * time.Millisecond
	return r
}

// TestReadRendersTheGoldenPage: the live read over a fake endpoint gives
// the same Window State as the recorded fixture, iframe included.
func TestReadRendersTheGoldenPage(t *testing.T) {
	f := newFakeChrome(t)
	conn := f.connection(t)
	raw, err := testReader().Read(context.Background(), conn, checkoutWindow())
	if err != nil {
		t.Fatal(err)
	}
	if raw.TargetID != "T3" || raw.Main.SessionID != "session-T3" || raw.Client != conn.Client {
		t.Fatalf("read %s over %s", raw.TargetID, raw.Main.SessionID)
	}
	if len(raw.Frames) != 1 || raw.Frames[0].Owner != 300 || raw.Frames[0].SessionID != "CHILD1" || raw.Frames[0].Loaders["F2"] != "L2" {
		t.Fatalf("frames = %+v", raw.Frames)
	}
	// The other Tabs' URLs, for telling a stale Tab's document (DocRule);
	// the iframe target is no Tab.
	if strings.Join(raw.Others, " ") != "https://shop.test/checkout?old chrome://newtab/" {
		t.Errorf("others = %q", raw.Others)
	}
	m, err := Merge(frameFor(t, raw.Title), raw)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "cross-origin-iframe.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got := wstate.Render(m.Tree); got != string(want) {
		t.Errorf("live read differs from the golden:\n%s", got)
	}
	// Ticket 01's attach: focus emulation on the Tab's session. And the
	// same-origin iframe already in the main tree is not read again.
	if f.count("Emulation.setFocusEmulationEnabled") != 1 {
		t.Error("focus emulation was not turned on")
	}
	for _, msg := range f.srv.Received() {
		if msg.Method == "Accessibility.getFullAXTree" && strings.Contains(string(msg.Params), "F3") {
			t.Error("the same-origin iframe was read on its own")
		}
		if msg.Method == "Page.bringToFront" || msg.Method == "Target.activateTarget" {
			t.Errorf("%s would take the foreground", msg.Method)
		}
	}
}

func TestReadReusesTheTabSession(t *testing.T) {
	f := newFakeChrome(t)
	conn := f.connection(t)
	r := testReader()
	for i := 0; i < 3; i++ {
		if _, err := r.Read(context.Background(), conn, checkoutWindow()); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.count("Target.attachToTarget"); n != 1 {
		t.Errorf("%d attaches for three reads, want 1", n)
	}
	if n := f.count("Target.setAutoAttach"); n != 1 {
		t.Errorf("%d setAutoAttach for three reads, want 1", n)
	}
}

func TestReadAttachesAgainAfterADetach(t *testing.T) {
	f := newFakeChrome(t)
	conn := f.connection(t)
	r := testReader()
	if _, err := r.Read(context.Background(), conn, checkoutWindow()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.gone = 1
	f.mu.Unlock()
	if _, err := r.Read(context.Background(), conn, checkoutWindow()); err != nil {
		t.Fatalf("a detached session must be attached again: %v", err)
	}
	if n := f.count("Target.attachToTarget"); n != 2 {
		t.Errorf("%d attaches, want 2", n)
	}
}

func TestReadNoMatchingTab(t *testing.T) {
	f := newFakeChrome(t)
	g := checkoutWindow()
	g.Title = "Elsewhere - Google Chrome"
	_, err := testReader().Read(context.Background(), f.connection(t), g)
	var none *NoTabError
	if !errors.As(err, &none) {
		t.Fatalf("err = %v", err)
	}
	if f.count("Target.attachToTarget") != 0 {
		t.Error("nothing may be attached without a match")
	}
}

// TestReadBrowserPageTab: a Window that shows the New Tab page maps to its
// Tab, marked Internal, and the read refuses it without attaching.
func TestReadBrowserPageTab(t *testing.T) {
	f := newFakeChrome(t)
	conn := f.connection(t)
	g := checkoutWindow()
	g.Title = "New Tab - Google Chrome"
	tab, err := FindTab(context.Background(), conn, g)
	if err != nil || tab.TargetID != "X1" || !tab.Internal || tab.URL != "chrome://newtab/" {
		t.Fatalf("FindTab = %+v, %v", tab, err)
	}
	_, err = testReader().Read(context.Background(), conn, g)
	var internal *InternalPageError
	if !errors.As(err, &internal) || internal.URL != "chrome://newtab/" {
		t.Fatalf("err = %v", err)
	}
	if got := Reason(err); got != "the active Tab shows a browser page (chrome://newtab/), which winuse does not read or act in" {
		t.Errorf("reason = %q", got)
	}
	if f.count("Target.attachToTarget") != 0 {
		t.Error("a browser page was attached")
	}
}

// TestReadMarks: the cursor-pointer pass's elements become password and
// clickable backendNodeIds.
func TestReadMarks(t *testing.T) {
	f := newFakeChrome(t)
	f.props["session-T3"] = []map[string]any{
		propEntry("0", map[string]any{"type": "object", "subtype": "node", "objectId": "el-pw"}),
		propEntry("1", map[string]any{"type": "string", "value": "p"}),
		propEntry("2", map[string]any{"type": "string", "value": ""}),
		propEntry("3", map[string]any{"type": "object", "subtype": "node", "objectId": "el-div"}),
		propEntry("4", map[string]any{"type": "string", "value": "c"}),
		propEntry("5", map[string]any{"type": "string", "value": "Open settings"}),
		propEntry("length", map[string]any{"type": "number", "value": 6}),
		propEntry("__proto__", map[string]any{"type": "object", "objectId": "proto"}),
	}
	f.nodes["el-pw"] = 117
	f.nodes["el-div"] = 210
	raw, err := testReader().Read(context.Background(), f.connection(t), checkoutWindow())
	if err != nil {
		t.Fatal(err)
	}
	m := raw.Main.Marks
	if m == nil || len(m.Passwords) != 1 || m.Passwords[0] != 117 || len(m.Clickables) != 1 || m.Clickables[0] != (Clickable{BackendNodeID: 210, Text: "Open settings"}) {
		t.Fatalf("marks = %+v", m)
	}
	if f.count("Runtime.releaseObjectGroup") == 0 {
		t.Error("the pass's objects were not released")
	}

	// A password field that cannot be named: no marks, so no values.
	delete(f.nodes, "el-pw")
	raw, err = testReader().Read(context.Background(), f.connection(t), checkoutWindow())
	if err != nil {
		t.Fatal(err)
	}
	if raw.Main.Marks != nil {
		t.Errorf("marks = %+v, want none", raw.Main.Marks)
	}
}

// TestFindTabOnTheExtension: Browser.* is refused there, so the bounds
// come from Winuse.windows and the active flag rules out background Tabs.
func TestFindTabOnTheExtension(t *testing.T) {
	ext := bridgetest.Start(t, t.TempDir(), &bridgetest.Extension{
		Tabs: []bridgetest.Tab{
			{TargetID: "E1", Title: "winuse probe page", URL: "https://a.test/", TabID: 11, WindowID: 1, Active: true},
			{TargetID: "E2", Title: "winuse probe window 2", URL: "https://b.test/", TabID: 21, WindowID: 2, Active: true},
			{TargetID: "E3", Title: "winuse probe window 3", URL: "https://c.test/", TabID: 31, WindowID: 3, Active: true},
			{TargetID: "E4", Title: "winuse probe window 2", URL: "https://b.test/old", TabID: 12, WindowID: 1, Active: false},
			{TargetID: "E5", Title: "winuse probe window 3", URL: "https://c.test/bg", TabID: 32, WindowID: 3, Active: false},
		},
		Windows: []bridgetest.Window{
			{WindowID: 1, Left: 100, Top: 60, Width: 1200, Height: 800, State: "normal"},
			{WindowID: 2, Left: 22, Top: 22, Width: 1050, Height: 700, State: "normal"},
			{WindowID: 3, Left: 22, Top: 22, Width: 1050, Height: 700, State: "normal"},
		},
	})
	bc, err := bridge.Dial(context.Background(), ext.Info.Addr(), ext.Info.Token)
	if err != nil {
		t.Fatal(err)
	}
	c := cdp.NewClient(bc)
	t.Cleanup(func() { c.Close() })
	conn := &browser.Connection{Browser: "chrome", Mode: browser.ModeExtension, Client: c}

	wins, _, _ := probeShape()
	want := []string{"E1", "E2", "E3"}
	for i, g := range wins {
		g.DPI = 168
		g.Rect = box([]DIPBounds{{Left: 100, Top: 60, Width: 1200, Height: 800}, {Left: 22, Top: 22, Width: 1050, Height: 700}, {Left: 22, Top: 22, Width: 1050, Height: 700}}[i], 168)
		tab, err := FindTab(context.Background(), conn, g)
		if err != nil || tab.TargetID != want[i] {
			t.Errorf("window %q: %s, %v", g.Title, tab.TargetID, err)
		}
	}
	for _, cmd := range ext.Commands() {
		if strings.HasPrefix(cmd.Method, "Browser.") {
			t.Errorf("%s reached the extension", cmd.Method)
		}
	}
}

// TestBrowserDomainRefusedOnTheExtension: the CDP route's bounds call is
// refused through the extension, which is why Winuse.windows exists.
func TestBrowserDomainRefusedOnTheExtension(t *testing.T) {
	if code, _ := bridge.Refusal("Browser.getWindowForTarget", ""); code != bridge.CodeRefused {
		t.Errorf("Browser.getWindowForTarget code %d", code)
	}
	if code, _ := bridge.Refusal(bridge.MethodWindows, ""); code != 0 {
		t.Errorf("Winuse.windows refused with %d", code)
	}
}
