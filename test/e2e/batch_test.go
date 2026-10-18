//go:build e2e

// The ticket-11 seam-1 tests: name:/id: Element References and Batches
// against the fixture app, on the live desktop. User-run only (see the
// package comment and test/e2e/README.md for the opt-in rules); an agent
// must never start them.
//
// The fixture's Element contract deliberately contains the ambiguity
// cases (internal/fixture/elements.go): twin-a/twin-b share the Name
// "Twin", solo-one/solo-two share the AutomationId "solo" — these tests
// use them for the never-guess failures.

package e2e

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
)

// runBatch invokes `winuse batch ...` with the suite's short helper idle
// timeout.
func runBatch(t *testing.T, args []string, extraEnv ...string) (string, string, int) {
	t.Helper()
	return runWinuse(t, append([]string{"batch"}, args...), append(extraEnv, "WINUSE_HELPER_IDLE="+e2eIdle)...)
}

func TestClickByNameReference(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	stdout, stderr, exit := runClick(t, []string{window, "name:Click Me"})
	assertActionOK(t, "click name:Click Me", stdout, stderr, exit)

	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool {
		return s.LastButton == "Click Me"
	})
}

func TestClickByIDReference(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	stdout, stderr, exit := runClick(t, []string{window, "id:button-save-as"})
	assertActionOK(t, "click id:button-save-as", stdout, stderr, exit)

	fix.waitState(t, 10*time.Second, func(s *fixture.State) bool {
		return s.DialogOpen && s.DialogWindowID != 0
	})
}

func TestClickAmbiguousNameListsCandidates(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	// twin-a and twin-b share the Name "Twin": the reference must fail
	// listing both candidates, never guess.
	stdout, stderr, exit := runClick(t, []string{window, "name:Twin"})
	assertActionFailed(t, "click name:Twin", stdout, stderr, exit,
		`name "Twin" matches 2 elements in the cached Window State for window `+window)
	if !strings.Contains(stderr, "candidates:") {
		t.Errorf("stderr = %q, want the candidate list", stderr)
	}
	for _, line := range []string{"button Twin ID: twin-a", "button Twin ID: twin-b"} {
		if !strings.Contains(stderr, line) {
			t.Errorf("stderr = %q, want the candidate line %q", stderr, line)
		}
	}
	// No input reached the app: neither twin recorded an invocation.
	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Generation >= 0 })
	if st.LastButton != "" {
		t.Errorf("an ambiguous reference must not click; lastButton = %q", st.LastButton)
	}
}

func TestClickAmbiguousIDListsCandidates(t *testing.T) {
	optIn(t)
	_, window := startFixtureWithState(t)

	// solo-one and solo-two share the AutomationId "solo".
	stdout, stderr, exit := runClick(t, []string{window, "id:solo"})
	assertActionFailed(t, "click id:solo", stdout, stderr, exit,
		`ID "solo" matches 2 elements in the cached Window State for window `+window)
	if !strings.Contains(stderr, "Solo One") || !strings.Contains(stderr, "Solo Two") {
		t.Errorf("stderr = %q, want both solo candidates named", stderr)
	}
}

func TestClickMissingReferenceFailsWithHint(t *testing.T) {
	optIn(t)
	_, window := startFixtureWithState(t)

	stdout, stderr, exit := runClick(t, []string{window, "name:Does Not Exist"})
	assertActionFailed(t, "click name:Does Not Exist", stdout, stderr, exit,
		`no element named "Does Not Exist" in the cached Window State for window `+window)
	if !strings.Contains(stderr, "get-window-state") {
		t.Errorf("the miss must instruct a fresh Window State read: %q", stderr)
	}
}

func TestReferencesResolveAgainstTheLatestWindowState(t *testing.T) {
	// A re-read replaces the Window State, and references resolve against
	// the new one: expanding the File menu exposes "New", which the
	// previous Window State did not contain.
	optIn(t)
	fix, window := startFixtureWithState(t)

	stdout, stderr, exit := runClick(t, []string{window, "name:File"})
	assertActionOK(t, "click name:File (expand)", stdout, stderr, exit)
	// Expanding File is the same as performing its Expand secondary
	// action; either way the dropdown's items appear. The click landed on
	// the menu item, which the fixture treats as expanding it.
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool {
		return s.OpenMenu == string(fixture.ElemMenuFile)
	})

	if _, _, exit := runGetWindowState(t, window); exit != 0 {
		t.Fatalf("get-window-state after expanding File: exit %d", exit)
	}
	stdout, stderr, exit = runClick(t, []string{window, "name:New"})
	assertActionOK(t, "click name:New", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool {
		return s.LastMenu == string(fixture.ElemMenuFileNew)
	})
}

