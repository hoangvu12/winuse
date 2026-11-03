//go:build e2e

// The ContextMenu Secondary Action (headless-max ticket 09):
// `perform-secondary-action <window> <element> ContextMenu` on the covered
// fixture's Classic Dialog name Edit opens the Edit's own `#32768` menu
// through a posted WM_CONTEXTMENU, reports its Window ID, and the cover
// keeps the foreground. The menu's Window State names its items; a click
// on "Select All" posts its mnemonic and selects the Edit's text;
// `press-key <menu> Escape` closes a second menu; a third one closes when
// the same Agent's next Action targets another Window. Every menu closes
// with the fixture at the latest. The browser leg opens a page input's
// menu in a covered browser started on the fixture page (no Browser
// Connection) and closes it with Escape through the window-message route,
// which no probe measured on a menu popup. User-run only (see the package
// comment and test/e2e/README.md); an agent must never start them.

package e2e

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
	"github.com/hoangvu12/winuse/internal/win"
)

// contextMenuAgent names one Agent for every call of a test, so the menu
// it opens is that Agent's.
const contextMenuAgent = "WINUSE_AGENT=e2e-context-menu"

// menuIDPattern reads the menu's Window ID from the ContextMenu's ok.
var menuIDPattern = regexp.MustCompile(`get-window-state (\d+)`)

// runAsMenuAgent runs `winuse <args>` as the test's one Agent.
func runAsMenuAgent(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	return runWinuse(t, args, contextMenuAgent, "WINUSE_HELPER_IDLE="+e2eIdle)
}

// openContextMenu runs the ContextMenu Secondary Action and returns the
// menu's Window ID, checking the cover kept the foreground.
func openContextMenu(t *testing.T, window, element string, cover uint64) uint64 {
	t.Helper()
	stdout, stderr, exit := runAsMenuAgent(t, "perform-secondary-action", window, element, "ContextMenu")
	if exit != 0 || !strings.HasPrefix(stdout, "ok\nNote: the context menu opened") {
		t.Fatalf("perform-secondary-action ContextMenu: exit %d, stdout %q, stderr %q", exit, stdout, stderr)
	}
	m := menuIDPattern.FindStringSubmatch(stdout)
	if m == nil {
		t.Fatalf("the ContextMenu's output names no menu: %q", stdout)
	}
	id, _ := strconv.ParseUint(m[1], 10, 64)
	assertForeground(t, "perform-secondary-action ContextMenu", cover)
	return id
}

// waitMenuClosed waits for a menu window to go.
func waitMenuClosed(t *testing.T, what string, menu uint64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !win.WindowOpen(menu) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("%s: menu %d is still open", what, menu)
}

func TestContextMenuOnACoveredEdit(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)
	dialog := openClassicDialog(t, fix, window, cover)
	id := mustWindowID(t, dialog)

	stdout, stderr, exit := runTypeText(t, []string{dialog, "alpha bravo"})
	assertActionOK(t, "type-text into the name Edit", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicText == "alpha bravo" })
	edit := win.FocusedControl(id)
	if !edit.IsEdit() {
		t.Fatalf("the dialog's focused control is %q, not its name Edit", edit.Class)
	}
	if _, stderr, exit := runAsMenuAgent(t, "get-window-state", dialog); exit != 0 {
		t.Fatalf("get-window-state: exit %d, stderr:\n%s", exit, stderr)
	}
	ref := "id:" + strconv.Itoa(fixture.ClassicEditID)

	// Open, read the menu, choose Select All by its mnemonic.
	menu := openContextMenu(t, dialog, ref, cover)
	if w, ok := windowByID(t, menu); !ok || w.Class != win.MenuClass || w.PID != uint32(fix.proc.Pid) {
		t.Fatalf("menu %d is %+v, want a #32768 of the fixture", menu, w)
	}
	menuRef := strconv.FormatUint(menu, 10)
	state, stderr, exit := runAsMenuAgent(t, "get-window-state", menuRef)
	if exit != 0 || !strings.Contains(state, "Select All") {
		t.Fatalf("get-window-state of the menu: exit %d, stderr %q, state:\n%s", exit, stderr, state)
	}
	stdout, stderr, exit = runAsMenuAgent(t, "click", menuRef, "name:Select All")
	assertActionOK(t, "click Select All in the menu", stdout, stderr, exit)
	waitMenuClosed(t, "click Select All", menu)
	assertForeground(t, "click Select All in the menu", cover)
	st, err := win.ReadEditState(edit.HWND)
	if err != nil {
		t.Fatalf("reading the Edit's selection: %v", err)
	}
	if !st.SelKnown || min(st.SelStart, st.SelEnd) != 0 || max(st.SelStart, st.SelEnd) != len("alpha bravo") {
		t.Errorf("after Select All the Edit selects %d to %d, want all of it", st.SelStart, st.SelEnd)
	}

	// Escape closes the second menu.
	menu = openContextMenu(t, dialog, ref, cover)
	stdout, stderr, exit = runAsMenuAgent(t, "press-key", strconv.FormatUint(menu, 10), "Escape")
	assertActionOK(t, "press-key <menu> Escape", stdout, stderr, exit)
	waitMenuClosed(t, "press-key Escape", menu)
	assertForeground(t, "press-key <menu> Escape", cover)

	// The third closes on the Agent's next Action on another Window: a
	// check box of the cover, the test's own fixture.
	menu = openContextMenu(t, dialog, ref, cover)
	coverRef := strconv.FormatUint(cover, 10)
	if _, stderr, exit := runAsMenuAgent(t, "get-window-state", coverRef); exit != 0 {
		t.Fatalf("get-window-state of the cover: exit %d, stderr:\n%s", exit, stderr)
	}
	stdout, stderr, exit = runAsMenuAgent(t, "click", coverRef, strconv.Itoa(mainElementIndexes()[fixture.ElemCheckboxNotify]))
	assertActionOK(t, "click on the cover", stdout, stderr, exit)
	if !strings.Contains(stdout, "Note: closed context menu "+strconv.FormatUint(menu, 10)) {
		t.Errorf("the click on the cover does not say it closed the menu: %q", stdout)
	}
	waitMenuClosed(t, "the Agent's next Action elsewhere", menu)
	assertForeground(t, "the Agent's next Action elsewhere", cover)
}

