// Package cli implements the winuse command line: a thin client that
// guarantees a helper is running, sends one request per invocation and
// prints the response. Command logic (what each command prints) lives with
// the command implementations; the helper does the Windows work.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hoangvu12/winuse/internal/agent"
	"github.com/hoangvu12/winuse/internal/pipe"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/version"
	"github.com/hoangvu12/winuse/internal/win"
)

// Call timeouts: dialing an existing helper is fast; after spawning one, the
// client keeps dialing for a few seconds (the helper starts a pipe listener
// in well under a second).
const (
	dialTimeout   = 1 * time.Second
	spawnDialWait = 5 * time.Second
)

// exchangeSlack is added on top of a command's budget for the connection
// deadline (launch-app gets 15 s, other commands 10 s).
const exchangeSlack = 2 * time.Second

// Client talks to the background helper. The dialer, spawner and verifier
// are pluggable so the state machine (start-on-demand, stale restart,
// dead-helper retry) is testable offline.
type Client struct {
	// Dial connects to the helper, or returns an error wrapping
	// ErrNoHelper when none is running.
	Dial func(ctx context.Context) (net.Conn, error)
	// Spawn starts a detached helper process.
	Spawn func() error
	// Verify checks the dialed pipe is served by our own helper (anti-squat).
	Verify func(conn net.Conn) error
	// Running reports whether a helper holds the single-instance mutex,
	// even one whose pipe is gone (nil: never).
	Running func() bool
	// spawnWait overrides how long to wait for a spawned helper to answer
	// (zero means spawnDialWait; tests shrink it).
	spawnWait time.Duration
	// exitWait overrides how long to wait for a helper on its way out
	// (zero means the request's budget; tests shrink it).
	exitWait time.Duration
}

// ErrNoHelper is embedded in dial errors when no helper is running.
var ErrNoHelper = errors.New("helper is not running")

// TimeoutError reports a request that exceeded its time budget, in the
// decided style: "winuse request timed out: <command>".
type TimeoutError struct{ Cmd string }

func (e *TimeoutError) Error() string { return "winuse request timed out: " + e.Cmd }

// DefaultClient builds a client wired to the real pipe and spawner.
func DefaultClient() (*Client, error) {
	id, err := win.CurrentIdentity()
	if err != nil {
		return nil, err
	}
	dial := func(ctx context.Context) (net.Conn, error) {
		return dialHelper(ctx, id)
	}
	return &Client{
		Dial:    dial,
		Spawn:   win.SpawnHelper,
		Verify:  func(conn net.Conn) error { return verifyHelper(conn, id) },
		Running: func() bool { return pipe.SingletonHeld(id) },
	}, nil
}

// Call sends one request to the helper, starting or restarting it as
// needed:
//
//   - no helper running: spawn one, dial until it answers;
//   - helper answers "stale" (different build): it exits itself; retry once
//     with a fresh helper, and the answer notes the restart (restartNote);
//   - connection drops before a response (helper exited under us): retry
//     once, which respawns it.
//
// A helper on its way out (a stale one finishing its requests in flight)
// is waited for, up to the request's budget, and the answer notes the
// wait (drainNote).
func (c *Client) Call(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	var lastErr error
	restarted, drained := false, false
	for attempt := 0; attempt < 2; attempt++ {
		conn, waited, err := c.dialOrStart(ctx, protocol.Budget(req, protocol.RequestTimeout))
		if err != nil {
			return nil, err
		}
		drained = drained || waited
		resp, err := c.roundTrip(conn, req)
		conn.Close()
		if err == nil {
			if resp.Stale && attempt == 0 {
				// The helper said it is from a different build and is
				// exiting. Wait until its pipe is really gone (it releases
				// the single-instance mutex on the way out), then loop into
				// dialOrStart, which spawns a current one.
				c.waitPipeGone(ctx, 2*time.Second)
				restarted = true
				continue
			}
			if drained && !resp.Stale {
				resp.Out = withNote(resp.Out, drainNote)
			}
			if restarted && !resp.Stale {
				resp.Out = withNote(resp.Out, restartNote)
			}
			return resp, nil
		}
		lastErr = err
		if !isConnDropped(err) {
			return nil, err
		}
		// Helper died under us (idle exit race). Retry once: the next dial
		// finds no helper and respawns it.
	}
	if lastErr == nil {
		lastErr = errors.New("no response from helper")
	}
	return nil, lastErr
}

