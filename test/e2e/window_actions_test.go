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
	"os/exec"
	"path/filepath"
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
	// The fixture is the foreground once before it is covered: an App that
	// has not been in front since it started takes the foreground on its
	// first restore (TestRestoreWindowReportsSelfActivation).
	stdout, stderr, exit := runActivateWindow(t, window)
	assertActionOK(t, "activate-window (the fixture)", stdout, stderr, exit)
	waitForeground(t, id, 5*time.Second)
	waitOverlayGone(t, 5*time.Second)
	cover := coverWindow(t, window)

	// minimize-window, then a placement refused while minimized.
	stdout, stderr, exit = runWindowAction(t, "minimize-window", window)
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

	// restore-window: back at its normal size, without the foreground. The
	// fixture has been in front before, so it does not take the foreground
	// back and the output is a plain ok, with no self-activation note
	// (headless-max ticket 02).
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
	// The restore is asynchronous: wait for the fixture to report its
	// canvas inside the restored frame, not the minimized position.
	canvas0 := fix.waitState(t, 2*time.Second, func(s *fixture.State) bool {
		c := s.CanvasRect
		return c[0] >= r0.Left && c[1] >= r0.Top && c[2] <= r0.Right && c[3] <= r0.Bottom
	}).CanvasRect
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

// TestRestoreWindowReportsSelfActivation (headless-max ticket 02): an App
// that brings itself to the foreground when it is restored. The target is
// the fixture in its -activate-on-restore mode: it calls
// SetForegroundWindow on itself when it is restored from minimized, as
// some real Apps do. Windows lets a background process do that only with
// the foreground process's grant (or in input states the test cannot
// arrange), so the cover, the user's stand-in, grants it
// (-allow-foreground <pid>, AllowSetForegroundWindow); winuse's restore
// grants nothing. Covered, minimized and restored by the non-activating
// restore-window, the fixture takes the foreground, and restore-window
// must say so; winuse does not take the foreground back.
//
// charmap, which the research measured (6 of 6 in
// .scratch/headless-gaps/headless-100.md section 0), takes the foreground
// on restore only when it has not been the foreground since it started,
// and whether a process the test starts takes the foreground at launch
// depends on the desktop's input state, so it is not a reliable target
// (ticket 02's 2026-11-03 comment).
func TestRestoreWindowReportsSelfActivation(t *testing.T) {
	optIn(t)
	statePath := filepath.Join(t.TempDir(), "state.json")
	cmd := exec.Command(fixtureBinary(t), "-state", statePath, "-activate-on-restore")
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the fixture app: %v", err)
	}
	proc := cmd.Process
	t.Cleanup(func() {
		_ = proc.Kill()
		_, _ = proc.Wait()
	})
	id := waitStateFile(t, statePath, 15*time.Second, func(s *fixture.State) bool { return s.WindowID != 0 }).WindowID
	window := strconv.FormatUint(id, 10)
	cover := coverWindow(t, window, "-allow-foreground", strconv.Itoa(proc.Pid))

	stdout, stderr, exit := runWindowAction(t, "minimize-window", window)
	assertActionOK(t, "minimize-window", stdout, stderr, exit)
	assertForeground(t, "minimize-window", cover)

	stdout, stderr, exit = runWindowAction(t, "restore-window", window)
	if exit != 0 {
		t.Fatalf("restore-window: exit %d, stderr:\n%s", exit, stderr)
	}
	if win.IsMinimized(id) {
		t.Error("restore-window: the Window is still minimized")
	}
	if fg := win.ForegroundWindow(); fg != id {
		t.Fatalf("the fixture did not bring itself to the foreground (the foreground is %s; restore-window printed %q)", describeWindow(t, fg), stdout)
	}
	if want := "ok (the App brought itself to the foreground)\n"; stdout != want {
		t.Errorf("restore-window: stdout = %q, want %q (the fixture took the foreground)", stdout, want)
	}
	if stderr != "" {
		t.Errorf("restore-window: unexpected stderr: %q", stderr)
	}
}
