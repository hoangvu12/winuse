package helper

// Offline tests for the notifications (desktop-parity 18,
// notifications.go) and open-shell (openshell.go): the toast trees are
// the recorded banner (testdata/toast-banner.json) and variations of it,
// over the shell desktop of shell_test.go with a banner added. The
// desktop, the trees, the Invoke, the keys and the foreground are all
// faked, so nothing is read from or sent to the real desktop.

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/agent"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

const (
	bannerID      = 970
	panelID       = 975
	quickID       = 976
	shellHostExe  = `C:\Windows\SystemApps\ShellExperienceHost_cw5n1h2txyewy\ShellExperienceHost.exe`
	shellHost2Exe = `C:\Windows\SystemApps\MicrosoftWindows.Client.CBS_cw5n1h2txyewy\ShellHost.exe`
)

// recordedBanner loads the recorded toast banner: one notification from
// Windows PowerShell, "winuse probe", with Alpha and Beta.
func recordedBanner(t *testing.T) *wstate.Tree {
	t.Helper()
	tree, err := wstate.LoadFixture("testdata/toast-banner.json")
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// bannerWindow is the showing toast banner's enumeration entry; hidden
// cloaks it, as the banner is between toasts.
func bannerWindow(hidden bool) win.WindowInfo {
	return win.WindowInfo{ID: bannerID, Class: "Windows.UI.Core.CoreWindow", Title: "New notification",
		App: "Windows Shell Experience Host", ExePath: shellHostExe, PID: 6100, Visible: true, Cloaked: hidden,
		Bounds: win.Rect{Left: 1966, Top: 1068, Right: 2560, Bottom: 1368}, Shell: win.ShellNotification}
}

// toastTrees serves the banner's current tree, by Capture or PeekTree,
// and counts both.
type toastTrees struct {
	mu       sync.Mutex
	tree     func() *wstate.Tree
	captures int
	peeks    int
	taskbar  *wstate.Tree
}

func (f *toastTrees) Capture(ctx context.Context, hwnd uint64, title, app string, budget time.Duration) (*wstate.Tree, error) {
	f.mu.Lock()
	f.captures++
	f.mu.Unlock()
	if hwnd == shellTaskbarID && f.taskbar != nil {
		return f.taskbar, nil
	}
	return f.tree(), nil
}

func (f *toastTrees) PeekTree(ctx context.Context, hwnd uint64, title, app string, budget time.Duration) (*wstate.Tree, error) {
	f.mu.Lock()
	f.peeks++
	f.mu.Unlock()
	return f.tree(), nil
}

// toastDesk is the shell desktop with a toast banner: it shows until a
// Headless click lands (the banner cloaks, as in the probe), unless
// stays is set.
type toastDesk struct {
	*shellSpy
	trees *toastTrees
	stays bool
	shown bool // the banner shows (before any click)
}

func newToastDesk(t *testing.T) *toastDesk {
	d := &toastDesk{shellSpy: newShellSpy(), shown: true}
	recorded := recordedBanner(t)
	d.trees = &toastTrees{tree: func() *wstate.Tree { return recorded }}
	d.svc.Trees = d.trees
	d.surfaces = func() []win.WindowInfo {
		clicked := hasStep(d.recorded(), "headless-click")
		return append(shellDesktop(false, false), bannerWindow(!d.shown || (clicked && !d.stays)))
	}
	return d
}

func (d *toastDesk) cmds() map[string]Command { return CommandsWith(d.svc) }

func TestToastsInTheRecordedBanner(t *testing.T) {
	got := toastsIn(recordedBanner(t), bannerID)
	if len(got) != 1 {
		t.Fatalf("got %d notifications, want 1: %+v", len(got), got)
	}
	n := got[0]
	if n.App != "Windows PowerShell" || n.Title != "winuse probe" || n.Text != "Pick one." || n.Window != bannerID {
		t.Errorf("notification = %+v", n)
	}
	if strings.Join(n.Buttons, ",") != "Alpha,Beta" || n.buttons[0] != 8 || n.buttons[1] != 9 || n.dismiss != 5 {
		t.Errorf("buttons %v at %v, dismiss at %d; want Alpha 8, Beta 9, dismiss 5", n.Buttons, n.buttons, n.dismiss)
	}
}

// Two stacked toasts are two toast views: each keeps its own subtree.
func TestToastsInStackedBanners(t *testing.T) {
	tree := recordedBanner(t)
	second := []wstate.Element{
		{Depth: 2, Role: "button", Name: "New notification from Signal", AutomationId: "NormalToastView"},
		{Depth: 3, Role: "text", Name: "Signal", AutomationId: "SenderName"},
		{Depth: 3, Role: "text", Name: "Mum", AutomationId: "Title"},
		{Depth: 3, Role: "text", Name: "Call me", AutomationId: "MessageText"},
		{Depth: 3, Role: "text", Name: "when   you can", AutomationId: "MessageText"},
	}
	tree = &wstate.Tree{Title: tree.Title, Elements: append(append([]wstate.Element{}, tree.Elements...), second...), FocusedIndex: -1}
	got := toastsIn(tree, bannerID)
	if len(got) != 2 {
		t.Fatalf("got %d notifications, want 2", len(got))
	}
	if got[0].App != "Windows PowerShell" || len(got[0].Buttons) != 2 {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].App != "Signal" || got[1].Title != "Mum" || got[1].Text != "Call me when you can" || len(got[1].Buttons) != 0 || got[1].dismiss != -1 {
		t.Errorf("second = %+v", got[1])
	}
}

func TestListNotifications(t *testing.T) {
	d := newToastDesk(t)
	cmds := d.cmds()
	out, cmdErr := runCommand(t, cmds, "list-notifications", protocol.ListNotificationsArgs{})
	if cmdErr != nil {
		t.Fatalf("list-notifications: %v", cmdErr.Error())
	}
	if want := "1\tWindows PowerShell\twinuse probe\tPick one.\tbuttons: Alpha, Beta\n"; out != want {
		t.Errorf("output:\n got %q\nwant %q", out, want)
	}
	if d.trees.captures != 0 || d.trees.peeks != 1 {
		t.Errorf("captures %d, peeks %d: a listing peeks, leaving the banner's Window State alone", d.trees.captures, d.trees.peeks)
	}

	for _, c := range []struct {
		args protocol.ListNotificationsArgs
		want string
	}{
		{protocol.ListNotificationsArgs{App: "powershell"}, "1\tWindows PowerShell"},
		{protocol.ListNotificationsArgs{Text: "PICK"}, "1\tWindows PowerShell"},
		{protocol.ListNotificationsArgs{Text: "probe"}, "1\tWindows PowerShell"},
		{protocol.ListNotificationsArgs{App: "Signal"}, "no showing notification matches; 1 other(s) show"},
	} {
		out, cmdErr := runCommand(t, cmds, "list-notifications", c.args)
		if cmdErr != nil || !strings.HasPrefix(out, c.want) {
			t.Errorf("%+v: %q, %v; want %q", c.args, out, cmdErr, c.want)
		}
	}

	// No banner showing: the banner window is cloaked between toasts.
	d.shown = false
	out, cmdErr = runCommand(t, cmds, "list-notifications", nil)
	if cmdErr != nil || out != noNotificationsLine {
		t.Errorf("no banner: %q, %v", out, cmdErr)
	}
	if !strings.Contains(out, "open-shell notifications") {
		t.Errorf("the empty listing does not point at the Notification Center: %q", out)
	}
}

func TestNotificationActionInvokesTheButton(t *testing.T) {
	d := newToastDesk(t)
	cmds := d.cmds()
	if _, cmdErr := runCommand(t, cmds, "list-notifications", nil); cmdErr != nil {
		t.Fatal(cmdErr.Error())
	}
	out, cmdErr := runCommand(t, cmds, "notification-action", protocol.NotificationActionArgs{ID: 1, Button: "beta"})
	if cmdErr != nil {
		t.Fatalf("notification-action 1 beta: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want ok with no note (the foreground stayed)", out)
	}
	if d.hIndex != 9 {
		t.Errorf("clicked element %d, want Beta (9)", d.hIndex)
	}
	if hasStep(d.recorded(), "activate") || hasStep(d.recorded(), "click") {
		t.Errorf("steps = %v: a notification's button is pressed Headless", d.recorded())
	}
	if d.trees.captures != 1 {
		t.Errorf("captures = %d: the action reads the banner again before pressing", d.trees.captures)
	}
}

// An App that is not listening hands the foreground to the shell: the ok
// says where it went (headless-max 03's detect-and-report).
func TestNotificationActionReportsTheForeground(t *testing.T) {
	d := newToastDesk(t)
	d.svc.Foreground = func() uint64 {
		if hasStep(d.recorded(), "headless-click") {
			return 202 // the shell or the App took it
		}
		return 101
	}
	cmds := d.cmds()
	runCommand(t, cmds, "list-notifications", nil)
	out, cmdErr := runCommand(t, cmds, "notification-action", protocol.NotificationActionArgs{ID: 1, Button: "Alpha"})
	if cmdErr != nil {
		t.Fatalf("notification-action: %v", cmdErr.Error())
	}
	if !strings.HasPrefix(out, "ok\n") || !strings.Contains(out, `Note: the foreground moved to 202 Chrome "One"`) || !strings.Contains(out, "the shell when the App was not running") {
		t.Errorf("output:\n%s", out)
	}
}

// The identity guard: an action acts only on the notification its number
// was listed with, read again first; anything else is refused with
// nothing sent.
func TestNotificationIdentityGuard(t *testing.T) {
	t.Run("no listing", func(t *testing.T) {
		d := newToastDesk(t)
		_, cmdErr := runCommand(t, d.cmds(), "notification-dismiss", protocol.NotificationDismissArgs{ID: 1})
		wantRefusal(t, d, cmdErr, "not in your latest list-notifications")
	})
	t.Run("a number past the listing", func(t *testing.T) {
		d := newToastDesk(t)
		cmds := d.cmds()
		runCommand(t, cmds, "list-notifications", nil)
		_, cmdErr := runCommand(t, cmds, "notification-action", protocol.NotificationActionArgs{ID: 2, Button: "Alpha"})
		wantRefusal(t, d, cmdErr, "notification 2 is not in your latest list-notifications")
	})
	t.Run("the text changed", func(t *testing.T) {
		d := newToastDesk(t)
		cmds := d.cmds()
		runCommand(t, cmds, "list-notifications", nil)
		changed := recordedBanner(t)
		changed.Elements[7].Name = "Pick the other one."
		d.trees.tree = func() *wstate.Tree { return changed }
		_, cmdErr := runCommand(t, cmds, "notification-action", protocol.NotificationActionArgs{ID: 1, Button: "Alpha"})
		wantRefusal(t, d, cmdErr, "notification 1 (from Windows PowerShell) no longer shows as it was listed")
	})
	t.Run("the banner timed out", func(t *testing.T) {
		d := newToastDesk(t)
		cmds := d.cmds()
		runCommand(t, cmds, "list-notifications", nil)
		d.shown = false
		_, cmdErr := runCommand(t, cmds, "notification-dismiss", protocol.NotificationDismissArgs{ID: 1})
		wantRefusal(t, d, cmdErr, "no longer shows")
	})
	t.Run("no such button", func(t *testing.T) {
		d := newToastDesk(t)
		cmds := d.cmds()
		runCommand(t, cmds, "list-notifications", nil)
		_, cmdErr := runCommand(t, cmds, "notification-action", protocol.NotificationActionArgs{ID: 1, Button: "Gamma"})
		wantRefusal(t, d, cmdErr, `has no button "Gamma"; its buttons: Alpha, Beta`)
	})
	t.Run("another Agent's listing", func(t *testing.T) {
		d := newToastDesk(t)
		cmds := d.cmds()
		runCommand(t, cmds, "list-notifications", nil) // the default Agent's
		raw, _ := json.Marshal(protocol.NotificationActionArgs{ID: 1, Button: "Alpha"})
		ctx := agent.NewContext(context.Background(), agent.Identity{ID: "pid:77", Label: "other"})
		_, cmdErr := cmds["notification-action"](ctx, raw)
		wantRefusal(t, d, cmdErr, "not in your latest list-notifications")
	})
	t.Run("a locked desktop", func(t *testing.T) {
		d := newToastDesk(t)
		cmds := d.cmds()
		runCommand(t, cmds, "list-notifications", nil)
		d.locked, d.lockedDesktop = true, "Winlogon"
		_, cmdErr := runCommand(t, cmds, "notification-action", protocol.NotificationActionArgs{ID: 1, Button: "Alpha"})
		if cmdErr == nil || hasStep(d.recorded(), "headless-click") {
			t.Errorf("a locked desktop: %v, steps %v", cmdErr, d.recorded())
		}
	})
}

// wantRefusal checks a refusal with nothing sent: no click was made.
func wantRefusal(t *testing.T, d *toastDesk, cmdErr *Error, want string) {
	t.Helper()
	if cmdErr == nil {
		t.Fatalf("accepted; want a refusal with %q", want)
	}
	if !strings.Contains(cmdErr.Msg, want) || !strings.HasSuffix(cmdErr.Msg, nothingSentNote) {
		t.Errorf("refusal = %q (hint %q); want %q and nothing sent", cmdErr.Msg, cmdErr.Hint, want)
	}
	if hasStep(d.recorded(), "headless-click") {
		t.Errorf("a refused action clicked: %v", d.recorded())
	}
}

func TestNotificationDismiss(t *testing.T) {
	d := newToastDesk(t)
	cmds := d.cmds()
	runCommand(t, cmds, "list-notifications", nil)
	out, cmdErr := runCommand(t, cmds, "notification-dismiss", protocol.NotificationDismissArgs{ID: 1})
	if cmdErr != nil || out != "ok\n" {
		t.Fatalf("notification-dismiss 1: %q, %v", out, cmdErr)
	}
	if d.hIndex != 5 {
		t.Errorf("clicked element %d, want DismissButton (5)", d.hIndex)
	}
}

// A press whose notification still shows after the read-back budget was
// delivered: the failure says so, and is no "nothing was sent".
func TestNotificationStillShowingAfterThePress(t *testing.T) {
	d := newToastDesk(t)
	d.stays = true
	cmds := d.cmds()
	runCommand(t, cmds, "list-notifications", nil)
	_, cmdErr := runCommand(t, cmds, "notification-action", protocol.NotificationActionArgs{ID: 1, Button: "Alpha"})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "still shows") || strings.Contains(cmdErr.Msg, nothingSentNote) || !strings.Contains(cmdErr.Hint, "do not repeat it") {
		t.Errorf("a press that left the banner showing: %v", cmdErr)
	}
}

