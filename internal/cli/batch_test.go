package cli

// The batch command's CLI-side tests (ticket 11): step splitting at
// `then`, every Action's step parsing (the same arguments the standalone
// command takes, without the Window), the usage errors that exit 2
// without dialing the helper, and the wire Step each command-line shape
// builds.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestParseBatchArgsSplitsAtThen(t *testing.T) {
	window, steps, err := parseBatchArgs([]string{
		"101", "click", "name:OK", "then", "type-text", "hello", "world",
		"THEN", "press-key", "Return", "then", "set-value", "id:field", "a b",
	})
	if err != nil {
		t.Fatalf("parseBatchArgs: %v", err)
	}
	if window != "101" {
		t.Errorf("window = %q, want 101", window)
	}
	wantKinds := []string{"click", "type-text", "press-key", "set-value"}
	if len(steps) != len(wantKinds) {
		t.Fatalf("steps = %d, want %d (%v)", len(steps), len(wantKinds), steps)
	}
	for i, kind := range wantKinds {
		if steps[i].Kind != kind {
			t.Errorf("step %d kind = %q, want %q", i, steps[i].Kind, kind)
		}
	}
	// Variadic arguments join inside their step.
	var tt protocol.TypeTextArgs
	if err := json.Unmarshal(steps[1].Args, &tt); err != nil || tt.Text != "hello world" {
		t.Errorf("type-text step text = %q, %v; want \"hello world\"", tt.Text, err)
	}
	var sv protocol.SetValueArgs
	if err := json.Unmarshal(steps[3].Args, &sv); err != nil || sv.Ref != "id:field" || sv.Value != "a b" {
		t.Errorf("set-value step = %+v, %v; want ref id:field, value \"a b\"", sv, err)
	}
}

func TestParseBatchArgsSingleStepAndNoThenInsideQuotedText(t *testing.T) {
	// One step: the same shape as the standalone command, minus the
	// Window. A `then` inside a quoted argument (one shell argument) stays
	// part of the text.
	window, steps, err := parseBatchArgs([]string{"notepad", "type-text", "first then second"})
	if err != nil {
		t.Fatalf("parseBatchArgs: %v", err)
	}
	if window != "notepad" || len(steps) != 1 || steps[0].Kind != "type-text" {
		t.Fatalf("parse = %q, %+v", window, steps)
	}
	var tt protocol.TypeTextArgs
	if err := json.Unmarshal(steps[0].Args, &tt); err != nil || tt.Text != "first then second" {
		t.Errorf("text = %q, %v; want \"first then second\"", tt.Text, err)
	}
}

func intPtr(v int) *int { return &v }

