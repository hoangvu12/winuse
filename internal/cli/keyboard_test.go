package cli

// Offline tests for the keyboard Action commands' argument handling
// (ticket 06): the Window-then-text/key split, argument joining, and the
// usage errors that exit 2 without dialing the helper.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestParseTypeTextArgs(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantWindow string
		wantText   string
		wantErr    string
	}{
		{name: "one text argument", args: []string{"101", "hello"},
			wantWindow: "101", wantText: "hello"},
		{name: "text with spaces quoted", args: []string{"notepad", "hello world"},
			wantWindow: "notepad", wantText: "hello world"},
		{name: "unquoted words join with single spaces", args: []string{"101", "hello", "brave", "world"},
			wantWindow: "101", wantText: "hello brave world"},
		{name: "consecutive spaces survive when quoted", args: []string{"101", "two  spaces"},
			wantWindow: "101", wantText: "two  spaces"},
		{name: "unicode text", args: []string{"101", "世界 🎉"},
			wantWindow: "101", wantText: "世界 🎉"},
		{name: "flag-looking text is text (type-text has no flags)", args: []string{"101", "--count", "2"},
			wantWindow: "101", wantText: "--count 2"},
		{name: "empty text argument", args: []string{"101", ""},
			wantWindow: "101", wantText: ""},
		{name: "app name as window", args: []string{"Notepad", "hi"},
			wantWindow: "Notepad", wantText: "hi"},
		{name: "no text", args: []string{"101"},
			wantErr: "type-text takes a window reference and text"},
		{name: "no arguments", args: nil,
			wantErr: "type-text takes a window reference and text"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			window, text, err := parseTypeTextArgs(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if window != tc.wantWindow || text != tc.wantText {
				t.Errorf("window/text = %q/%q, want %q/%q", window, text, tc.wantWindow, tc.wantText)
			}
		})
	}
}

func TestParsePressKeyArgs(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantWindow string
		wantKey    string
		wantErr    string
	}{
		{name: "single key", args: []string{"101", "Return"},
			wantWindow: "101", wantKey: "Return"},
		{name: "chord", args: []string{"101", "Control_L+a"},
			wantWindow: "101", wantKey: "Control_L+a"},
		{name: "chord with spaces joins (whitespace around + is ignored)", args: []string{"101", "Control_L", "+", "a"},
			wantWindow: "101", wantKey: "Control_L + a"},
		{name: "lowercase key", args: []string{"notepad", "return"},
			wantWindow: "notepad", wantKey: "return"},
		{name: "no key", args: []string{"101"},
			wantErr: "press-key takes a window reference and a key"},
		{name: "no arguments", args: nil,
			wantErr: "press-key takes a window reference and a key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			window, key, err := parsePressKeyArgs(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if window != tc.wantWindow || key != tc.wantKey {
				t.Errorf("window/key = %q/%q, want %q/%q", window, key, tc.wantWindow, tc.wantKey)
			}
		})
	}
}

func TestKeyboardUsageErrorsExit2WithoutDialing(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"type-text with no arguments", []string{"type-text"}, "usage: winuse type-text <window> [--post] <text>"},
		{"type-text with only a window", []string{"type-text", "101"}, "usage: winuse type-text <window> [--post] <text>"},
		{"press-key with no arguments", []string{"press-key"}, "usage: winuse press-key <window> <key> [--post]"},
		{"press-key with only a window", []string{"press-key", "101"}, "usage: winuse press-key <window> <key> [--post]"},
		// Posted Keys (ticket 07): --post needs text or a key, and never
		// combines with --headed.
		{"type-text --post with no text", []string{"type-text", "101", "--post"}, "usage: winuse type-text <window> [--post] <text>"},
		{"press-key --post with no key", []string{"press-key", "101", "--post"}, "usage: winuse press-key <window> <key> [--post]"},
		{"press-key --post --headed", []string{"press-key", "101", "Return", "--post", "--headed"}, "--post and --headed cannot be combined"},
		{"--headed press-key --post", []string{"--headed", "press-key", "101", "--post", "Return"}, "--post and --headed cannot be combined"},
		{"--headed type-text --post", []string{"--headed", "type-text", "101", "--post", "hi"}, "--post and --headed cannot be combined"},
		{"type-text --headed before the window with --post", []string{"type-text", "--headed", "101", "--post", "hi"}, "--post and --headed cannot be combined"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "stderr")
			f, err := os.Create(path)
			if err != nil {
				t.Fatalf("create stderr file: %v", err)
			}
			oldStderr, oldStdout := os.Stderr, os.Stdout
			os.Stderr, os.Stdout = f, f
			code := Main(tc.args)
			os.Stderr, os.Stdout = oldStderr, oldStdout
			f.Close()
			if code != 2 {
				t.Fatalf("exit = %d, want 2 (usage error)", code)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read stderr: %v", err)
			}
			if !strings.Contains(string(got), tc.want) {
				t.Errorf("stderr = %q, want it to contain %q", got, tc.want)
			}
		})
	}
}