func TestWaitNotification(t *testing.T) {
	d := newToastDesk(t)
	polls := 0
	d.shown = false
	d.svc.Sleep = func(time.Duration) {
		polls++
		d.shown = polls >= 3 // the toast shows on the fourth poll
	}
	cmds := d.cmds()
	out, cmdErr := runCommand(t, cmds, "wait", protocol.WaitArgs{For: protocol.WaitNotification, App: "PowerShell", Value: "pick"})
	if cmdErr != nil {
		t.Fatalf("wait notification: %v", cmdErr.Error())
	}
	if !strings.HasPrefix(out, "1\tWindows PowerShell\twinuse probe") {
		t.Errorf("output = %q", out)
	}
	// The wait numbers its matches: the action takes them.
	if _, cmdErr := runCommand(t, cmds, "notification-action", protocol.NotificationActionArgs{ID: 1, Button: "Alpha"}); cmdErr != nil {
		t.Errorf("notification-action after the wait: %v", cmdErr.Error())
	}

	// No match before the timeout.
	d = newToastDesk(t)
	_, cmdErr = runCommand(t, d.cmds(), "wait", protocol.WaitArgs{For: protocol.WaitNotification, App: "Signal", TimeoutMS: 500})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, `waiting for a notification from "Signal"`) || !strings.Contains(cmdErr.Hint, "Do Not Disturb") {
		t.Errorf("a wait with no match: %v", cmdErr)
	}
}

