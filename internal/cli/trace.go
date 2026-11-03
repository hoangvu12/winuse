package cli

// Session traces (desktop-parity ticket 19): with --trace <file> before
// the command, or WINUSE_TRACE=<file>, every command appends one JSON
// line to the file: when it ran, the command, the calling Agent, the
// Window (ID, App and title), its arguments, the routes it took
// (Headless, Posted, Headed, CDP), the outcome, what it printed, the
// files it referenced and how long it took. The Window and the routes
// come from the helper (protocol.Response.Trace); the rest is the CLI's.
//
// Redacted by default: typed text, set-value values, clipboard-set text,
// a prompt's dialog answer, eval expressions, eval results and
// clipboard-get's text and files become {"redacted":true,"len":N} (N in
// characters). --trace-unredacted keeps them, for local debugging only.
// Whatever the flag, a Window State tree is never kept (only its line
// count, and the screenshot by path), so neither its size nor the names
// of its password fields reach the trace, and the output kept is cut at
// traceOutCap. A file past traceMaxBytes is renamed <name>.1<ext> (the
// first free number) and a new one started.
//
// The trace never changes a command's outcome: a trace that cannot be
// written is one "winuse: trace: ..." line on stderr. `winuse trace show
// <file>` prints a readable summary; it is not traced itself.

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hoangvu12/winuse/internal/protocol"
)

const (
	traceFlag           = "--trace"
	traceUnredactedFlag = "--trace-unredacted"
	traceEnv            = "WINUSE_TRACE"

	// traceOutCap bounds the output a line keeps, after redaction.
	traceOutCap = 4 << 10
	// traceTimeLayout is a line's time: local, to the millisecond.
	traceTimeLayout = "2006-01-02T15:04:05.000Z07:00"
)

// traceMaxBytes is the size a trace file rolls over at (agent-desktop's
// 32 MiB). Tests shrink it.
var traceMaxBytes int64 = 32 << 20

// traceRun is one traced command, from Main's start to its exit code.
// nil means the command is not traced; every method is a no-op then.
type traceRun struct {
	path       string
	unredacted bool
	start      time.Time
	command    string // the CLI's command; the request's wire name replaces it
	req        *protocol.Request
	resp       *protocol.Response
	err        error
}

// activeTrace is the running command's trace, nil when it is not traced.
// runRequest reports its exchange to it.
var activeTrace *traceRun

// traceLine is one line of a trace file.
type traceLine struct {
	Time       string                `json:"time"`
	Command    string                `json:"command"`
	Agent      string                `json:"agent,omitempty"`
	AgentLabel string                `json:"agent_label,omitempty"`
	Window     *protocol.TraceWindow `json:"window,omitempty"`
	Args       json.RawMessage       `json:"args,omitempty"`
	Routes     []string              `json:"routes,omitempty"`
	OK         bool                  `json:"ok"`
	Exit       int                   `json:"exit"`
	Error      string                `json:"error,omitempty"`
	Hint       string                `json:"hint,omitempty"`
	// Ticket 07's code and delivery go here, copied from
	// protocol.Response once it carries them.
	Out        any      `json:"out,omitempty"`       // the output kept: text, or a redaction
	OutLen     int      `json:"out_len,omitempty"`   // the whole output's length in characters
	OutLines   int      `json:"out_lines,omitempty"` // a Window State's line count (its text is not kept)
	Truncated  bool     `json:"truncated,omitempty"` // Out stops at traceOutCap
	Refs       []string `json:"refs,omitempty"`      // files the output names: screenshots, crops, images
	Unredacted bool     `json:"unredacted,omitempty"`
	DurationMS int64    `json:"duration_ms"`
}

// redaction replaces a redacted value in a trace line.
type redaction struct {
	Redacted bool `json:"redacted"`
	Len      int  `json:"len"`
}

func redact(s string) redaction { return redaction{Redacted: true, Len: utf8.RuneCountInString(s)} }

