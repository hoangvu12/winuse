package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/jev"
	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestParseRunArgsTakesTheGoalAndItsValues(t *testing.T) {
	a, err := parseRunArgs([]string{"5902020:a269b6", "search", `for "Kurt Godel"`, "--set", "x", "--max-steps=7", "--model", "openjev-0.1", "then", "open", "it"})
	if err != nil {
		t.Fatal(err)
	}
	if a.window != "5902020:a269b6" || a.goal != `search for "Kurt Godel" then open it` || a.maxSteps != 7 || a.model != "openjev-0.1" {
		t.Errorf("parsed %+v", a)
	}
	if strings.Join(a.values, "|") != "Kurt Godel|x" {
		t.Errorf("values = %q (quoted goal text first, then --set)", a.values)
	}
	if a, _ := parseRunArgs([]string{"w", "do", "it"}); a.maxSteps != runDefaultSteps || a.values != nil {
		t.Errorf("defaults: %+v", a)
	}
	for _, bad := range [][]string{
		{"w"},
		{"w", "  "},
		{"w", "goal", "--max-steps", "0"},
		{"w", "goal", "--max-steps", "101"},
		{"w", "goal", "--set"},
		{"w", "goal", "--set="},
		{"w", "goal", "--model", " "},
		{"w", "goal", "--headed-ish"},
	} {
		if _, err := parseRunArgs(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func TestStateFingerprintSeesAChangedElement(t *testing.T) {
	a := []jev.Element{{Index: 1, Line: "button A"}, {Index: 2, Line: "edit (settable, string) Q"}}
	b := []jev.Element{{Index: 1, Line: "button A"}, {Index: 2, Line: "edit (settable, string) Q Value: x"}}
	if stateFingerprint(a) == stateFingerprint(b) || stateFingerprint(a) != stateFingerprint(append([]jev.Element{}, a...)) {
		t.Error("fingerprint does not track the Elements")
	}
}

func TestScrollOnADesktopAppScrollsItsContentPaneAndOnAPageItsWindow(t *testing.T) {
	r := &runner{a: runArgs{window: "w"}}
	box := jev.Element{Index: 46, Line: "pane Secondary Actions: Scroll Up, Scroll Down"}
	label, act := r.action(jev.Decision{Op: jev.OpScrollDown}, &readResult{scrollBox: &box})
	sa, ok := act.args.(protocol.ScrollArgs)
	if act.cmd != "scroll" || !ok || *sa.ElementIndex != 46 || sa.Direction != "down" || label != "SCROLL_DOWN 46" {
		t.Errorf("desktop: %q %s %+v", label, act.cmd, act.args)
	}
	_, act = r.action(jev.Decision{Op: jev.OpScrollUp}, &readResult{page: true})
	ea, ok := act.args.(protocol.EvalArgs)
	if act.cmd != "eval" || !ok || !strings.Contains(ea.Expression, "scrollBy(0, -") {
		t.Errorf("page: %s %+v", act.cmd, act.args)
	}
	if (&readResult{}).scrolls() || !(&readResult{page: true}).scrolls() || !(&readResult{scrollBox: &box}).scrolls() {
		t.Error("scrolls() does not follow page and scrollBox")
	}
}

func TestWithoutJevPickAndRunHandTheWorkBackToTheAgent(t *testing.T) {
	for _, cmd := range []string{"pick", "run"} {
		for _, cause := range []error{nil, errors.New("jev.json is not valid JSON")} {
			e := noJev(cmd, cause)
			if ExitCode(e) != 1 || !strings.Contains(e.msg, "nothing was read or sent") ||
				!strings.Contains(e.hint, "get-window-state") || !strings.Contains(e.hint, "only if the user asks") {
				t.Errorf("%s %v: %q / %q", cmd, cause, e.msg, e.hint)
			}
			if cause != nil && !strings.Contains(e.msg, cause.Error()) {
				t.Errorf("%s: the cause is not named: %q", cmd, e.msg)
			}
		}
	}
	if !strings.Contains(noJev("run", nil).hint, "do the task yourself") {
		t.Error("run's fallback does not say to do the task")
	}
}
