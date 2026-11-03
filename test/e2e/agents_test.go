//go:build e2e

// Window States per Agent (ticket 03 of multi-agent): two Agents named
// by WINUSE_AGENT against the fixture through one helper. An Agent that
// has not read the Window is refused with nothing sent, even after the
// other read it; each Agent's read keeps its own Element Indexes; and
// `winuse status` lists both. User-run only (see the package comment and
// test/e2e/README.md); an agent must never start them.

package e2e

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
)

func TestWindowStatesArePerAgent(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)
	assertCovered(t, fix, window, fixture.ElemCheckboxNotify, cover)
	checkbox := strconv.Itoa(mainElementIndexes()[fixture.ElemCheckboxNotify])
	as := func(name string) []string { return []string{"WINUSE_AGENT=" + name, "WINUSE_HELPER_IDLE=" + e2eIdle} }

	// alpha reads the Window; beta has not, and is refused.
	if _, stderr, exit := runWinuse(t, []string{"get-window-state", window}, as("alpha")...); exit != 0 {
		t.Fatalf("alpha get-window-state: exit %d, stderr:\n%s", exit, stderr)
	}
	before := fix.waitState(t, time.Second, func(*fixture.State) bool { return true })
	stdout, stderr, exit := runWinuse(t, []string{"click", window, checkbox}, as("beta")...)
	if exit == 0 || !strings.Contains(stderr, "has not been read by this agent") || !strings.Contains(stderr, "nothing was sent") {
		t.Errorf("beta's click before its read: exit %d, stdout %q, stderr %q", exit, stdout, stderr)
	}
	if st := fix.waitState(t, time.Second, func(*fixture.State) bool { return true }); st.Checkbox != before.Checkbox {
		t.Error("beta's refused click toggled the checkbox")
	}

	// beta reads; alpha's Window State survives it, and alpha's click
	// acts on alpha's Element.
	if _, stderr, exit := runWinuse(t, []string{"get-window-state", window}, as("beta")...); exit != 0 {
		t.Fatalf("beta get-window-state: exit %d, stderr:\n%s", exit, stderr)
	}
	stdout, stderr, exit = runWinuse(t, []string{"click", window, checkbox}, as("alpha")...)
	assertActionOK(t, "alpha's click after beta's read", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Checkbox != before.Checkbox })
	stdout, stderr, exit = runWinuse(t, []string{"click", window, checkbox}, as("beta")...)
	assertActionOK(t, "beta's click after its read", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Checkbox == before.Checkbox })
	assertForeground(t, "two Agents' Headless clicks", cover)

	stdout, _, exit = runWinuse(t, []string{"status"}, as("alpha")...)
	if exit != 0 || !strings.Contains(stdout, "alpha (") || !strings.Contains(stdout, "beta (") {
		t.Errorf("status: exit %d, want alpha and beta on the agents line:\n%s", exit, stdout)
	}
}
