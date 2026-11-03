package cli

// Offline tests for list-windows's flags (desktop-parity 22): --details
// and --app parse locally, and so does --shell (desktop-parity 09); usage
// errors exit 2 and never dial the helper.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestParseListWindowsArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		want    protocol.ListWindowsArgs
		wantErr string
	}{
		{name: "no flags: Codex's list_windows", args: nil},
		{name: "details", args: []string{"--details"}, want: protocol.ListWindowsArgs{Details: true}},
		{name: "shell", args: []string{"--shell"}, want: protocol.ListWindowsArgs{Shell: true}},
		{name: "shell with the others", args: []string{"--shell", "--details", "--app", "explorer.exe"},
			want: protocol.ListWindowsArgs{Details: true, App: "explorer.exe", Shell: true}},
		{name: "app", args: []string{"--app", "Notepad"}, want: protocol.ListWindowsArgs{App: "Notepad"}},
		{name: "app=", args: []string{"--app=Google Chrome"}, want: protocol.ListWindowsArgs{App: "Google Chrome"}},
		{name: "both, any order", args: []string{"--app", "chrome.exe", "--details"},
			want: protocol.ListWindowsArgs{Details: true, App: "chrome.exe"}},
		{name: "app without a name", args: []string{"--app"}, wantErr: "--app takes an App name"},
		{name: "app with an empty name", args: []string{"--app="}, wantErr: "--app takes an App name"},
		{name: "app twice", args: []string{"--app", "a", "--app", "b"}, wantErr: "--app is given twice"},
		{name: "a stray argument", args: []string{"notepad"}, wantErr: `unexpected argument "notepad"`},
		{name: "an unknown flag", args: []string{"--all"}, wantErr: `unexpected argument "--all"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseListWindowsArgs(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("args = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestListWindowsUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"notepad"}, {"--app"}, {"--details", "extra"}} {
		path := filepath.Join(t.TempDir(), "stderr")
		f, err := os.Create(path)
		if err != nil {
			t.Fatalf("create stderr file: %v", err)
		}
		oldStderr, oldStdout := os.Stderr, os.Stdout
		os.Stderr, os.Stdout = f, f
		exit := Main(append([]string{"list-windows"}, args...))
		os.Stderr, os.Stdout = oldStderr, oldStdout
		f.Close()
		if exit != 2 {
			t.Fatalf("list-windows %v: exit = %d, want 2 (usage error)", args, exit)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read stderr: %v", err)
		}
		if !strings.Contains(string(got), listWindowsUsage) {
			t.Errorf("list-windows %v: stderr = %q, want the usage line", args, got)
		}
	}
}

func TestUsageListsListWindowsFlags(t *testing.T) {
	for _, want := range []string{"list-windows [--details] [--app <name>]", "focused", "minimized", "maximized", "admin"} {
		if !strings.Contains(Usage, want) {
			t.Errorf("usage text missing %q", want)
		}
	}
}
