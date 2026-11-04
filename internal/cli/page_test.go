package cli

// The page-only Actions' CLI parsing (browser merge ticket 06): each
// command's arguments, upload's absolute paths and optional element,
// wait's --timeout cap, hover and drag's Page Element forms, the Batch
// steps, usage errors exiting 2 without dialing, and the budgets.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestParsePageArgs(t *testing.T) {
	cwd, _ := os.Getwd()
	three := 3
	text := "Ada Lovelace"
	empty := ""
	cases := []struct {
		cmd  string
		args []string
		want any
	}{
		{"navigate", []string{"example.com"}, protocol.NavigateArgs{URL: "example.com"}},
		{"navigate", []string{"--wait", "none", "https://a.test/"}, protocol.NavigateArgs{URL: "https://a.test/", Wait: "none"}},
		{"navigate", []string{"https://a.test/", "--wait=domcontentloaded"}, protocol.NavigateArgs{URL: "https://a.test/", Wait: "domcontentloaded"}},
		{"back", nil, protocol.NavigateArgs{}},
		{"reload", []string{"--wait", "load"}, protocol.NavigateArgs{Wait: "load"}},
		{"upload", []string{"a.pdf", `C:\x\b.png`}, protocol.UploadArgs{Files: []string{filepath.Join(cwd, "a.pdf"), `C:\x\b.png`}}},
		{"upload", []string{"3", "a.pdf"}, protocol.UploadArgs{ElementIndex: &three, Files: []string{filepath.Join(cwd, "a.pdf")}}},
		{"upload", []string{"name:Resume", `.\3`}, protocol.UploadArgs{Ref: "name:Resume", Files: []string{filepath.Join(cwd, "3")}}},
		{"dialog", []string{"accept"}, protocol.DialogArgs{Action: "accept"}},
		{"dialog", []string{"Accept", "Ada", "Lovelace"}, protocol.DialogArgs{Action: "accept", Text: &text}},
		{"dialog", []string{"accept", ""}, protocol.DialogArgs{Action: "accept", Text: &empty}},
		{"wait", []string{"selector", "div", ">", "a.done"}, protocol.WaitArgs{For: "selector", Value: "div > a.done"}},
		{"wait", []string{"text", "Thank you", "--timeout", "2.5"}, protocol.WaitArgs{For: "text", Value: "Thank you", TimeoutMS: 2500}},
		{"wait", []string{"--timeout=20", "url", "*/done"}, protocol.WaitArgs{For: "url", Value: "*/done", TimeoutMS: 20000}},
		{"wait", []string{"load"}, protocol.WaitArgs{For: "load"}},
		{"wait", []string{"load", "domcontentloaded"}, protocol.WaitArgs{For: "load", Value: "domcontentloaded"}},
		{"wait", []string{"download"}, protocol.WaitArgs{For: "download"}},
		{"eval", []string{"document.title", "+", "'!'"}, protocol.EvalArgs{Expression: "document.title + '!'"}},
	}
	for _, c := range cases {
		got, err := parsePageArgs(c.cmd, c.args)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s %q: %#v %v, want %#v", c.cmd, c.args, got, err, c.want)
		}
	}
}

