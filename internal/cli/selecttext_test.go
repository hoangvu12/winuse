package cli

// Offline tests for select-text's CLI side (headless-max ticket 10): the
// span parsing (a start and a length, or all), the usage errors, the Batch
// step and the usage text. None of these dial the helper, so no desktop
// is touched.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestParseSelectTextArgs(t *testing.T) {
	two := 2
	cases := []struct {
		name    string
		args    []string
		want    protocol.SelectTextArgs
		wantErr string
	}{
		{name: "a span", args: []string{"101", "2", "6", "5"},
			want: protocol.SelectTextArgs{Window: "101", ElementIndex: &two, Start: 6, Length: 5}},
		{name: "a caret", args: []string{"101", "id:notes", "0", "0"},
			want: protocol.SelectTextArgs{Window: "101", Ref: "id:notes"}},
		{name: "all in any case", args: []string{"notepad", "name:Text editor", "ALL"},
			want: protocol.SelectTextArgs{Window: "notepad", Ref: "name:Text editor", All: true}},
		{name: "too few", args: []string{"101", "2"}, wantErr: "select-text takes a window reference"},
		{name: "a start alone", args: []string{"101", "2", "6"}, wantErr: `select-text takes a start and a length, or all, after the element; got "6"`},
		{name: "too many", args: []string{"101", "2", "6", "5", "1"}, wantErr: "select-text takes a start and a length, or all"},
		{name: "a point", args: []string{"101", "@4,5", "all"}, wantErr: "not @x,y coordinates"},
		{name: "a negative start", args: []string{"101", "2", "-1", "5"}, wantErr: `the start must be an integer from 0 to 100000000, got "-1"`},
		{name: "a fractional length", args: []string{"101", "2", "1", "2.5"}, wantErr: `the length must be an integer from 0 to 100000000, got "2.5"`},
		{name: "past the bound", args: []string{"101", "2", "100000000", "1"}, wantErr: "start+length must be at most 100000000"},
		{name: "a bad element", args: []string{"101", "x", "all"}, wantErr: "the element reference must be"},
		{name: "an unquoted name", args: []string{"101", "name:Text", "editor", "1", "2"}, wantErr: "quote it"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSelectTextArgs(tc.args)
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

func TestParseBatchStepSelectText(t *testing.T) {
	_, steps, err := parseBatchArgs([]string{"101", "select-text", "3", "6", "5", "then", "type-text", "world", "then", "select-text", "3", "all"})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 3 || steps[0].Kind != "select-text" || steps[2].Kind != "select-text" {
		t.Fatalf("steps = %+v", steps)
	}
	if got, want := string(steps[0].Args), `{"window":"","element_index":3,"start":6,"length":5}`; got != want {
		t.Errorf("step 1 args = %s, want %s", got, want)
	}
	if got, want := string(steps[2].Args), `{"window":"","element_index":3,"start":0,"length":0,"all":true}`; got != want {
		t.Errorf("step 3 args = %s, want %s", got, want)
	}
	if _, _, err := parseBatchArgs([]string{"101", "select-text", "3"}); err == nil || !strings.Contains(err.Error(), "a batch select-text Action") {
		t.Errorf("malformed step: %v", err)
	}
}

func TestSelectTextUsageErrorsExit2WithoutDialing(t *testing.T) {
	for _, args := range [][]string{
		{"select-text"},
		{"select-text", "101", "2"},
		{"select-text", "101", "2", "one", "two"},
		{"--headed", "select-text", "101", "2", "-4", "1"},
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
		if !strings.Contains(string(got), selectTextUsage) {
			t.Errorf("%q: stderr = %q, want the usage line", args, got)
		}
	}
}

func TestUsageListsSelectText(t *testing.T) {
	for _, want := range []string{
		"select-text <window> <element index | name: | id:> <start> <length> | all",
		"select-text and\n  invoke-menu, which have no headed form",
	} {
		if !strings.Contains(Usage, want) {
			t.Errorf("usage text missing %q", want)
		}
	}
}
