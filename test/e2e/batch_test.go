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
	"os"
	"os/exec"
	"regexp"
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

// batchStepLine matches the first line of one step's output in a Batch's
// stdout (desktop-parity ticket 20): "Action N (<kind>): <output>".
var batchStepLine = regexp.MustCompile(`^Action (\d+) \(([a-z-]+)\): (.*)$`)

// batchSteps splits a Batch's stdout into each step's first line, by step
// number, and the note lines the whole Batch added at the left margin.
// Indented lines (a step's further output or hint) are skipped; ok is
// false when a left-margin line is neither.
func batchSteps(stdout string) (steps map[int]string, notes []string, ok bool) {
	steps = map[int]string{}
	for _, l := range strings.Split(strings.TrimSuffix(stdout, "\n"), "\n") {
		if m := batchStepLine.FindStringSubmatch(l); m != nil {
			n, _ := strconv.Atoi(m[1])
			steps[n] = m[3]
			continue
		}
		if strings.HasPrefix(l, "Note: ") {
			notes = append(notes, l)
			continue
		}
		if !strings.HasPrefix(l, "  ") {
			return nil, nil, false
		}
	}
	return steps, notes, strings.HasSuffix(stdout, "\n")
}

// assertBatchOK checks a Batch whose steps all succeeded: exit 0, nothing
// on stderr, and one "Action N (<kind>): ok" line per step, any note lines
// after them.
func assertBatchOK(t *testing.T, what, stdout, stderr string, exit, n int) {
	t.Helper()
	if exit != 0 {
		t.Fatalf("%s: exit %d, stdout:\n%s\nstderr:\n%s", what, exit, stdout, stderr)
	}
	steps, notes, ok := batchSteps(stdout)
	if !ok || len(steps) != n {
		t.Errorf("%s: stdout = %q, want %d step lines", what, stdout, n)
	}
	for i := 1; i <= n; i++ {
		if !strings.HasPrefix(steps[i], "ok") {
			t.Errorf("%s: Action %d printed %q, want ok", what, i, steps[i])
		}
	}
	for _, note := range notes {
		t.Logf("%s: %s", what, note)
	}
	if stderr != "" {
		t.Errorf("%s: unexpected stderr: %q", what, stderr)
	}
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
		`no element named "Does Not Exist" in window `+window+`, in its cached Window State or a fresh read; nothing was sent`)
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
	// text field then clicks a button by name: succeeds and prints each
	// step's ok.
	// Clicking a text field to focus it is a real-pointer click: the field
	// offers no Headless click route (ADR 0010), so the Batch is --headed.
	optIn(t)
	fix, window := startFixtureWithState(t)

	stdout, stderr, exit := runBatch(t, []string{
		"--headed", window,
		"click", "id:text-field",
		"then", "type-text", "batch typed this",
		"then", "click", "name:Click Me",
	})
	assertBatchOK(t, "batch type then click", stdout, stderr, exit, 3)

	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool {
		return s.Text == "batch typed this" && s.LastButton == "Click Me"
	})
	// The steps ran in order: the text was typed before the button click
	// (the click's status line reflects the app's status field).
	if st.Text != "batch typed this" {
		t.Errorf("text = %q, want the batch's typed text", st.Text)
	}
}

