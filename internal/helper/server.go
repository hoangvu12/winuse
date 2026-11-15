// Package helper runs the winuse background helper: it listens on the
// user-restricted named pipe, answers one request per connection, and exits
// by itself after an idle period.
//
// The serving loop is transport-agnostic (a net.Listener), so tests drive it
// with fake listeners; Main wires it to the real pipe.
package helper

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/version"
)

// Error is a command failure with the process exit code the CLI should use
// (1 for ordinary failures, protocol.ExitEsc when the user pressed Esc).
// Out is what the CLI prints on stdout before the failure: the steps a
// failed Batch ran (batch.go, desktop-parity ticket 20); empty otherwise.
// Code is the failure's code (protocol codes.go, desktop-parity ticket
// 07) when the site knows better than its message; empty, the message
// and hint decide it (protocol.CodeFor).
type Error struct {
	Msg  string
	Hint string
	Exit int
	Out  string
	Code string
	// Delivery is the failure's own delivery (protocol.Delivery*, set by
	// nothingSent, mayHaveActed, wasDelivered or actionDelivered); "" lets
	// Stamp read it from the text (delivery.go, reliability-council 02).
	Delivery string
	// wait is what a refusal that sent nothing and may pass on its own
	// waits for (briefwait.go, reliability-council 06); nil for any other
	// failure.
	wait *refusalWait
}

func (e *Error) Error() string { return e.Msg }

// Fail builds an *Error with exit code 1.
func Fail(msg, hint string) *Error { return &Error{Msg: msg, Hint: hint, Exit: 1} }

// Command executes one helper command. Args is the command-specific JSON
// (nil when the command takes none). Handlers run on the serving goroutine;
// commands that need UI Automation will hop onto the helper's single
// COM-locked thread (later tickets) — the contract stays the same.
type Command func(ctx context.Context, args json.RawMessage) (string, *Error)

// Server serves helper requests until it goes idle.
type Server struct {
	version        string // binary version; mismatching CLI versions make the helper exit
	build          string // executable build (version.Build); a CLI of another build makes it exit too, "" matches any
	idle           time.Duration
	requestTimeout time.Duration
	commands       map[string]Command
	log            *log.Logger

	mu        sync.Mutex // guards idleTimer, ln and active
	idleTimer *time.Timer
	ln        net.Listener
	active    int // connections being served; the idle period starts when the last one ends

	shutdownOnce sync.Once
	closed       atomic.Bool
}

// New creates a server. idle is how long without a connection being
// served before the helper exits by itself; requestTimeout bounds one
// command (slightly under the client's deadline). logger may be nil.
func New(ver string, idle, requestTimeout time.Duration, commands map[string]Command, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.Default()
	}
	return &Server{
		version:        ver,
		build:          version.Build(),
		idle:           idle,
		requestTimeout: requestTimeout,
		commands:       commands,
		log:            logger,
	}
}

// Serve accepts connections on ln until the idle timer closes it or Shutdown
// is requested. It returns nil after draining in-flight requests; any other
// error is a fatal accept failure.
func (s *Server) Serve(ln net.Listener) error {
	s.mu.Lock()
	s.ln = ln
	s.idleTimer = time.AfterFunc(s.idle, s.idleExpired)
	s.mu.Unlock()

	var inflight sync.WaitGroup
	for {
		conn, err := ln.Accept()
		if err != nil {
			if s.closed.Load() || errors.Is(err, net.ErrClosed) {
				inflight.Wait()
				return nil
			}
			// Transient accept failure: back off briefly rather than spin.
			s.log.Printf("accept error: %v", err)
			time.Sleep(10 * time.Millisecond)
			continue
		}
		s.begin()
		inflight.Add(1)
		go func() {
			defer inflight.Done()
			defer s.end()
			s.serveConn(conn)
		}()
	}
}

// Shutdown stops accepting new connections. In-flight requests finish first;
// Serve then returns. Safe to call more than once.
func (s *Server) Shutdown() {
	s.shutdownOnce.Do(func() {
		s.closed.Store(true)
		s.mu.Lock()
		ln := s.ln
		s.mu.Unlock()
		if ln != nil {
			ln.Close()
		}
	})
}

// begin marks a connection being served: the helper is not idle until it
// ends.
func (s *Server) begin() {
	s.mu.Lock()
	s.active++
	s.mu.Unlock()
}

// end marks a served connection done and, when it was the last one,
// restarts the idle period from now. A request that outlasts the idle
// period (a cold Chromium read takes seconds) must not take the helper,
// and the Window State it just cached, down right after it answers.
func (s *Server) end() {
	s.mu.Lock()
	s.active--
	if s.active == 0 && s.idleTimer != nil {
		s.idleTimer.Reset(s.idle)
	}
	s.mu.Unlock()
}

// idleExpired shuts the helper down after the idle period, unless a
// connection is still being served (its end restarts the period).
func (s *Server) idleExpired() {
	s.mu.Lock()
	busy := s.active > 0
	s.mu.Unlock()
	if busy {
		return
	}
	s.log.Printf("helper idle for %s, exiting", s.idle)
	s.Shutdown()
}

