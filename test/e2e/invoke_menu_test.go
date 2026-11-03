//go:build e2e

// invoke-menu (ticket 08 of headless-max): `invoke-menu <window> "Edit >
// Select All"` runs a classic menu bar command through a posted
// WM_COMMAND, without opening the menu and without the foreground. The
// target is the covered fixture's Classic Dialog, whose menu bar is a real
// HMENU (see fixture.ClassicMenuFillID): Select All is checked by a
// type-text that replaces the selection, Clear Name is enabled only in
// WM_INITMENUPOPUP (so it passes only because winuse sends that message
// before it reads the Edit menu), and Locked is always greyed. User-run
// only (see the package comment and test/e2e/README.md); an agent must
// never start them.

package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
)

// runInvokeMenu invokes `winuse invoke-menu <args...>`.
func runInvokeMenu(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	return runWinuse(t, append([]string{"invoke-menu"}, args...), "WINUSE_HELPER_IDLE="+e2eIdle)
}

func TestHeadlessInvokeMenuOnClassicDialog(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)
	dialog := openClassicDialog(t, fix, window, cover)
	logKeyTarget(t, mustWindowID(t, dialog))
	pointer := pointerPos(t)

	stdout, stderr, exit := runTypeText(t, []string{dialog, "hello world"})
	assertActionOK(t, "type-text into the Classic Dialog's Edit", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicText == "hello world" })

	// Edit > Select All, then a type-text replaces the whole text.
	res, overlaySeen := runWatched(t, []string{"invoke-menu", dialog, "Edit > Select All"})
	assertActionOK(t, "invoke-menu Edit > Select All", res.stdout, res.stderr, res.exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicMenu == "select-all" })
	assertForeground(t, "invoke-menu Edit > Select All", cover)
	if overlaySeen {
		t.Error("the in-control banner was shown during invoke-menu")
	}
	if got := pointerPos(t); got != pointer {
		t.Errorf("the real pointer moved from %v to %v during invoke-menu", pointer, got)
	}
	stdout, stderr, exit = runTypeText(t, []string{dialog, "bye"})
	assertActionOK(t, "type-text replacing the selection", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicText == "bye" })

	// Edit > Clear Name is greyed until WM_INITMENUPOPUP enables it.
	stdout, stderr, exit = runInvokeMenu(t, dialog, "edit > clear name")
	assertActionOK(t, "invoke-menu Edit > Clear Name", stdout, stderr, exit)
	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicMenu == "clear-name" && s.ClassicText == "" })
	if st.ClassicMenuInits == 0 {
		t.Error("the Edit menu received no WM_INITMENUPOPUP")
	}
	assertForeground(t, "invoke-menu Edit > Clear Name", cover)

	// A greyed item is refused with nothing sent.
	stdout, stderr, exit = runInvokeMenu(t, dialog, "Edit > Locked")
	if exit == 0 || !strings.Contains(stderr, "greyed out") || !strings.Contains(stderr, "nothing was sent") {
		t.Errorf("invoke-menu on a greyed item: exit %d, stdout %q, stderr %q", exit, stdout, stderr)
	}
	if st := fix.waitState(t, time.Second, func(*fixture.State) bool { return true }); st.ClassicMenu != "clear-name" {
		t.Errorf("the greyed item ran: classicMenu = %q", st.ClassicMenu)
	}

	// The ellipsis and the case are optional; the & markers never typed.
	stdout, stderr, exit = runInvokeMenu(t, dialog, "file", ">", "FILL NAME")
	assertActionOK(t, "invoke-menu File > Fill Name", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicText == fixture.ClassicFillText })

	// A Batch step: Select All, then the replacement.
	res, overlaySeen = runWatched(t, []string{"batch", dialog, "invoke-menu", "Edit > Select All", "then", "type-text", "from a batch"})
	assertBatchOK(t, "batch invoke-menu then type-text", res.stdout, res.stderr, res.exit, 2)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicText == "from a batch" })
	if overlaySeen {
		t.Error("the in-control banner was shown during an all-Headless Batch")
	}
	assertForeground(t, "batch invoke-menu then type-text", cover)

	// The main window draws its own menu bar: there is no HMENU.
	stdout, stderr, exit = runInvokeMenu(t, window, "File > New")
	if exit == 0 || !strings.Contains(stderr, "no classic menu bar") || !strings.Contains(stderr, "nothing was sent") {
		t.Errorf("invoke-menu on the drawn menu bar: exit %d, stdout %q, stderr %q", exit, stdout, stderr)
	}
	assertForeground(t, "invoke-menu refusals", cover)
}
