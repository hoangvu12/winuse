//go:build e2e

// The Window Actions (ticket 02 of headless-parity): restore-window,
// minimize-window, move-window, resize-window and close-window on a
// covered fixture. User-run only (see the package comment and
// test/e2e/README.md); an agent must never start them.
//
// Each Action must leave the covering Window in the foreground and read
// back on the desktop, independently of winuse's own verification: the
// iconic state, the visible frame (win.WindowFrame: the screenshot line's
// rectangle, which move-window and resize-window take), the fixture's own
// report of where its canvas went, and the fixture process ending on
// close.

package e2e

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/hoangvu12/winuse/internal/fixture"
	"github.com/hoangvu12/winuse/internal/win"
)

// runWindowAction invokes `winuse <action> <window> [numbers]`.
func runWindowAction(t *testing.T, action, window string, numbers ...int) (string, string, int) {
	t.Helper()
	args := []string{action, window}
	for _, n := range numbers {
		args = append(args, strconv.Itoa(n))
	}
	return runWinuse(t, args, "WINUSE_HELPER_IDLE="+e2eIdle)
}

// near reports whether got is within the Window Actions' 8 px tolerance of
// want.
func near(got, want int32) bool {
	return abs32(got-want) <= 8
}

// waitProcessExit waits for a process to end (read-only: a SYNCHRONIZE
// handle and a wait).
func waitProcessExit(t *testing.T, pid uint32, timeout time.Duration) bool {
	t.Helper()
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return true // already gone
	}
	defer windows.CloseHandle(h)
	ev, err := windows.WaitForSingleObject(h, uint32(timeout/time.Millisecond))
	return err == nil && ev == windows.WAIT_OBJECT_0
}

func TestWindowActionsKeepTheForeground(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	id := mustWindowID(t, window)
	cover := coverWindow(t, window)

	// minimize-window, then a placement refused while minimized.
	stdout, stderr, exit := runWindowAction(t, "minimize-window", window)
	assertActionOK(t, "minimize-window", stdout, stderr, exit)
	if !win.IsMinimized(id) {
		t.Error("minimize-window: the Window is not minimized")
	}
	assertForeground(t, "minimize-window", cover)

	stdout, stderr, exit = runWindowAction(t, "move-window", window, 100, 100)
	assertActionFailed(t, "move-window on a minimized Window", stdout, stderr, exit, "is minimized")
	if !strings.Contains(stderr, "nothing was sent") || !strings.Contains(stderr, "winuse restore-window "+window) {
		t.Errorf("the refusal must say nothing was sent and name restore-window: %q", stderr)
	}

	// restore-window: back at its normal size, without the foreground.
	stdout, stderr, exit = runWindowAction(t, "restore-window", window)
	assertActionOK(t, "restore-window", stdout, stderr, exit)
	if win.IsMinimized(id) {
		t.Error("restore-window: the Window is still minimized")
	}
	assertForeground(t, "restore-window", cover)

	// move-window: the visible frame's top-left goes to the point, the size stays.
	r0, err := win.WindowFrame(id)
	if err != nil {
		t.Fatalf("reading the window rectangle: %v", err)
	}
	canvas0 := fix.waitState(t, time.Second, func(*fixture.State) bool { return true }).CanvasRect
	x, y := r0.Left+60, r0.Top+40
	stdout, stderr, exit = runWindowAction(t, "move-window", window, int(x), int(y))
	assertActionOK(t, "move-window", stdout, stderr, exit)
	r1, err := win.WindowFrame(id)
	if err != nil {
		t.Fatalf("reading the window rectangle: %v", err)
	}
	if !near(r1.Left, x) || !near(r1.Top, y) || !near(r1.Width(), r0.Width()) || !near(r1.Height(), r0.Height()) {
		t.Errorf("move-window to %d,%d: the Window is at %+v, was %+v", x, y, r1, r0)
	}
	// The fixture saw the move too: its canvas moved with the frame.
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool {
		return near(s.CanvasRect[0]-canvas0[0], r1.Left-r0.Left) && near(s.CanvasRect[1]-canvas0[1], r1.Top-r0.Top)
	})
	assertForeground(t, "move-window", cover)

	// resize-window: the size changes, the top-left stays.
	w, h := r1.Width()+80, r1.Height()+40
	stdout, stderr, exit = runWindowAction(t, "resize-window", window, int(w), int(h))
	assertActionOK(t, "resize-window", stdout, stderr, exit)
	r2, err := win.WindowFrame(id)
	if err != nil {
		t.Fatalf("reading the window rectangle: %v", err)
	}
	if !near(r2.Width(), w) || !near(r2.Height(), h) || !near(r2.Left, r1.Left) || !near(r2.Top, r1.Top) {
		t.Errorf("resize-window to %dx%d: the Window is %+v, was %+v", w, h, r2, r1)
	}
	assertForeground(t, "resize-window", cover)

	// close-window: WM_CLOSE ends the fixture (it asks nothing).
	stdout, stderr, exit = runWindowAction(t, "close-window", window)
	assertActionOK(t, "close-window", stdout, stderr, exit)
	if win.WindowOpen(id) {
		t.Error("close-window printed ok but the Window is still open")
	}
	if !waitProcessExit(t, uint32(fix.proc.Pid), 5*time.Second) {
		t.Error("the fixture process is still running 5 s after close-window")
	}
	assertForeground(t, "close-window", cover)
}
