//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/pipe"
	"github.com/hoangvu12/winuse/internal/win"
	"golang.org/x/sys/windows"
)

// This file holds the runner's shared plumbing: the opt-in gate, the winuse
// binary build and the run helpers. The rules that matter are in the
// package comment and test/e2e/README.md: these tests drive the real
// desktop and are started only by the user, never by an agent.

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

// runWinuse runs the binary with the given arguments and extra environment
// and returns (stdout, stderr, exit code).
func runWinuse(t *testing.T, args []string, extraEnv ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(winuseBinary(t), args...)
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

// runListWindows is the common entry for the ticket-01 contract tests.
func runListWindows(t *testing.T, extraEnv ...string) (string, string, int) {
	t.Helper()
	return runWinuse(t, []string{"list-windows"}, extraEnv...)
}

// helperPID dials the running helper and returns its process id. Requires a
// helper to be listening.
func helperPID(t *testing.T) uint32 {
	t.Helper()
	id, err := win.CurrentIdentity()
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	conn, err := dialHelper(id)
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

// dialHelper dials the helper's pipe with a short timeout.
func dialHelper(id win.Identity) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return pipe.Dial(ctx, id)
}

// killHelper terminates the running helper (if any) so the next command
// spawns a fresh one — used when a test needs the helper to inherit this
// process's environment (apps that winuse launches inherit the helper's,
// not the CLI's). Killing the helper is process management only: no input,
// no windows.
func killHelper(t *testing.T) {
	t.Helper()
	id, err := win.CurrentIdentity()
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	conn, err := dialHelper(id)
	if err != nil {
		return // none running
	}
	pid, err := pipe.ServerPID(conn)
	conn.Close()
	if err != nil {
		t.Fatalf("helper pid: %v", err)
	}
	terminatePID(t, pid)
	// The next command respawns only after the old helper's pipe is gone.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := dialHelper(id)
		if err != nil {
			return
		}
		conn.Close()
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the killed helper is still answering")
}

// terminatePID terminates a process (test cleanup for processes the test
// does not own, such as apps winuse launched).
func terminatePID(t *testing.T, pid uint32) {
	t.Helper()
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		t.Fatalf("opening process %d: %v", pid, err)
	}
	defer windows.CloseHandle(h)
	if err := windows.TerminateProcess(h, 1); err != nil {
		t.Fatalf("terminating process %d: %v", pid, err)
	}
}

// killWindowProcess terminates the process owning a Window (cleanup for
// apps winuse launched: the test never owned them). Read-only lookup, no
// input, no activation.
func killWindowProcess(t *testing.T, windowID uint64) {
	t.Helper()
	pid := win.WindowPID(windowID)
	if pid == 0 {
		t.Fatalf("no process found for Window %d", windowID)
	}
	terminatePID(t, pid)
}
