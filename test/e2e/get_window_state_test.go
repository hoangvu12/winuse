//go:build e2e

// The ticket-03 seam-1 tests: `winuse get-window-state` against the fixture
// app, on the live desktop. User-run only (see the package comment and
// test/e2e/README.md for the opt-in rules); an agent must never start them.
//
// The expected tree is derived from the fixture's Element contract
// (internal/fixture: BuildTree of the initial state), so these tests assert
// exactly what the agent will see: header, depth-first numbered indexes,
// TAB indentation, roles, names and AutomationIds.

package e2e

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// runGetWindowState invokes `winuse get-window-state <window>` with the
// suite's short helper idle timeout.
func runGetWindowState(t *testing.T, window string, extraEnv ...string) (string, string, int) {
	t.Helper()
	return runWinuse(t, []string{"get-window-state", window}, append(extraEnv, "WINUSE_HELPER_IDLE="+e2eIdle)...)
}

// expectedLines flattens the fixture's initial tree into the exact Element
// lines winuse must print (index, TABs, role, states, name, Secondary
// Actions, ID), depth-first — the same rich lines the live walk renders
// since ticket 07 (pattern availability, value/toggle/selection/expand
// state), derived from the fixture's Element contract and initial state.
func expectedLines() []string {
	state := fixture.State{}
	nodes := fixture.NewTreeMap(fixture.BuildTree(state))
	indexes := mainElementIndexes() // reserves the OS caption subtree's indexes
	var lines []string
	var walk func(e fixture.Elem, depth int)
	walk = func(e fixture.Elem, depth int) {
		spec, ok := fixture.SpecFor(e)
		if !ok {
			return
		}
		line := strings.Repeat("\t", depth+1) +
			fmt.Sprintf("%d %s", indexes[e], wstate.RoleName(int32(spec.Role)))
		// Codex's state list, between role and name: selectable, selected,
		// disabled, collapsed/expanded, settable.
		var states []string
		if fixture.HasPattern(e, fixture.PatternSelectionItem) {
			states = append(states, "selectable")
			if state.SelectedItem == fixture.Name(e, "") {
				states = append(states, "selected")
			}
		}
		if !fixture.ElementEnabled(e, state) {
			states = append(states, "disabled")
		}
		if fixture.HasPattern(e, fixture.PatternExpandCollapse) {
			states = append(states, "collapsed") // no menu open, details collapsed
		}
		if fixture.HasPattern(e, fixture.PatternValue) {
			states = append(states, "settable, string") // the fixture's edits are writable
		}
		if len(states) > 0 {
			line += " (" + strings.Join(states, ", ") + ")"
		}
		if name := fixture.Name(e, fixture.InitialStatus); name != "" {
			line += " " + name
		}
		if e == fixture.ElemCheckboxNotify {
			line += " off" // the toggle token, after the name (and Value)
		}
		// Secondary Actions, in winuse's fixed order.
		var actions []string
		if fixture.HasPattern(e, fixture.PatternWindow) {
			actions = append(actions, "Raise")
		}
		if fixture.HasPattern(e, fixture.PatternScroll) {
			// The list scrolls vertically only.
			actions = append(actions, "Scroll Up", "Scroll Down")
		}
		if fixture.HasPattern(e, fixture.PatternExpandCollapse) {
			actions = append(actions, "Expand")
		}
		if fixture.HasPattern(e, fixture.PatternToggle) {
			actions = append(actions, "Toggle")
		}
		if fixture.HasPattern(e, fixture.PatternSelectionItem) {
			actions = append(actions, "Select")
		}
		if len(actions) > 0 {
			line += " Secondary Actions: " + strings.Join(actions, ", ")
		}
		if id := fixture.AutomationID(e); id != "" {
			line += " ID: " + id
		}
		lines = append(lines, line)
		for _, kid := range nodes.Children(e) {
			walk(kid, depth+1)
		}
	}
	walk(fixture.ElemWindow, 0)
	return lines
}

