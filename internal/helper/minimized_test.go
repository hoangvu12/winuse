package helper

// Headless Actions on minimized Windows (headless-max ticket 14): the UI
// Automation pattern Actions (click, set-value, the Secondary Actions,
// select-text) reach a minimized classic Win32, WPF or WinUI 3 Desktop
// Window with the Agent Cursor hidden; a Store app, Chromium, Element
// scroll and the message routes stay refused with nothing sent. The
// readiness and the element side are faked; no desktop is touched.

import (
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

func TestPatternActionsReachAMinimizedClassicWindow(t *testing.T) {
	two := 2
	cases := []struct {
		cmd  string
		args any
		step string // the pattern call the act makes
	}{
		{"click", clickArgs("101", 2, 1, "left"), "headless-click"},
		{"set-value", setValueArgs("101", 2, "hello"), "headless-set-value"},
		{"perform-secondary-action", secondaryArgs("101", 2, "Toggle"), "headless-secondary"},
		{"select-text", protocol.SelectTextArgs{Window: "101", ElementIndex: &two, All: true}, "text-select"},
	}
	for _, class := range []string{"#32770", "HwndWrapper[App;;1]", "WinUIDesktopWin32WindowClass"} {
		for _, tc := range cases {
			t.Run(class+" "+tc.cmd, func(t *testing.T) {
				spy := newHeadlessSpy()
				spy.readyErr = &win.MinimizedError{WindowID: 101, Class: class}
				out, cmdErr := runCommand(t, CommandsWith(spy.svc), tc.cmd, tc.args)
				if cmdErr != nil {
					t.Fatalf("%s on a minimized %s Window: %v", tc.cmd, class, cmdErr.Error())
				}
				if out != "ok\n" {
					t.Errorf("output = %q, want ok", out)
				}
				steps := spy.recorded()
				if !hasStepPrefix(steps, tc.step) {
					t.Errorf("steps = %v, want the pattern call %q", steps, tc.step)
				}
				// No rectangle on screen: the Agent Cursor stays hidden.
				if containsStep(steps, "cursor-travel") || containsStep(steps, "cursor-click") {
					t.Errorf("the Agent Cursor moved over a minimized Window: %v", steps)
				}
				assertNoV1(t, steps)
				if b := spy.esc.brackets(); !equalStrings(b, []string{"arm-passive", "disarm-passive"}) {
					t.Errorf("Esc brackets = %v, want one passive scope", b)
				}
			})
		}
	}
}

func TestMinimizedRefusalsThatStay(t *testing.T) {
	cases := []struct {
		name  string
		class string
		cmd   string
		args  any
		want  string
	}{
		{"a Store app", "ApplicationFrameWindow", "click", clickArgs("101", 2, 1, "left"), "a minimized Store app has no UI to act on"},
		{"Chromium", "Chrome_WidgetWin_1", "set-value", setValueArgs("101", 2, "x"), "window 101 is minimized"},
		{"Firefox", "MozillaWindowClass", "click", clickArgs("101", 2, 1, "left"), "window 101 is minimized"},
		{"Element scroll", "#32770", "scroll", protocol.ScrollArgs{Window: "101", ElementIndex: intPtrBatch(2), Direction: "down"}, "window 101 is minimized"},
		{"scroll into view", "#32770", "scroll", protocol.ScrollArgs{Window: "101", ElementIndex: intPtrBatch(2), Direction: "into-view"}, "window 101 is minimized"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := newHeadlessSpy()
			spy.readyErr = &win.MinimizedError{WindowID: 101, Class: tc.class}
			_, cmdErr := runCommand(t, CommandsWith(spy.svc), tc.cmd, tc.args)
			if cmdErr == nil {
				t.Fatalf("%s on a minimized %s Window succeeded, want a refusal", tc.cmd, tc.class)
			}
			if !strings.Contains(cmdErr.Msg, tc.want) || !strings.HasSuffix(cmdErr.Msg, nothingSentNote) {
				t.Errorf("message = %q, want %q and %q", cmdErr.Msg, tc.want, nothingSentNote)
			}
			if !strings.Contains(cmdErr.Hint, "restore-window 101") {
				t.Errorf("hint = %q, want restore-window", cmdErr.Hint)
			}
			if b := spy.esc.brackets(); len(b) != 0 {
				t.Errorf("a refusal before winuse acts must arm nothing: %v", b)
			}
			assertNoV1(t, spy.recorded())
		})
	}
}

func TestMinimizedRefusalStaysForTheMessageRoutes(t *testing.T) {
	// The keyboard routes, Posted Keys and invoke-menu post or send window
	// messages; none of them is measured on a minimized Window.
	spy := newKeyRouteSpy(dialogTarget())
	spy.readyErr = &win.MinimizedError{WindowID: 101, Class: "#32770"}
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "press-key", pressKeyArgs("101", "Return"))
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "window 101 is minimized") || !strings.HasSuffix(cmdErr.Msg, nothingSentNote) {
		t.Fatalf("press-key Return on a minimized dialog: %v, want the minimized refusal", cmdErr)
	}
	if len(spy.posted) != 0 {
		t.Errorf("posted %+v to a minimized dialog", spy.posted)
	}
}