func TestBatchRunsPastAFailedStep(t *testing.T) {
	// Desktop-parity ticket 20: step 1 succeeds, step 2 fails (an ambiguous
	// name:), and step 3 still runs (the text changes). Each step's line is
	// on stdout, the failed one with its message and hint; the Batch then
	// fails, naming step 2.
	optIn(t)
	fix, window := startFixtureWithState(t)

	stdout, stderr, exit := runBatch(t, []string{
		window,
		"click", "id:button-click",
		"then", "click", "name:Twin",
		"then", "set-value", "id:text-field", "after the failure",
	})
	if exit != 1 {
		t.Fatalf("exit %d, want 1; stdout:\n%s\nstderr:\n%s", exit, stdout, stderr)
	}
	steps, _, ok := batchSteps(stdout)
	if !ok || len(steps) != 3 || !strings.HasPrefix(steps[1], "ok") || !strings.HasPrefix(steps[3], "ok") ||
		!strings.HasPrefix(steps[2], `failed: name "Twin" matches 2 elements`) {
		t.Errorf("stdout = %q, want steps 1 and 3 ok and step 2's failure", stdout)
	}
	if !strings.Contains(stdout, "  hint: ") || !strings.Contains(stdout, "button Twin ID: twin-a") {
		t.Errorf("stdout = %q, want step 2's hint with the candidates under it", stdout)
	}
	if !strings.Contains(stderr, "batch: 1 of 3 Actions failed: Action 2 (click)") || !strings.Contains(stderr, "hint: ") {
		t.Errorf("stderr = %q, want the report naming Action 2 and a hint", stderr)
	}

	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool {
		return s.LastButton == "Click Me" && s.Text == "after the failure"
	})
}

func TestBatchStopOnErrorStopsAtTheFirstFailure(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	// With --stop-on-error, step 2's failure stops the Batch: step 3 must
	// not run (the text would change).
	stdout, stderr, exit := runBatch(t, []string{
		window, "--stop-on-error",
		"click", "id:button-click",
		"then", "click", "name:Twin",
		"then", "set-value", "id:text-field", "must not appear",
	})
	if exit != 1 {
		t.Fatalf("exit %d, want 1; stdout:\n%s\nstderr:\n%s", exit, stdout, stderr)
	}
	if steps, _, ok := batchSteps(stdout); !ok || len(steps) != 1 || !strings.HasPrefix(steps[1], "ok") {
		t.Errorf("stdout = %q, want only step 1's ok", stdout)
	}
	if !strings.Contains(stderr, "batch stopped at Action 2 (click): name \"Twin\" matches 2 elements") || !strings.Contains(stderr, "candidates:") {
		t.Errorf("stderr = %q, want the step's own failure and candidates", stderr)
	}

	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool {
		return s.LastButton == "Click Me"
	})
	time.Sleep(300 * time.Millisecond)
	if st = fix.waitState(t, time.Second, func(*fixture.State) bool { return true }); st.Text != "" {
		t.Errorf("steps after the failure must not run; text = %q", st.Text)
	}
}

func TestBatchFromStdin(t *testing.T) {
	// `winuse batch -` (desktop-parity ticket 20): the Window and the Batch
	// flags on the first line, one step per line, a comment and a blank
	// line skipped, and quoted text that holds `then` and quotes arrives
	// whole.
	optIn(t)
	fix, window := startFixtureWithState(t)

	const text = `from stdin: then "quoted"`
	cmd := exec.Command(winuseBinary(t), "batch", "-")
	cmd.Env = append(os.Environ(), "WINUSE_HELPER_IDLE="+e2eIdle)
	cmd.Stdin = strings.NewReader("# the e2e stdin Batch\r\n" + window + " --stop-on-error\r\n\r\n" +
		"click id:button-click\r\n" +
		`set-value id:text-field "from stdin: then \"quoted\""` + "\r\n")
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	exit := 0
	if err := cmd.Run(); err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running winuse: %v", err)
		}
		exit = exitErr.ExitCode()
	}
	assertBatchOK(t, "batch - from stdin", stdout.String(), stderr.String(), exit, 2)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool {
		return s.LastButton == "Click Me" && s.Text == text
	})
}

func TestBatchUnknownWindowFailsBeforeAnyStep(t *testing.T) {
	optIn(t)
	_, _ = startFixtureWithState(t)

	// A Window reference that matches nothing fails the Batch before its
	// first step: every step would fail on it.
	stdout, stderr, exit := runBatch(t, []string{"999999", "click", "id:x", "then", "click", "id:y"})
	assertActionFailed(t, "batch with an unknown window", stdout, stderr, exit,
		"batch: no open Window with ID 999999")
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
	assertBatchOK(t, "batch on the dialog", stdout, stderr, exit, 2)

	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool {
		return !s.DialogOpen && s.DialogResult == "save" && s.DialogFileName == "report.txt"
	})
}
