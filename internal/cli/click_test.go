package cli

// Offline tests for the click command's argument parsing (flags anywhere,
// =joined values, unknown flags, bad indexes) and the usage errors of the
// new commands. None of these dial the helper, so no desktop is touched.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseClickArgs(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantWindow string
		wantIndex  int
		wantCount  int
		wantButton string
		wantErr    string
	}{
		{name: "positional only", args: []string{"101", "3"},
			wantWindow: "101", wantIndex: 3, wantCount: 1, wantButton: "left"},
		{name: "flags before positionals", args: []string{"--count", "2", "--button", "right", "notepad", "0"},
			wantWindow: "notepad", wantIndex: 0, wantCount: 2, wantButton: "right"},
		{name: "flags after positionals", args: []string{"101", "3", "--button", "middle"},
			wantWindow: "101", wantIndex: 3, wantCount: 1, wantButton: "middle"},
		{name: "flags between positionals", args: []string{"101", "--count", "3", "3"},
			wantWindow: "101", wantIndex: 3, wantCount: 3, wantButton: "left"},
		{name: "equals-joined values", args: []string{"--count=2", "--button=right", "101", "1"},
			wantWindow: "101", wantIndex: 1, wantCount: 2, wantButton: "right"},
		{name: "short flags", args: []string{"-c", "2", "-b", "r", "101", "1"},
			wantWindow: "101", wantIndex: 1, wantCount: 2, wantButton: "r"},
		{name: "window id zero index zero", args: []string{"0", "0"},
			wantWindow: "0", wantIndex: 0, wantCount: 1, wantButton: "left"},
		{name: "dash dash ends flags", args: []string{"--button", "right", "--", "-c", "2"},
			wantWindow: "-c", wantIndex: 2, wantCount: 1, wantButton: "right"},

		{name: "too few positionals", args: []string{"101"},
			wantErr: "click takes a window reference and an element index"},
		{name: "too many positionals", args: []string{"101", "1", "2"},
			wantErr: "click takes a window reference and an element index"},
		{name: "unknown flag", args: []string{"--speed", "fast", "101", "1"},
			wantErr: "unknown flag --speed"},
		{name: "count without a value", args: []string{"101", "1", "--count"},
			wantErr: "--count needs a value"},
		{name: "count is not a number", args: []string{"--count", "two", "101", "1"},
			wantErr: `--count needs a value (1, 2 or 3), got "two"`},
		{name: "count is zero", args: []string{"--count", "0", "101", "1"},
			wantErr: `--count needs a value (1, 2 or 3), got "0"`},
		{name: "button without a value", args: []string{"101", "1", "--button"},
			wantErr: "--button needs a value"},
		{name: "index is not a number", args: []string{"101", "x"},
			wantErr: `the element index must be a non-negative integer, got "x"`},
		{name: "negative index is not a flag", args: []string{"101", "-3"},
			wantErr: `the element index must be a non-negative integer, got "-3"`},
		{name: "no arguments", args: nil,
			wantErr: "click takes a window reference and an element index"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			window, index, o, err := parseClickArgs(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if window != tc.wantWindow || index != tc.wantIndex {
				t.Errorf("window/index = %q/%d, want %q/%d", window, index, tc.wantWindow, tc.wantIndex)
			}
			if o.count != tc.wantCount || o.button != tc.wantButton {
				t.Errorf("count/button = %d/%q, want %d/%q", o.count, o.button, tc.wantCount, tc.wantButton)
			}
		})
	}
}

func TestIsNegativeNumber(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{"-3", true},
		{"-3x", false},
		{"-", false},
		{"--count", false},
		{"101", false},
		{"-0", true},
	}
	for _, tc := range cases {
		if got := isNegativeNumber(tc.s); got != tc.want {
			t.Errorf("isNegativeNumber(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

func TestClickUsageErrorsExit2WithoutDialing(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"click with no arguments", []string{"click"}, "usage: winuse click <window> <element index>"},
		{"click with an unknown flag", []string{"click", "101", "1", "--speed", "5"}, "unknown flag --speed"},
		{"click with a bad index", []string{"click", "101", "abc"}, "the element index must be a non-negative integer"},
		{"activate-window with no window", []string{"activate-window"}, "usage: winuse activate-window <window>"},
		{"activate-window with two windows", []string{"activate-window", "101", "102"}, "usage: winuse activate-window <window>"},
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

func TestUsageMentionsActionCommands(t *testing.T) {
	for _, cmd := range []string{"click", "activate-window", "--count", "--button"} {
		if !strings.Contains(usage, cmd) {
			t.Errorf("usage text missing %q", cmd)
		}
	}
}

func TestClickUnknownCommandStillExits2(t *testing.T) {
	// Sanity: the dispatcher routes click and activate-window; an unknown
	// command keeps the usage failure.
	if code := Main([]string{"clickx"}); code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
}
