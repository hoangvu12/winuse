//go:build e2e

// The classic-window keyboard routes (tickets 05 and 06 of
// headless-parity): press-key Return and Escape on a #32770 dialog
// through the dialog manager's WM_COMMAND, and type-text into a focused
// Win32 edit control through EM_REPLACESEL, all without the foreground.
// The targets are the fixture's Classic Dialog and its MessageBox (real
// system dialogs and controls; see fixture.ClassicDialogTitle), and the
// Windows 11 Notepad editor where installed. User-run only (see the
// package comment and test/e2e/README.md); an agent must never start
// them.
//
// These routes are decided from the live Window: the focused control
// that GetGUIThreadInfo reports for the Window's thread, and the dialog's
// default button. When either reads differently from what the offline
// tests assume (a background thread with no focus, a MessageBox with no
// DM_GETDEFID answer), the Action has no Headless route and, sent without
// --headed, is refused with nothing sent (headless-max ticket 21), so the
// test fails on the exit and the refusal names the reason. Each test logs
// the route probe first, so the failure shows what winuse read.

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
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/hoangvu12/winuse/internal/fixture"
	"github.com/hoangvu12/winuse/internal/win"
)

// openClassicDialog opens the fixture's Classic Dialog with a Headless
// click on its main-window button, waits until its name Edit has the
// focus, and puts the covering Window back in front of it. It returns the
// dialog's Window reference.
func openClassicDialog(t *testing.T, fix *fixtureProc, window string, cover uint64) string {
	t.Helper()
	idx := mainElementIndexes()[fixture.ElemButtonClassic]
	stdout, stderr, exit := runClick(t, []string{window, strconv.Itoa(idx)})
	assertActionOK(t, "Headless click (Classic Dialog...)", stdout, stderr, exit)
	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool {
		return s.ClassicOpen && s.ClassicWindowID != 0 && s.ClassicFocus == "name"
	})
	coverAgain(t, cover)
	assertAbove(t, cover, st.ClassicWindowID)
	return strconv.FormatUint(st.ClassicWindowID, 10)
}

// openMessageBox opens the fixture's MessageBox with a Headless click on
// its main-window button, finds its Window, and puts the covering Window
// back in front of it. It returns the box's Window reference.
func openMessageBox(t *testing.T, fix *fixtureProc, window string, cover uint64) string {
	t.Helper()
	idx := mainElementIndexes()[fixture.ElemButtonMessageBox]
	stdout, stderr, exit := runClick(t, []string{window, strconv.Itoa(idx)})
	assertActionOK(t, "Headless click (Message Box...)", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.MessageBoxOpen })
	box := findWindowOf(t, uint32(fix.proc.Pid), win.DialogClass, fixture.MessageBoxTitle, 5*time.Second)
	coverAgain(t, cover)
	assertAbove(t, cover, box)
	return strconv.FormatUint(box, 10)
}

func TestHeadlessReturnAndEscapeOnClassicDialog(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)

	for _, c := range []struct{ key, result string }{
		{"Return", "ok"},
		{"Escape", "cancel"},
	} {
		dialog := openClassicDialog(t, fix, window, cover)
		logKeyTarget(t, mustWindowID(t, dialog))

		res, overlaySeen := runWatched(t, []string{"press-key", dialog, c.key})
		assertActionOK(t, "press-key "+c.key+" on the Classic Dialog", res.stdout, res.stderr, res.exit)
		fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return !s.ClassicOpen && s.ClassicResult == c.result })
		assertForeground(t, "press-key "+c.key+" on the Classic Dialog", cover)
		if overlaySeen {
			t.Errorf("press-key %s: the in-control banner was shown during a Headless key", c.key)
		}
		if win.WindowOpen(mustWindowID(t, dialog)) {
			t.Errorf("press-key %s: the Classic Dialog is still open", c.key)
		}
	}
}

func TestHeadlessReturnAndEscapeOnMessageBox(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)

	for _, c := range []struct{ key, result string }{
		{"Return", "ok"},
		{"Escape", "cancel"},
	} {
		box := openMessageBox(t, fix, window, cover)
		logKeyTarget(t, mustWindowID(t, box))

		res, overlaySeen := runWatched(t, []string{"press-key", box, c.key})
		assertActionOK(t, "press-key "+c.key+" on the MessageBox", res.stdout, res.stderr, res.exit)
		fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return !s.MessageBoxOpen && s.MessageBoxResult == c.result })
		assertForeground(t, "press-key "+c.key+" on the MessageBox", cover)
		if overlaySeen {
			t.Errorf("press-key %s: the in-control banner was shown during a Headless key", c.key)
		}
	}
}

