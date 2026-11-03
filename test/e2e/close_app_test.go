//go:build e2e

// close-app (desktop-parity ticket 10): one command closes every Window
// of an App and waits for its processes to exit; --force ends an App that
// stays open. User-run only (see the package comment and
// test/e2e/README.md); an agent must never start them.
//
// The App is a copy of the fixture under a name of its own, so close-app
// can only ever reach the fixture processes the test started. A plain
// fixture runs beside it as another App, which must stay. The protected
// list is tested offline only: a live test would aim at the user's own
// explorer.exe.

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
	"github.com/hoangvu12/winuse/internal/win"
)

// closeAppFixture copies the fixture to a uniquely named executable and
// returns its path and App name (the base name: a Go binary has no
// FileDescription).
func closeAppFixture(t *testing.T) (exe, app string) {
	t.Helper()
	data, err := os.ReadFile(fixtureBinary(t))
	if err != nil {
		t.Fatalf("reading the fixture binary: %v", err)
	}
	app = fmt.Sprintf("winuseclose%d", os.Getpid())
	exe = filepath.Join(t.TempDir(), app+".exe")
	if err := os.WriteFile(exe, data, 0o755); err != nil {
		t.Fatalf("copying the fixture: %v", err)
	}
	return exe, app
}

// startNamedFixture starts one instance of a fixture executable and waits
// for its main Window. Cleanup kills it if it is still running.
func startNamedFixture(t *testing.T, exe string, flags ...string) (*os.Process, uint64) {
	t.Helper()
	statePath := filepath.Join(t.TempDir(), "state.json")
	cmd := exec.Command(exe, append([]string{"-state", statePath}, flags...)...)
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting %s: %v", filepath.Base(exe), err)
	}
	proc := cmd.Process
	t.Cleanup(func() {
		_ = proc.Kill()
		_, _ = proc.Wait()
	})
	st := waitStateFile(t, statePath, 15*time.Second, func(s *fixture.State) bool { return s.WindowID != 0 })
	return proc, st.WindowID
}

func runCloseApp(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	return runWinuse(t, append([]string{"close-app"}, args...), "WINUSE_HELPER_IDLE="+e2eIdle)
}

func TestCloseAppClosesTwoWindowsWithOneCommand(t *testing.T) {
	optIn(t)
	exe, app := closeAppFixture(t)
	one, oneID := startNamedFixture(t, exe)
	two, twoID := startNamedFixture(t, exe)
	other, otherID := startNamedFixture(t, fixtureBinary(t))

	stdout, stderr, exit := runCloseApp(t, app)
	if exit != 0 || stdout != "ok\n" || stderr != "" {
		t.Fatalf("close-app %s: exit %d, stdout %q, stderr %q; want ok", app, exit, stdout, stderr)
	}
	for _, w := range []uint64{oneID, twoID} {
		if win.WindowOpen(w) {
			t.Errorf("close-app printed ok but Window %d is still open", w)
		}
	}
	for _, p := range []*os.Process{one, two} {
		if !waitProcessExit(t, uint32(p.Pid), time.Second) {
			t.Errorf("close-app printed ok but process %d is still running", p.Pid)
		}
	}
	// Another App's Window stays.
	if !win.WindowOpen(otherID) || waitProcessExit(t, uint32(other.Pid), 0) {
		t.Error("close-app closed another App's Window")
	}
}

func TestCloseAppForceEndsAnAppThatStaysOpen(t *testing.T) {
	optIn(t)
	exe, app := closeAppFixture(t)
	proc, id := startNamedFixture(t, exe, "-ignore-close")

	// Without --force: the close is requested and the App keeps running.
	stdout, stderr, exit := runCloseApp(t, app)
	if exit != 0 || stderr != "" || !strings.HasPrefix(stdout, "ok (the close was requested; "+app+" is still running") {
		t.Fatalf("close-app %s: exit %d, stdout %q, stderr %q; want the still-running ok", app, exit, stdout, stderr)
	}
	if !win.WindowOpen(id) || waitProcessExit(t, uint32(proc.Pid), 0) {
		t.Fatal("the fixture ignores WM_CLOSE, yet it closed without --force")
	}

	// --force: the process is ended.
	stdout, stderr, exit = runCloseApp(t, app, "--force")
	want := "ok (" + app + " was still running after the close; --force ended 1 process)\n"
	if exit != 0 || stdout != want || stderr != "" {
		t.Fatalf("close-app %s --force: exit %d, stdout %q, stderr %q; want %q", app, exit, stdout, stderr, want)
	}
	if !waitProcessExit(t, uint32(proc.Pid), time.Second) {
		t.Error("close-app --force printed ok but the process is still running")
	}
}
