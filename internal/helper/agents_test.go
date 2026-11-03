package helper

// The Agent behind each request (multi-agent ticket 02): the server puts
// the request's Agent on the command's context, a request with none is
// the default Agent, and the helper remembers the Agents it served for
// `winuse status`. No desktop is touched.

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/agent"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/shot"
	"github.com/hoangvu12/winuse/internal/uia"
)

func TestRequestAgent(t *testing.T) {
	cases := []struct {
		req  protocol.Request
		want agent.Identity
	}{
		{protocol.Request{}, agent.Identity{ID: agent.DefaultID, Label: agent.DefaultID}},
		{protocol.Request{Agent: "proc:17048:30", AgentLabel: "claude 17048"}, agent.Identity{ID: "proc:17048:30", Label: "claude 17048"}},
		{protocol.Request{Agent: "name:reviewer"}, agent.Identity{ID: "name:reviewer", Label: "name:reviewer"}},
	}
	for _, c := range cases {
		if got := requestAgent(&c.req); got != c.want {
			t.Errorf("requestAgent(%+v) = %+v, want %+v", c.req, got, c.want)
		}
	}
	if got := AgentOf(context.Background()); got.ID != agent.DefaultID {
		t.Errorf("AgentOf(no Agent) = %+v, want the default Agent", got)
	}
}

func TestServerPutsTheRequestsAgentOnTheContext(t *testing.T) {
	var seen agent.Identity
	cmds := map[string]Command{
		"probe": func(ctx context.Context, args json.RawMessage) (string, *Error) {
			seen = AgentOf(ctx)
			return "ok\n", nil
		},
	}
	srv := New("v1", time.Minute, time.Second, cmds, log.New(io.Discard, "", 0))
	resp := srv.handle(&protocol.Request{V: protocol.Version, Ver: "v1", Cmd: "probe", Agent: "name:builder", AgentLabel: "builder"})
	if !resp.OK {
		t.Fatalf("probe: %+v", resp)
	}
	if seen != (agent.Identity{ID: "name:builder", Label: "builder"}) {
		t.Errorf("the command saw %+v, want the request's Agent", seen)
	}
	srv.handle(&protocol.Request{V: protocol.Version, Ver: "v1", Cmd: "probe"})
	if seen.ID != agent.DefaultID {
		t.Errorf("a request without an Agent ran for %+v, want the default Agent", seen)
	}
}

func TestStatusListsTheAgentsSeen(t *testing.T) {
	s := statusServices(&heldFakeBrowsers{})
	now := s.Now()
	clock := now.Add(-time.Minute)
	s.Now = func() time.Time { return clock }
	s.Agents = newAgentLog()
	noop := func(ctx context.Context, args json.RawMessage) (string, *Error) { return "ok\n", nil }
	claude := withAgent(context.Background(), agent.Identity{ID: "proc:1:2", Label: "claude 1"})
	reviewer := withAgent(context.Background(), agent.Identity{ID: "name:reviewer", Label: "reviewer"})
	s.tracked("get-window-state", noop)(claude, nil)
	clock = now.Add(-10 * time.Second)
	s.tracked("click", noop)(reviewer, nil)
	clock = now.Add(-5 * time.Second)
	s.tracked("click", noop)(claude, nil)

	s.Now = func() time.Time { return now }
	cmds := CommandsWith(s)
	// status itself is no Agent's work: it is not recorded.
	if _, err := cmds[StatusCommand](claude, nil); err != nil {
		t.Fatalf("status: %v", err)
	}
	r := runStatusCommand(t, s)
	if len(r.Agents) != 2 {
		t.Fatalf("agents = %+v, want 2", r.Agents)
	}
	if a := r.Agents[0]; a.Label != "claude 1" || a.Command != "click" || a.Ago != 5*time.Second {
		t.Errorf("first agent = %+v, want claude 1's click 5s ago", a)
	}
	if a := r.Agents[1]; a.Label != "reviewer" || a.Command != "click" || a.Ago != 10*time.Second {
		t.Errorf("second agent = %+v, want reviewer's click 10s ago", a)
	}
}

func TestAgentLogForgetsTheOldestPastTheCap(t *testing.T) {
	l := newAgentLog()
	start := time.Unix(1000, 0)
	for i := range maxAgentsSeen + 1 {
		l.record(agent.Identity{ID: fmt.Sprintf("name:a%d", i), Label: fmt.Sprintf("a%d", i)}, "click", start.Add(time.Duration(i)*time.Second))
	}
	list := l.list(start.Add(time.Hour))
	if len(list) != maxAgentsSeen {
		t.Fatalf("remembered %d Agents, want %d", len(list), maxAgentsSeen)
	}
	for _, a := range list {
		if a.ID == "name:a0" {
			t.Error("the oldest Agent is still remembered")
		}
	}
	if list[0].ID != fmt.Sprintf("name:a%d", maxAgentsSeen) {
		t.Errorf("the most recent Agent is %q, want it first", list[0].ID)
	}
}

func TestAnAgentThatHasNotReadTheWindowIsToldPlainly(t *testing.T) {
	w := fakeWindows()[0]
	e := elementError(&uia.NotReadByAgentError{WindowID: w.ID}, w, 2)
	if !strings.Contains(e.Msg, "has not been read by this agent; another agent read it") {
		t.Errorf("message = %q", e.Msg)
	}
	if !strings.Contains(e.Hint, "winuse get-window-state 101 first") || !strings.Contains(e.Hint, "WINUSE_AGENT") {
		t.Errorf("hint = %q, want the read-first step and the WINUSE_AGENT advice", e.Hint)
	}
	// The same through a name:/id: reference, and still a refusal that
	// says nothing was sent.
	if r := refError(&uia.NotReadByAgentError{WindowID: w.ID}, w); r.Msg != e.Msg {
		t.Errorf("refError = %q, want %q", r.Msg, e.Msg)
	}
	if !refusedBeforeCall(&uia.NotReadByAgentError{WindowID: w.ID}) {
		t.Error("a Window this Agent has not read is a refusal before any call")
	}
	// Nobody read it: the plain message, as before.
	if e := elementError(fmt.Errorf("%w: window 101", uia.ErrNoState), w, 2); !strings.HasPrefix(e.Msg, "no cached Window State is available for window 101") {
		t.Errorf("message = %q", e.Msg)
	}
}

func TestScreenshotPointsArePerAgent(t *testing.T) {
	w := fakeWindows()[0]
	s := Services{Shots: shot.NewStore()}
	s.Shots.Record("A", w.ID, shot.Mapping{Scale: 1, Width: 100, Height: 100, Coverage: image.Rect(0, 0, 100, 100)})
	a := agent.NewContext(context.Background(), agent.Identity{ID: "A"})
	b := agent.NewContext(context.Background(), agent.Identity{ID: "B"})
	if _, serr := shotPoint(b, s, w, 10, 10, "click"); serr == nil || !strings.Contains(serr.Msg, "no screenshot is available") {
		t.Errorf("B's @10,10 = %v, want no screenshot: A's screenshot is not B's", serr)
	}
	if m, ok := s.Shots.Lookup(AgentOf(a).ID, w.ID); !ok || m.Width != 100 {
		t.Errorf("A's mapping = %v, %v", m, ok)
	}
}
