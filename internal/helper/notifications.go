package helper

// Notifications (desktop-parity ticket 18, like agent-desktop's
// list-notifications, notification-action, dismiss-notification and
// wait --notification). Four Desktop commands over the toasts Windows
// shows:
//
//	list-notifications [--app <name>] [--text <text>]
//	notification-action <n> <button>
//	notification-dismiss <n>
//	wait notification [--app <name>] [--text <text>]
//
// The routes, from the probe (headless-gaps round 5, section 7):
//
//   - A toast shows in ShellExperienceHost's toast banner, one reused
//     CoreWindow ("New notification", win.ShellNotification). It is found
//     by that name among the shell surfaces: a before-and-after window
//     diff never sees it, because the window is reused.
//   - Its tree holds one toast view per notification (AutomationId
//     NormalToastView), with the sender (SenderName), Title, MessageText,
//     the action buttons (VerbButton) and DismissButton. It reads Headless.
//   - Invoke on an action button reached the raising App and Invoke on
//     DismissButton closed the banner, both with the foreground unchanged
//     while the App was listening. They are the measured exception to the
//     shell rule (ADR 0015 decision 4): a toast's buttons are pressed
//     Headless, by these commands only, never by a click on the banner's
//     Elements. An action whose App is not listening can hand the
//     foreground to the shell: that is reported after the ok (headless-max
//     03's detect-and-report), never prevented.
//   - The Notification Center panel does not open from UI Automation, so
//     these commands see the showing banners only. Older notifications are
//     read in the panel, opened with `winuse --headed open-shell
//     notifications` (openshell.go).
//
// list-notifications numbers what it lists, from 1, per Agent; wait
// notification numbers its matches the same way. notification-action and
// notification-dismiss take such a number. agent-desktop's identity guard:
// before acting they read the banners again and act only on the
// notification that still shows with the App, title and text that number
// was listed with; otherwise they refuse with nothing sent. A banner shows
// for about 5 seconds, so the agent acts right after listing it.
//
// The read and the wait only read: they take no lane. The two actions
// name no Window, so they take the desktop lane like the other Desktop
// commands that act (lanes.go), run in a passive Esc scope and show no
// Agent Cursor.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hoangvu12/winuse/internal/agent"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// The AutomationIds of a toast's Elements in the banner (probes round 5,
// section 7). A toast view's AutomationId ends in toastViewSuffix
// (NormalToastView).
const (
	toastViewSuffix     = "ToastView"
	toastSenderID       = "SenderName"
	toastTitleID        = "Title"
	toastTextID         = "MessageText"
	toastButtonID       = "VerbButton"
	toastDismissID      = "DismissButton"
	notificationPollGap = 100 * time.Millisecond // the read-back's and the wait's pause
	// notificationGoneBudget bounds the read-back of a pressed button:
	// the banner cloaked within about a second in the probe.
	notificationGoneBudget = 2 * time.Second
)

// notificationsUsage is the commands' usage line, for their hints.
const notificationsUsage = "usage: winuse list-notifications [--app <name>] [--text <text>]; winuse notification-action <n> <button>; winuse notification-dismiss <n>"

// noNotificationsLine is list-notifications' output when no banner shows.
const noNotificationsLine = "no notification is showing; older ones are in the Notification Center: winuse --headed open-shell notifications opens it, and winuse get-window-state <its window> reads it\n"

// notification is one toast a showing banner holds, read from the
// banner's tree.
type notification struct {
	Window  uint64
	App     string // the sender's name
	Title   string
	Text    string
	Buttons []string // the action buttons' names, in order

	buttons []int          // their Element Indexes in the read, as Buttons
	dismiss int            // DismissButton's Element Index, -1 when it has none
	banner  win.WindowInfo // the banner it shows in (readNotifications)
}

// notificationID is what a listed number stands for: the identity
// guard's record.
type notificationID struct {
	Window           uint64
	App, Title, Text string
}

func (n notification) id() notificationID {
	return notificationID{Window: n.Window, App: n.App, Title: n.Title, Text: n.Text}
}

