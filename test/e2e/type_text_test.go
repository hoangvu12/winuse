//go:build e2e

// The ticket-06 seam-1 tests: `winuse type-text` and `winuse press-key`
// against the fixture app, on the live desktop. User-run only (see the
// package comment and test/e2e/README.md for the opt-in rules); an agent
// must never start them.
//
// The typing assertions read the fixture's state file (Text, Typed,
// TextSelected, LastKey) — never pixels. Element Indexes and the
// fixture's Element contract come from internal/fixture.

package e2e

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
)

// runTypeText invokes `winuse type-text ...` with the suite's short helper
// idle timeout.
func runTypeText(t *testing.T, args []string, extraEnv ...string) (string, string, int) {
	t.Helper()
	return runWinuse(t, append([]string{"type-text"}, args...), append(extraEnv, "WINUSE_HELPER_IDLE="+e2eIdle)...)
}

// runPressKey invokes `winuse press-key ...`.
func runPressKey(t *testing.T, args []string, extraEnv ...string) (string, string, int) {
	t.Helper()
	return runWinuse(t, append([]string{"press-key"}, args...), append(extraEnv, "WINUSE_HELPER_IDLE="+e2eIdle)...)
}

func TestTypeTextTypesUnicode(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	// Latin, non-Latin scripts, combining-free emoji (a surrogate pair
	// arrives as two WM_CHAR messages the fixture must recombine) and
	// punctuation, in one typed string.
	text := "Hello, 世界! Caña 🎉 déjà-vu"
	stdout, stderr, exit := runTypeText(t, []string{window, text})
	assertActionOK(t, "type-text", stdout, stderr, exit)

	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Text == text })
	if st.Typed != text {
		t.Errorf("typed stream = %q, want %q", st.Typed, text)
	}
}

func TestTypeTextWordsJoinWithoutQuotes(t *testing.T) {
	// Agents may pass multi-word text unquoted: every argument after the
	// Window is part of the text, joined with single spaces.
	optIn(t)
	fix, window := startFixtureWithState(t)

	stdout, stderr, exit := runTypeText(t, []string{window, "hello", "brave", "world"})
	assertActionOK(t, "type-text unquoted words", stdout, stderr, exit)

	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Text == "hello brave world" })
}

func TestTypeTextLongText(t *testing.T) {
	// A text longer than one SendInput batch (typing is batched 16 runes
	// per call): every batch must land, in order.
	optIn(t)
	fix, window := startFixtureWithState(t)

	text := strings.Repeat("αβγdéjà ", 30) // 240 runes, several batches
	stdout, stderr, exit := runTypeText(t, []string{window, text})
	assertActionOK(t, "type-text long text", stdout, stderr, exit)

	fix.waitState(t, 10*time.Second, func(s *fixture.State) bool { return s.Text == text })
}

func TestTypeTextSelectAllAndReplace(t *testing.T) {
	// The ticket's acceptance flow: type into the text field, Control_L+a
	// selects everything, and a replacement leaves exactly the new text.
	optIn(t)
	fix, window := startFixtureWithState(t)

	stdout, stderr, exit := runTypeText(t, []string{window, "Hello world"})
	assertActionOK(t, "type-text", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Text == "Hello world" })

	stdout, stderr, exit = runPressKey(t, []string{window, "Control_L+a"})
	assertActionOK(t, "press-key Control_L+a", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.TextSelected })

	stdout, stderr, exit = runTypeText(t, []string{window, "Bye"})
	assertActionOK(t, "type-text replacement", stdout, stderr, exit)
	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Text == "Bye" })
	if st.TextSelected {
		t.Errorf("typing over a selection must clear it (textSelected still true)")
	}
}

func TestTypeTextBackspaceReplacesSelection(t *testing.T) {
	// Backspace over a selection deletes the whole selection, like a real
	// edit control.
	optIn(t)
	fix, window := startFixtureWithState(t)

	if _, _, exit := runTypeText(t, []string{window, "scratch"}); exit != 0 {
		t.Fatalf("type-text: exit %d", exit)
	}
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Text == "scratch" })
	if _, _, exit := runPressKey(t, []string{window, "Control_L+a"}); exit != 0 {
		t.Fatalf("press-key: exit %d", exit)
	}
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.TextSelected })

	if _, _, exit := runPressKey(t, []string{window, "BackSpace"}); exit != 0 {
		t.Fatalf("press-key BackSpace: exit %d", exit)
	}
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Text == "" })
}

func TestTypeTextIntoDialogEdit(t *testing.T) {
	// The Save As dialog's file-name edit is focused when the dialog
	// opens, so type-text into the dialog Window lands there.
	optIn(t)
	fix, window := startFixtureWithState(t)

	saveAs := mainElementIndexes()[fixture.ElemButtonSaveAs]
	if _, _, exit := runClick(t, []string{window, strconv.Itoa(saveAs)}); exit != 0 {
		t.Fatalf("click button-save-as: exit %d", exit)
	}
	st := fix.waitState(t, 10*time.Second, func(s *fixture.State) bool { return s.DialogOpen && s.DialogWindowID != 0 })
	dialog := strconv.FormatUint(st.DialogWindowID, 10)

	stdout, stderr, exit := runTypeText(t, []string{dialog, "report.txt"})
	assertActionOK(t, "type-text into the dialog", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.DialogFileName == "report.txt" })
}

