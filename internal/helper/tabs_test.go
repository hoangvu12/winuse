package helper

// Offline tests for the tab commands (browser merge ticket 07) over a fake
// launched browser (pagetest, on cdptest) and the fake winuse extension
// (bridgetest): each command per connection mode, the Headless or Headed
// routing of tab switch, tab id lookup, only the Window's own Tabs, the
// last-Tab refusal, the no-connection failure, the Batch's scope, and that
// nothing ever sends Target.activateTarget or Page.bringToFront.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/bridge"
	"github.com/hoangvu12/winuse/internal/bridge/bridgetest"
	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/keys"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/page/pagetest"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

const chromeExe = `C:\Program Files\Google\Chrome\Application\chrome.exe`

// The Windows of these tests: three Chrome Windows (one per fake browser
// window) and Notepad.
var tabTestWindows = []win.WindowInfo{
	{ID: 301, Class: chromiumWindowClass, PID: 4242, App: "Google Chrome", ExePath: chromeExe, Title: "Docs - Google Chrome",
		Bounds: win.Rect{Left: 100, Top: 60, Right: 1300, Bottom: 860}, Visible: true},
	{ID: 302, Class: chromiumWindowClass, PID: 4242, App: "Google Chrome", ExePath: chromeExe, Title: "Docs - Google Chrome",
		Bounds: win.Rect{Left: 22, Top: 22, Right: 1072, Bottom: 722}, Visible: true},
	{ID: 303, Class: chromiumWindowClass, PID: 4242, App: "Google Chrome", ExePath: chromeExe, Title: "Solo - Google Chrome",
		Bounds: win.Rect{Left: 400, Top: 300, Right: 1200, Bottom: 900}, Visible: true},
	{ID: 101, Class: "Notepad", PID: 7, App: "Notepad", ExePath: `C:\Windows\notepad.exe`, Title: "notes - Notepad", Visible: true},
}

// tabGeometry is each Chrome Window's GetWindowRect at 96 DPI.
func tabGeometry(w win.WindowInfo) page.Geometry {
	return page.Geometry{Title: w.Title, DPI: 96,
		Rect: page.Box{Left: w.Bounds.Left, Top: w.Bounds.Top, Right: w.Bounds.Right, Bottom: w.Bounds.Bottom}}
}

// Target ids of the launched browser's Tabs.
const (
	tInbox = "AAAA000000000000000000000000000A"
	tDocs  = "BBBB000000000000000000000000000B"
	tNews  = "CCCC000000000000000000000000000C"
	tDocs2 = "DDDD000000000000000000000000000D"
	tShop  = "EEEE000000000000000000000000000E"
	tSolo  = "F00D000000000000000000000000000F"
)

func launchedBrowser(t *testing.T) *pagetest.Browser {
	return pagetest.New(t,
		pagetest.Window{ID: 1, Left: 100, Top: 60, Width: 1200, Height: 800, Active: 1, Tabs: []pagetest.Tab{
			{TargetID: tInbox, Title: "Inbox", URL: "https://mail.test/"},
			{TargetID: tDocs, Title: "Docs", URL: "https://docs.test/"},
			{TargetID: tNews, Title: "News", URL: "https://news.test/"},
		}},
		pagetest.Window{ID: 2, Left: 22, Top: 22, Width: 1050, Height: 700, Tabs: []pagetest.Tab{
			{TargetID: tDocs2, Title: "Docs", URL: "https://docs.test/old"},
			{TargetID: tShop, Title: "Shop", URL: "https://shop.test/"},
		}},
		pagetest.Window{ID: 3, Left: 400, Top: 300, Width: 800, Height: 600, Tabs: []pagetest.Tab{
			{TargetID: tSolo, Title: "Solo", URL: "https://solo.test/"},
		}},
	)
}

// stripActions is Services.Actions for these tests: the latest Window
// State's Elements (the tab strip) and their bounds, without COM.
type stripActions struct {
	els    map[uint64][]wstate.Element
	bounds map[int]win.Rect
}

func (a *stripActions) ElementPoint(ctx context.Context, hwnd uint64, index int) (uia.Point, error) {
	return uia.Point{}, errors.New("no point in these tests")
}
func (a *stripActions) StoredElements(hwnd uint64) ([]wstate.Element, bool) {
	els, ok := a.els[hwnd]
	return els, ok
}
func (a *stripActions) ElementBounds(ctx context.Context, hwnd uint64, index int) (int32, int32, int32, int32, string, error) {
	r, ok := a.bounds[index]
	if !ok {
		return 0, 0, 0, 0, "", errors.New("no bounds")
	}
	return r.Left, r.Top, r.Right, r.Bottom, "", nil
}
func (a *stripActions) SplicePage(ctx context.Context, hwnd uint64, base, merged []wstate.Element, from []int, origins map[int]any) error {
	return nil
}
func (a *stripActions) ElementOrigin(hwnd uint64, index int) (any, error) {
	if a.els[hwnd][index].Role == "tab" && a.els[hwnd][index].Name == "Page tab" {
		return &page.Origin{}, nil // a web page's own tab widget
	}
	return nil, nil
}