// The banner is a shell surface: listed by --shell, read by Window ID,
// and a Headless click on its Elements is refused with the notification
// commands in the hint (desktop-parity 09's rule, 18's route).
func TestToastBannerIsAShellSurface(t *testing.T) {
	d := newToastDesk(t)
	cmds := d.cmds()
	out, _ := runCommand(t, cmds, "list-windows", protocol.ListWindowsArgs{Shell: true})
	if !strings.Contains(out, "970\tWindows Shell Experience Host\tNew notification\n") {
		t.Errorf("list-windows --shell misses the banner:\n%s", out)
	}
	_, cmdErr := runCommand(t, cmds, "click", protocol.ClickArgs{Window: "970", ElementIndex: intPtr(8)})
	if cmdErr == nil || !strings.HasSuffix(cmdErr.Msg, nothingSentNote) || !strings.Contains(cmdErr.Hint, "winuse notification-action <n> <button>") {
		t.Errorf("a Headless click on the banner: %v", cmdErr)
	}
	if hasStep(d.recorded(), "headless-click") {
		t.Errorf("the refused click clicked: %v", d.recorded())
	}
}

// A banner showing while an Action on a shell surface runs is no Window
// the Action opened; Start still is.
func TestOpenedNotesSkipTheToastBanner(t *testing.T) {
	before := openedBefore{target: shellDesktop(false, false)[0], known: map[uint64]bool{}, shells: map[uint64]bool{}}
	after := append(shellDesktop(false, true), bannerWindow(false))
	opened, _ := openedChanges(before, after)
	if len(opened) != 1 || opened[0].ID != shellStartID {
		t.Errorf("opened = %+v, want Start alone", opened)
	}
}

