package helper

// Offline tests for the status command (desktop parity ticket 16): the
// report it answers over faked probes, the Browser Connections read
// without any browser call, that it never waits behind an Action, and
// that a CLI of another build may ask it without stopping the helper.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/status"
	"github.com/hoangvu12/winuse/internal/version"
	"github.com/hoangvu12/winuse/internal/win"
)

// heldFakeBrowsers is a fake Browser service that also lists its held
// connections; every other call is recorded, so a test can prove status
// made none.
type heldFakeBrowsers struct {
	fakeBrowsers
	held []string
}

func (f *heldFakeBrowsers) Held() []string { return f.held }

func statusServices(b BrowserService) Services {
	started := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	return Services{
		Trees:        &fakeTrees{},
		Browser:      b,
		Locked:       func() (bool, string) { return false, "Default" },
		Started:      started,
		Now:          func() time.Time { return started.Add(4*time.Minute + 12*time.Second + 300*time.Millisecond) },
		OwnIntegrity: func() (uint32, error) { return win.IntegrityMedium, nil },
		DPIAwareness: func() string { return "per-monitor v2" },
		WindowsBuild: func() string { return "Windows 11 25H2, build 26200.6899" },
	}
}

func runStatusCommand(t *testing.T, s Services) status.Report {
	t.Helper()
	out, err := CommandsWith(s)[StatusCommand](context.Background(), nil)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	var r status.Report
	if jerr := json.Unmarshal([]byte(out), &r); jerr != nil {
		t.Fatalf("status answered %q: %v", out, jerr)
	}
	return r
}

func TestStatusReportsTheHelperAndMachine(t *testing.T) {
	fb := &heldFakeBrowsers{held: []string{"chrome process 1234, your running browser through the winuse extension"}}
	r := runStatusCommand(t, statusServices(fb))
	r.CLIVersion = version.Version

	want := "winuse: " + version.Version + "\n" +
		"helper: running, " + version.Version + ", pid " + strconv.Itoa(os.Getpid()) + ", up 4m12s\n" +
		"desktop: unlocked\n" +
		"integrity: medium (the helper's)\n" +
		"dpi awareness: per-monitor v2 (the helper's)\n" +
		"browser connections: chrome process 1234, your running browser through the winuse extension\n" +
		"agents: none since the helper started\n" +
		"windows: Windows 11 25H2, build 26200.6899\n"
	if got := r.Format(); got != want {
		t.Errorf("report =\n%s\nwant\n%s", got, want)
	}
	if len(fb.calls) != 0 {
		t.Errorf("status called the Browser service: %v (it must only read the held connections)", fb.calls)
	}
}

func TestStatusReportsALockedDesktopAndNoConnections(t *testing.T) {
	s := statusServices(&heldFakeBrowsers{})
	s.Locked = func() (bool, string) { return true, "Winlogon" }
	r := runStatusCommand(t, s)
	if !r.Locked || r.Desktop != "Winlogon" {
		t.Errorf("locked, desktop = %v, %q; want true, Winlogon", r.Locked, r.Desktop)
	}
	if !r.BrowsersKnown || len(r.Browsers) != 0 {
		t.Errorf("browsers = %v known %v; want none, known", r.Browsers, r.BrowsersKnown)
	}
}

func TestStatusLeavesUnreadableProbesUnknown(t *testing.T) {
	// A Browser service that cannot list its held connections is not
	// asked for a status (which would dial): the line says unknown.
	fb := &fakeBrowsers{}
	s := statusServices(fb)
	s.OwnIntegrity = func() (uint32, error) { return 0, errors.New("no token") }
	r := runStatusCommand(t, s)
	if r.BrowsersKnown {
		t.Errorf("browsers known with a service that cannot list them")
	}
	if r.Integrity != "" {
		t.Errorf("integrity = %q, want empty (unknown)", r.Integrity)
	}
	if len(fb.calls) != 0 {
		t.Errorf("status called the Browser service: %v", fb.calls)
	}
}

func TestStatusRejectsArguments(t *testing.T) {
	_, err := CommandsWith(statusServices(&heldFakeBrowsers{}))[StatusCommand](context.Background(), json.RawMessage(`{"window":"1"}`))
	if err == nil {
		t.Fatal("status accepted arguments")
	}
}

// TestStatusNeverWaitsBehindAnAction pins status as observation-only: it
// answers while another command holds the action gate.
func TestStatusNeverWaitsBehindAnAction(t *testing.T) {
	if !observationOnlyCommand(StatusCommand) {
		t.Fatal("status is serialized on the action gate")
	}
	s := statusServices(&heldFakeBrowsers{})
	s.lanes = newLaneManager()
	s.lanes.lane(desktopLane) <- struct{}{} // an Action holds the desktop
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := CommandsWith(s)[StatusCommand](ctx, nil); err != nil {
		t.Fatalf("status behind a held gate: %v", err)
	}
}

// TestServeAnswersStatusForAnotherBuild: status only reads, so a CLI of
// another build gets the report and the helper keeps serving, where any
// other command makes it answer stale and exit.
func TestServeAnswersStatusForAnotherBuild(t *testing.T) {
	s, ln := startServer(t, time.Hour, map[string]Command{StatusCommand: echoCommand(`{"helper":{"state":"running"}}`)})
	client := ln.push()
	defer client.Close()
	client.SetDeadline(time.Now().Add(5 * time.Second))
	resp, err := call(client, &protocol.Request{V: protocol.Version, Ver: "9.9.9", Cmd: StatusCommand})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !resp.OK || resp.Stale {
		t.Fatalf("response = %+v, want ok and not stale", resp)
	}
	if s.closed.Load() {
		t.Error("the helper shut down after a status from another build")
	}
}
