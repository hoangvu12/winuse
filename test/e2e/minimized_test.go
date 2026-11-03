//go:build e2e

// Headless Actions on a minimized Window (ticket 14 of headless-max): the
// UI Automation pattern Actions reach a minimized classic Window (the
// fixture's main window, a Win32 window with its own provider) without
// restoring it, while Element scroll and the screenshot stay refused.
// User-run only (see the package comment and test/e2e/README.md); an agent
// must never start them.

package e2e

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
	"github.com/hoangvu12/winuse/internal/win"
)

func TestHeadlessActionsOnMinimizedWindow(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)
	assertCovered(t, fix, window, fixture.ElemCheckboxNotify, cover)
	indexes := mainElementIndexes()
	id := mustWindowID(t, window)

	stdout, stderr, exit := runWinuse(t, []string{"minimize-window", window}, "WINUSE_HELPER_IDLE="+e2eIdle)
	assertActionOK(t, "minimize-window", stdout, stderr, exit)
	if !win.IsMinimized(id) {
		t.Fatal("the fixture is not minimized")
	}
	coverAgain(t, cover)
	pointer := pointerPos(t)

	// A click (Toggle) and a set-value, both on the minimized Window.
	res, overlaySeen := runWatched(t, []string{"click", window, strconv.Itoa(indexes[fixture.ElemCheckboxNotify])})
	assertActionOK(t, "click on a minimized Window", res.stdout, res.stderr, res.exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Checkbox })
	if overlaySeen {
		t.Error("the in-control banner was shown during a click on a minimized Window")
	}
	stdout, stderr, exit = runSetvalue(t, window, indexes[fixture.ElemTextField], "minimized")
	assertActionOK(t, "set-value on a minimized Window", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Text == "minimized" })

	if !win.IsMinimized(id) {
		t.Error("an Action restored the minimized Window")
	}
	assertForeground(t, "Actions on a minimized Window", cover)
	if got := pointerPos(t); got != pointer {
		t.Errorf("the real pointer moved from %v to %v", pointer, got)
	}

	// Element scroll and the screenshot stay refused, with nothing sent.
	stdout, stderr, exit = runWinuse(t, []string{"scroll", window, strconv.Itoa(indexes[fixture.ElemCheckboxNotify]), "down"}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit == 0 || !strings.Contains(stderr, "is minimized") || !strings.Contains(stderr, "nothing was sent") {
		t.Errorf("scroll on a minimized Window: exit %d, stdout %q, stderr %q", exit, stdout, stderr)
	}
	stdout, stderr, exit = runGetWindowStateScreenshot(t, window)
	assertActionFailed(t, "screenshot of a minimized Window", stdout, stderr, exit, "is minimized")
	if !win.IsMinimized(id) {
		t.Error("a refusal restored the minimized Window")
	}
	assertForeground(t, "refusals on a minimized Window", cover)
}
