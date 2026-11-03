//go:build e2e

// No Headed Action without --headed (headless-max ticket 21, ADR 0013
// rule 3): every Action whose only route is Headed is refused before
// anything is sent when the agent did not pass --headed. winuse never
// takes the user's foreground, pointer or keyboard on its own. User-run
// only (see the package comment and test/e2e/README.md); an agent must
// never start it.
//
// The target is the fixture, covered the way headless_test.go covers it:
// a second fixture instance in front, brought to the foreground with
// `winuse activate-window`. Each refusal must exit 1 with nothing on
// stdout, say "nothing was sent", give the exact `winuse --headed ...`
// retry in its hint, leave the cover in the foreground, the real pointer
// where it was and the banner down, and change nothing in the fixture.

package e2e

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
)

// assertHeadedRefusal pins the refusal of an Action whose only route is
// Headed, sent without --headed: exit 1, nothing on stdout, "nothing was
// sent" and a hint with the `winuse --headed` retry on stderr. retry, when
// not empty, is the exact retry the hint must contain.
func assertHeadedRefusal(t *testing.T, what, stdout, stderr string, exit int, retry string) {
	t.Helper()
	if exit != 1 {
		t.Errorf("%s: exit %d, want 1 (refused without --headed); stdout %q, stderr %q", what, exit, stdout, stderr)
	}
	if stdout != "" {
		t.Errorf("%s: a refusal must print nothing on stdout, got %q", what, stdout)
	}
	if !strings.Contains(stderr, "nothing was sent") {
		t.Errorf("%s: the refusal must say nothing was sent: %q", what, stderr)
	}
	if !strings.Contains(stderr, "hint: ") || !strings.Contains(stderr, "winuse --headed ") {
		t.Errorf("%s: the refusal needs a hint with the winuse --headed retry: %q", what, stderr)
	}
	if retry != "" && !strings.Contains(stderr, retry) {
		t.Errorf("%s: the hint must give the exact retry %q: %q", what, retry, stderr)
	}
}

func TestHeadedOnlyActionsAreRefusedWithoutTheFlag(t *testing.T) {
	optIn(t)
	// The screenshot first, so the @x,y forms have a coordinate mapping and
	// are refused for having no Headless route, not for a missing
	// screenshot. Its Window State read also caches the Element Indexes.
	fix, window, info := startFixtureForShots(t)
	cover := coverWindow(t, window)
	indexes := mainElementIndexes()
	canvas := strconv.Itoa(indexes[fixture.ElemCanvas])
	root := strconv.Itoa(indexes[fixture.ElemWindow])

	// The points pointer_test.go aims at: the canvas, the hover band under
	// the menu bar, and a drag across the canvas.
	point := func(cx, cy int32) string {
		x, y := shotCoordsOfClient(t, fix, window, info.scale, cx, cy)
		return fmt.Sprintf("@%d,%d", x, y)
	}
	onCanvas := point(636, 232)
	band := point(200, 42)
	dragFrom, dragTo := point(530, 80), point(680, 250)

	cases := []struct {
		name  string
		args  []string
		retry string
	}{
		{"double click on the canvas", []string{"click", window, canvas, "--count", "2"},
			"winuse --headed click " + window + " " + canvas + " --count 2"},
		{"right click on the canvas", []string{"click", window, canvas, "--button", "right"},
			"winuse --headed click " + window + " " + canvas + " --button right"},
		{"click at a point", []string{"click", window, onCanvas},
			"winuse --headed click " + window + " " + onCanvas},
		{"hover at a point", []string{"hover", window, band},
			"winuse --headed hover " + window + " " + band},
		{"drag between points", []string{"drag", window, dragFrom, dragTo},
			"winuse --headed drag " + window + " " + dragFrom + " " + dragTo},
		{"press-key Control_L+a", []string{"press-key", window, "Control_L+a"},
			"winuse --headed press-key " + window + " Control_L+a"},
		{"type-text into the main window", []string{"type-text", window, "never typed"},
			"winuse --headed type-text " + window + " <text>"},
		{"perform-secondary-action Raise on the window", []string{"perform-secondary-action", window, root, "Raise"},
			"winuse --headed perform-secondary-action " + window + " " + root + " Raise"},
	}
	for _, c := range cases {
		before := fix.waitState(t, time.Second, func(*fixture.State) bool { return true })
		pointer := pointerPos(t)

		res, overlaySeen := runWatched(t, c.args)
		assertHeadedRefusal(t, c.name, res.stdout, res.stderr, res.exit, c.retry)
		assertForeground(t, c.name, cover)
		if overlaySeen {
			t.Errorf("%s: the in-control banner was shown for a refused Action", c.name)
		}
		if got := pointerPos(t); got != pointer {
			t.Errorf("%s: the real pointer moved from %v to %v for a refused Action", c.name, pointer, got)
		}

		// Let anything that slipped through land before comparing.
		time.Sleep(300 * time.Millisecond)
		if after := fix.waitState(t, time.Second, func(*fixture.State) bool { return true }); after.Generation != before.Generation {
			t.Errorf("%s: the refused Action changed the fixture (generation %d -> %d)", c.name, before.Generation, after.Generation)
		}
	}

	// None of the refused Actions had an effect the fixture can show.
	st := fix.waitState(t, time.Second, func(*fixture.State) bool { return true })
	if len(st.CanvasClicks) != 0 || st.CanvasDoubleClicks != 0 || len(st.CanvasRightClicks) != 0 || len(st.CanvasDrags) != 0 {
		t.Errorf("pointer input reached the canvas: clicks %v, double clicks %d, right clicks %v, drags %v",
			st.CanvasClicks, st.CanvasDoubleClicks, st.CanvasRightClicks, st.CanvasDrags)
	}
	if st.ToolbarVisible {
		t.Error("the hover-only toolbar is shown: the refused hover moved the pointer")
	}
	if st.Text != "" || st.Typed != "" || st.TextSelected || st.LastKey != "" {
		t.Errorf("keyboard input reached the fixture: text %q, typed %q, selected %v, last key %q", st.Text, st.Typed, st.TextSelected, st.LastKey)
	}
	// Raise did not bring the fixture forward.
	assertAbove(t, cover, mustWindowID(t, window))

	// A Batch refuses such a step at its turn: the steps before it took
	// effect (Headless), nothing of the refused step was sent, and the steps
	// after it did not run.
	checkbox := "id:" + fixture.AutomationID(fixture.ElemCheckboxNotify)
	stdout, stderr, exit := runBatch(t, []string{
		window,
		"click", "id:" + fixture.AutomationID(fixture.ElemButtonClick),
		"then", "press-key", "Control_L+a",
		"then", "click", checkbox,
	})
	assertHeadedRefusal(t, "a Batch with press-key Control_L+a", stdout, stderr, exit,
		"winuse --headed press-key "+window+" Control_L+a")
	if !strings.Contains(stderr, "batch stopped at Action 2 (press-key)") {
		t.Errorf("the Batch refusal must name step 2: %q", stderr)
	}
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.LastButton == "Click Me" })
	time.Sleep(300 * time.Millisecond)
	st = fix.waitState(t, time.Second, func(*fixture.State) bool { return true })
	if st.TextSelected || st.LastKey != "" {
		t.Errorf("the refused press-key step reached the fixture: selected %v, last key %q", st.TextSelected, st.LastKey)
	}
	if st.Checkbox {
		t.Error("the step after the refused one ran: the checkbox is on")
	}
	assertForeground(t, "a Batch with a refused step", cover)
}
