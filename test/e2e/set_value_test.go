//go:build e2e

// The ticket-07 seam-1 tests: `winuse set-value` and `winuse
// perform-secondary-action` against the fixture app, on the live desktop.
// User-run only (see the package comment and test/e2e/README.md for the
// opt-in rules); an agent must never start them.
//
// Element Indexes come from the fixture's Element contract
// (internal/fixture): the tests flatten the same depth-first order the
// Window State numbers, against the State the fixture is in when the
// cached Window State was read.

package e2e

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
)

// runSetvalue invokes `winuse set-value <window> <index> <value>` with the
// suite's short helper idle timeout. The value is passed as one argument
// (the CLI also joins unquoted multi-argument values, tested below).
func runSetvalue(t *testing.T, window string, index int, value string, extraEnv ...string) (string, string, int) {
	t.Helper()
	return runWinuse(t, []string{"set-value", window, strconv.Itoa(index), value},
		append(extraEnv, "WINUSE_HELPER_IDLE="+e2eIdle)...)
}

// runSecondary invokes `winuse perform-secondary-action <window> <index>
// <action>`.
func runSecondary(t *testing.T, window string, index int, action string, extraEnv ...string) (string, string, int) {
	t.Helper()
	return runWinuse(t, []string{"perform-secondary-action", window, strconv.Itoa(index), action},
		append(extraEnv, "WINUSE_HELPER_IDLE="+e2eIdle)...)
}

func TestSetValueSetsTheTextField(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	idx := mainElementIndexes()[fixture.ElemTextField]
	stdout, stderr, exit := runSetvalue(t, window, idx, "hello winuse")
	assertActionOK(t, "set-value text-field", stdout, stderr, exit)

	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Text == "hello winuse" })
	// The Element was focused first (Codex's set_value order); the fixture
	// records focus as the text field.
	if st.Text != "hello winuse" {
		t.Fatalf("text = %q, want %q", st.Text, "hello winuse")
	}

	// A fresh Window State shows the value in the Element's line.
	stdout, stderr, exit = runGetWindowState(t, window)
	if exit != 0 {
		t.Fatalf("get-window-state: exit %d, stderr:\n%s", exit, stderr)
	}
	want := fmt.Sprintf("\t%d edit (settable, string) Text Field Value: hello winuse ID: %s",
		idx, fixture.AutomationID(fixture.ElemTextField))
	if !strings.Contains(stdout, want) {
		t.Errorf("the Window State must show the set value:\nwant %q\n got:\n%s", want, stdout)
	}
}

func TestSetValueSetsTheDialogFileName(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	// Open the Save As dialog, read its Window State (so its Elements are
	// cached), then set the file-name edit's value.
	saveAs := mainElementIndexes()[fixture.ElemButtonSaveAs]
	if _, _, exit := runClick(t, []string{window, strconv.Itoa(saveAs)}); exit != 0 {
		t.Fatalf("click button-save-as: exit %d", exit)
	}
	st := fix.waitState(t, 10*time.Second, func(s *fixture.State) bool { return s.DialogOpen && s.DialogWindowID != 0 })
	dialog := strconv.FormatUint(st.DialogWindowID, 10)
	if _, _, exit := runGetWindowState(t, dialog); exit != 0 {
		t.Fatalf("get-window-state of the dialog: exit %d", exit)
	}

	idx := dialogElementIndexes()[fixture.ElemDialogEditFileName]
	stdout, stderr, exit := runSetvalue(t, dialog, idx, "report q4.docx")
	assertActionOK(t, "set-value dialog-edit-file-name", stdout, stderr, exit)

	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.DialogFileName == "report q4.docx" })
}

func TestSetValueJoinsUnquotedValueArguments(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	idx := mainElementIndexes()[fixture.ElemTextField]
	// An unquoted value with spaces still reaches the Element as one
	// string (the CLI joins the trailing arguments).
	stdout, stderr, exit := runWinuse(t, []string{"set-value", window, strconv.Itoa(idx), "hello", "winuse", "world"},
		"WINUSE_HELPER_IDLE="+e2eIdle)
	assertActionOK(t, "set-value with joined value", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Text == "hello winuse world" })
}

func TestSetValueOnNonSettableElementFails(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	// A plain button offers no value pattern: Codex's refusal, with the
	// hint pointing at type-text.
	idx := mainElementIndexes()[fixture.ElemButtonClick]
	stdout, stderr, exit := runSetvalue(t, window, idx, "nope")
	assertActionFailed(t, "set-value on a button", stdout, stderr, exit,
		"Cannot set a value for an element that is not settable")
	if !strings.Contains(stderr, "type-text") {
		t.Errorf("the failure must hint at type-text: %q", stderr)
	}
	// No value reached the app.
	st := fix.waitState(t, time.Second, func(s *fixture.State) bool { return true })
	if st.Text != "" {
		t.Errorf("text = %q, want empty (nothing was set)", st.Text)
	}
}

