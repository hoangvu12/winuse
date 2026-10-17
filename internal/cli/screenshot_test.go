package cli

// The screenshot commands' CLI-side offline tests (ticket 08): argument
// parsing only — no dialing, no helper, no desktop.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestParseWindowStateArgs(t *testing.T) {
	cases := []struct {
		name           string
		args           []string
		wantWindow     string
		wantScreenshot bool
		wantErr        string
	}{
		{name: "window only", args: []string{"101"}, wantWindow: "101"},
		{name: "long flag after", args: []string{"101", "--screenshot"}, wantWindow: "101", wantScreenshot: true},
		{name: "long flag before", args: []string{"--screenshot", "notepad"}, wantWindow: "notepad", wantScreenshot: true},
		{name: "short flag", args: []string{"-s", "101"}, wantWindow: "101", wantScreenshot: true},
		{name: "joined true", args: []string{"101", "--screenshot=true"}, wantWindow: "101", wantScreenshot: true},
		{name: "joined false", args: []string{"101", "--screenshot=false"}, wantWindow: "101"},
		{name: "flag value is not boolean", args: []string{"101", "--screenshot=maybe"},
			wantErr: `--screenshot is a flag (no value), got "--screenshot=maybe"`},
		{name: "unknown flag", args: []string{"101", "--shooting"}, wantErr: "unknown flag --shooting"},
		{name: "no window", args: []string{"--screenshot"}, wantErr: "get-window-state takes one window reference"},
		{name: "two windows", args: []string{"101", "202"}, wantErr: "get-window-state takes one window reference"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			window, screenshot, err := parseWindowStateArgs(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if window != tc.wantWindow || screenshot != tc.wantScreenshot {
				t.Errorf("window/screenshot = %q/%v, want %q/%v", window, screenshot, tc.wantWindow, tc.wantScreenshot)
			}
		})
	}
}

func TestParsePointRef(t *testing.T) {
	cases := []struct {
		raw     string
		wantX   int
		wantY   int
		wantErr string
	}{
		{raw: "@100,200", wantX: 100, wantY: 200},
		{raw: "@0,0", wantX: 0, wantY: 0},
		{raw: "@ 100 , 200 ", wantX: 100, wantY: 200},
		{raw: "@10", wantErr: "must be @x,y"},
		{raw: "@100,200,300", wantErr: "must be @x,y"},
		{raw: "@x,y", wantErr: "must be @x,y"},
		{raw: "@-5,10", wantErr: "must be @x,y"},
		{raw: "@5,-10", wantErr: "must be @x,y"},
		{raw: "@", wantErr: "must be @x,y"},
	}
	for _, tc := range cases {
		x, y, err := parsePointRef(tc.raw)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("parsePointRef(%q) error = %v, want %q", tc.raw, err, tc.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("parsePointRef(%q): %v", tc.raw, err)
			continue
		}
		if x != tc.wantX || y != tc.wantY {
			t.Errorf("parsePointRef(%q) = (%d, %d), want (%d, %d)", tc.raw, x, y, tc.wantX, tc.wantY)
		}
	}
}

func TestParseClickArgsCoordinateForm(t *testing.T) {
	window, target, o, err := parseClickArgs([]string{"101", "@400,235"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if window != "101" || target.index != nil {
		t.Errorf("window/index = %q/%v", window, target.index)
	}
	if target.point == nil || target.point.X != 400 || target.point.Y != 235 {
		t.Fatalf("point = %v, want (400, 235)", target.point)
	}
	if o.count != 1 || o.button != "left" {
		t.Errorf("count/button = %d/%q", o.count, o.button)
	}

	// Flags combine with the coordinate reference.
	_, target, o, err = parseClickArgs([]string{"--button", "right", "--count", "2", "notepad", "@ 1 , 2 "})
	if err != nil {
		t.Fatalf("parse with flags: %v", err)
	}
	if target.point == nil || target.point.X != 1 || target.point.Y != 2 {
		t.Fatalf("point = %v, want (1, 2)", target.point)
	}
	if o.count != 2 || o.button != "right" {
		t.Errorf("count/button = %d/%q", o.count, o.button)
	}

	// Malformed coordinate references are usage errors.
	for _, bad := range []string{"101 @x,y", "101 @1", "101 @1,2,3", "101 @-1,2"} {
		args := strings.Split(bad, " ")
		if _, _, _, err := parseClickArgs(args); err == nil {
			t.Errorf("parseClickArgs(%q) must fail", args)
		}
	}
}

func TestParseRegionArg(t *testing.T) {
	cases := []struct {
		raw     string
		want    protocol.Region
		wantErr string
	}{
		{raw: "100,200,300,150", want: protocol.Region{X: 100, Y: 200, W: 300, H: 150}},
		{raw: " 100 , 200 , 300 , 150 ", want: protocol.Region{X: 100, Y: 200, W: 300, H: 150}},
		{raw: "0,0,1568,784", want: protocol.Region{X: 0, Y: 0, W: 1568, H: 784}},
		{raw: "1,2,3", wantErr: "four comma-separated numbers"},
		{raw: "1,2,3,4,5", wantErr: "four comma-separated numbers"},
		{raw: "a,b,c,d", wantErr: "four comma-separated numbers"},
		{raw: "-1,2,3,4", wantErr: "non-negative"},
		{raw: "1,-2,3,4", wantErr: "non-negative"},
		{raw: "1,2,0,4", wantErr: "at least 1 px"},
		{raw: "1,2,3,0", wantErr: "at least 1 px"},
	}
	for _, tc := range cases {
		got, err := parseRegionArg(tc.raw)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("parseRegionArg(%q) error = %v, want %q", tc.raw, err, tc.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseRegionArg(%q): %v", tc.raw, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseRegionArg(%q) = %+v, want %+v", tc.raw, got, tc.want)
		}
	}
}

func TestZoomUsageErrorsExit2WithoutDialing(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"zoom with no arguments", []string{"zoom"}, zoomUsage},
		{"zoom with one argument", []string{"zoom", "101"}, zoomUsage},
		{"zoom with a bad region", []string{"zoom", "101", "1,2"}, "four comma-separated numbers"},
		{"zoom with a zero-size region", []string{"zoom", "101", "1,2,0,0"}, "at least 1 px"},
		{"zoom with a negative origin", []string{"zoom", "101", "-1,2,5,5"}, "non-negative"},
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

func TestGetWindowStateUsageErrorsExit2WithoutDialing(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no window", []string{"get-window-state"}, "usage: winuse get-window-state <window> [--screenshot]"},
		{"two windows", []string{"get-window-state", "101", "202"}, "get-window-state takes one window reference"},
		{"unknown flag", []string{"get-window-state", "101", "--shot"}, "unknown flag --shot"},
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

func TestUsageMentionsScreenshotCommands(t *testing.T) {
	for _, want := range []string{"--screenshot", "zoom", "@x,y", "screenshot"} {
		if !strings.Contains(usage, want) {
			t.Errorf("usage text missing %q", want)
		}
	}
}
