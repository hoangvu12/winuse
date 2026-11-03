//go:build e2e

// Point forms resolved to an Element in the target Window (headless-max
// ticket 11): `click @x,y` (left, single), `set-value @x,y` and
// `scroll @x,y` on the covered fixture act on the fixture's own Element
// under the point, Headless. The cover keeps the foreground, the banner
// stays down, the real pointer stays where it was, and the output names
// the Element. User-run only (see the package comment and
// test/e2e/README.md).

package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
)

func TestPointFormsActHeadlessOnTheCoveredWindow(t *testing.T) {
	optIn(t)
	fix, window, info := startFixtureForShots(t)
	cover := coverWindow(t, window)

	// The screenshot pixel at the centre of an Element's client rectangle
	// (fixture.Rect: the fixture's own layout, list items at the top).
	at := func(e fixture.Elem) string {
		r, ok := fixture.Rect(e, 0)
		if !ok {
			t.Fatalf("no layout rectangle for %s", e)
		}
		x, y := shotCoordsOfClient(t, fix, window, info.scale, (r.Left+r.Right)/2, (r.Top+r.Bottom)/2)
		return fmt.Sprintf("@%d,%d", x, y)
	}
	run := func(what string, args []string, wantIn string) {
		t.Helper()
		pointer := pointerPos(t)
		res, overlaySeen := runWatched(t, args)
		if res.exit != 0 {
			t.Fatalf("%s: exit %d, stderr:\n%s", what, res.exit, res.stderr)
		}
		if !strings.HasPrefix(res.stdout, "ok (") || !strings.Contains(res.stdout, wantIn) {
			t.Errorf("%s: stdout %q, want an ok that names %q", what, res.stdout, wantIn)
		}
		if overlaySeen {
			t.Errorf("%s: the in-control banner was shown for a Headless Action", what)
		}
		if got := pointerPos(t); got != pointer {
			t.Errorf("%s: the real pointer moved from %v to %v", what, pointer, got)
		}
		assertForeground(t, what, cover)
	}

	button := at(fixture.ElemButtonClick)
	run("click at the Click Me button", []string{"click", window, button}, `"Click Me" at `+button)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.LastButton == "Click Me" })

	item := at(fixture.ElemItemAlpha)
	run("click at the Alpha list item", []string{"click", window, item}, `"Alpha" at `+item)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.SelectedItem == "Alpha" })

	checkbox := at(fixture.ElemCheckboxNotify)
	run("set-value on at the checkbox", []string{"set-value", window, checkbox, "on"}, `"Notify me" at `+checkbox)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Checkbox })

	// One wheel notch is three Element scroll steps: the list (8 items, 5
	// visible) scrolls to its end, then back.
	list := at(fixture.ElemItemBravo)
	run("scroll down at the list", []string{"scroll", window, list, "0", "120"}, "scrolled element")
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ListFirstVisible == 3 })
	run("scroll up at the list", []string{"scroll", window, list, "0", "-120"}, "scrolled element")
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ListFirstVisible == 0 })

	// The canvas offers no click route: refused with nothing sent and the
	// --headed retry, as before this ticket.
	canvas := at(fixture.ElemCanvas)
	res, _ := runWatched(t, []string{"click", window, canvas})
	assertHeadedRefusal(t, "click at the canvas", res.stdout, res.stderr, res.exit, "winuse --headed click "+window+" "+canvas)
	assertForeground(t, "click at the canvas", cover)
	if st := fix.waitState(t, time.Second, func(*fixture.State) bool { return true }); len(st.CanvasClicks) != 0 {
		t.Errorf("the refused click reached the canvas: %v", st.CanvasClicks)
	}
}
