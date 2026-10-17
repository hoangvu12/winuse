//go:build e2e

// The ticket-05 seam-1 tests: `winuse click` and `winuse activate-window`
// against the fixture app, on the live desktop. User-run only (see the
// package comment and test/e2e/README.md for the opt-in rules); an agent
// must never start them.
//
// Element Indexes come from the fixture's Element contract
// (internal/fixture): the tests flatten the same depth-first order the
// Window State numbers, so they click by the same index an agent reads
// off the tree.

package e2e

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
)

// elementIndexes flattens one window's tree in winuse's depth-first
// order — the order the Window State numbers — returning Element →
// Element Index.
func elementIndexes(nodes []fixture.Node, root fixture.Elem) map[fixture.Elem]int {
	m := fixture.NewTreeMap(nodes)
	indexes := map[fixture.Elem]int{}
	var visit func(e fixture.Elem)
	visit = func(e fixture.Elem) {
		indexes[e] = len(indexes)
		for _, kid := range m.Children(e) {
			visit(kid)
		}
	}
	visit(root)
	return indexes
}

// mainElementIndexes returns the Element Indexes of the fixture's initial
// main-window tree (no menu open, toolbar hidden, details collapsed).
func mainElementIndexes() map[fixture.Elem]int {
	return elementIndexes(fixture.BuildTree(fixture.State{}), fixture.ElemWindow)
}

// dialogElementIndexes returns the Element Indexes of the Save As
// dialog's tree.
func dialogElementIndexes() map[fixture.Elem]int {
	return elementIndexes(fixture.DialogTree(fixture.State{}), fixture.DialogRootElem)
}

// runClick invokes `winuse click ...` with the suite's short helper idle
// timeout.
func runClick(t *testing.T, args []string, extraEnv ...string) (string, string, int) {
	t.Helper()
	return runWinuse(t, append([]string{"click"}, args...), append(extraEnv, "WINUSE_HELPER_IDLE="+e2eIdle)...)
}

// runActivateWindow invokes `winuse activate-window <window>`.
func runActivateWindow(t *testing.T, window string, extraEnv ...string) (string, string, int) {
	t.Helper()
	return runWinuse(t, []string{"activate-window", window}, append(extraEnv, "WINUSE_HELPER_IDLE="+e2eIdle)...)
}

// startFixtureWithState starts the fixture, waits for its Window ID and
// reads the Window State so the helper holds the live Elements behind the
// Element Indexes the test is about to use.
func startFixtureWithState(t *testing.T) (*fixtureProc, string) {
	t.Helper()
	fix := startFixture(t)
	st := fix.waitState(t, 15*time.Second, func(s *fixture.State) bool { return s.WindowID != 0 })
	window := strconv.FormatUint(st.WindowID, 10)
	stdout, stderr, exit := runGetWindowState(t, window)
	if exit != 0 {
		t.Fatalf("get-window-state: exit %d, stderr:\n%s", exit, stderr)
	}
	_ = stdout
	return fix, window
}

// assertActionOK pins the Action output contract: exactly "ok" on stdout,
// nothing on stderr, exit 0.
func assertActionOK(t *testing.T, what, stdout, stderr string, exit int) {
	t.Helper()
	if exit != 0 {
		t.Fatalf("%s: exit %d, stderr:\n%s", what, exit, stderr)
	}
	if stdout != "ok\n" {
		t.Errorf("%s: stdout = %q, want %q", what, stdout, "ok\n")
	}
	if stderr != "" {
		t.Errorf("%s: unexpected stderr: %q", what, stderr)
	}
}

// assertActionFailed pins the failed-Action contract: nothing on stdout,
// the failure line on stderr (plus its hint), non-zero exit.
func assertActionFailed(t *testing.T, what, stdout, stderr string, exit int, wantMsg string) {
	t.Helper()
	if exit == 0 {
		t.Fatalf("%s: exit 0 with stdout:\n%s", what, stdout)
	}
	if stdout != "" {
		t.Errorf("%s: a failed action must print nothing on stdout, got %q", what, stdout)
	}
	if !strings.Contains(stderr, wantMsg) {
		t.Errorf("%s: stderr = %q, want it to contain %q", what, stderr, wantMsg)
	}
	if !strings.Contains(stderr, "hint: ") {
		t.Errorf("%s: the failure needs a recovery hint, got %q", what, stderr)
	}
}

