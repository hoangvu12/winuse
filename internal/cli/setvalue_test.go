package cli

// The ticket-07 Action commands' CLI-side tests: argument parsing (the
// value and the action name join their trailing arguments, so unquoted
// spaces still form one value), usage errors exiting 2 without dialing
// the helper, and the dispatch wiring.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSetValueArgs(t *testing.T) {
	// Two arguments: usage error.
	if _, _, _, err := parseSetValueArgs([]string{"101", "3"}); err == nil {
		t.Error("missing value must be a usage error")
	}
	// A bad index is a usage error.
	if _, _, _, err := parseSetValueArgs([]string{"101", "x", "v"}); err == nil {
		t.Error("a non-numeric index must be a usage error")
	}
	if _, _, _, err := parseSetValueArgs([]string{"101", "-1", "v"}); err == nil {
		t.Error("a negative index must be a usage error")
	}
	// The value is every remaining argument joined with single spaces.
	window, index, value, err := parseSetValueArgs([]string{"101", "3", "hello"})
	if err != nil || window != "101" || index != 3 || value != "hello" {
		t.Fatalf("parse = %q, %d, %q, %v", window, index, value, err)
	}
	_, _, value, err = parseSetValueArgs([]string{"notepad", "0", "hello", "world", "  twice"})
	if err != nil || value != "hello world   twice" {
		t.Fatalf("joined value = %q, %v; want the arguments joined with single spaces", value, err)
	}
	// A value that looks like a flag or a negative number is a value.
	_, _, value, err = parseSetValueArgs([]string{"101", "2", "--not-a-flag"})
	if err != nil || value != "--not-a-flag" {
		t.Fatalf("value = %q, %v; want the raw argument", value, err)
	}
}

func TestParseSecondaryActionArgs(t *testing.T) {
	if _, _, _, err := parseSecondaryActionArgs([]string{"101", "3"}); err == nil {
		t.Error("missing action must be a usage error")
	}
	if _, _, _, err := parseSecondaryActionArgs([]string{"101", "x", "Expand"}); err == nil {
		t.Error("a non-numeric index must be a usage error")
	}
	// An unquoted two-word action joins into one action name.
	window, index, action, err := parseSecondaryActionArgs([]string{"101", "3", "scroll", "down"})
	if err != nil || window != "101" || index != 3 || action != "scroll down" {
		t.Fatalf("parse = %q, %d, %q, %v", window, index, action, err)
	}
}

func TestSetValueAndSecondaryUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"set-value without arguments", []string{"set-value"}, setValueUsage},
		{"set-value without a value", []string{"set-value", "101", "3"}, setValueUsage},
		{"set-value with a bad index", []string{"set-value", "101", "x", "v"}, "the element index must be a non-negative integer"},
		{"perform-secondary-action without arguments", []string{"perform-secondary-action"}, secondaryActionUsage},
		{"perform-secondary-action without an action", []string{"perform-secondary-action", "101", "3"}, secondaryActionUsage},
		{"perform-secondary-action with a bad index", []string{"perform-secondary-action", "101", "x", "Expand"}, "the element index must be a non-negative integer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Usage errors write to stderr and never dial the helper, so
			// no desktop interaction is possible from these paths.
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

func TestUsageMentionsTicket07Commands(t *testing.T) {
	for _, cmd := range []string{"set-value", "perform-secondary-action"} {
		if !strings.Contains(usage, cmd) {
			t.Errorf("usage text missing %q", cmd)
		}
	}
}
