package latency

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"

	"github.com/hoangvu12/winuse/internal/fixture"
	"github.com/hoangvu12/winuse/internal/pipe"
	"github.com/hoangvu12/winuse/internal/win"
)

// Agent is the WINUSE_AGENT every timed call runs as, so its Window States
// stay apart from any other Agent's in the helper (ADR 0014).
const Agent = "latency-ab"

// helperIdle keeps a build's helper alive through its block (a huge read
// can take seconds); the run stops the last one itself.
const helperIdle = "60s"

// callTimeout bounds one call: a read has 10 s, and a stale helper's exit
// and a fresh one's start come on top.
const callTimeout = 60 * time.Second

// Call runs one winuse call of arm (a Caller): the wall time from start to
// exit, the exit code and the first stderr line on a failure.
func Call(ctx context.Context, arm *Arm, args []string) (time.Duration, int, string, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	cmd := win.NoConsoleWindow(exec.CommandContext(ctx, arm.Exe, args...))
	cmd.Env = append(os.Environ(), "WINUSE_AGENT="+Agent, "WINUSE_HELPER_IDLE="+helperIdle)
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	start := time.Now()
	err := cmd.Run()
	took := time.Since(start)
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return took, 0, "", nil
	case errors.As(err, &exitErr):
		line, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n")
		if ctx.Err() != nil {
			line = "timed out after " + callTimeout.String()
		}
		return took, exitErr.ExitCode(), strings.TrimSpace(line), nil
	default:
		return took, -1, "", fmt.Errorf("running %s: %w", arm.Exe, err)
	}
}

// Fixture is the running fixture app.
type Fixture struct {
	proc     *os.Process
	Main     uint64 // its main Window's ID
	HugeList uint64 // its huge-list Window's ID, 0 when not started
}

// StartFixture starts the fixture app with its state file in dir, and the
// huge-list Window when hugeList is above 0, and waits for their Window IDs.
func StartFixture(exe, dir string, hugeList int) (*Fixture, error) {
	state := filepath.Join(dir, "fixture-state.json")
	args := []string{"-state", state}
	if hugeList > 0 {
		args = append(args, "-huge-list", fmt.Sprint(hugeList))
	}
	cmd := exec.Command(exe, args...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting the fixture: %w", err)
	}
	f := &Fixture{proc: cmd.Process}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		var st fixture.State
		if data, err := os.ReadFile(state); err == nil && json.Unmarshal(data, &st) == nil &&
			st.WindowID != 0 && (hugeList <= 0 || st.HugeListWindowID != 0) {
			f.Main, f.HugeList = st.WindowID, st.HugeListWindowID
			return f, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	f.Close()
	return nil, errors.New("the fixture did not report its Windows within 60 s")
}

// Close ends the fixture process.
func (f *Fixture) Close() {
	if f != nil && f.proc != nil {
		_ = f.proc.Kill()
		_, _ = f.proc.Wait()
		f.proc = nil
	}
}

// RunningHelper names the winuse helper serving this session now, if any:
// its process id and executable path.
func RunningHelper() (pid uint32, path string, ok bool) {
	id, err := win.CurrentIdentity()
	if err != nil {
		return 0, "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := pipe.Dial(ctx, id)
	if err != nil {
		return 0, "", false
	}
	defer conn.Close()
	pid, err = pipe.ServerPID(conn)
	if err != nil {
		return 0, "", false
	}
	return pid, processPath(pid), true
}

// StopHelperUnder ends the running helper when its executable lies under
// dir (one of the run's own builds), and waits for its pipe to close. A
// helper of any other winuse is left alone. It reports whether it stopped
// one.
func StopHelperUnder(dir string) (bool, error) {
	pid, path, ok := RunningHelper()
	if !ok || !under(longPath(path), longPath(dir)) {
		return false, nil
	}
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return false, fmt.Errorf("opening the helper (pid %d): %w", pid, err)
	}
	defer windows.CloseHandle(h)
	if err := windows.TerminateProcess(h, 1); err != nil {
		return false, fmt.Errorf("ending the helper (pid %d): %w", pid, err)
	}
	windows.WaitForSingleObject(h, 5000)
	return true, nil
}

// under reports whether path lies inside dir (case-insensitive, as Windows
// paths are).
func under(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	rel, err := filepath.Rel(strings.ToLower(filepath.Clean(dir)), strings.ToLower(filepath.Clean(path)))
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

// longPath expands an existing path's 8.3 short names (a temp folder can
// come back as C:\Users\HPMEDI~1\...), so it compares with a process's
// image path; the path as given when it cannot.
func longPath(p string) string {
	if p == "" {
		return p
	}
	in, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return p
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetLongPathName(in, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || n > uint32(len(buf)) {
		return p
	}
	return windows.UTF16ToString(buf[:n])
}

// processPath is a process's executable path, "" when it cannot be read.
func processPath(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}
