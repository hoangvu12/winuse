//go:build e2e

// Package e2e holds winuse's seam-1 end-to-end tests: the real winuse
// binary against the live desktop.
//
// THESE TESTS OBSERVE THE REAL DESKTOP (window titles and process paths).
// They are double-gated so they can never run as part of the default test
// command: they compile only with the `e2e` build tag, and they skip unless
// WINUSE_E2E=1 is set in the environment. Only the user starts them:
//
//	set WINUSE_E2E=1
//	go test -tags e2e -v ./test/e2e
//
// Agents must never enable the opt-in, never set the environment variable,
// and never run these tests without the user's explicit go-ahead.
package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/pipe"
	"github.com/hoangvu12/winuse/internal/win"
)

// e2eIdle is the helper idle timeout used by these tests so nothing lingers
// on the user's machine after the suite ends.
const e2eIdle = "5s"

// idleForRestart is short enough to observe an idle exit within a test.
const idleForRestart = "2s"

var (
	binaryOnce sync.Once
	binaryPath string
	binaryErr  error
)

func optIn(t *testing.T) {
	t.Helper()
	if os.Getenv("WINUSE_E2E") != "1" {
		t.Skip("end-to-end tests are opt-in only: the user must set WINUSE_E2E=1 (see test/e2e docs; agents must not enable this)")
	}
}

// winuseBinary builds the winuse binary once per test run.
func winuseBinary(t *testing.T) string {
	t.Helper()
	binaryOnce.Do(func() {
		dir, err := os.MkdirTemp("", "winuse-e2e-")
		if err != nil {
			binaryErr = err
			return
		}
		binaryPath = filepath.Join(dir, "winuse.exe")
		cmd := exec.Command("go", "build", "-o", binaryPath, "../../cmd/winuse")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		out, err := cmd.CombinedOutput()
		if err != nil {
			binaryErr = fmt.Errorf("go build: %v\n%s", err, out)
		}
	})
	if binaryErr != nil {
		t.Fatalf("building winuse: %v", binaryErr)
	}
	return binaryPath
}

// runWinuse runs the binary with the given extra environment.
func runWinuse(t *testing.T, extraEnv ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(winuseBinary(t), "list-windows")
	cmd.Env = append(os.Environ(), extraEnv...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exit := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exit = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("running winuse: %v", err)
	}
	return stdout.String(), stderr.String(), exit
}

// helperPID dials the running helper and returns its process id. Requires a
// helper to be listening.
func helperPID(t *testing.T) uint32 {
	t.Helper()
	id, err := win.CurrentIdentity()
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := pipe.Dial(ctx, id)
	if err != nil {
		t.Fatalf("dialing helper: %v", err)
	}
	defer conn.Close()
	pid, err := pipe.ServerPID(conn)
	if err != nil {
		t.Fatalf("helper pid: %v", err)
	}
	return pid
}

// TestListWindowsOutput checks the output contract: one line per visible
// Window, "<Window ID>\t<App>\t<title>", exit code 0.
func TestListWindowsOutput(t *testing.T) {
	optIn(t)
	stdout, stderr, exit := runWinuse(t, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("exit %d, stderr:\n%s", exit, stderr)
	}
	if stderr != "" {
		t.Errorf("unexpected stderr: %q", stderr)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatalf("no windows listed; the desktop should have at least one visible window:\n%q", stdout)
	}
	line := regexp.MustCompile(`^[0-9]+\t[^\t]*\t.*$`)
	for i, l := range lines {
		if !line.MatchString(l) {
			t.Errorf("line %d does not match \"<Window ID>\\t<App>\\t<title>\": %q", i+1, l)
		}
	}
}

// TestHelperIsReusedAcrossInvocations checks that two quick commands share
// one helper process instead of spawning two.
func TestHelperIsReusedAcrossInvocations(t *testing.T) {
	optIn(t)
	if _, _, exit := runWinuse(t, "WINUSE_HELPER_IDLE="+e2eIdle); exit != 0 {
		t.Fatalf("first invocation failed (exit %d)", exit)
	}
	pid1 := helperPID(t)
	if _, _, exit := runWinuse(t, "WINUSE_HELPER_IDLE="+e2eIdle); exit != 0 {
		t.Fatalf("second invocation failed (exit %d)", exit)
	}
	pid2 := helperPID(t)
	if pid1 != pid2 {
		t.Errorf("helper was restarted between invocations: %d then %d", pid1, pid2)
	}
}

// TestHelperIdleExitAndTransparentRestart checks that the helper exits
// after the idle period and the next command restarts it without the user
// noticing.
func TestHelperIdleExitAndTransparentRestart(t *testing.T) {
	optIn(t)
	if _, _, exit := runWinuse(t, "WINUSE_HELPER_IDLE="+idleForRestart); exit != 0 {
		t.Fatalf("first invocation failed (exit %d)", exit)
	}
	oldPID := helperPID(t)

	// Wait past the idle period; the helper must be gone (no pipe).
	time.Sleep(3500 * time.Millisecond)
	id, err := win.CurrentIdentity()
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if conn, err := pipe.Dial(ctx, id); err == nil {
		conn.Close()
		t.Fatalf("helper %d is still listening after the idle period", oldPID)
	}

	// The next command must restart it transparently.
	stdout, stderr, exit := runWinuse(t, "WINUSE_HELPER_IDLE="+idleForRestart)
	if exit != 0 {
		t.Fatalf("restart invocation failed (exit %d), stderr:\n%s", exit, stderr)
	}
	if stdout == "" {
		t.Fatal("restart invocation listed no windows")
	}
	newPID := helperPID(t)
	if newPID == oldPID {
		t.Errorf("expected a fresh helper process, but pid %d survived the idle period", oldPID)
	}
}

// TestHelperHasNoConsoleWindow is a light check that the spawned helper is
// detached: it never shows up in its own listing (own-process exclusion)
// and does not hold the invoking terminal's console.
func TestHelperHasNoConsoleWindow(t *testing.T) {
	optIn(t)
	stdout, _, exit := runWinuse(t, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("exit %d", exit)
	}
	for _, l := range strings.Split(strings.TrimSuffix(stdout, "\n"), "\n") {
		if l == "" {
			continue
		}
		parts := strings.SplitN(l, "\t", 3)
		if len(parts) == 3 && strings.EqualFold(parts[1], "winuse") {
			t.Errorf("a winuse window appears in the listing: %q", l)
		}
	}
}
