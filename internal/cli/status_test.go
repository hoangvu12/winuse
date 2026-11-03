package cli

// Offline tests for winuse status (desktop parity ticket 16) over a
// scripted dial: with no helper it says "not running", reads the machine
// lines itself and never spawns one; with a helper it prints the helper's
// report; a stale or failing helper is named, never retried or restarted.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/status"
	"github.com/hoangvu12/winuse/internal/version"
	"github.com/hoangvu12/winuse/internal/win"
)

func fakeProbes() localProbes {
	return localProbes{
		Locked:       func() (bool, string) { return false, "Default" },
		Integrity:    func() (uint32, error) { return win.IntegrityMedium, nil },
		DPIAwareness: func() string { return "per-monitor v2" },
		WindowsBuild: func() string { return "Windows 11 25H2, build 26200.6899" },
	}
}

// noSpawnClient fails the test if anything tries to start a helper.
func noSpawnClient(t *testing.T, script *dialScript) *Client {
	return &Client{
		Dial:   script.dial,
		Spawn:  func() error { t.Error("status started a helper"); return errors.New("no spawning") },
		Verify: func(net.Conn) error { return nil },
	}
}

func runStatusOut(t *testing.T, script *dialScript, probes localProbes) string {
	t.Helper()
	var out bytes.Buffer
	if code := runStatus(&out, noSpawnClient(t, script), probes); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if script.remaining() != 0 {
		t.Errorf("dial steps left: %d", script.remaining())
	}
	return out.String()
}

func TestStatusWithNoHelperSaysNotRunningAndStartsNothing(t *testing.T) {
	script := &dialScript{steps: []dialStep{{err: notFoundErr()}}}
	got := runStatusOut(t, script, fakeProbes())
	want := "winuse: " + version.Version + "\n" +
		"helper: not running; the next command starts it\n" +
		"desktop: unlocked\n" +
		"integrity: medium (this CLI's; a helper it starts runs at the same level)\n" +
		"dpi awareness: per-monitor v2 (this CLI's)\n" +
		"browser connections: none; only a running helper holds them\n" +
		"agents: none; only a running helper sees them\n" +
		"windows: Windows 11 25H2, build 26200.6899\n"
	if got != want {
		t.Errorf("status =\n%s\nwant\n%s", got, want)
	}
}

func TestStatusWithNoHelperReportsALockedDesktop(t *testing.T) {
	probes := fakeProbes()
	probes.Locked = func() (bool, string) { return true, "Winlogon" }
	got := runStatusOut(t, &dialScript{steps: []dialStep{{err: notFoundErr()}}}, probes)
	if !strings.Contains(got, "desktop: locked (input desktop is Winlogon); unlock before using winuse\n") {
		t.Errorf("status = %q, want the locked desktop", got)
	}
}

func TestStatusPrintsTheRunningHelpersReport(t *testing.T) {
	report := status.Report{
		Helper:        status.Helper{State: status.HelperRunning, Version: version.Version, PID: 4242, Uptime: 90 * time.Second},
		Integrity:     "medium",
		DPI:           "per-monitor v2",
		Source:        status.SourceHelper,
		BrowsersKnown: true,
		Windows:       "Windows 11 25H2, build 26200.6899",
	}
	raw, _ := json.Marshal(report)
	conn := respondingConn(&protocol.Response{OK: true, Out: string(raw)})
	probes := localProbes{} // must not be called: the helper answered
	got := runStatusOut(t, &dialScript{steps: []dialStep{{conn: conn}}}, probes)
	want := "winuse: " + version.Version + "\n" +
		"helper: running, " + version.Version + ", pid 4242, up 1m30s\n" +
		"desktop: unlocked\n" +
		"integrity: medium (the helper's)\n" +
		"dpi awareness: per-monitor v2 (the helper's)\n" +
		"browser connections: none open; winuse browser status connects one\n" +
		"agents: none since the helper started\n" +
		"windows: Windows 11 25H2, build 26200.6899\n"
	if got != want {
		t.Errorf("status =\n%s\nwant\n%s", got, want)
	}
	var req protocol.Request
	if err := json.Unmarshal(bytes.TrimSpace(conn.written.Bytes()), &req); err != nil {
		t.Fatalf("request line %q: %v", conn.written.String(), err)
	}
	if req.Cmd != "status" || req.Ver != version.Version {
		t.Errorf("request = %+v, want status from this build", req)
	}
}

