//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
)

// This file is the fixture side of the runner: it builds and starts the
// fixture app, waits for its state file, and shuts it down. The fixture's
// Element contract (names, AutomationIds, roles, patterns) lives in
// internal/fixture/elements.go — later tickets' e2e tests import that
// package instead of restating values here.

var (
	fixtureOnce sync.Once
	fixturePath string
	fixtureErr  error
)

// fixtureBinary builds the fixture app once per test run. It is linked as
// a GUI-subsystem binary (no console) like a real desktop app.
func fixtureBinary(t *testing.T) string {
	t.Helper()
	fixtureOnce.Do(func() {
		dir, err := os.MkdirTemp("", "winuse-e2e-")
		if err != nil {
			fixtureErr = err
			return
		}
		fixturePath = filepath.Join(dir, "winusefixture.exe")
		cmd := exec.Command("go", "build", "-ldflags", "-H windowsgui", "-o", fixturePath, "../../cmd/winusefixture")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		out, err := cmd.CombinedOutput()
		if err != nil {
			fixtureErr = fmt.Errorf("go build: %v\n%s", err, out)
		}
	})
	if fixtureErr != nil {
		t.Fatalf("building the fixture app: %v", fixtureErr)
	}
	return fixturePath
}

// fixtureProc is a running fixture app.
type fixtureProc struct {
	proc  *os.Process
	state string
}

// startFixture launches the fixture app with its state file in t's temp
// dir and registers cleanup that kills it. The app takes over a piece of
// the desktop: this function must only be called from opt-in tests.
func startFixture(t *testing.T) *fixtureProc {
	t.Helper()
	statePath := filepath.Join(t.TempDir(), "state.json")
	cmd := exec.Command(fixtureBinary(t), "-state", statePath)
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the fixture app: %v", err)
	}
	proc := cmd.Process
	fp := &fixtureProc{proc: proc, state: statePath}
	t.Cleanup(func() {
		_ = proc.Kill()
		_, _ = proc.Wait()
	})
	return fp
}

// waitState polls the fixture's state file until pred holds or the timeout
// passes. It fails the test on timeout.
func (fp *fixtureProc) waitState(t *testing.T, timeout time.Duration, pred func(*fixture.State) bool) *fixture.State {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last fixture.State
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(fp.state)
		if err == nil && json.Unmarshal(data, &last) == nil {
			if pred(&last) {
				return &last
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("fixture state did not satisfy the condition within %v; last state: %+v", timeout, last)
	return nil
}

// windowLine finds a line of `winuse list-windows` output by its Window ID
// and returns its three tab-separated fields.
func windowLine(stdout string, windowID uint64) (id, app, title string, ok bool) {
	for _, line := range strings.Split(strings.TrimSuffix(stdout, "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		if parts[0] == itoa64(windowID) {
			return parts[0], parts[1], parts[2], true
		}
	}
	return "", "", "", false
}

func itoa64(v uint64) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

// TestListWindowsShowsFixtureWindow is the first seam-1 end-to-end test
// (ticket 02): the real `winuse list-windows` output must contain the
// fixture app's Window, identified by the exact Window ID and title the
// fixture reports in its state file. While the fixture's "Save As" dialog
// is closed, only the main window may be listed.
func TestListWindowsShowsFixtureWindow(t *testing.T) {
	optIn(t)
	fp := startFixture(t)
	st := fp.waitState(t, 15*time.Second, func(s *fixture.State) bool {
		return s.WindowID != 0
	})

	stdout, stderr, exit := runListWindows(t, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("exit %d, stderr:\n%s", exit, stderr)
	}
	if stderr != "" {
		t.Errorf("unexpected stderr: %q", stderr)
	}

	id, app, title, ok := windowLine(stdout, st.WindowID)
	if !ok {
		t.Fatalf("the fixture's Window %d is not listed:\n%s", st.WindowID, stdout)
	}
	if title != fixture.WindowTitle {
		t.Errorf("the fixture Window's title = %q, want %q", title, fixture.WindowTitle)
	}
	if app != "winusefixture" {
		t.Errorf("the fixture Window's App = %q, want %q (the exe base name; the fixture ships no FileDescription)", app, "winusefixture")
	}
	_ = id

	// The dialog starts hidden: it must not be listed as its own Window.
	if st.DialogOpen {
		t.Fatal("the fixture started with its dialog open; the fixture contract changed")
	}
	if st.DialogWindowID != 0 {
		if _, _, _, ok := windowLine(stdout, st.DialogWindowID); ok {
			t.Errorf("the hidden dialog Window %d is listed; hidden windows must be excluded:\n%s", st.DialogWindowID, stdout)
		}
	}
}