// toastsIn reads the notifications out of a banner's tree: one per toast
// view, with its sender, title, text and buttons from the view's subtree.
// Pure: the offline tests feed it recorded trees.
func toastsIn(tree *wstate.Tree, window uint64) []notification {
	var out []notification
	els := tree.Elements
	for i := 0; i < len(els); i++ {
		if !strings.HasSuffix(els[i].AutomationId, toastViewSuffix) {
			continue
		}
		n := notification{Window: window, dismiss: -1}
		var text []string
		j := i + 1
		for ; j < len(els) && els[j].Depth > els[i].Depth; j++ {
			el := els[j]
			name := wstate.Collapse(el.Name)
			switch el.AutomationId {
			case toastSenderID:
				if n.App == "" {
					n.App = name
				}
			case toastTitleID:
				if n.Title == "" {
					n.Title = name
				}
			case toastTextID:
				if name != "" {
					text = append(text, name)
				}
			case toastButtonID:
				n.Buttons = append(n.Buttons, name)
				n.buttons = append(n.buttons, j)
			case toastDismissID:
				n.dismiss = j
			}
		}
		n.Text = strings.Join(text, " ")
		out = append(out, n)
		i = j - 1
	}
	return out
}

// notificationMatches reports whether a notification passes the filters:
// app in its sender's name, text in its title or text, any part, without
// case. Empty filters pass everything.
func notificationMatches(n notification, app, text string) bool {
	has := func(s, part string) bool { return strings.Contains(strings.ToLower(s), strings.ToLower(part)) }
	app, text = strings.TrimSpace(app), strings.TrimSpace(text)
	return (app == "" || has(n.App, app)) && (text == "" || has(n.Title, text) || has(n.Text, text))
}

