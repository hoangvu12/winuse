package cli

// The ticket-07 Action commands' CLI-side tests: argument parsing (the
// value and the action name join their trailing arguments, so unquoted
// spaces still form one value), usage errors exiting 2 without dialing
// the helper, and the dispatch wiring.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSetValueArgs(t *testing.T) {
	// Two arguments: usage error.
	if _, _, _, _, _, err := parseSetValueArgs([]string{"101", "3"}); err == nil {
		t.Error("missing value must be a usage error")
	}
	// A bad element reference is a usage error.
	if _, _, _, _, _, err := parseSetValueArgs([]string{"101", "x", "v"}); err == nil {
		t.Error("a non-numeric index must be a usage error")
	}
	if _, _, _, _, _, err := parseSetValueArgs([]string{"101", "-1", "v"}); err == nil {
		t.Error("a negative index must be a usage error")
	}
	// The value is every remaining argument joined with single spaces.
	window, index, _, _, value, err := parseSetValueArgs([]string{"101", "3", "hello"})
	if err != nil || window != "101" || index == nil || *index != 3 || value != "hello" {
		t.Fatalf("parse = %q, %v, %q, %v", window, index, value, err)
	}
	_, _, _, _, value, err = parseSetValueArgs([]string{"notepad", "0", "hello", "world", "  twice"})
	if err != nil || value != "hello world   twice" {
		t.Fatalf("joined value = %q, %v; want the arguments joined with single spaces", value, err)
	}
	// A value that looks like a flag or a negative number is a value.
	_, _, _, _, value, err = parseSetValueArgs([]string{"101", "2", "--not-a-flag"})
	if err != nil || value != "--not-a-flag" {
		t.Fatalf("value = %q, %v; want the raw argument", value, err)
	}
}

func TestParseSetValueArgsElementReferences(t *testing.T) {
	// name: and id: references pass through verbatim for the helper to
	// resolve against the Window's latest Window State.
	window, index, ref, _, value, err := parseSetValueArgs([]string{"101", "name:Text Field", "hello"})
	if err != nil || window != "101" || index != nil || ref != "name:Text Field" || value != "hello" {
		t.Fatalf("parse = %q, %v, %q, %q, %v", window, index, ref, value, err)
	}
	_, _, ref, _, _, err = parseSetValueArgs([]string{"101", "id:text-field", "42"})
	if err != nil || ref != "id:text-field" {
		t.Fatalf("id: reference = %q, %v", ref, err)
	}
	// The @x,y form parses into the point (finding 8).
	if window, index, ref, point, value, err := parseSetValueArgs([]string{"101", "@400,235", "hello"}); err != nil ||
		window != "101" || index != nil || ref != "" || point == nil || point.X != 400 || point.Y != 235 || value != "hello" {
		t.Fatalf("parse @x,y = %q, %v, %q, %v, %q, %v", window, index, ref, point, value, err)
	}
	// A reference with an empty text is a usage error.
	for _, bad := range []string{"name:", "id:"} {
		if _, _, _, _, _, err := parseSetValueArgs([]string{"101", bad, "v"}); err == nil {
			t.Errorf("%q must be a usage error", bad)
		}
	}
}

func TestParseSecondaryActionArgs(t *testing.T) {
	if _, _, _, _, _, err := parseSecondaryActionArgs([]string{"101", "3"}); err == nil {
		t.Error("missing action must be a usage error")
	}
	if _, _, _, _, _, err := parseSecondaryActionArgs([]string{"101", "x", "Expand"}); err == nil {
		t.Error("a non-numeric index must be a usage error")
	}
	// An unquoted two-word action joins into one action name.
	window, index, _, _, action, err := parseSecondaryActionArgs([]string{"101", "3", "scroll", "down"})
	if err != nil || window != "101" || index == nil || *index != 3 || action != "scroll down" {
		t.Fatalf("parse = %q, %v, %q, %v", window, index, action, err)
	}
	if _, _, _, point, action, err := parseSecondaryActionArgs([]string{"101", "@10,20", "Toggle"}); err != nil ||
		point == nil || point.X != 10 || point.Y != 20 || action != "Toggle" {
		t.Fatalf("parse secondary @x,y = point %v, action %q, err %v", point, action, err)
	}
	// A name: reference names the Element.
	if _, _, ref, _, action, err := parseSecondaryActionArgs([]string{"101", "name:File", "expand"}); err != nil ||
		ref != "name:File" || action != "expand" {
		t.Fatalf("parse = %q, %q, %v", ref, action, err)
	}
}

func TestSetValueAndSecondaryUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"set-value without arguments", []string{"set-value"}, setValueUsage},
		{"set-value without a value", []string{"set-value", "101", "3"}, setValueUsage},
		{"set-value with a bad index", []string{"set-value", "101", "x", "v"}, "the element reference must be a non-negative element index, name:<text> or id:<AutomationId>"},
		{"perform-secondary-action without arguments", []string{"perform-secondary-action"}, secondaryActionUsage},
		{"perform-secondary-action without an action", []string{"perform-secondary-action", "101", "3"}, secondaryActionUsage},
		{"perform-secondary-action with a bad index", []string{"perform-secondary-action", "101", "x", "Expand"}, "the element reference must be a non-negative element index, name:<text> or id:<AutomationId>"},
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

func TestUsageMentionsTicket07Commands(t *testing.T) {
	for _, cmd := range []string{"set-value", "perform-secondary-action"} {
		if !strings.Contains(Usage, cmd) {
			t.Errorf("usage text missing %q", cmd)
		}
	}
}

// A value that ends with winuse flags (2026-11-07): set-value, standalone
// and as a batch step, refuses it with exit 2 and a hint that moves the
// flags after the Window; -- before the value keeps it as text. Every case
// is a usage error, so none dials the helper.
func TestSetValueRefusesTrailingFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
		msg  string // in the refusal's first line
		hint string // in the hint line
	}{
		{"--observe", []string{"set-value", "101", "id:text-field", "observed", "--observe"},
			`winuse: set-value's value ends with the winuse flag --observe, but everything after the Element is the value: it would be set to "observed --observe"; nothing was sent`,
			"hint: move it out of the value: winuse set-value 101 --observe id:text-field observed; to set that text, put -- before the value: winuse set-value 101 id:text-field -- observed --observe"},
		{"--observe=full", []string{"set-value", "101", "3", "hello", "world", "--observe=full"},
			`it would be set to "hello world --observe=full"`,
			"hint: move it out of the value: winuse set-value 101 --observe=full 3 hello world;"},
		{"--headed", []string{"set-value", "101", "3", "hello", "--headed"},
			"ends with the winuse flag --headed,",
			"hint: move it out of the value: winuse --headed set-value 101 3 hello;"},
		{"--no-wait", []string{"set-value", "101", "name:Text Field", "hello", "--no-wait"},
			"ends with the winuse flag --no-wait,",
			`hint: move it out of the value: winuse --no-wait set-value 101 "name:Text Field" hello; to set that text, put -- before the value: winuse set-value 101 "name:Text Field" -- hello --no-wait`},
		{"--timeout 5", []string{"set-value", "101", "3", "hello", "--timeout", "5"},
			"ends with the winuse flag --timeout 5,",
			"hint: set-value takes no --timeout (an Action's budget is fixed): winuse set-value 101 3 hello;"},
		{"several flags", []string{"set-value", "101", "3", "hello", "--observe", "--depth", "3", "--headed", "--timeout=5"},
			`ends with the winuse flags --observe --depth 3 --headed --timeout=5, but everything after the Element is the value: it would be set to "hello --observe --depth 3 --headed --timeout=5"`,
			"hint: move them out of the value (set-value takes no --timeout): winuse --headed set-value 101 --observe --depth 3 3 hello;"},
		{"a budget before --observe", []string{"set-value", "101", "3", "hello", "--interactive", "--observe"},
			"ends with the winuse flags --interactive --observe,",
			"winuse set-value 101 --interactive --observe 3 hello;"},
		{"only flags", []string{"set-value", "101", "3", "--observe"},
			`it would be set to "--observe"`,
			"winuse set-value 101 --observe 3 <value>;"},
		{"flags already taken stay in the hint", []string{"--no-wait", "set-value", "101", "--headed", "3", "hello", "--observe"},
			"ends with the winuse flag --observe,",
			"winuse --headed --no-wait set-value 101 --observe 3 hello;"},
		{"an earlier --observe stays in the hint", []string{"set-value", "101", "--observe=full", "--depth", "2", "3", "hello", "--headed"},
			"ends with the winuse flag --headed,",
			"hint: move it out of the value: winuse --headed set-value 101 --observe=full --depth 2 3 hello; to set that text, put -- before the value: winuse set-value 101 --observe=full --depth 2 3 -- hello --headed"},
		{"a batch set-value step", []string{"batch", "101", "click", "3", "then", "set-value", "id:qty", "3", "--observe", "then", "press-key", "Return"},
			`winuse: a batch set-value Action's value ends with the winuse flag --observe, but everything after the Element is the value: it would be set to "3 --observe"; nothing was sent`,
			"hint: move it out of the value: winuse batch 101 --observe click 3 then set-value id:qty 3 then press-key Return; to set that text, put -- before the value: winuse batch 101 click 3 then set-value id:qty -- 3 --observe then press-key Return"},
		{"a batch step's Batch flags", []string{"batch", "101", "--timeout", "30", "set-value", "id:qty", "3", "--stop-on-error", "--timeout", "60"},
			"ends with the winuse flags --stop-on-error --timeout 60,",
			"hint: move them out of the value: winuse batch 101 --timeout 30 --stop-on-error --timeout 60 set-value id:qty 3;"},
		{"a batch step's --headed", []string{"batch", "101", "set-value", "3", "on", "--headed"},
			"ends with the winuse flag --headed,",
			"hint: move it out of the value: winuse --headed batch 101 set-value 3 on;"},
		{"a batch step in its own Window", []string{"batch", "101", "in", "202", "set-value", "3", "on", "--observe"},
			"ends with the winuse flag --observe,",
			"hint: move it out of the value: winuse batch 101 --observe in 202 set-value 3 on;"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !refusedForValueFlags(tc.args) {
				t.Fatal("the line's parse does not refuse it; not running Main, which would dial the helper")
			}
			code, out := runMain(t, tc.args...)
			if code != 2 {
				t.Fatalf("exit = %d, want 2 (usage error); output %q", code, out)
			}
			lines := strings.SplitN(out, "\n", 3)
			if len(lines) < 3 || !strings.Contains(lines[0], tc.msg) || !strings.Contains(lines[1], tc.hint) {
				t.Fatalf("output = %q,\nwant a first line with %q\nand a hint line with %q", out, tc.msg, tc.hint)
			}
			// Both of the hint's command lines parse clean (one with a
			// quoted argument is checked by its expected text above).
			for _, part := range strings.Split(lines[1], "; ") {
				_, cmd, ok := strings.Cut(part, ": winuse ")
				if !ok || strings.Contains(cmd, `"`) {
					continue
				}
				if err := parseOffline(strings.Fields(strings.ReplaceAll(cmd, "<value>", "x"))); err != nil {
					t.Errorf("the hint's line %q does not parse: %v", cmd, err)
				}
			}
		})
	}
}

