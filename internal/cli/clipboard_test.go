package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestParseClipboardGetArgs(t *testing.T) {
	abs, _ := filepath.Abs("clip.png")
	cases := []struct {
		args []string
		want protocol.ClipboardGetArgs
	}{
		{nil, protocol.ClipboardGetArgs{}},
		{[]string{"--format", "files"}, protocol.ClipboardGetArgs{Format: "files"}},
		{[]string{"--format=text"}, protocol.ClipboardGetArgs{Format: "text"}},
		{[]string{"--format", "image", "--out", "clip.png"}, protocol.ClipboardGetArgs{Format: "image", Out: abs}},
		{[]string{"--out=clip.png", "--format=image"}, protocol.ClipboardGetArgs{Format: "image", Out: abs}},
	}
	for _, tc := range cases {
		got, err := parseClipboardGetArgs(tc.args)
		if err != nil || got != tc.want {
			t.Errorf("%q = %+v, %v; want %+v", tc.args, got, err, tc.want)
		}
	}
	for _, bad := range [][]string{
		{"--format", "html"},
		{"--format"},
		{"--format", "text", "--format", "image"},
		{"--out", "x.png"},
		{"--format", "text", "--out", "x.png"},
		{"--format", "image", "--out", ""},
		{"extra"},
	} {
		if _, err := parseClipboardGetArgs(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestParseClipboardSetArgs(t *testing.T) {
	text := func(s string) protocol.ClipboardSetArgs { return protocol.ClipboardSetArgs{Text: &s} }
	png, _ := filepath.Abs("a.png")
	a, _ := filepath.Abs("a.txt")
	b, _ := filepath.Abs(`dir\b c.txt`)
	cases := []struct {
		args []string
		want protocol.ClipboardSetArgs
	}{
		{[]string{"hello", "world"}, text("hello world")},
		{[]string{""}, text("")},
		{[]string{"--format", "--headed"}, text("--format --headed")},
		{[]string{"--image", "a.png"}, protocol.ClipboardSetArgs{Image: png}},
		{[]string{"--file", "a.txt", `dir\b c.txt`}, protocol.ClipboardSetArgs{Files: []string{a, b}}},
		{[]string{"--file", "a.txt", "--file", `dir\b c.txt`}, protocol.ClipboardSetArgs{Files: []string{a, b}}},
	}
	for _, tc := range cases {
		got, err := parseClipboardSetArgs(tc.args)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q = %+v, %v; want %+v", tc.args, got, err, tc.want)
		}
	}
	for _, bad := range [][]string{
		nil,
		{"--image"},
		{"--image", "a.png", "b.png"},
		{"--file"},
		{"--file", "--file"},
	} {
		if _, err := parseClipboardSetArgs(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

// TestClipboardSetKeepsALiteralHeaded: --headed in clipboard-set's text is
// text; before the command it is the global flag, ignored.
func TestClipboardSetKeepsALiteralHeaded(t *testing.T) {
	rest, headed := extractHeaded([]string{"clipboard-set", "run", "--headed", "now"})
	if headed || !reflect.DeepEqual(rest, []string{"clipboard-set", "run", "--headed", "now"}) {
		t.Errorf("in the text: %q, headed %v", rest, headed)
	}
	rest, headed = extractHeaded([]string{"--headed", "clipboard-set", "x"})
	if !headed || !reflect.DeepEqual(rest, []string{"clipboard-set", "x"}) {
		t.Errorf("before the command: %q, headed %v", rest, headed)
	}
}

// TestClipboardUsageErrorsExitTwo: usage failures never dial the helper.
func TestClipboardUsageErrorsExitTwo(t *testing.T) {
	for _, args := range [][]string{
		{"clipboard-get", "--format", "rtf"},
		{"clipboard-set"},
		{"clipboard-clear", "now"},
	} {
		if code := clipboardUsageExit(t, args); code != 2 {
			t.Errorf("%q: exit %d, want 2", args, code)
		}
	}
}

func clipboardUsageExit(t *testing.T, args []string) int {
	t.Helper()
	var code int
	stderr := captureStderr(t, func() { code = Main(args) })
	if !strings.Contains(stderr, "usage: winuse "+args[0]) {
		t.Errorf("%q: stderr %q, want the usage line", args, stderr)
	}
	return code
}

// captureStderr runs fn with stdout and stderr sent to a file, as
// TestMainUsageErrors does, and returns what it printed.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stderr")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create stderr file: %v", err)
	}
	oldStderr, oldStdout := os.Stderr, os.Stdout
	os.Stderr, os.Stdout = f, f
	fn()
	os.Stderr, os.Stdout = oldStderr, oldStdout
	f.Close()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	return string(got)
}

func TestUsageDocumentsTheClipboard(t *testing.T) {
	for _, cmd := range []string{"clipboard-get", "clipboard-set", "clipboard-clear"} {
		if !strings.Contains(Usage, "\n  "+cmd) {
			t.Errorf("usage text does not document %q", cmd)
		}
	}
}
