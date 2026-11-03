package cli

// Offline tests for invoke-menu's CLI side (headless-max ticket 08): the
// path parsing, the usage errors, the Batch step and the usage text. None
// of these dial the helper, so no desktop is touched.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestParseInvokeMenuArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		want    protocol.InvokeMenuArgs
		wantErr string
	}{
		{name: "one quoted path", args: []string{"101", "File > Save As..."},
			want: protocol.InvokeMenuArgs{Window: "101", Path: []string{"File", "Save As..."}}},
		{name: "the path split over arguments", args: []string{"101", "Edit", ">", "Select", "All"},
			want: protocol.InvokeMenuArgs{Window: "101", Path: []string{"Edit", "Select All"}}},
		{name: "no spaces around >", args: []string{"app", "View>Zoom>Zoom In"},
			want: protocol.InvokeMenuArgs{Window: "app", Path: []string{"View", "Zoom", "Zoom In"}}},
		{name: "a bar item alone", args: []string{"101", "Help"},
			want: protocol.InvokeMenuArgs{Window: "101", Path: []string{"Help"}}},
		{name: "no path", args: []string{"101"}, wantErr: "invoke-menu takes a window reference and a menu path"},
		{name: "a blank path", args: []string{"101", "  "}, wantErr: "invoke-menu takes a menu path"},
		{name: "an empty name", args: []string{"101", "File >> Save"}, wantErr: "has an empty item name"},
		{name: "a trailing >", args: []string{"101", "File >"}, wantErr: "has an empty item name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseInvokeMenuArgs(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			gb, _ := json.Marshal(got)
			wb, _ := json.Marshal(tc.want)
			if string(gb) != string(wb) {
				t.Errorf("args = %s, want %s", gb, wb)
			}
		})
	}
}

func TestParseBatchStepInvokeMenu(t *testing.T) {
	_, steps, err := parseBatchArgs([]string{"101", "invoke-menu", "Edit > Select All", "then", "invoke-menu", "File", ">", "Save"})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0].Kind != "invoke-menu" || steps[1].Kind != "invoke-menu" {
		t.Fatalf("steps = %+v", steps)
	}
	if got, want := string(steps[0].Args), `{"window":"","path":["Edit","Select All"]}`; got != want {
		t.Errorf("step 1 args = %s, want %s", got, want)
	}
	if got, want := string(steps[1].Args), `{"window":"","path":["File","Save"]}`; got != want {
		t.Errorf("step 2 args = %s, want %s", got, want)
	}
	if _, _, err := parseBatchArgs([]string{"101", "invoke-menu", "File >"}); err == nil || !strings.Contains(err.Error(), "a batch invoke-menu Action") {
		t.Errorf("malformed step: %v", err)
	}
}

func TestInvokeMenuUsageErrorsExit2WithoutDialing(t *testing.T) {
	for _, args := range [][]string{
		{"invoke-menu"},
		{"invoke-menu", "101"},
		{"invoke-menu", "101", "> Save"},
	} {
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
			t.Fatalf("%q: exit = %d, want 2 (usage error)", args, code)
		}
		got, _ := os.ReadFile(path)
		if !strings.Contains(string(got), invokeMenuUsage) {
			t.Errorf("%q: stderr = %q, want the usage line", args, got)
		}
	}
}

func TestUsageListsInvokeMenu(t *testing.T) {
	for _, want := range []string{
		`invoke-menu <window> "<menu> > <item>"`,
		"invoke-menu, which have no headed form",
	} {
		if !strings.Contains(Usage, want) {
			t.Errorf("usage text missing %q", want)
		}
	}
}
