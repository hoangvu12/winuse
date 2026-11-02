package page

// Offline tests for the Tabs of a browser Window (ticket 07): short ids
// and their lookup, listing only the Window's own browser window over a
// launched browser and over the extension, where new Tabs land, and the
// visibility read the Headed switch verifies with.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/bridge"
	"github.com/hoangvu12/winuse/internal/bridge/bridgetest"
	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/page/pagetest"
)

func TestTabIDIsStableAndShort(t *testing.T) {
	id := "9F2C61AB0D3E4F5A6B7C8D9E0F1A2B3C"
	if got := TabID(id); got != "9f2c61ab" {
		t.Errorf("TabID = %q", got)
	}
	if TabID(id) != TabID(id) || TabID("AB12") != "ab12" {
		t.Error("TabID must depend on the target id alone")
	}
}

func TestResolveTab(t *testing.T) {
	tabs := []Tab{
		{TargetID: "9F2C61AB0D3E4F5A6B7C8D9E0F1A2B3C", Title: "Inbox"},
		{TargetID: "9F2C77000D3E4F5A6B7C8D9E0F1A2B3C", Title: "Docs"},
		{TargetID: "1234ABCD0D3E4F5A6B7C8D9E0F1A2B3C", Title: "News"},
	}
	for _, c := range []struct {
		ref, want, err string
	}{
		{ref: "9f2c61ab", want: "Inbox"},
		{ref: "9F2C61", want: "Inbox"},
		{ref: "1234", want: "News"},
		{ref: " 1234abcd ", want: "News"},
		{ref: "9f2c", err: `tab id "9f2c" matches 2 tabs (9f2c61ab "Inbox", 9f2c7700 "Docs"); use more characters`},
		{ref: "abc", err: `tab id "abc" is too short: use at least 4 characters of an id from tab list`},
		{ref: "ffff", err: `no tab has the id "ffff"`},
	} {
		got, err := ResolveTab(tabs, c.ref)
		if c.err != "" {
			var re *TabRefError
			if !errors.As(err, &re) || err.Error() != c.err {
				t.Errorf("%q: err = %v, want %q", c.ref, err, c.err)
			}
			continue
		}
		if err != nil || got.Title != c.want {
			t.Errorf("%q: %+v, %v", c.ref, got, err)
		}
	}
}

