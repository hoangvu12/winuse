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
			wantErr: "click takes a window reference and an element index, name:/id: reference or @x,y"},
		{name: "too many positionals", args: []string{"101", "1", "2"},
			wantErr: "click takes a window reference and an element index, name:/id: reference or @x,y"},
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
			wantErr: `the element reference must be a non-negative element index, name:<text> or id:<AutomationId>, got "x"`},
		{name: "negative index is not a flag", args: []string{"101", "-3"},
			wantErr: `the element reference must be a non-negative element index, name:<text> or id:<AutomationId>, got "-3"`},
		{name: "no arguments", args: nil,
			wantErr: "click takes a window reference and an element index, name:/id: reference or @x,y"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			window, target, o, err := parseClickArgs(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if window != tc.wantWindow || target.index == nil || *target.index != tc.wantIndex {
				t.Errorf("window/index = %q/%v, want %q/%d", window, target.index, tc.wantWindow, tc.wantIndex)
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
		{"click with no arguments", []string{"click"}, "usage: winuse click <window> <element index | name:<text> | id:<id> | @x,y>"},
		{"click with an unknown flag", []string{"click", "101", "1", "--speed", "5"}, "unknown flag --speed"},
		{"click with a bad index", []string{"click", "101", "abc"}, "the element reference must be a non-negative element index"},
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

func TestParseClickArgsElementReferences(t *testing.T) {
	// The name: and id: forms pass through verbatim (the helper resolves
	// them against the Window's latest Window State); an Element Index
	// still parses as an index.
	cases := []struct {
		args     []string
		wantRef  string
		wantIdx  int
		wantNone bool // neither form set
	}{
		{args: []string{"101", "name:OK"}, wantRef: "name:OK"},
		{args: []string{"101", "name:Save As"}, wantRef: "name:Save As"},
		{args: []string{"notepad", "id:ParityBox"}, wantRef: "id:ParityBox"},
		{args: []string{"101", "id:14681076"}, wantRef: "id:14681076"},
		{args: []string{"101", "7"}, wantIdx: 7},
		{args: []string{"101", "name:"}, wantNone: true}, // empty text
		{args: []string{"101", "id:"}, wantNone: true},
	}
	for _, tc := range cases {
		window, target, _, err := parseClickArgs(tc.args)
		if tc.wantNone {
			if err == nil {
				t.Errorf("parseClickArgs(%q) succeeded; an empty reference must fail", tc.args)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseClickArgs(%q): %v", tc.args, err)
			continue
		}
		if window != tc.args[0] || target.point != nil {
			t.Errorf("parseClickArgs(%q) window/point = %q/%v", tc.args, window, target.point)
		}
		if tc.wantRef != "" {
			if target.ref != tc.wantRef || target.index != nil {
				t.Errorf("parseClickArgs(%q) = ref %q, index %v; want ref %q only", tc.args, target.ref, target.index, tc.wantRef)
			}
		} else if target.index == nil || *target.index != tc.wantIdx || target.ref != "" {
			t.Errorf("parseClickArgs(%q) = index %v, ref %q; want index %d only", tc.args, target.index, target.ref, tc.wantIdx)
		}
	}

	// A reference the shell split (unquoted multi-word name) gets the
	// quoting hint instead of a generic arity error.
	_, _, _, err := parseClickArgs([]string{"101", "name:New", "Document"})
	if err == nil || !strings.Contains(err.Error(), "must be one argument") || !strings.Contains(err.Error(), `name:New Document`) {
		t.Errorf("unquoted name error = %v, want the quoting hint naming the reference", err)
	}
}

func TestParseClickTargetTokenForms(t *testing.T) {
	// @x,y still parses as the coordinate form; every other token goes
	// through the shared element-reference parser.
	if target, err := parseClickTargetToken("@400,235"); err != nil || target.point == nil || target.point.X != 400 || target.point.Y != 235 {
		t.Errorf("parseClickTargetToken(@400,235) = %+v, %v", target, err)
	}
	if target, err := parseClickTargetToken("name:OK"); err != nil || target.ref != "name:OK" {
		t.Errorf("parseClickTargetToken(name:OK) = %+v, %v", target, err)
	}
}
