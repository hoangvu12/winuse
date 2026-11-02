package helper

// Offline tests for a Tab that shows a browser page (chrome://newtab/):
// it maps and lists like any Tab on a launched browser (pagetest) and
// through the fake extension (bridgetest), the tab commands act on it,
// navigate leaves it (Page.navigate on a launched browser, the bridge's
// Winuse.navigateTab through the extension, which never receives
// Page.navigate for it), every other page Action refuses it with nothing
// sent and nothing attached, and the Window State says why its Page is
// read through UI Automation. No browser runs.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/bridge"
	"github.com/hoangvu12/winuse/internal/bridge/bridgetest"
	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/page/pagetest"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// newTabWindow is window 311: a Chrome Window that shows its New Tab page,
// on browser window 1's bounds.
var newTabWindow = win.WindowInfo{ID: 311, Class: chromiumWindowClass, PID: 4242, App: "Google Chrome", ExePath: chromeExe,
	Title: "New Tab - Google Chrome", Bounds: win.Rect{Left: 100, Top: 60, Right: 1300, Bottom: 860}, Visible: true}

// Target ids of the New Tab page and the site beside it.
const (
	tNewTab = "4E70000000000000000000000000000B"
	tMail   = "AAAA000000000000000000000000000A"
)

// newTabBrowser is a launched Chrome whose window 1 shows its New Tab page
// beside Inbox, with a DevTools window and tab search mapped to the same
// window (no Tabs of its strip).
func newTabBrowser(t *testing.T) *pagetest.Browser {
	b := pagetest.New(t,
		pagetest.Window{ID: 1, Left: 100, Top: 60, Width: 1200, Height: 800, Active: 1, Tabs: []pagetest.Tab{
			{TargetID: tMail, Title: "Inbox", URL: "https://mail.test/"},
			{TargetID: tNewTab, Title: "New Tab", URL: "chrome://newtab/"},
		}},
	)
	b.Others = []pagetest.Other{
		{TargetID: "DE00", Type: "page", Title: "DevTools - mail.test/", URL: "devtools://devtools/bundled/devtools_app.html", Window: 1},
		{TargetID: "5EA0", Type: "page", Title: "Tab search", URL: "chrome://tab-search.top-chrome/", Window: 1},
	}
	return b
}

// newTabExtension is the user's Chrome through the fake extension, its
// window 1 showing the New Tab page beside Inbox. Page answers the reads
// navigate makes once the Tab shows a site.
func newTabExtension(t *testing.T) (*bridgetest.Extension, *browser.Connection) {
	t.Helper()
	ext := &bridgetest.Extension{
		Tabs: []bridgetest.Tab{
			{TargetID: "E1000000000000000000000000000001", Title: "Inbox", URL: "https://mail.test/", TabID: 11, WindowID: 1},
			{TargetID: "E2000000000000000000000000000002", Title: "New Tab", URL: "chrome://newtab/", TabID: 12, WindowID: 1, Active: true},
		},
		Windows: []bridgetest.Window{{WindowID: 1, Left: 100, Top: 60, Width: 1200, Height: 800, State: "normal"}},
	}
	ext.Page = func(sid, method string, params json.RawMessage) (any, error) {
		if method != "Runtime.evaluate" {
			return nil, nil
		}
		var p struct {
			Expression string `json:"expression"`
		}
		json.Unmarshal(params, &p)
		switch p.Expression {
		case "document.readyState":
			return map[string]any{"result": map[string]any{"type": "string", "value": "complete"}}, nil
		case "location.href":
			u := ""
			for _, n := range ext.Navigated() {
				if id, url, _ := strings.Cut(n, " "); id == sid {
					u = url
				}
			}
			return map[string]any{"result": map[string]any{"type": "string", "value": u}}, nil
		}
		return map[string]any{"result": map[string]any{"type": "undefined"}}, nil
	}
	bridgetest.Start(t, t.TempDir(), ext)
	bc, err := bridge.Dial(context.Background(), ext.Info.Addr(), ext.Info.Token)
	if err != nil {
		t.Fatal(err)
	}
	c := cdp.NewClient(bc)
	t.Cleanup(func() { c.Close() })
	return ext, &browser.Connection{Browser: "chrome", Mode: browser.ModeExtension, PID: 4242, Client: c}
}

