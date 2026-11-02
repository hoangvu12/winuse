package page

import (
	"context"
	"errors"
	"testing"

	"github.com/hoangvu12/winuse/internal/bridge/bridgetest"
	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/page/pagetest"
)

// Chrome opens new windows at one default position, so two windows of one
// site have the same bounds and the same title: the live shape seen
// through the extension.
var defaultBounds = DIPBounds{Left: 22, Top: 22, Width: 1050, Height: 700}

// windowAt is the Window hwnd at the default bounds, titled after title.
func windowAt(mem *WindowMemory, hwnd uint64, pid uint32, title string) Geometry {
	return Geometry{Title: title + " - Google Chrome", Rect: box(defaultBounds, 96), DPI: 96, HWND: hwnd, PID: pid, Memory: mem}
}

func extTab(id, title, url string, window int) bridgetest.Tab {
	return bridgetest.Tab{TargetID: id, Title: title, URL: url, TabID: len(id) + window*10, WindowID: window, Active: true}
}

func extWindow(id int) bridgetest.Window {
	return bridgetest.Window{WindowID: id, Left: defaultBounds.Left, Top: defaultBounds.Top, Width: defaultBounds.Width, Height: defaultBounds.Height, State: "normal"}
}

func wantTab(t *testing.T, what string, conn *browser.Connection, g Geometry, want string) {
	t.Helper()
	tab, err := FindTab(context.Background(), conn, g)
	if err != nil || tab.TargetID != want {
		t.Errorf("%s: %s, %v; want %s", what, tab.TargetID, err, want)
	}
}

func wantAmbiguous(t *testing.T, what string, conn *browser.Connection, g Geometry, text string) {
	t.Helper()
	_, err := FindTab(context.Background(), conn, g)
	var several *AmbiguousTabError
	if !errors.As(err, &several) || (text != "" && err.Error() != text) {
		t.Errorf("%s: %v", what, err)
	}
}

// TestRememberedWindowOnTheExtension: two windows at the same default
// bounds, each Window mapped once while their titles differed, then both
// showing "Example Domain". Each Window keeps its own browser window; a
// Window never mapped uniquely, and a lookup without the memory, still
// get the ambiguity error.
func TestRememberedWindowOnTheExtension(t *testing.T) {
	ext := bridgetest.Start(t, t.TempDir(), &bridgetest.Extension{
		Tabs: []bridgetest.Tab{
			extTab("EF717E4A0000", "Example Domain", "https://example.com/", 1),
			extTab("5AC8F8EF0000", "Other", "https://other.test/", 2),
		},
		Windows: []bridgetest.Window{extWindow(1), extWindow(2)},
	})
	conn := extensionConn(t, ext)
	mem := NewWindowMemory()
	wantTab(t, "first sight of 501", conn, windowAt(mem, 501, 5151, "Example Domain"), "EF717E4A0000")
	wantTab(t, "first sight of 502", conn, windowAt(mem, 502, 5151, "Other"), "5AC8F8EF0000")

	// Window 2 goes to the same site.
	ext.Set([]bridgetest.Tab{
		extTab("EF717E4A0000", "Example Domain", "https://example.com/", 1),
		extTab("5AC8F8EF0000", "Example Domain", "https://example.com/", 2),
	}, []bridgetest.Window{extWindow(1), extWindow(2)})
	wantAmbiguous(t, "no memory", conn, windowAt(nil, 501, 5151, "Example Domain"),
		`several Tabs match this Window's title and bounds: "Example Domain" (target EF717E4A), "Example Domain" (target 5AC8F8EF)`)
	wantTab(t, "501 remembered", conn, windowAt(mem, 501, 5151, "Example Domain"), "EF717E4A0000")
	wantTab(t, "502 remembered", conn, windowAt(mem, 502, 5151, "Example Domain"), "5AC8F8EF0000")
	wantAmbiguous(t, "503, never mapped", conn, windowAt(mem, 503, 5151, "Example Domain"), "")
	wantAmbiguous(t, "501 of another process", conn, windowAt(mem, 501, 777, "Example Domain"), "")
	// ListWindowTabs (the tab commands) goes through the same lookup.
	if wt, err := ListWindowTabs(context.Background(), conn, windowAt(mem, 502, 5151, "Example Domain")); err != nil || wt.WindowID != 2 {
		t.Errorf("tab list of 502: window %d, %v", wt.WindowID, err)
	}

	// Window 1 closes: its entry goes with it, 502's stays.
	ext.Set([]bridgetest.Tab{extTab("5AC8F8EF0000", "Example Domain", "https://example.com/", 2)}, []bridgetest.Window{extWindow(2)})
	wantTab(t, "502 alone", conn, windowAt(mem, 502, 5151, "Example Domain"), "5AC8F8EF0000")
	if _, ok := mem.Window(conn.Client, windowAt(nil, 501, 5151, "")); ok {
		t.Error("501 is still remembered after its browser window closed")
	}
	if w, ok := mem.Window(conn.Client, windowAt(nil, 502, 5151, "")); !ok || w != 2 {
		t.Errorf("502 = %d, %v", w, ok)
	}

	// A new window 3 at the same place, ambiguous with window 2: 502 is
	// still told apart.
	ext.Set([]bridgetest.Tab{
		extTab("5AC8F8EF0000", "Example Domain", "https://example.com/", 2),
		extTab("C3C3C3C30000", "Example Domain", "https://example.com/", 3),
	}, []bridgetest.Window{extWindow(2), extWindow(3)})
	wantTab(t, "502 next to a new window", conn, windowAt(mem, 502, 5151, "Example Domain"), "5AC8F8EF0000")

	// The connection is dialed again: window ids belong to the old one, so
	// the entry is forgotten and the ambiguity is back.
	again := extensionConn(t, ext)
	wantAmbiguous(t, "after a re-dial", again, windowAt(mem, 502, 5151, "Example Domain"), "")
	if _, ok := mem.Window(conn.Client, windowAt(nil, 502, 5151, "")); ok {
		t.Error("502 is still remembered after the re-dial")
	}
}

