//go:build e2e

// The insertion caret (ticket 11 of multi-agent): after a Headless
// `type-text --into` a WPF TextBox, the Agent Cursor shows a short caret
// bar where the box's caret now is. The bar is one of the helper's
// `winuse.cursor` windows, a few pixels wide and a line high; the cursors
// are excluded from captures, so the test finds it in the window list and
// the user checks where it stands. The WPF window is the one
// caret_typing_test.go starts through Windows PowerShell, left uncovered
// here so the caret can be seen. User-run only (see the package comment
// and test/e2e/README.md); an agent must never start them.

package e2e

import (
	"testing"
	"time"
)

func TestTheInsertionCaretShowsAfterASplice(t *testing.T) {
	optIn(t)
	window, read := startWPFBox(t)
	if _, stderr, exit := runGetWindowState(t, window); exit != 0 {
		t.Fatalf("get-window-state: exit %d, stderr:\n%s", exit, stderr)
	}
	stdout, stderr, exit := runSelectText(t, window, "id:box", "5", "0")
	assertActionOK(t, "select-text (the caret after hello)", stdout, stderr, exit)
	stdout, stderr, exit = runTypeText(t, []string{window, "--into", "id:box", "XY"})
	assertActionOK(t, "type-text --into the WPF box", stdout, stderr, exit)
	waitText(t, read, "helloXY world")

	// The caret's window: a visible cursor window a few pixels wide and at
	// least a few lines' pixels high, within its 1.2 s showing.
	found := false
	deadline := time.Now().Add(time.Second)
	for !found && time.Now().Before(deadline) {
		for _, w := range agentCursorWindows(t) {
			if b := w.Bounds; b.Width() > 0 && b.Width() <= 6 && b.Height() >= 8 {
				found = true
				t.Logf("LOOK: the caret bar at %+v should stand right after \"helloXY\" in the box", b)
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !found {
		t.Error("no caret bar among the cursor windows after the splice")
	}
}
