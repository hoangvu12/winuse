//go:build e2e

// Select by name (desktop-parity 12): `winuse set-value <list or combo>
// <label>` picks the item with that name, headless. The targets are the
// fixture's drawn Item List (its own UIA provider: Selection, items with
// SelectionItem, three below the fold) and the Classic Dialog's Win32
// drop-down combo (the system proxy: ExpandCollapse, Selection, a
// read-only Value; fixture.ClassicColors). User-run only (see the
// package comment and test/e2e/README.md); an agent must never start
// them.

package e2e

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
)

func TestSetValueSelectsAListItemByName(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	list := mainElementIndexes()[fixture.ElemItemList]

	stdout, stderr, exit := runSetvalue(t, window, list, "Charlie")
	assertActionOK(t, "set-value Item List Charlie", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.SelectedItem == "Charlie" })

	// Case-insensitive when nothing matches exactly; an item below the
	// fold is in the search too.
	stdout, stderr, exit = runSetvalue(t, window, list, "golf")
	assertActionOK(t, "set-value Item List golf", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.SelectedItem == "Golf" })

	// No such item: a list has nothing to open, so nothing was sent.
	stdout, stderr, exit = runSetvalue(t, window, list, "Zulu")
	assertActionFailed(t, "set-value Item List Zulu", stdout, stderr, exit,
		`no item named "Zulu"; its items are "Alpha", "Bravo"`)
	if !strings.Contains(stderr, "; nothing was sent") {
		t.Errorf("a refusal on a list (nothing to open) must say nothing was sent: %q", stderr)
	}
	st := fix.waitState(t, time.Second, func(s *fixture.State) bool { return true })
	if st.SelectedItem != "Golf" {
		t.Errorf("selectedItem = %q after a refused set-value, want Golf", st.SelectedItem)
	}
}

func TestSetValueSelectsAComboItemByName(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)
	dialog := openClassicDialog(t, fix, window, cover)
	if _, stderr, exit := runGetWindowState(t, dialog); exit != 0 {
		t.Fatalf("get-window-state %s: exit %d, stderr:\n%s", dialog, exit, stderr)
	}
	combo := "id:" + strconv.Itoa(fixture.ClassicComboID)
	setCombo := func(value string) (string, string, int) {
		return runWinuse(t, []string{"set-value", dialog, combo, value}, "WINUSE_HELPER_IDLE="+e2eIdle)
	}

	// The collapsed combo is expanded, the item selected, the combo
	// collapsed again; the covering Window keeps the foreground.
	stdout, stderr, exit := setCombo("Green")
	assertActionOK(t, "set-value combo Green", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicColor == "Green" })
	assertForeground(t, "set-value on the combo", cover)

	// An exact name wins over one that differs only in case.
	stdout, stderr, exit = setCombo("blue")
	assertActionOK(t, "set-value combo blue", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicColor == "blue" })

	// Two case-insensitive matches are refused, listing them.
	stdout, stderr, exit = setCombo("BLUE")
	assertActionFailed(t, "set-value combo BLUE", stdout, stderr, exit, `2 items match "BLUE": "Blue", "blue"`)

	// No such item: the combo was opened for the search and closed again.
	stdout, stderr, exit = setCombo("Purple")
	assertActionFailed(t, "set-value combo Purple", stdout, stderr, exit,
		`the list was opened and closed; no item named "Purple"`)

	st := fix.waitState(t, time.Second, func(s *fixture.State) bool { return true })
	if st.ClassicColor != "blue" {
		t.Errorf("classicColor = %q after the refusals, want blue", st.ClassicColor)
	}
	assertForeground(t, "the refused set-values", cover)
}