// windowByID finds a window in the unfiltered enumeration.
func windowByID(t *testing.T, id uint64) (win.WindowInfo, bool) {
	t.Helper()
	all, err := win.Collect()
	if err != nil {
		t.Fatalf("enumerating windows: %v", err)
	}
	for _, w := range all {
		if w.ID == id {
			return w, true
		}
	}
	return win.WindowInfo{}, false
}

// TestContextMenuOnACoveredBrowserPage: the browser leg. Started with the
// occlusion flag (headless-max 04: a covered browser without it has no
// page tree), so the input is in the Window State.
func TestContextMenuOnACoveredBrowserPage(t *testing.T) {
	optIn(t)
	browsers := findChromiumBrowsers()
	if len(browsers) == 0 {
		t.Skip("no Chromium browser (Edge or Chrome) found in the standard install locations")
	}
	page, err := filepath.Abs("testdata/chromium-fixture.html")
	if err != nil {
		t.Fatalf("resolving the fixture page: %v", err)
	}
	url := "file:///" + strings.ReplaceAll(filepath.ToSlash(page), " ", "%20")
	// The cleanup's terminate closes any menu left open.
	pid := startIsolatedBrowser(t, browsers[0].exe, "--new-window", "--disable-features=CalculateNativeWinOcclusion", url)

	window := strconv.FormatUint(findIsolatedWindow(t, pid, chromiumPageTitle, 30*time.Second), 10)
	cover := coverWindow(t, window)
	state, stderr, exit := runAsMenuAgent(t, "get-window-state", window)
	if exit != 0 {
		t.Fatalf("get-window-state: exit %d, stderr:\n%s", exit, stderr)
	}
	input := strconv.Itoa(webElementIndex(t, state, "edit", "winuse e2e input"))

	stdout, stderr, exit := runAsMenuAgent(t, "perform-secondary-action", window, input, "ContextMenu")
	if exit != 0 || !strings.Contains(stdout, "act on the browser's focus") {
		t.Fatalf("perform-secondary-action ContextMenu: exit %d, stdout %q, stderr %q", exit, stdout, stderr)
	}
	m := menuIDPattern.FindStringSubmatch(stdout)
	if m == nil {
		t.Fatalf("the ContextMenu's output names no menu: %q", stdout)
	}
	menu, _ := strconv.ParseUint(m[1], 10, 64)
	assertForeground(t, "ContextMenu on a page input", cover)
	if w, ok := windowByID(t, menu); !ok || w.Class != win.ChromiumClass {
		t.Fatalf("menu %d is %+v, want a Chromium menu popup", menu, w)
	}

	// Unmeasured: Escape through the window-message route on the popup.
	stdout, stderr, exit = runAsMenuAgent(t, "press-key", m[1], "Escape")
	if exit != 0 {
		t.Errorf("press-key <menu> Escape: exit %d, stdout %q, stderr %q", exit, stdout, stderr)
	}
	waitMenuClosed(t, "press-key Escape on the browser's menu", menu)
	assertForeground(t, "press-key Escape on the browser's menu", cover)
}
