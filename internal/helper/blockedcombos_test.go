package helper

// The Blocked Combos' offline tests (desktop-parity ticket 05): press-key
// refuses Alt+F4, Alt+Tab, Ctrl+Shift+Escape and their supersets with
// "nothing was sent" on every route (Headed, Posted, the Page, a Batch
// step) unless forced, and the refusal's hint points at the command that
// does the job instead. Everything is faked; no key is sent.

import (
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestPressKeyRefusesBlockedCombos(t *testing.T) {
	cases := []struct {
		key, wantMsg, wantHint string
	}{
		{"Alt_L+F4", "press-key refuses Alt_L+F4: Alt+F4 closes the foreground window; nothing was sent", "winuse close-window <window>"},
		{"Alt_L+Tab", "press-key refuses Alt_L+Tab: Alt+Tab switches the user's foreground window; nothing was sent", "winuse activate-window <window>"},
		{"ctrl+shift+esc", "press-key refuses Control_L+Shift_L+Escape: Ctrl+Shift+Escape opens Task Manager; nothing was sent", "Task Manager"},
		{"Control_L+Alt_L+F4", "press-key refuses Control_L+Alt_L+F4: Alt+F4 closes the foreground window; nothing was sent", "close-window"},
		{"Alt_L+Shift_L+Tab", "press-key refuses Alt_L+Shift_L+Tab: Alt+Tab switches the user's foreground window; nothing was sent", "activate-window"},
	}
	for _, tc := range cases {
		for _, a := range []protocol.PressKeyArgs{
			{Window: "101", Key: tc.key},
			{Window: "101", Key: tc.key, Headed: true},
			{Window: "101", Key: tc.key, Post: true}, // the blocked refusal, not the modifier one
		} {
			spy := &keySpy{}
			_, cmdErr := runCommand(t, CommandsWith(spy.services()), "press-key", a)
			if cmdErr == nil || cmdErr.Msg != tc.wantMsg {
				t.Fatalf("%+v: message = %v, want %q", a, cmdErr, tc.wantMsg)
			}
			if !strings.Contains(cmdErr.Hint, tc.wantHint) || !strings.Contains(cmdErr.Hint, "--force") {
				t.Errorf("%+v: hint = %q, want %q and --force", a, cmdErr.Hint, tc.wantHint)
			}
			if cmdErr.Exit != 1 {
				t.Errorf("%+v: exit = %d, want 1", a, cmdErr.Exit)
			}
			if got := spy.recorded(); len(got) != 0 {
				t.Errorf("%+v: a blocked combo must not touch the desktop: steps %v", a, got)
			}
		}
	}
}

func TestPressKeyForceSendsBlockedCombos(t *testing.T) {
	// A forced combo has no Headless route: it is pressed Headed, so the
	// requests send `headed` (headless-max ticket 21).
	for _, key := range []string{"Alt_L+F4", "Alt_L+Tab", "Control_L+Shift_L+Escape", "Control_L+Alt_L+F4"} {
		spy := &keySpy{}
		out, cmdErr := runCommand(t, CommandsWith(spy.services()), "press-key", protocol.PressKeyArgs{Window: "101", Key: key, Force: true, Headed: true})
		if cmdErr != nil {
			t.Fatalf("%s --force: %v", key, cmdErr.Error())
		}
		if out != "ok\n" {
			t.Errorf("%s --force: output = %q", key, out)
		}
		if len(spy.pressChords) != 1 || spy.pressChords[0].String() != key {
			t.Errorf("%s --force: pressed %v", key, spy.pressChords)
		}
	}
	// --force never posts a Blocked Combo (headless-max ticket 07: chords
	// can be posted, but not these).
	spy := &keySpy{}
	_, cmdErr := runCommand(t, CommandsWith(spy.services()), "press-key", protocol.PressKeyArgs{Window: "101", Key: "Alt_L+F4", Post: true, Force: true})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "never sends Alt_L+F4: it is a Blocked Combo") || !strings.HasSuffix(cmdErr.Msg, nothingSentNote) {
		t.Fatalf("posted --force: %v", cmdErr)
	}
}

// TestPageRefusesBlockedCombos: on a Page, Alt+F4 and Ctrl+Shift+Escape
// would reach the browser frame, so the Page route refuses them too.
func TestPageRefusesBlockedCombos(t *testing.T) {
	for _, key := range []string{"Alt_L+F4", "Control_L+Shift_L+Escape", "Alt_L+Tab"} {
		r := newPageRig(t)
		_, e := r.run(t, "press-key", protocol.PressKeyArgs{Window: "301", Key: key})
		if e == nil || !strings.HasSuffix(e.Msg, "; nothing was sent") {
			t.Fatalf("%s on a Page: %v", key, e)
		}
		if len(r.sent()) != 0 || len(r.recorded()) != 0 {
			t.Errorf("%s on a Page: sent %v, steps %v", key, r.sent(), r.recorded())
		}
	}
	r := newPageRig(t)
	if _, e := r.run(t, "press-key", protocol.PressKeyArgs{Window: "301", Key: "Alt_L+F4", Force: true}); e != nil {
		t.Fatalf("Alt_L+F4 --force on a Page: %v", e.Error())
	}
	if len(r.sent()) == 0 {
		t.Error("Alt_L+F4 --force on a Page sent nothing")
	}
}

// TestBatchRefusesBlockedStepsUpFront: a blocked press-key step fails the
// Batch while it prepares, so no step acts; --force is per step.
func TestBatchRefusesBlockedStepsUpFront(t *testing.T) {
	spy := newBatchSpy()
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
		Window: "101",
		Steps: []protocol.Step{
			stepOf("type-text", protocol.TypeTextArgs{Text: "hello"}),
			stepOf("press-key", protocol.PressKeyArgs{Key: "Alt_L+F4", Force: true}),
			stepOf("press-key", protocol.PressKeyArgs{Key: "Alt_L+Tab"}),
		},
	})
	want := "batch: Action 3 (press-key): press-key refuses Alt_L+Tab: Alt+Tab switches the user's foreground window; nothing was sent"
	if cmdErr == nil || cmdErr.Msg != want {
		t.Fatalf("message = %v, want %q", cmdErr, want)
	}
	if !strings.Contains(cmdErr.Hint, "activate-window") {
		t.Errorf("hint = %q", cmdErr.Hint)
	}
	if got := spy.recorded(); len(got) != 0 {
		t.Errorf("no step may act: steps %v", got)
	}

	spy = newBatchSpy()
	if _, cmdErr := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
		Window: "101",
		Headed: true,
		Steps:  []protocol.Step{stepOf("press-key", protocol.PressKeyArgs{Key: "Alt_L+F4", Force: true})},
	}); cmdErr != nil {
		t.Fatalf("forced step: %v", cmdErr.Error())
	}
	if pressed := spy.pressedKeys(); len(pressed) != 1 || pressed[0] != "Alt_L+F4" {
		t.Errorf("pressed %q, want one Alt_L+F4", pressed)
	}
}