// TestRememberedWindowOnALaunchedBrowser: the same over CDP, where the
// window ids come from Browser.getWindowForTarget and no Tab says whether
// it is active, so a background Tab of the same title in the remembered
// window keeps the error.
func TestRememberedWindowOnALaunchedBrowser(t *testing.T) {
	w1 := pagetest.Window{ID: 1, Left: 22, Top: 22, Width: 1050, Height: 700, Tabs: []pagetest.Tab{
		{TargetID: "A1000000000000000000000000000000", Title: "Example Domain", URL: "https://example.com/"},
	}}
	w2 := pagetest.Window{ID: 2, Left: 22, Top: 22, Width: 1050, Height: 700, Tabs: []pagetest.Tab{
		{TargetID: "B2000000000000000000000000000000", Title: "Other", URL: "https://other.test/"},
	}}
	b := pagetest.New(t, w1, w2)
	conn := b.Connection(t)
	mem := NewWindowMemory()
	wantTab(t, "first sight", conn, windowAt(mem, 601, 4242, "Example Domain"), "A1000000000000000000000000000000")

	w2.Tabs[0].Title = "Example Domain"
	b.Set(w1, w2)
	wantAmbiguous(t, "no memory", conn, windowAt(nil, 601, 4242, "Example Domain"), "")
	wantTab(t, "remembered", conn, windowAt(mem, 601, 4242, "Example Domain"), "A1000000000000000000000000000000")
	wantAmbiguous(t, "602, never mapped", conn, windowAt(mem, 602, 4242, "Example Domain"), "")

	// Two candidates in the remembered window: still ambiguous, and kept.
	w1.Tabs = append(w1.Tabs, pagetest.Tab{TargetID: "A1BB000000000000000000000000000B", Title: "Example Domain", URL: "https://example.com/b"})
	b.Set(w1, w2)
	wantAmbiguous(t, "two in the window", conn, windowAt(mem, 601, 4242, "Example Domain"), "")
	if w, ok := mem.Window(conn.Client, windowAt(nil, 601, 4242, "")); !ok || w != 1 {
		t.Errorf("601 = %d, %v", w, ok)
	}

	// Window 1 is gone and two others share its place: no candidate in
	// the remembered window, so the entry is forgotten.
	w3 := w2
	w3.ID = 3
	w3.Tabs = []pagetest.Tab{{TargetID: "C3000000000000000000000000000000", Title: "Example Domain", URL: "https://example.com/"}}
	b.Set(w2, w3)
	wantAmbiguous(t, "window 1 gone", conn, windowAt(mem, 601, 4242, "Example Domain"), "")
	if _, ok := mem.Window(conn.Client, windowAt(nil, 601, 4242, "")); ok {
		t.Error("601 is still remembered after its browser window closed")
	}
}

// TestWindowMemoryEntries: one Window per browser window, ended
// connections, and what is never remembered.
func TestWindowMemoryEntries(t *testing.T) {
	b := pagetest.New(t)
	c1, c2 := b.Connection(t).Client, b.Connection(t).Client
	mem := NewWindowMemory()
	g := func(hwnd uint64) Geometry { return Geometry{HWND: hwnd, PID: 4242} }

	// A browser window has one Window: a recycled handle mapping to it
	// takes it over.
	mem.Remember(c1, g(701), 1)
	mem.Remember(c1, g(702), 1)
	if _, ok := mem.Window(c1, g(701)); ok {
		t.Error("two Windows remembered for browser window 1")
	}
	if w, ok := mem.Window(c1, g(702)); !ok || w != 1 {
		t.Errorf("702 = %d, %v", w, ok)
	}
	// The same window id on another connection is another browser window.
	mem.Remember(c2, g(703), 1)
	if _, ok := mem.Window(c1, g(702)); !ok {
		t.Error("another connection's window 1 took 702's entry")
	}

	// An ended connection's entries go at the next write.
	c1.Close()
	mem.Remember(c2, g(704), 2)
	if _, ok := mem.Window(c1, g(702)); ok {
		t.Error("an ended connection's entry is still there")
	}

	// No handle, no memory: nothing is remembered.
	mem.Remember(c2, g(0), 5)
	if _, ok := mem.Window(c2, g(0)); ok {
		t.Error("a Window with no handle was remembered")
	}
	var none *WindowMemory
	none.Remember(c2, g(705), 1)
	if _, ok := none.Window(c2, g(705)); ok {
		t.Error("a nil memory remembered")
	}
}
