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
	"syscall"
	"time"

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
	// spawnWait overrides how long to wait for a spawned helper to answer
	// (zero means spawnDialWait; tests shrink it).
	spawnWait time.Duration
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
		Dial:   dial,
		Spawn:  win.SpawnHelper,
		Verify: func(conn net.Conn) error { return verifyHelper(conn, id) },
	}, nil
}

// Call sends one request to the helper, starting or restarting it as
// needed:
//
//   - no helper running: spawn one, dial until it answers;
//   - helper answers "stale" (different build): it exits itself; retry once
//     with a fresh helper;
//   - connection drops before a response (helper exited under us): retry
//     once, which respawns it.
func (c *Client) Call(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		conn, err := c.dialOrStart(ctx)
		if err != nil {
			return nil, err
		}
		resp, err := c.roundTrip(conn, req)
		conn.Close()
		if err == nil {
			if resp.Stale && attempt == 0 {
				// The helper said it is from a different build and is
				// exiting. Wait until its pipe is really gone (it releases
				// the single-instance mutex on the way out), then loop into
				// dialOrStart, which spawns a current one.
				c.waitPipeGone(ctx, 2*time.Second)
				continue
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
// until it appears.
func (c *Client) dialOrStart(ctx context.Context) (net.Conn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	conn, err := c.Dial(dialCtx)
	if err == nil || !errors.Is(err, ErrNoHelper) {
		return conn, err
	}
	if err := c.Spawn(); err != nil {
		return nil, fmt.Errorf("starting helper: %w", err)
	}
	wait := c.spawnWait
	if wait == 0 {
		wait = spawnDialWait
	}
	spawnCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	return c.dialUntil(spawnCtx)
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
	if err := conn.SetDeadline(time.Now().Add(protocol.RequestTimeout + 2*time.Second)); err != nil {
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

// Request builds a protocol request for a command.
func Request(cmd string, args any) (*protocol.Request, error) {
	req := &protocol.Request{V: protocol.Version, Ver: version.Version, Cmd: cmd}
	if args != nil {
		raw, err := json.Marshal(args)
		if err != nil {
			return nil, err
		}
		req.Args = raw
	}
	return req, nil
}