// openShellDesk is the shell desktop for open-shell: the notification
// panel and quick settings show once a key was pressed, Start too.
func openShellDesk(t *testing.T) *toastDesk {
	d := newToastDesk(t)
	d.shown = false
	d.surfaces = func() []win.WindowInfo {
		pressed := len(d.pressedKeys()) > 0
		clicked := hasStep(d.recorded(), "click")
		ws := append(shellDesktop(clicked, pressed && d.pressedKeys()[0] == "Control_L+Escape"), bannerWindow(true))
		panel := win.WindowInfo{ID: panelID, Class: "Windows.UI.Core.CoreWindow", Title: "Notification Center", App: "Windows Shell Experience Host",
			ExePath: shellHostExe, Visible: true, Cloaked: !(pressed && d.pressedKeys()[0] == "Win+n"), Bounds: win.Rect{Left: 1900, Top: 300, Right: 2560, Bottom: 1368}, Shell: win.ShellActionCenter}
		quick := win.WindowInfo{ID: quickID, Class: "Windows.UI.Core.CoreWindow", Title: "Quick settings", App: "Shell Host",
			ExePath: shellHost2Exe, Visible: true, Cloaked: !(pressed && d.pressedKeys()[0] == "Win+a"), Bounds: win.Rect{Left: 1900, Top: 700, Right: 2560, Bottom: 1368}, Shell: win.ShellActionCenter}
		return append(ws, panel, quick)
	}
	return d
}

