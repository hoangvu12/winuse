package helper

// No Headed Action without --headed (headless-max ticket 21): every path
// in the ticket's inventory, sent without `headed` over the v1 fakes, is
// refused before the pipeline runs, with the exact --headed retry; with
// `headed` the same request reaches the activation.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// headedArgs sets `headed: true` on a command's arguments, as the CLI does
// for --headed: the tests of the Headed pipeline send it, since without it
// an Action with no Headless route is refused (headless-max ticket 21).
func headedArgs(raw json.RawMessage) json.RawMessage {
	out, err := withHeaded(raw)
	if err != nil {
		panic(err)
	}
	return out
}

// wantHeadedRefusal fails unless out and cmdErr are the refusal of an
// Action whose only route is Headed, sent without `headed`: a one-line
// failure ending in "; nothing was sent", with the exact --headed retry in
// the hint (headless-max ticket 21). The caller checks that nothing ran.
func wantHeadedRefusal(t *testing.T, what, out string, cmdErr *Error) {
	t.Helper()
	if out != "" || cmdErr == nil {
		t.Fatalf("%s without --headed: got %q, %v; want the --headed refusal", what, out, cmdErr)
	}
	if !strings.HasSuffix(cmdErr.Msg, "; nothing was sent") {
		t.Errorf("%s without --headed: message = %q, want it to end in \"; nothing was sent\"", what, cmdErr.Msg)
	}
	if !strings.Contains(cmdErr.Hint, "winuse --headed ") {
		t.Errorf("%s without --headed: hint = %q, want the --headed retry", what, cmdErr.Hint)
	}
	if cmdErr.Exit != 1 {
		t.Errorf("%s without --headed: exit = %d, want 1", what, cmdErr.Exit)
	}
}

func TestEveryHeadedOnlyPathIsRefusedWithItsRetry(t *testing.T) {
	two := 2
	cases := []struct {
		name, cmd string
		args      any
		retry     string // the exact command after "winuse --headed "
		alt       string // another way named in the hint, "" for none
	}{
		{"click @x,y", "click", protocol.ClickArgs{Window: "101", Point: &protocol.Point{X: 40, Y: 50}},
			"click 101 @40,50", "name the Element"},
		{"right click @x,y", "click", protocol.ClickArgs{Window: "101", Point: &protocol.Point{X: 40, Y: 50}, MouseButton: "right"},
			"click 101 @40,50 --button right", ""},
		{"double click on an Element", "click", protocol.ClickArgs{Window: "101", ElementIndex: &two, ClickCount: 2},
			"click 101 2 --count 2", ""},
		{"middle click on a reference", "click", protocol.ClickArgs{Window: "101", Ref: "name:Save As", MouseButton: "m"},
			`click 101 "name:Save As" --button middle`, ""},
		{"type-text with no route", "type-text", protocol.TypeTextArgs{Window: "101", Text: "secret"},
			"type-text 101 <text>", "winuse type-text 101 --post <text>"},
		{"press-key with no route", "press-key", protocol.PressKeyArgs{Window: "101", Key: "a"},
			"press-key 101 a", "winuse press-key 101 a --post"},
		{"press-key chord, forced", "press-key", protocol.PressKeyArgs{Window: "101", Key: "Alt_L+F4", Force: true},
			"press-key 101 Alt_L+F4 --force", ""},
		{"set-value @x,y", "set-value", protocol.SetValueArgs{Window: "101", Point: &protocol.Point{X: 7, Y: 8}, Value: "pw"},
			"set-value 101 @7,8 <value>", "name the Element"},
		{"perform-secondary-action @x,y", "perform-secondary-action", protocol.SecondaryActionArgs{Window: "101", Point: &protocol.Point{X: 7, Y: 8}, Action: "scroll down"},
			`perform-secondary-action 101 @7,8 "Scroll Down"`, "name the Element"},
		{"Raise", "perform-secondary-action", protocol.SecondaryActionArgs{Window: "101", ElementIndex: &two, Action: "raise"},
			"perform-secondary-action 101 2 Raise", "activate-window"},
		{"scroll @x,y", "scroll", protocol.ScrollArgs{Window: "101", X: 3, Y: 4, ScrollY: 120},
			"scroll 101 @3,4 0 120", "name an Element inside the list"},
		{"drag @x,y @x,y", "drag", protocol.DragArgs{Window: "101", FromX: 1, FromY: 2, ToX: 30, ToY: 40},
			"drag 101 @1,2 @30,40", ""},
		{"hover @x,y", "hover", protocol.HoverArgs{Window: "101", X: 9, Y: 10},
			"hover 101 @9,10", ""},
	}
	for _, tc := range cases {
		spy := newActorSpy()
		cmds := CommandsWith(spy.svc)
		out, cmdErr := runCommand(t, cmds, tc.cmd, tc.args)
		wantHeadedRefusal(t, tc.name, out, cmdErr)
		if cmdErr == nil {
			continue
		}
		if want := "winuse --headed " + tc.retry; !strings.HasSuffix(cmdErr.Hint, want) {
			t.Errorf("%s: hint = %q, want it to end in %q", tc.name, cmdErr.Hint, want)
		}
		if tc.alt != "" && !strings.Contains(cmdErr.Hint, tc.alt) {
			t.Errorf("%s: hint = %q, want it to name %q", tc.name, cmdErr.Hint, tc.alt)
		}
		if strings.Contains(cmdErr.Hint, "secret") || strings.Contains(cmdErr.Hint, "pw ") {
			t.Errorf("%s: the hint repeats the text or value: %q", tc.name, cmdErr.Hint)
		}
		if got := spy.recorded(); len(got) != 0 {
			t.Errorf("%s: a refused Action ran %v", tc.name, got)
		}

		// With --headed the same request goes the v1 way: it reaches the
		// activation (what follows needs a screenshot or an Element the
		// fakes may not have).
		raw, err := json.Marshal(tc.args)
		if err != nil {
			t.Fatal(err)
		}
		spy = newActorSpy()
		_, _ = CommandsWith(spy.svc)[tc.cmd](t.Context(), headedArgs(raw))
		if got := spy.recorded(); !containsString(got, "activate") {
			t.Errorf("%s with --headed: pipeline steps = %v, want the activation", tc.name, got)
		}
	}
}

func TestActivateWindowNeedsNoFlag(t *testing.T) {
	spy := newActorSpy()
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "activate-window", protocol.ActivateArgs{Window: "101"})
	if cmdErr != nil || out != okOutput {
		t.Fatalf("activate-window = %q, %v; want ok without --headed", out, cmdErr)
	}
	if got := spy.recorded(); !containsString(got, "activate") {
		t.Errorf("activate-window steps = %v, want the activation", got)
	}
}

func TestARefusedPathResolvesTheWindowFirst(t *testing.T) {
	spy := newActorSpy()
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "hover", protocol.HoverArgs{Window: "nope", X: 1, Y: 1})
	if cmdErr == nil || strings.Contains(cmdErr.Msg, "no background route") {
		t.Fatalf("hover on an unknown Window = %v; want the Window's own resolution error", cmdErr)
	}
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