// refusedForValueFlags reports a line parseOffline refuses for a value
// that ends with winuse flags.
func refusedForValueFlags(line []string) bool {
	var vf *valueFlagsError
	return errors.As(parseOffline(line), &vf)
}

// parseOffline runs a set-value or batch line through Main's offline
// passes and parse, and returns its usage error.
func parseOffline(line []string) error {
	args, _, _ := extractJSON(hoistGlobalFlags(line))
	args, _ = extractNoWait(args)
	args, _ = extractHeaded(args)
	args, _, err := extractObserve(args)
	if err != nil {
		return err
	}
	switch args[0] {
	case "set-value":
		_, _, _, _, _, err = parseSetValueArgs(args[1:])
	case "batch":
		rest, _ := takeBatchStopOnError(args[1:])
		if rest, _, err = takeBatchTimeout(rest); err == nil {
			_, _, err = parseBatchArgs(rest)
		}
	}
	return err
}

func TestSetValueTrailingFlagsInAStdinBatch(t *testing.T) {
	_, _, _, _, err := parseBatchScript("101\nset-value id:qty 3 --observe\n")
	var vf *valueFlagsError
	if !errors.As(err, &vf) || !strings.HasPrefix(err.Error(), "batch stdin line 2: a batch set-value Action's value ends with the winuse flag --observe") {
		t.Fatalf("err = %v, want the trailing-flag refusal on line 2", err)
	}
	if h := vf.hint(nil, nil); !strings.Contains(h, "put them on the command line (winuse batch - --observe)") {
		t.Errorf("hint = %q, want the command line and the first line named", h)
	}
	// One quoted word is text: only whole arguments count.
	_, steps, _, _, err := parseBatchScript("101\nset-value id:qty \"3 --observe\"\n")
	if err != nil || !strings.Contains(string(steps[0].Args), `"value":"3 --observe"`) {
		t.Fatalf("quoted value: steps %v, err %v", steps, err)
	}
}