func TestTabURL(t *testing.T) {
	for in, want := range map[string]string{
		"":                      "about:blank",
		"  ":                    "about:blank",
		"about:blank":           "about:blank",
		"example.com":           "https://example.com",
		"http://localhost:3000": "http://localhost:3000",
		"file:///C:/a.html":     "file:///C:/a.html",
	} {
		if got := tabURL(in); got != want {
			t.Errorf("tabURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// twoWindows is a launched browser with two windows: window 1 holds three
// Tabs (Docs selected), window 2 two Tabs, one of them titled like
// window 1's selected Tab.
func twoWindows(t *testing.T) *pagetest.Browser {
	return pagetest.New(t,
		pagetest.Window{ID: 1, Left: 100, Top: 60, Width: 1200, Height: 800, Active: 1, Tabs: []pagetest.Tab{
			{TargetID: "AAAA000000000000000000000000000A", Title: "Inbox", URL: "https://mail.test/"},
			{TargetID: "BBBB000000000000000000000000000B", Title: "Docs", URL: "https://docs.test/"},
			{TargetID: "CCCC000000000000000000000000000C", Title: "News", URL: "https://news.test/"},
		}},
		pagetest.Window{ID: 2, Left: 22, Top: 22, Width: 1050, Height: 700, Tabs: []pagetest.Tab{
			{TargetID: "DDDD000000000000000000000000000D", Title: "Docs", URL: "https://docs.test/old"},
			{TargetID: "EEEE000000000000000000000000000E", Title: "Shop", URL: "https://shop.test/"},
		}},
	)
}

func windowOne() Geometry {
	return Geometry{Title: "Docs - Google Chrome", Rect: box(DIPBounds{Left: 100, Top: 60, Width: 1200, Height: 800}, 96), DPI: 96}
}

func TestListWindowTabsOnALaunchedBrowser(t *testing.T) {
	b := twoWindows(t)
	conn := b.Connection(t)
	wt, err := ListWindowTabs(context.Background(), conn, windowOne())
	if err != nil {
		t.Fatal(err)
	}
	if wt.WindowID != 1 || wt.Active.TargetID != "BBBB000000000000000000000000000B" {
		t.Errorf("window %d, active %s", wt.WindowID, wt.Active.TargetID)
	}
	var got []string
	for _, tab := range wt.Tabs {
		mark := ""
		if wt.IsActive(tab) {
			mark = "*"
		}
		got = append(got, mark+tab.Title)
	}
	// Only window 1's Tabs, in the order the browser listed them; the
	// service worker and window 2's "Docs" are left out.
	if strings.Join(got, ",") != "News,*Docs,Inbox" {
		t.Errorf("tabs = %v", got)
	}
}

func TestListWindowTabsOnTheExtension(t *testing.T) {
	ext := bridgetest.Start(t, t.TempDir(), &bridgetest.Extension{
		Tabs: []bridgetest.Tab{
			{TargetID: "E1", Title: "Inbox", URL: "https://mail.test/", TabID: 11, WindowID: 1, Active: false},
			{TargetID: "E2", Title: "Docs", URL: "https://docs.test/", TabID: 12, WindowID: 1, Active: true},
			{TargetID: "E3", Title: "Docs", URL: "https://docs.test/old", TabID: 21, WindowID: 2, Active: true},
		},
		Windows: []bridgetest.Window{
			{WindowID: 1, Left: 100, Top: 60, Width: 1200, Height: 800, State: "normal"},
			{WindowID: 2, Left: 22, Top: 22, Width: 1050, Height: 700, State: "normal"},
		},
	})
	conn := extensionConn(t, ext)
	wt, err := ListWindowTabs(context.Background(), conn, windowOne())
	if err != nil {
		t.Fatal(err)
	}
	if len(wt.Tabs) != 2 || wt.Tabs[0].TargetID != "E1" || wt.Tabs[1].TargetID != "E2" || wt.IsActive(wt.Tabs[0]) || !wt.IsActive(wt.Tabs[1]) {
		t.Errorf("tabs = %+v", wt.Tabs)
	}
	for _, cmd := range ext.Commands() {
		if strings.HasPrefix(cmd.Method, "Browser.") {
			t.Errorf("%s reached the extension", cmd.Method)
		}
	}
}

func TestIsTabTarget(t *testing.T) {
	for _, c := range []struct {
		typ, url      string
		tabID         bool
		tab, internal bool
	}{
		{"page", "https://docs.test/", false, true, false},
		{"page", "about:blank", false, true, false},
		{"page", "chrome://newtab/", false, true, true},
		{"page", "chrome://new-tab-page/", false, true, true},
		{"page", "edge://newtab/", false, true, true},
		{"page", "chrome://settings/passwords", false, true, true},
		{"page", "chrome://tab-search.top-chrome/", false, false, true},
		{"page", "chrome://Read-Later.Top-Chrome/", false, false, true},
		{"page", "devtools://devtools/bundled/devtools_app.html", false, false, true},
		{"page", "chrome-extension://abcdefghijklmnop/popup.html", false, false, true},
		{"page", "chrome-untrusted://lens-overlay/", false, false, true},
		// Through the extension a tabId proves a tab of the strip.
		{"page", "chrome-extension://abcdefghijklmnop/options.html", true, true, true},
		{"page", "chrome://newtab/", true, true, true},
		{"service_worker", "https://a.test/sw.js", false, false, false},
		{"iframe", "https://pay.test/", false, false, false},
	} {
		tab, internal := IsTabTarget(c.typ, c.url, c.tabID)
		if tab != c.tab || (tab && internal != c.internal) {
			t.Errorf("IsTabTarget(%q, %q, %v) = %v, %v; want %v, %v", c.typ, c.url, c.tabID, tab, internal, c.tab, c.internal)
		}
	}
}

// newTabWindow is a launched browser whose window 1 shows its New Tab
// page, beside a site Tab, with page targets that are no Tabs mapped to
// the same window: a DevTools window, tab search and an extension popup.
func newTabWindow(t *testing.T) *pagetest.Browser {
	b := pagetest.New(t,
		pagetest.Window{ID: 1, Left: 100, Top: 60, Width: 1200, Height: 800, Active: 1, Tabs: []pagetest.Tab{
			{TargetID: "AAAA000000000000000000000000000A", Title: "Inbox", URL: "https://mail.test/"},
			{TargetID: "4E70000000000000000000000000000B", Title: "New Tab", URL: "chrome://newtab/"},
		}},
	)
	b.Others = []pagetest.Other{
		{TargetID: "DE00", Type: "page", Title: "DevTools - mail.test/", URL: "devtools://devtools/bundled/devtools_app.html", Window: 1},
		{TargetID: "5EA0", Type: "page", Title: "New Tab", URL: "chrome://tab-search.top-chrome/", Window: 1},
		{TargetID: "E0E0", Type: "page", Title: "New Tab", URL: "chrome-extension://abcdefghijklmnop/popup.html", Window: 1},
	}
	return b
}

func newTabGeometry() Geometry {
	return Geometry{Title: "New Tab - Google Chrome", Rect: box(DIPBounds{Left: 100, Top: 60, Width: 1200, Height: 800}, 96), DPI: 96}
}

// TestListWindowTabsWithABrowserPageOnALaunchedBrowser: the Window maps
// to its New Tab page, marked Internal, and only the strip's Tabs are
// listed; nothing is attached.
func TestListWindowTabsWithABrowserPageOnALaunchedBrowser(t *testing.T) {
	b := newTabWindow(t)
	conn := b.Connection(t)
	wt, err := ListWindowTabs(context.Background(), conn, newTabGeometry())
	if err != nil {
		t.Fatal(err)
	}
	if wt.Active.TargetID != "4E70000000000000000000000000000B" || !wt.Active.Internal || wt.WindowID != 1 {
		t.Errorf("active = %+v", wt.Active)
	}
	var got []string
	for _, tab := range wt.Tabs {
		got = append(got, fmt.Sprintf("%s %v %v", tab.URL, tab.Internal, wt.IsActive(tab)))
	}
	if strings.Join(got, ",") != "chrome://newtab/ true true,https://mail.test/ false false" {
		t.Errorf("tabs = %v", got)
	}
	for _, m := range b.Methods() {
		if m == "Target.attachToTarget" {
			t.Error("listing attached a Tab")
		}
	}
}

// TestBrowserPageTabsOnTheExtension: chrome.tabs' tabs that show browser
// pages are Tabs, the active one maps, and chrome.debugger is never asked
// to attach to them. Winuse.navigateTab takes the Tab to a site, which
// can then be attached.
func TestBrowserPageTabsOnTheExtension(t *testing.T) {
	ext := bridgetest.Start(t, t.TempDir(), &bridgetest.Extension{
		Tabs: []bridgetest.Tab{
			{TargetID: "E1", Title: "Inbox", URL: "https://mail.test/", TabID: 11, WindowID: 1},
			{TargetID: "E2", Title: "Thẻ mới", URL: "chrome://newtab/", TabID: 12, WindowID: 1, Active: true},
			{TargetID: "E3", Title: "Options", URL: "chrome-extension://abcdefghijklmnop/options.html", TabID: 13, WindowID: 1},
		},
		Windows: []bridgetest.Window{{WindowID: 1, Left: 100, Top: 60, Width: 1200, Height: 800, State: "normal"}},
	})
	conn := extensionConn(t, ext)
	g := newTabGeometry()
	g.Title = "Thẻ mới - Google Chrome"
	wt, err := ListWindowTabs(context.Background(), conn, g)
	if err != nil {
		t.Fatal(err)
	}
	if wt.Active.TargetID != "E2" || !wt.Active.Internal || len(wt.Tabs) != 3 || !wt.Tabs[2].Internal || wt.Tabs[0].Internal {
		t.Errorf("tabs = %+v, active %+v", wt.Tabs, wt.Active)
	}
	if _, err := conn.AttachPage(context.Background(), "E2"); err == nil || !strings.Contains(err.Error(), "Cannot access a chrome:// URL") {
		t.Errorf("the fake attached to a browser page: %v", err)
	}

	if err := NavigateTabThroughExtension(context.Background(), conn.Client, "E2", "https://a.test/"); err != nil {
		t.Fatal(err)
	}
	if got := ext.Navigated(); len(got) != 1 || got[0] != "E2 https://a.test/" {
		t.Errorf("navigated %v", got)
	}
	now, err := TabNow(context.Background(), conn.Client, "E2")
	if err != nil || now.URL != "https://a.test/" || now.Internal {
		t.Errorf("after navigateTab: %+v, %v", now, err)
	}
	if _, err := conn.AttachPage(context.Background(), "E2"); err != nil {
		t.Errorf("attaching to the site: %v", err)
	}
	if _, err := TabNow(context.Background(), conn.Client, "E9"); !errors.Is(err, ErrTabGone) {
		t.Errorf("a missing Tab: %v", err)
	}
	for _, cmd := range ext.Commands() {
		if strings.HasPrefix(cmd.Method, "Browser.") || cmd.Method == "Page.navigate" {
			t.Errorf("%s reached the extension", cmd.Method)
		}
	}
}

func extensionConn(t *testing.T, ext *bridgetest.Extension) *browser.Connection {
	t.Helper()
	bc, err := bridge.Dial(context.Background(), ext.Info.Addr(), ext.Info.Token)
	if err != nil {
		t.Fatal(err)
	}
	c := cdp.NewClient(bc)
	t.Cleanup(func() { c.Close() })
	return &browser.Connection{Browser: "chrome", Mode: browser.ModeExtension, Client: c}
}

func TestNewTabOnALaunchedBrowser(t *testing.T) {
	b := twoWindows(t)
	conn := b.Connection(t)
	id, err := NewTab(context.Background(), conn, 1, "example.com")
	if err != nil || len(b.Window(1).Tabs) != 4 {
		t.Fatalf("new tab %s: %v", id, err)
	}
	var params map[string]any
	for _, m := range b.Srv.Received() {
		if m.Method == "Target.createTarget" {
			json.Unmarshal(m.Params, &params)
		}
	}
	// Background, in no new window; CDP has no window parameter.
	if params["background"] != true || params["newWindow"] != false || params["url"] != "https://example.com" || params["windowId"] != nil {
		t.Errorf("createTarget params = %v", params)
	}

	// Chrome puts it in the last active window, which is not window 2.
	id, err = NewTab(context.Background(), conn, 2, "")
	var other *OtherWindowError
	if !errors.As(err, &other) || other.WindowID != 1 || id == "" {
		t.Fatalf("landing elsewhere: %s, %v", id, err)
	}
}

func TestNewTabOnTheExtensionNamesTheWindow(t *testing.T) {
	ext := bridgetest.Start(t, t.TempDir(), &bridgetest.Extension{
		Tabs: []bridgetest.Tab{{TargetID: "E1", Title: "Inbox", TabID: 11, WindowID: 1, Active: true},
			{TargetID: "E3", Title: "Shop", TabID: 21, WindowID: 2, Active: true}},
	})
	conn := extensionConn(t, ext)
	id, err := NewTab(context.Background(), conn, 2, "")
	if err != nil || id == "" {
		t.Fatalf("new tab: %s, %v", id, err)
	}
	for _, cmd := range ext.Commands() {
		if cmd.Method == "Target.createTarget" {
			var p map[string]any
			json.Unmarshal(cmd.Params, &p)
			if p["windowId"] != float64(2) || p["background"] != true || p["url"] != "about:blank" {
				t.Errorf("createTarget params = %v", p)
			}
		}
	}
}

func TestVisibility(t *testing.T) {
	b := twoWindows(t)
	conn := b.Connection(t)
	v := NewVisibility(conn)
	ctx := context.Background()
	if on, err := v.Visible(ctx, "BBBB000000000000000000000000000B"); err != nil || !on {
		t.Errorf("the selected Tab: %v, %v", on, err)
	}
	if on, err := v.Visible(ctx, "AAAA000000000000000000000000000A"); err != nil || on {
		t.Errorf("a background Tab: %v, %v", on, err)
	}
	v.Visible(ctx, "BBBB000000000000000000000000000B")
	attaches, detaches := 0, 0
	v.Close(ctx)
	for _, m := range b.Methods() {
		switch m {
		case "Target.attachToTarget":
			attaches++
		case "Target.detachFromTarget":
			detaches++
		}
	}
	if attaches != 2 || detaches != 2 {
		t.Errorf("attaches %d, detaches %d; want one session per Tab, all detached", attaches, detaches)
	}
}
