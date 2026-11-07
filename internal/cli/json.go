package cli

// The opt-in --json output (desktop-parity ticket 07): with --json before
// the command, among the other leading flags, every command prints one
// JSON line on stdout instead of its text:
//
//	{"ok":true,"command":"click","out":"..."}
//	{"ok":false,"command":"click","out":"","error":{"code":"STALE_REF","message":"...","hint":"...","delivery":"not_delivered","retry":"safe"}}
//
// The command runs exactly as without the flag; its stdout and stderr are
// captured and become the line. out is the text it printed (a Window
// State stays its text tree), error is its failure: the helper's message,
// hint, code and delivery (protocol codes.go) when the helper reported
// it, else the CLI's own failure line classified the same way. Exit codes
// are unchanged, and without --json nothing changes (ADR 0004).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/hoangvu12/winuse/internal/protocol"
)

const jsonFlag = "--json"

// lastResponse is the helper's answer to the running command's last
// request (finish records it): a --json failure line takes its code,
// message and hint from it.
var lastResponse *protocol.Response

// jsonLine is the one line --json prints.
type jsonLine struct {
	OK      bool       `json:"ok"`
	Command string     `json:"command"`
	Out     string     `json:"out"`
	Error   *jsonError `json:"error,omitempty"`
}

// jsonError is a failed command's error object.
type jsonError struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Hint     string `json:"hint,omitempty"`
	Delivery string `json:"delivery,omitempty"`
	Retry    string `json:"retry,omitempty"`
}

// leadingFlag reports how many tokens the global flag at args[0] takes
// (0 when args[0] is not one): --headed, --no-wait, --json, --trace-unredacted and
// --trace=<file> take one, --trace <file> two.
func leadingFlag(args []string) int {
	switch tok := args[0]; {
	case tok == headedFlag || tok == noWaitFlag || tok == jsonFlag || tok == traceUnredactedFlag || strings.HasPrefix(tok, traceFlag+"="):
		return 1
	case tok == traceFlag:
		return min(2, len(args))
	}
	return 0
}

// extractJSON takes every --json off the leading flags, and names the
// command after them ("" when there is none).
func extractJSON(args []string) (rest []string, on bool, command string) {
	rest = make([]string, 0, len(args))
	i := 0
	for i < len(args) {
		n := leadingFlag(args[i:])
		if n == 0 {
			break
		}
		if args[i] == jsonFlag {
			on = true
		} else {
			rest = append(rest, args[i:i+n]...)
		}
		i += n
	}
	if i < len(args) {
		command = args[i]
	}
	return append(rest, args[i:]...), on, command
}

// runJSON runs the command with its output captured and prints its JSON
// line; the exit code is the command's.
func runJSON(command string, run func() int) int {
	lastResponse = nil
	code, stdout, stderr, err := captureOutput(run)
	if err != nil {
		code, stdout, stderr = 1, "", fmt.Sprintf("winuse: --json: %v\n", err)
	}
	line := jsonLineFor(command, code, stdout, stderr, lastResponse)
	if line.OK && stderr != "" {
		os.Stderr.WriteString(stderr) // a note beside a success stays on stderr
	}
	b, err := json.Marshal(line)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: --json: %v\n", err)
		return 1
	}
	os.Stdout.Write(append(b, '\n'))
	return code
}

// jsonLineFor builds the line of a command that exited with code after
// printing stdout and stderr; resp is the helper's last answer, if any.
func jsonLineFor(command string, code int, stdout, stderr string, resp *protocol.Response) jsonLine {
	line := jsonLine{OK: code == 0, Command: command, Out: stdout}
	if line.OK {
		return line
	}
	var r protocol.Response
	if resp != nil && !resp.OK {
		r = *resp
	} else {
		r = protocol.Response{Exit: code}
		r.Err, r.Hint = failureText(stderr)
		if r.Err == "" {
			r.Err = fmt.Sprintf("winuse exited with code %d", code)
		}
	}
	if r.Code == "" { // the CLI's own failure, or a helper of an older build
		r.Stamp("", "")
	}
	line.Error = &jsonError{Code: r.Code, Message: r.Err, Hint: r.Hint, Delivery: r.Delivery, Retry: r.Retry}
	return line
}

// failureText splits what the CLI printed on stderr for a failure into
// its message (the first line) and hint: the "hint: " line reportError
// prints, or else the next line (a usage line).
func failureText(stderr string) (msg, hint string) {
	msg, rest, _ := strings.Cut(strings.TrimSpace(stderr), "\n")
	hint, _, _ = strings.Cut(strings.TrimSpace(rest), "\n")
	hint = strings.TrimSpace(hint)
	if h, ok := strings.CutPrefix(hint, "hint: "); ok {
		return msg, h
	}
	return msg, hint
}

// captureOutput runs run with os.Stdout and os.Stderr sent to pipes, and
// returns what it printed on each.
func captureOutput(run func() int) (code int, stdout, stderr string, err error) {
	outR, outW, err := os.Pipe()
	if err != nil {
		return 0, "", "", err
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		outR.Close()
		outW.Close()
		return 0, "", "", err
	}
	var out, errOut bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); io.Copy(&out, outR) }()
	go func() { defer wg.Done(); io.Copy(&errOut, errR) }()
	saveOut, saveErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	defer func() {
		os.Stdout, os.Stderr = saveOut, saveErr
		outW.Close()
		errW.Close()
		wg.Wait()
		outR.Close()
		errR.Close()
		stdout, stderr = out.String(), errOut.String()
	}()
	return run(), "", "", nil
}