// startTrace takes the trace flags off the front of a command line, before
// the command (where --headed may sit among them), and starts the
// command's trace: from --trace <file>, --trace=<file> or WINUSE_TRACE.
// It returns the rest of the command line; ok is false after a usage
// error, which it printed. `trace` (trace show) is never traced.
func startTrace(args []string, getenv func(string) string) (rest []string, run *traceRun, ok bool) {
	path, unredacted, set := "", false, false
	rest = make([]string, 0, len(args))
	i := 0
	for ; i < len(args); i++ {
		tok := args[i]
		switch {
		case tok == headedFlag:
			rest = append(rest, tok)
			continue
		case tok == traceUnredactedFlag:
			unredacted = true
			continue
		case tok == traceFlag || strings.HasPrefix(tok, traceFlag+"="):
			p, inline := strings.CutPrefix(tok, traceFlag+"=")
			if !inline {
				if i+1 >= len(args) {
					p = ""
				} else {
					i++
					p = args[i]
				}
			}
			if strings.TrimSpace(p) == "" {
				fmt.Fprintf(os.Stderr, "winuse: --trace takes a file\n%s", Usage)
				return nil, nil, false
			}
			path, set = p, true
			continue
		}
		break
	}
	rest = append(rest, args[i:]...)
	if !set {
		path = strings.TrimSpace(getenv(traceEnv))
	}
	if path == "" {
		if unredacted {
			fmt.Fprintf(os.Stderr, "winuse: --trace-unredacted needs --trace <file> or %s\n", traceEnv)
			return nil, nil, false
		}
		return rest, nil, true
	}
	command := ""
	for _, tok := range rest {
		if tok != headedFlag {
			command = tok
			break
		}
	}
	if command == "" || command == "trace" {
		return rest, nil, true
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return rest, &traceRun{path: path, unredacted: unredacted, start: time.Now(), command: command}, true
}

// tracing reports whether the running command is traced: its requests
// then ask the helper for the trace facts.
func tracing() bool { return activeTrace != nil }

// observe records the command's exchange with the helper (the last one,
// for a command that sends several).
func (t *traceRun) observe(req *protocol.Request, resp *protocol.Response, err error) {
	if t == nil {
		return
	}
	t.req, t.resp, t.err = req, resp, err
}

// finish appends the command's line, with its exit code. A failure to
// write is reported on stderr and changes nothing else.
func (t *traceRun) finish(code int) {
	if t == nil {
		return
	}
	if err := appendTraceLine(t.path, t.line(code, time.Now())); err != nil {
		fmt.Fprintf(os.Stderr, "winuse: trace: %v\n", err)
	}
}

// line builds the command's trace line.
func (t *traceRun) line(code int, end time.Time) traceLine {
	l := traceLine{
		Time:       t.start.Format(traceTimeLayout),
		Command:    t.command,
		OK:         code == 0,
		Exit:       code,
		Unredacted: t.unredacted,
		DurationMS: end.Sub(t.start).Milliseconds(),
	}
	if t.req == nil {
		who := callingAgent()
		l.Agent, l.AgentLabel = who.ID, who.Label
		return l
	}
	l.Command, l.Agent, l.AgentLabel = t.req.Cmd, t.req.Agent, t.req.AgentLabel
	l.Args = t.req.Args
	if !t.unredacted {
		l.Args = redactArgs(t.req.Cmd, t.req.Args)
	}
	if t.err != nil {
		l.Error = t.err.Error()
	}
	if t.resp == nil {
		return l
	}
	if tr := t.resp.Trace; tr != nil {
		l.Window, l.Routes = tr.Window, tr.Routes
	}
	if !t.resp.OK {
		l.Error, l.Hint = t.resp.Err, t.resp.Hint
	}
	t.keepOut(&l, t.resp.Out)
	return l
}

// keepOut puts what the line keeps of the output into it: a Window
// State's line count, a redaction, or the text cut at traceOutCap; and
// the files the output names.
func (t *traceRun) keepOut(l *traceLine, out string) {
	if out == "" {
		return
	}
	l.OutLen = utf8.RuneCountInString(out)
	if t.req.Cmd == "get-window-state" {
		l.OutLines = strings.Count(strings.TrimSuffix(out, "\n"), "\n") + 1
		l.Refs = outRefs(out)
		return
	}
	if !t.unredacted && redactsOut(t.req.Cmd, t.req.Args) {
		l.Out = redact(out)
		return
	}
	l.Refs = outRefs(out)
	if len(out) > traceOutCap {
		cut := traceOutCap
		for cut > 0 && !utf8.RuneStart(out[cut]) {
			cut--
		}
		out, l.Truncated = out[:cut], true
	}
	l.Out = out
}

// redactedArgs are the wire arguments each command's redaction replaces:
// typed text, values, clipboard text, a prompt's answer, JavaScript.
var redactedArgs = map[string][]string{
	"type-text":     {"text"},
	"set-value":     {"value"},
	"clipboard-set": {"text"},
	"dialog":        {"text"},
	"eval":          {"expression"},
}

// redactArgs returns a command's wire arguments with redactedArgs
// replaced, a Batch's steps included. Arguments it cannot read are
// dropped whole rather than kept.
func redactArgs(cmd string, raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" || (redactedArgs[cmd] == nil && cmd != "batch") {
		return raw
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return redactedJSON(string(raw))
	}
	for _, key := range redactedArgs[cmd] {
		var s string
		if v, ok := fields[key]; ok && json.Unmarshal(v, &s) == nil {
			fields[key] = redactedJSON(s)
		}
	}
	if cmd == "batch" {
		var steps []protocol.Step
		if v, ok := fields["steps"]; ok && json.Unmarshal(v, &steps) == nil {
			for i := range steps {
				steps[i].Args = redactArgs(steps[i].Kind, steps[i].Args)
			}
			fields["steps"], _ = json.Marshal(steps)
		}
	}
	out, err := json.Marshal(fields)
	if err != nil {
		return redactedJSON(string(raw))
	}
	return out
}

func redactedJSON(s string) json.RawMessage {
	b, _ := json.Marshal(redact(s))
	return b
}

