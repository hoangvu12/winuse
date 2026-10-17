package cli

// Offline tests for the pointer commands' argument parsing (scroll, drag,
// hover): the @x,y point syntax, negative scroll amounts, and the usage
// errors. None of these dial the helper, so no desktop is touched.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseScrollArgs(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantWindow string
		wantX      int
		wantY      int
		wantDX     int
		wantDY     int
		wantErr    string
	}{
		{name: "down and right", args: []string{"101", "@400,235", "240", "600"},
			wantWindow: "101", wantX: 400, wantY: 235, wantDX: 240, wantDY: 600},
		{name: "up and left (negative amounts)", args: []string{"notepad", "@10,10", "-240", "-360"},
			wantWindow: "notepad", wantX: 10, wantY: 10, wantDX: -240, wantDY: -360},
		{name: "spaces around the point parts", args: []string{"101", "@ 400 , 235 ", "0", "120"},
			wantWindow: "101", wantX: 400, wantY: 235, wantDX: 0, wantDY: 120},
		{name: "zero and zero parses", args: []string{"101", "@1,1", "0", "0"},
			wantWindow: "101", wantX: 1, wantY: 1, wantDX: 0, wantDY: 0},

		{name: "too few arguments", args: []string{"101", "@1,1", "120"},
			wantErr: "scroll takes a window reference, an @x,y point and two scroll amounts"},
		{name: "too many arguments", args: []string{"101", "@1,1", "120", "120", "extra"},
			wantErr: "scroll takes a window reference, an @x,y point and two scroll amounts"},
		{name: "point without @", args: []string{"101", "400,235", "120", "120"},
			wantErr: "a coordinate reference must be @x,y"},
		{name: "point with one coordinate", args: []string{"101", "@400", "120", "120"},
			wantErr: "a coordinate reference must be @x,y"},
		{name: "negative point", args: []string{"101", "@-1,5", "120", "120"},
			wantErr: "a coordinate reference must be @x,y"},
		{name: "amount is not a number", args: []string{"101", "@1,1", "lots", "120"},
			wantErr: `the scroll amount must be an integer, got "lots"`},
		{name: "vertical amount is not a number", args: []string{"101", "@1,1", "120", "down"},
			wantErr: `the scroll amount must be an integer, got "down"`},
		{name: "no arguments", args: nil,
			wantErr: "scroll takes a window reference, an @x,y point and two scroll amounts"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			window, x, y, dx, dy, err := parseScrollArgs(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if window != tc.wantWindow || x != tc.wantX || y != tc.wantY || dx != tc.wantDX || dy != tc.wantDY {
				t.Errorf("parsed %q @%d,%d %d,%d, want %q @%d,%d %d,%d",
					window, x, y, dx, dy, tc.wantWindow, tc.wantX, tc.wantY, tc.wantDX, tc.wantDY)
			}
		})
	}
}

func TestParseDragArgs(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantWindow string
		wantFromX  int
		wantFromY  int
		wantToX    int
		wantToY    int
		wantErr    string
	}{
		{name: "from and to", args: []string{"101", "@100,50", "@400,235"},
			wantWindow: "101", wantFromX: 100, wantFromY: 50, wantToX: 400, wantToY: 235},
		{name: "same point", args: []string{"notepad", "@5,5", "@5,5"},
			wantWindow: "notepad", wantFromX: 5, wantFromY: 5, wantToX: 5, wantToY: 5},

		{name: "too few arguments", args: []string{"101", "@1,1"},
			wantErr: "drag takes a window reference and two @x,y points (from, to)"},
		{name: "too many arguments", args: []string{"101", "@1,1", "@2,2", "@3,3"},
			wantErr: "drag takes a window reference and two @x,y points (from, to)"},
		{name: "bad from point", args: []string{"101", "1,1", "@2,2"},
			wantErr: "a coordinate reference must be @x,y"},
		{name: "bad to point", args: []string{"101", "@1,1", "2,2"},
			wantErr: "a coordinate reference must be @x,y"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			window, fx, fy, tx, ty, err := parseDragArgs(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if window != tc.wantWindow || fx != tc.wantFromX || fy != tc.wantFromY || tx != tc.wantToX || ty != tc.wantToY {
				t.Errorf("parsed %q (%d,%d)→(%d,%d), want %q (%d,%d)→(%d,%d)",
					window, fx, fy, tx, ty, tc.wantWindow, tc.wantFromX, tc.wantFromY, tc.wantToX, tc.wantToY)
			}
		})
	}
}

func TestParseHoverArgs(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantWindow string
		wantX      int
		wantY      int
		wantErr    string
	}{
		{name: "point", args: []string{"101", "@200,42"},
			wantWindow: "101", wantX: 200, wantY: 42},
		{name: "app name", args: []string{"notepad", "@0,0"},
			wantWindow: "notepad", wantX: 0, wantY: 0},

		{name: "too few arguments", args: []string{"101"},
			wantErr: "hover takes a window reference and an @x,y point"},
		{name: "too many arguments", args: []string{"101", "@1,1", "@2,2"},
			wantErr: "hover takes a window reference and an @x,y point"},
		{name: "bad point", args: []string{"101", "200,42"},
			wantErr: "a coordinate reference must be @x,y"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			window, x, y, err := parseHoverArgs(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if window != tc.wantWindow || x != tc.wantX || y != tc.wantY {
				t.Errorf("parsed %q @%d,%d, want %q @%d,%d", window, x, y, tc.wantWindow, tc.wantX, tc.wantY)
			}
		})
	}
}

// TestPointerUsageErrorsExit2WithoutDialing pins the dispatch: usage
// failures exit 2 and never reach the helper.
func TestPointerUsageErrorsExit2WithoutDialing(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"scroll with no arguments", []string{"scroll"}, scrollUsage},
		{"scroll with a bad point", []string{"scroll", "101", "1,2", "120", "120"}, "a coordinate reference must be @x,y"},
		{"scroll with a bad amount", []string{"scroll", "101", "@1,2", "many", "120"}, "the scroll amount must be an integer"},
		{"drag with one point", []string{"drag", "101", "@1,1"}, dragUsage},
		{"hover with no point", []string{"hover", "101"}, hoverUsage},
		{"hover with a bad point", []string{"hover", "101", "1,1"}, "a coordinate reference must be @x,y"},
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

func TestUsageMentionsPointerCommands(t *testing.T) {
	for _, cmd := range []string{"scroll", "drag", "hover"} {
		if !strings.Contains(usage, cmd) {
			t.Errorf("usage text missing %q", cmd)
		}
	}
}
