package cli

// The global --headed flag's parsing (ADR 0010): accepted before the
// command and anywhere after it, never taken out of a literal-text
// position (type-text's text, set-value's value, the same in Batch
// steps), and carried on the wire by the Actions that have a Headless
// form.

import (
	"strings"
	"testing"
)

func TestExtractHeadedPositions(t *testing.T) {
	cases := []struct {
		name   string
		args   string // space-separated; "_" stands for one argument with a space
		want   string
		headed bool
	}{
		{"absent", "click 101 3", "click 101 3", false},
		{"before the command", "--headed click 101 3", "click 101 3", true},
		{"repeated before", "--headed --headed click 101 3", "click 101 3", true},
		{"after the command", "click --headed 101 3", "click 101 3", true},
		{"between positionals", "click 101 --headed 3", "click 101 3", true},
		{"at the end", "click 101 3 --headed", "click 101 3", true},
		{"among click flags", "click 101 3 --count 1 --headed --button left", "click 101 3 --count 1 --button left", true},
		{"secondary action at the end", "perform-secondary-action 101 3 scroll down --headed", "perform-secondary-action 101 3 scroll down", true},
		{"press-key at the end", "press-key 101 Return --headed", "press-key 101 Return", true},
		{"observation command", "list-windows --headed", "list-windows", true},
		{"only the flag", "--headed", "", true},

		// Literal text is never eaten.
		{"type-text text", "type-text 101 --headed", "type-text 101 --headed", false},
		{"type-text text middle", "type-text 101 use --headed now", "type-text 101 use --headed now", false},
		{"type-text flag before the window", "type-text --headed 101 hi", "type-text 101 hi", true},
		{"type-text flag before the command", "--headed type-text 101 --headed", "type-text 101 --headed", true},
		{"set-value value", "set-value 101 3 --headed", "set-value 101 3 --headed", false},
		{"set-value value after text", "set-value 101 3 hello --headed", "set-value 101 3 hello --headed", false},
		{"set-value flag before the value", "set-value 101 --headed 3 hello", "set-value 101 3 hello", true},

		// Batches: the flag anywhere outside literal text makes the whole
		// Batch Headed.
		{"batch before the window", "batch --headed 101 click 3", "batch 101 click 3", true},
		{"batch before the first step", "batch 101 --headed click 3", "batch 101 click 3", true},
		{"batch inside a click step", "batch 101 click 3 --headed then press-key Return", "batch 101 click 3 then press-key Return", true},
		{"batch after then", "batch 101 click 3 then --headed set-value 4 x", "batch 101 click 3 then set-value 4 x", true},
		{"batch type-text text", "batch 101 type-text --headed then click 3", "batch 101 type-text --headed then click 3", false},
		{"batch set-value value", "batch 101 set-value 4 --headed", "batch 101 set-value 4 --headed", false},
		{"batch set-value flag before the ref", "batch 101 set-value --headed 4 x", "batch 101 set-value 4 x", true},
		{"batch text then flag in the next step", "batch 101 type-text --headed THEN click 3 --headed", "batch 101 type-text --headed THEN click 3", true},
		// type-text --into (desktop-parity ticket 23): the text starts after
		// the Element, so a --headed before it is the flag.
		{"type-text --into text", "type-text 101 --into 3 --headed", "type-text 101 --into 3 --headed", false},
		{"type-text --into flag before the element", "type-text 101 --into --headed 3 hi", "type-text 101 --into 3 hi", true},
		{"type-text --into flag before --into", "type-text 101 --headed --into 3 hi", "type-text 101 --into 3 hi", true},
		{"batch type-text --into text", "batch 101 type-text --into 3 --headed", "batch 101 type-text --into 3 --headed", false},
		{"batch type-text --into flag before the element", "batch 101 type-text --into --headed 3 hi", "batch 101 type-text --into 3 hi", true},
		{"batch type-text text that says --into", "batch 101 type-text hi --into --headed", "batch 101 type-text hi --into --headed", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, headed := extractHeaded(split(tc.args))
			if strings.Join(got, " ") != tc.want || headed != tc.headed {
				t.Errorf("extractHeaded(%q) = %q, %v; want %q, %v", tc.args, strings.Join(got, " "), headed, tc.want, tc.headed)
			}
		})
	}
}

func TestExtractHeadedKeepsQuotedTextWhole(t *testing.T) {
	// One shell argument holding the flag's spelling and more stays one
	// argument everywhere; only the exact token is the flag.
	got, headed := extractHeaded([]string{"click", "101", "name:--headed button"})
	if headed || len(got) != 3 || got[2] != "name:--headed button" {
		t.Errorf("got %q, %v; the reference must pass untouched", got, headed)
	}
}

func TestHeadedFlagReachesTheParsers(t *testing.T) {
	// After extraction the per-command parsers see the same arguments as
	// without the flag.
	rest, headed := extractHeaded(split("set-value 101 --headed name:Search hello world"))
	if !headed {
		t.Fatal("the flag before the value must be taken")
	}
	window, _, ref, _, value, err := parseSetValueArgs(rest[1:])
	if err != nil || window != "101" || ref != "name:Search" || value != "hello world" {
		t.Errorf("parse = %q %q %q %v", window, ref, value, err)
	}
	rest, headed = extractHeaded(split("batch 101 --headed click 3 then type-text a --headed b"))
	if !headed {
		t.Fatal("the batch flag must be taken")
	}
	_, steps, err := parseBatchArgs(rest[1:])
	if err != nil || len(steps) != 2 || !strings.Contains(string(steps[1].Args), `"a --headed b"`) {
		t.Errorf("batch parse = %v, %v; want the text kept literal", steps, err)
	}
}

func TestUsageDescribesHeaded(t *testing.T) {
	for _, want := range []string{"usage: winuse [--headed] <command>", "global options:", "--headed", "headless"} {
		if !strings.Contains(Usage, want) {
			t.Errorf("usage text missing %q", want)
		}
	}
}

// split splits a test command line at spaces ("" is no arguments).
func split(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, " ")
}
