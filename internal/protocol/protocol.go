// Package protocol defines the request/response contract between the winuse
// CLI and the background helper.
//
// Transport is a named pipe (see internal/pipe) carrying JSON lines, one
// request per connection: the CLI dials, writes a single Request line, reads
// a single Response line and closes. There is no multiplexing; cancellation
// (the Esc key) is detected by the helper itself and reported through
// Response.Exit.
package protocol

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// Version is the wire protocol version. The helper refuses mismatching
// requests with a version error.
const Version = 1

// ExitEsc is the exit code reported when the user pressed Esc to stop an
// Action or Batch (Codex's interrupt code).
const ExitEsc = 130

// RequestTimeout is the default per-request time limit (~10 s, Codex-like).
// The client's connection deadline adds slack on top.
const RequestTimeout = 10 * time.Second

// LaunchTimeout is the launch-app budget: Codex gives launching an App
// 15 s instead of the default 10 s (orchestrator decision).
const LaunchTimeout = 15 * time.Second

// MaxLine bounds one JSON line in either direction (a Window State tree can
// be large, but not unbounded).
const MaxLine = 4 << 20

// commandTimeouts holds the per-command request budgets that differ from
// RequestTimeout.
var commandTimeouts = map[string]time.Duration{
	"launch-app": LaunchTimeout,
}

// TimeoutFor returns the request budget for a command (RequestTimeout for
// everything without an explicit entry, e.g. launch-app's 15 s). The CLI
// uses this for its deadlines; the helper starts from its own configured
// requestTimeout and only explicit entries (CommandTimeout) override it.
func TimeoutFor(cmd string) time.Duration {
	if d, ok := commandTimeouts[cmd]; ok {
		return d
	}
	return RequestTimeout
}

// CommandTimeout returns a command's explicitly configured budget, if it
// has one (launch-app: 15 s).
func CommandTimeout(cmd string) (time.Duration, bool) {
	d, ok := commandTimeouts[cmd]
	return d, ok
}

// Request is one command sent by the CLI.
type Request struct {
	V    int             `json:"v"`              // wire protocol version
	Ver  string          `json:"ver"`            // CLI binary version; a helper from a different build exits (see Response.Stale)
	Cmd  string          `json:"cmd"`            // command name, e.g. "list-windows"
	Args json.RawMessage `json:"args,omitempty"` // command arguments, command-specific JSON
}

// LaunchArgs are launch-app's arguments: an App name from the catalog or
// an explicit identifier (executable path, shortcut path, App User Model
// ID) — Codex's launch_app "app" parameter.
type LaunchArgs struct {
	App string `json:"app"`
}

// Response is the helper's answer.
type Response struct {
	OK    bool   `json:"ok"`
	Stale bool   `json:"stale,omitempty"` // helper is from a different winuse build: it exits after answering; the CLI restarts one and retries
	Out   string `json:"out,omitempty"`   // text the CLI prints on stdout
	Err   string `json:"err,omitempty"`   // single-line error message
	Hint  string `json:"hint,omitempty"`  // recovery hint printed after the error
	Exit  int    `json:"exit,omitempty"`  // exit code the CLI should use on failure
}

// ErrLineTooLarge is returned when a line exceeds MaxLine.
var ErrLineTooLarge = errors.New("winuse: message exceeds line limit")

// WriteLine encodes v as one JSON line (json.Encoder terminates with '\n').
func WriteLine(w io.Writer, v any) error {
	return json.NewEncoder(w).Encode(v)
}

// ReadLine reads one '\n'-terminated JSON line capped at MaxLine and
// unmarshals it into v.
func ReadLine(r *bufio.Reader, v any) error {
	var buf []byte
	for {
		chunk, err := r.ReadSlice('\n')
		buf = append(buf, chunk...)
		if errors.Is(err, bufio.ErrBufferFull) {
			if len(buf) > MaxLine {
				return ErrLineTooLarge
			}
			continue
		}
		if err != nil {
			return err // EOF or connection error before a full line
		}
		break
	}
	if len(buf) > MaxLine {
		return ErrLineTooLarge
	}
	return json.Unmarshal(trimEOL(buf), v)
}

func trimEOL(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

// Errorf formats a failure response.
func Errorf(exit int, format string, args ...any) Response {
	return Response{OK: false, Err: fmt.Sprintf(format, args...), Exit: exit}
}

// OKf formats a success response carrying text for stdout.
func OKf(out string) Response {
	return Response{OK: true, Out: out}
}