// assertFixtureTree checks the full get-window-state contract against the
// fixture's initial state: header line, every expected Element line present
// verbatim, and depth-first sequential indexes.
func assertFixtureTree(t *testing.T, stdout string) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) == 0 {
		t.Fatalf("empty output")
	}
	if want := `Window: "` + fixture.WindowTitle + `", App: winusefixture.exe.`; lines[0] != want {
		t.Errorf("header = %q, want %q", lines[0], want)
	}

	output := make(map[string]bool, len(lines))
	for _, l := range lines[1:] {
		output[l] = true
	}
	for _, want := range expectedLines() {
		if !output[want] {
			t.Errorf("missing Element line %q", want)
		}
	}

	// Indexes are depth-first and sequential from 0 (the root window).
	indexLine := regexp.MustCompile(`^\t+(\d+) `)
	last := -1
	for _, l := range lines[1:] {
		m := indexLine.FindStringSubmatch(l)
		if m == nil {
			continue // focused-Element or Note lines
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatalf("bad index in line %q", l)
		}
		if n != last+1 {
			t.Errorf("Element index %d after %d: indexes must be depth-first and sequential\n%q", n, last, l)
		}
		last = n
	}

	// The text field is focused at startup, so the tail names it, without
	// the leading indent.
	focused := "The focused UI element is " + strings.TrimLeft(expectedLines()[5], "\t") + "."
	if !strings.Contains(stdout, focused) {
		t.Errorf("missing focused-Element tail %q", focused)
	}
}

func TestGetWindowStateByWindowID(t *testing.T) {
	optIn(t)
	fix := startFixture(t)
	st := fix.waitState(t, 15*time.Second, func(s *fixture.State) bool { return s.WindowID != 0 })
	stdout, stderr, exit := runGetWindowState(t, strconv.FormatUint(st.WindowID, 10))
	if exit != 0 {
		t.Fatalf("exit %d, stderr:\n%s", exit, stderr)
	}
	if stderr != "" {
		t.Errorf("unexpected stderr: %q", stderr)
	}
	assertFixtureTree(t, stdout)
}

func TestGetWindowStateByAppName(t *testing.T) {
	optIn(t)
	fix := startFixture(t)
	fix.waitState(t, 15*time.Second, func(s *fixture.State) bool { return s.WindowID != 0 })
	stdout, stderr, exit := runGetWindowState(t, "winusefixture")
	if exit != 0 {
		t.Fatalf("exit %d, stderr:\n%s", exit, stderr)
	}
	if stderr != "" {
		t.Errorf("unexpected stderr: %q", stderr)
	}
	assertFixtureTree(t, stdout)
}

func TestGetWindowStateAmbiguousAppListsCandidates(t *testing.T) {
	optIn(t)
	f1 := startFixture(t)
	s1 := f1.waitState(t, 15*time.Second, func(s *fixture.State) bool { return s.WindowID != 0 })
	f2 := startFixture(t)
	s2 := f2.waitState(t, 15*time.Second, func(s *fixture.State) bool { return s.WindowID != 0 })

	stdout, stderr, exit := runGetWindowState(t, "winusefixture")
	if exit == 0 {
		t.Fatalf("two fixture Windows of one App must not resolve; stdout:\n%s", stdout)
	}
	if !strings.Contains(stderr, "has 2 open Windows") {
		t.Errorf("error must say the App is ambiguous, got %q", stderr)
	}
	for _, id := range []uint64{s1.WindowID, s2.WindowID} {
		if !strings.Contains(stderr, strconv.FormatUint(id, 10)) {
			t.Errorf("error must list candidate Window %d, got %q", id, stderr)
		}
	}
	if stdout != "" {
		t.Errorf("a failed observation must print nothing on stdout, got %q", stdout)
	}
}

