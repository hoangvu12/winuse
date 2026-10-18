//go:build e2e

// These are the ticket-01 seam-1 tests: the real winuse binary against the
// live desktop. See the package comment and test/e2e/README.md for the
// opt-in rules; the shared runner plumbing lives in runner_test.go.

package e2e

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/win"
)

// TestListWindowsOutput checks the output contract: one line per visible
// Window, "<Window ID>\t<App>\t<title>", exit code 0.
func TestListWindowsOutput(t *testing.T) {
	optIn(t)
	stdout, stderr, exit := runListWindows(t, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("exit %d, stderr:\n%s", exit, stderr)
	}
	if stderr != "" {
		t.Errorf("unexpected stderr: %q", stderr)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatalf("no windows listed; the desktop should have at least one visible window:\n%q", stdout)
	}
	line := regexp.MustCompile(`^[0-9]+\t[^\t]*\t.*$`)
	for i, l := range lines {
		if !line.MatchString(l) {
			t.Errorf("line %d does not match \"<Window ID>\\t<App>\\t<title>\": %q", i+1, l)
		}
	}
}

// TestHelperIsReusedAcrossInvocations checks that two quick commands share
// one helper process instead of spawning two.
func TestHelperIsReusedAcrossInvocations(t *testing.T) {
	optIn(t)
	if _, _, exit := runListWindows(t, "WINUSE_HELPER_IDLE="+e2eIdle); exit != 0 {
		t.Fatalf("first invocation failed (exit %d)", exit)
	}
	pid1 := helperPID(t)
	if _, _, exit := runListWindows(t, "WINUSE_HELPER_IDLE="+e2eIdle); exit != 0 {
		t.Fatalf("second invocation failed (exit %d)", exit)
	}
	pid2 := helperPID(t)
	if pid1 != pid2 {
		t.Errorf("helper was restarted between invocations: %d then %d", pid1, pid2)
	}
}

// TestHelperIdleExitAndTransparentRestart checks that the helper exits
// after the idle period and the next command restarts it without the user
// noticing.
func TestHelperIdleExitAndTransparentRestart(t *testing.T) {
	optIn(t)
	// The idle period is read when the helper starts: a helper left by an
	// earlier test (with e2eIdle) would outlive the wait below.
	killHelper(t)
	if _, _, exit := runListWindows(t, "WINUSE_HELPER_IDLE="+idleForRestart); exit != 0 {
		t.Fatalf("first invocation failed (exit %d)", exit)
	}
	oldPID := helperPID(t)

	// Wait past the idle period; the helper must be gone (no pipe).
	time.Sleep(3500 * time.Millisecond)
	id, err := win.CurrentIdentity()
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	conn, err := dialHelper(id)
	if err == nil {
		conn.Close()
		t.Fatalf("helper %d is still listening after the idle period", oldPID)
	}

	// The next command must restart it transparently.
	stdout, stderr, exit := runListWindows(t, "WINUSE_HELPER_IDLE="+idleForRestart)
	if exit != 0 {
		t.Fatalf("restart invocation failed (exit %d), stderr:\n%s", exit, stderr)
	}
	if stdout == "" {
		t.Fatal("restart invocation listed no windows")
	}
	newPID := helperPID(t)
	if newPID == oldPID {
		t.Errorf("expected a fresh helper process, but pid %d survived the idle period", oldPID)
	}
}

// TestHelperHasNoConsoleWindow is a light check that the spawned helper is
// detached: it never shows up in its own listing (own-process exclusion)
// and does not hold the invoking terminal's console.
func TestHelperHasNoConsoleWindow(t *testing.T) {
	optIn(t)
	stdout, _, exit := runListWindows(t, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("exit %d", exit)
	}
	for _, l := range strings.Split(strings.TrimSuffix(stdout, "\n"), "\n") {
		if l == "" {
			continue
		}
		parts := strings.SplitN(l, "\t", 3)
		if len(parts) == 3 && strings.EqualFold(parts[1], "winuse") {
			t.Errorf("a winuse window appears in the listing: %q", l)
		}
	}
}

// TestGetWindowPrintsTheResovedLine checks `winuse get-window <window>`:
// the one list-windows line of the Window the listing reported, by ID and
// by App name, with the ambiguity and unknown-Window failures.
func TestGetWindowPrintsTheResovedLine(t *testing.T) {
	optIn(t)
	stdout, _, exit := runListWindows(t, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("list-windows failed (exit %d)", exit)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	// Resolving by App name needs an App with exactly one Window (several
	// fail as ambiguous, by design).
	perApp := map[string]int{}
	for _, l := range lines {
		perApp[strings.Split(l, "\t")[1]]++
	}
	line := ""
	for _, l := range lines {
		if perApp[strings.Split(l, "\t")[1]] == 1 {
			line = l
			break
		}
	}
	if line == "" {
		t.Skip("every listed App has several Windows; get-window by App name needs one with exactly one")
	}
	fields := strings.Split(line, "\t")
	id, app := fields[0], fields[1]

	want := line + "\n"
	for _, tc := range []struct {
		name string
		ref  string
	}{
		{"by Window ID", id},
		{"by App name", app},
	} {
		stdout, stderr, exit := runWinuse(t, []string{"get-window", tc.ref},
			"WINUSE_HELPER_IDLE="+e2eIdle)
		if exit != 0 {
			t.Fatalf("get-window %s (%s): exit %d, stderr:\n%s", tc.name, tc.ref, exit, stderr)
		}
		if stdout != want {
			t.Errorf("get-window %s (%s) = %q, want %q", tc.name, tc.ref, stdout, want)
		}
	}

	// An unknown Window ID fails with the shared resolution error and a
	// non-zero exit.
	_, stderr, exit := runWinuse(t, []string{"get-window", "999999999"},
		"WINUSE_HELPER_IDLE="+e2eIdle)
	if exit == 0 {
		t.Fatal("get-window of an unknown Window must fail")
	}
	if !strings.Contains(stderr, "no open Window with ID 999999999") {
		t.Errorf("stderr = %q, want the unknown-Window failure", stderr)
	}
}
