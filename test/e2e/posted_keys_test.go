//go:build e2e

// Posted Keys (ticket 07 of headless-parity): `press-key <window> <key>
// --post` and `type-text <window> --post <text>` post WM_KEYDOWN/WM_KEYUP
// or WM_CHAR to the Window's focused control, with no foreground change.
// User-run only (see the package comment and test/e2e/README.md); an
// agent must never start them.
//
// Against the fixture's Classic Dialog (a real Win32 Edit) the posted
// keys are asserted. Against Chrome's address bar they are a measurement:
// whether Chrome acts on a posted Return is the open question behind the
// option, so that test logs what happened and never fails on it.

package e2e

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
	"github.com/hoangvu12/winuse/internal/win"
)

func TestPostedKeysIntoClassicEdit(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	cover := coverWindow(t, window)
	dialog := openClassicDialog(t, fix, window, cover)

	// type-text --post: one WM_CHAR per UTF-16 unit.
	res, overlaySeen := runWatched(t, []string{"type-text", dialog, "--post", "posted text"})
	if res.exit != 0 || !strings.HasPrefix(res.stdout, postedOK) || res.stderr != "" {
		t.Fatalf("type-text --post: exit %d, stdout %q, stderr %q", res.exit, res.stdout, res.stderr)
	}
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicText == "posted text" })
	assertForeground(t, "type-text --post", cover)
	if overlaySeen {
		t.Error("the in-control banner was shown during posted keys")
	}

	// press-key --post: WM_KEYDOWN/WM_KEYUP, which the app's message loop
	// translates (BackSpace becomes a WM_CHAR the Edit deletes with).
	stdout, stderr, exit := runPressKey(t, []string{dialog, "BackSpace", "--post"})
	if exit != 0 || !strings.HasPrefix(stdout, postedOK) {
		t.Fatalf("press-key BackSpace --post: exit %d, stdout %q, stderr %q", exit, stdout, stderr)
	}
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicText == "posted tex" })
	assertForeground(t, "press-key --post", cover)

	// A chord with a modifier cannot be posted: refused, nothing sent.
	stdout, stderr, exit = runPressKey(t, []string{dialog, "Control_L+a", "--post"})
	if exit == 0 || stdout != "" || !strings.Contains(stderr, "nothing was sent") {
		t.Errorf("press-key Control_L+a --post: exit %d, stdout %q, stderr %q, want a refusal saying nothing was sent", exit, stdout, stderr)
	}
	time.Sleep(300 * time.Millisecond)
	if st := fix.waitState(t, time.Second, func(*fixture.State) bool { return true }); st.ClassicText != "posted tex" {
		t.Errorf("the refused chord changed the Edit to %q", st.ClassicText)
	}
}

func TestPostedKeysRefusedOnBlockedAndMinimizedWindows(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	indexes := mainElementIndexes()

	// The Save As dialog is modal: it disables the main window, and a
	// posted key would land in the disabled owner behind it.
	stdout, stderr, exit := runClick(t, []string{window, strconv.Itoa(indexes[fixture.ElemButtonSaveAs])})
	assertActionOK(t, "Headless click (Save As...)", stdout, stderr, exit)
	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.DialogOpen })
	dialog := strconv.FormatUint(st.DialogWindowID, 10)

	refused := func(what string, args []string, want string) {
		t.Helper()
		before := fix.waitState(t, time.Second, func(*fixture.State) bool { return true })
		stdout, stderr, exit := runWinuse(t, args, "WINUSE_HELPER_IDLE="+e2eIdle)
		if exit == 0 || stdout != "" {
			t.Errorf("%s: exit %d, stdout %q, want a refusal", what, exit, stdout)
			return
		}
		if !strings.Contains(stderr, "nothing was sent") || !strings.Contains(stderr, want) {
			t.Errorf("%s: stderr = %q, want %q and nothing was sent", what, stderr, want)
		}
		time.Sleep(300 * time.Millisecond)
		after := fix.waitState(t, time.Second, func(*fixture.State) bool { return true })
		if after.LastKey != before.LastKey || after.Typed != before.Typed || after.Generation != before.Generation {
			t.Errorf("%s: input reached the fixture (lastKey %q -> %q, generation %d -> %d)",
				what, before.LastKey, after.LastKey, before.Generation, after.Generation)
		}
	}
	refused("press-key --post behind a modal dialog", []string{"press-key", window, "a", "--post"}, "has a modal dialog open (window "+dialog+")")
	refused("type-text --post behind a modal dialog", []string{"type-text", window, "--post", "hi"}, "has a modal dialog open (window "+dialog+")")

	// Close the dialog with its Cancel button.
	if _, stderr, exit := runGetWindowState(t, dialog); exit != 0 {
		t.Fatalf("get-window-state of the dialog: exit %d, stderr:\n%s", exit, stderr)
	}
	stdout, stderr, exit = runClick(t, []string{dialog, strconv.Itoa(dialogElementIndexes()[fixture.ElemDialogButtonCancel])})
	assertActionOK(t, "Headless click (Cancel)", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return !s.DialogOpen })

	// A minimized Window has nothing to post to either.
	stdout, stderr, exit = runWinuse(t, []string{"minimize-window", window}, "WINUSE_HELPER_IDLE="+e2eIdle)
	assertActionOK(t, "minimize-window", stdout, stderr, exit)
	refused("press-key --post into a minimized Window", []string{"press-key", window, "a", "--post"}, "is minimized")
	refused("type-text --post into a minimized Window", []string{"type-text", window, "--post", "hi"}, "is minimized")
}