func TestSecondaryActionTogglesTheCheckbox(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	idx := mainElementIndexes()[fixture.ElemCheckboxNotify]
	stdout, stderr, exit := runSecondary(t, window, idx, "toggle")
	assertActionOK(t, "perform-secondary-action toggle", stdout, stderr, exit)

	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Checkbox })
	if !st.Checkbox {
		t.Fatalf("checkbox state = %v, want true", st.Checkbox)
	}

	// A fresh Window State renders the new toggle state (the "on" token)
	// beside the Element's Secondary Actions.
	stdout, stderr, exit = runGetWindowState(t, window)
	if exit != 0 {
		t.Fatalf("get-window-state: exit %d, stderr:\n%s", exit, stderr)
	}
	want := fmt.Sprintf("\t%d check box Notify me on Secondary Actions: Toggle ID: %s",
		idx, fixture.AutomationID(fixture.ElemCheckboxNotify))
	if !strings.Contains(stdout, want) {
		t.Errorf("the Window State must show the toggled state:\nwant %q\n got:\n%s", want, stdout)
	}
}

func TestSecondaryActionExpandsAndCollapsesTheMenu(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	// Expand the File menu: the dropdown's items appear in the fixture's
	// tree, and the Window State line flips to (expanded) ... Collapse.
	idx := mainElementIndexes()[fixture.ElemMenuFile]
	stdout, stderr, exit := runSecondary(t, window, idx, "Expand")
	assertActionOK(t, "perform-secondary-action expand", stdout, stderr, exit)

	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool {
		return s.OpenMenu == string(fixture.ElemMenuFile)
	})
	if st.OpenMenu != string(fixture.ElemMenuFile) {
		t.Fatalf("openMenu = %q, want %q", st.OpenMenu, fixture.ElemMenuFile)
	}

	stdout, stderr, exit = runGetWindowState(t, window)
	if exit != 0 {
		t.Fatalf("get-window-state: exit %d, stderr:\n%s", exit, stderr)
	}
	expanded := fmt.Sprintf("\t%d menu item (expanded) File Secondary Actions: Collapse ID: %s",
		idx, fixture.AutomationID(fixture.ElemMenuFile))
	if !strings.Contains(stdout, expanded) {
		t.Errorf("the expanded menu must render (expanded) with Collapse:\nwant %q\n got:\n%s", expanded, stdout)
	}
	for _, item := range []fixture.Elem{fixture.ElemMenuFileNew, fixture.ElemMenuFileOpen, fixture.ElemMenuFileExit} {
		if !strings.Contains(stdout, " ID: "+fixture.AutomationID(item)) {
			t.Errorf("the expanded menu must expose %s:\n%s", item, stdout)
		}
	}

	// Collapse it again. The fresh Window State above renumbered the
	// Elements (the menu items are in the tree now), so the index comes
	// from the expanded tree's contract.
	expandedIdx := elementIndexes(fixture.BuildTree(fixture.State{OpenMenu: string(fixture.ElemMenuFile)}),
		fixture.ElemWindow)[fixture.ElemMenuFile]
	stdout, stderr, exit = runSecondary(t, window, expandedIdx, "Collapse")
	assertActionOK(t, "perform-secondary-action collapse", stdout, stderr, exit)

	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.OpenMenu == "" })
}

func TestSecondaryActionSelectsAListItem(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	idx := mainElementIndexes()[fixture.ElemItemBravo]
	stdout, stderr, exit := runSecondary(t, window, idx, "Select")
	assertActionOK(t, "perform-secondary-action select", stdout, stderr, exit)

	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.SelectedItem == "Bravo" })
	if st.SelectedItem != "Bravo" {
		t.Fatalf("selectedItem = %q, want Bravo", st.SelectedItem)
	}

	// A fresh Window State marks the selected item.
	stdout, stderr, exit = runGetWindowState(t, window)
	if exit != 0 {
		t.Fatalf("get-window-state: exit %d, stderr:\n%s", exit, stderr)
	}
	want := fmt.Sprintf("\t%d list item (selectable, selected) Bravo Secondary Actions: Select ID: %s",
		idx, fixture.AutomationID(fixture.ElemItemBravo))
	if !strings.Contains(stdout, want) {
		t.Errorf("the Window State must show the selected item:\nwant %q\n got:\n%s", want, stdout)
	}
}

func TestSecondaryActionScrollsTheListDown(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	// The list offers Scroll Down (it scrolls vertically); one large step
	// moves a viewport of items.
	idx := mainElementIndexes()[fixture.ElemItemList]
	stdout, stderr, exit := runSecondary(t, window, idx, "Scroll Down")
	assertActionOK(t, "perform-secondary-action scroll down", stdout, stderr, exit)

	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool {
		return s.ListFirstVisible == fixture.ListVisibleItems
	})
	if st.ListFirstVisible != fixture.ListVisibleItems {
		t.Fatalf("list scroll position = %d, want %d", st.ListFirstVisible, fixture.ListVisibleItems)
	}
}

