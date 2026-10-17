package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
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
	for _, cmd := range []string{"list-windows", "get-window", "get-window-state", "record-tree"} {
		if !strings.Contains(Usage, cmd) {
			t.Errorf("usage text missing %q", cmd)
		}
	}
}

// TestGetWindowUsageErrors pins get-window's local validation: usage
// failures exit 2 and never dial the helper.
func TestGetWindowUsageErrors(t *testing.T) {
	for _, args := range [][]string{{}, {"101", "extra"}} {
		path := filepath.Join(t.TempDir(), "stderr")
		f, err := os.Create(path)
		if err != nil {
			t.Fatalf("create stderr file: %v", err)
		}
		oldStderr, oldStdout := os.Stderr, os.Stdout
		os.Stderr, os.Stdout = f, f
		exit := Main(append([]string{"get-window"}, args...))
		os.Stderr, os.Stdout = oldStderr, oldStdout
		f.Close()
		if exit != 2 {
			t.Fatalf("get-window %v: exit = %d, want 2 (usage error)", args, exit)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read stderr: %v", err)
		}
		if !strings.Contains(string(got), "usage: winuse get-window <window>") {
			t.Errorf("get-window %v: stderr = %q, want the usage line", args, got)
		}
	}
}

// TestInputDesktopExitErrorReportsLocked pins finding 9's CLI half: when
// the input desktop cannot be opened in an interactive session (the lock
// screen / secure desktop), the CLI's early exit reports the
// locked-desktop refusal — the helper's own message and hint, exit 1 —
// not the session-0 message that would send the agent chasing a service.
func TestInputDesktopExitErrorReportsLocked(t *testing.T) {
	id, err := win.CurrentIdentity()
	if err != nil {
		t.Fatalf("CurrentIdentity: %v", err)
	}
	if id.Session == 0 {
		t.Skip("running in session 0: the refusal is ErrNoDesktop by definition")
	}
	err = inputDesktopExitError()
	var ee *exitError
	if !errors.As(err, &ee) {
		t.Fatalf("error = %v (%T), want an exitError", err, err)
	}
	if ee.msg != "Windows desktop is locked; unlock before using winuse" {
		t.Errorf("message = %q, want the locked-desktop refusal", ee.msg)
	}
	if ee.hint != win.LockedDesktopHint {
		t.Errorf("hint = %q, want the stop-and-ask hint", ee.hint)
	}
	if code := ExitCode(err); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
}

// TestUsageListsTheCanonicalSecondaryActions pins finding 18's wiring:
// the usage text's Secondary Actions list is built from uia's canonical
// table, so the CLI can never drift from the Window State's listing and
// the Action validation.
func TestUsageListsTheCanonicalSecondaryActions(t *testing.T) {
	for _, name := range uia.SecondaryActionNames {
		if !strings.Contains(Usage, name) {
			t.Errorf("usage text missing the Secondary Action %q", name)
		}
	}
}