func TestClickButtonChangesFixtureState(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	idx := mainElementIndexes()[fixture.ElemButtonClick]
	stdout, stderr, exit := runClick(t, []string{window, strconv.Itoa(idx)})
	assertActionOK(t, "click button-click", stdout, stderr, exit)

	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool {
		return s.LastButton == "Click Me"
	})
}

func TestClickCheckboxTogglesState(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	idx := mainElementIndexes()[fixture.ElemCheckboxNotify]
	stdout, stderr, exit := runClick(t, []string{window, strconv.Itoa(idx)})
	assertActionOK(t, "click checkbox-notify", stdout, stderr, exit)

	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool {
		return s.Checkbox
	})
}

func TestDoubleClickAndRightClickCanvas(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	idx := mainElementIndexes()[fixture.ElemCanvas]

	stdout, stderr, exit := runClick(t, []string{window, strconv.Itoa(idx), "--count", "2"})
	assertActionOK(t, "double click canvas", stdout, stderr, exit)
	// A double click records one click from the first press plus the
	// double-click itself, marked in the counter.
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool {
		return s.CanvasDoubleClicks == 1 && len(s.CanvasClicks) == 2
	})

	stdout, stderr, exit = runClick(t, []string{window, strconv.Itoa(idx), "--button", "right"})
	assertActionOK(t, "right click canvas", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool {
		return len(s.CanvasRightClicks) == 1
	})
}

func TestClickScrollsOffscreenElementIntoView(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	// Hotel is below the fold (items 6-8 are offscreen until scrolled).
	idx := mainElementIndexes()[fixture.ElemItemHotel]
	stdout, stderr, exit := runClick(t, []string{window, strconv.Itoa(idx)})
	assertActionOK(t, "click item-hotel", stdout, stderr, exit)

	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool {
		return s.SelectedItem == "Hotel"
	})
	if st.ListFirstVisible != len(fixture.ListItems)-fixture.ListVisibleItems {
		t.Errorf("list scroll position = %d, want %d (hotel scrolled into view)",
			st.ListFirstVisible, len(fixture.ListItems)-fixture.ListVisibleItems)
	}
}

func TestClickOpensSaveAsDialogAsNewWindow(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	// Clicking "Save As..." opens the dialog...
	idx := mainElementIndexes()[fixture.ElemButtonSaveAs]
	stdout, stderr, exit := runClick(t, []string{window, strconv.Itoa(idx)})
	assertActionOK(t, "click button-save-as", stdout, stderr, exit)

	st := fix.waitState(t, 10*time.Second, func(s *fixture.State) bool {
		return s.DialogOpen && s.DialogWindowID != 0
	})

	// ...and the dialog appears as its own Window of the same App.
	stdout, stderr, exit = runListWindows(t, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("list-windows: exit %d, stderr:\n%s", exit, stderr)
	}
	_, app, title, ok := windowLine(stdout, st.DialogWindowID)
	if !ok {
		t.Fatalf("the Save As dialog (Window %d) is not listed:\n%s", st.DialogWindowID, stdout)
	}
	if title != fixture.DialogTitle {
		t.Errorf("dialog title = %q, want %q", title, fixture.DialogTitle)
	}
	if app != "winusefixture" {
		t.Errorf("dialog App = %q, want %q", app, "winusefixture")
	}

	// The dialog is a full Action target: read its Window State, click its
	// Cancel button by Element Index.
	dialog := strconv.FormatUint(st.DialogWindowID, 10)
	stdout, stderr, exit = runGetWindowState(t, dialog)
	if exit != 0 {
		t.Fatalf("get-window-state of the dialog: exit %d, stderr:\n%s", exit, stderr)
	}
	cancel := dialogElementIndexes()[fixture.ElemDialogButtonCancel]
	stdout, stderr, exit = runClick(t, []string{dialog, strconv.Itoa(cancel)})
	assertActionOK(t, "click dialog-button-cancel", stdout, stderr, exit)

	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool {
		return !s.DialogOpen && s.DialogResult == "cancel"
	})
}

