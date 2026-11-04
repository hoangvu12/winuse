//go:build e2e

// Notifications and open-shell (desktop-parity 18, with 09's open-shell)
// against the user's own shell, which no test can replace with a fixture.
//
// The toast tests raise their own toasts from Windows PowerShell's App
// User Model ID, in their own group (fixture.Toaster: the group is removed
// from the Notification Center after, and so are the settings and backup
// keys Windows adds when they were not there), and hear their buttons in this process, so
// they never act on a notification of the user's. They show
// toasts on the user's screen and need the user's notifications turned on
// (Settings > System > Notifications), so they skip unless
// WINUSE_E2E_TOASTS=1 is set as well, and skip when the notifications are
// off.
//
// The open-shell tests open the notification panel, which changes the
// user's screen and takes the foreground: they skip unless
// WINUSE_E2E_SHELL=1 is set as well (shell_test.go's gate).

package e2e

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/fixture"
	"github.com/hoangvu12/winuse/internal/win"
)

// optInToasts gates the tests that raise toasts.
func optInToasts(t *testing.T) *fixture.Toaster {
	t.Helper()
	optIn(t)
	if os.Getenv("WINUSE_E2E_TOASTS") != "1" {
		t.Skip("raises toasts on the user's screen: set WINUSE_E2E_TOASTS=1 only when the user agreed to it, with their notifications on")
	}
	toaster, err := fixture.NewToaster()
	if err != nil {
		t.Fatalf("NewToaster: %v", err)
	}
	t.Cleanup(func() {
		if err := toaster.Close(); err != nil {
			t.Errorf("cleaning the e2e toasts up: %v", err)
		}
	})
	setting, err := toaster.Setting()
	if err != nil {
		t.Fatalf("reading the toast setting: %v", err)
	}
	if setting != fixture.ToastEnabled {
		t.Skipf("toasts are off for this user (ToastNotifier.Setting %d); turn notifications on in Settings > System > Notifications", setting)
	}
	return toaster
}

// giveForegroundBack activates fg again at the end of the test when it
// moved (a toast press can hand the foreground to the shell).
func giveForegroundBack(t *testing.T, fg uint64) {
	t.Cleanup(func() { restoreForeground(t, fg) })
}

// showAndWait raises a toast and waits for winuse to list it: the wait's
// first line, which numbers it 1.
func showAndWait(t *testing.T, toaster *fixture.Toaster, tag, title string, buttons ...string) (*fixture.Toast, string) {
	t.Helper()
	toast, err := toaster.Show(tag, title, "Pick one.", buttons...)
	if err != nil {
		t.Fatalf("raising the toast: %v", err)
	}
	out, stderr, exit := runWinuse(t, []string{"wait", "notification", "--text", title, "--timeout", "10"}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("wait notification --text %q: exit %d, stderr:\n%s", title, exit, stderr)
	}
	line := strings.SplitN(out, "\n", 2)[0]
	if !strings.HasPrefix(line, "1\t") || !strings.Contains(line, "\t"+title+"\tPick one.") {
		t.Fatalf("wait notification printed:\n%s", out)
	}
	return toast, line
}

