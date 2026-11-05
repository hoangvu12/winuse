//go:build e2e

// --observe and the Batch's read steps (speed observe, ADR 0021) on the
// covered fixture: an Action prints the changed Element Indexes after its
// ok, --observe=full the whole Window State, and a Batch reads between its
// Actions. The foreground stays with the cover. User-run only (see the
// package comment and test/e2e/README.md); an agent must never start them.

package e2e

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
	"github.com/hoangvu12/winuse/internal/win"
)

func TestObserveOnTheFixture(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)
	run := func(args ...string) string {
		t.Helper()
		out, stderr, exit := runWinuse(t, args, "WINUSE_HELPER_IDLE="+e2eIdle)
		if exit != 0 {
			t.Fatalf("winuse %s: exit %d, stderr:\n%s", strings.Join(args, " "), exit, stderr)
		}
		return out
	}

	// A set-value with --observe: ok, then the field's line as changed.
	out := run("set-value", window, "id:"+string(fixture.ElemTextField), "observed", "--observe")
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Text == "observed" })
	if !strings.HasPrefix(out, "ok\n") || !strings.Contains(out, "Changed since this agent's last read of window "+window) ||
		!regexp.MustCompile(`(?m)^\t+~\d+ edit .*Value: observed`).MatchString(out) {
		t.Errorf("set-value --observe:\n%s", out)
	}

	// Nothing else changed: the next diff of a no-op says so.
	run("get-window-state", window)
	out = run("batch", window, "--observe", "get-window-state", "--interactive")
	if !strings.Contains(out, "No change since this agent's last read of window "+window) {
		t.Errorf("a Batch that only reads observes no change:\n%s", out)
	}

	// --observe=full prints the whole State, through the budgets.
	out = run("click", window, "id:"+string(fixture.ElemCheckboxNotify), "--observe=full", "--interactive")
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Checkbox })
	if !strings.Contains(out, "check box Notify me on") || !strings.Contains(out, "Note: --interactive left out") {
		t.Errorf("click --observe=full --interactive:\n%s", out)
	}

	// A Batch acts, reads and acts: its read step prints under its number.
	out = run("batch", window, "set-value", "id:"+string(fixture.ElemTextField), "batched", "then", "find", "--role", "edit", "then", "press-key", "Tab")
	if !regexp.MustCompile(`(?m)^Action 2 \(find\): `).MatchString(out) || !strings.Contains(out, "Value: batched") {
		t.Errorf("a Batch with a find step:\n%s", out)
	}
	if fg := win.ForegroundWindow(); fg != cover {
		t.Errorf("the foreground is %s; the cover %d must stay in front", describeWindow(t, fg), cover)
	}
}