func TestUsageMentionsKeyboardCommands(t *testing.T) {
	for _, cmd := range []string{"type-text", "press-key", "Control_L+a", "Numpad_0", "Unicode"} {
		if !strings.Contains(Usage, cmd) {
			t.Errorf("usage text missing %q", cmd)
		}
	}
}

func TestKeyboardUnknownCommandStillExits2(t *testing.T) {
	// Sanity: the dispatcher routes type-text and press-key; an unknown
	// command keeps the usage failure.
	for _, cmd := range []string{"type-texts", "presskey"} {
		if code := Main([]string{cmd}); code != 2 {
			t.Errorf("%s: exit = %d, want 2", cmd, code)
		}
	}
}

// TestHeadedReachesPressKeyRequests pins ticket 05's CLI half: press-key
// has a Headless form now (Return and Escape on a classic dialog), so the
// global --headed flag, wherever it stands, reaches its request.
func TestHeadedReachesPressKeyRequests(t *testing.T) {
	for _, line := range []string{
		"press-key 101 Return --headed",
		"--headed press-key 101 Return",
		"press-key --headed 101 Return",
		"press-key 101 Return",
	} {
		rest, headed := extractHeaded(split(line))
		req, err := pressKeyRequest(rest[1:], headed)
		if err != nil {
			t.Fatalf("%q: %v", line, err)
		}
		want := protocol.PressKeyArgs{Window: "101", Key: "Return", Headed: strings.Contains(line, "--headed")}
		if req != want {
			t.Errorf("%q: request = %+v, want %+v", line, req, want)
		}
	}
}

// TestHeadedReachesTypeTextRequests pins ticket 06's CLI half: type-text
// has a Headless form now (text into a focused classic edit control), so
// the global --headed flag reaches its request when it stands before the
// text; inside the text it stays literal.
func TestHeadedReachesTypeTextRequests(t *testing.T) {
	cases := []struct {
		line   string
		text   string
		headed bool
	}{
		{"--headed type-text 101 hello", "hello", true},
		{"type-text --headed 101 hello", "hello", true},
		{"type-text 101 hello", "hello", false},
		{"type-text 101 hello --headed", "hello --headed", false},
	}
	for _, tc := range cases {
		rest, headed := extractHeaded(split(tc.line))
		req, err := typeTextRequest(rest[1:], headed)
		if err != nil {
			t.Fatalf("%q: %v", tc.line, err)
		}
		want := protocol.TypeTextArgs{Window: "101", Text: tc.text, Headed: tc.headed}
		if req != want {
			t.Errorf("%q: request = %+v, want %+v", tc.line, req, want)
		}
	}
}

