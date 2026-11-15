package cli

// Offline tests for --stdin and --file (textsource.go, learn-from 06):
// the text comes from stdin or a file, decoded and with one trailing
// newline dropped; the flags around it; the refusals; Batches take
// neither. None of these reaches the helper.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestDecodeText(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{"plain", []byte(`say "hi"`), `say "hi"`},
		{"empty", nil, ""},
		{"one newline dropped", []byte("a\n"), "a"},
		{"one CRLF dropped", []byte("a\r\n"), "a"},
		{"only one", []byte("a\n\n"), "a\n"},
		{"only one CRLF", []byte("a\r\n\r\n"), "a\r\n"},
		{"inner newlines kept", []byte("a\nb\n"), "a\nb"},
		{"a lone CR kept", []byte("a\r"), "a\r"},
		{"just a newline", []byte("\r\n"), ""},
		{"UTF-8 BOM dropped", []byte("\xEF\xBB\xBFnaïve 🙂\n"), "naïve 🙂"},
		{"UTF-16 LE", []byte{0xFF, 0xFE, 'h', 0, 0xEF, 0, 0x3D, 0xD8, 0x42, 0xDE, '\r', 0, '\n', 0}, "hï🙂"},
		{"UTF-16 BE", []byte{0xFE, 0xFF, 0, 'o', 0, 'k', 0, '\n'}, "ok"},
	}
	for _, tc := range cases {
		got, err := decodeText(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("%s: decodeText = %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
	for name, bad := range map[string][]byte{
		"ANSI code page": {'c', 'a', 'f', 0xE9},
		"odd UTF-16":     {0xFF, 0xFE, 'a', 0, 'b'},
	} {
		if _, err := decodeText(bad); err == nil || !strings.Contains(err.Error(), "save it as UTF-8") {
			t.Errorf("%s: err = %v, want a refusal that says to save it as UTF-8", name, err)
		}
	}
}

// writeText writes a temp file and returns its path.
func writeText(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "text.txt")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// withStdin runs fn with stdin read from a file holding content.
func withStdin(t *testing.T, content string, fn func()) {
	t.Helper()
	f, err := os.Open(writeText(t, content))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	old := os.Stdin
	os.Stdin = f
	defer func() { os.Stdin = old }()
	fn()
}

func TestTypeTextReadsTheText(t *testing.T) {
	three := 3
	p := writeText(t, "He said \"stop\" --headed\r\n")
	want := `He said "stop" --headed`
	cases := []struct {
		args []string
		want protocol.TypeTextArgs
	}{
		{[]string{"type-text", "101", "--file", p}, protocol.TypeTextArgs{Window: "101", Text: want}},
		{[]string{"type-text", "101", "--file=" + p}, protocol.TypeTextArgs{Window: "101", Text: want}},
		{[]string{"type-text", "101", "--into", "3", "--file", p}, protocol.TypeTextArgs{Window: "101", Text: want, IntoIndex: &three}},
		{[]string{"type-text", "101", "--post", "--interval-ms", "20", "--file", p}, protocol.TypeTextArgs{Window: "101", Text: want, Post: true, IntervalMS: 20}},
		// After the source there is no literal text: --headed is the flag.
		{[]string{"type-text", "101", "--file", p, "--headed"}, protocol.TypeTextArgs{Window: "101", Text: want, Headed: true}},
		{[]string{"type-text", "101", "--into", "3", "--file", p, "--headed"}, protocol.TypeTextArgs{Window: "101", Text: want, IntoIndex: &three, Headed: true}},
		// Before the source, as text, flags stay text.
		{[]string{"type-text", "101", "hello", "--file", p}, protocol.TypeTextArgs{Window: "101", Text: "hello --file " + p}},
	}
	for _, tc := range cases {
		rest, headed := extractHeaded(tc.args)
		req, err := typeTextRequest(rest[1:], headed)
		if err != nil {
			t.Fatalf("%q: %v", tc.args, err)
		}
		if !reflect.DeepEqual(req, tc.want) {
			t.Errorf("%q: request = %+v, want %+v", tc.args, req, tc.want)
		}
	}
	withStdin(t, "from a pipe\n", func() {
		for _, args := range [][]string{{"101", "--stdin"}, {"101", "--file", "-"}} {
			req, err := typeTextRequest(args, false)
			if err != nil || req.Text != "from a pipe" {
				t.Errorf("%q: request = %+v, %v; want the piped text", args, req, err)
			}
			os.Stdin.Seek(0, 0)
		}
	})
}

func TestTextSourceRefusals(t *testing.T) {
	p := writeText(t, "x")
	big := writeText(t, strings.Repeat("a", maxSourcedText+1))
	missing := filepath.Join(t.TempDir(), "missing.txt")
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"101", "--stdin", "hello"}, `type-text takes the text or --stdin, not both; "hello" follows --stdin`},
		{[]string{"101", "--file", p, "hello"}, "type-text takes the text or --file, not both"},
		{[]string{"101", "--stdin", "--interval-ms", "5"}, "type-text's own flags go before it"},
		{[]string{"101", "--file"}, "--file takes a file path"},
		{[]string{"101", "--file="}, "--file takes a file path"},
		{[]string{"101", "--file", missing}, "reading the text from " + missing},
		{[]string{"101", "--file", big}, "longer than 1048576 bytes"},
	}
	for _, tc := range cases {
		if _, err := typeTextRequest(tc.args, false); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("type-text %q: err = %v, want %q", tc.args, err, tc.want)
		}
	}
	old := stdinIsTerminal
	stdinIsTerminal = func() bool { return true }
	defer func() { stdinIsTerminal = old }()
	if _, err := typeTextRequest([]string{"101", "--stdin"}, false); err == nil || !strings.Contains(err.Error(), "stdin is a terminal") {
		t.Errorf("--stdin on a terminal: err = %v, want the refusal", err)
	}
	if _, _, _, _, _, err := parseSetValueArgs([]string{"101", "3", "--stdin"}); err == nil || !strings.Contains(err.Error(), "stdin is a terminal") {
		t.Errorf("set-value --stdin on a terminal: err = %v, want the refusal", err)
	}
}

