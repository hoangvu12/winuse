//go:build e2e

// Session traces (desktop-parity ticket 19): the real binary with --trace
// and WINUSE_TRACE against the fixture. A Window State read is kept as its
// line count and screenshot path, a Headless set-value's value is
// redacted, a refusal keeps its error, the helper names the Window and
// the route, and `winuse trace show` summarizes the file. The trace lives
// in the test's temp folder. User-run only (see the package comment and
// test/e2e/README.md); an agent must never start them.

package e2e

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
)

// traceEntry is the part of a trace line the test reads.
type traceEntry struct {
	Command string `json:"command"`
	Window  *struct {
		ID    uint64 `json:"id"`
		App   string `json:"app"`
		Title string `json:"title"`
	} `json:"window"`
	Args     json.RawMessage `json:"args"`
	Routes   []string        `json:"routes"`
	OK       bool            `json:"ok"`
	Exit     int             `json:"exit"`
	Error    string          `json:"error"`
	Out      json.RawMessage `json:"out"`
	OutLines int             `json:"out_lines"`
	Refs     []string        `json:"refs"`
}

func readTrace(t *testing.T, path string) ([]traceEntry, string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the trace: %v", err)
	}
	var out []traceEntry
	sc := bufio.NewScanner(strings.NewReader(string(raw)))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var e traceEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("trace line %q: %v", sc.Text(), err)
		}
		out = append(out, e)
	}
	return out, string(raw)
}

func TestTraceRecordsRedactedLines(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	path := filepath.Join(t.TempDir(), "session.jsonl")
	idle := "WINUSE_HELPER_IDLE=" + e2eIdle
	const secret = "hunter2 trace"

	// --trace before the command, and WINUSE_TRACE for the others.
	if _, stderr, exit := runWinuse(t, []string{"--trace", path, "get-window-state", window, "--screenshot"}, idle); exit != 0 {
		t.Fatalf("get-window-state --screenshot: exit %d, stderr:\n%s", exit, stderr)
	}
	field := strconv.Itoa(mainElementIndexes()[fixture.ElemTextField])
	stdout, stderr, exit := runWinuse(t, []string{"set-value", window, field, secret}, idle, "WINUSE_TRACE="+path)
	assertActionOK(t, "traced set-value", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Text == secret })
	if _, _, exit := runWinuse(t, []string{"click", window, "9999"}, idle, "WINUSE_TRACE="+path); exit == 0 {
		t.Fatal("click on element 9999 succeeded, want a refusal")
	}

	lines, raw := readTrace(t, path)
	if len(lines) != 3 {
		t.Fatalf("trace has %d lines, want 3:\n%s", len(lines), raw)
	}
	if strings.Contains(raw, "hunter2") {
		t.Errorf("the trace holds the set-value value:\n%s", raw)
	}
	id, _ := strconv.ParseUint(window, 10, 64)
	read, set, click := lines[0], lines[1], lines[2]
	if read.Command != "get-window-state" || !read.OK || read.OutLines < 5 || len(read.Out) != 0 ||
		len(read.Refs) != 1 || filepath.Ext(read.Refs[0]) == "" || !strings.Contains(read.Refs[0], "winuse-screenshot-") {
		t.Errorf("get-window-state line = %+v, want its line count and one screenshot path, no tree", read)
	}
	if read.Window == nil || read.Window.ID != id || read.Window.Title == "" {
		t.Errorf("get-window-state line names Window %+v, want %d with its title", read.Window, id)
	}
	if set.Command != "set-value" || !set.OK || strings.Join(set.Routes, ",") != "Headless" ||
		!strings.Contains(string(set.Args), `"value":{"redacted":true,"len":13}`) {
		t.Errorf("set-value line = %+v (args %s), want Headless and the value redacted", set, set.Args)
	}
	if click.Command != "click" || click.OK || click.Exit == 0 || click.Error == "" {
		t.Errorf("refused click line = %+v, want its exit and error", click)
	}

	stdout, stderr, exit = runWinuse(t, []string{"trace", "show", path})
	if exit != 0 || !strings.Contains(stdout, "3 commands, 1 failed") || !strings.Contains(stdout, "set-value") ||
		!strings.Contains(stdout, "  error: ") {
		t.Errorf("trace show: exit %d, stderr %q, stdout:\n%s", exit, stderr, stdout)
	}
	if again, _ := readTrace(t, path); len(again) != 3 {
		t.Errorf("trace show added lines to the trace: %d, want 3", len(again))
	}
}