// formatNotifications renders one line per notification, numbered from
// 1: "<n>\t<App>\t<title>\t<text>", then "\tbuttons: A, B" when it has
// any.
func formatNotifications(ns []notification) string {
	var b strings.Builder
	for i, n := range ns {
		fmt.Fprintf(&b, "%d\t%s\t%s\t%s", i+1, n.App, n.Title, n.Text)
		if len(n.Buttons) > 0 {
			b.WriteString("\tbuttons: " + strings.Join(n.Buttons, ", "))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// notificationLog is what each Agent's notification numbers stand for:
// its latest list-notifications or wait notification.
type notificationLog struct {
	mu    sync.Mutex
	lists map[string][]notificationID
}

func newNotificationLog() *notificationLog {
	return &notificationLog{lists: map[string][]notificationID{}}
}

// set records an Agent's latest listing.
func (l *notificationLog) set(agent string, ns []notification) {
	ids := make([]notificationID, len(ns))
	for i, n := range ns {
		ids[i] = n.id()
	}
	l.mu.Lock()
	l.lists[agent] = ids
	l.mu.Unlock()
}

// get returns what number n stood for in the Agent's latest listing.
func (l *notificationLog) get(agent string, n int) (notificationID, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	list := l.lists[agent]
	if n < 1 || n > len(list) {
		return notificationID{}, false
	}
	return list[n-1], true
}

// notificationClicker is the element side of the two actions: the
// Headless click (UIA Invoke on a toast's buttons) on an Element of the
// banner's latest Window State. The real one is *uia.Client;
// Services.Actions is type-asserted to it.
type notificationClicker interface {
	HeadlessClick(ctx context.Context, hwnd uint64, index int) (string, error)
}

// notificationBanners lists the toast banners showing now, in z-order.
func (s Services) notificationBanners() ([]win.WindowInfo, *Error) {
	if s.Surfaces == nil {
		return nil, Fail("notifications cannot be read here", "")
	}
	all, err := s.Surfaces()
	if err != nil {
		return nil, Fail("enumerating windows: "+err.Error(), "")
	}
	var out []win.WindowInfo
	for _, w := range all {
		if w.Shell == win.ShellNotification && win.ShellSurface(w) {
			out = append(out, w)
		}
	}
	return out, nil
}

// readNotifications reads every showing banner's notifications. With
// store, each read becomes the banner's Window State, so its Element
// Indexes can be acted on; otherwise the trees are peeked and the stored
// Window States stay as they were. A banner that closed while it was read
// is skipped.
func (s Services) readNotifications(ctx context.Context, store bool) ([]notification, *Error) {
	banners, e := s.notificationBanners()
	if e != nil || len(banners) == 0 {
		return nil, e
	}
	peeker, canPeek := s.Trees.(TreePeeker)
	var out []notification
	for _, w := range banners {
		var tree *wstate.Tree
		var err error
		if store || !canPeek {
			tree, err = s.Trees.Capture(ctx, w.ID, w.Title, win.AppExeName(w), s.Budget)
		} else {
			tree, err = peeker.PeekTree(ctx, w.ID, w.Title, win.AppExeName(w), s.Budget)
		}
		switch {
		case errors.Is(err, uia.ErrWindowGone):
			continue // it timed out while being read
		case err != nil:
			return nil, treeReadError(err)
		}
		for _, n := range toastsIn(tree, w.ID) {
			n.banner = w
			out = append(out, n)
		}
	}
	return out, nil
}

// listNotifications is `winuse list-notifications`: the showing
// notifications, numbered for the Agent's next notification-action or
// notification-dismiss.
func listNotifications(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.ListNotificationsArgs
		if len(args) > 0 && string(args) != "null" {
			if err := json.Unmarshal(args, &a); err != nil {
				return "", Fail("list-notifications: invalid arguments: "+err.Error(), notificationsUsage)
			}
		}
		all, e := s.readNotifications(ctx, false)
		if e != nil {
			return "", e
		}
		var ns []notification
		for _, n := range all {
			if notificationMatches(n, a.App, a.Text) {
				ns = append(ns, n)
			}
		}
		s.notifications.set(agent.Key(ctx), ns)
		switch {
		case len(all) == 0:
			return noNotificationsLine, nil
		case len(ns) == 0:
			return fmt.Sprintf("no showing notification matches; %d other(s) show: winuse list-notifications lists them all\n", len(all)), nil
		}
		return formatNotifications(ns), nil
	}
}

// waitNotification is `wait notification [--app <name>] [--text <text>]`:
// it polls the banners until a matching notification shows, then prints
// and numbers the matches as list-notifications does.
func (s Services) waitNotification(ctx context.Context, a protocol.WaitArgs, want time.Duration, stopped func() bool) (string, *Error) {
	found, end, e := pollUntil(ctx, s.Sleep, want, notificationPollGap, stopped, func() ([]notification, bool, *Error) {
		all, e := s.readNotifications(ctx, false)
		if e != nil {
			return nil, false, e
		}
		var ns []notification
		for _, n := range all {
			if notificationMatches(n, a.App, a.Value) {
				ns = append(ns, n)
			}
		}
		return ns, len(ns) > 0, nil
	})
	switch {
	case e != nil:
		return "", e
	case end == waitStopped:
		return "", escStopBeforeActError()
	case end == waitTimedOut:
		return "", waitTimeoutError(notificationWanted(a), want, "",
			"a banner shows only while the user's notifications are on (Settings > System > Notifications) and Do Not Disturb is off; one that already left is in the Notification Center")
	}
	s.notifications.set(agent.Key(ctx), found)
	return formatNotifications(found), nil
}

// notificationWanted names what a notification wait waits for.
func notificationWanted(a protocol.WaitArgs) string {
	what := "a notification"
	if app := strings.TrimSpace(a.App); app != "" {
		what += fmt.Sprintf(" from %q", app)
	}
	if text := strings.TrimSpace(a.Value); text != "" {
		what += fmt.Sprintf(" with %q", text)
	}
	return what
}

// notificationAction is `winuse notification-action <n> <button>`: Invoke
// on the named action button of notification n.
func notificationAction(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.NotificationActionArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("notification-action: "+err.Error(), notificationsUsage)
		}
		button := strings.TrimSpace(a.Button)
		if button == "" {
			return "", Fail("notification-action needs the name of one of the notification's buttons", notificationsUsage)
		}
		return s.pressNotification(ctx, "notification-action", a.ID, func(n notification) (int, *Error) {
			for i, name := range n.Buttons {
				if strings.EqualFold(name, button) {
					return n.buttons[i], nil
				}
			}
			if len(n.Buttons) == 0 {
				return 0, nothingSent(Fail(fmt.Sprintf("notification %d has no buttons", a.ID),
					"winuse notification-dismiss "+strconv.Itoa(a.ID)+" closes it"))
			}
			return 0, nothingSent(Fail(fmt.Sprintf("notification %d has no button %q; its buttons: %s", a.ID, button, strings.Join(n.Buttons, ", ")),
				"name one of those buttons"))
		})
	}
}

// notificationDismiss is `winuse notification-dismiss <n>`: Invoke on the
// dismiss button of notification n. Windows keeps the notification in the
// Notification Center.
func notificationDismiss(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.NotificationDismissArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("notification-dismiss: "+err.Error(), notificationsUsage)
		}
		return s.pressNotification(ctx, "notification-dismiss", a.ID, func(n notification) (int, *Error) {
			if n.dismiss < 0 {
				return 0, nothingSent(Fail(fmt.Sprintf("notification %d shows no dismiss button", a.ID),
					"it closes by itself after a few seconds"))
			}
			return n.dismiss, nil
		})
	}
}

