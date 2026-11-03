//go:build e2e

// set-value read-backs that used to raise false alarms (headless-max
// ticket 24, from the 2026-11-03 Task Suite run): a multi-line value in
// Windows 11 Notepad, whose editor reads CRLF back as CR, and the rename
// of a file in File Explorer, whose list item is rebuilt by the write and
// reads "" afterwards. Both must print a plain ok. The test launches its
// own Notepad on a temp file and its own Explorer window on a temp
// folder, and closes both. User-run only (see the package comment and
// test/e2e/README.md).

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/win"
)

func TestMultiLineSetValueIntoNotepadReadsBack(t *testing.T) {
	optIn(t)
	all, err := win.Collect()
	if err != nil {
		t.Fatalf("enumerating windows: %v", err)
	}
	for _, w := range all {
		if w.Class == "Notepad" {
			t.Skip("Notepad is already running: close it first (this test ends the Notepad process it writes into)")
		}
	}
	name := "winuse-e2e-readback-" + strconv.FormatInt(time.Now().UnixNano(), 36) + ".txt"
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	if err := exec.Command("notepad.exe", path).Start(); err != nil {
		t.Skipf("starting notepad.exe: %v", err)
	}
	id := findWindowByTitle(t, strings.TrimSuffix(name, ".txt"), 30*time.Second)
	defer func() {
		if win.WindowOpen(id) {
			killWindowProcess(t, id)
		}
	}()
	window := strconv.FormatUint(id, 10)
	if _, stderr, exit := runGetWindowState(t, window); exit != 0 {
		t.Fatalf("get-window-state: exit %d, stderr %q", exit, stderr)
	}

	const value = "Museum visit notes\r\n\r\nOpens at 9:30.\r\nNo flash photography."
	stdout, stderr, exit := runWinuse(t, []string{"set-value", window, "name:Text editor", value}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 || stdout != "ok\n" {
		t.Errorf("multi-line set-value into Notepad: exit %d, stdout %q, stderr %q; want a plain ok", exit, stdout, stderr)
	}
}

func TestRenameInExplorerReadsBack(t *testing.T) {
	optIn(t)
	folder := "winuse-e2e-rename-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	dir := filepath.Join(t.TempDir(), folder)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// As in the Task Suite's invoice folder: several files, and the new
	// name sorts elsewhere, so Explorer moves (rebuilds) the item.
	for _, f := range []string{"alpha.txt", "draft.txt", "notes.txt", "report.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := exec.Command("explorer.exe", dir).Start(); err != nil {
		t.Skipf("starting explorer.exe: %v", err)
	}
	id := findWindowByTitle(t, folder, 30*time.Second)
	window := strconv.FormatUint(id, 10)
	// The Window belongs to the shell's own process: close it, never kill.
	defer func() {
		if win.WindowOpen(id) {
			if _, stderr, exit := runWinuse(t, []string{"close-window", window}, "WINUSE_HELPER_IDLE="+e2eIdle); exit != 0 {
				t.Errorf("closing the test's Explorer window: %s", stderr)
			}
		}
	}()

	// The item reads "draft.txt", or "draft" when the user hides file
	// extensions; the new name follows the same form.
	item, renamed := "draft.txt", "summary.txt"
	var tree string
	deadline := time.Now().Add(10 * time.Second)
	for {
		out, stderr, exit := runGetWindowState(t, window)
		if exit != 0 {
			t.Fatalf("get-window-state: exit %d, stderr %q", exit, stderr)
		}
		tree = out
		if strings.Contains(tree, "list item (selectable") && strings.Contains(tree, " draft") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the Explorer window does not list draft.txt:\n%s", tree)
		}
		time.Sleep(300 * time.Millisecond)
	}
	if !strings.Contains(tree, " draft.txt") {
		item, renamed = "draft", "summary"
	}

	stdout, stderr, exit := runWinuse(t, []string{"set-value", window, "name:" + item, renamed}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 || stdout != "ok\n" {
		t.Errorf("rename in Explorer: exit %d, stdout %q, stderr %q; want a plain ok", exit, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "summary.txt")); err != nil {
		t.Errorf("the file was not renamed to summary.txt: %v", err)
	}
}