// newTabRigFor is a tab rig whose desktop holds window 311.
func newTabRigFor(t *testing.T, conn *browser.Connection, b *pagetest.Browser) *tabRig {
	r := newTabRig(t, conn, nil, b, &stripActions{})
	r.svc.Windows = func() ([]win.WindowInfo, error) { return []win.WindowInfo{newTabWindow}, nil }
	r.svc.Pages.(*livePages).reader.Settle = time.Millisecond
	r.cmds = CommandsWith(r.svc)
	return r
}

// noAttach fails if a Tab was attached.
func noAttach(t *testing.T, methods []string) {
	t.Helper()
	for _, m := range methods {
		if m == "Target.attachToTarget" {
			t.Errorf("a Tab was attached: %v", methods)
			return
		}
	}
}

func TestTabCommandsOnABrowserPageOnALaunchedBrowser(t *testing.T) {
	b := newTabBrowser(t)
	r := newTabRigFor(t, b.Connection(t), b)
	out, e := r.run(t, "tab-list", protocol.TabArgs{Window: "311"})
	if e != nil || out != "* 4e700000 \"New Tab\" chrome://newtab/\n  aaaa0000 \"Inbox\" https://mail.test/\n" {
		t.Errorf("tab list = %q, %+v", out, e)
	}
	noAttach(t, b.Methods())

	// A Tab opens beside it, and the New Tab page itself closes.
	if out, e := r.run(t, "tab-new", protocol.TabArgs{Window: "311", URL: "https://b.test/"}); e != nil || !strings.HasPrefix(out, "ok c0ffee01") {
		t.Errorf("tab new = %q, %+v", out, e)
	}
	if out, e := r.run(t, "tab-close", protocol.TabArgs{Window: "311", Tab: "4e70"}); e != nil || out != okOutput {
		t.Errorf("tab close = %q, %+v", out, e)
	}
	for _, tab := range b.Window(1).Tabs {
		if tab.TargetID == tNewTab {
			t.Error("the New Tab page is still open")
		}
	}
	noForegroundCalls(t, b.Methods())
}

func TestTabCommandsOnABrowserPageThroughTheExtension(t *testing.T) {
	ext, conn := newTabExtension(t)
	r := newTabRigFor(t, conn, nil)
	out, e := r.run(t, "tab-list", protocol.TabArgs{Window: "311"})
	if e != nil || out != "  e1000000 \"Inbox\" https://mail.test/\n* e2000000 \"New Tab\" chrome://newtab/\n" {
		t.Errorf("tab list = %q, %+v", out, e)
	}
	if out, e := r.run(t, "tab-new", protocol.TabArgs{Window: "311"}); e != nil || !strings.HasPrefix(out, "ok newtab") {
		t.Errorf("tab new = %q, %+v", out, e)
	}
	// Switch away from it, then close it.
	if out, e := r.run(t, "tab-switch", protocol.TabArgs{Window: "311", Tab: "e100"}); e != nil || out != okOutput {
		t.Errorf("tab switch = %q, %+v", out, e)
	}
	if got := ext.Activated(); len(got) != 1 || got[0] != "E1000000000000000000000000000001" {
		t.Errorf("activated %v", got)
	}
	r.svc.Windows = func() ([]win.WindowInfo, error) {
		w := newTabWindow
		w.Title = "Inbox - Google Chrome"
		return []win.WindowInfo{w}, nil
	}
	r.cmds = CommandsWith(r.svc)
	if out, e := r.run(t, "tab-close", protocol.TabArgs{Window: "311", Tab: "e200"}); e != nil || out != okOutput {
		t.Errorf("tab close = %q, %+v", out, e)
	}
	noAttach(t, extMethods(ext))
	noForegroundCalls(t, extMethods(ext))
}

