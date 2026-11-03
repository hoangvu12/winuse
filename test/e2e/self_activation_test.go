//go:build e2e

// Self-activation after a Headless Action (headless-max 03, detect and
// report). The safe probe batch measured the fixture's Classic Dialog take
// the foreground 19 ms after a Headless click opened it, once the
// foreground lock had lapsed (the user's last-input process had exited).
// That cannot be set up on demand, so this test checks the contract both
// ways: when the dialog (or the fixture) ends up in the foreground, the
// click's output says `Note: the App took the foreground`; when the cover
// keeps it, the output says nothing of the kind. The log says which case
// ran. User-run only (see the package comment and test/e2e/README.md); an
// agent must never start it.

package e2e

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
	"github.com/hoangvu12/winuse/internal/win"
)

func TestHeadlessClickReportsSelfActivation(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)

	idx := mainElementIndexes()[fixture.ElemButtonClassic]
	stdout, stderr, exit := runClick(t, []string{window, strconv.Itoa(idx)})
	if exit != 0 {
		t.Fatalf("Headless click (Classic Dialog...): exit %d, stderr:\n%s", exit, stderr)
	}
	// Read at once: the output was written after the settle, and a later
	// activation is past what winuse watches.
	fg := win.ForegroundWindow()
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicOpen && s.ClassicWindowID != 0 })
	took := fg != cover && win.WindowPID(fg) == uint32(fix.proc.Pid)
	noted := strings.Contains(stdout, "Note: the App took the foreground")
	switch {
	case took && !noted:
		t.Errorf("the fixture took the foreground (%s), but the click's output does not say so: %q", describeWindow(t, fg), stdout)
	case !took && noted:
		t.Errorf("the click's output says the App took the foreground, but %s holds it: %q", describeWindow(t, fg), stdout)
	case took:
		t.Logf("the fixture took the foreground and the output said so: %q", stdout)
	default:
		t.Logf("the cover kept the foreground and the output said nothing of it: %q", stdout)
	}
}
