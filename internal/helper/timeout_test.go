package helper

// Caller-set timeouts (desktop-parity ticket 14): the request's
// TimeoutMS reaches the command's context deadline, clamped to the
// command's cap; Actions keep their fixed budget; get-window-state widens
// the Chromium settle with it and launch-app its poll budget.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// deadlineCommand reports how much time its context had left when it ran.
func deadlineCommand(left chan<- time.Duration) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		dl, ok := ctx.Deadline()
		if !ok {
			left <- 0
			return "ok\n", nil
		}
		left <- time.Until(dl)
		return "ok\n", nil
	}
}

func TestServeCallerTimeoutReachesTheContext(t *testing.T) {
	left := make(chan time.Duration, 1)
	cmds := map[string]Command{}
	for _, name := range []string{"get-window-state", "launch-app", "batch", "wait", "click"} {
		cmds[name] = deadlineCommand(left)
	}
	_, ln := startServer(t, time.Minute, cmds)
	cases := []struct {
		cmd  string
		ms   int
		want time.Duration
	}{
		{"get-window-state", 0, protocol.RequestTimeout},
		{"get-window-state", 40000, 40 * time.Second},
		{"get-window-state", 3000, protocol.RequestTimeout},             // only widens
		{"get-window-state", 900000, protocol.MaxStateTimeout},          // clamped
		{"launch-app", 90000, 90 * time.Second},                         // over the 15 s launch budget
		{"launch-app", 900000, protocol.MaxLaunchTimeout},               // clamped
		{"batch", 240000, 240 * time.Second},                            // over the 60 s batch budget
		{"wait", 61000, 61 * time.Second},                               // a wait of 60 s plus its margin
		{"wait", 900000, protocol.MaxWaitTimeout + protocol.WaitMargin}, // clamped
		{"click", 60000, protocol.RequestTimeout},                       // an Action keeps its budget
	}
	for _, c := range cases {
		client := ln.push()
		client.SetDeadline(time.Now().Add(5 * time.Second))
		req := testRequest(c.cmd)
		req.TimeoutMS = c.ms
		resp, err := call(client, req)
		client.Close()
		if err != nil || !resp.OK {
			t.Fatalf("%s: %v %+v", c.cmd, err, resp)
		}
		got := <-left
		if got > c.want || got < c.want-2*time.Second {
			t.Errorf("%s with %d ms: the command had %s left, want about %s", c.cmd, c.ms, got, c.want)
		}
	}
}

// settleTrees is a TreeReader that records the Chromium settle budget its
// reads would use (uia.SettleOf of the read's context).
type settleTrees struct {
	mu     sync.Mutex
	settle time.Duration
}

func (s *settleTrees) Capture(ctx context.Context, hwnd uint64, title, app string, budget time.Duration) (*wstate.Tree, error) {
	s.mu.Lock()
	s.settle = uia.SettleOf(ctx)
	s.mu.Unlock()
	return &wstate.Tree{Title: title, App: app, FocusedIndex: -1}, nil
}

func (s *settleTrees) got() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settle
}

// TestGetWindowStateTimeoutWidensSettle drives get-window-state through the
// server: the default budget keeps the 5 s settle; --timeout 40 widens it
// by the 30 s it adds, which the request deadline still bounds.
func TestGetWindowStateTimeoutWidensSettle(t *testing.T) {
	trees := &settleTrees{}
	_, ln := startServer(t, time.Minute, CommandsWith(testServices(trees)))
	for _, c := range []struct {
		ms   int
		want time.Duration
	}{
		{0, uia.DefaultChromiumSettle},
		{3000, uia.DefaultChromiumSettle},
		{40000, uia.DefaultChromiumSettle + 30*time.Second},
		{900000, uia.DefaultChromiumSettle + protocol.MaxStateTimeout - protocol.RequestTimeout},
	} {
		client := ln.push()
		client.SetDeadline(time.Now().Add(5 * time.Second))
		req := testRequest("get-window-state")
		req.Args, _ = json.Marshal(windowRef{Window: "101"})
		req.TimeoutMS = c.ms
		resp, err := call(client, req)
		client.Close()
		if err != nil || !resp.OK {
			t.Fatalf("get-window-state with %d ms: %v %+v", c.ms, err, resp)
		}
		if got := trees.got(); got > c.want || got < c.want-2*time.Second {
			t.Errorf("get-window-state with %d ms: settle %s, want about %s", c.ms, got, c.want)
		}
	}
}

func TestStateSettle(t *testing.T) {
	if got := stateSettle(context.Background()); got != uia.DefaultChromiumSettle {
		t.Errorf("no deadline: %s", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), protocol.RequestTimeout)
	defer cancel()
	if got := stateSettle(ctx); got != uia.DefaultChromiumSettle {
		t.Errorf("the default budget: %s, want %s", got, uia.DefaultChromiumSettle)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel2()
	if got := stateSettle(ctx2); got < 54*time.Second || got > 55*time.Second {
		t.Errorf("a 60 s budget: settle %s, want about 55s", got)
	}
}

func TestLaunchPoll(t *testing.T) {
	if got := launchPoll(context.Background()); got != launchPollBudget {
		t.Errorf("no deadline: %s", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), protocol.LaunchTimeout)
	defer cancel()
	if got := launchPoll(ctx); got != launchPollBudget {
		t.Errorf("the default budget: %s, want %s", got, launchPollBudget)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel2()
	if got := launchPoll(ctx2); got < 89*time.Second || got >= 90*time.Second {
		t.Errorf("a 90 s budget: poll %s, want just under 90s", got)
	}
}
