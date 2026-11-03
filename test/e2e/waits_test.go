//go:build e2e

// The desktop waits (desktop-parity 03) against the fixture: a window wait
// sees the Classic Dialog open and close, a text wait in the dialog fails
// when it closes, an element wait sees its Edit go, a value wait and a
// text wait see a set-value land, and a pause pauses. Each wait starts
// before the Action it waits for and runs beside it: a desktop wait skips
// the action gate. User-run only (see the package comment and
// test/e2e/README.md); an agent must never start them.

package e2e

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
)

// startWait starts `winuse wait ...` in the background and checks it is
// still waiting a moment later, before the Action it waits for.
func startWait(t *testing.T, args ...string) <-chan actionResult {
	t.Helper()
	bg := runActionAsync(t, append([]string{"wait"}, args...), "WINUSE_HELPER_IDLE="+e2eIdle)
	select {
	case res := <-bg:
		t.Fatalf("wait %v ended before the Action it waits for: exit %d, stdout %q, stderr %q", args, res.exit, res.stdout, res.stderr)
	case <-time.After(700 * time.Millisecond):
	}
	return bg
}

// waitResult is a background wait's result, within 20 s.
func waitResult(t *testing.T, what string, bg <-chan actionResult) actionResult {
	t.Helper()
	select {
	case res := <-bg:
		return res
	case <-time.After(20 * time.Second):
		t.Fatalf("%s did not end within 20 s", what)
	}
	return actionResult{}
}

func TestWaitForADialogAndItsClose(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)

	opened := startWait(t, "window", fixture.ClassicDialogTitle, "--timeout", "15")
	idx := mainElementIndexes()[fixture.ElemButtonClassic]
	stdout, stderr, exit := runClick(t, []string{window, strconv.Itoa(idx)})
	assertActionOK(t, "Headless click (Classic Dialog...)", stdout, stderr, exit)
	st := fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.ClassicOpen && s.ClassicWindowID != 0 })
	dialog := strconv.FormatUint(st.ClassicWindowID, 10)

	res := waitResult(t, "wait window", opened)
	if res.exit != 0 || res.stderr != "" {
		t.Fatalf("wait window: exit %d, stderr %q", res.exit, res.stderr)
	}
	if id, _, title, ok := windowLine(res.stdout, st.ClassicWindowID); !ok || id != dialog || title != fixture.ClassicDialogTitle {
		t.Errorf("wait window printed %q, want the Classic Dialog's line (Window %s)", res.stdout, dialog)
	}

	// The Edit's element wait resolves against the dialog's Window State.
	if _, errOut, code := runGetWindowState(t, dialog); code != 0 {
		t.Fatalf("get-window-state %s: exit %d, stderr %q", dialog, code, errOut)
	}
	gone := startWait(t, "window", fixture.ClassicDialogTitle, "--gone", "--timeout", "15")
	editGone := startWait(t, dialog, "id:"+strconv.Itoa(fixture.ClassicEditID), "gone", "--timeout", "15")
	text := startWait(t, dialog, "text", "never shown in the dialog", "--timeout", "15")

	r, _ := runWatched(t, []string{"press-key", dialog, "Escape"})
	assertActionOK(t, "press-key Escape on the Classic Dialog", r.stdout, r.stderr, r.exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return !s.ClassicOpen })

	if res := waitResult(t, "wait window --gone", gone); res.exit != 0 || res.stdout != "ok\n" {
		t.Errorf("wait window --gone: exit %d, stdout %q, stderr %q", res.exit, res.stdout, res.stderr)
	}
	if res := waitResult(t, "wait gone", editGone); res.exit != 0 || !strings.HasPrefix(res.stdout, "ok") {
		t.Errorf("wait id:%d gone: exit %d, stdout %q, stderr %q", fixture.ClassicEditID, res.exit, res.stdout, res.stderr)
	}
	if res := waitResult(t, "wait text", text); res.exit != 1 || !strings.Contains(res.stderr, "the Window closed") {
		t.Errorf("wait text in a Window that closes: exit %d, stderr %q, want the Window closed", res.exit, res.stderr)
	}
}

func TestWaitForAValueChange(t *testing.T) {
	optIn(t)
	fix, window := startFixtureWithState(t)
	field := "id:" + string(fixture.ElemTextField)

	value := startWait(t, window, field, "value", "waited for", "--timeout", "15")
	text := startWait(t, window, "text", "waited for", "--timeout", "15")
	stdout, stderr, exit := runWinuse(t, []string{"set-value", window, field, "waited for"}, "WINUSE_HELPER_IDLE="+e2eIdle)
	assertActionOK(t, "set-value on the text field", stdout, stderr, exit)
	fix.waitState(t, 5*time.Second, func(s *fixture.State) bool { return s.Text == "waited for" })

	if res := waitResult(t, "wait value", value); res.exit != 0 || res.stdout != "ok\n" {
		t.Errorf("wait %s value: exit %d, stdout %q, stderr %q", field, res.exit, res.stdout, res.stderr)
	}
	if res := waitResult(t, "wait text", text); res.exit != 0 || res.stdout != "ok\n" {
		t.Errorf("wait text: exit %d, stdout %q, stderr %q", res.exit, res.stdout, res.stderr)
	}

	// A value that never comes says what the Element reads.
	out, errOut, code := runWinuse(t, []string{"wait", window, field, "value", "something else", "--timeout", "1"}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if code != 1 || !strings.Contains(errOut, `it reads "waited for"`) {
		t.Errorf("a value that never comes: exit %d, stdout %q, stderr %q", code, out, errOut)
	}

	start := time.Now()
	if out, errOut, code := runWinuse(t, []string{"wait", "400"}, "WINUSE_HELPER_IDLE="+e2eIdle); code != 0 || out != "ok\n" || time.Since(start) < 400*time.Millisecond {
		t.Errorf("wait 400: exit %d, stdout %q, stderr %q after %s", code, out, errOut, time.Since(start))
	}
}