// restartNote ends the first answer from a helper the CLI restarted for
// its own build (desktop-parity ticket 25): the old helper's Window
// States and Element Indexes went with it.
const restartNote = "Note: the helper was restarted for another winuse build, so Window States and Element Indexes from before are gone; read the Window again, or use name: or id:, which re-resolve live.\n"

// drainNote ends an answer that first waited for the old helper to finish
// the requests it was running and exit (desktop-parity ticket 25).
const drainNote = "Note: waited for the old helper to finish its requests in flight and exit first\n"

// withNote appends a note line to a command's output.
func withNote(out, note string) string {
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out + note
}

// CallExisting sends one request to a helper that is already running and
// never starts one: no helper is an error wrapping ErrNoHelper, and a
// stale answer or a dropped connection is returned as is, with no retry
// (winuse status, desktop parity ticket 16).
func (c *Client) CallExisting(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	conn, err := c.Dial(dialCtx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return c.roundTrip(conn, req)
}

// waitPipeGone dials until the helper pipe stops answering (or the wait
// runs out), so a restart does not race the old helper's exit.
func (c *Client) waitPipeGone(ctx context.Context, wait time.Duration) {
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
		conn, err := c.Dial(dialCtx)
		cancel()
		if err != nil {
			return // pipe gone (or unreachable): safe to restart
		}
		conn.Close()
		time.Sleep(50 * time.Millisecond)
	}
}

// dialOrStart dials the helper; if none is running it spawns one and dials
// until it appears. A helper on its way out is waited for first, up to
// budget (waitExit); drained reports it was still running requests.
func (c *Client) dialOrStart(ctx context.Context, budget time.Duration) (conn net.Conn, drained bool, err error) {
	start := time.Now()
	conn, err = c.dialOnce(ctx)
	if err == nil {
		return conn, false, nil
	}
	if c.exiting(ctx, err) {
		if c.exitWait != 0 {
			budget = c.exitWait
		}
		conn, drained, err = c.waitExit(ctx, err, start.Add(budget))
		if conn != nil || err != nil {
			return conn, drained, err
		}
	} else if !errors.Is(err, ErrNoHelper) {
		return nil, false, err
	}
	if err := c.Spawn(); err != nil {
		return nil, drained, fmt.Errorf("starting helper: %w", err)
	}
	wait := c.spawnWait
	if wait == 0 {
		wait = spawnDialWait
	}
	spawnCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	conn, err = c.dialUntil(spawnCtx)
	return conn, drained, err
}

// dialOnce dials the helper once, within dialTimeout.
func (c *Client) dialOnce(ctx context.Context) (net.Conn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	return c.Dial(dialCtx)
}

// exiting reports whether a dial error means a helper on its way out
// (desktop-parity ticket 25). A stale helper stops listening and then
// finishes its requests in flight, up to a Batch's 300 s: meanwhile its
// pipe stays busy (no instance listens, so a dial times out), and once
// the pipe is gone it still holds the single-instance mutex for a moment,
// so a helper spawned then loses the mutex and exits.
func (c *Client) exiting(ctx context.Context, err error) bool {
	if errors.Is(err, ErrNoHelper) {
		return c.Running != nil && c.Running()
	}
	return isBusy(ctx, err)
}

// isBusy reports a dial that timed out on its own dialTimeout, with the
// caller's ctx still live: the pipe exists but nothing listens on it. A
// live helper listens again within milliseconds of each accept.
func isBusy(ctx context.Context, err error) bool {
	return errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil
}

// exitPoll is how often waitExit checks a helper on its way out.
const exitPoll = 50 * time.Millisecond

// waitExit waits, until deadline, for a helper on its way out to exit
// (its pipe gone and the mutex free: conn and err are nil, spawn one), or
// for a helper's pipe to come up (another CLI started one: conn). err is
// the dial error that found it exiting.
func (c *Client) waitExit(ctx context.Context, err error, deadline time.Time) (conn net.Conn, drained bool, _ error) {
	for {
		if isBusy(ctx, err) {
			drained = true
		}
		if time.Now().After(deadline) {
			return nil, drained, exitWaitError(drained)
		}
		select {
		case <-ctx.Done():
			return nil, drained, ctx.Err()
		case <-time.After(exitPoll):
		}
		conn, err = c.dialOnce(ctx)
		if err == nil {
			return conn, drained, nil
		}
		if !c.exiting(ctx, err) {
			if errors.Is(err, ErrNoHelper) {
				return nil, drained, nil // gone: spawn a helper
			}
			return nil, drained, err
		}
	}
}