func (d *toastDesk) pressedKeys() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.pressed...)
}

func TestOpenShellRefusesWithoutHeaded(t *testing.T) {
	d := openShellDesk(t)
	for _, surface := range protocol.ShellSurfaces {
		_, cmdErr := runCommand(t, d.cmds(), "open-shell", protocol.OpenShellArgs{Surface: surface})
		if cmdErr == nil || !strings.HasSuffix(cmdErr.Msg, nothingSentNote) || !strings.Contains(cmdErr.Hint, "winuse --headed open-shell "+surface) {
			t.Errorf("open-shell %s without --headed: %v", surface, cmdErr)
		}
	}
	if len(d.pressedKeys()) > 0 || len(d.recorded()) > 0 && hasStep(d.recorded(), "click") {
		t.Errorf("a refused open-shell sent something: keys %v, steps %v", d.pressedKeys(), d.recorded())
	}
	if _, cmdErr := runCommand(t, d.cmds(), "open-shell", protocol.OpenShellArgs{Surface: "taskbar", Headed: true}); cmdErr == nil {
		t.Error("open-shell taskbar was accepted")
	}
}

func TestOpenShellPressesTheAccelerator(t *testing.T) {
	for _, c := range []struct {
		surface, key, line string
	}{
		{"notifications", "Win+n", "975\tWindows Shell Experience Host\tNotification Center\n"},
		{"quick-settings", "Win+a", "976\tShell Host\tQuick settings\n"},
		{"start", "Control_L+Escape", "960\tStart\tStart\n"},
	} {
		d := openShellDesk(t)
		out, cmdErr := runCommand(t, d.cmds(), "open-shell", protocol.OpenShellArgs{Surface: c.surface, Headed: true})
		if cmdErr != nil {
			t.Errorf("open-shell %s: %v", c.surface, cmdErr.Error())
			continue
		}
		if keys := d.pressedKeys(); len(keys) != 1 || keys[0] != c.key {
			t.Errorf("open-shell %s pressed %v, want %s", c.surface, keys, c.key)
		}
		if !strings.HasPrefix(out, c.line) {
			t.Errorf("open-shell %s output:\n got %q\nwant %q first", c.surface, out, c.line)
		}
		if hasStep(d.recorded(), "activate") {
			t.Errorf("open-shell %s activated a Window: %v", c.surface, d.recorded())
		}
	}
}