// window 301's strip: Inbox, Docs, News, plus a page's own tab widget
// that is no strip item.
func stripOf301() *stripActions {
	return &stripActions{
		els: map[uint64][]wstate.Element{301: {
			{Depth: 0, Role: "window", Name: "Docs - Google Chrome"},
			{Depth: 1, Role: "tab", Name: "Inbox"},
			{Depth: 1, Role: "tab", Name: "Docs - Audio playing"},
			{Depth: 1, Role: "tab", Name: "News"},
			{Depth: 1, Role: "document", Name: "Docs"},
			{Depth: 2, Role: "tab", Name: "Page tab"},
		}},
		bounds: map[int]win.Rect{1: {Left: 110, Top: 66, Right: 300, Bottom: 96}, 2: {Left: 300, Top: 66, Right: 490, Bottom: 96}, 3: {Left: 490, Top: 66, Right: 680, Bottom: 96}},
	}
}

// tabRig is a helper wired to one Browser Connection.
type tabRig struct {
	svc     Services
	cmds    map[string]Command
	esc     *scopeEsc
	cursor  *cursorSpy
	mu      sync.Mutex
	steps   []string
	browser *pagetest.Browser // the launched browser, or nil
	ready   error             // Services.Ready's refusal (the Headless switch's)
}

func (r *tabRig) step(s string) {
	r.mu.Lock()
	r.steps = append(r.steps, s)
	r.mu.Unlock()
}

func (r *tabRig) log() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.steps...)
}

func newTabRig(t *testing.T, conn *browser.Connection, connErr error, b *pagetest.Browser, actions ElementPointer) *tabRig {
	r := &tabRig{esc: &scopeEsc{}, browser: b}
	r.cursor = &cursorSpy{share: r.step, esc: r.esc}
	if actions == nil {
		actions = &stripActions{}
	}
	r.svc = Services{
		Trees:     &fakeTrees{},
		Windows:   func() ([]win.WindowInfo, error) { return tabTestWindows, nil },
		Locked:    func() (bool, string) { return false, "" },
		Integrity: func(win.WindowInfo) error { return nil },
		Activate: func(hwnd uint64) error {
			r.step("activate")
			if b != nil {
				b.Foreground = int(hwnd - 300) // window 301 is browser window 1
			}
			return nil
		},
		PressKeys: func(c keys.Chord) error {
			r.step("press " + c.String())
			if b != nil {
				b.Press(c.String())
			}
			return nil
		},
		Click: func(uint64, int32, int32, win.MouseButton, int) error {
			t.Error("the tab commands never click")
			return nil
		},
		Ready:   func(uint64) error { return r.ready },
		Actions: actions,
		Cursor:  r.cursor,
		Esc:     r.esc,
		Sleep:   func(time.Duration) {},
		Browser: &fakeBrowsers{},
		Pages: &livePages{
			browsers: pageConns{identify: map[string]string{chromeExe: "chrome"}, conn: conn, err: connErr},
			reader:   page.NewReader(),
			geometry: tabGeometry,
		},
	}
	r.cmds = CommandsWith(r.svc)
	return r
}

func (r *tabRig) run(t *testing.T, cmd string, args any) (string, *Error) {
	t.Helper()
	return runCommand(t, r.cmds, cmd, args)
}

// noForegroundCalls fails on any CDP method that could raise a window.
func noForegroundCalls(t *testing.T, methods []string) {
	t.Helper()
	for _, m := range methods {
		if m == "Target.activateTarget" || m == "Page.bringToFront" {
			t.Errorf("%s was sent", m)
		}
	}
}

func extMethods(ext *bridgetest.Extension) []string {
	var out []string
	for _, c := range ext.Commands() {
		out = append(out, c.Method)
	}
	return out
}

func TestTabListOnALaunchedBrowserListsOnlyThatWindow(t *testing.T) {
	b := launchedBrowser(t)
	r := newTabRig(t, b.Connection(t), nil, b, nil)
	out, e := r.run(t, "tab-list", protocol.TabArgs{Window: "301"})
	if e != nil {
		t.Fatal(e.Msg)
	}
	want := `  cccc0000 "News" https://news.test/
* bbbb0000 "Docs" https://docs.test/
  aaaa0000 "Inbox" https://mail.test/
`
	if out != want {
		t.Errorf("tab list =\n%s\nwant\n%s", out, want)
	}
	// Window 302 shows the other "Docs": its own Tabs only.
	out, e = r.run(t, "tab-list", protocol.TabArgs{Window: "302"})
	if e != nil || out != "  eeee0000 \"Shop\" https://shop.test/\n* dddd0000 \"Docs\" https://docs.test/old\n" {
		t.Errorf("window 302: %q, %v", out, e)
	}
	// A read: no cursor, no Esc scope, no input.
	if len(r.log()) != 0 || len(r.esc.brackets()) != 0 {
		t.Errorf("tab list acted: %v %v", r.log(), r.esc.brackets())
	}
	noForegroundCalls(t, b.Methods())
}

func TestTabListOnTheExtension(t *testing.T) {
	ext, conn := extensionBrowser(t)
	r := newTabRig(t, conn, nil, nil, nil)
	out, e := r.run(t, "tab-list", protocol.TabArgs{Window: "301"})
	want := `  e1000000 "Inbox" https://mail.test/
* e2000000 "Docs" https://docs.test/
  e3000000 "A very long title that goes on and on and on, well past the eighty characters..." https://long.test/
`
	if e != nil || out != want {
		t.Errorf("tab list = %q, %v\nwant %q", out, e, want)
	}
	noForegroundCalls(t, extMethods(ext))
}

