//go:build e2e

// The ContextMenu Secondary Action's three routes hm-09b measured (headless-max
// ticket 09), each on a covered target, the cover keeping the foreground
// throughout:
//
//   - the Classic Dialog's bare control, whose UI Automation provider
//     reports an empty FrameworkId: a posted WM_CONTEXTMENU opens its
//     `#32768`, an item is chosen by its mnemonic and Escape closes it;
//   - a WinForms ContextMenuStrip (WINUSE_E2E_WINFORMS_APP): a posted
//     WM_CONTEXTMENU opens its drop-down, UIA Invoke chooses an item, and
//     press-key Escape and the Agent's next Action elsewhere close it;
//   - a WinUI 3 MenuFlyout (WINUSE_E2E_WINUI_APP): ShowContextMenu opens a
//     PopupHost, the same three steps.
//
// The two App legs need a prebuilt measuring App each (test/e2e/README.md,
// "Context menus in WinForms and WinUI 3"); each skips without its
// variable. User-run only (see the package comment and
// test/e2e/README.md); an agent must never start them.

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
	"github.com/hoangvu12/winuse/internal/win"
)

func TestContextMenuOnACoveredBareControl(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)
	dialog := openClassicDialog(t, fix, window, cover)
	if _, stderr, exit := runAsMenuAgent(t, "get-window-state", dialog); exit != 0 {
		t.Fatalf("get-window-state: exit %d, stderr:\n%s", exit, stderr)
	}
	ref := "id:" + strconv.Itoa(fixture.ClassicBareID)

	// Open: the empty FrameworkId takes the classic post, and the bare
	// control's own `#32768` opens.
	menu := openContextMenu(t, dialog, ref, cover)
	if w, ok := windowByID(t, menu); !ok || w.Class != win.MenuClass || w.PID != uint32(fix.proc.Pid) {
		t.Fatalf("menu %d is %+v, want a #32768 of the fixture", menu, w)
	}
	menuRef := strconv.FormatUint(menu, 10)
	state, stderr, exit := runAsMenuAgent(t, "get-window-state", menuRef)
	if exit != 0 || !strings.Contains(state, "Fill Name") {
		t.Fatalf("get-window-state of the menu: exit %d, stderr %q, state:\n%s", exit, stderr, state)
	}
	stdout, stderr, exit := runAsMenuAgent(t, "click", menuRef, "name:Fill Name")
	assertActionOK(t, "click Fill Name in the menu", stdout, stderr, exit)
	waitMenuClosed(t, "click Fill Name", menu)
	assertForeground(t, "click Fill Name in the menu", cover)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool {
		return s.ClassicMenu == "fill-name" && s.ClassicText == fixture.ClassicFillText
	})

	// Escape closes the second menu.
	menu = openContextMenu(t, dialog, ref, cover)
	stdout, stderr, exit = runAsMenuAgent(t, "press-key", strconv.FormatUint(menu, 10), "Escape")
	assertActionOK(t, "press-key <menu> Escape", stdout, stderr, exit)
	waitMenuClosed(t, "press-key Escape", menu)
	assertForeground(t, "press-key <menu> Escape", cover)
}

func TestContextMenuOnACoveredWinFormsApp(t *testing.T) {
	exe := menuAppExe(t, "WINUSE_E2E_WINFORMS_APP", "a WinForms ContextMenuStrip")
	contextMenuAppLeg(t, exe, "winuse e2e WinForms menu", func(w win.WindowInfo) bool { return win.WinFormsDropDown(w.Class) })
}

func TestContextMenuOnACoveredWinUIApp(t *testing.T) {
	exe := menuAppExe(t, "WINUSE_E2E_WINUI_APP", "a WinUI 3 MenuFlyout")
	contextMenuAppLeg(t, exe, "winuse e2e WinUI menu", func(w win.WindowInfo) bool { return w.Class == win.PopupHostClass })
}

// menuAppExe is the prebuilt measuring App named by env, or skips.
func menuAppExe(t *testing.T, env, what string) string {
	t.Helper()
	optIn(t)
	exe := os.Getenv(env)
	if exe == "" {
		t.Skipf("%s is not set: point it at the measuring App with %s (test/e2e/README.md, \"Context menus in WinForms and WinUI 3\")", env, what)
	}
	return exe
}

// contextMenuAppLeg starts a measuring App (`<exe> <state-file> <x> <y>`:
// a Window titled title, shown without activation, whose TextBox "box"
// has a context menu of Alpha and Bravo; the state file gets "picked
// <item>" lines), covers it, and runs the open, the pick, Escape and the
// close on the Agent's next Action elsewhere, checking the cover keeps
// the foreground. isMenu says the opened Window is the App's menu kind.
func contextMenuAppLeg(t *testing.T, exe, title string, isMenu func(win.WindowInfo) bool) {
	t.Helper()
	statePath := filepath.Join(t.TempDir(), "menu-app.log")
	cmd := exec.Command(exe, statePath, "120", "120")
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting %s: %v", exe, err)
	}
	pid := uint32(cmd.Process.Pid)
	t.Cleanup(func() { terminatePID(t, pid) }) // closes any menu left open

	window := strconv.FormatUint(findWindowByTitle(t, title, 30*time.Second), 10)
	cover := coverWindow(t, window)
	if _, stderr, exit := runAsMenuAgent(t, "get-window-state", window); exit != 0 {
		t.Fatalf("get-window-state: exit %d, stderr:\n%s", exit, stderr)
	}
	const ref = "id:box"

	menu := openContextMenu(t, window, ref, cover)
	if w, ok := windowByID(t, menu); !ok || !isMenu(w) || w.PID != pid {
		t.Fatalf("menu %d is %+v, want the App's menu", menu, w)
	}
	menuRef := strconv.FormatUint(menu, 10)
	state, stderr, exit := runAsMenuAgent(t, "get-window-state", menuRef)
	if exit != 0 {
		t.Fatalf("get-window-state of the menu: exit %d, stderr %q", exit, stderr)
	}
	// By Element Index: a WinUI 3 item holds a TextBlock of the same
	// name, so name:Alpha is ambiguous there (refused, nothing sent).
	alpha := strconv.Itoa(webElementIndex(t, state, "menu item", "Alpha"))
	stdout, stderr, exit := runAsMenuAgent(t, "click", menuRef, alpha)
	assertActionOK(t, "click Alpha in the menu", stdout, stderr, exit)
	waitMenuClosed(t, "click Alpha", menu)
	assertForeground(t, "click Alpha in the menu", cover)
	waitMenuAppLine(t, statePath, "picked Alpha")

	menu = openContextMenu(t, window, ref, cover)
	stdout, stderr, exit = runAsMenuAgent(t, "press-key", strconv.FormatUint(menu, 10), "Escape")
	assertActionOK(t, "press-key <menu> Escape", stdout, stderr, exit)
	waitMenuClosed(t, "press-key Escape", menu)
	assertForeground(t, "press-key <menu> Escape", cover)

	menu = openContextMenu(t, window, ref, cover)
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

// waitMenuAppLine waits for a line in a measuring App's state file.
func waitMenuAppLine(t *testing.T, path, line string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && strings.Contains(string(data), line) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	data, _ := os.ReadFile(path)
	t.Errorf("the App's state file has no %q line:\n%s", line, data)
}
