package cli

// Session traces (desktop-parity ticket 19): the trace flags, the line a
// command writes (with the helper's Window and routes), the redaction
// rules, the output cap, the roll-over and trace show. No helper runs:
// the exchange is handed to the trace as runRequest would.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/agent"
	"github.com/hoangvu12/winuse/internal/protocol"
)

func noEnv(string) string { return "" }

func TestStartTraceFlags(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "t.jsonl")
	env := func(k string) string {
		if k == traceEnv {
			return filepath.Join(dir, "env.jsonl")
		}
		return ""
	}
	cases := []struct {
		name       string
		args       []string
		getenv     func(string) string
		rest       []string
		path       string // "" means not traced
		unredacted bool
		ok         bool
	}{
		{"flag", []string{"--trace", file, "click", "1", "2"}, noEnv, []string{"click", "1", "2"}, file, false, true},
		{"inline, among --headed", []string{"--headed", "--trace=" + file, "--trace-unredacted", "click"}, noEnv, []string{"--headed", "click"}, file, true, true},
		{"environment", []string{"click", "1"}, env, []string{"click", "1"}, filepath.Join(dir, "env.jsonl"), false, true},
		{"the flag beats the environment", []string{"--trace", file, "list-windows"}, env, []string{"list-windows"}, file, false, true},
		{"after the command it is the command's", []string{"type-text", "1", "--trace", file}, noEnv, []string{"type-text", "1", "--trace", file}, "", false, true},
		{"trace show is not traced", []string{"trace", "show", file}, env, []string{"trace", "show", file}, "", false, true},
		{"no tracing", []string{"list-windows"}, noEnv, []string{"list-windows"}, "", false, true},
		{"--trace without a file", []string{"--trace"}, noEnv, nil, "", false, false},
		{"--trace with an empty file", []string{"--trace=", "click"}, noEnv, nil, "", false, false},
		{"--trace-unredacted alone", []string{"--trace-unredacted", "click"}, noEnv, nil, "", false, false},
		{"--trace-unredacted with the environment", []string{"--trace-unredacted", "click"}, env, []string{"click"}, filepath.Join(dir, "env.jsonl"), true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rest, run, ok := startTrace(c.args, c.getenv)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v", ok, c.ok)
			}
			if !ok {
				return
			}
			if !reflect.DeepEqual(rest, c.rest) {
				t.Errorf("rest = %q, want %q", rest, c.rest)
			}
			switch {
			case c.path == "" && run != nil:
				t.Errorf("traced to %s, want no trace", run.path)
			case c.path != "" && run == nil:
				t.Errorf("not traced, want %s", c.path)
			case run != nil && (run.path != c.path || run.unredacted != c.unredacted):
				t.Errorf("trace = %s unredacted %v, want %s unredacted %v", run.path, run.unredacted, c.path, c.unredacted)
			}
		})
	}
}

// tracedLine runs one exchange through a trace and returns the line it
// writes, as JSON.
func tracedLine(t *testing.T, unredacted bool, cmd string, args any, resp *protocol.Response, err error, code int) string {
	t.Helper()
	withCallingAgent(t, agent.Identity{ID: "name:builder", Label: "builder"})
	req, rerr := Request(cmd, args)
	if rerr != nil {
		t.Fatal(rerr)
	}
	run := &traceRun{path: "unused", unredacted: unredacted, start: time.Date(2026, 11, 3, 14, 2, 11, 120e6, time.UTC), command: cmd}
	run.observe(req, resp, err)
	raw, merr := json.Marshal(run.line(code, run.start.Add(84*time.Millisecond)))
	if merr != nil {
		t.Fatal(merr)
	}
	return string(raw)
}

func TestTraceLineCarriesTheHelpersWindowAndRoutes(t *testing.T) {
	resp := &protocol.Response{OK: true, Out: "ok\n", Trace: &protocol.Trace{
		Window: &protocol.TraceWindow{ID: 101, App: "Notepad", Title: "Untitled - Notepad"},
		Routes: []string{protocol.RouteHeadless},
	}}
	got := tracedLine(t, false, "click", map[string]any{"window": "101", "element_index": 12}, resp, nil, 0)
	want := `{"time":"2026-11-03T14:02:11.120Z","command":"click","agent":"name:builder","agent_label":"builder",` +
		`"window":{"id":101,"app":"Notepad","title":"Untitled - Notepad"},"args":{"element_index":12,"window":"101"},` +
		`"routes":["Headless"],"ok":true,"exit":0,"out":"ok\n","out_len":3,"duration_ms":84}`
	if got != want {
		t.Errorf("line:\n got %s\nwant %s", got, want)
	}
}

