//go:build e2e

// A Batch across several Windows (desktop-parity ticket 24): one Batch
// clicks the covered fixture's "Message Box..." button, then presses
// Return in the MessageBox it opened (`in new`), all Headless. User-run
// only (see the package comment and test/e2e/README.md); an agent must
// never start them.

package e2e

import (
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
)

func TestBatchInNewActsInTheOpenedMessageBox(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)

	idx := mainElementIndexes()[fixture.ElemButtonMessageBox]
	stdout, stderr, exit := runBatch(t, []string{
		window, "click", strconv.Itoa(idx),
		"then", "in", "new", "press-key", "Return",
	})
	assertBatchOK(t, "batch: click Message Box..., then Return in new", stdout, stderr, exit, 2)
	if !regexp.MustCompile(`(?m)^Action 2 \(press-key in \d+\): ok`).MatchString(stdout) {
		t.Errorf("stdout %q: want Action 2 named with the MessageBox's Window ID", stdout)
	}
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return !s.MessageBoxOpen && s.MessageBoxResult == "ok" })
	assertForeground(t, "the Batch across the main Window and its MessageBox", cover)
}
