package cli

// Offline tests for the tab commands' CLI side (browser merge ticket 07):
// parsing, usage errors (exit 2, the helper never dialed), the batch step
// forms, the usage text and the budgets. The helper registry test checks
// the same wire names.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestParseTabArgs(t *testing.T) {
	for _, c := range []struct {
		args    []string
		cmd     string
		want    protocol.TabArgs
		wantErr string
	}{
		{args: []string{"list", "301"}, cmd: "tab-list", want: protocol.TabArgs{Window: "301"}},
		{args: []string{"new", "301"}, cmd: "tab-new", want: protocol.TabArgs{Window: "301"}},
		{args: []string{"new", "chrome", "example.com"}, cmd: "tab-new", want: protocol.TabArgs{Window: "chrome", URL: "example.com"}},
		{args: []string{"close", "301", "1a2b3c4d"}, cmd: "tab-close", want: protocol.TabArgs{Window: "301", Tab: "1a2b3c4d"}},
		{args: []string{"switch", "301", "1a2b"}, cmd: "tab-switch", want: protocol.TabArgs{Window: "301", Tab: "1a2b"}},
		{args: nil, wantErr: "tab needs a subcommand: list, new, close or switch"},
		{args: []string{"open", "301"}, wantErr: `unknown tab subcommand "open"`},
		{args: []string{"list"}, wantErr: "tab list takes a window reference"},
		{args: []string{"list", "301", "extra"}, wantErr: "tab list takes a window reference"},
		{args: []string{"new"}, wantErr: "tab new takes a window reference and at most one URL"},
		{args: []string{"new", "301", "a.test", "b.test"}, wantErr: "tab new takes a window reference and at most one URL"},
		{args: []string{"close", "301"}, wantErr: "tab close takes a window reference and a tab id from winuse tab list"},
		{args: []string{"switch", "301", "1a2"}, wantErr: `tab id "1a2" is too short`},
		{args: []string{"switch", "301", "1a2b", "--force"}, wantErr: "tab switch has no option --force"},
	} {
		cmd, got, err := parseTabArgs(c.args)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%q: err = %v, want %q", c.args, err, c.wantErr)
			}
			continue
		}
		if err != nil || cmd != c.cmd || got != c.want {
			t.Errorf("%q: %s %+v, %v", c.args, cmd, got, err)
		}
	}
}

func TestTabUsageErrorsExit2(t *testing.T) {
	for _, args := range [][]string{{"tab"}, {"tab", "close", "301"}, {"tab", "switch", "301", "ab"}} {
		path := filepath.Join(t.TempDir(), "stderr")
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		oldStderr, oldStdout := os.Stderr, os.Stdout
		os.Stderr, os.Stdout = f, f
		code := Main(args)
		os.Stderr, os.Stdout = oldStderr, oldStdout
		f.Close()
		got, _ := os.ReadFile(path)
		if code != 2 || !strings.Contains(string(got), "usage: winuse tab list <window>") {
			t.Errorf("%q: exit %d, %q", args, code, got)
		}
	}
}

func TestBatchTabSteps(t *testing.T) {
	_, steps, err := parseBatchArgs([]string{"301", "tab", "new", "a.test", "then", "tab", "switch", "1a2b", "then", "tab-close", "9f2c61ab", "then", "tab", "new"})
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ kind, args string }{
		{"tab-new", `{"window":"","url":"a.test"}`},
		{"tab-switch", `{"window":"","tab":"1a2b"}`},
		{"tab-close", `{"window":"","tab":"9f2c61ab"}`},
		{"tab-new", `{"window":""}`},
	}
	if len(steps) != len(want) {
		t.Fatalf("steps = %+v", steps)
	}
	for i, w := range want {
		var got, exp any
		json.Unmarshal(steps[i].Args, &got)
		json.Unmarshal([]byte(w.args), &exp)
		if steps[i].Kind != w.kind || !reflect.DeepEqual(got, exp) {
			t.Errorf("step %d = %s %s, want %s %s", i, steps[i].Kind, steps[i].Args, w.kind, w.args)
		}
	}
	for _, c := range []struct {
		args []string
		err  string
	}{
		{[]string{"301", "tab", "list"}, "tab list is not a batch Action"},
		{[]string{"301", "tab"}, "a batch tab Action needs new, close or switch"},
		{[]string{"301", "tab", "switch"}, "a batch tab switch Action takes a tab id"},
		{[]string{"301", "tab", "close", "ab"}, "is too short"},
		{[]string{"301", "tab", "move", "1a2b"}, `unknown batch tab Action "move"`},
	} {
		if _, _, err := parseBatchArgs(c.args); err == nil || !strings.Contains(err.Error(), c.err) {
			t.Errorf("%q: %v, want %q", c.args, err, c.err)
		}
	}
	// Every wire kind a tab step produces is one the helper takes.
	for _, kind := range []string{"tab-new", "tab-close", "tab-switch"} {
		found := false
		for _, k := range protocol.BatchStepKinds {
			found = found || k == kind
		}
		if !found {
			t.Errorf("%s is not in BatchStepKinds", kind)
		}
	}
}

func TestUsageMentionsTabCommands(t *testing.T) {
	for _, s := range []string{"tab list <window>", "tab new <window> [url]", "tab close <window> <tab>", "tab switch <window> <tab>",
		"tab switch is headless through the\n  winuse extension"} {
		if !strings.Contains(Usage, s) {
			t.Errorf("usage text missing %q", s)
		}
	}
	for _, cmd := range []string{"tab-list", "tab-new", "tab-close", "tab-switch"} {
		if d, ok := protocol.CommandTimeout(cmd); !ok || d != protocol.BrowserTimeout {
			t.Errorf("%s budget = %v, %v", cmd, d, ok)
		}
	}
}