// pressNotification is the two actions' shared route: the identity
// guard, then the Headless click on the Element pick chooses, then the
// read-back (the notification left the banner) and the foreground note.
func (s Services) pressNotification(ctx context.Context, cmd string, n int, pick func(notification) (int, *Error)) (string, *Error) {
	if n < 1 {
		return "", Fail(fmt.Sprintf("%s takes a notification's number from 1, got %d", cmd, n), notificationsUsage)
	}
	if locked, desktop := s.Locked(); locked {
		return "", lockedDesktopFailure(desktop)
	}
	want, ok := s.notifications.get(agent.Key(ctx), n)
	if !ok {
		return "", nothingSent(Fail(fmt.Sprintf("notification %d is not in your latest list-notifications", n),
			"run winuse list-notifications (or winuse wait notification) first; its numbers name the notifications"))
	}
	clicker, ok := s.Actions.(notificationClicker)
	if !ok {
		return "", nothingSent(Fail("notifications cannot be acted on here: no UI Automation client", ""))
	}
	// The identity guard: read the banners again, and act only on the
	// notification that still shows as it was listed.
	now, e := s.readNotifications(ctx, true)
	if e != nil {
		return "", e
	}
	at := slices.IndexFunc(now, func(x notification) bool { return x.id() == want })
	if at < 0 {
		return "", nothingSent(Fail(fmt.Sprintf("notification %d (from %s) no longer shows as it was listed: it closed, timed out or changed", n, want.App),
			"run winuse list-notifications again and act right after it; a banner shows for about 5 seconds (winuse wait notification --app <app> catches the next one)"))
	}
	target := now[at]
	index, e := pick(target)
	if e != nil {
		return "", e
	}
	noteRoute(ctx, target.banner, protocol.RouteHeadless) // trace.go
	esc := s.Esc
	if esc == nil {
		esc = noopEscWatch{}
	}
	defer armPassive(esc)()
	if esc.Stopped() {
		return "", escStopBeforeActError()
	}
	fg := s.Foreground()
	if _, err := clicker.HeadlessClick(ctx, target.Window, index); err != nil {
		if errors.Is(err, uia.ErrNoClickRoute) || refusedBeforeCall(err) || errors.Is(err, uia.ErrStale) || errors.Is(err, uia.ErrNoState) {
			return "", nothingSent(Fail(fmt.Sprintf("%s on notification %d: %v", cmd, n, err),
				"run winuse list-notifications again; the banner may have changed"))
		}
		return "", mayHaveActed(Fail(fmt.Sprintf("%s on notification %d: %v", cmd, n, err),
			"the button may have been pressed; run winuse list-notifications before pressing it again"))
	}
	if !s.notificationLeft(ctx, want) {
		return "", &Error{Msg: fmt.Sprintf("%s pressed the button of notification %d and it still shows", cmd, n),
			Hint: "the press was delivered; do not repeat it, read winuse list-notifications", Exit: 1, Code: protocol.CodeVerifyFailed}
	}
	return withNotes(okOutput, s.notificationForegroundNote(fg)), nil
}

// notificationLeft polls, up to notificationGoneBudget, for a pressed
// notification to leave the banners: the banner hides (it cloaks), or it
// no longer holds that notification.
func (s Services) notificationLeft(ctx context.Context, want notificationID) bool {
	sleep := s.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	for waited := time.Duration(0); ; waited += notificationPollGap {
		now, e := s.readNotifications(ctx, false)
		if e == nil && !slices.ContainsFunc(now, func(x notification) bool { return x.id() == want }) {
			return true
		}
		if waited >= notificationGoneBudget || ctx.Err() != nil {
			return false
		}
		sleep(notificationPollGap)
	}
}

// notificationForegroundNote is the detect-and-report note of a pressed
// notification that moved the foreground: to its App, or to the shell when
// the App was not listening (probes round 5, section 7).
func (s Services) notificationForegroundNote(before uint64) string {
	note := s.foregroundNote(before, s.Foreground())
	if note == "" {
		return ""
	}
	return strings.TrimSuffix(note, "\n") + " (the notification's App took it, or the shell when the App was not running)\n"
}