func TestSetValueByReference(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	// Direct: set-value by name: reference.
	stdout, stderr, exit := runWinuse(t, []string{"set-value", window, "name:Text Field", "hello by name"},
		"WINUSE_HELPER_IDLE="+e2eIdle)
	assertActionOK(t, "set-value name:Text Field", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Text == "hello by name" })

	// And by id: reference.
	stdout, stderr, exit = runWinuse(t, []string{"set-value", window, "id:text-field", "hello by id"},
		"WINUSE_HELPER_IDLE="+e2eIdle)
	assertActionOK(t, "set-value id:text-field", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Text == "hello by id" })
}

func TestPerformSecondaryActionByReference(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	// Toggle the checkbox by its name: reference.
	stdout, stderr, exit := runWinuse(t, []string{"perform-secondary-action", window, "name:Notify me", "Toggle"},
		"WINUSE_HELPER_IDLE="+e2eIdle)
	assertActionOK(t, "perform-secondary-action name:Notify me", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Checkbox })
}

func TestBatchTypesThenClicksAndSucceeds(t *testing.T) {
	// The ticket's acceptance flow: a Batch that types into the fixture's
	// text field then clicks a button by name: succeeds and prints ok.
	optIn(t)
	fix, window := startFixtureWithState(t)

	stdout, stderr, exit := runBatch(t, []string{
		window,
		"click", "id:text-field",
		"then", "type-text", "batch typed this",
		"then", "click", "name:Click Me",
	})
	assertActionOK(t, "batch type then click", stdout, stderr, exit)

	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool {
		return s.Text == "batch typed this" && s.LastButton == "Click Me"
	})
	// The steps ran in order: the text was typed before the button click
	// (the click's status line reflects the app's status field).
	if st.Text != "batch typed this" {
		t.Errorf("text = %q, want the batch's typed text", st.Text)
	}
}

func TestBatchStopsAtTheFirstFailure(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	// Step 1 succeeds, step 2 fails (an ambiguous name:), step 3 must not
	// run (the text would change).
	stdout, stderr, exit := runBatch(t, []string{
		window,
		"click", "id:button-click",
		"then", "click", "name:Twin",
		"then", "type-text", "must not appear",
	})
	assertActionFailed(t, "batch stops at the bad step", stdout, stderr, exit,
		"batch stopped at Action 2 (click): name \"Twin\" matches 2 elements")
	if !strings.Contains(stderr, "candidates:") {
		t.Errorf("stderr = %q, want the step's own failure and candidates", stderr)
	}

	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool {
		return s.LastButton == "Click Me"
	})
	if st.Text != "" {
		t.Errorf("steps after the failure must not run; text = %q", st.Text)
	}
}

func TestBatchUnknownWindowFailsStepOne(t *testing.T) {
	optIn(t)
	_, _ = startFixtureWithState(t)

	// A Window reference that matches nothing fails the first step, which
	// the failure names.
	stdout, stderr, exit := runBatch(t, []string{"999999", "click", "id:x"})
	assertActionFailed(t, "batch with an unknown window", stdout, stderr, exit,
		"batch stopped at Action 1 (click): no open Window with ID 999999")
}

func TestBatchUsageErrorsExit2WithoutDialing(t *testing.T) {
	optIn(t)

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no steps", []string{"101"}, "batch takes a window reference and at least one Action"},
		{"empty step", []string{"101", "then", "click", "1"}, "a batch Action is empty"},
		{"unknown action", []string{"101", "zoom", "1,2,3,4"}, "unknown batch action"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, exit := runBatch(t, tc.args)
			if exit != 2 {
				t.Fatalf("exit = %d, want 2 (usage error)", exit)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing", stdout)
			}
			if !strings.Contains(stderr, tc.want) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, tc.want)
			}
		})
	}
}

func TestBatchAgainstTheDialog(t *testing.T) {
	// A Batch on the Save As dialog: set the file name by id:, then click
	// Save — one call, both steps, the dialog closes with result "save".
	optIn(t)
	fix, window := startFixtureWithState(t)

	if _, _, exit := runClick(t, []string{window, "id:button-save-as"}); exit != 0 {
		t.Fatalf("click id:button-save-as: exit %d", exit)
	}
	st := fix.waitState(t, 10*time.Second, func(s *fixture.State) bool { return s.DialogOpen && s.DialogWindowID != 0 })
	dialog := strconv.FormatUint(st.DialogWindowID, 10)

	// References resolve against the dialog's latest Window State.
	if _, stderr, exit := runGetWindowState(t, dialog); exit != 0 {
		t.Fatalf("get-window-state of the dialog: exit %d, stderr:\n%s", exit, stderr)
	}
	stdout, stderr, exit := runBatch(t, []string{
		dialog,
		"set-value", "id:dialog-edit-file-name", "report.txt",
		"then", "click", "name:Save",
	})
	assertActionOK(t, "batch on the dialog", stdout, stderr, exit)

	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool {
		return !s.DialogOpen && s.DialogResult == "save" && s.DialogFileName == "report.txt"
	})
}
