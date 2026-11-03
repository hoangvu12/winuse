//go:build e2e

// select-text (ticket 10 of headless-max): `select-text <window>
// <element> <start> <length>` and `select-text <window> <element> all`
// with no keys, no mouse and no foreground. The targets are the covered
// fixture's Classic Dialog name Edit (EM_SETSEL, then type-text replacing
// the selection) and the Windows 11 Notepad editor where installed
// (RichEditD2DPT, EM_SETSEL, read back with EM_GETSEL); the Page input is covered in
// browser_pages_test.go's pageActions. User-run only (see the package
// comment and test/e2e/README.md); an agent must never start them.
//
// The UIA Text pattern route (WPF, WinUI, Chromium's frame) has no
// target here yet and is unmeasured headless.

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
	"github.com/hoangvu12/winuse/internal/win"
)

// runSelectText invokes `winuse select-text <args...>`.
func runSelectText(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	return runWinuse(t, append([]string{"select-text"}, args...), "WINUSE_HELPER_IDLE="+e2eIdle)
}

// readState reads a Window's State, failing the test on an error.
func readState(t *testing.T, window string) string {
	t.Helper()
	stdout, stderr, exit := runWinuse(t, []string{"get-window-state", window}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("get-window-state %s: exit %d, stderr:\n%s", window, exit, stderr)
	}
	return stdout
}

func TestHeadlessSelectTextInClassicEdit(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)
	dialog := openClassicDialog(t, fix, window, cover)
	logKeyTarget(t, mustWindowID(t, dialog))
	pointer := pointerPos(t)

	stdout, stderr, exit := runTypeText(t, []string{dialog, "hello world"})
	assertActionOK(t, "type-text into the Classic Dialog's Edit", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicText == "hello world" })

	// The name Edit by its AutomationId, the control ID UI Automation
	// gives a dialog's child.
	state := readState(t, dialog)
	edit := "id:" + strconv.Itoa(fixture.ClassicEditID)
	if !strings.Contains(state, "ID: "+strconv.Itoa(fixture.ClassicEditID)) {
		t.Logf("the Classic Dialog's Window State:\n%s", state)
	}

	// "world" is 5 units from 6; the replacement goes through EM_REPLACESEL.
	res, overlaySeen := runWatched(t, []string{"select-text", dialog, edit, "6", "5"})
	assertActionOK(t, "select-text in the Classic Dialog's Edit", res.stdout, res.stderr, res.exit)
	assertForeground(t, "select-text in a classic Edit", cover)
	if overlaySeen {
		t.Error("the in-control banner was shown during select-text")
	}
	if got := pointerPos(t); got != pointer {
		t.Errorf("the real pointer moved from %v to %v during select-text", pointer, got)
	}
	stdout, stderr, exit = runTypeText(t, []string{dialog, "winuse"})
	assertActionOK(t, "type-text replacing the selection", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicText == "hello winuse" })

	// all, then a replacement of the whole text.
	stdout, stderr, exit = runSelectText(t, dialog, edit, "all")
	assertActionOK(t, "select-text all", stdout, stderr, exit)
	stdout, stderr, exit = runTypeText(t, []string{dialog, "replaced"})
	assertActionOK(t, "type-text replacing all", stdout, stderr, exit)
	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicText == "replaced" })
	if st.ClassicPassword != "" {
		t.Errorf("text reached the password Edit: %q", st.ClassicPassword)
	}
	assertForeground(t, "select-text all and type-text", cover)

	// A span past the end is refused with nothing sent.
	stdout, stderr, exit = runSelectText(t, dialog, edit, "5", "10")
	if exit == 0 || !strings.Contains(stderr, "past the end of the text") || !strings.Contains(stderr, "nothing was sent") {
		t.Errorf("select-text past the end: exit %d, stdout %q, stderr %q", exit, stdout, stderr)
	}
}

func TestHeadlessSelectTextInNotepad(t *testing.T) {
	optIn(t)
	all, err := win.Collect()
	if err != nil {
		t.Fatalf("enumerating windows: %v", err)
	}
	for _, w := range all {
		if w.Class == "Notepad" {
			t.Skip("Notepad is already running: close it first (this test ends the Notepad process it selects in)")
		}
	}

	name := "winuse-e2e-select-" + strconv.FormatInt(time.Now().UnixNano(), 36) + ".txt"
	path := filepath.Join(t.TempDir(), name)
	// The text is in the file, not typed: once covered, Windows 11
	// Notepad reports no focused control, so plain type-text has no
	// Headless route there and is refused without --headed (type-text
	// --into the editor's Element is Headless;
	// TestHeadlessTypeTextIntoNotepad).
	if err := os.WriteFile(path, []byte("hello notepad"), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	if err := exec.Command("notepad.exe", path).Start(); err != nil {
		t.Skipf("starting notepad.exe: %v", err)
	}
	id := findWindowByTitle(t, strings.TrimSuffix(name, ".txt"), 30*time.Second)
	defer func() {
		if win.WindowOpen(id) {
			killWindowProcess(t, id)
		}
	}()
	window := strconv.FormatUint(id, 10)

	var focus win.Control
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if focus = win.FocusedControl(id); focus.HWND != 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.EqualFold(focus.Class, "RichEditD2DPT") {
		t.Skipf("Notepad's focused control is %q, not RichEditD2DPT: this is not the Windows 11 Notepad", focus.Class)
	}

	cover := coverWindow(t, window)

	// The editor's Element: the one line of the Window State named "Text
	// editor" (Windows 11 Notepad's editor), whatever its role.
	state := readState(t, window)
	m := regexp.MustCompile(`(?m)^\t+(\d+) [^\n]*Text editor`).FindAllStringSubmatch(state, -1)
	if len(m) != 1 {
		t.Fatalf("want one Element named Text editor in Notepad's Window State, found %d:\n%s", len(m), state)
	}
	editor := m[0][1]

	res, overlaySeen := runWatched(t, []string{"select-text", window, editor, "6", "7"})
	assertActionOK(t, "select-text in Notepad", res.stdout, res.stderr, res.exit)
	assertForeground(t, "select-text in Notepad", cover)
	if overlaySeen {
		t.Error("the in-control banner was shown during select-text")
	}
	assertEditSelection(t, "select-text 6 7 in Notepad", focus.HWND, 6, 13)

	stdout, stderr, exit := runSelectText(t, window, editor, "all")
	assertActionOK(t, "select-text all in Notepad", stdout, stderr, exit)
	assertForeground(t, "select-text all in Notepad", cover)
	assertEditSelection(t, "select-text all in Notepad", focus.HWND, 0, 13)
}

// assertEditSelection reads an edit control's selection (EM_GETSEL, read
// only) and wants start to end.
func assertEditSelection(t *testing.T, what string, edit uint64, start, end int) {
	t.Helper()
	st, err := win.ReadEditState(edit)
	if err != nil {
		t.Fatalf("%s: reading the editor's selection: %v", what, err)
	}
	if !st.SelKnown || min(st.SelStart, st.SelEnd) != start || max(st.SelStart, st.SelEnd) != end {
		t.Errorf("%s: the editor selects %d to %d (known %v), want %d to %d", what, st.SelStart, st.SelEnd, st.SelKnown, start, end)
	}
}
