//go:build e2e

// Polite real input (multi-agent ticket 08): a Headed Action gives the
// user's foreground and cursor back when it is done. The user's Window is
// a covering fixture; the Headed Action is a double click on the target
// fixture's canvas, which opens nothing, so the cover gets the foreground
// back and the pointer returns to where it was parked. User-run only (see
// the package comment and test/e2e/README.md).

package e2e

import (
	"strconv"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
)

func TestHeadedClickGivesTheForegroundAndCursorBack(t *testing.T) {
	optIn(t)
	parkPointer(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)
	pointer := pointerPos(t)

	canvas := strconv.Itoa(mainElementIndexes()[fixture.ElemCanvas])
	stdout, stderr, exit := runClickHeaded(t, []string{window, canvas, "--count", "2"})
	assertActionOK(t, "--headed click --count 2 on the canvas", stdout, stderr, exit)
	if stdout != "ok\n" {
		t.Errorf("stdout = %q, want ok alone (nothing was left open)", stdout)
	}
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.CanvasDoubleClicks >= 1 })

	// The give-back is done before the output is written; a Chromium-style
	// re-activation would show here, a moment later.
	assertForeground(t, "after the Headed click", cover)
	time.Sleep(500 * time.Millisecond)
	assertForeground(t, "after the Headed click, a moment later", cover)
	if got := pointerPos(t); abs32(got[0]-pointer[0]) > 2 || abs32(got[1]-pointer[1]) > 2 {
		t.Errorf("the pointer is at %v after the Headed click, want it back at %v", got, pointer)
	}
}