func TestStatusNamesAHelperOfAnotherBuild(t *testing.T) {
	report := status.Report{Helper: status.Helper{State: status.HelperRunning, Version: "0.0.9", Uptime: time.Hour}, Source: status.SourceHelper}
	raw, _ := json.Marshal(report)
	got := runStatusOut(t, &dialScript{steps: []dialStep{{conn: respondingConn(&protocol.Response{OK: true, Out: string(raw)})}}}, localProbes{})
	if !strings.Contains(got, "helper: running, 0.0.9 (a different build; the next command restarts it), up 1h0m0s\n") {
		t.Errorf("status = %q", got)
	}
}

func TestStatusFromAStaleHelperFallsBackToLocalProbes(t *testing.T) {
	// A helper older than status's stale exemption answers stale: status
	// says so, does not retry or spawn, and reads the rest itself.
	stale := respondingConn(&protocol.Response{Stale: true, Err: "helper is from a different winuse build", Exit: 1})
	got := runStatusOut(t, &dialScript{steps: []dialStep{{conn: stale}}}, fakeProbes())
	if !strings.Contains(got, "helper: a different winuse build was running and is exiting; the next command starts a current one\n") {
		t.Errorf("status = %q", got)
	}
	if !strings.Contains(got, "integrity: medium (this CLI's") {
		t.Errorf("status = %q, want the CLI's own probes", got)
	}
}

func TestStatusNamesAHelperThatDoesNotAnswer(t *testing.T) {
	cases := map[string]dialStep{
		"dropped":   {conn: droppingConn()},
		"dial":      {err: errors.New("access denied")},
		"refused":   {conn: respondingConn(&protocol.Response{Err: `unknown command "status"`, Exit: 1})},
		"bad reply": {conn: respondingConn(&protocol.Response{OK: true, Out: "not json"})},
	}
	for name, step := range cases {
		t.Run(name, func(t *testing.T) {
			got := runStatusOut(t, &dialScript{steps: []dialStep{step}}, fakeProbes())
			if !strings.Contains(got, "helper: running but not answering: ") {
				t.Errorf("status = %q", got)
			}
			if !strings.Contains(got, "windows: Windows 11 25H2, build 26200.6899\n") {
				t.Errorf("status = %q, want the CLI's own probes", got)
			}
		})
	}
}

func TestStatusUsageError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stderr")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	oldStderr, oldStdout := os.Stderr, os.Stdout
	os.Stderr, os.Stdout = f, f
	code := Main([]string{"status", "extra"})
	os.Stderr, os.Stdout = oldStderr, oldStdout
	f.Close()
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "status takes no arguments") {
		t.Errorf("stderr = %q", got)
	}
}

func TestCallExistingNeverSpawns(t *testing.T) {
	script := &dialScript{steps: []dialStep{{err: notFoundErr()}}}
	_, err := noSpawnClient(t, script).CallExisting(context.Background(), testRequest())
	if !errors.Is(err, ErrNoHelper) {
		t.Fatalf("err = %v, want ErrNoHelper", err)
	}
}

func TestUsageListsStatus(t *testing.T) {
	if !strings.Contains(Usage, "\n  status ") {
		t.Error("usage text does not list status")
	}
}

func TestStatusNamesBothBuildsOfOneVersion(t *testing.T) {
	// desktop-parity 25: a helper of this version from another build is
	// named with both builds; the next command restarts it.
	report := status.Report{Helper: status.Helper{State: status.HelperRunning, Version: version.Version, Build: "oldbuild", Uptime: time.Hour}, Source: status.SourceHelper}
	raw, _ := json.Marshal(report)
	conn := respondingConn(&protocol.Response{OK: true, Out: string(raw)})
	got := runStatusOut(t, &dialScript{steps: []dialStep{{conn: conn}}}, localProbes{})
	if !strings.HasPrefix(got, "winuse: "+version.Version+" build "+version.Build()+"\n") ||
		!strings.Contains(got, "helper: running, "+version.Version+" build oldbuild (a different build; the next command restarts it), up 1h0m0s\n") {
		t.Errorf("status = %q", got)
	}
	var req protocol.Request
	if err := json.Unmarshal(bytes.TrimSpace(conn.written.Bytes()), &req); err != nil {
		t.Fatal(err)
	}
	if req.Build == "" || req.Build != version.Build() {
		t.Errorf("request build = %q, want %q", req.Build, version.Build())
	}
}