func TestTraceRedactsByDefault(t *testing.T) {
	text := "hunter2 é"
	cases := []struct {
		cmd  string
		args any
		want string // a fragment of the redacted args
	}{
		{"type-text", protocol.TypeTextArgs{Window: "101", Text: text}, `"text":{"redacted":true,"len":9}`},
		{"set-value", protocol.SetValueArgs{Window: "101", Value: text}, `"value":{"redacted":true,"len":9}`},
		{"clipboard-set", protocol.ClipboardSetArgs{Text: &text}, `"text":{"redacted":true,"len":9}`},
		{"dialog", protocol.DialogArgs{Window: "101", Action: "accept", Text: &text}, `"text":{"redacted":true,"len":9}`},
		{"eval", protocol.EvalArgs{Window: "101", Expression: "hunter2 é"}, `"expression":{"redacted":true,"len":9}`},
		{"batch", protocol.BatchArgs{Window: "101", Steps: []protocol.Step{
			{Kind: "click", Args: json.RawMessage(`{"element_index":3}`)},
			{Kind: "type-text", Args: json.RawMessage(`{"text":"hunter2 é"}`)},
			{Kind: "set-value", Args: json.RawMessage(`{"element_index":4,"value":"hunter2 é"}`)},
		}}, `{"kind":"type-text","args":{"text":{"redacted":true,"len":9}}},{"kind":"set-value","args":{"element_index":4,"value":{"redacted":true,"len":9}}}`},
	}
	for _, c := range cases {
		t.Run(c.cmd, func(t *testing.T) {
			got := tracedLine(t, false, c.cmd, c.args, &protocol.Response{OK: true, Out: "ok\n"}, nil, 0)
			if strings.Contains(got, "hunter2") || !strings.Contains(got, c.want) {
				t.Errorf("redacted line = %s, want %s and no secret", got, c.want)
			}
			got = tracedLine(t, true, c.cmd, c.args, &protocol.Response{OK: true, Out: "ok\n"}, nil, 0)
			if !strings.Contains(got, "hunter2") || !strings.Contains(got, `"unredacted":true`) {
				t.Errorf("--trace-unredacted line = %s, want the text kept and marked", got)
			}
		})
	}
}

func TestTraceOutput(t *testing.T) {
	tree := "Window: 101 Notepad\n0 window Untitled - Notepad\n  1 edit Password secret\n\nScreenshot: C:\\Users\\me\\AppData\\Local\\Temp\\winuse\\shot 1.png (800x470 px, scale 1, window at 10,20 800x470)\n"
	long := strings.Repeat("a", traceOutCap-1) + "é" + "tail"
	cases := []struct {
		name       string
		unredacted bool
		cmd        string
		args       any
		out        string
		want       []string // fragments of the line
		not        []string
	}{
		{"eval result", false, "eval", protocol.EvalArgs{Window: "101", Expression: "1"}, `"the secret"` + "\n",
			[]string{`"out":{"redacted":true,"len":13}`, `"out_len":13`}, []string{"the secret"}},
		{"eval result, unredacted", true, "eval", protocol.EvalArgs{Window: "101", Expression: "1"}, `"the secret"` + "\n",
			[]string{`the secret`}, nil},
		{"clipboard text", false, "clipboard-get", protocol.ClipboardGetArgs{}, "the secret\n",
			[]string{`"out":{"redacted":true,"len":11}`}, []string{"the secret"}},
		{"clipboard files", false, "clipboard-get", protocol.ClipboardGetArgs{Format: "files"}, "C:\\secret\\a.png\n",
			[]string{`"redacted":true`}, []string{"secret", "refs"}},
		{"clipboard image is a path", false, "clipboard-get", protocol.ClipboardGetArgs{Format: "image"}, "C:\\Temp\\winuse\\winuse-clipboard-1.png (10x10 px)\n",
			[]string{`"refs":["C:\\Temp\\winuse\\winuse-clipboard-1.png"]`, `"out":"C:\\Temp`}, nil},
		{"a Window State keeps its line count and screenshot", true, "get-window-state", map[string]any{"window": "101", "screenshot": true}, tree,
			[]string{`"out_lines":5`, `"refs":["C:\\Users\\me\\AppData\\Local\\Temp\\winuse\\shot 1.png"]`}, []string{"Password", `"out":`}},
		{"long output is cut at the cap", false, "list-apps", nil, long,
			[]string{`"truncated":true`, `"out_len":4100`}, []string{"tail", "é"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := tracedLine(t, c.unredacted, c.cmd, c.args, &protocol.Response{OK: true, Out: c.out}, nil, 0)
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Errorf("line = %s\nwant %s", got, w)
				}
			}
			for _, n := range c.not {
				if strings.Contains(got, n) {
					t.Errorf("line = %s\nwant no %s", got, n)
				}
			}
		})
	}
	var l traceLine
	json.Unmarshal([]byte(tracedLine(t, false, "list-apps", nil, &protocol.Response{OK: true, Out: long}, nil, 0)), &l)
	if s, _ := l.Out.(string); len(s) != traceOutCap-1 {
		t.Errorf("cut output is %d bytes, want %d (the cap, back to a character's start)", len(s), traceOutCap-1)
	}
}