// serveConn answers exactly one request on one connection.
func (s *Server) serveConn(conn net.Conn) {
	defer conn.Close()
	// Cover the whole exchange: the handler runs under its own, tighter
	// requestTimeout; this deadline also unblocks a client that vanishes.
	deadline := time.Now().Add(s.requestTimeout + 2*time.Second)
	if err := conn.SetDeadline(deadline); err != nil {
		s.log.Printf("setting deadline: %v", err)
		return
	}
	// Refuse junk early: cap what we read (protocol.MaxLine).
	var req protocol.Request
	if err := protocol.ReadLine(bufio.NewReader(conn), &req); err != nil {
		return // malformed request; drop the connection
	}
	// Commands with an explicitly longer budget (launch-app: 15 s) or a
	// caller-set --timeout get it: extend the exchange deadline to cover
	// the whole request.
	if d := protocol.Budget(&req, s.requestTimeout); d > s.requestTimeout {
		if err := conn.SetDeadline(time.Now().Add(d + 2*time.Second)); err != nil {
			s.log.Printf("extending deadline: %v", err)
			return
		}
	}
	resp := s.handle(&req)
	if err := protocol.WriteLine(conn, &resp); err != nil {
		s.log.Printf("writing response: %v", err)
	}
	if resp.Stale {
		// The CLI that just talked to us is from a different build: this
		// helper is outdated. Exit so the CLI can start a current one.
		// Shutdown drains first: the requests in flight finish, with
		// their give-back and held-key release, before Serve returns.
		s.log.Printf("helper is from a different build (CLI %s, helper %s), exiting after the requests in flight",
			buildName(req.Ver, req.Build), buildName(s.version, s.build))
		s.Shutdown()
	}
}

// handle dispatches one request to its command. Every failure leaves
// with its code and delivery (protocol codes.go, desktop-parity ticket 07).
func (s *Server) handle(req *protocol.Request) (resp protocol.Response) {
	var code, delivery string // the failing command's own code and delivery, if it set them
	defer func() { resp.Stamp(code, delivery) }()
	if req.V != protocol.Version {
		return protocol.Errorf(1, "wire protocol version %d not supported (helper speaks %d); upgrade winuse", req.V, protocol.Version)
	}
	// status only reads, so a CLI of another build may ask it without
	// stopping this helper: the report names both versions and builds
	// (tickets 16 and 25).
	if s.otherBuild(req) && req.Cmd != StatusCommand {
		return protocol.Response{
			OK:    false,
			Stale: true,
			Err:   fmt.Sprintf("helper is from a different winuse build (helper %s, CLI %s)", buildName(s.version, s.build), buildName(req.Ver, req.Build)),
			Hint:  "the helper exits itself; the CLI restarts a current one",
			Exit:  1,
		}
	}
	cmd, ok := s.commands[req.Cmd]
	if !ok {
		return protocol.Errorf(1, "unknown command %q", req.Cmd)
	}
	// The request budget: the server's configured default, overridden by
	// commands with an explicit budget (launch-app: 15 s) and widened by a
	// caller-set --timeout (desktop-parity ticket 14).
	ctx, cancel := context.WithTimeout(context.Background(), protocol.Budget(req, s.requestTimeout))
	defer cancel()
	// The Agent the request comes from (agents.go, multi-agent ticket 02).
	ctx = withAgent(ctx, requestAgent(req))
	if req.Observe != nil {
		ctx = withObserve(ctx, req.Observe) // observe.go: the registry's wrapper reads it
	}
	if req.NoWait {
		ctx = withNoWait(ctx) // briefwait.go: a refusal fails at once
	}
	// A traced request gets what its command noted (trace.go,
	// desktop-parity ticket 19), whatever the outcome.
	ctx, notes := withTraceNotes(ctx, req.Trace)
	defer func() { resp.Trace = notes.report() }()
	out, cmdErr := cmd(ctx, req.Args)
	if cmdErr != nil {
		code = cmdErr.Code
	}
	// Timeout policy (orchestrator decision): a command that failed or
	// produced nothing while its budget expired is reported centrally as
	// "winuse request timed out: <command>". A command that returned
	// partial output past its budget delivers that output — the
	// partial-result-with-note behaviour later tickets rely on. An Esc
	// stop outranks the timeout: a press in the budget's last moment is
	// the user's signal, and reporting it keeps exit 130 and its message
	// instead of a timeout the user did not cause.
	// A failure's Out (the steps a Batch ran) travels with it either way.
	if cmdErr != nil && cmdErr.Exit == protocol.ExitEsc {
		code, delivery = protocol.CodeStopped, cmdErr.Delivery
		resp := protocol.Errorf(cmdErr.Exit, "%s", cmdErr.Msg)
		resp.Hint, resp.Out = cmdErr.Hint, cmdErr.Out
		return resp
	}
	if ctx.Err() != nil && (cmdErr != nil || out == "") {
		code = protocol.CodeTimeout
		resp := timeoutResponse(req.Cmd)
		if cmdErr != nil {
			resp.Out = cmdErr.Out
		}
		return resp
	}
	if cmdErr != nil {
		delivery = cmdErr.Delivery
		resp := protocol.Errorf(cmdErr.Exit, "%s", cmdErr.Msg)
		resp.Hint, resp.Out = cmdErr.Hint, cmdErr.Out
		if resp.Exit == 0 {
			resp.Exit = 1
		}
		return resp
	}
	return protocol.OK(out)
}

// otherBuild reports whether the request comes from a CLI of another
// build: another version, or the same version with another executable
// build (desktop-parity ticket 25). An empty build on either side, a CLI
// or helper from before builds were compared, matches.
func (s *Server) otherBuild(req *protocol.Request) bool {
	if req.Ver != s.version {
		return true
	}
	return req.Build != "" && s.build != "" && req.Build != s.build
}

// buildName names a version and, when known, its build: "0.1.0 build
// <id>".
func buildName(ver, build string) string {
	if build == "" {
		return ver
	}
	return ver + " build " + build
}

// timeoutResponse builds the request-timeout failure in the decided style.
func timeoutResponse(command string) protocol.Response {
	return protocol.Response{OK: false, Err: "winuse request timed out: " + command, Exit: 1}
}
