package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMainUsageErrors pins the argument-count validation of the
// window-state commands: usage failures exit 2 and never dial the helper.
func TestMainUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"get-window-state without a window", []string{"get-window-state"}, "usage: winuse get-window-state <window>"},
		{"get-window-state with two arguments", []string{"get-window-state", "101", "extra"}, "usage: winuse get-window-state <window>"},
		{"record-tree without a file", []string{"record-tree", "101"}, "usage: winuse record-tree <window> <file>"},
		{"record-tree with three arguments", []string{"record-tree", "101", "a", "b"}, "usage: winuse record-tree <window> <file>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Main writes usage errors to stderr; stdout stays empty. It
			// must not touch the helper, so no desktop interaction is
			// possible from these paths.
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

// TestUsageMentionsWindowStateCommands guards the command surface against
// accidental removal from the help text.
func TestUsageMentionsWindowStateCommands(t *testing.T) {
	for _, cmd := range []string{"list-windows", "get-window-state", "record-tree"} {
		if !strings.Contains(usage, cmd) {
			t.Errorf("usage text missing %q", cmd)
		}
	}
}