func TestTraceFailures(t *testing.T) {
	got := tracedLine(t, false, "click", map[string]any{"window": "101"},
		&protocol.Response{Err: "window 101 is gone; nothing was sent", Hint: "run winuse list-windows", Exit: 1}, nil, 1)
	if !strings.Contains(got, `"ok":false,"exit":1,"error":"window 101 is gone; nothing was sent","hint":"run winuse list-windows"`) {
		t.Errorf("helper failure line = %s", got)
	}
	got = tracedLine(t, false, "click", map[string]any{"window": "101"}, nil, &TimeoutError{Cmd: "click"}, 1)
	if !strings.Contains(got, `"ok":false,"exit":1,"error":"winuse request timed out: click"`) {
		t.Errorf("timeout line = %s", got)
	}
	// A usage error sends nothing: the line has the command and the exit.
	withCallingAgent(t, agent.Identity{ID: "name:builder", Label: "builder"})
	run := &traceRun{start: time.Now(), command: "click"}
	l := run.line(2, run.start)
	if l.Command != "click" || l.OK || l.Exit != 2 || l.Args != nil || l.Agent != "name:builder" {
		t.Errorf("usage error line = %+v", l)
	}
}

func TestTraceAppendsAndRollsOver(t *testing.T) {
	withCallingAgent(t, agent.Identity{ID: "name:builder", Label: "builder"})
	start := time.Now() // finish ends the line now: its duration stays a digit or two
	run := &traceRun{command: "list-windows", start: start}
	one, _ := json.Marshal(run.line(0, start))
	old := traceMaxBytes
	traceMaxBytes = int64(len(one)+1)*2 + 10 // two lines fit, the third rolls the file
	t.Cleanup(func() { traceMaxBytes = old })
	path := filepath.Join(t.TempDir(), "sub", "trace.jsonl")
	run.path = path
	for i := 0; i < 3; i++ {
		run.finish(0)
	}
	rolled := filepath.Join(filepath.Dir(path), "trace.1.jsonl")
	if b, err := os.ReadFile(rolled); err != nil || strings.Count(string(b), "\n") != 2 {
		t.Fatalf("rolled file = %q, %v; want the first two lines", b, err)
	}
	if b, err := os.ReadFile(path); err != nil || strings.Count(string(b), "\n") != 1 {
		t.Fatalf("trace file = %q, %v; want one new line", b, err)
	}
	for i := 0; i < 2; i++ {
		run.finish(0)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "trace.2.jsonl")); err != nil {
		t.Errorf("second roll-over: %v; want trace.2.jsonl beside trace.1.jsonl", err)
	}
}

func TestTraceShow(t *testing.T) {
	var file bytes.Buffer
	lines := []traceLine{
		{Time: "2026-11-03T14:02:11.120+07:00", Command: "get-window-state", AgentLabel: "claude 17048",
			Window: &protocol.TraceWindow{ID: 101, App: "Notepad", Title: "Untitled - Notepad"}, Args: json.RawMessage(`{"window":"101"}`),
			OK: true, OutLines: 40, Refs: []string{`C:\Temp\shot.png`}, DurationMS: 812},
		{Time: "2026-11-03T14:02:12.000+07:00", Command: "click", Routes: []string{"Headless"},
			Window: &protocol.TraceWindow{ID: 101, App: "Notepad", Title: "Untitled - Notepad"},
			Exit: 1, Error: "element 12 is stale; nothing was sent", Hint: "read the Window State again", DurationMS: 40, Unredacted: true},
	}
	for _, l := range lines {
		b, _ := json.Marshal(l)
		file.Write(append(b, '\n'))
	}
	file.WriteString("not json\n\n")
	var out bytes.Buffer
	if err := showTrace(&out, &file); err != nil {
		t.Fatal(err)
	}
	want := `2026-11-03 14:02:11.120  get-window-state  [claude 17048]  101 Notepad "Untitled - Notepad"  ok  812ms
  args: {"window":"101"}
  file: C:\Temp\shot.png
2026-11-03 14:02:12.000  click  101 Notepad "Untitled - Notepad"  Headless  exit 1  40ms  (unredacted)
  error: element 12 is stale; nothing was sent
  hint: read the Window State again
2 commands, 1 failed, 852ms in all; 1 lines were not trace lines
`
	if out.String() != want {
		t.Errorf("trace show:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestTraceShowUsage(t *testing.T) {
	if code := traceCmd([]string{"show"}); code != 2 {
		t.Errorf("trace show without a file = %d, want 2", code)
	}
	if code := traceCmd([]string{"show", filepath.Join(t.TempDir(), "missing.jsonl")}); code != 1 {
		t.Errorf("trace show of a missing file = %d, want 1", code)
	}
	for _, want := range []string{"--trace <file>", "--trace-unredacted", "WINUSE_TRACE", "trace show <file>"} {
		if !strings.Contains(Usage, want) {
			t.Errorf("usage text missing %q", want)
		}
	}
}
