//go:build e2e

// Shell surfaces (desktop-parity 09): `list-windows --shell` and the
// taskbar's Window State against the user's own shell, which no test can
// replace with a fixture.
//
// TestShellReadsTheTaskbar only reads: it runs with the usual opt-in. The
// others act on the shell and can change what the user sees (a refusal
// that regressed would open Start; the overflow test opens the tray
// overflow and takes the foreground), so they skip unless
// WINUSE_E2E_SHELL=1 is set as well, by the user, at a moment they agree
// to it.

package e2e

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/win"
)

// optInShell gates the tests that act on the user's shell.
func optInShell(t *testing.T) {
	t.Helper()
	optIn(t)
	if os.Getenv("WINUSE_E2E_SHELL") != "1" {
		t.Skip("acts on the user's own taskbar and can open the tray overflow or Start: set WINUSE_E2E_SHELL=1 only when the user agreed to it")
	}
}

// shellLine finds the `list-windows --shell` line whose title is title.
func shellLine(t *testing.T, title string) (uint64, bool) {
	t.Helper()
	stdout, stderr, exit := runWinuse(t, []string{"list-windows", "--shell"}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("list-windows --shell: exit %d, stderr:\n%s", exit, stderr)
	}
	for _, l := range strings.Split(strings.TrimSuffix(stdout, "\n"), "\n") {
		f := strings.SplitN(l, "\t", 3)
		if len(f) == 3 && f[2] == title {
			id, err := strconv.ParseUint(f[0], 10, 64)
			if err == nil {
				return id, true
			}
		}
	}
	return 0, false
}

// taskbarID is the primary taskbar's Window ID from `list-windows --shell`.
func taskbarID(t *testing.T) uint64 {
	t.Helper()
	id, ok := shellLine(t, "Taskbar")
	if !ok {
		t.Fatal("list-windows --shell lists no Taskbar line")
	}
	return id
}

// treeLine matches one Element line of a Window State: its index and the
// rest of the line.
var treeLine = regexp.MustCompile(`^\t+(\d+) (.*)$`)

// elementWith returns the Element Index of the first line holding every
// one of parts.
func elementWith(state string, parts ...string) (string, bool) {
	for _, l := range strings.Split(state, "\n") {
		m := treeLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		all := true
		for _, p := range parts {
			all = all && strings.Contains(m[2], p)
		}
		if all {
			return m[1], true
		}
	}
	return "", false
}

// TestShellReadsTheTaskbar lists the taskbar behind --shell only, and
// reads its Window State Headless: Start and the notification area are
// there, and the foreground is unchanged.
func TestShellReadsTheTaskbar(t *testing.T) {
	optIn(t)
	fg := win.ForegroundWindow()
	id := taskbarID(t)

	plain, _, exit := runListWindows(t, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("list-windows: exit %d", exit)
	}
	if _, _, _, ok := windowLine(plain, id); ok {
		t.Errorf("plain list-windows lists the taskbar %d", id)
	}

	state, stderr, exit := runWinuse(t, []string{"get-window-state", itoa64(id)}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("get-window-state %d: exit %d, stderr:\n%s", id, exit, stderr)
	}
	for _, want := range []string{"ID: StartButton", "ID: SystemTrayIcon"} {
		if !strings.Contains(state, want) {
			t.Errorf("the taskbar's Window State has no %q line:\n%s", want, state)
		}
	}
	if got := win.ForegroundWindow(); got != fg {
		t.Errorf("the foreground moved from %d to %d while reading the taskbar", fg, got)
	}
}

// TestShellRefusesWithoutHeaded checks that nothing opens on the shell
// without --headed: Start's button and Control_L+Escape are refused with
// nothing sent, and the foreground stays where it was.
func TestShellRefusesWithoutHeaded(t *testing.T) {
	optInShell(t)
	fg := win.ForegroundWindow()
	id := itoa64(taskbarID(t))
	if _, _, exit := runWinuse(t, []string{"get-window-state", id}, "WINUSE_HELPER_IDLE="+e2eIdle); exit != 0 {
		t.Fatalf("get-window-state %s: exit %d", id, exit)
	}
	for _, args := range [][]string{
		{"click", id, "id:StartButton"},
		{"press-key", id, "Control_L+Escape"},
	} {
		_, stderr, exit := runWinuse(t, args, "WINUSE_HELPER_IDLE="+e2eIdle)
		if exit == 0 || !strings.Contains(stderr, "nothing was sent") || !strings.Contains(stderr, "--headed") {
			t.Errorf("%v: exit %d, stderr:\n%s", args, exit, stderr)
		}
	}
	time.Sleep(500 * time.Millisecond)
	if _, open := shellLine(t, "Start"); open {
		t.Error("Start opened")
	}
	if got := win.ForegroundWindow(); got != fg {
		t.Errorf("the foreground moved from %d to %d", fg, got)
	}
}

// TestShellOpensAndClosesTheTrayOverflow opens the tray overflow with a
// Headed click on the chevron (it takes the foreground), reads its icons,
// and closes it with the Headless Escape. The user's foreground Window is
// activated again at the end: the overflow does not give it back.
func TestShellOpensAndClosesTheTrayOverflow(t *testing.T) {
	optInShell(t)
	fg := win.ForegroundWindow()
	id := itoa64(taskbarID(t))
	state, _, exit := runWinuse(t, []string{"get-window-state", id}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("get-window-state %s: exit %d", id, exit)
	}
	chevron, ok := elementWith(state, "Show Hidden Icons", "ID: SystemTrayIcon")
	if !ok {
		t.Skip("this taskbar shows no hidden-icons chevron")
	}
	t.Cleanup(func() {
		if overflow, open := shellLine(t, "System tray overflow window."); open {
			runWinuse(t, []string{"press-key", itoa64(overflow), "Escape"}, "WINUSE_HELPER_IDLE="+e2eIdle)
		}
		if fg != 0 && win.ForegroundWindow() != fg {
			runWinuse(t, []string{"activate-window", itoa64(fg)}, "WINUSE_HELPER_IDLE="+e2eIdle)
		}
	})

	out, stderr, exit := runWinuse(t, []string{"--headed", "click", id, chevron}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("--headed click %s %s: exit %d, stderr:\n%s", id, chevron, exit, stderr)
	}
	var overflow uint64
	deadline := time.Now().Add(2 * time.Second)
	for overflow == 0 && time.Now().Before(deadline) {
		overflow, _ = shellLine(t, "System tray overflow window.")
		time.Sleep(100 * time.Millisecond)
	}
	if overflow == 0 {
		t.Fatalf("the tray overflow did not open; click output:\n%s", out)
	}
	if !strings.Contains(out, "a new Window opened: "+itoa64(overflow)) {
		t.Errorf("the click did not report the overflow opening:\n%s", out)
	}

	icons, stderr, exit := runWinuse(t, []string{"get-window-state", itoa64(overflow)}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("get-window-state %d: exit %d, stderr:\n%s", overflow, exit, stderr)
	}
	if _, ok := elementWith(icons, "button"); !ok {
		t.Errorf("the open overflow has no buttons:\n%s", icons)
	}

	out, stderr, exit = runWinuse(t, []string{"press-key", itoa64(overflow), "Escape"}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 || !strings.HasPrefix(out, "ok") {
		t.Fatalf("press-key %d Escape: exit %d, out %q, stderr:\n%s", overflow, exit, out, stderr)
	}
	if _, open := shellLine(t, "System tray overflow window."); open {
		t.Error("the overflow is still listed after Escape")
	}
}
