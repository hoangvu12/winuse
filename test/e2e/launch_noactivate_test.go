//go:build e2e

// launch-app without the foreground (headless-max ticket 01): the App
// starts with a non-activating show state behind a covering Window, and
// launch-app says so when an App takes the foreground anyway. User-run
// only (see the package comment and test/e2e/README.md).
//
// The cover is a fixture brought to the foreground with activate-window,
// a Headed Action: its input gives the helper the foreground right until
// the next real input, and a process the helper starts inherits it. That
// is the case the ticket names for `browser launch`, so the launch has to
// keep the foreground through its show state, not through the foreground
// lock.

package e2e

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
	"github.com/hoangvu12/winuse/internal/win"
)

// startCover starts a fixture and makes it the foreground through
// activate-window; it returns its Window ID.
func startCover(t *testing.T) uint64 {
	t.Helper()
	fix := startFixture(t)
	id := fix.waitState(t, 15*time.Second, func(s *fixture.State) bool { return s.WindowID != 0 }).WindowID
	stdout, stderr, exit := runActivateWindow(t, strconv.FormatUint(id, 10))
	assertActionOK(t, "activate-window (the cover)", stdout, stderr, exit)
	waitForeground(t, id, 5*time.Second)
	waitOverlayGone(t, 5*time.Second)
	return id
}

// launchFixture runs launch-app on the fixture with its state file passed
// through --arg, plus extra options, and returns the output and the
// launched fixture's Window ID (killed on cleanup).
func launchFixture(t *testing.T, cover uint64, extra ...string) (stdout, stderr string, id uint64) {
	t.Helper()
	statePath := filepath.Join(t.TempDir(), "launched-state.json")
	fb, err := win.FrameBounds(cover)
	if err != nil {
		t.Fatalf("reading the cover's frame: %v", err)
	}
	args := []string{"launch-app", fixtureBinary(t), "--arg", "-state", "--arg", statePath,
		"--env", fmt.Sprintf("WINUSE_FIXTURE_ORIGIN=%d,%d", fb.Left+40, fb.Top+40), "--cwd", t.TempDir()}
	args = append(args, extra...)
	stdout, stderr, exit := runWinuse(t, args, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("launch-app: exit %d, stderr:\n%s", exit, stderr)
	}
	id = waitStateFile(t, statePath, 15*time.Second, func(s *fixture.State) bool { return s.WindowID != 0 }).WindowID
	t.Cleanup(func() {
		if win.WindowOpen(id) {
			killWindowProcess(t, id)
		}
	})
	return stdout, stderr, id
}

// TestLaunchAppKeepsTheForeground: the fixture launched by path behind the
// cover opens its Window without taking the foreground, and launch-app
// prints the Window line alone. The fixture takes its state file from
// --arg and its origin from --env (it reads WINUSE_FIXTURE_ORIGIN), so
// both options reach it.
func TestLaunchAppKeepsTheForeground(t *testing.T) {
	optIn(t)
	cover := startCover(t)
	stdout, stderr, id := launchFixture(t, cover)
	if want := itoa64(id) + "\twinusefixture\t" + fixture.WindowTitle + "\n"; stdout != want {
		t.Errorf("launch-app stdout = %q, want the Window line alone: %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("unexpected stderr: %q", stderr)
	}
	assertForeground(t, "launch-app", cover)
	fb, err := win.FrameBounds(id)
	cb, cerr := win.FrameBounds(cover)
	if err != nil || cerr != nil {
		t.Fatalf("reading the frames: %v, %v", err, cerr)
	}
	if !near(fb.Left, cb.Left+40) || !near(fb.Top, cb.Top+40) {
		t.Errorf("the launched fixture is at %d,%d, want --env's origin %d,%d", fb.Left, fb.Top, cb.Left+40, cb.Top+40)
	}
	time.Sleep(time.Second) // an App that activates late would show here
	assertForeground(t, "launch-app, a second later", cover)
}

// TestLaunchAppMinimized: --minimized opens the fixture's Window minimized
// (SW_SHOWMINNOACTIVE), and the cover keeps the foreground.
func TestLaunchAppMinimized(t *testing.T) {
	optIn(t)
	cover := startCover(t)
	stdout, stderr, id := launchFixture(t, cover, "--minimized")
	if !strings.HasPrefix(stdout, itoa64(id)+"\t") || strings.Contains(stdout, "Note:") {
		t.Errorf("launch-app --minimized stdout = %q, want the fixture's Window line alone", stdout)
	}
	if stderr != "" {
		t.Errorf("unexpected stderr: %q", stderr)
	}
	if !win.IsMinimized(id) {
		t.Error("launch-app --minimized: the Window is not minimized")
	}
	assertForeground(t, "launch-app --minimized", cover)
}

// TestLaunchAppNotepadReportsTheForeground: Windows 11 Notepad is a
// packaged App, launched by its App User Model ID. Its activation has no
// no-foreground option and took the foreground for Calculator
// (.scratch/headless-gaps/probes-round5.md section 5), so the test does
// not demand the foreground stays: it demands that launch-app's note says
// exactly what happened. It logs which it was.
func TestLaunchAppNotepadReportsTheForeground(t *testing.T) {
	optIn(t)
	all, err := win.Collect()
	if err != nil {
		t.Fatalf("enumerating windows: %v", err)
	}
	for _, w := range all {
		if w.Class == "Notepad" {
			t.Skip("Notepad is already running: close it first (this test ends the Notepad process it launches)")
		}
	}
	cover := startCover(t)
	stdout, stderr, exit := runWinuse(t, []string{"launch-app", "Microsoft.WindowsNotepad_8wekyb3d8bbwe!App"}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("launch-app Notepad: exit %d, stderr:\n%s", exit, stderr)
	}
	line, note, _ := strings.Cut(stdout, "\n")
	id, err := strconv.ParseUint(strings.SplitN(line, "\t", 2)[0], 10, 64)
	if err != nil {
		t.Fatalf("launch-app Notepad: stdout %q has no Window line", stdout)
	}
	t.Cleanup(func() {
		if win.WindowOpen(id) {
			killWindowProcess(t, id)
		}
	})
	fg := win.ForegroundWindow()
	took := fg != cover && win.WindowPID(fg) == win.WindowPID(id)
	t.Logf("measured: Notepad took the foreground: %v (foreground %s; stdout %q)", took, describeWindow(t, fg), stdout)
	switch {
	case took && note != "Note: the App took the foreground; winuse does not take it back.\n":
		t.Errorf("Notepad took the foreground, but launch-app printed %q", stdout)
	case !took && note != "":
		t.Errorf("Notepad did not take the foreground, but launch-app printed %q", stdout)
	case !took:
		assertForeground(t, "launch-app Notepad", cover)
	}
}
