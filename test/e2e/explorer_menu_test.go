//go:build e2e

// A context menu that opens late (multi-agent ticket 12): a Windows 11
// File Explorer item's first context menu in a Window shows 1.35-1.65 s
// after the --headed right click's ok (measured 2026-11-03), and polite
// input's hand-back used to close it before it showed. The test opens an
// Explorer Window of its own on a temp folder holding one file, covers
// it, right-clicks the file --headed and expects the menu still open: the
// ok notes the new Window and that the foreground stays for it. The menu
// and the Explorer Window are closed afterwards. User-run only (see the
// package comment and test/e2e/README.md); an agent must never start it.

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/win"
)

// openExplorerOn opens a File Explorer Window of the test's own on a new
// temp folder holding file, and returns its Window ID. Every Window titled
// after the folder is closed on cleanup, registered before Explorer
// starts, so it runs on every exit path, a failure included (a Window
// that opens after the test gave up on finding it too).
func openExplorerOn(t *testing.T, file string) string {
	t.Helper()
	name := "winuse-e2e-explorer-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte("winuse e2e"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeExplorerWindows(t, name) }) // runs before t.TempDir's removal
	if err := exec.Command("explorer.exe", dir).Start(); err != nil {
		t.Fatalf("starting explorer.exe: %v", err)
	}
	window := strconv.FormatUint(findWindowByTitle(t, name, 20*time.Second), 10)
	time.Sleep(time.Second) // the items view fills in
	return window
}

// closeExplorerWindows closes every top-level Window whose title starts
// with the test's folder name, with WM_CLOSE posted straight to it (no
// helper in the way), and fails the test when one is still open 5 s later.
func closeExplorerWindows(t *testing.T, name string) {
	t.Helper()
	ws, err := win.Collect()
	if err != nil {
		t.Errorf("listing the Windows to close the test's Explorer: %v", err)
		return
	}
	for _, w := range ws {
		if !strings.HasPrefix(w.Title, name) {
			continue
		}
		if err := win.PostClose(w.ID); err != nil {
			t.Errorf("closing the test's Explorer Window %d: %v", w.ID, err)
			continue
		}
		deadline := time.Now().Add(5 * time.Second)
		for win.WindowOpen(w.ID) && time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
		}
		if win.WindowOpen(w.ID) {
			t.Errorf("the test's Explorer Window %d %q is still open after WM_CLOSE", w.ID, w.Title)
		}
	}
}

// explorerItem is the Element Index of the list item named file.
func explorerItem(t *testing.T, window, file string) string {
	t.Helper()
	state := readState(t, window)
	m := regexp.MustCompile(`(?m)^\t+(\d+) list item [^\n]*` + regexp.QuoteMeta(file)).FindStringSubmatch(state)
	if m == nil {
		t.Fatalf("no list item %q in Explorer's Window State:\n%s", file, state)
	}
	return m[1]
}

func TestHeadedRightClickKeepsALateExplorerMenu(t *testing.T) {
	optIn(t)
	const file = "winuse-e2e-menu.txt"
	window := openExplorerOn(t, file)
	cover := coverWindow(t, window)
	item := explorerItem(t, window, file)

	stdout, stderr, exit := runWinuse(t, []string{"--headed", "click", window, item, "--button", "right"}, "WINUSE_HELPER_IDLE="+e2eIdle)
	assertActionOK(t, "--headed right click on the Explorer item", stdout, stderr, exit)
	m := regexp.MustCompile(`Note: a new Window opened: (\d+) `).FindStringSubmatch(stdout)
	if m == nil {
		t.Fatalf("the ok does not note the menu's Window: %q", stdout)
	}
	if !strings.Contains(stdout, "Note: the foreground stays on window "+window) {
		t.Errorf("the ok does not say the foreground stays for the menu: %q", stdout)
	}
	menu, _ := strconv.ParseUint(m[1], 10, 64)
	if !win.WindowOpen(menu) {
		t.Errorf("the menu %d closed: the foreground was given back before it showed", menu)
	}
	if fg := win.ForegroundWindow(); fg == cover {
		t.Errorf("the cover %d holds the foreground again: the menu lost it", cover)
	}
	if _, stderr, exit := runWinuse(t, []string{"press-key", m[1], "Escape"}, "WINUSE_HELPER_IDLE="+e2eIdle); exit != 0 {
		t.Errorf("closing the menu: exit %d, stderr %q", exit, stderr)
	}
}

// The Headless ContextMenu on a File Explorer item (headless-max ticket
// 22): ShowContextMenu opens it, but an Explorer that showed no menu for a
// while shows it up to about 6 s later (measured 2026-11-03), past the 2 s
// read-back other Apps get. The test opens an Explorer Window of its own
// (cold), covers it, and expects the menu read back, the cover keeping
// the foreground; it then closes the menu and the Window.
func TestContextMenuOnACoveredExplorerItem(t *testing.T) {
	optIn(t)
	const file = "winuse-e2e-context.txt"
	window := openExplorerOn(t, file)
	cover := coverWindow(t, window)
	item := explorerItem(t, window, file)

	start := time.Now()
	stdout, stderr, exit := runWinuse(t, []string{"perform-secondary-action", window, item, "ContextMenu"}, "WINUSE_HELPER_IDLE="+e2eIdle)
	t.Logf("ContextMenu took %s", time.Since(start))
	assertActionOK(t, "ContextMenu on the Explorer item", stdout, stderr, exit)
	m := regexp.MustCompile(`Note: the context menu opened: read it with winuse get-window-state (\d+)`).FindStringSubmatch(stdout)
	if m == nil {
		t.Fatalf("the ok does not name the menu: %q", stdout)
	}
	menu, _ := strconv.ParseUint(m[1], 10, 64)
	if !win.WindowOpen(menu) {
		t.Errorf("the menu %d is not open", menu)
	}
	if fg := win.ForegroundWindow(); fg != cover {
		t.Errorf("the foreground is %s; the cover %d must keep it", describeWindow(t, fg), cover)
	}
	if _, stderr, exit := runWinuse(t, []string{"press-key", m[1], "Escape"}, "WINUSE_HELPER_IDLE="+e2eIdle); exit != 0 {
		t.Errorf("closing the menu: exit %d, stderr %q", exit, stderr)
	}
}
