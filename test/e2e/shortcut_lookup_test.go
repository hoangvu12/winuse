//go:build e2e

// press-key's shortcut lookup (ticket 06 of headless-max): a chord the
// Window labels on a command runs that command Headless instead of being
// refused. The target is the covered fixture's Classic Dialog, whose real
// HMENU labels Edit > Select All with Ctrl+A ("Select &All\tCtrl+A"). User-run
// only (see the package comment and test/e2e/README.md); an agent must
// never start them.

package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
)

func TestShortcutRunsAClassicMenuCommand(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)
	dialog := openClassicDialog(t, fix, window, cover)

	stdout, stderr, exit := runTypeText(t, []string{dialog, "hello world"})
	assertActionOK(t, "type-text into the Classic Dialog's Edit", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicText == "hello world" })

	res, overlaySeen := runWatched(t, []string{"press-key", dialog, "Control_L+a"})
	assertActionOK(t, "press-key Control_L+a (the Select All shortcut)", res.stdout, res.stderr, res.exit)
	if !strings.Contains(res.stdout, `invoked "Edit > Select All" through its shortcut`) {
		t.Errorf("output = %q, want the shortcut note", res.stdout)
	}
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicMenu == "select-all" })
	if overlaySeen {
		t.Error("the in-control banner was shown during the shortcut")
	}
	stdout, stderr, exit = runTypeText(t, []string{dialog, "replaced"})
	assertActionOK(t, "type-text replacing the selection", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicText == "replaced" })
	assertForeground(t, "a shortcut through the menu", cover)

	// A chord the dialog labels nowhere keeps press-key's refusal.
	stdout, stderr, exit = runPressKey(t, []string{dialog, "Control_L+q"})
	if exit == 0 || !strings.Contains(stderr, "nothing was sent") {
		t.Errorf("press-key Control_L+q: exit %d, stdout %q, stderr %q, want the refusal", exit, stdout, stderr)
	}
}
