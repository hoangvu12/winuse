//go:build e2e

// Modifiers on clicks (desktop-parity ticket 06). The fixture's list is
// single-select (its Selection reports CanSelectMultiple false), so a
// Ctrl+click on one of its items has no Headless route: the multi-select
// route reads the list and refuses with nothing sent and the exact
// --headed retry, and the cover keeps the foreground. The Classic
// Dialog's fruit list box is multi-select (LBS_EXTENDEDSEL): Ctrl+click
// and Shift+click select in it Headless, read back by the route and here
// by LB_GETSEL. User-run only (see the package comment and
// test/e2e/README.md).

package e2e

import (
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"

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

var (
	procGetDlgItemE2E  = user32.NewProc("GetDlgItem")
	procSendMessageE2E = user32.NewProc("SendMessageW")
)

// listSelection reads which items of the Classic Dialog's fruit list box
// are selected (LB_GETSEL, read only).
func listSelection(t *testing.T, dialog uint64) []string {
	t.Helper()
	const lbGetSel = 0x0187
	list, _, _ := procGetDlgItemE2E.Call(uintptr(dialog), fixture.ClassicListID)
	if list == 0 {
		t.Fatalf("the Classic Dialog has no list box %d", fixture.ClassicListID)
	}
	var sel []string
	for i, name := range fixture.ClassicFruits {
		if r, _, _ := procSendMessageE2E.Call(list, lbGetSel, uintptr(i), 0); r != 0 && int32(r) > 0 {
			sel = append(sel, name)
		}
	}
	return sel
}

func TestCtrlAndShiftClickSelectInAMultiSelectList(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)
	dialog := openClassicDialog(t, fix, window, cover)
	id := windows.HWND(mustWindowID(t, dialog))
	if _, stderr, exit := runGetWindowState(t, dialog); exit != 0 {
		t.Fatalf("get-window-state: exit %d, stderr:\n%s", exit, stderr)
	}

	click := func(item, mods string, want []string) {
		t.Helper()
		res, overlaySeen := runWatched(t, []string{"click", dialog, "name:" + item, "--modifiers", mods})
		if res.exit != 0 || !strings.HasPrefix(res.stdout, "ok") {
			t.Fatalf("%s+click %s: exit %d, stdout %q, stderr %q", mods, item, res.exit, res.stdout, res.stderr)
		}
		if overlaySeen {
			t.Errorf("%s+click %s: the in-control banner was shown", mods, item)
		}
		assertForeground(t, mods+"+click "+item, cover)
		deadline := time.Now().Add(3 * time.Second)
		var got []string
		for time.Now().Before(deadline) {
			if got = listSelection(t, uint64(id)); slices.Equal(got, want) {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Errorf("after %s+click %s the list selects %v, want %v", mods, item, got, want)
	}

	click("Apple", "ctrl", []string{"Apple"})
	click("Cherry", "ctrl", []string{"Apple", "Cherry"})
	// Shift adds the range from the last selected item (Cherry) to Elder.
	click("Elder", "shift", []string{"Apple", "Cherry", "Damson", "Elder"})
	// Ctrl on a selected item takes it out again.
	click("Apple", "ctrl", []string{"Cherry", "Damson", "Elder"})
}
