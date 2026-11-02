package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestTakeTimeout(t *testing.T) {
	cases := []struct {
		cmd     string
		args    []string
		rest    []string
		timeout time.Duration
		err     string
	}{
		{"get-window-state", []string{"101"}, []string{"101"}, 0, ""},
		{"get-window-state", []string{"101", "--timeout", "30"}, []string{"101"}, 30 * time.Second, ""},
		{"get-window-state", []string{"--timeout=2.5", "101", "--screenshot"}, []string{"101", "--screenshot"}, 2500 * time.Millisecond, ""},
		{"get-window-state", []string{"101", "--timeout", "60"}, []string{"101"}, 60 * time.Second, ""},
		{"get-window-state", []string{"101", "--timeout", "61"}, nil, 0, "at most 60"},
		{"get-window-state", []string{"101", "--timeout", "0"}, nil, 0, "above 0"},
		{"get-window-state", []string{"101", "--timeout", "soon"}, nil, 0, `got "soon"`},
		{"get-window-state", []string{"101", "--timeout"}, nil, 0, "needs a number of seconds"},
		{"launch-app", []string{"Excel", "--timeout", "120"}, []string{"Excel"}, 120 * time.Second, ""},
		{"launch-app", []string{"Excel", "--timeout", "121"}, nil, 0, "at most 120"},
		{"click", []string{"--timeout", "5"}, nil, 0, "click takes no --timeout"},
	}
	for _, c := range cases {
		rest, timeout, err := takeTimeout(c.cmd, c.args)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%s %q: err %v, want %q", c.cmd, c.args, err, c.err)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(rest, c.rest) || timeout != c.timeout {
			t.Errorf("%s %q: %q, %s, %v; want %q, %s", c.cmd, c.args, rest, timeout, err, c.rest, c.timeout)
		}
	}
}

// TestTakeBatchTimeout: batch's --timeout goes before the first Action; a
// later one is a step's (wait's own).
func TestTakeBatchTimeout(t *testing.T) {
	rest, timeout, err := takeBatchTimeout([]string{"101", "--timeout", "120", "click", "3"})
	if err != nil || timeout != 120*time.Second || !reflect.DeepEqual(rest, []string{"101", "click", "3"}) {
		t.Fatalf("after the Window: %q %s %v", rest, timeout, err)
	}
	rest, timeout, err = takeBatchTimeout([]string{"--timeout=90", "101", "click", "3"})
	if err != nil || timeout != 90*time.Second || !reflect.DeepEqual(rest, []string{"101", "click", "3"}) {
		t.Fatalf("before the Window: %q %s %v", rest, timeout, err)
	}
	args := []string{"301", "navigate", "a.test", "then", "wait", "text", "Done", "--timeout", "20"}
	rest, timeout, err = takeBatchTimeout(args)
	if err != nil || timeout != 0 || !reflect.DeepEqual(rest, args) {
		t.Fatalf("a step's --timeout must stay the step's: %q %s %v", rest, timeout, err)
	}
	if _, _, err := takeBatchTimeout([]string{"101", "--timeout", "301", "click", "3"}); err == nil || !strings.Contains(err.Error(), "at most 300") {
		t.Fatalf("over the cap: %v", err)
	}
	if code := batchCmd([]string{"101", "--timeout", "999", "click", "3"}, false); code != 2 {
		t.Fatalf("batch over the cap exits %d, want 2", code)
	}
}

// TestWaitTimeout: wait's --timeout (up to 300 s) asks for the wait plus
// the margin as the request budget, so the CLI's deadline and the
// helper's follow it.
func TestWaitTimeout(t *testing.T) {
	a, err := parseWaitArgs([]string{"text", "Done", "--timeout", "120"})
	if err != nil {
		t.Fatal(err)
	}
	if got := waitTimeout(a); got != 120*time.Second+protocol.WaitMargin {
		t.Fatalf("waitTimeout = %s", got)
	}
	if got := waitTimeout(protocol.WaitArgs{For: "load"}); got != 0 {
		t.Fatalf("no --timeout: %s", got)
	}
	if _, err := parseWaitArgs([]string{"load", "--timeout", "300"}); err != nil {
		t.Fatalf("300 s is allowed: %v", err)
	}
	req := &protocol.Request{Cmd: "wait", TimeoutMS: int(waitTimeout(a) / time.Millisecond)}
	if got := protocol.Budget(req, protocol.RequestTimeout); got != 121*time.Second {
		t.Fatalf("the request budget = %s, want 121s", got)
	}
}

// deadlineConn records the connection deadline roundTrip sets.
type deadlineConn struct {
	*fakeConn
	deadline time.Time
}

func (c *deadlineConn) SetDeadline(t time.Time) error { c.deadline = t; return nil }

// TestRoundTripDeadlineFollowsTimeout: the CLI's pipe deadline is the
// request's budget, caller-set timeout included, plus the exchange slack;
// the request carries the timeout to the helper.
func TestRoundTripDeadlineFollowsTimeout(t *testing.T) {
	cases := []struct {
		req  protocol.Request
		want time.Duration
	}{
		{protocol.Request{Cmd: "get-window-state"}, protocol.RequestTimeout},
		{protocol.Request{Cmd: "get-window-state", TimeoutMS: 45000}, 45 * time.Second},
		{protocol.Request{Cmd: "get-window-state", TimeoutMS: 999000}, protocol.MaxStateTimeout},
		{protocol.Request{Cmd: "batch", TimeoutMS: 200000}, 200 * time.Second},
		{protocol.Request{Cmd: "click", TimeoutMS: 60000}, protocol.RequestTimeout},
	}
	for _, c := range cases {
		conn := &deadlineConn{fakeConn: respondingConn(&protocol.Response{OK: true, Out: "ok\n"})}
		start := time.Now()
		if _, err := (&Client{}).roundTrip(conn, &c.req); err != nil {
			t.Fatalf("%s: %v", c.req.Cmd, err)
		}
		got := conn.deadline.Sub(start)
		want := c.want + exchangeSlack
		if got < want-time.Second || got > want+time.Second {
			t.Errorf("%s with %d ms: deadline in %s, want about %s", c.req.Cmd, c.req.TimeoutMS, got, want)
		}
		var sent protocol.Request
		if err := json.Unmarshal([]byte(strings.TrimSpace(conn.written.String())), &sent); err != nil || sent.TimeoutMS != c.req.TimeoutMS {
			t.Errorf("%s: sent %s (%v)", c.req.Cmd, conn.written.String(), err)
		}
	}
}