func TestClickStaleElementIndex(t *testing.T) {
	optIn(t)
	_, window := startFixtureWithState(t)

	// An index beyond every Element of the cached Window State fails as
	// stale and tells the agent to read the Window State again.
	stdout, stderr, exit := runClick(t, []string{window, "999"})
	assertActionFailed(t, "click stale index", stdout, stderr, exit,
		"element 999 is not available in the cached Window State for window "+window)
	if !strings.Contains(stderr, "get-window-state") {
		t.Errorf("the stale failure must instruct a fresh Window State read: %q", stderr)
	}
}

func TestClickWithoutCachedWindowState(t *testing.T) {
	optIn(t)
	// The fixture keeps running across the helper restart (its cleanup
	// is registered on t).
	_, window := startFixtureWithState(t)

	// A helper restart loses the cached Window State (Element Indexes are
	// only valid within the helper that read them); the click says so.
	killHelper(t)
	stdout, stderr, exit := runClick(t, []string{window, "3"})
	assertActionFailed(t, "click with no cached state", stdout, stderr, exit,
		"no cached Window State is available for window "+window)
	if !strings.Contains(stderr, "get-window-state") {
		t.Errorf("the failure must instruct a Window State read: %q", stderr)
	}
}

func TestClickWhileModalDialogOpenFails(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	// Open the dialog, then click a main-window Element whose index is
	// still cached: the main window is disabled by its modal dialog, so
	// the click must refuse (naming the dialog) without sending input.
	idx := mainElementIndexes()[fixture.ElemButtonClick]
	saveAs := mainElementIndexes()[fixture.ElemButtonSaveAs]
	if _, _, exit := runClick(t, []string{window, strconv.Itoa(saveAs)}); exit != 0 {
		t.Fatalf("click button-save-as: exit %d", exit)
	}
	st := fix.waitState(t, 10*time.Second, func(s *fixture.State) bool { return s.DialogOpen })

	stdout, stderr, exit := runClick(t, []string{window, strconv.Itoa(idx)})
	assertActionFailed(t, "click blocked main window", stdout, stderr, exit,
		"has a modal dialog open (window "+strconv.FormatUint(st.DialogWindowID, 10)+")")

	// No input reached the app: the button was never invoked.
	st = fix.waitState(t, 10*time.Second, func(s *fixture.State) bool { return s.DialogOpen })
	if st.LastButton != "" {
		t.Errorf("input reached the disabled main window: lastButton = %q", st.LastButton)
	}
}

func TestActivateWindowStandalone(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	// Activating the fixture's Window works on its own.
	stdout, stderr, exit := runActivateWindow(t, window)
	assertActionOK(t, "activate-window", stdout, stderr, exit)

	// While the modal dialog is open, the dialog Window activates fine...
	saveAs := mainElementIndexes()[fixture.ElemButtonSaveAs]
	if _, _, exit := runClick(t, []string{window, strconv.Itoa(saveAs)}); exit != 0 {
		t.Fatalf("click button-save-as: exit %d", exit)
	}
	st := fix.waitState(t, 10*time.Second, func(s *fixture.State) bool { return s.DialogOpen })
	dialog := strconv.FormatUint(st.DialogWindowID, 10)

	stdout, stderr, exit = runActivateWindow(t, dialog)
	assertActionOK(t, "activate-window (dialog)", stdout, stderr, exit)

	// ...and the main Window behind it is refused with the dialog named.
	stdout, stderr, exit = runActivateWindow(t, window)
	assertActionFailed(t, "activate-window (blocked main)", stdout, stderr, exit,
		"has a modal dialog open (window "+dialog+")")
}

func TestClickFlagsAnywhereAndJoined(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	idx := mainElementIndexes()[fixture.ElemButtonClick]

	// Flags after the positionals, =joined: still parsed (agents write
	// both shapes).
	stdout, stderr, exit := runClick(t, []string{window, strconv.Itoa(idx), "--button=left", "--count", "1"})
	assertActionOK(t, "click with joined flags", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool {
		return s.LastButton == "Click Me"
	})
}