func TestParseBatchStepClick(t *testing.T) {
	cases := []struct {
		name    string
		tokens  []string
		want    protocol.ClickArgs
		wantErr string
	}{
		{name: "element index", tokens: []string{"click", "3"},
			want: protocol.ClickArgs{ElementIndex: intPtr(3), ClickCount: 1, MouseButton: "left"}},
		{name: "name reference", tokens: []string{"click", "name:OK"},
			want: protocol.ClickArgs{Ref: "name:OK", ClickCount: 1, MouseButton: "left"}},
		{name: "coordinate", tokens: []string{"click", "@12,34"},
			want: protocol.ClickArgs{Point: &protocol.Point{X: 12, Y: 34}, ClickCount: 1, MouseButton: "left"}},
		{name: "flags", tokens: []string{"click", "name:OK", "--count", "2", "--button", "right"},
			want: protocol.ClickArgs{Ref: "name:OK", ClickCount: 2, MouseButton: "right"}},
		{name: "flags before the target", tokens: []string{"click", "--count=2", "id:b"},
			want: protocol.ClickArgs{Ref: "id:b", ClickCount: 2, MouseButton: "left"}},
		{name: "no target", tokens: []string{"click"}, wantErr: "a batch click step takes one element index"},
		{name: "unquoted reference", tokens: []string{"click", "name:New", "Document"},
			wantErr: "must be one argument"},
		{name: "unknown flag", tokens: []string{"click", "1", "--speed"}, wantErr: "unknown flag --speed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			step, err := parseBatchStep(tc.tokens)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseBatchStep: %v", err)
			}
			if step.Kind != "click" {
				t.Fatalf("kind = %q", step.Kind)
			}
			var got protocol.ClickArgs
			if err := json.Unmarshal(step.Args, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("click step = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseBatchStepRemainingActions(t *testing.T) {
	cases := []struct {
		name    string
		tokens  []string
		want    any
		wantErr string
	}{
		{name: "type-text joins its step", tokens: []string{"type-text", "hello", "world"},
			want: protocol.TypeTextArgs{Text: "hello world"}},
		{name: "type-text needs text", tokens: []string{"type-text"}, wantErr: "a batch type-text step needs text"},
		{name: "press-key", tokens: []string{"press-key", "Control_L+a"},
			want: protocol.PressKeyArgs{Key: "Control_L+a"}},
		{name: "press-key needs a key", tokens: []string{"press-key"}, wantErr: "a batch press-key step needs a key"},
		{name: "set-value with a name reference", tokens: []string{"set-value", "name:Text Field", "hello", "world"},
			want: protocol.SetValueArgs{Ref: "name:Text Field", Value: "hello world"}},
		{name: "set-value with an index", tokens: []string{"set-value", "5", "42"},
			want: protocol.SetValueArgs{ElementIndex: intPtr(5), Value: "42"}},
		{name: "set-value needs a value", tokens: []string{"set-value", "5"}, wantErr: "a batch set-value step takes an element index"},
		{name: "perform-secondary-action", tokens: []string{"perform-secondary-action", "id:details", "scroll", "down"},
			want: protocol.SecondaryActionArgs{Ref: "id:details", Action: "scroll down"}},
		{name: "perform-secondary-action needs an action", tokens: []string{"perform-secondary-action", "id:details"},
			wantErr: "a batch perform-secondary-action step takes an element index"},
		{name: "activate-window takes no arguments", tokens: []string{"activate-window"},
			want: protocol.Step{Kind: "activate-window"}},
		{name: "activate-window rejects arguments", tokens: []string{"activate-window", "101"},
			wantErr: "a batch activate-window step takes no arguments"},
		{name: "unknown action", tokens: []string{"get-window-state", "101"},
			wantErr: `unknown batch action "get-window-state"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			step, err := parseBatchStep(tc.tokens)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseBatchStep: %v", err)
			}
			if want, ok := tc.want.(protocol.Step); ok { // activate-window: no arguments at all
				if step.Args != nil {
					t.Errorf("activate-window step args = %s, want none", step.Args)
				}
				if step.Kind != want.Kind {
					t.Errorf("kind = %q, want %q", step.Kind, want.Kind)
				}
				return
			}
			raw, merr := json.Marshal(tc.want)
			if merr != nil {
				t.Fatal(merr)
			}
			var got, want map[string]any
			if err := json.Unmarshal(step.Args, &got); err != nil {
				t.Fatalf("unmarshal step args: %v", err)
			}
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatalf("unmarshal want: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("step args = %s, want %s", step.Args, raw)
			}
			if step.Args == nil {
				t.Error("a step with arguments must carry them")
			}
		})
	}
}

func TestParseBatchArgsErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no arguments", nil, "batch takes a window reference and at least one action step"},
		{"window only", []string{"101"}, "batch takes a window reference and at least one action step"},
		{"leading then", []string{"101", "then", "click", "1"}, "a batch step is empty"},
		{"trailing then", []string{"101", "click", "1", "then"}, "a batch step is empty"},
		{"double then", []string{"101", "click", "1", "then", "then", "click", "2"}, "a batch step is empty"},
		{"unknown action in a later step", []string{"101", "click", "1", "then", "zoom", "1,2,3,4"},
			`unknown batch action "zoom"`},
		{"a bad step flag in a later step", []string{"101", "click", "1", "then", "click", "2", "--count", "0"},
			"--count needs a value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := parseBatchArgs(tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestBatchUsageErrorsExit2WithoutDialing(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"batch with no arguments", []string{"batch"}, batchUsage},
		{"batch with a window only", []string{"batch", "101"}, batchUsage},
		{"batch with an empty step", []string{"batch", "101", "then", "click", "1"}, "a batch step is empty"},
		{"batch with an unknown action", []string{"batch", "101", "scroll", "5"}, "unknown batch action"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Usage errors write to stderr and never dial the helper, so
			// no desktop interaction is possible from these paths.
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

func TestUsageMentionsBatch(t *testing.T) {
	for _, want := range []string{"batch", "then"} {
		if !strings.Contains(usage, want) {
			t.Errorf("usage text missing %q", want)
		}
	}
}
