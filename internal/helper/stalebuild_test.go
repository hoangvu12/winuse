package helper

// A helper from another build of the same version (desktop-parity ticket
// 25): the request's Build decides it beside Ver, an empty build matches,
// status still answers, and the stale exit waits for the requests in
// flight.

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/status"
	"github.com/hoangvu12/winuse/internal/version"
)

func buildRequest(cmd, build string) *protocol.Request {
	r := testRequest(cmd)
	r.Build = build
	return r
}

func TestServeSameVersionOtherBuildExits(t *testing.T) {
	s := New("0.1.0", time.Hour, protocol.RequestTimeout, map[string]Command{"x": echoCommand("")}, discardLogger())
	s.build = "helperbuild"
	ln := newFakeListener()
	defer ln.Close()
	done := make(chan error, 1)
	go func() { done <- s.Serve(ln) }()

	client := ln.push()
	client.SetDeadline(time.Now().Add(5 * time.Second))
	resp, err := call(client, buildRequest("x", "clibuild"))
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if resp.OK || !resp.Stale {
		t.Fatalf("want stale response, got %+v", resp)
	}
	if !strings.Contains(resp.Err, "helper 0.1.0 build helperbuild, CLI 0.1.0 build clibuild") {
		t.Errorf("Err = %q, want both builds named", resp.Err)
	}
	if resp.Code != protocol.CodeInternal {
		t.Errorf("Code = %q, want %q", resp.Code, protocol.CodeInternal)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v, want nil (clean exit)", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("helper did not exit after a request from another build")
	}
}

func TestServeEmptyBuildsMatch(t *testing.T) {
	// A CLI or helper from before builds were compared sends or has no
	// build: it matches any, so mixed builds across the change behave as
	// before. The same build matches too.
	cases := []struct{ helper, cli string }{
		{"helperbuild", ""},
		{"", "clibuild"},
		{"", ""},
		{"same", "same"},
	}
	for _, tc := range cases {
		s := New("0.1.0", time.Hour, protocol.RequestTimeout, map[string]Command{"x": echoCommand("ok\n")}, discardLogger())
		s.build = tc.helper
		if resp := s.handle(buildRequest("x", tc.cli)); !resp.OK || resp.Stale {
			t.Errorf("helper build %q, CLI build %q: %+v, want ok", tc.helper, tc.cli, resp)
		}
	}
	// Another version is stale whatever the builds say.
	s := New("0.1.0", time.Hour, protocol.RequestTimeout, map[string]Command{"x": echoCommand("ok\n")}, discardLogger())
	s.build = ""
	req := buildRequest("x", "")
	req.Ver = "9.9.9"
	if resp := s.handle(req); !resp.Stale {
		t.Errorf("another version: %+v, want stale", resp)
	}
}

func TestServeAnswersStatusForAnotherExecutableBuild(t *testing.T) {
	s, ln := startServer(t, time.Hour, map[string]Command{StatusCommand: echoCommand(`{"helper":{"state":"running"}}`)})
	s.build = "helperbuild"
	client := ln.push()
	defer client.Close()
	client.SetDeadline(time.Now().Add(5 * time.Second))
	resp, err := call(client, buildRequest(StatusCommand, "clibuild"))
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

func TestStatusReportsTheHelpersBuild(t *testing.T) {
	out, err := CommandsWith(statusServices(&heldFakeBrowsers{}))[StatusCommand](context.Background(), nil)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	var r status.Report
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	if r.Helper.Build == "" || r.Helper.Build != version.Build() {
		t.Errorf("helper build = %q, want %q", r.Helper.Build, version.Build())
	}
}

func TestStaleExitFinishesTheRequestsInFlight(t *testing.T) {
	// An Action of this helper's own build is running when a CLI of
	// another build calls: the stale answer comes at once, but the helper
	// exits only after the Action finished, released what it held and
	// answered (no request cut mid-Action, no keys left down).
	started, finish := make(chan struct{}), make(chan struct{})
	var released atomic.Bool
	slow := func(ctx context.Context, args json.RawMessage) (string, *Error) {
		close(started)
		<-finish
		released.Store(true) // the Action's give-back (releaseHeld)
		return "done\n", nil
	}
	s := New("0.1.0", time.Hour, protocol.RequestTimeout, map[string]Command{"slow": slow, "x": echoCommand("")}, discardLogger())
	s.build = "helperbuild"
	ln := newFakeListener()
	defer ln.Close()
	done := make(chan error, 1)
	go func() { done <- s.Serve(ln) }()

	own := ln.push()
	own.SetDeadline(time.Now().Add(10 * time.Second))
	answered := make(chan *protocol.Response, 1)
	go func() {
		resp, err := call(own, buildRequest("slow", "helperbuild"))
		if err != nil {
			t.Errorf("the request in flight: %v", err)
		}
		answered <- resp
	}()
	<-started

	other := ln.push()
	other.SetDeadline(time.Now().Add(5 * time.Second))
	resp, err := call(other, buildRequest("x", "clibuild"))
	if err != nil || !resp.Stale {
		t.Fatalf("another build: %+v, %v; want stale", resp, err)
	}
	select {
	case <-done:
		t.Fatal("the helper exited with a request in flight")
	case <-time.After(100 * time.Millisecond):
	}

	close(finish)
	if r := <-answered; r == nil || !r.OK || r.Out != "done\n" {
		t.Fatalf("the request in flight answered %+v, want done", r)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the helper did not exit after its last request")
	}
	if !released.Load() {
		t.Error("the helper exited before the Action's release")
	}
}