func TestPressKeyNames(t *testing.T) {
	// Named keys reach the app as their virtual keys: the fixture records
	// each WM_KEYDOWN as a key name (keyName), so the assertion covers
	// exactly what arrived.
	optIn(t)
	fix, window := startFixtureWithState(t)

	cases := []struct {
		key  string // the key name given to press-key
		want string // the key name the fixture records
	}{
		{"Return", "Return"},
		{"ENTER", "Return"}, // alias
		{"Tab", "Tab"},
		{"Escape", "Escape"},
		{"space", "space"},
		{"F5", "F5"},
		{"f12", "F12"},
		{"Left", "Left"}, // extended keys arrive as arrows, not numpad keys
		{"Right", "Right"},
		{"Down", "Down"},
		{"Prior", "Prior"}, // Page Up
		{"BackSpace", "BackSpace"},
		{"q", "q"},
		{"7", "7"},
		// The numpad keys are distinct from the main row (the point of the
		// KP_ names): the fixture reports the raw numpad VK.
		{"Numpad_5", "vk-0x00000065"},
		{"Numpad_Add", "vk-0x0000006b"},
	}
	for _, tc := range cases {
		stdout, stderr, exit := runPressKey(t, []string{window, tc.key})
		if exit != 0 {
			t.Fatalf("press-key %q: exit %d, stderr:\n%s", tc.key, exit, stderr)
		}
		if stdout != "ok\n" {
			t.Errorf("press-key %q: stdout = %q, want %q", tc.key, stdout, "ok\n")
		}
		fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.LastKey == tc.want })
	}
}

func TestPressKeyChord(t *testing.T) {
	// A chord: key-downs in order, key-ups in reverse. The fixture sees
	// the final key-down (a), with the Control held (Ctrl+A selects the
	// focused edit).
	optIn(t)
	fix, window := startFixtureWithState(t)

	stdout, stderr, exit := runPressKey(t, []string{window, "Control_L+a"})
	assertActionOK(t, "press-key chord", stdout, stderr, exit)
	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.TextSelected })
	if st.LastKey != "a" {
		t.Errorf("lastKey = %q, want \"a\" (the chord's final key-down)", st.LastKey)
	}
}

func TestPressKeyUnknownKeyFailsWithoutInput(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	// A state generation to compare against: the refused key must not
	// activate the Window or send any input.
	before := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return true })

	stdout, stderr, exit := runPressKey(t, []string{window, "NotAKey"})
	assertActionFailed(t, "press-key unknown key", stdout, stderr, exit, "unsupported key: NotAKey")
	if !strings.Contains(stderr, "keysym") {
		t.Errorf("the failure needs a hint naming the accepted key names: %q", stderr)
	}

	time.Sleep(300 * time.Millisecond)
	after := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return true })
	if after.Generation != before.Generation {
		t.Errorf("a refused key changed the fixture state (generation %d -> %d): input must not be sent",
			before.Generation, after.Generation)
	}
}

func TestPressKeyWindowsKeyRefused(t *testing.T) {
	// The Windows key is never sent (Codex's rule), under every alias.
	optIn(t)
	_, window := startFixtureWithState(t)

	for _, key := range []string{"Win", "Windows", "Meta", "Command", "Cmd", "Super", "OS", "Super_L"} {
		stdout, stderr, exit := runPressKey(t, []string{window, key})
		assertActionFailed(t, "press-key "+key, stdout, stderr, exit,
			"unsupported key: "+key+" (the Windows key is never sent)")
		if !strings.Contains(stderr, "never sends the Windows key") {
			t.Errorf("the failure needs the Windows-key hint: %q", stderr)
		}
	}
}

func TestTypeTextUsageErrors(t *testing.T) {
	// Argument-count failures are usage errors: exit 2, no helper dial.
	optIn(t)

	stdout, stderr, exit := runTypeText(t, []string{})
	if exit != 2 || stdout != "" || !strings.Contains(stderr, "usage: winuse type-text <window> <text>") {
		t.Errorf("type-text with no arguments: exit %d, stdout %q, stderr %q", exit, stdout, stderr)
	}
	stdout, stderr, exit = runTypeText(t, []string{"101"})
	if exit != 2 || !strings.Contains(stderr, "usage: winuse type-text") {
		t.Errorf("type-text with only a window: exit %d, stderr %q", exit, stderr)
	}
	stdout, stderr, exit = runPressKey(t, []string{"101"})
	if exit != 2 || !strings.Contains(stderr, "usage: winuse press-key") {
		t.Errorf("press-key with only a window: exit %d, stderr %q", exit, stderr)
	}
}

func TestPressKeyEmptyKeyIsRefused(t *testing.T) {
	// An explicitly empty key argument is a command failure (Codex's
	// "key is required"), not a usage error: the CLI forwards it and the
	// helper refuses before resolving the Window or activating anything.
	optIn(t)
	_, window := startFixtureWithState(t)

	stdout, stderr, exit := runPressKey(t, []string{window, ""})
	assertActionFailed(t, "press-key empty key", stdout, stderr, exit, "key is required")
	if !strings.Contains(stderr, "hint: ") {
		t.Errorf("the failure needs a recovery hint: %q", stderr)
	}
}