// extensionBrowser is the user's Chrome through the fake extension: window
// 1 (Window 301) with three Tabs in strip order, window 2 (Window 302)
// with one.
func extensionBrowser(t *testing.T) (*bridgetest.Extension, *browser.Connection) {
	t.Helper()
	ext := bridgetest.Start(t, t.TempDir(), &bridgetest.Extension{
		Tabs: []bridgetest.Tab{
			{TargetID: "E1000000000000000000000000000001", Title: "Inbox", URL: "https://mail.test/", TabID: 11, WindowID: 1},
			{TargetID: "E2000000000000000000000000000002", Title: "Docs", URL: "https://docs.test/", TabID: 12, WindowID: 1, Active: true},
			{TargetID: "E3000000000000000000000000000003", Title: "A very long title that goes on and on and on, well past the eighty characters of a tab list line",
				URL: "https://long.test/", TabID: 13, WindowID: 1},
			{TargetID: "E4000000000000000000000000000004", Title: "Docs", URL: "https://docs.test/old", TabID: 21, WindowID: 2, Active: true},
		},
		Windows: []bridgetest.Window{
			{WindowID: 1, Left: 100, Top: 60, Width: 1200, Height: 800, State: "normal"},
			{WindowID: 2, Left: 22, Top: 22, Width: 1050, Height: 700, State: "normal"},
		},
	})
	bc, err := bridge.Dial(context.Background(), ext.Info.Addr(), ext.Info.Token)
	if err != nil {
		t.Fatal(err)
	}
	c := cdp.NewClient(bc)
	t.Cleanup(func() { c.Close() })
	return ext, &browser.Connection{Browser: "chrome", Mode: browser.ModeExtension, PID: 4242, Client: c}
}

func TestTabCommandsWithoutABrowserConnection(t *testing.T) {
	r := newTabRig(t, nil, errors.New("nothing debuggable"), nil, nil)
	for _, c := range []struct {
		cmd  string
		args protocol.TabArgs
		sent string
	}{
		{"tab-list", protocol.TabArgs{Window: "301"}, ""},
		{"tab-new", protocol.TabArgs{Window: "301"}, nothingSentNote},
		{"tab-close", protocol.TabArgs{Window: "301", Tab: "aaaa"}, nothingSentNote},
		{"tab-switch", protocol.TabArgs{Window: "301", Tab: "aaaa"}, nothingSentNote},
	} {
		_, e := r.run(t, c.cmd, c.args)
		if e == nil || e.Msg != "window 301 is a Chrome Window with no Browser Connection"+c.sent || e.Hint != "run winuse browser status chrome" {
			t.Errorf("%s: %+v", c.cmd, e)
		}
	}
	if len(r.log()) != 0 {
		t.Errorf("acted without a connection: %v", r.log())
	}

	// Notepad has no Tabs at all.
	_, e := r.run(t, "tab-list", protocol.TabArgs{Window: "101"})
	if e == nil || e.Msg != "window 101 is not a browser Window, so it has no Tabs" {
		t.Errorf("notepad: %+v", e)
	}
	// A connection to another Chrome process.
	r = newTabRig(t, &browser.Connection{Browser: "chrome", Mode: browser.ModeLaunch, PID: 999}, nil, nil, nil)
	_, e = r.run(t, "tab-list", protocol.TabArgs{Window: "301"})
	if e == nil || e.Msg != "window 301 belongs to Chrome process 4242, which has no Browser Connection (winuse can reach Chrome process 999)" ||
		e.Hint != "if it is the user's own Chrome, run winuse browser ext install chrome to reach it through the winuse extension; otherwise work in a window of Chrome process 999" {
		t.Errorf("another process: %+v", e)
	}
}

func TestTabArgumentFailures(t *testing.T) {
	b := launchedBrowser(t)
	r := newTabRig(t, b.Connection(t), nil, b, nil)
	_, e := r.run(t, "tab-close", protocol.TabArgs{Window: "301"})
	if e == nil || e.Hint != "usage: winuse tab close <window> <tab>" {
		t.Errorf("no tab: %+v", e)
	}
	_, e = r.run(t, "tab-list", protocol.TabArgs{})
	if e == nil || !strings.Contains(e.Msg, "a window reference is required") {
		t.Errorf("no window: %+v", e)
	}
}

func TestTabNewOnALaunchedBrowser(t *testing.T) {
	b := launchedBrowser(t)
	r := newTabRig(t, b.Connection(t), nil, b, nil)
	out, e := r.run(t, "tab-new", protocol.TabArgs{Window: "301", URL: "example.com"})
	if e != nil || out != "ok c0ffee01\n" {
		t.Fatalf("tab new = %q, %+v", out, e)
	}
	w := b.Window(1)
	if len(w.Tabs) != 4 || w.Tabs[3].URL != "https://example.com" || b.ActiveTab(1) != tDocs {
		t.Errorf("window 1 = %+v", w)
	}
	// Headless: the cursor shows it, a passive Esc scope, no activation.
	if got := strings.Join(r.log(), ","); got != "cursor-travel,cursor-click" {
		t.Errorf("steps = %s", got)
	}
	if r.cursor.labels[0] != "open tab" || r.cursor.targets[0] != tabTestWindows[0].Bounds {
		t.Errorf("cursor %q to %+v", r.cursor.labels[0], r.cursor.targets[0])
	}
	if got := strings.Join(r.esc.brackets(), ","); got != "arm-passive,disarm-passive" {
		t.Errorf("esc = %s", got)
	}

	// Window 302 is not the browser's last active window: CDP cannot name
	// a window, the Tab lands in window 1, and winuse closes it again.
	_, e = r.run(t, "tab-new", protocol.TabArgs{Window: "302"})
	if e == nil || e.Msg != "Chrome opened the new tab in another of its windows, so winuse closed it again" ||
		!strings.Contains(e.Hint, "winuse activate-window 302") {
		t.Errorf("elsewhere: %+v", e)
	}
	if len(b.Window(1).Tabs) != 4 || len(b.Window(2).Tabs) != 2 {
		t.Errorf("the misplaced tab stayed: %d, %d", len(b.Window(1).Tabs), len(b.Window(2).Tabs))
	}
	noForegroundCalls(t, b.Methods())
}

