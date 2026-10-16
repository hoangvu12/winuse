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
)

// Error is a command failure with the process exit code the CLI should use
// (1 for ordinary failures, protocol.ExitEsc when the user pressed Esc).
type Error struct {
	Msg  string
	Hint string
	Exit int
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
	idle           time.Duration
	requestTimeout time.Duration
	commands       map[string]Command
	log            *log.Logger

	mu        sync.Mutex // guards idleTimer and ln
	idleTimer *time.Timer
	ln        net.Listener

	shutdownOnce sync.Once
	closed       atomic.Bool
}

// New creates a server. idle is how long without a connection before the
// helper exits by itself; requestTimeout bounds one command (slightly under
// the client's deadline). logger may be nil.
func New(version string, idle, requestTimeout time.Duration, commands map[string]Command, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.Default()
	}
	return &Server{
		version:        version,
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
	s.idleTimer = time.AfterFunc(s.idle, func() {
		s.log.Printf("helper idle for %s, exiting", s.idle)
		s.Shutdown()
	})
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
		s.resetIdle()
		inflight.Add(1)
		go func() {
			defer inflight.Done()
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

// resetIdle pushes the idle deadline out after activity.
func (s *Server) resetIdle() {
	s.mu.Lock()
	if s.idleTimer != nil {
		s.idleTimer.Reset(s.idle)
	}
	s.mu.Unlock()
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
	resp := s.handle(&req)
	if err := protocol.WriteLine(conn, &resp); err != nil {
		s.log.Printf("writing response: %v", err)
	}
	if resp.Stale {
		// The CLI that just talked to us is from a different build: this
		// helper is outdated. Exit so the CLI can start a current one.
		s.log.Printf("helper is from a different build (CLI %s, helper %s), exiting", req.Ver, s.version)
		s.Shutdown()
	}
}

// handle dispatches one request to its command.
func (s *Server) handle(req *protocol.Request) protocol.Response {
	if req.V != protocol.Version {
		return protocol.Errorf(1, "wire protocol version %d not supported (helper speaks %d); upgrade winuse", req.V, protocol.Version)
	}
	if req.Ver != s.version {
		return protocol.Response{
			OK:    false,
			Stale: true,
			Err:   fmt.Sprintf("helper is from a different winuse build (helper %s, CLI %s)", s.version, req.Ver),
			Hint:  "the helper exits itself; the CLI restarts a current one",
			Exit:  1,
		}
	}
	cmd, ok := s.commands[req.Cmd]
	if !ok {
		return protocol.Errorf(1, "unknown command %q", req.Cmd)
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.requestTimeout)
	defer cancel()
	out, cmdErr := cmd(ctx, req.Args)
	if cmdErr != nil {
		resp := protocol.Errorf(cmdErr.Exit, "%s", cmdErr.Msg)
		resp.Hint = cmdErr.Hint
		if resp.Exit == 0 {
			resp.Exit = 1
		}
		return resp
	}
	return protocol.OKf(out)
}