// TestNavigateFromABrowserPageOnALaunchedBrowser: the New Tab page is
// attached only by navigate, and Page.navigate leaves it as any page.
func TestNavigateFromABrowserPageOnALaunchedBrowser(t *testing.T) {
	b := newTabBrowser(t)
	conn := b.Connection(t)
	r := &winRig{conn: conn, loader: "L1", url: "chrome://newtab/", ready: "complete",
		entries: []page.HistoryEntry{{ID: 7, URL: "chrome://newtab/"}}}
	r.tabRig = newTabRigFor(t, conn, b)
	r.handlers(b.Srv)
	t.Cleanup(func() { page.Unwatch(conn.Client, "session-"+tNewTab) })

	// Anything else is refused first, with nothing attached.
	_, e := r.run(t, "eval", protocol.EvalArgs{Window: "311", Expression: "1"})
	if e == nil || e.Msg != "the active Tab of window 311 shows a browser page (chrome://newtab/), which winuse does not read or act in; nothing was sent" ||
		e.Hint != "run winuse navigate 311 <url> to open a site" {
		t.Errorf("eval: %+v", e)
	}
	noAttach(t, b.Methods())

	out, e := r.run(t, "navigate", protocol.NavigateArgs{Window: "311", URL: "a.test"})
	if e != nil || out != "ok https://a.test\n" {
		t.Fatalf("navigate = %q, %+v", out, e)
	}
	var sent bool
	for _, m := range b.Srv.Received() {
		if m.Method == "Page.navigate" {
			sent = m.SessionID == "session-"+tNewTab && strings.Contains(string(m.Params), `"url":"https://a.test"`)
		}
	}
	if !sent {
		t.Error("Page.navigate did not go to the New Tab page's session")
	}
	r.passiveOnly(t)
}

// TestNavigateFromABrowserPageThroughTheExtension: the bridge's
// Winuse.navigateTab leaves the New Tab page; the extension never gets
// Page.navigate, and the Tab is attached only once it shows the site.
func TestNavigateFromABrowserPageThroughTheExtension(t *testing.T) {
	ext, conn := newTabExtension(t)
	r := newTabRigFor(t, conn, nil)

	_, e := r.run(t, "back", protocol.NavigateArgs{Window: "311"})
	if e == nil || e.Msg != "the active Tab of window 311 shows a browser page (chrome://newtab/), and through the winuse extension only navigate can leave it; nothing was sent" ||
		e.Hint != "run winuse navigate 311 <url> to open a site" {
		t.Errorf("back: %+v", e)
	}

	out, e := r.run(t, "navigate", protocol.NavigateArgs{Window: "311", URL: "a.test/x"})
	if e != nil || out != "ok https://a.test/x\n" {
		t.Fatalf("navigate = %q, %+v", out, e)
	}
	if got := ext.Navigated(); len(got) != 1 || got[0] != "E2000000000000000000000000000002 https://a.test/x" {
		t.Errorf("navigated %v", got)
	}
	navigated, attachedAfter := false, false
	for _, c := range ext.Commands() {
		switch {
		case c.Method == "Page.navigate":
			t.Errorf("Page.navigate reached the extension: %s", c.Params)
		case c.Method == bridge.MethodNavigateTab:
			navigated = true
		case c.Method == "Target.attachToTarget" && strings.Contains(string(c.Params), "E2000000000000000000000000000002"):
			if !navigated {
				t.Error("the New Tab page was attached before it was left")
			}
			attachedAfter = true
		}
	}
	if !attachedAfter {
		t.Error("the site was never attached to wait for its load")
	}
	if len(r.cursor.labels) != 1 || r.cursor.labels[0] != `go to "a.test"` {
		t.Errorf("cursor %q", r.cursor.labels)
	}
	if len(ext.Activated()) != 0 {
		t.Error("navigate switched Tabs")
	}
	noForegroundCalls(t, extMethods(ext))
}

// TestNavigateFromABrowserPageThroughTheExtensionUnread: a destination
// that is a browser page too, and --wait none, print as soon as the Tab
// shows the URL, with nothing attached.
func TestNavigateFromABrowserPageThroughTheExtensionUnread(t *testing.T) {
	ext, conn := newTabExtension(t)
	r := newTabRigFor(t, conn, nil)
	out, e := r.run(t, "navigate", protocol.NavigateArgs{Window: "311", URL: "chrome://settings/"})
	if e != nil || out != "ok chrome://settings/\n" {
		t.Fatalf("to settings = %q, %+v", out, e)
	}
	// The Tab is titled with its URL now; the Window follows.
	r.svc.Windows = func() ([]win.WindowInfo, error) {
		w := newTabWindow
		w.Title = "chrome://settings/ - Google Chrome"
		return []win.WindowInfo{w}, nil
	}
	r.cmds = CommandsWith(r.svc)
	out, e = r.run(t, "navigate", protocol.NavigateArgs{Window: "311", URL: "https://b.test/", Wait: "none"})
	if e != nil || out != "ok https://b.test/\n" {
		t.Fatalf("--wait none = %q, %+v", out, e)
	}
	noAttach(t, extMethods(ext))
}