func TestHeadlessTypeTextIntoClassicEdit(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)
	dialog := openClassicDialog(t, fix, window, cover)
	logKeyTarget(t, mustWindowID(t, dialog))
	pointer := pointerPos(t)

	// Non-ASCII text goes through EM_REPLACESEL as UTF-16, like typing.
	text := "hello classic édit 世界"
	res, overlaySeen := runWatched(t, []string{"type-text", dialog, text})
	assertActionOK(t, "type-text into the Classic Dialog's Edit", res.stdout, res.stderr, res.exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicText == text })
	assertForeground(t, "type-text into a classic Edit", cover)
	if overlaySeen {
		t.Error("the in-control banner was shown during a Headless type-text")
	}
	if got := pointerPos(t); got != pointer {
		t.Errorf("the real pointer moved from %v to %v during a Headless type-text", pointer, got)
	}

	// The text replaces the selection or inserts at the caret, like
	// typing: a second call appends.
	stdout, stderr, exit := runTypeText(t, []string{dialog, "!"})
	assertActionOK(t, "type-text appending", stdout, stderr, exit)
	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicText == text+"!" })
	if st.ClassicPassword != "" {
		t.Errorf("text reached the password Edit: %q", st.ClassicPassword)
	}
	assertForeground(t, "type-text appending", cover)
}

func TestHeadlessTypeTextIntoPasswordEdit(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)
	dialog := openClassicDialog(t, fix, window, cover)

	// A posted Tab reaches the dialog manager (the fixture's message loop
	// runs IsDialogMessage), which moves the focus to the password Edit
	// without the foreground.
	stdout, stderr, exit := runPressKey(t, []string{dialog, "Tab", "--post"})
	if exit != 0 || !strings.HasPrefix(stdout, postedOK) {
		t.Fatalf("press-key Tab --post: exit %d, stdout %q, stderr %q", exit, stdout, stderr)
	}
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicFocus == "password" })
	logKeyTarget(t, mustWindowID(t, dialog))

	// The text is verified by length only and never echoed: the output is
	// exactly "ok".
	secret := "s3cret-Pass word"
	stdout, stderr, exit = runTypeText(t, []string{dialog, secret})
	assertActionOK(t, "type-text into the password Edit", stdout, stderr, exit)
	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicPassword == secret })
	if st.ClassicText != "" {
		t.Errorf("text reached the name Edit: %q", st.ClassicText)
	}
	assertForeground(t, "type-text into a password Edit", cover)
}

// Cross-process reads of an edit control's text, for Notepad.
var procSendMessageTimeoutW = user32.NewProc("SendMessageTimeoutW")

// controlText reads a window's text from another process (WM_GETTEXT,
// which Windows marshals; read-only, 2 s timeout, given up on a hung app).
func controlText(t *testing.T, hwnd uint64) string {
	t.Helper()
	const (
		wmGetText       = 0x000D
		wmGetTextLength = 0x000E
		smtoAbortIfHung = 0x0002
	)
	var n uintptr
	if r, _, err := procSendMessageTimeoutW.Call(uintptr(hwnd), wmGetTextLength, 0, 0, smtoAbortIfHung, 2000, uintptr(unsafe.Pointer(&n))); r == 0 {
		t.Fatalf("WM_GETTEXTLENGTH to %d: %v", hwnd, err)
	}
	buf := make([]uint16, n+1)
	var copied uintptr
	if r, _, err := procSendMessageTimeoutW.Call(uintptr(hwnd), wmGetText, uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])), smtoAbortIfHung, 2000, uintptr(unsafe.Pointer(&copied))); r == 0 {
		t.Fatalf("WM_GETTEXT to %d: %v", hwnd, err)
	}
	return windows.UTF16ToString(buf)
}

