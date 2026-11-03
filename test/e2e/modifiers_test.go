//go:build e2e

// Modifiers on clicks (desktop-parity ticket 06). The fixture's list is
// single-select (its Selection reports CanSelectMultiple false), so a
// Ctrl+click on one of its items has no Headless route: the multi-select
// route reads the list and refuses with nothing sent and the exact
// --headed retry, and the cover keeps the foreground. The positive
// multi-select leg (two items selected, covered) needs a multi-select list
// in the fixture and is not here; probes round 4 section 6 measured it on
// WPF, SysListView32 and WinUI 3. User-run only (see the package comment
// and test/e2e/README.md).

package e2e

import (
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
)

func TestCtrlClickOnASingleSelectListIsRefused(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)
	before := fix.waitState(t, time.Second, func(*fixture.State) bool { return true })

	item := "id:" + fixture.AutomationID(fixture.ElemItemAlpha)
	res, overlaySeen := runWatched(t, []string{"click", window, item, "--modifiers", "ctrl"})
	assertHeadedRefusal(t, "ctrl+click on a single-select item", res.stdout, res.stderr, res.exit,
		"winuse --headed click "+window+" "+item+" --modifiers ctrl")
	if overlaySeen {
		t.Error("the in-control banner was shown for a refused Action")
	}
	assertForeground(t, "ctrl+click on a single-select item", cover)
	time.Sleep(300 * time.Millisecond)
	if after := fix.waitState(t, time.Second, func(*fixture.State) bool { return true }); after.SelectedItem != before.SelectedItem {
		t.Errorf("the refused ctrl+click changed the selection from %q to %q", before.SelectedItem, after.SelectedItem)
	}
}