func TestSecondaryActionRaiseOnTheWindowRoot(t *testing.T) {
	optIn(t)
	_, window := startFixtureWithState(t)

	// The fixture's window root offers Raise (Window pattern).
	idx := mainElementIndexes()[fixture.ElemWindow]
	stdout, stderr, exit := runSecondary(t, window, idx, "Raise")
	assertActionOK(t, "perform-secondary-action raise", stdout, stderr, exit)
}

func TestUnsupportedSecondaryActionFails(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	stdout, stderr, exit := runSecondary(t, window, 1, "Fly")
	assertActionFailed(t, "unsupported secondary action", stdout, stderr, exit,
		"unsupported secondary action: Fly; expected Raise, Scroll Up, Scroll Down, Scroll Left, Scroll Right, Expand, Collapse, Toggle, or Select")
	if !strings.Contains(stderr, "hint: ") {
		t.Errorf("the failure needs a recovery hint: %q", stderr)
	}

	// Nothing happened to the app.
	st := fix.waitState(t, time.Second, func(s *fixture.State) bool { return true })
	if st.Checkbox || st.OpenMenu != "" || st.SelectedItem != "" {
		t.Errorf("an unsupported action must not change the app: %+v", st)
	}
}

func TestSecondaryActionTheElementDoesNotOfferFails(t *testing.T) {
	optIn(t)
	_, window := startFixtureWithState(t)

	// The check box offers Toggle only: requesting Expand fails listing
	// what the Element offers.
	idx := mainElementIndexes()[fixture.ElemCheckboxNotify]
	stdout, stderr, exit := runSecondary(t, window, idx, "Expand")
	assertActionFailed(t, "secondary action not offered", stdout, stderr, exit,
		fmt.Sprintf("secondary action 'Expand' is not available for element %d; available actions: Toggle", idx))
	if !strings.Contains(stderr, "get-window-state") {
		t.Errorf("the failure must instruct a fresh Window State read: %q", stderr)
	}

	// An Element with no Secondary Actions at all (the status text) says
	// so explicitly.
	idx = mainElementIndexes()[fixture.ElemStatus]
	stdout, stderr, exit = runSecondary(t, window, idx, "Toggle")
	assertActionFailed(t, "secondary action on an Element with none", stdout, stderr, exit,
		fmt.Sprintf("secondary action 'Toggle' is not available for element %d; available actions: none", idx))
}

func TestSetValueStaleElementIndex(t *testing.T) {
	optIn(t)
	_, window := startFixtureWithState(t)

	stdout, stderr, exit := runSetvalue(t, window, 999, "x")
	assertActionFailed(t, "set-value stale index", stdout, stderr, exit,
		"element 999 is not available in the cached Window State for window "+window)
	if !strings.Contains(stderr, "get-window-state") {
		t.Errorf("the stale failure must instruct a fresh Window State read: %q", stderr)
	}
}

func TestSetValueByPoint(t *testing.T) {
	// The @x,y Element Reference (finding 8): the point of the latest
	// screenshot names the Element under it — here the text field's
	// centre — and set-value writes into that Element without any index.
	optIn(t)
	fix, window, info := startFixtureForShots(t)

	r, _ := fixture.Rect(fixture.ElemTextField, 0)
	shotX, shotY := shotCoordsOfClient(t, fix, window, info.scale, (r.Left+r.Right)/2, (r.Top+r.Bottom)/2)
	stdout, stderr, exit := runWinuse(t, []string{"set-value", window, fmt.Sprintf("@%d,%d", shotX, shotY), "typed at a point"},
		"WINUSE_HELPER_IDLE="+e2eIdle)
	assertActionOK(t, "set-value @x,y", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Text == "typed at a point" })
}

func TestPerformSecondaryActionByPoint(t *testing.T) {
	// The @x,y form for perform-secondary-action: the checkbox's centre
	// names the Element under it, and Toggle flips it.
	optIn(t)
	fix, window, info := startFixtureForShots(t)

	r, _ := fixture.Rect(fixture.ElemCheckboxNotify, 0)
	shotX, shotY := shotCoordsOfClient(t, fix, window, info.scale, (r.Left+r.Right)/2, (r.Top+r.Bottom)/2)
	stdout, stderr, exit := runWinuse(t, []string{"perform-secondary-action", window, fmt.Sprintf("@%d,%d", shotX, shotY), "Toggle"},
		"WINUSE_HELPER_IDLE="+e2eIdle)
	assertActionOK(t, "perform-secondary-action @x,y", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Checkbox })
}
