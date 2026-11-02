package browser

// The connection manager over cdptest servers: one connection reused
// across commands, a new one after the browser restarts, one approval
// prompt at a time, and the attach sequence.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
)

// launchedChrome is a fake machine with a winuse-launched Chrome served by
// a cdptest server.
func launchedChrome(t *testing.T) (*fakeMachine, *cdptest.Server, *Manager) {
	t.Helper()
	f := newFake(t)
	f.install("chrome")
	f.process("chrome", 100)
	s := f.serve(100)
	f.ownProfile("chrome", ModeLaunch, s.Port())
	m := NewManager(f.sys)
	t.Cleanup(func() { m.Close() })
	return f, s, m
}

func TestManagerReusesTheConnectionAcrossCommands(t *testing.T) {
	f, s, m := launchedChrome(t)
	ctx := context.Background()
	first, err := m.Connection(ctx, "")
	if err != nil {
		t.Fatalf("Connection: %v", err)
	}
	for i := 0; i < 3; i++ {
		c, err := m.Connection(ctx, "chrome")
		if err != nil {
			t.Fatalf("Connection %d: %v", i, err)
		}
		if c != first {
			t.Fatalf("call %d got a new connection", i)
		}
		if _, err := m.Status(ctx, ""); err != nil {
			t.Fatalf("Status: %v", err)
		}
	}
	if n := s.Upgrades(); n != 1 {
		t.Errorf("handshakes = %d, want 1 for the helper's lifetime", n)
	}
	if n := f.dialCount(); n != 1 {
		t.Errorf("dials = %d, want 1", n)
	}
	if first.Mode != ModeLaunch || first.PID != 100 {
		t.Errorf("connection = %+v", first)
	}
}

func TestManagerRedialsAfterTheBrowserRestarts(t *testing.T) {
	_, s, m := launchedChrome(t)
	ctx := context.Background()
	first, err := m.Connection(ctx, "chrome")
	if err != nil {
		t.Fatalf("Connection: %v", err)
	}
	s.DropAll()
	select {
	case <-first.Client.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the dropped connection never ended")
	}
	second, err := m.Connection(ctx, "chrome")
	if err != nil {
		t.Fatalf("Connection after the restart: %v", err)
	}
	if second == first || !second.Client.Alive() {
		t.Fatal("the manager kept the dead connection")
	}
	if n := s.Upgrades(); n != 2 {
		t.Errorf("handshakes = %d, want 2", n)
	}
	if _, err := second.Client.Call(ctx, "Browser.getVersion", nil); err != nil {
		t.Errorf("the new connection does not work: %v", err)
	}
}

func TestManagerHoldsOneApprovalPromptAtATime(t *testing.T) {
	f := newFake(t)
	f.install("chrome")
	f.process("chrome", 100)
	s := f.serve(100)
	f.listen(s.Port(), "", 100) // the inspect toggle: no /json/version
	f.toggle("chrome", s.Port(), "/devtools/browser/fake")
	release := s.Hold() // Chrome holds the handshake while its prompt is up
	m := NewManager(f.sys)
	m.ApprovalWait = 100 * time.Millisecond
	defer m.Close()
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = m.Connection(ctx, "chrome")
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		p := problemOf(t, err, AwaitingApproval)
		if i == 0 && p.Sentence() != "Chrome is waiting for remote-debugging approval; approve it in Chrome and retry." {
			t.Errorf("sentence = %q", p.Sentence())
		}
	}
	if _, err := m.Status(ctx, "chrome"); err == nil {
		t.Fatal("status succeeded while the prompt is up")
	}
	if n := s.Upgrades(); n != 1 {
		t.Fatalf("handshakes = %d while the prompt is up, want 1 (never a second prompt)", n)
	}

	release() // the user clicks Allow
	var c *Connection
	deadline := time.Now().Add(2 * time.Second)
	for {
		var err error
		c, err = m.Connection(ctx, "chrome")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no connection after approval: %v", err)
		}
	}
	if c.Mode != ModeToggle {
		t.Errorf("mode = %s, want toggle", c.Mode)
	}
	if n := s.Upgrades(); n != 1 {
		t.Errorf("handshakes = %d, want the one approved dial", n)
	}
}

func TestManagerReportsARefusedHandshake(t *testing.T) {
	_, s, m := launchedChrome(t)
	s.Reject = "403 Forbidden"
	_, err := m.Connection(context.Background(), "chrome")
	p := problemOf(t, err, Refused)
	if p.Detail != "403 Forbidden" {
		t.Errorf("detail = %q", p.Detail)
	}
	s.Reject = ""
	if _, err := m.Connection(context.Background(), "chrome"); err != nil {
		t.Errorf("a refused dial blocks the next one: %v", err)
	}
}

func TestAttachPageEmulatesFocusAndResumes(t *testing.T) {
	_, s, m := launchedChrome(t)
	ctx := context.Background()
	c, err := m.Connection(ctx, "chrome")
	if err != nil {
		t.Fatal(err)
	}
	sid, err := c.AttachPage(ctx, "T1")
	if err != nil {
		t.Fatalf("AttachPage: %v", err)
	}
	if sid != "session-T1" {
		t.Errorf("session = %q", sid)
	}
	got := s.Received()
	want := []struct{ method, session, params string }{
		{"Target.attachToTarget", "", `{"flatten":true,"targetId":"T1"}`},
		{"Emulation.setFocusEmulationEnabled", "session-T1", `{"enabled":true}`},
		{"Runtime.runIfWaitingForDebugger", "session-T1", `{}`},
	}
	if len(got) != len(want) {
		t.Fatalf("received %v, want %d messages", s.Methods(), len(want))
	}
	for i, w := range want {
		if got[i].Method != w.method || got[i].SessionID != w.session || string(got[i].Params) != w.params {
			t.Errorf("message %d = %s on %q %s, want %s on %q %s", i, got[i].Method, got[i].SessionID, got[i].Params, w.method, w.session, w.params)
		}
	}
}

func TestPagesLeavesOutInternalTargets(t *testing.T) {
	_, s, m := launchedChrome(t)
	s.Targets = []cdptest.Target{
		{TargetID: "A", Type: "page", URL: "https://example.com"},
		{TargetID: "B", Type: "page", URL: "chrome://newtab/"},
		{TargetID: "C", Type: "service_worker", URL: "https://example.com/sw.js"},
		{TargetID: "D", Type: "page", URL: "edge://settings"},
	}
	ctx := context.Background()
	c, err := m.Connection(ctx, "chrome")
	if err != nil {
		t.Fatal(err)
	}
	pages, err := c.Pages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 || pages[0].TargetID != "A" {
		b, _ := json.Marshal(pages)
		t.Errorf("pages = %s, want only A", b)
	}
	out, err := m.Status(ctx, "chrome")
	if err != nil || out != "ok chrome connected: winuse's own profile, 1 tab\n" {
		t.Errorf("status = %q, %v", out, err)
	}
}

func TestManagerCloseEndsConnections(t *testing.T) {
	_, _, m := launchedChrome(t)
	c, err := m.Connection(context.Background(), "chrome")
	if err != nil {
		t.Fatal(err)
	}
	m.Close()
	if c.Client.Alive() {
		t.Error("connection alive after Close")
	}
	if _, err := m.Connection(context.Background(), "chrome"); err == nil {
		t.Error("a closed manager dialed again")
	}
}
