//go:build e2e

// Element scroll (ticket 03 of headless-parity): `scroll <window>
// <element> <direction> [steps]` and `scroll <window> <element>
// into-view` through UI Automation, on the covered fixture's list.
// User-run only (see the package comment and test/e2e/README.md); an
// agent must never start them.
//
// The fixture's list shows 5 of its 8 items and scrolls one item per
// ScrollPattern SmallIncrement, so its scroll position (listFirstVisible
// in the state file) runs from 0 to 3 and counts the steps exactly.

package e2e

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
)

// runElementScroll invokes `winuse scroll <window> <element> <words...>`.
func runElementScroll(t *testing.T, window, element string, words ...string) (string, string, int) {
	t.Helper()
	return runWinuse(t, append([]string{"scroll", window, element}, words...), "WINUSE_HELPER_IDLE="+e2eIdle)
}

func TestElementScrollByStepsAndIntoView(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)
	indexes := mainElementIndexes()
	list := strconv.Itoa(indexes[fixture.ElemItemList])
	assertCovered(t, fix, window, fixture.ElemItemList, cover)
	pointer := pointerPos(t)

	firstVisible := func(want int) {
		t.Helper()
		fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ListFirstVisible == want })
	}

	// Two steps down on the list itself.
	res, overlaySeen := runWatched(t, []string{"scroll", window, list, "down", "2"})
	assertActionOK(t, "scroll the list down 2", res.stdout, res.stderr, res.exit)
	firstVisible(2)
	assertForeground(t, "scroll down 2", cover)
	if overlaySeen {
		t.Error("the in-control banner was shown during a Headless scroll")
	}
	if got := pointerPos(t); got != pointer {
		t.Errorf("the real pointer moved from %v to %v during a Headless scroll", pointer, got)
	}

	// The default 3 steps meet the end after one: a partial move is ok.
	stdout, stderr, exit := runElementScroll(t, window, list, "down")
	assertActionOK(t, "scroll the list down (default steps) to its end", stdout, stderr, exit)
	firstVisible(3)

	// At the end: delivered, nothing moved, and the failure says so.
	before := fix.waitState(t, time.Second, func(*fixture.State) bool { return true })
	stdout, stderr, exit = runElementScroll(t, window, list, "down", "1")
	if exit == 0 || stdout != "" {
		t.Fatalf("scroll down at the end: exit %d, stdout %q", exit, stdout)
	}
	if !strings.Contains(stderr, "the Element did not scroll; it may already be at the end") {
		t.Errorf("scroll down at the end: stderr = %q", stderr)
	}
	if st := fix.waitState(t, time.Second, func(*fixture.State) bool { return true }); st.ListFirstVisible != before.ListFirstVisible {
		t.Errorf("the list moved from %d to %d on a failed scroll", before.ListFirstVisible, st.ListFirstVisible)
	}

	// A list item has no Scroll pattern of its own: its list scrolls.
	stdout, stderr, exit = runElementScroll(t, window, "id:"+fixture.AutomationID(fixture.ElemItemBravo), "up", "3")
	assertActionOK(t, "scroll up 3 by a list item", stdout, stderr, exit)
	firstVisible(0)
	assertForeground(t, "scroll up by a list item", cover)

	// into-view brings the last item into the viewport: first visible 3.
	stdout, stderr, exit = runElementScroll(t, window, "id:"+fixture.AutomationID(fixture.ElemItemHotel), "into-view")
	assertActionOK(t, "scroll Hotel into view", stdout, stderr, exit)
	firstVisible(3)
	assertForeground(t, "scroll into view", cover)

	// Refusals before anything is sent: no scrollable ancestor on that
	// axis, and no ScrollItem pattern.
	checkbox := strconv.Itoa(indexes[fixture.ElemCheckboxNotify])
	for _, words := range [][]string{{"left"}, {"into-view"}} {
		target := list
		if words[0] == "into-view" {
			target = checkbox
		}
		before := fix.waitState(t, time.Second, func(*fixture.State) bool { return true })
		stdout, stderr, exit := runElementScroll(t, window, target, words...)
		if exit == 0 || stdout != "" {
			t.Errorf("scroll %s %s: exit %d, stdout %q, want a refusal", target, words[0], exit, stdout)
			continue
		}
		if !strings.Contains(stderr, "nothing was sent") {
			t.Errorf("scroll %s %s: the refusal must say nothing was sent: %q", target, words[0], stderr)
		}
		if st := fix.waitState(t, time.Second, func(*fixture.State) bool { return true }); st.Generation != before.Generation {
			t.Errorf("scroll %s %s: a refused scroll changed the fixture (generation %d -> %d)", target, words[0], before.Generation, st.Generation)
		}
	}
}