// TestPostReachesKeyboardRequests pins ticket 07's CLI parsing: on
// press-key, --post may stand right after the Window or after the key; on
// type-text it must stand right after the Window, because everything
// after that is the text (a later --post is text).
func TestPostReachesKeyboardRequests(t *testing.T) {
	keyCases := []struct {
		line string
		want protocol.PressKeyArgs
	}{
		{"press-key 101 --post Return", protocol.PressKeyArgs{Window: "101", Key: "Return", Post: true}},
		{"press-key 101 Return --post", protocol.PressKeyArgs{Window: "101", Key: "Return", Post: true}},
		{"press-key 101 Control_L + a --post", protocol.PressKeyArgs{Window: "101", Key: "Control_L + a", Post: true}},
		{"press-key 101 Return", protocol.PressKeyArgs{Window: "101", Key: "Return"}},
	}
	for _, tc := range keyCases {
		rest, headed := extractHeaded(split(tc.line))
		req, err := pressKeyRequest(rest[1:], headed)
		if err != nil {
			t.Fatalf("%q: %v", tc.line, err)
		}
		if req != tc.want {
			t.Errorf("%q: request = %+v, want %+v", tc.line, req, tc.want)
		}
	}
	textCases := []struct {
		line string
		want protocol.TypeTextArgs
	}{
		{"type-text 101 --post hello world", protocol.TypeTextArgs{Window: "101", Text: "hello world", Post: true}},
		{"type-text 101 hello --post", protocol.TypeTextArgs{Window: "101", Text: "hello --post"}},
		{"type-text 101 --post --post", protocol.TypeTextArgs{Window: "101", Text: "--post", Post: true}},
		{"type-text 101 --post --headed", protocol.TypeTextArgs{Window: "101", Text: "--headed", Post: true}},
	}
	for _, tc := range textCases {
		rest, headed := extractHeaded(split(tc.line))
		req, err := typeTextRequest(rest[1:], headed)
		if err != nil {
			t.Fatalf("%q: %v", tc.line, err)
		}
		if req != tc.want {
			t.Errorf("%q: request = %+v, want %+v", tc.line, req, tc.want)
		}
	}
	if _, err := pressKeyRequest([]string{"101", "Return", "--post"}, true); err == nil || !strings.Contains(err.Error(), "--post and --headed") {
		t.Errorf("press-key --post with headed: err = %v, want the usage error", err)
	}
	if _, err := typeTextRequest([]string{"101", "--post", "hi"}, true); err == nil || !strings.Contains(err.Error(), "--post and --headed") {
		t.Errorf("type-text --post with headed: err = %v, want the usage error", err)
	}
}

// TestForceReachesPressKeyRequests pins desktop-parity ticket 05's CLI
// parsing: --force may stand anywhere after the Window and combines with
// --post and --headed.
func TestForceReachesPressKeyRequests(t *testing.T) {
	cases := []struct {
		line string
		want protocol.PressKeyArgs
	}{
		{"press-key 101 Alt_L+F4 --force", protocol.PressKeyArgs{Window: "101", Key: "Alt_L+F4", Force: true}},
		{"press-key 101 --force Alt_L+Tab", protocol.PressKeyArgs{Window: "101", Key: "Alt_L+Tab", Force: true}},
		{"press-key 101 --force Return --post", protocol.PressKeyArgs{Window: "101", Key: "Return", Post: true, Force: true}},
		{"--headed press-key 101 ctrl+shift+esc --force", protocol.PressKeyArgs{Window: "101", Key: "ctrl+shift+esc", Headed: true, Force: true}},
	}
	for _, tc := range cases {
		rest, headed := extractHeaded(split(tc.line))
		req, err := pressKeyRequest(rest[1:], headed)
		if err != nil {
			t.Fatalf("%q: %v", tc.line, err)
		}
		if req != tc.want {
			t.Errorf("%q: request = %+v, want %+v", tc.line, req, tc.want)
		}
	}
	if _, err := pressKeyRequest([]string{"101", "--force"}, false); err == nil {
		t.Error("press-key 101 --force: want the missing-key usage error")
	}
}

func TestUsageMentionsPost(t *testing.T) {
	for _, want := range []string{"type-text <window> [--post] <text>", "press-key <window> <key> [--post] [--force]", "posted", "Alt+F4, Alt+Tab, Ctrl+Shift+Escape"} {
		if !strings.Contains(Usage, want) {
			t.Errorf("usage text missing %q", want)
		}
	}
}