func TestTabNewOnTheExtension(t *testing.T) {
	ext, conn := extensionBrowser(t)
	r := newTabRig(t, conn, nil, nil, nil)
	out, e := r.run(t, "tab-new", protocol.TabArgs{Window: "302", URL: "https://a.test/"})
	if e != nil || !strings.HasPrefix(out, "ok newtab") {
		t.Fatalf("tab new = %q, %+v", out, e)
	}
	out, _ = r.run(t, "tab-list", protocol.TabArgs{Window: "302"})
	if !strings.Contains(out, "  newtab5 \"\" https://a.test/") {
		t.Errorf("the new Tab is not in window 2 in the background:\n%s", out)
	}
	noForegroundCalls(t, extMethods(ext))
}

func TestTabClose(t *testing.T) {
	b := launchedBrowser(t)
	r := newTabRig(t, b.Connection(t), nil, b, stripOf301())

	// An id of another window's Tab sends nothing.
	_, e := r.run(t, "tab-close", protocol.TabArgs{Window: "301", Tab: "dddd"})
	if e == nil || e.Msg != `no tab has the id "dddd" in window 301; nothing was sent` || e.Hint != "list the Tabs and their ids: winuse tab list 301" {
		t.Errorf("another window's tab: %+v", e)
	}

	out, e := r.run(t, "tab-close", protocol.TabArgs{Window: "301", Tab: "CCCC0000"})
	if e != nil || out != okOutput {
		t.Fatalf("close = %q, %+v", out, e)
	}
	if w := b.Window(1); len(w.Tabs) != 2 || b.ActiveTab(1) != tDocs {
		t.Errorf("window 1 = %+v", w)
	}
	// The cursor went to News's strip item.
	if r.cursor.labels[0] != `close tab "News"` || r.cursor.targets[0] != (win.Rect{Left: 490, Top: 66, Right: 680, Bottom: 96}) {
		t.Errorf("cursor %q to %+v", r.cursor.labels[0], r.cursor.targets[0])
	}

	// The last Tab of a Window: refused, pointing at close-window.
	_, e = r.run(t, "tab-close", protocol.TabArgs{Window: "303", Tab: "f00d"})
	if e == nil || e.Msg != "tab f00d0000 is the last tab of window 303, and closing it would close the Window; nothing was sent" ||
		e.Hint != "to close the Window, run winuse close-window 303" {
		t.Errorf("last tab: %+v", e)
	}
	if len(b.Window(3).Tabs) != 1 {
		t.Error("the last tab was closed")
	}

	// A page that asks whether to leave keeps its Tab.
	b.KeepOpen = true
	out, e = r.run(t, "tab-close", protocol.TabArgs{Window: "301", Tab: "aaaa"})
	if e != nil || out != "ok (the close was requested and tab aaaa0000 is still open; the page may be asking whether to leave: read winuse tab list 301)\n" {
		t.Errorf("kept open: %q, %+v", out, e)
	}
	noForegroundCalls(t, b.Methods())
}

func TestTabCloseOnTheExtension(t *testing.T) {
	ext, conn := extensionBrowser(t)
	r := newTabRig(t, conn, nil, nil, nil)
	out, e := r.run(t, "tab-close", protocol.TabArgs{Window: "301", Tab: "e1"})
	if e == nil || !strings.Contains(e.Msg, "too short") {
		t.Errorf("short id: %q, %+v", out, e)
	}
	out, e = r.run(t, "tab-close", protocol.TabArgs{Window: "301", Tab: "e100"})
	if e != nil || out != okOutput {
		t.Fatalf("close = %q, %+v", out, e)
	}
	for _, c := range ext.Commands() {
		if c.Method == "Target.closeTarget" && !strings.Contains(string(c.Params), "E1000000000000000000000000000001") {
			t.Errorf("closed %s", c.Params)
		}
	}
	// Window 302 has one Tab left.
	_, e = r.run(t, "tab-close", protocol.TabArgs{Window: "302", Tab: "e400"})
	if e == nil || !strings.Contains(e.Msg, "is the last tab of window 302") {
		t.Errorf("last tab: %+v", e)
	}
}

func TestTabSwitchThroughTheExtensionIsHeadless(t *testing.T) {
	ext, conn := extensionBrowser(t)
	r := newTabRig(t, conn, nil, nil, nil)
	// Switching to the active Tab does nothing.
	out, e := r.run(t, "tab-switch", protocol.TabArgs{Window: "301", Tab: "e200"})
	if e != nil || out != okOutput || len(r.log()) != 0 || len(ext.Activated()) != 0 {
		t.Errorf("already active: %q, %+v, %v", out, e, r.log())
	}
	out, e = r.run(t, "tab-switch", protocol.TabArgs{Window: "301", Tab: "e1000000"})
	if e != nil || out != okOutput {
		t.Fatalf("switch = %q, %+v", out, e)
	}
	if got := ext.Activated(); len(got) != 1 || got[0] != "E1000000000000000000000000000001" {
		t.Errorf("activated %v", got)
	}
	// No activation, no keys, no banner: the cursor and a passive scope.
	if got := strings.Join(r.log(), ","); got != "cursor-travel,cursor-click" {
		t.Errorf("steps = %s", got)
	}
	if got := strings.Join(r.esc.brackets(), ","); got != "arm-passive,disarm-passive" {
		t.Errorf("esc = %s", got)
	}
	if r.cursor.labels[0] != `switch to "Inbox"` {
		t.Errorf("label %q", r.cursor.labels[0])
	}
	noForegroundCalls(t, extMethods(ext))
}