// exitWaitError is the failure of a request whose budget ran out while it
// waited for the old helper to exit: nothing reached a helper.
func exitWaitError(drained bool) error {
	msg := "the old winuse helper did not exit before this request's budget expired; nothing was sent"
	if drained {
		msg = "the old winuse helper was still finishing its requests in flight when this request's budget expired; nothing was sent"
	}
	return &exitError{msg: msg, hint: "retry once it has exited; winuse then starts a helper of its own build", code: 1}
}

// dialUntil dials with backoff until a helper answers or ctx ends. Covers
// both "pipe not created yet" (ErrNoHelper) and "helper still starting".
func (c *Client) dialUntil(ctx context.Context) (net.Conn, error) {
	backoff := 20 * time.Millisecond
	for {
		conn, err := c.Dial(ctx)
		if err == nil {
			return conn, nil
		}
		if !errors.Is(err, ErrNoHelper) {
			return nil, fmt.Errorf("helper is not answering: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("helper did not start: %w", ctx.Err())
		case <-time.After(backoff):
		}
		if backoff < 200*time.Millisecond {
			backoff *= 2
		}
	}
}

// roundTrip performs one request/response exchange on one connection.
func (c *Client) roundTrip(conn net.Conn, req *protocol.Request) (*protocol.Response, error) {
	if c.Verify != nil {
		if err := c.Verify(conn); err != nil {
			return nil, fmt.Errorf("verifying helper: %w", err)
		}
	}
	// The command's budget (the explicit entry, else the default, widened
	// by a caller-set --timeout) plus the exchange slack for the
	// connection deadline.
	deadline := protocol.Budget(req, protocol.RequestTimeout) + exchangeSlack
	if err := conn.SetDeadline(time.Now().Add(deadline)); err != nil {
		return nil, err
	}
	if err := protocol.WriteLine(conn, req); err != nil {
		if isDeadline(err) {
			return nil, &TimeoutError{Cmd: req.Cmd}
		}
		return nil, fmt.Errorf("sending request: %w", err)
	}
	var resp protocol.Response
	if err := readResponse(conn, &resp); err != nil {
		if isDeadline(err) {
			return nil, &TimeoutError{Cmd: req.Cmd}
		}
		return nil, fmt.Errorf("reading response: %w", err)
	}
	return &resp, nil
}

// isDeadline reports whether an error came from a connection or context
// deadline (the client's copy of the request-timeout policy).
func isDeadline(err error) bool {
	return errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, context.DeadlineExceeded)
}

// isConnDropped reports whether an exchange error means the helper vanished
// mid-request (idle exit race, crash) rather than timed out or failed
// cleanly — only those are worth an immediate retry.
func isConnDropped(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	// go-winio surfaces a closed pipe as one of these Windows errnos
	// (109 = broken pipe, 232 = pipe being closed), possibly wrapped.
	if errors.Is(err, syscall.Errno(109)) || errors.Is(err, syscall.Errno(232)) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "has been ended") ||
		strings.Contains(msg, "is being closed") ||
		strings.Contains(msg, "connection reset")
}

// callingAgent is the Agent this CLI process runs for, resolved once per
// process (multi-agent ticket 02): WINUSE_AGENT, or the Harness found by
// walking up the parent processes. Tests replace it.
var callingAgent = sync.OnceValue(agent.Resolve)

// Request builds a protocol request for a command, naming the calling
// Agent.
func Request(cmd string, args any) (*protocol.Request, error) {
	who := callingAgent()
	req := &protocol.Request{V: protocol.Version, Ver: version.Version, Cmd: cmd, Agent: who.ID, AgentLabel: who.Label, AgentColor: who.Color, Build: version.Build()}
	if args != nil {
		raw, err := json.Marshal(args)
		if err != nil {
			return nil, err
		}
		req.Args = raw
	}
	return req, nil
}
