//go:build e2e

// Posted Clicks (headless-max ticket 12): `click @x,y --count 2 --post`
// and `click @x,y --button right --post` on the covered fixture's Classic
// Dialog name Edit, a classic control. The research measured both on a
// background Edit (../../.scratch/headless-gaps/headless-100.md G1): the
// double click selects a word, the right click opens the Edit's context
// menu, and the foreground stays put. User-run only (see the package
// comment and test/e2e/README.md).

package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
	"github.com/hoangvu12/winuse/internal/win"
)

const postedClickOK = "ok (posted; the app may ignore posted clicks, read the Window State to check)\n"

func TestPostedClicksOnACoveredEdit(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)
	dialog := openClassicDialog(t, fix, window, cover)
	id := mustWindowID(t, dialog)

	// Two words in the name Edit (Headless typing into a focused classic
	// edit control), then the dialog's screenshot for the point.
	stdout, stderr, exit := runTypeText(t, []string{dialog, "alpha bravo"})
	assertActionOK(t, "type-text into the name Edit", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicText == "alpha bravo" })
	edit := win.FocusedControl(id)
	if !edit.IsEdit() {
		t.Fatalf("the dialog's focused control is %q, not its name Edit", edit.Class)
	}
	stdout, stderr, exit = runGetWindowStateScreenshot(t, dialog)
	if exit != 0 {
		t.Fatalf("get-window-state --screenshot: exit %d, stderr:\n%s", exit, stderr)
	}
	info := parseScreenshotLine(t, stdout)
	coverAgain(t, cover)

	// A point on the first word: a few pixels into the Edit, mid-height.
	x, y := shotCoordsOfScreen(t, dialog, info.scale, edit.Rect.Left+8, (edit.Rect.Top+edit.Rect.Bottom)/2)
	at := fmt.Sprintf("@%d,%d", x, y)

	res, overlaySeen := runWatched(t, []string{"click", dialog, at, "--count", "2", "--post"})
	if res.exit != 0 || res.stdout != postedClickOK {
		t.Fatalf("click %s --count 2 --post: exit %d, stdout %q, stderr %q", at, res.exit, res.stdout, res.stderr)
	}
	if overlaySeen {
		t.Error("the in-control banner was shown for a posted click")
	}
	assertForeground(t, "a posted double click", cover)
	st, err := win.ReadEditState(edit.HWND)
	if err != nil {
		t.Fatalf("reading the Edit's selection: %v", err)
	}
	if !st.SelKnown || min(st.SelStart, st.SelEnd) != 0 || max(st.SelStart, st.SelEnd) < 5 {
		t.Errorf("after the posted double click the Edit selects %d to %d, want the first word (0 to 5 or 6)", st.SelStart, st.SelEnd)
	}

	// The context menu it opens is reported after the ok (desktop-parity 02).
	res, overlaySeen = runWatched(t, []string{"click", dialog, at, "--button", "right", "--post"})
	if res.exit != 0 || !strings.HasPrefix(res.stdout, postedClickOK) {
		t.Fatalf("click %s --button right --post: exit %d, stdout %q, stderr %q", at, res.exit, res.stdout, res.stderr)
	}
	if overlaySeen {
		t.Error("the in-control banner was shown for a posted right click")
	}
	// The Edit's context menu: a #32768 popup of the fixture's process. It
	// closes with the fixture when the test ends.
	menu := uint64(0)
	deadline := time.Now().Add(5 * time.Second)
	for menu == 0 && time.Now().Before(deadline) {
		all, err := win.Collect()
		if err != nil {
			t.Fatalf("enumerating windows: %v", err)
		}
		for _, w := range all {
			if w.Class == "#32768" && w.PID == uint32(fix.proc.Pid) {
				menu = w.ID
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if menu == 0 {
		t.Error("no context menu of the fixture opened after the posted right click")
	}
	assertForeground(t, "a posted right click", cover)
}