func TestSetValueValueKeepsFlagLikeText(t *testing.T) {
	cases := []struct {
		name  string
		value []string
		want  string
	}{
		{"a flag mid-value", []string{"run", "--observe", "now"}, "run --observe now"},
		{"a budget without --observe", []string{"make", "it", "--compact"}, "make it --compact"},
		{"an unknown flag", []string{"--not-a-flag"}, "--not-a-flag"},
		{"a --timeout without seconds", []string{"wait", "--timeout", "soon"}, "wait --timeout soon"},
		{"one quoted argument", []string{"hello --observe"}, "hello --observe"},
		{"--stop-on-error outside a batch", []string{"go", "--stop-on-error"}, "go --stop-on-error"},
		{"-- ends the flags", []string{"--", "observed", "--observe"}, "observed --observe"},
		{"-- before a lone flag", []string{"--", "--headed"}, "--headed"},
		{"-- alone is the value", []string{"--"}, "--"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, _, value, err := parseSetValueArgs(append([]string{"101", "3"}, tc.value...))
			if err != nil || value != tc.want {
				t.Errorf("value = %q, %v; want %q", value, err, tc.want)
			}
		})
	}
	// The same in a batch step, where --stop-on-error counts too.
	_, steps, err := parseBatchArgs([]string{"101", "set-value", "3", "--", "a", "--observe", "then", "set-value", "4", "run", "--headed", "now"})
	if err != nil || !strings.Contains(string(steps[0].Args), `"value":"a --observe"`) || !strings.Contains(string(steps[1].Args), `"value":"run --headed now"`) {
		t.Fatalf("batch steps = %v, %v", steps, err)
	}
	if _, _, err := parseBatchArgs([]string{"101", "set-value", "3", "go", "--stop-on-error"}); err == nil {
		t.Error("a batch step's trailing --stop-on-error must be refused")
	}
}

// Flags before the Element are flags, as before: the line's own passes
// (hoistGlobalFlags, extractNoWait, extractHeaded, extractObserve) take
// them, and the value parses clean.
func TestSetValueFlagsBeforeTheElementStillWork(t *testing.T) {
	line := []string{"--headed", "set-value", "101", "--no-wait", "--json", "--observe=full", "--depth", "2", "id:text-field", "observed"}
	args, _, _ := extractJSON(hoistGlobalFlags(line))
	args, noWait := extractNoWait(args)
	args, headed := extractHeaded(args)
	args, o, err := extractObserve(args)
	if err != nil || !noWait || !headed || o == nil || o.Mode != "full" || o.Depth != 2 {
		t.Fatalf("flags: noWait %v, headed %v, observe %+v, err %v", noWait, headed, o, err)
	}
	window, _, ref, _, value, err := parseSetValueArgs(args[1:])
	if err != nil || window != "101" || ref != "id:text-field" || value != "observed" {
		t.Fatalf("parse %q = %q, %q, %q, %v", args, window, ref, value, err)
	}
	// A Batch's, after its Window.
	args, o, err = extractObserve([]string{"batch", "101", "--observe", "--stop-on-error", "set-value", "id:qty", "3"})
	if err != nil || o == nil {
		t.Fatalf("batch observe = %+v, %v", o, err)
	}
	rest, stop := takeBatchStopOnError(args[1:])
	_, steps, err := parseBatchArgs(rest)
	if err != nil || !stop || !strings.Contains(string(steps[0].Args), `"value":"3"`) {
		t.Fatalf("batch = %v, stop %v, %v", steps, stop, err)
	}
}