func TestSetValueReadsTheValue(t *testing.T) {
	p := writeText(t, "\xEF\xBB\xBF{\"a\": 1}\n")
	window, _, ref, _, value, err := parseSetValueArgs([]string{"101", "id:json", "--file", p})
	if err != nil || window != "101" || ref != "id:json" || value != `{"a": 1}` {
		t.Errorf("--file: %q %q %q, %v", window, ref, value, err)
	}
	withStdin(t, "secret\n", func() {
		_, _, _, _, value, err := parseSetValueArgs([]string{"101", "3", "--stdin"})
		if err != nil || value != "secret" {
			t.Errorf("--stdin: %q, %v", value, err)
		}
	})
	if _, _, _, _, _, err := parseSetValueArgs([]string{"101", "3", "--stdin", "x"}); err == nil || !strings.Contains(err.Error(), "set-value takes the text or --stdin, not both") {
		t.Errorf("--stdin and a value: err = %v", err)
	}
	// A value that only contains the word is still the value.
	if _, _, _, _, value, err := parseSetValueArgs([]string{"101", "3", "use", "--stdin"}); err != nil || value != "use --stdin" {
		t.Errorf("literal: %q, %v", value, err)
	}
	// After the source, --headed and --json are global flags again.
	rest, headed := extractHeaded([]string{"set-value", "101", "3", "--file", p, "--headed"})
	if !headed || !reflect.DeepEqual(rest, []string{"set-value", "101", "3", "--file", p}) {
		t.Errorf("extractHeaded: %q, %v", rest, headed)
	}
	if got := strings.Join(hoistGlobalFlags([]string{"set-value", "101", "3", "--stdin", "--json"}), " "); got != "--json set-value 101 3 --stdin" {
		t.Errorf("hoistGlobalFlags = %q", got)
	}
}