func TestTabSwitchThroughTheExtensionStoppedDuringTravel(t *testing.T) {
	ext, conn := extensionBrowser(t)
	r := newTabRig(t, conn, nil, nil, nil)
	r.esc.pressOn = "travel"
	_, e := r.run(t, "tab-switch", protocol.TabArgs{Window: "301", Tab: "e1000000"})
	if e == nil || e.Exit != protocol.ExitEsc || !strings.Contains(e.Hint, "nothing was sent") {
		t.Errorf("stop: %+v", e)
	}
	if len(ext.Activated()) != 0 {
		t.Error("the switch was sent after the stop")
	}
}

func TestTabSwitchOnALaunchedBrowserWithHeaded(t *testing.T) {
	b := launchedBrowser(t)
	r := newTabRig(t, b.Connection(t), nil, b, nil)
	// No Window State: Ctrl+Tab steps from Docs, checked one by one.
	out, e := r.run(t, "tab-switch", protocol.TabArgs{Window: "301", Tab: "aaaa", Headed: true})
	if e != nil || out != okOutput {
		t.Fatalf("switch = %q, %+v", out, e)
	}
	if got := strings.Join(r.log(), ","); got != "activate,press Control_L+Tab,press Control_L+Tab" {
		t.Errorf("steps = %s", got)
	}
	if b.ActiveTab(1) != tInbox {
		t.Errorf("active %s", b.ActiveTab(1))
	}
	// The Headed pipeline: a swallowing scope, no cursor.
	if got := strings.Join(r.esc.brackets(), ","); got != "arm,disarm" {
		t.Errorf("esc = %s", got)
	}
	if len(r.cursor.labels) != 0 {
		t.Errorf("the cursor showed a Headed switch: %v", r.cursor.labels)
	}
	noForegroundCalls(t, b.Methods())
}

func TestTabSwitchUsesTheStripPosition(t *testing.T) {
	b := launchedBrowser(t)
	r := newTabRig(t, b.Connection(t), nil, b, stripOf301())
	out, e := r.run(t, "tab-switch", protocol.TabArgs{Window: "301", Tab: "cccc", Headed: true})
	if e != nil || out != okOutput {
		t.Fatalf("switch = %q, %+v", out, e)
	}
	if got := strings.Join(r.log(), ","); got != "activate,press Control_L+3" {
		t.Errorf("steps = %s", got)
	}
	if b.ActiveTab(1) != tNews {
		t.Errorf("active %s", b.ActiveTab(1))
	}
}

func TestTabSwitchThatNeverShows(t *testing.T) {
	b := launchedBrowser(t)
	r := newTabRig(t, b.Connection(t), nil, b, nil)
	r.svc.Activate = func(uint64) error { r.step("activate"); return nil } // keys reach no browser window
	r.cmds = CommandsWith(r.svc)
	_, e := r.run(t, "tab-switch", protocol.TabArgs{Window: "301", Tab: "aaaa", Headed: true})
	if e == nil || e.Msg != "tab aaaa0000 is not the active tab of window 301 after the switch" ||
		e.Hint != "keys were sent to the Window; read winuse tab list 301 before repeating it" {
		t.Errorf("never shown: %+v", e)
	}
	// One full cycle of steps at most.
	if n := strings.Count(strings.Join(r.log(), ","), "press"); n != 3 {
		t.Errorf("%d presses, want 3", n)
	}
}