// TestPostedReturnIntoCoveredChromeAddressBar measures whether Chrome acts
// on a posted Return in its address bar while another Window has the
// foreground: the address bar is set to a data: URL with set-value, then
// Return is posted, and the test watches for the navigation (the page's
// title in the Window title). It logs the outcome on a line starting
// "MEASURED:" and fails only when winuse breaks its own contract (the
// output, or a foreground change).
func TestPostedReturnIntoCoveredChromeAddressBar(t *testing.T) {
	optIn(t)
	var chrome *chromiumBrowser
	for _, b := range findChromiumBrowsers() {
		if b.name == "chrome" {
			b := b
			chrome = &b
		}
	}
	if chrome == nil {
		t.Skip("Chrome is not installed in a standard location")
	}

	// An isolated profile (see chromium_test.go): a fresh browser process
	// whatever the user's own Chrome is doing. about:blank puts the focus
	// in the address bar, as a new tab does.
	profile := t.TempDir()
	cmd := exec.Command(chrome.exe, "--user-data-dir="+profile, "--no-first-run", "--no-default-browser-check", "--new-window", "about:blank")
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting %s: %v", chrome.exe, err)
	}
	pid := uint32(cmd.Process.Pid)
	defer terminatePID(t, pid)

	id := findWindowOf(t, pid, "Chrome_WidgetWin_1", "", 30*time.Second)
	window := strconv.FormatUint(id, 10)
	cover := coverWindow(t, window)

	state, stderr, exit := runGetWindowState(t, window)
	if exit != 0 {
		t.Fatalf("get-window-state of Chrome: exit %d, stderr:\n%s", exit, stderr)
	}
	m := regexp.MustCompile(`(?m)^\t+(\d+) edit( \([^)]*\))? [^\n]*[Aa]ddress`).FindStringSubmatch(state)
	if m == nil {
		t.Skipf("no address bar Element (an edit named with \"address\") in Chrome's Window State; nothing measured:\n%s", state)
	}
	omnibox, _ := strconv.Atoi(m[1])

	const title = "winuse posted return"
	stdout, stderr, exit := runSetvalue(t, window, omnibox, "data:text/html,<title>"+title+"</title>posted")
	if exit != 0 {
		t.Skipf("set-value on Chrome's address bar failed (exit %d, stderr %q); nothing measured", exit, stderr)
	}
	_ = stdout
	assertForeground(t, "set-value on Chrome's address bar", cover)
	logKeyTarget(t, id)

	stdout, stderr, exit = runPressKey(t, []string{window, "Return", "--post"})
	if exit != 0 || !strings.HasPrefix(stdout, postedOK) {
		t.Fatalf("press-key Return --post: exit %d, stdout %q, stderr %q", exit, stdout, stderr)
	}
	assertForeground(t, "press-key Return --post into Chrome", cover)

	navigated := false
	deadline := time.Now().Add(5 * time.Second)
	for !navigated && time.Now().Before(deadline) {
		all, err := win.Collect()
		if err != nil {
			t.Fatalf("enumerating windows: %v", err)
		}
		for _, w := range all {
			if w.PID == pid && strings.Contains(w.Title, title) {
				navigated = true
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if navigated {
		t.Logf("MEASURED: Chrome acted on the posted Return in its covered address bar (it navigated to the data: page)")
	} else {
		t.Logf("MEASURED: Chrome ignored the posted Return in its covered address bar (no navigation within 5 s; Window %s)",
			describeWindow(t, id))
	}
}