// TestNotificationsPressAndDismissARaisedToast: a raised toast is listed
// with its App, title, text and buttons; notification-action reaches the
// raising process with the button's arguments; notification-dismiss
// closes the next one.
func TestNotificationsPressAndDismissARaisedToast(t *testing.T) {
	toaster := optInToasts(t)
	fg := win.ForegroundWindow()
	giveForegroundBack(t, fg)
	run := strconv.FormatInt(time.Now().UnixNano(), 36)

	toast, line := showAndWait(t, toaster, "action", "winuse e2e "+run, "Alpha", "Beta")
	if !strings.HasSuffix(line, "\tbuttons: Alpha, Beta") {
		t.Errorf("the listed line has no buttons: %q", line)
	}
	t.Logf("listed: %q", line)
	out, stderr, exit := runWinuse(t, []string{"notification-action", "1", "Beta"}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 || !strings.HasPrefix(out, "ok") {
		t.Fatalf("notification-action 1 Beta: exit %d, out %q, stderr:\n%s", exit, out, stderr)
	}
	t.Logf("notification-action: %q (foreground before %d, after %d)", out, fg, win.ForegroundWindow())
	select {
	case args := <-toast.Activated:
		if args != "beta" {
			t.Errorf("the toast was activated with %q, want beta", args)
		}
	case <-time.After(5 * time.Second):
		t.Error("the raising process heard no activation within 5 s")
	}

	toast, _ = showAndWait(t, toaster, "dismiss", "winuse e2e dismiss "+run, "Alpha")
	out, stderr, exit = runWinuse(t, []string{"notification-dismiss", "1"}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 || !strings.HasPrefix(out, "ok") {
		t.Fatalf("notification-dismiss 1: exit %d, out %q, stderr:\n%s", exit, out, stderr)
	}
	select {
	case reason := <-toast.Dismissed:
		if reason != fixture.ToastUserCanceled {
			t.Errorf("dismissed with reason %d, want UserCanceled (%d)", reason, fixture.ToastUserCanceled)
		}
	case <-time.After(5 * time.Second):
		t.Error("the raising process heard no dismissal within 5 s")
	}
}

// TestNotificationIdentityGuardRefusesAGoneToast: a notification hidden
// after it was listed is refused with nothing sent, and its App hears
// nothing.
func TestNotificationIdentityGuardRefusesAGoneToast(t *testing.T) {
	toaster := optInToasts(t)
	giveForegroundBack(t, win.ForegroundWindow())
	toast, _ := showAndWait(t, toaster, "guard", fmt.Sprintf("winuse e2e guard %d", time.Now().UnixNano()), "Alpha")
	if err := toaster.Hide(toast); err != nil {
		t.Fatalf("hiding the toast: %v", err)
	}
	select {
	case <-toast.Dismissed:
	case <-time.After(3 * time.Second):
		t.Fatal("the hidden toast reported no dismissal")
	}
	_, stderr, exit := runWinuse(t, []string{"notification-action", "1", "Alpha"}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit == 0 || !strings.Contains(stderr, "nothing was sent") {
		t.Errorf("notification-action on a hidden toast: exit %d, stderr:\n%s", exit, stderr)
	}
	select {
	case args := <-toast.Activated:
		t.Errorf("the refused action activated the toast (%q)", args)
	case <-time.After(time.Second):
	}
}

// shellSurfaceIDs is the set of Window IDs `list-windows --shell` lists.
func shellSurfaceIDs(t *testing.T) map[string]bool {
	t.Helper()
	out, stderr, exit := runWinuse(t, []string{"list-windows", "--shell"}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("list-windows --shell: exit %d, stderr:\n%s", exit, stderr)
	}
	ids := map[string]bool{}
	for _, l := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		ids[strings.SplitN(l, "\t", 2)[0]] = true
	}
	return ids
}

// TestOpenShellRefusesWithoutHeaded: without --headed nothing is sent and
// nothing opens.
func TestOpenShellRefusesWithoutHeaded(t *testing.T) {
	optInShell(t)
	fg := win.ForegroundWindow()
	before := shellSurfaceIDs(t)
	for _, surface := range []string{"notifications", "quick-settings", "start", "overflow"} {
		_, stderr, exit := runWinuse(t, []string{"open-shell", surface}, "WINUSE_HELPER_IDLE="+e2eIdle)
		if exit == 0 || !strings.Contains(stderr, "nothing was sent") || !strings.Contains(stderr, "winuse --headed open-shell "+surface) {
			t.Errorf("open-shell %s: exit %d, stderr:\n%s", surface, exit, stderr)
		}
	}
	time.Sleep(500 * time.Millisecond)
	for id := range shellSurfaceIDs(t) {
		if !before[id] {
			t.Errorf("window %s showed after a refused open-shell", id)
		}
	}
	if got := win.ForegroundWindow(); got != fg {
		t.Errorf("the foreground moved from %d to %d", fg, got)
	}
}

// TestOpenShellOpensTheNotificationPanel opens the notification panel
// with --headed (Win+N), reads it, and closes it with a --headed Escape;
// the Window that was in the foreground is activated again.
func TestOpenShellOpensTheNotificationPanel(t *testing.T) {
	optInShell(t)
	giveForegroundBack(t, win.ForegroundWindow())
	out, stderr, exit := runWinuse(t, []string{"--headed", "open-shell", "notifications"}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("--headed open-shell notifications: exit %d, stderr:\n%s", exit, stderr)
	}
	id := strings.SplitN(out, "\t", 2)[0]
	t.Logf("open-shell: %q", out)
	t.Cleanup(func() {
		if shellSurfaceIDs(t)[id] {
			runWinuse(t, []string{"--headed", "press-key", id, "Escape"}, "WINUSE_HELPER_IDLE="+e2eIdle)
		}
	})
	if strings.Contains(out, "was open already") {
		t.Skip("the panel was open before the test")
	}
	state, stderr, exit := runWinuse(t, []string{"get-window-state", id}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("get-window-state %s: exit %d, stderr:\n%s", id, exit, stderr)
	}
	if strings.Count(state, "\n") < 3 {
		t.Errorf("the open panel's Window State is nearly empty:\n%s", state)
	}
	if _, stderr, exit := runWinuse(t, []string{"--headed", "press-key", id, "Escape"}, "WINUSE_HELPER_IDLE="+e2eIdle); exit != 0 {
		t.Fatalf("--headed press-key %s Escape: exit %d, stderr:\n%s", id, exit, stderr)
	}
	deadline := time.Now().Add(2 * time.Second)
	for shellSurfaceIDs(t)[id] && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if shellSurfaceIDs(t)[id] {
		t.Error("the panel is still listed after Escape")
	}
}

// TestListNotificationsOnlyReads runs with the usual opt-in: it reads the
// toast banners (whatever shows, the user's included, is only printed),
// and a notification wait for a text no toast has times out; the
// foreground stays where it was.
func TestListNotificationsOnlyReads(t *testing.T) {
	optIn(t)
	fg := win.ForegroundWindow()
	out, stderr, exit := runWinuse(t, []string{"list-notifications"}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("list-notifications: exit %d, stderr:\n%s", exit, stderr)
	}
	if out == "" {
		t.Error("list-notifications printed nothing")
	}
	text := fmt.Sprintf("no toast says %d", time.Now().UnixNano())
	_, stderr, exit = runWinuse(t, []string{"wait", "notification", "--text", text, "--timeout", "1"}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit == 0 || !strings.Contains(stderr, "waiting for a notification with") {
		t.Errorf("wait notification for %q: exit %d, stderr:\n%s", text, exit, stderr)
	}
	if got := win.ForegroundWindow(); got != fg {
		t.Errorf("the foreground moved from %d to %d", fg, got)
	}
}