func TestHeadlessTypeTextIntoNotepad(t *testing.T) {
	optIn(t)
	all, err := win.Collect()
	if err != nil {
		t.Fatalf("enumerating windows: %v", err)
	}
	for _, w := range all {
		if w.Class == "Notepad" {
			t.Skip("Notepad is already running: close it first (this test ends the Notepad process it types into)")
		}
	}

	// A temp file with a unique name: Notepad's title carries it.
	name := "winuse-e2e-notepad-" + strconv.FormatInt(time.Now().UnixNano(), 36) + ".txt"
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
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

	// Windows 11 Notepad edits in a RichEditD2DPT control; the classic
	// Notepad's Edit is covered by the fixture tests.
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
	logKeyTarget(t, id)

	// Covered, Windows 11 Notepad reports no focused control, so plain
	// type-text has no Headless route (headless-max ticket 19). Without
	// --headed it is refused with nothing sent, and the foreground stays
	// with the cover (ticket 21; it used to go Headed on its own and print
	// "ok (typed Headed: ...)").
	const refused = "not typed without --headed"
	stdout, stderr, exit := runTypeText(t, []string{window, refused})
	assertHeadedRefusal(t, "plain type-text into the covered Notepad", stdout, stderr, exit,
		"winuse --headed type-text "+window+" <text>")
	if !strings.Contains(stderr, "--into") {
		t.Errorf("the refusal must point at type-text --into: %q", stderr)
	}
	assertForeground(t, "plain type-text into the covered Notepad", cover)
	if got := controlText(t, focus.HWND); strings.Contains(got, refused) {
		t.Errorf("the refused text reached Notepad's editor: %q", got)
	}

	// --into names the editor's Element, whose own window is the
	// RichEditD2DPT: the one line of the Window State named "Text editor".
	state := readState(t, window)
	m := regexp.MustCompile(`(?m)^\t+(\d+) [^\n]*Text editor`).FindAllStringSubmatch(state, -1)
	if len(m) != 1 {
		t.Fatalf("want one Element named Text editor in Notepad's Window State, found %d:\n%s", len(m), state)
	}
	editor := m[0][1]

	text := "hello notepad from winuse"
	res, overlaySeen := runWatched(t, []string{"type-text", window, "--into", editor, text})
	assertActionOK(t, "type-text --into Notepad's editor", res.stdout, res.stderr, res.exit)
	assertForeground(t, "type-text --into Notepad's editor", cover)
	if overlaySeen {
		t.Error("the in-control banner was shown during type-text --into")
	}
	if got := controlText(t, focus.HWND); !strings.Contains(got, text) {
		t.Errorf("Notepad's editor holds %q, want it to contain %q", got, text)
	}
}

// TestHeadlessTypeTextIntoElement types into an Element by its reference,
// not the focus (desktop-parity ticket 23): with the focus in the Classic
// Dialog's name Edit, --into id:1002 types into the password Edit, and a
// Batch step --into id:1001 into the name Edit, all covered.
func TestHeadlessTypeTextIntoElement(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)
	dialog := openClassicDialog(t, fix, window, cover)
	logKeyTarget(t, mustWindowID(t, dialog))
	pointer := pointerPos(t)
	readState(t, dialog) // the references resolve against it

	// The name Edit holds the focus (openClassicDialog waits for it); the
	// text goes into the password Edit all the same.
	secret := "s3cret into"
	password := "id:" + strconv.Itoa(fixture.ClassicPasswordID)
	res, overlaySeen := runWatched(t, []string{"type-text", dialog, "--into", password, secret})
	assertActionOK(t, "type-text --into the password Edit", res.stdout, res.stderr, res.exit)
	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicPassword == secret })
	if st.ClassicText != "" {
		t.Errorf("text reached the focused name Edit: %q", st.ClassicText)
	}
	assertForeground(t, "type-text --into the password Edit", cover)
	if overlaySeen {
		t.Error("the in-control banner was shown during type-text --into")
	}
	if got := pointerPos(t); got != pointer {
		t.Errorf("the real pointer moved from %v to %v during type-text --into", pointer, got)
	}

	// A Batch step, by the name Edit's ID.
	name := "id:" + strconv.Itoa(fixture.ClassicEditID)
	stdout, stderr, exit := runWinuse(t, []string{"batch", dialog, "type-text", "--into", name, "hello", "into"}, "WINUSE_HELPER_IDLE="+e2eIdle)
	assertActionOK(t, "batch type-text --into the name Edit", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicText == "hello into" && s.ClassicPassword == secret })
	assertForeground(t, "batch type-text --into the name Edit", cover)

	// A button has no Headless route: refused, nothing sent, the
	// --headed retry in the hint.
	stdout, stderr, exit = runTypeText(t, []string{dialog, "--into", "id:1", "x"})
	if exit == 0 || !strings.Contains(stderr, "nothing was sent") || !strings.Contains(stderr, "winuse --headed type-text "+dialog+" --into id:1 <text>") {
		t.Errorf("type-text --into a button: exit %d, stdout %q, stderr %q", exit, stdout, stderr)
	}
	assertForeground(t, "type-text --into a button", cover)
}