func TestOpenShellAlreadyOpenSendsNothing(t *testing.T) {
	d := openShellDesk(t)
	d.mu.Lock()
	d.pressed = []string{"Win+n"} // the panel shows already
	d.mu.Unlock()
	out, cmdErr := runCommand(t, d.cmds(), "open-shell", protocol.OpenShellArgs{Surface: "notifications", Headed: true})
	if cmdErr != nil || !strings.HasPrefix(out, "975\t") || !strings.Contains(out, "was open already; nothing was sent") {
		t.Errorf("an open panel: %q, %v", out, cmdErr)
	}
	if len(d.pressedKeys()) != 1 {
		t.Errorf("keys pressed: %v, want none added", d.pressedKeys())
	}
}

func TestOpenShellSurfaceThatDoesNotShow(t *testing.T) {
	d := openShellDesk(t)
	d.surfaces = func() []win.WindowInfo { return shellDesktop(false, false) } // nothing opens
	_, cmdErr := runCommand(t, d.cmds(), "open-shell", protocol.OpenShellArgs{Surface: "notifications", Headed: true})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "sent Win+N and the notification panel did not show") || strings.Contains(cmdErr.Msg, nothingSentNote) {
		t.Errorf("a panel that did not open: %v", cmdErr)
	}
}

// open-shell overflow clicks the chevron the Headed way: the taskbar is
// read, activated and clicked with the real pointer.
func TestOpenShellOverflowClicksTheChevron(t *testing.T) {
	d := openShellDesk(t)
	d.trees.taskbar = &wstate.Tree{FocusedIndex: -1, Elements: []wstate.Element{
		{Role: "pane", Name: "Taskbar"},
		{Depth: 1, Role: "button", Name: "Start", AutomationId: "StartButton"},
		{Depth: 1, Role: "button", Name: "Show Hidden Icons", AutomationId: "SystemTrayIcon"},
	}}
	out, cmdErr := runCommand(t, d.cmds(), "open-shell", protocol.OpenShellArgs{Surface: "overflow", Headed: true})
	if cmdErr != nil {
		t.Fatalf("open-shell overflow: %v", cmdErr.Error())
	}
	if steps := d.recorded(); !hasStep(steps, "activate") || !hasStep(steps, "click") || d.clickTarget != shellTaskbarID {
		t.Errorf("steps = %v, click target %d; want the taskbar activated and clicked", steps, d.clickTarget)
	}
	if !strings.HasPrefix(out, "950\tWindows Explorer\tSystem tray overflow window.\n") {
		t.Errorf("output = %q", out)
	}

	// No chevron (every icon shown): refused with nothing sent.
	d = openShellDesk(t)
	d.trees.taskbar = &wstate.Tree{FocusedIndex: -1, Elements: []wstate.Element{{Role: "pane", Name: "Taskbar"}}}
	_, cmdErr = runCommand(t, d.cmds(), "open-shell", protocol.OpenShellArgs{Surface: "overflow", Headed: true})
	if cmdErr == nil || !strings.HasSuffix(cmdErr.Msg, nothingSentNote) || hasStep(d.recorded(), "click") {
		t.Errorf("no chevron: %v, steps %v", cmdErr, d.recorded())
	}
}