// TestPageActionsRefuseABrowserPage: the window-level page Actions refuse
// a browser page with nothing sent and nothing attached, but for wait
// download, which is the browser's.
func TestPageActionsRefuseABrowserPage(t *testing.T) {
	ext, conn := newTabExtension(t)
	r := newTabRigFor(t, conn, nil)
	file := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(file, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := "the active Tab of window 311 shows a browser page (chrome://newtab/), which winuse does not read or act in; nothing was sent"
	for cmd, args := range map[string]any{
		"eval":   protocol.EvalArgs{Window: "311", Expression: "1"},
		"wait":   protocol.WaitArgs{Window: "311", For: "selector", Value: "#a"},
		"dialog": protocol.DialogArgs{Window: "311", Action: "accept"},
		"upload": protocol.UploadArgs{Window: "311", Files: []string{file}},
	} {
		_, e := r.run(t, cmd, args)
		if e == nil || e.Msg != want || e.Hint != "run winuse navigate 311 <url> to open a site" {
			t.Errorf("%s: %+v", cmd, e)
		}
	}
	_, e := r.run(t, "wait", protocol.WaitArgs{Window: "311", For: "download"})
	if e == nil || !strings.HasPrefix(e.Msg, "downloads through the winuse extension go to the browser's own download folder") {
		t.Errorf("wait download: %+v", e)
	}
	if len(r.log()) != 0 {
		t.Errorf("acted: %v", r.log())
	}
	noAttach(t, extMethods(ext))
}

// TestWindowStateOfABrowserPage: the Window State is the UI Automation
// read with a note naming the browser page and navigate, on both routes.
func TestWindowStateOfABrowserPage(t *testing.T) {
	b := newTabBrowser(t)
	_, extConn := newTabExtension(t)
	for name, c := range map[string]struct {
		conn    *browser.Connection
		methods func() []string
	}{
		"launched":  {b.Connection(t), b.Methods},
		"extension": {extConn, nil},
	} {
		r := newTabRigFor(t, c.conn, nil)
		tree, rerr := readWindowTree(r.svc, context.Background(), newTabWindow)
		if rerr != nil {
			t.Fatalf("%s: %v", name, rerr.Msg)
		}
		note := "the Page is read through UI Automation because the active Tab shows a browser page (chrome://newtab/); run winuse navigate 311 <url> to open a site."
		if len(tree.Notes) != 1 || tree.Notes[0] != note {
			t.Errorf("%s: notes %q", name, tree.Notes)
		}
		if c.methods != nil {
			noAttach(t, c.methods())
		}
	}
}

// internalTabPages is a Pages service whose Window shows a browser page now.
type internalTabPages struct{ fakePages }

func (p *internalTabPages) PageActiveTab(ctx context.Context, w win.WindowInfo) (string, error) {
	return "", &page.InternalPageError{URL: "chrome://newtab/"}
}

// TestPageElementOfATabNowOnABrowserPageIsStale: a Page Element whose Tab
// went to a browser page fails as stale, as when the Tab was switched.
func TestPageElementOfATabNowOnABrowserPageIsStale(t *testing.T) {
	w := newTabWindow
	o := &page.Origin{TargetID: "T1"}
	switched := Services{Pages: &tabPages{tab: "T2"}}.pageTabCheck(context.Background(), w, 4, o)
	internal := Services{Pages: &internalTabPages{}}.pageTabCheck(context.Background(), w, 4, o)
	if switched == nil || internal == nil || internal.Msg != switched.Msg || internal.Hint != switched.Hint {
		t.Errorf("browser page %+v, switched %+v", internal, switched)
	}
	if e := (Services{Pages: &internalTabPages{}}).pageTabCheck(context.Background(), w, -1, o); e == nil || !strings.Contains(e.Msg, "shows another Tab") {
		t.Errorf("no index: %+v", e)
	}
}
