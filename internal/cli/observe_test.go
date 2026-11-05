package cli

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestExtractObserve(t *testing.T) {
	abs, _ := filepath.Abs("seen.txt")
	cases := []struct {
		name    string
		args    []string
		rest    []string
		want    *protocol.Observe
		wantErr string
	}{
		{name: "none", args: []string{"click", "101", "5"}, rest: []string{"click", "101", "5"}},
		{name: "bare is diff", args: []string{"click", "101", "5", "--observe"},
			rest: []string{"click", "101", "5"}, want: &protocol.Observe{Mode: "diff"}},
		{name: "full with budgets anywhere", args: []string{"set-value", "--observe=full", "--interactive", "101", "3", "hi", "--max-chars", "2000"},
			rest: []string{"set-value", "101", "3", "hi", "--max-chars", "2000"}, want: &protocol.Observe{Mode: "full", Interactive: true}},
		{name: "type-text keeps its text", args: []string{"type-text", "--observe", "101", "say", "--compact"},
			rest: []string{"type-text", "101", "say", "--compact"}, want: &protocol.Observe{Mode: "diff"}},
		{name: "before the command", args: []string{"--observe=full", "set-value", "101", "id:qty", "3 --observe"},
			rest: []string{"set-value", "101", "id:qty", "3 --observe"}, want: &protocol.Observe{Mode: "full"}},
		{name: "set-value's value is text", args: []string{"set-value", "101", "id:qty", "3", "--observe"},
			rest: []string{"set-value", "101", "id:qty", "3", "--observe"}},
		{name: "before a read", args: []string{"--observe", "find", "101"},
			wantErr: "find does not take --observe"},
		{name: "full and no cap", args: []string{"click", "101", "5", "--observe=full", "--full"},
			rest: []string{"click", "101", "5"}, want: &protocol.Observe{Mode: "full", Full: true}},
		{name: "max-chars 0 lifts the cap", args: []string{"click", "101", "5", "--observe", "--max-chars", "0"},
			rest: []string{"click", "101", "5"}, want: &protocol.Observe{Mode: "diff", MaxChars: charsOf(0)}},
		{name: "out is absolute", args: []string{"press-key", "101", "Return", "--observe", "--out", "seen.txt"},
			rest: []string{"press-key", "101", "Return"}, want: &protocol.Observe{Mode: "diff", Out: abs}},
		{name: "batch before its first Action", args: []string{"batch", "101", "--observe", "--stop-on-error", "--depth", "3", "click", "5", "then", "get-window-state", "--interactive"},
			rest: []string{"batch", "101", "--stop-on-error", "click", "5", "then", "get-window-state", "--interactive"}, want: &protocol.Observe{Mode: "diff", Depth: 3}},
		{name: "batch from stdin", args: []string{"batch", "-", "--observe=full"},
			rest: []string{"batch", "-"}, want: &protocol.Observe{Mode: "full"}},
		{name: "get-window-state keeps its budgets", args: []string{"get-window-state", "101", "--interactive"},
			rest: []string{"get-window-state", "101", "--interactive"}},
		{name: "a read refuses --observe", args: []string{"get-window-state", "101", "--observe"},
			wantErr: "get-window-state does not take --observe"},
		{name: "budgets need --observe", args: []string{"click", "101", "5", "--interactive"},
			wantErr: "--interactive shapes the observation and goes with --observe"},
		{name: "not a mode", args: []string{"click", "101", "5", "--observe=changes"},
			wantErr: "--observe=changes is not a mode"},
		{name: "twice", args: []string{"click", "101", "5", "--observe", "--observe=full"},
			wantErr: "--observe is given twice"},
		{name: "out and max-chars", args: []string{"click", "101", "5", "--observe", "--out", "x.txt", "--max-chars", "10"},
			wantErr: "pass --out or --max-chars, not both"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rest, o, err := extractObserve(c.args)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("error = %v, want %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(rest, c.rest) {
				t.Errorf("rest = %q, want %q", rest, c.rest)
			}
			if !reflect.DeepEqual(o, c.want) {
				t.Errorf("observe = %+v, want %+v", o, c.want)
			}
		})
	}
}

// The commands the CLI sends --observe with are the ones the helper takes
// it on, and each is a command the CLI runs.
func TestObservableCommandsAreCommands(t *testing.T) {
	for _, cmd := range protocol.ObservableCommands {
		if _, ok := lookupHelp(cmd); !ok {
			t.Errorf("%s takes --observe but has no help entry", cmd)
		}
	}
}

// charsOf is a --max-chars value as the requests carry it.
func charsOf(n int) *int { return &n }
