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

// ClickArgs are click's arguments — Codex's click parameters. Ticket 05
// implements the Element Index form; later tickets add the coordinate form
// (x, y) and Element References (name:, id:, @x,y).
type ClickArgs struct {
	Window       string `json:"window"`                 // Window ID, or an App name when that App has exactly one Window
	ElementIndex *int   `json:"element_index"`          // the Element to click, from that Window's latest Window State
	ClickCount   int    `json:"click_count,omitempty"`  // 1 (default), 2 (double click) or 3 (triple click)
	MouseButton  string `json:"mouse_button,omitempty"` // "left" (default), "right" or "middle"; aliases l, r, m
}

// ActivateArgs are activate-window's arguments: the Window to bring to
// the foreground.
type ActivateArgs struct {
	Window string `json:"window"`
}

// SetValueArgs are set-value's arguments — Codex's set_value parameters:
// the Element Index of a settable Element and its new value.
type SetValueArgs struct {
	Window       string `json:"window"`        // Window ID, or an App name when that App has exactly one Window
	ElementIndex *int   `json:"element_index"` // the Element to set, from that Window's latest Window State
	Value        string `json:"value"`         // the new value (a string, or a number for range Elements)
}

// SecondaryActionArgs are perform-secondary-action's arguments — Codex's
// perform_secondary_action parameters: an Element Index and one of the
// Element's Secondary Actions (case-insensitive).
type SecondaryActionArgs struct {
	Window       string `json:"window"`        // Window ID, or an App name when that App has exactly one Window
	ElementIndex *int   `json:"element_index"` // the Element to act on, from that Window's latest Window State
	Action       string `json:"action"`        // e.g. "Raise", "Scroll Down", "Expand", "Collapse", "Toggle", "Select"
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
