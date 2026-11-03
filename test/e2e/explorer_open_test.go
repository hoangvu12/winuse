//go:build e2e

// Opening a folder in File Explorer (headless-max ticket 23, from the
// 2026-11-03 Task Suite run, where a --headed double click on a folder
// printed ok and opened nothing). A --headed double click opens a folder,
// and so does a Headless click (Invoke, the item's default action). The
// run's failure reproduced only while a Notepad, started by the Action
// before, came up at the same moment (see the ticket's measurements). The
// test opens its own Explorer window on a temp folder holding a
// subfolder, and closes it afterwards with close-window (an Explorer
// window belongs to the shell's process: never killed). User-run only
// (see the package comment and test/e2e/README.md).

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// explorerOn opens an Explorer window on a fresh temp folder holding the
// subfolder inner and two files, read once, and returns its Window and
// the subfolder's name. A cleanup registered before Explorer starts
// closes every window titled with the stamp (the folder's and, once
// opened, the subfolder's), even when the test fails.
func explorerOn(t *testing.T) (window, inner string) {
	t.Helper()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 36)
	folder := "winuse-e2e-open-" + stamp
	inner = "inner-" + stamp
	dir := filepath.Join(t.TempDir(), folder)
	if err := os.MkdirAll(filepath.Join(dir, inner), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"alpha.txt", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { closeTestWindows(t, stamp) })
	if err := exec.Command("explorer.exe", dir).Start(); err != nil {
		t.Skipf("starting explorer.exe: %v", err)
	}
	window = strconv.FormatUint(findWindowByTitle(t, folder, 30*time.Second), 10)
	deadline := time.Now().Add(10 * time.Second)
	for {
		out, stderr, exit := runGetWindowState(t, window)
		if exit != 0 {
			t.Fatalf("get-window-state: exit %d, stderr %q", exit, stderr)
		}
		if strings.Contains(out, " "+inner) {
			return window, inner
		}
		if time.Now().After(deadline) {
			t.Fatalf("the Explorer window does not list %s:\n%s", inner, out)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// waitTitle reports whether window's title comes to contain want within
// the timeout (the folder opened in the same window).
func waitTitle(t *testing.T, window, want string, timeout time.Duration) (bool, string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		out, _, exit := runWinuse(t, []string{"get-window", window}, "WINUSE_HELPER_IDLE="+e2eIdle)
		line := strings.TrimSpace(out)
		if exit == 0 && strings.Contains(line, want) {
			return true, line
		}
		if time.Now().After(deadline) {
			return false, line
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func TestHeadedDoubleClickOpensAnExplorerFolder(t *testing.T) {
	optIn(t)
	window, inner := explorerOn(t)
	stdout, stderr, exit := runWinuse(t, []string{"--headed", "click", window, "name:" + inner, "--count", "2"}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("--headed double click on the folder: exit %d, stdout %q, stderr %q", exit, stdout, stderr)
	}
	if ok, line := waitTitle(t, window, inner, 5*time.Second); !ok {
		t.Errorf("the --headed double click printed %q, and the window still reads %q: the folder did not open", stdout, line)
	}
}

// A Headless click on an Explorer item is its default action (Invoke): it
// opens a folder in the same window, with no foreground (and opens a file
// in its App, which is why SKILL.md says to select files with Select).
func TestHeadlessClickOpensAnExplorerFolder(t *testing.T) {
	optIn(t)
	window, inner := explorerOn(t)
	stdout, stderr, exit := runWinuse(t, []string{"click", window, "name:" + inner}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 || stdout != "ok\n" {
		t.Fatalf("Headless click on the folder: exit %d, stdout %q, stderr %q", exit, stdout, stderr)
	}
	if ok, line := waitTitle(t, window, inner, 5*time.Second); !ok {
		t.Errorf("the Headless click did not open the folder; the window reads %q", line)
	}
}
