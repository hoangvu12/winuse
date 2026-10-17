//go:build e2e

// These are the ticket-04 seam-1 tests: the real winuse binary against the
// live desktop, driving list-apps and launch-app exactly as an agent
// would. See the package comment and test/e2e/README.md for the opt-in
// rules; the shared runner plumbing lives in runner_test.go. They start
// apps and take over pieces of the desktop: user-run only.

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
)

// TestListAppsShowsFixtureApp: the fixture's App must be listed as running
// with exactly its Window, by the exact Window ID the fixture reports.
func TestListAppsShowsFixtureApp(t *testing.T) {
	optIn(t)
	fp := startFixture(t)
	st := fp.waitState(t, 15*time.Second, func(s *fixture.State) bool {
		return s.WindowID != 0
	})

	stdout, stderr, exit := runWinuse(t, []string{"list-apps"}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("exit %d, stderr:\n%s", exit, stderr)
	}
	if stderr != "" {
		t.Errorf("unexpected stderr: %q", stderr)
	}

	header, windows, ok := appBlock(stdout, "winusefixture")
	if !ok {
		t.Fatalf("the fixture App is not listed:\n%s", stdout)
	}
	if header[1] != "running" {
		t.Errorf("fixture App state = %q, want \"running\"", header[1])
	}
	if len(windows) != 1 {
		t.Fatalf("the fixture App must list exactly one Window (the dialog starts hidden), got %d:\n%s", len(windows), stdout)
	}
	if windows[0][0] != itoa64(st.WindowID) {
		t.Errorf("listed Window ID = %s, want the fixture's %d", windows[0][0], st.WindowID)
	}
	if windows[0][1] != fixture.WindowTitle {
		t.Errorf("listed Window title = %q, want %q", windows[0][1], fixture.WindowTitle)
	}
}

// TestLaunchAppByPathOpensFixtureWindow is the ticket's seam-1 acceptance
// test: launching the fixture by executable path must report the Window it
// opened — the exact Window the fixture itself reports, and a Window
// list-windows lists.
func TestLaunchAppByPathOpensFixtureWindow(t *testing.T) {
	optIn(t)
	// The fixture cannot take a -state flag when winuse starts it, so it
	// reads WINUSE_FIXTURE_STATE — inherited from the helper. A helper
	// left running by an earlier test lacks this environment, so kill it
	// and let the first call below spawn a fresh one.
	killHelper(t)
	statePath := filepath.Join(t.TempDir(), "state.json")
	env := []string{"WINUSE_FIXTURE_STATE=" + statePath, "WINUSE_HELPER_IDLE=" + e2eIdle}

	if _, _, exit := runWinuse(t, []string{"list-windows"}, env...); exit != 0 {
		t.Fatalf("starting a helper with the fixture state environment failed (exit %d)", exit)
	}

	stdout, stderr, exit := runWinuse(t, []string{"launch-app", fixtureBinary(t)}, env...)
	if exit != 0 {
		t.Fatalf("launch-app exit %d, stderr:\n%s", exit, stderr)
	}
	if stderr != "" {
		t.Errorf("unexpected stderr: %q", stderr)
	}

	st := waitStateFile(t, statePath, 15*time.Second, func(s *fixture.State) bool {
		return s.WindowID != 0
	})
	t.Cleanup(func() { killWindowProcess(t, st.WindowID) })

	line := strings.TrimSuffix(stdout, "\n")
	if line == "" || strings.Contains(line, "\n") {
		t.Fatalf("launch-app must print exactly one Window line, got:\n%q", stdout)
	}
	parts := strings.SplitN(line, "\t", 3)
	if len(parts) != 3 {
		t.Fatalf("launch-app output %q is not <Window ID>\\t<App>\\t<title>", line)
	}
	if parts[0] != itoa64(st.WindowID) {
		t.Errorf("reported Window ID %s, want the fixture's own %d", parts[0], st.WindowID)
	}
	if parts[1] != "winusefixture" {
		t.Errorf("reported App = %q, want winusefixture", parts[1])
	}
	if parts[2] != fixture.WindowTitle {
		t.Errorf("reported title = %q, want %q", parts[2], fixture.WindowTitle)
	}

	// The reported Window must be a real, targetable Window.
	lw, lwErr, lwExit := runWinuse(t, []string{"list-windows"}, env...)
	if lwExit != 0 {
		t.Fatalf("list-windows after launch failed (exit %d), stderr:\n%s", lwExit, lwErr)
	}
	if _, _, _, ok := windowLine(lw, st.WindowID); !ok {
		t.Errorf("the Window reported by launch-app is not listed by list-windows:\n%s", lw)
	}
}

// TestLaunchAppNotFound: a name that matches no App fails cleanly with the
// app-not-found error (no app starts, nothing changes on screen).
func TestLaunchAppNotFound(t *testing.T) {
	optIn(t)
	stdout, stderr, exit := runWinuse(t, []string{"launch-app", "definitely-not-an-app-winuse-t04"}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 1 {
		t.Fatalf("exit %d, want 1", exit)
	}
	if stdout != "" {
		t.Errorf("unexpected stdout: %q", stdout)
	}
	if !strings.HasPrefix(stderr, "app not found: definitely-not-an-app-winuse-t04") {
		t.Errorf("stderr = %q, want the app-not-found message", stderr)
	}
}

// TestLaunchAppNoWindowTimesOut: launching an executable that opens no
// Window (rundll32 without arguments starts and exits immediately) must
// fail with Codex's "did not expose a targetable window" error after the
// 15 s launch timeout — not report success, and not hang.
func TestLaunchAppNoWindowTimesOut(t *testing.T) {
	optIn(t)
	rundll32 := filepath.Join(os.Getenv("SystemRoot"), "System32", "rundll32.exe")
	if _, err := os.Stat(rundll32); err != nil {
		t.Skipf("rundll32 not found: %v", err)
	}
	start := time.Now()
	stdout, stderr, exit := runWinuse(t, []string{"launch-app", rundll32}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 1 {
		t.Fatalf("exit %d, want 1 (no Window was opened)", exit)
	}
	if stdout != "" {
		t.Errorf("unexpected stdout: %q", stdout)
	}
	if !strings.Contains(stderr, "launched app did not expose a targetable window: ") {
		t.Errorf("stderr = %q, want the no-window failure", stderr)
	}
	if elapsed := time.Since(start); elapsed < 10*time.Second {
		t.Errorf("gave up after %s; the launch timeout is 15 s (the poll budget must be honored)", elapsed)
	}
}