func TestClipboardSetReadsTheText(t *testing.T) {
	p := writeText(t, "line one\nline two\n")
	got, err := parseClipboardSetArgs([]string{"--text-file", p})
	if err != nil || got.Text == nil || *got.Text != "line one\nline two" {
		t.Errorf("--text-file: %+v, %v", got, err)
	}
	withStdin(t, "piped", func() {
		got, err := parseClipboardSetArgs([]string{"--stdin"})
		if err != nil || got.Text == nil || *got.Text != "piped" {
			t.Errorf("--stdin: %+v, %v", got, err)
		}
	})
	// --file still copies the file itself.
	abs, _ := filepath.Abs(p)
	if got, err := parseClipboardSetArgs([]string{"--file", p}); err != nil || !reflect.DeepEqual(got.Files, []string{abs}) {
		t.Errorf("--file: %+v, %v", got, err)
	}
	if _, err := parseClipboardSetArgs([]string{"--stdin", "more"}); err == nil || !strings.Contains(err.Error(), "not both") {
		t.Errorf("--stdin and text: err = %v", err)
	}
	if got := strings.Join(hoistGlobalFlags([]string{"clipboard-set", "--text-file", p, "--json"}), " "); got != "--json clipboard-set --text-file "+p {
		t.Errorf("hoistGlobalFlags = %q", got)
	}
	if got := strings.Join(hoistGlobalFlags([]string{"clipboard-set", "hi", "--json"}), " "); got != "clipboard-set hi --json" {
		t.Errorf("literal text: hoistGlobalFlags = %q", got)
	}
}

func TestObserveAfterATextSource(t *testing.T) {
	p := writeText(t, "x")
	rest, o, err := extractObserve([]string{"type-text", "101", "--into", "3", "--file", p, "--observe"})
	if err != nil || o == nil || !reflect.DeepEqual(rest, []string{"type-text", "101", "--into", "3", "--file", p}) {
		t.Errorf("extractObserve = %q, %+v, %v", rest, o, err)
	}
}

// TestBatchStepsTakeNoTextSource: a Batch step's text is in the step;
// stdin belongs to `batch -`.
func TestBatchStepsTakeNoTextSource(t *testing.T) {
	for _, args := range [][]string{
		{"101", "type-text", "--stdin"},
		{"101", "type-text", "--into", "3", "--file", "x.txt"},
		{"101", "click", "3", "then", "set-value", "4", "--stdin"},
		{"101", "set-value", "4", "--file=x.txt"},
	} {
		if _, _, err := parseBatchArgs(args); err == nil || !strings.Contains(err.Error(), "winuse batch -") {
			t.Errorf("%q: err = %v, want the Batch refusal", args, err)
		}
	}
	if _, _, _, _, err := parseBatchScript("101\ntype-text --stdin\n"); err == nil || !strings.Contains(err.Error(), "batch stdin line 2") {
		t.Errorf("batch -: err = %v, want line 2 refused", err)
	}
	// A value that merely holds the word stays literal.
	_, steps, err := parseBatchArgs([]string{"101", "set-value", "4", "say", "--stdin"})
	if err != nil || len(steps) != 1 || !strings.Contains(string(steps[0].Args), `"value":"say --stdin"`) {
		t.Errorf("literal step: %+v, %v", steps, err)
	}
}

// TestTextSourceUsageErrorsExitTwo: a bad source is a usage error, before
// the helper is dialed.
func TestTextSourceUsageErrorsExitTwo(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.txt")
	for _, args := range [][]string{
		{"type-text", "101", "--stdin", "extra"},
		{"type-text", "101", "--file", missing},
		{"set-value", "101", "3", "--file"},
		{"clipboard-set", "--text-file", missing},
	} {
		var code int
		stderr := captureStderr(t, func() { code = Main(args) })
		if code != 2 || !strings.Contains(stderr, "usage: winuse "+args[0]) {
			t.Errorf("%q: exit %d, stderr %q; want 2 and the usage", args, code, stderr)
		}
	}
}