func TestGetWindowStateUnknownWindowID(t *testing.T) {
	optIn(t)
	// An ID no HWND can ever have (user handles are far smaller).
	stdout, stderr, exit := runGetWindowState(t, "18446744073709551615")
	if exit == 0 {
		t.Fatalf("an unknown Window ID must fail; stdout:\n%s", stdout)
	}
	if !strings.Contains(stderr, "no open Window with ID 18446744073709551615") {
		t.Errorf("error = %q", stderr)
	}
	if !strings.Contains(stderr, "list-windows") {
		t.Errorf("the error needs a recovery hint, got %q", stderr)
	}
}

func TestGetWindowStateAppWithNoWindows(t *testing.T) {
	optIn(t)
	stdout, stderr, exit := runGetWindowState(t, "No Such App 42")
	if exit == 0 {
		t.Fatalf("an App with no Windows must fail; stdout:\n%s", stdout)
	}
	if !strings.Contains(stderr, `app "No Such App 42" has no open Windows`) {
		t.Errorf("error = %q", stderr)
	}
}

// TestGetWindowStateTinyBudgetIsPartialWithNote checks the bounded read: an
// impossibly small tree budget still succeeds, returning the partial tree
// (the root) with the time-limit note instead of hanging or failing.
//
// The budget env is read at helper start, so this test must run against a
// helper started by itself: it first waits out the suite's idle period
// (polling dials would reset the idle timer, so it sleeps once and checks
// once).
func TestGetWindowStateTinyBudgetIsPartialWithNote(t *testing.T) {
	optIn(t)
	time.Sleep(6 * time.Second) // > e2eIdle (5s) from earlier tests' helpers
	id, err := win.CurrentIdentity()
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	if conn, err := dialHelper(id); err == nil {
		conn.Close()
		t.Fatal("a helper is still serving; its idle period should have passed")
	}
	fix := startFixture(t)
	st := fix.waitState(t, 15*time.Second, func(s *fixture.State) bool { return s.WindowID != 0 })
	stdout, stderr, exit := runGetWindowState(t, strconv.FormatUint(st.WindowID, 10), "WINUSE_TREE_BUDGET=1ms")
	if exit != 0 {
		t.Fatalf("a partial tree is a success; exit %d, stderr:\n%s", exit, stderr)
	}
	if !strings.Contains(stdout, `Window: "`+fixture.WindowTitle+`", App: winusefixture.exe.`) {
		t.Errorf("partial output missing the header:\n%s", stdout)
	}
	if !strings.Contains(stdout, "\t0 window "+fixture.WindowTitle+" Secondary Actions: Raise ID: "+string(fixture.ElemWindow)) {
		t.Errorf("partial output missing the root element:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Note: this Window State is partial:") || !strings.Contains(stdout, "time limit") {
		t.Errorf("a timed-out read must return the partial tree plus a note:\n%s", stdout)
	}
}

// TestRecordTreeWritesFixture runs the recorder against the fixture app and
// checks the saved file loads as a seam-2 fixture and renders the same
// tree.
func TestRecordTreeWritesFixture(t *testing.T) {
	optIn(t)
	fix := startFixture(t)
	st := fix.waitState(t, 15*time.Second, func(s *fixture.State) bool { return s.WindowID != 0 })
	file := t.TempDir() + "\\fixture-tree.json"

	stdout, stderr, exit := runWinuse(t, []string{"record-tree", strconv.FormatUint(st.WindowID, 10), file},
		"WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("exit %d, stderr:\n%s", exit, stderr)
	}
	if !strings.Contains(stdout, file) {
		t.Errorf("the recorder must print the file it wrote, got %q", stdout)
	}

	tree, err := wstate.LoadFixture(file)
	if err != nil {
		t.Fatalf("the recorder's output must be a loadable fixture: %v", err)
	}
	if tree.Format != wstate.FixtureFormat || tree.Captured == nil {
		t.Errorf("fixture header = format %d, captured %v", tree.Format, tree.Captured)
	}
	// The recorded tree of the fixture's initial state renders the expected
	// Element lines.
	rendered := wstate.Render(tree)
	for _, want := range expectedLines() {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered recording missing %q", want)
		}
	}
}