func TestParsePageArgsRefusals(t *testing.T) {
	cases := []struct {
		cmd  string
		args []string
		want string
	}{
		{"navigate", nil, "navigate takes one URL"},
		{"navigate", []string{"a", "b"}, "navigate takes one URL"},
		{"navigate", []string{"a", "--wait", "idle"}, "--wait must be load, domcontentloaded or none"},
		{"navigate", []string{"a", "--wait"}, "--wait needs"},
		{"back", []string{"https://a.test/"}, "back takes no other arguments"},
		{"forward", []string{"--fast"}, "unknown option --fast"},
		{"upload", nil, "upload needs at least one file"},
		{"upload", []string{"3"}, "upload needs at least one file"},
		{"upload", []string{"a.pdf", "--all"}, "upload has no option --all"},
		{"dialog", nil, "dialog takes accept or dismiss"},
		{"dialog", []string{"ok"}, `dialog takes accept or dismiss, got "ok"`},
		{"dialog", []string{"dismiss", "x"}, "text answers a prompt, so it goes with accept"},
		{"wait", nil, "wait needs what to wait for"},
		{"wait", []string{"selector"}, "wait selector needs a value"},
		{"wait", []string{"idle"}, `not "idle"`},
		{"wait", []string{"download", "x"}, "wait download takes no value"},
		{"wait", []string{"text", "x", "--timeout", "301"}, "at most 300"},
		{"wait", []string{"text", "x", "--timeout", "0"}, "above 0"},
		{"eval", nil, "eval needs an expression"},
	}
	for _, c := range cases {
		if _, err := parsePageArgs(c.cmd, c.args); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %q: %v, want %q", c.cmd, c.args, err, c.want)
		}
	}
}

// TestParseEvalArgs (browser-ux 06): --max raises the printed cut, and
// --file reads the expression (quotes and all, a BOM dropped) where no
// shell quoting reaches it.
func TestParseEvalArgs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.js")
	js := "[...document.querySelectorAll(\"a[href*='t.co']\")].map(a => a.href).join(\"\\n\")"
	if err := os.WriteFile(path, []byte("\uFEFF"+js), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := parseEvalArgs([]string{"--file", path, "--max", "20000"}, readEvalFile)
	if err != nil || a.Expression != js || a.Max != 20000 {
		t.Errorf("--file --max: %+v %v", a, err)
	}
	a, err = parseEvalArgs([]string{"document.title", "--max", "100000"}, readEvalFile)
	if err != nil || a.Expression != "document.title" || a.Max != 100000 {
		t.Errorf("--max after the expression: %+v %v", a, err)
	}
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--file"}, "--file needs a path"},
		{[]string{"x", "--max"}, "--max needs a number"},
		{[]string{"x", "--max", "100001"}, "--max takes 1 to 100000"},
		{[]string{"x", "--max", "0"}, "--max takes 1 to 100000"},
		{[]string{"--file", path, "1+1"}, "not both"},
		{[]string{"--file", filepath.Join(dir, "missing.js")}, "reading the expression from"},
		{[]string{"--max", "10"}, "eval needs an expression"},
	} {
		if _, err := parseEvalArgs(c.args, readEvalFile); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: %v, want %q", c.args, err, c.want)
		}
	}
}

func TestParseHoverAndDragPageForms(t *testing.T) {
	seven, eight := 7, 8
	h, err := parseHoverTokens([]string{"7"})
	if err != nil || h.ElementIndex == nil || *h.ElementIndex != 7 {
		t.Errorf("hover element: %+v %v", h, err)
	}
	if h, err = parseHoverTokens([]string{"@3,4"}); err != nil || h.X != 3 || h.Y != 4 || h.ElementIndex != nil {
		t.Errorf("hover point: %+v %v", h, err)
	}
	if _, err = parseHoverTokens([]string{"name:Big", "Button"}); err == nil || !strings.Contains(err.Error(), "quote it") {
		t.Errorf("unquoted name: %v", err)
	}
	cases := []struct {
		args []string
		want protocol.DragArgs
	}{
		{[]string{"7", "8"}, protocol.DragArgs{FromIndex: &seven, ToIndex: &eight}},
		{[]string{"name:Card", "@40,50"}, protocol.DragArgs{FromRef: "name:Card", ToX: 40, ToY: 50}},
		{[]string{"7", "id:lane-2"}, protocol.DragArgs{FromIndex: &seven, ToRef: "id:lane-2"}},
		{[]string{"@1,2", "@3,4"}, protocol.DragArgs{FromX: 1, FromY: 2, ToX: 3, ToY: 4}},
	}
	for _, c := range cases {
		got, err := parseDragTokens(c.args)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("drag %q: %+v %v", c.args, got, err)
		}
	}
	if _, err := parseDragTokens([]string{"@1,2", "7"}); err == nil {
		t.Error("a point to an element was accepted")
	}
}