func TestPlanTabKeys(t *testing.T) {
	items := func(names ...string) []stripItem {
		var out []stripItem
		for i, n := range names {
			out = append(out, stripItem{index: i + 1, name: n})
		}
		return out
	}
	tabs := func(active string, titles ...string) page.WindowTabs {
		var wt page.WindowTabs
		for _, ti := range titles {
			tab := page.Tab{TargetID: ti, Title: ti}
			wt.Tabs = append(wt.Tabs, tab)
			if ti == active {
				wt.Active = tab
			}
		}
		return wt
	}
	ten := []string{"t1", "t2", "t3", "t4", "t5", "t6", "t7", "t8", "t9", "t10"}
	for _, c := range []struct {
		name   string
		items  []stripItem
		tabs   page.WindowTabs
		target string
		want   tabKeys
	}{
		{"second of three", items("a", "b", "c"), tabs("a", "a", "b", "c"), "b", tabKeys{direct: "Control_L+2"}},
		{"the last of ten", items(ten...), tabs("t1", ten...), "t10", tabKeys{direct: "Control_L+9", back: true}},
		{"ninth of ten, from the second", items(ten...), tabs("t2", ten...), "t9", tabKeys{back: true}},
		{"ninth of ten, from the seventh", items(ten...), tabs("t7", ten...), "t9", tabKeys{}},
		{"a stale strip", items("a", "b"), tabs("a", "a", "b", "c"), "b", tabKeys{}},
		{"two items with the title", items("a", "b", "b"), tabs("a", "a", "b", "b"), "b", tabKeys{}},
		{"no Window State", nil, tabs("a", "a", "b"), "b", tabKeys{}},
	} {
		var target page.Tab
		for _, tab := range c.tabs.Tabs {
			if tab.Title == c.target {
				target = tab
			}
		}
		if got := planTabKeys(c.items, c.tabs, target); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestTabStepsInABatch(t *testing.T) {
	// Through the extension every tab step is Headless: one passive scope.
	ext, conn := extensionBrowser(t)
	r := newTabRig(t, conn, nil, nil, nil)
	out, e := r.run(t, "batch", protocol.BatchArgs{Window: "301", Steps: []protocol.Step{
		{Kind: "tab-new", Args: json.RawMessage(`{"url":"https://a.test/"}`)},
		{Kind: "tab-switch", Args: json.RawMessage(`{"tab":"e100"}`)},
	}})
	if e != nil || out != okOutput {
		t.Fatalf("batch = %q, %+v", out, e)
	}
	for _, br := range r.esc.brackets() {
		if br == "arm" {
			t.Errorf("a swallowing scope in an all-Headless batch: %v", r.esc.brackets())
		}
	}
	if len(ext.Activated()) != 1 {
		t.Errorf("activated %v", ext.Activated())
	}

	// On a launched browser a headed tab switch is Headed: the Batch takes
	// the swallowing scope up front; tab new alone stays Headless.
	b := launchedBrowser(t)
	r = newTabRig(t, b.Connection(t), nil, b, nil)
	w := tabTestWindows[0]
	headed := json.RawMessage(`{"tab":"cccc","headed":true}`)
	if !r.svc.stepHeadless("tab-new", nil, &w) || !r.svc.stepHeadless("tab-close", nil, &w) || r.svc.stepHeadless("tab-switch", headed, &w) {
		t.Error("launched: tab-new and tab-close Headless, a headed tab-switch Headed")
	}
	_, e = r.run(t, "batch", protocol.BatchArgs{Window: "301", Headed: true, Steps: []protocol.Step{
		{Kind: "tab-switch", Args: json.RawMessage(`{"tab":"cccc"}`)},
	}})
	if e != nil || r.esc.brackets()[0] != "arm" {
		t.Errorf("launched headed batch: %+v, %v", e, r.esc.brackets())
	}
	if b.ActiveTab(1) != tNews {
		t.Errorf("launched headed batch: active %s", b.ActiveTab(1))
	}

	// Unheaded, the switch goes through the tab strip: an all-Headless
	// Batch, one passive scope, no keys.
	sel := &stripSelector{names: []string{"Inbox", "Docs", "News"}}
	r = newTabRig(t, b.Connection(t), nil, b, sel)
	if !r.svc.stepHeadless("tab-switch", json.RawMessage(`{"tab":"aaaa"}`), &w) {
		t.Error("launched, unheaded: tab-switch Headless")
	}
	out, e = r.run(t, "batch", protocol.BatchArgs{Window: "301", Steps: []protocol.Step{
		{Kind: "tab-switch", Args: json.RawMessage(`{"tab":"aaaa"}`)},
	}})
	if e != nil || out != okOutput || strings.Join(r.esc.brackets(), ",") != "arm-passive,arm-passive,disarm-passive,disarm-passive" {
		t.Errorf("launched batch: %q, %+v, %v", out, e, r.esc.brackets())
	}
	if sel.picked != 0 || strings.Contains(strings.Join(r.log(), ","), "press") {
		t.Errorf("launched batch: picked %d, steps %v", sel.picked, r.log())
	}
	extRig := newTabRig(t, conn, nil, nil, nil)
	if !extRig.svc.stepHeadless("tab-switch", nil, &w) {
		t.Error("extension: tab-switch is Headless")
	}
	if extRig.svc.stepHeadless("tab-switch", nil, nil) {
		t.Error("no Window: Headed, the safe side")
	}
	noForegroundCalls(t, b.Methods())
}

func TestTabCommandsRegisteredAndGated(t *testing.T) {
	cmds := CommandsWith(Services{Trees: &fakeTrees{}})
	for _, name := range []string{"tab-list", "tab-new", "tab-close", "tab-switch"} {
		if _, ok := cmds[name]; !ok {
			t.Errorf("%s is not registered", name)
		}
		if _, ok := protocol.CommandTimeout(name); !ok {
			t.Errorf("%s has no budget", name)
		}
		if takesHeaded(name) != (name == "tab-switch") {
			t.Errorf("%s: takesHeaded = %v; only tab-switch takes --headed", name, takesHeaded(name))
		}
	}
	if !observationOnlyCommand("tab-list") {
		t.Error("tab list only reads")
	}
	for _, name := range []string{"tab-new", "tab-close", "tab-switch"} {
		if observationOnlyCommand(name) {
			t.Errorf("%s must run on the action gate", name)
		}
		if !batchableStep(name) {
			t.Errorf("%s is a Batch step", name)
		}
	}
	if batchableStep("tab-list") {
		t.Error("tab list prints, so it is no Batch step")
	}
}

// TestNothingSendsActivateTargetOrBringToFront scans winuse's own Go
// sources: no CDP call anywhere names Target.activateTarget or
// Page.bringToFront (the extension refuses both too; bridge tests cover
// that). The methods that reach the fakes are checked in each test above.
func TestNothingSendsActivateTargetOrBringToFront(t *testing.T) {
	root := filepath.Join("..", "..")
	call := regexp.MustCompile(`(?s)\.Call(Session)?\([^;]*?"(Target\.activateTarget|Page\.bringToFront)"`)
	method := regexp.MustCompile(`"(Target\.activateTarget|Page\.bringToFront)"`)
	scanned := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".scratch", "node_modules", "third_party":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		scanned++
		src := string(data)
		if call.MatchString(src) {
			t.Errorf("%s sends a forbidden method", path)
		}
		// The method names may appear only where they are refused: the
		// extension bridge's refusal, and the attached Electron App's shim
		// (headless-max 15), which refuses them the same way.
		switch filepath.ToSlash(path) {
		case "../../internal/bridge/bridge.go", "../../internal/browser/electron.go":
		default:
			if method.MatchString(src) {
				t.Errorf("%s names a forbidden method outside the bridge's refusal", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned < 50 {
		t.Fatalf("scanned only %d files", scanned)
	}
}

// TestTabCommandsRememberTheWindow: a Window mapped once (here by tab new,
// whose read-back confirms the window) keeps its browser window when a new
// window opens at the same default bounds with a Tab of the same title.
// Without the memory the same lookup is the ambiguity error.
func TestTabCommandsRememberTheWindow(t *testing.T) {
	ext, conn := extensionBrowser(t)
	r := newTabRig(t, conn, nil, nil, nil)
	mem := page.NewWindowMemory()
	r.svc.Pages.(*livePages).geometry = func(w win.WindowInfo) page.Geometry {
		g := tabGeometry(w)
		g.HWND, g.PID, g.Memory = w.ID, w.PID, mem
		return g
	}
	if out, e := r.run(t, "tab-new", protocol.TabArgs{Window: "302", URL: "https://a.test/"}); e != nil || !strings.HasPrefix(out, "ok ") {
		t.Fatalf("tab new = %q, %+v", out, e)
	}
	if w, ok := mem.Window(conn.Client, page.Geometry{HWND: 302, PID: 4242}); !ok || w != 2 {
		t.Fatalf("302 = %d, %v", w, ok)
	}

	// Chrome opens window 3 at window 2's place, on the same site.
	ext.Set([]bridgetest.Tab{
		{TargetID: "E2000000000000000000000000000002", Title: "Docs", URL: "https://docs.test/", TabID: 12, WindowID: 1, Active: true},
		{TargetID: "E4000000000000000000000000000004", Title: "Docs", URL: "https://docs.test/old", TabID: 21, WindowID: 2, Active: true},
		{TargetID: "E5000000000000000000000000000005", Title: "Docs", URL: "https://docs.test/old", TabID: 31, WindowID: 3, Active: true},
	}, []bridgetest.Window{
		{WindowID: 1, Left: 100, Top: 60, Width: 1200, Height: 800, State: "normal"},
		{WindowID: 2, Left: 22, Top: 22, Width: 1050, Height: 700, State: "normal"},
		{WindowID: 3, Left: 22, Top: 22, Width: 1050, Height: 700, State: "normal"},
	})

	out, e := r.run(t, "tab-list", protocol.TabArgs{Window: "302"})
	if e != nil || !strings.Contains(out, "* e4000000 \"Docs\" https://docs.test/old\n") || strings.Contains(out, "e5000000") {
		t.Errorf("tab list 302 = %q, %+v", out, e)
	}
	if _, err := page.FindTab(context.Background(), conn, tabGeometry(tabTestWindows[1])); err == nil || !strings.Contains(err.Error(), "several Tabs match") {
		t.Errorf("without the memory: %v", err)
	}
}

// stripSelector is a TabSelector over window 301's tab strip: the items'
// names, and what the Select reads back. picked is the item chosen, -1
// before any.
type stripSelector struct {
	stripActions
	names    []string
	noStrip  bool
	selErr   error
	selCalls int
	picked   int
}

func (f *stripSelector) SelectTab(ctx context.Context, hwnd uint64, pick func([]string) (int, error)) error {
	f.selCalls++
	f.picked = -1
	if hwnd != 301 {
		return fmt.Errorf("selected in window %d", hwnd)
	}
	if f.noStrip {
		return uia.ErrNoTabStrip
	}
	i, err := pick(f.names)
	if err != nil {
		return err
	}
	f.picked = i
	return f.selErr
}

func TestTabSwitchOnALaunchedBrowserIsHeadless(t *testing.T) {
	b := launchedBrowser(t)
	sel := &stripSelector{stripActions: *stripOf301(), names: []string{"Inbox", "Docs - Memory usage - 17.3 MB", "News"}}
	r := newTabRig(t, b.Connection(t), nil, b, sel)
	out, e := r.run(t, "tab-switch", protocol.TabArgs{Window: "301", Tab: "aaaa"})
	if e != nil || out != okOutput {
		t.Fatalf("switch = %q, %+v", out, e)
	}
	if sel.picked != 0 {
		t.Errorf("picked item %d, want Inbox (0)", sel.picked)
	}
	// No activation, no keys, no banner: the cursor to the strip item and a
	// passive scope.
	if got := strings.Join(r.log(), ","); got != "cursor-travel,cursor-click" {
		t.Errorf("steps = %s", got)
	}
	if got := strings.Join(r.esc.brackets(), ","); got != "arm-passive,disarm-passive" {
		t.Errorf("esc = %s", got)
	}
	if r.cursor.labels[0] != `switch to "Inbox"` || r.cursor.targets[0] != (win.Rect{Left: 110, Top: 66, Right: 300, Bottom: 96}) {
		t.Errorf("cursor %q to %+v", r.cursor.labels[0], r.cursor.targets[0])
	}
	// The title matches with the state Chrome appends.
	if out, e = r.run(t, "tab-switch", protocol.TabArgs{Window: "301", Tab: "cccc"}); e != nil || sel.picked != 2 {
		t.Errorf("News: %q, %+v, picked %d", out, e, sel.picked)
	}
	noForegroundCalls(t, b.Methods())
}

// A switch with no Headless route sends nothing and gives the --headed
// retry: winuse never switches to the keys on its own.
func TestTabSwitchStripRefusals(t *testing.T) {
	retry := "retry with --headed to switch with real keys (this brings the Window to the foreground): winuse --headed tab switch 301 aaaa0000"
	for _, c := range []struct {
		name string
		set  func(r *tabRig, sel *stripSelector)
		msg  string
	}{
		{"no item", func(r *tabRig, sel *stripSelector) { sel.names = []string{"Docs", "News"} },
			`tab aaaa0000 cannot be switched to headless: no item of the tab strip is named "Inbox"; nothing was sent`},
		{"two items", func(r *tabRig, sel *stripSelector) { sel.names = []string{"Inbox", "Inbox - Audio playing", "News"} },
			`tab aaaa0000 cannot be switched to headless: 2 items of the tab strip are named "Inbox"; nothing was sent`},
		{"no tab strip", func(r *tabRig, sel *stripSelector) { sel.noStrip = true },
			"tab aaaa0000 cannot be switched to headless: the Window shows no tab strip through UI Automation; nothing was sent"},
	} {
		b := launchedBrowser(t)
		sel := &stripSelector{names: []string{"Inbox", "Docs", "News"}}
		r := newTabRig(t, b.Connection(t), nil, b, sel)
		c.set(r, sel)
		_, e := r.run(t, "tab-switch", protocol.TabArgs{Window: "301", Tab: "aaaa"})
		if e == nil || e.Msg != c.msg || e.Hint != retry {
			t.Errorf("%s: %+v", c.name, e)
		}
		if got := strings.Join(r.log(), ","); got != "cursor-travel" {
			t.Errorf("%s: steps = %s, want the cursor's travel alone", c.name, got)
		}
		if b.ActiveTab(1) != tDocs {
			t.Errorf("%s: the Tab changed", c.name)
		}
	}

	// A minimized Window and a helper without the tab strip's side refuse
	// before the cursor moves.
	b := launchedBrowser(t)
	sel := &stripSelector{names: []string{"Inbox"}}
	r := newTabRig(t, b.Connection(t), nil, b, sel)
	r.ready = &win.MinimizedError{WindowID: 301}
	_, e := r.run(t, "tab-switch", protocol.TabArgs{Window: "301", Tab: "aaaa"})
	if e == nil || !strings.HasSuffix(e.Msg, nothingSentNote) || !strings.Contains(e.Hint, "restore-window 301") || sel.selCalls != 0 {
		t.Errorf("minimized: %+v, %d selects", e, sel.selCalls)
	}
	r = newTabRig(t, b.Connection(t), nil, b, nil)
	_, e = r.run(t, "tab-switch", protocol.TabArgs{Window: "301", Tab: "aaaa"})
	if e == nil || e.Msg != "tab switch is unavailable: no UIA client; nothing was sent" || e.Hint != retry {
		t.Errorf("no TabSelector: %+v", e)
	}
	if len(r.log()) != 0 {
		t.Errorf("steps %v", r.log())
	}
}

func TestTabSwitchStripFailuresAfterTheSelect(t *testing.T) {
	b := launchedBrowser(t)
	sel := &stripSelector{names: []string{"Inbox", "Docs", "News"}, selErr: &uia.VerifyError{Action: "selecting the tab", Expected: "selected"}}
	r := newTabRig(t, b.Connection(t), nil, b, sel)
	_, e := r.run(t, "tab-switch", protocol.TabArgs{Window: "301", Tab: "aaaa"})
	if e == nil || e.Msg != "tab aaaa0000 is not the active tab of window 301 after the switch" ||
		e.Hint != "the switch was delivered; read winuse tab list 301 before repeating it" {
		t.Errorf("not selected: %+v", e)
	}
	if got := strings.Join(r.log(), ","); got != "cursor-travel,cursor-click" {
		t.Errorf("a delivered switch ripples: %s", got)
	}
	sel.selErr = &uia.WriteError{What: "selecting the tab", Err: errors.New("E_FAIL")}
	_, e = r.run(t, "tab-switch", protocol.TabArgs{Window: "301", Tab: "aaaa"})
	if e == nil || strings.Contains(e.Msg, "nothing was sent") || !strings.Contains(e.Hint, "may have taken effect") {
		t.Errorf("failed Select: %+v", e)
	}
}

func TestStripMatches(t *testing.T) {
	names := []string{"Inbox - Memory usage - 17.3 MB", "Inbox (2)", "Docs, Audio playing", "Docs2", "  News  "}
	for _, c := range []struct {
		title string
		want  []int
	}{
		{"Inbox", []int{0, 1}}, // "Inbox (2)" carries the title too: ambiguous, refused
		{"Docs", []int{2}},
		{"News", []int{4}},
		{"Inbox (2)", []int{1}},
		{"", nil},
		{"Shop", nil},
	} {
		if got := stripMatches(names, c.title); fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("%q: %v, want %v", c.title, got, c.want)
		}
	}
}
