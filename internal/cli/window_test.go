package cli

// Offline tests for the Window Actions' CLI side (ADR 0011): the argument
// parsing and number validation of move-window and resize-window, the
// usage errors of all five commands, and the usage text. None of these
// dial the helper, so no desktop is touched.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestParseMoveWindowArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		want    protocol.MoveWindowArgs
		wantErr string
	}{
		{name: "origin", args: []string{"101", "0", "0"}, want: protocol.MoveWindowArgs{Window: "101"}},
		{name: "a monitor left of the primary", args: []string{"notepad", "-1920", "40"},
			want: protocol.MoveWindowArgs{Window: "notepad", X: -1920, Y: 40}},
		{name: "too few", args: []string{"101", "10"}, wantErr: "move-window takes a window reference, x and y"},
		{name: "too many", args: []string{"101", "10", "20", "30"}, wantErr: "move-window takes a window reference, x and y"},
		{name: "x is not a number", args: []string{"101", "ten", "20"}, wantErr: `x must be an integer (physical pixels), got "ten"`},
		{name: "y is a fraction", args: []string{"101", "10", "2.5"}, wantErr: `y must be an integer (physical pixels), got "2.5"`},
		{name: "x out of range", args: []string{"101", "5000000", "0"}, wantErr: "x and y must be between -1000000 and 1000000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseMoveWindowArgs(tc.args)
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

func TestParseResizeWindowArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		want    protocol.ResizeWindowArgs
		wantErr string
	}{
		{name: "size", args: []string{"101", "1280", "720"}, want: protocol.ResizeWindowArgs{Window: "101", Width: 1280, Height: 720}},
		{name: "too few", args: []string{"101"}, wantErr: "resize-window takes a window reference, a width and a height"},
		{name: "width is not a number", args: []string{"101", "wide", "720"}, wantErr: `width must be an integer (physical pixels), got "wide"`},
		{name: "zero width", args: []string{"101", "0", "720"}, wantErr: "width and height must be between 1 and 1000000"},
		{name: "negative height", args: []string{"101", "640", "-480"}, wantErr: "width and height must be between 1 and 1000000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseResizeWindowArgs(tc.args)
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

func TestWindowActionUsageErrorsExit2WithoutDialing(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"restore-window"}, "usage: winuse restore-window <window>"},
		{[]string{"minimize-window", "101", "102"}, "usage: winuse minimize-window <window>"},
		{[]string{"close-window"}, "usage: winuse close-window <window>"},
		{[]string{"move-window", "101", "x", "0"}, "usage: winuse move-window <window> <x> <y>"},
		{[]string{"resize-window", "101", "0", "0"}, "usage: winuse resize-window <window> <width> <height>"},
		{[]string{"--headed", "close-window", "101", "102"}, "usage: winuse close-window <window>"},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
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

func TestUsageListsWindowActions(t *testing.T) {
	for _, want := range []string{
		"restore-window <window>", "minimize-window <window>", "move-window <window> <x> <y>",
		"resize-window <window> <width> <height>", "close-window <window>",
	} {
		if !strings.Contains(Usage, want) {
			t.Errorf("usage text missing %q", want)
		}
	}
}