// redactsOut reports whether a command's output is redacted: eval's
// result, and clipboard-get's text or files (an image's path and size
// are kept).
func redactsOut(cmd string, raw json.RawMessage) bool {
	switch cmd {
	case "eval":
		return true
	case "clipboard-get":
		var a protocol.ClipboardGetArgs
		json.Unmarshal(raw, &a)
		return a.Format != "image"
	}
	return false
}

// screenshotLine and imagePath find the files an output names: a
// Screenshot: line's path, and any absolute path of an image.
var (
	screenshotLine = regexp.MustCompile(`(?m)^Screenshot: (.+?) \(`)
	imagePath      = regexp.MustCompile(`(?i)[a-z]:\\[^\r\n\t"<>|?*]*?\.(?:png|jpe?g|bmp|gif)\b`)
)

// outRefs returns the files an output names, each once, in order.
func outRefs(out string) []string {
	var refs []string
	add := func(p string) {
		for _, r := range refs {
			if r == p {
				return
			}
		}
		refs = append(refs, p)
	}
	for _, m := range screenshotLine.FindAllStringSubmatch(out, -1) {
		add(m[1])
	}
	for _, p := range imagePath.FindAllString(out, -1) {
		add(p)
	}
	return refs
}

// appendTraceLine appends one line to the trace file, rolling the file
// over first when the line would take it past traceMaxBytes. Each line is
// one write to a file opened for appending, so several winuse processes
// (several Agents) can share a trace.
func appendTraceLine(path string, l traceLine) error {
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if fi, err := os.Stat(path); err == nil && fi.Size() > 0 && fi.Size()+int64(len(b)) > traceMaxBytes {
		if err := rollTrace(path); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// rollTrace renames a full trace file to <name>.<n><ext>, the first free
// n. Another process that rolled it first leaves nothing to rename.
func rollTrace(path string) error {
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(path, ext)
	for n := 1; ; n++ {
		to := stem + "." + strconv.Itoa(n) + ext
		if _, err := os.Stat(to); err == nil {
			continue
		}
		err := os.Rename(path, to)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
}

const traceUsage = "usage: winuse trace show <file>"

// traceCmd implements `winuse trace show <file>`.
func traceCmd(args []string) int {
	if len(args) != 2 || args[0] != "show" {
		fmt.Fprintln(os.Stderr, traceUsage)
		return 2
	}
	f, err := os.Open(args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n", err)
		return 1
	}
	defer f.Close()
	if err := showTrace(os.Stdout, f); err != nil {
		fmt.Fprintf(os.Stderr, "winuse: reading %s: %v\n", args[1], err)
		return 1
	}
	return 0
}

// showTrace prints a trace readably: per command, a line with its time,
// command, Window, routes, outcome and duration, then its arguments,
// error, hint and files indented; at the end the totals. Lines that are
// not trace lines are counted and skipped.
func showTrace(w io.Writer, r io.Reader) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), protocol.MaxLine)
	var total, failed, skipped int
	var spent time.Duration
	for sc.Scan() {
		raw := sc.Bytes()
		if len(strings.TrimSpace(string(raw))) == 0 {
			continue
		}
		var l traceLine
		if json.Unmarshal(raw, &l) != nil || l.Command == "" {
			skipped++
			continue
		}
		total++
		if !l.OK {
			failed++
		}
		spent += time.Duration(l.DurationMS) * time.Millisecond
		fmt.Fprintln(w, showHead(l))
		if len(l.Args) > 0 && string(l.Args) != "null" {
			fmt.Fprintf(w, "  args: %s\n", l.Args)
		}
		if l.Error != "" {
			fmt.Fprintf(w, "  error: %s\n", l.Error)
		}
		if l.Hint != "" {
			fmt.Fprintf(w, "  hint: %s\n", l.Hint)
		}
		for _, ref := range l.Refs {
			fmt.Fprintf(w, "  file: %s\n", ref)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	fmt.Fprintf(w, "%d commands, %d failed, %s in all", total, failed, spent.Round(time.Millisecond))
	if skipped > 0 {
		fmt.Fprintf(w, "; %d lines were not trace lines", skipped)
	}
	fmt.Fprintln(w)
	return nil
}

// showHead is a command's first line in trace show.
func showHead(l traceLine) string {
	parts := []string{l.Time}
	if t, err := time.Parse(traceTimeLayout, l.Time); err == nil {
		parts[0] = t.Format("2006-01-02 15:04:05.000")
	}
	parts = append(parts, l.Command)
	if l.AgentLabel != "" {
		parts = append(parts, "["+l.AgentLabel+"]")
	}
	if l.Window != nil {
		parts = append(parts, fmt.Sprintf("%d %s %q", l.Window.ID, l.Window.App, l.Window.Title))
	}
	if len(l.Routes) > 0 {
		parts = append(parts, strings.Join(l.Routes, ","))
	}
	outcome := "ok"
	if !l.OK {
		outcome = "exit " + strconv.Itoa(l.Exit)
	}
	parts = append(parts, outcome, (time.Duration(l.DurationMS) * time.Millisecond).String())
	if l.Unredacted {
		parts = append(parts, "(unredacted)")
	}
	return strings.Join(parts, "  ")
}
