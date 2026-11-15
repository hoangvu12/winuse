//go:build e2e

// --observe follows the Window the Action opened (learn-from 01, ADR 0021
// amendment): a Headless click on the fixture's "Classic Dialog..." button
// with --observe prints the main Window's diff, then the dialog's whole
// Window State under a line naming its Window ID, and an Action on that
// Window ID works at once. The foreground stays with the cover.
// User-run only (see the package comment and test/e2e/README.md); an agent
// must never start them.

package e2e

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
	"github.com/hoangvu12/winuse/internal/win"
)

func TestObserveReadsTheOpenedDialog(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)

	idx := mainElementIndexes()[fixture.ElemButtonClassic]
	out, stderr, exit := runWinuse(t, []string{"click", window, strconv.Itoa(idx), "--observe"}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("click --observe: exit %d, stderr:\n%s", exit, stderr)
	}
	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicOpen && s.ClassicWindowID != 0 })
	dialog := strconv.FormatUint(st.ClassicWindowID, 10)
	head := "The Window the Action opened, " + dialog + ", read whole; its Element Indexes go with window " + dialog + ":\n"
	main := strings.Index(out, " read of window "+window) // the diff's header, or the whole State's note
	opened := strings.Index(out, head)
	if main < 0 || opened < main {
		t.Fatalf("click --observe prints the main Window, then the dialog:\n%s", out)
	}
	if !strings.Contains(out[opened:], `Window: "`+fixture.ClassicDialogTitle+`"`) {
		t.Errorf("the dialog's Window State follows its line:\n%s", out[opened:])
	}
	if fg := win.ForegroundWindow(); fg != cover {
		t.Errorf("the foreground is %s; the cover %d must stay in front", describeWindow(t, fg), cover)
	}

	// The printed Window ID is the one to act on: Escape on it closes the
	// dialog without another read.
	coverAgain(t, cover)
	res, _ := runWatched(t, []string{"press-key", dialog, "Escape"})
	assertActionOK(t, "press-key Escape on the observed Classic Dialog", res.stdout, res.stderr, res.exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return !s.ClassicOpen })
}
