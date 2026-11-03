package cli

// Offline tests for close-app's CLI side (desktop-parity ticket 10): the
// argument parsing, the usage errors and the usage text. None of these
// dial the helper, so no desktop is touched.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestParseCloseAppArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		want    protocol.CloseAppArgs
		wantErr string
	}{
		{name: "an App", args: []string{"notepad"}, want: protocol.CloseAppArgs{App: "notepad"}},
		{name: "--force after", args: []string{"Notepad", "--force"}, want: protocol.CloseAppArgs{App: "Notepad", Force: true}},
		{name: "--force before", args: []string{"--force", "notepad.exe"}, want: protocol.CloseAppArgs{App: "notepad.exe", Force: true}},
		{name: "a name with a space", args: []string{"Windows Notepad"}, want: protocol.CloseAppArgs{App: "Windows Notepad"}},
		{name: "no App", args: nil, wantErr: "close-app takes exactly one App"},
		{name: "only --force", args: []string{"--force"}, wantErr: "close-app takes exactly one App"},
		{name: "two Apps", args: []string{"notepad", "paint"}, wantErr: "close-app takes exactly one App"},
		{name: "an unknown flag", args: []string{"notepad", "--kill"}, wantErr: "unknown flag --kill"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseCloseAppArgs(tc.args)
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

func TestCloseAppUsageErrorsExit2WithoutDialing(t *testing.T) {
	for _, args := range [][]string{
		{"close-app"},
		{"close-app", "notepad", "paint"},
		{"close-app", "notepad", "--kill"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "stderr")
			f, err := os.Create(path)
			if err != nil {
				t.Fatalf("create stderr file: %v", err)
			}
			oldStderr, oldStdout := os.Stderr, os.Stdout
			os.Stderr, os.Stdout = f, f
			code := Main(args)
			os.Stderr, os.Stdout = oldStderr, oldStdout
			f.Close()
			if code != 2 {
				t.Fatalf("exit = %d, want 2 (usage error)", code)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read stderr: %v", err)
			}
			if !strings.Contains(string(got), closeAppUsage) {
				t.Errorf("stderr = %q, want it to contain %q", got, closeAppUsage)
			}
		})
	}
}

func TestUsageListsCloseApp(t *testing.T) {
	for _, want := range []string{"close-app <app> [--force]", "the only\n                               command that ends a process"} {
		if !strings.Contains(Usage, want) {
			t.Errorf("usage text missing %q", want)
		}
	}
}
