package cli

// Offline tests for the keyboard Action commands' argument handling
// (ticket 06): the Window-then-text/key split, argument joining, and the
// usage errors that exit 2 without dialing the helper.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		{"type-text with no arguments", []string{"type-text"}, "usage: winuse type-text <window> <text>"},
		{"type-text with only a window", []string{"type-text", "101"}, "usage: winuse type-text <window> <text>"},
		{"press-key with no arguments", []string{"press-key"}, "usage: winuse press-key <window> <key>"},
		{"press-key with only a window", []string{"press-key", "101"}, "usage: winuse press-key <window> <key>"},
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
