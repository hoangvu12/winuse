//go:build e2e

// The Windows an Action opened (desktop-parity 02): a Headless click on
// the fixture's "Classic Dialog..." button reports the dialog as a new
// Window, with its Window ID, App and title, and a Headless Escape on the
// dialog reports it closed. User-run only (see the package comment and
// test/e2e/README.md); an agent must never start them.

package e2e

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
)

func TestActionReportsOpenedAndClosedWindows(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)

	idx := mainElementIndexes()[fixture.ElemButtonClassic]
	stdout, stderr, exit := runClick(t, []string{window, strconv.Itoa(idx)})
	assertActionOK(t, "Headless click (Classic Dialog...)", stdout, stderr, exit)
	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicOpen && s.ClassicWindowID != 0 })
	dialog := strconv.FormatUint(st.ClassicWindowID, 10)
	notes, _ := actionNotes(stdout)
	opened := "Note: a new Window opened: " + dialog + " "
	found := 0
	for _, n := range notes {
		if strings.HasPrefix(n, opened) {
			found++
			if !strings.HasSuffix(n, ` "`+fixture.ClassicDialogTitle+`"`) {
				t.Errorf("the dialog's note %q does not end with its title %q", n, fixture.ClassicDialogTitle)
			}
		}
	}
	if found != 1 {
		t.Errorf("click output %q: want one note %q... for the Classic Dialog", stdout, opened)
	}
	assertForeground(t, "the click that opened the Classic Dialog", cover)

	// The reported Window ID is the one to read next.
	if out, errOut, code := runWinuse(t, []string{"get-window", dialog}, "WINUSE_HELPER_IDLE="+e2eIdle); code != 0 || !strings.Contains(out, fixture.ClassicDialogTitle) {
		t.Errorf("get-window %s: exit %d, stdout %q, stderr %q", dialog, code, out, errOut)
	}

	coverAgain(t, cover)
	res, _ := runWatched(t, []string{"press-key", dialog, "Escape"})
	assertActionOK(t, "press-key Escape on the Classic Dialog", res.stdout, res.stderr, res.exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return !s.ClassicOpen })
	if !strings.Contains(res.stdout, "Note: window "+dialog+" closed\n") {
		t.Errorf("press-key Escape output %q: want %q", res.stdout, "Note: window "+dialog+" closed")
	}
	assertForeground(t, "press-key Escape on the Classic Dialog", cover)
}
