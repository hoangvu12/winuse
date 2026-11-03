//go:build e2e

// Live re-resolution of name:/id: (desktop-parity 17) on the covered
// fixture: after the helper restarts, no Window State is cached, and a
// Headless click and set-value by name:/id: still act, through a fresh
// read, each saying so in a note; an Element Index is refused as before.
// The foreground stays with the cover. User-run only (see the package
// comment and test/e2e/README.md); an agent must never start it.

package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
	"github.com/hoangvu12/winuse/internal/win"
)

func TestNameReferenceAfterHelperRestart(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)
	run := func(args ...string) (string, string, int) {
		return runWinuse(t, args, "WINUSE_HELPER_IDLE="+e2eIdle)
	}
	if _, stderr, exit := run("get-window-state", window); exit != 0 {
		t.Fatalf("get-window-state: exit %d, stderr:\n%s", exit, stderr)
	}
	killHelper(t) // the next command starts a helper with no Window State

	out, stderr, exit := run("click", window, "name:Click Me")
	if exit != 0 || !strings.HasPrefix(out, "ok\n") || !strings.Contains(out, "Note: found name:Click Me in a fresh read of window "+window) {
		t.Fatalf("click by name after the restart: exit %d, stdout %q, stderr %q", exit, out, stderr)
	}
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.LastButton == "Click Me" })

	out, stderr, exit = run("set-value", window, "id:text-field", "found live")
	if exit != 0 || !strings.Contains(out, "Note: found id:text-field in a fresh read") {
		t.Fatalf("set-value by id: exit %d, stdout %q, stderr %q", exit, out, stderr)
	}
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Text == "found live" })

	// The same reference again resolves to the Element already added: no
	// second fresh read is needed, so no note.
	out, _, exit = run("click", window, "name:Click Me")
	if exit != 0 || strings.Contains(out, "fresh read") {
		t.Errorf("second click by name: exit %d, stdout %q", exit, out)
	}

	// Two Twin buttons: a fresh read refuses with both candidates.
	killHelper(t)
	_, stderr, exit = run("click", window, "name:Twin")
	if exit != 1 || !strings.Contains(stderr, "matches 2 elements in a fresh read") {
		t.Errorf("ambiguous: exit %d, stderr %q", exit, stderr)
	}
	if fg := win.ForegroundWindow(); fg != cover {
		t.Errorf("the foreground is %s; the cover %d must stay in front", describeWindow(t, fg), cover)
	}
}