func TestBatchPageSteps(t *testing.T) {
	_, steps, err := parseBatchArgs([]string{"301", "navigate", "a.test", "then", "wait", "text", "Done", "then", "hover", "7",
		"then", "drag", "7", "8", "then", "dialog", "accept", "then", "eval", "1+1", "then", "back"})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, s := range steps {
		kinds = append(kinds, s.Kind)
	}
	if want := []string{"navigate", "wait", "hover", "drag", "dialog", "eval", "back"}; !reflect.DeepEqual(kinds, want) {
		t.Errorf("kinds %v", kinds)
	}
	var nav protocol.NavigateArgs
	json.Unmarshal(steps[0].Args, &nav)
	if nav.URL != "a.test" || nav.Window != "" {
		t.Errorf("navigate step %s", steps[0].Args)
	}
	if string(steps[2].Args) != `{"window":"","x":0,"y":0,"element_index":7}` {
		t.Errorf("hover step %s", steps[2].Args)
	}
	if _, _, err := parseBatchArgs([]string{"301", "wait"}); err == nil || !strings.Contains(err.Error(), "a batch wait Action: wait needs what to wait for") {
		t.Errorf("bad step: %v", err)
	}
}

func TestPageCommandUsageErrorsExit2(t *testing.T) {
	for _, args := range [][]string{{"navigate"}, {"navigate", "301"}, {"back", "301", "x"}, {"dialog", "301", "maybe"}, {"wait", "301", "idle"}, {"eval", "301"}, {"upload", "301"}} {
		path := filepath.Join(t.TempDir(), "stderr")
		f, _ := os.Create(path)
		oldErr, oldOut := os.Stderr, os.Stdout
		os.Stderr, os.Stdout = f, f
		code := Main(args)
		os.Stderr, os.Stdout = oldErr, oldOut
		f.Close()
		got, _ := os.ReadFile(path)
		if code != 2 || !strings.Contains(string(got), protocol.PageUsage(args[0])) {
			t.Errorf("%q: exit %d, %q", args, code, got)
		}
	}
}

// TestEveryPageCommandIsDispatched: the CLI's dispatch and Batch step
// switches name the page-only Actions one by one; each must reach the page
// parsers (protocol.PageCommands is the list).
func TestEveryPageCommandIsDispatched(t *testing.T) {
	for _, cmd := range protocol.PageCommands {
		path := filepath.Join(t.TempDir(), "stderr")
		f, _ := os.Create(path)
		oldErr, oldOut := os.Stderr, os.Stdout
		os.Stderr, os.Stdout = f, f
		code := Main([]string{cmd})
		os.Stderr, os.Stdout = oldErr, oldOut
		f.Close()
		got, _ := os.ReadFile(path)
		want := cmd + " takes a window reference first"
		if cmd == "wait" {
			want = "wait takes a window reference, window <app or title>, or a number of milliseconds first" // wait.go: the desktop waits
		}
		if code != 2 || !strings.Contains(string(got), want) {
			t.Errorf("%s: exit %d, %q", cmd, code, got)
		}
		if _, err := parseBatchStep([]string{cmd}); err != nil && strings.Contains(err.Error(), "unknown batch action") {
			t.Errorf("batch %s: %v", cmd, err)
		}
	}
}

func TestPageCommandBudgets(t *testing.T) {
	for _, cmd := range protocol.PageCommands {
		if d, ok := protocol.CommandTimeout(cmd); !ok || d != protocol.BrowserTimeout {
			t.Errorf("%s budget %v", cmd, d)
		}
	}
	for _, cmd := range protocol.PageCommands {
		if !strings.Contains(Usage, "\n  "+cmd+" <window>") {
			t.Errorf("usage misses %s", cmd)
		}
	}
}
