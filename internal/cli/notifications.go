package cli

// The notifications' Desktop commands and open-shell, CLI side
// (desktop-parity ticket 18, with ticket 09's open-shell):
// list-notifications, notification-action, notification-dismiss, `wait
// notification` (parsed with wait, wait.go) and `--headed open-shell`.
// Argument parsing is local (usage errors exit 2 and never dial the
// helper).

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
)

const (
	listNotificationsUsage   = "usage: winuse list-notifications [--app <name>] [--text <text>]"
	notificationActionUsage  = "usage: winuse notification-action <n> <button>   (n from winuse list-notifications)"
	notificationDismissUsage = "usage: winuse notification-dismiss <n>   (n from winuse list-notifications)"
	openShellUsage           = "usage: winuse --headed open-shell start|notifications|quick-settings|overflow"
)

// listNotificationsCmd implements `winuse list-notifications`: one line
// per notification the showing toast banners hold, numbered.
func listNotificationsCmd(args []string) int {
	a, err := parseNotificationFilters("list-notifications", args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, listNotificationsUsage)
		return 2
	}
	return runAndReport("list-notifications", protocol.ListNotificationsArgs{App: a.App, Text: a.Value})
}

// parseNotificationFilters parses --app <name> and --text <text> (or the
// --flag=value forms), each at most once, for list-notifications and
// `wait notification`; the text goes in Value. timeout also takes wait's
// --timeout.
func parseNotificationFilters(cmd string, args []string) (protocol.WaitArgs, error) {
	var a protocol.WaitArgs
	appSet, textSet := false, false
	for i := 0; i < len(args); i++ {
		flag, value, inline := strings.Cut(args[i], "=")
		switch flag {
		case "--app", "--text":
		case "--timeout":
			if cmd != "wait notification" {
				return a, fmt.Errorf("%s takes --app and --text, not --timeout", cmd)
			}
		default:
			return a, fmt.Errorf("%s takes --app and --text, not %q", cmd, args[i])
		}
		if !inline {
			if i+1 >= len(args) {
				return a, fmt.Errorf("%s needs a value", flag)
			}
			i++
			value = args[i]
		}
		switch flag {
		case "--app":
			if appSet {
				return a, errors.New("--app is given twice")
			}
			appSet, a.App = true, value
		case "--text":
			if textSet {
				return a, errors.New("--text is given twice")
			}
			textSet, a.Value = true, value
		case "--timeout":
			ms, err := parseWaitTimeoutMS(value)
			if err != nil {
				return a, err
			}
			a.TimeoutMS = ms
		}
		if strings.TrimSpace(value) == "" && flag != "--timeout" {
			return a, fmt.Errorf("%s needs a value", flag)
		}
	}
	return a, nil
}

// parseNotificationWait parses `wait notification [--app <name>] [--text
// <text>] [--timeout <s>]` after the word notification.
func parseNotificationWait(tokens []string) (protocol.WaitArgs, error) {
	a, err := parseNotificationFilters("wait notification", tokens)
	a.For = protocol.WaitNotification
	return a, err
}

// notificationActionCmd implements `winuse notification-action <n>
// <button>`: the button is the rest, joined with spaces.
func notificationActionCmd(args []string) int {
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "winuse: notification-action takes a notification's number and a button's name\n%s\n", notificationActionUsage)
		return 2
	}
	n, err := parseNotificationNumber(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, notificationActionUsage)
		return 2
	}
	return runAndReport("notification-action", protocol.NotificationActionArgs{ID: n, Button: strings.Join(args[1:], " ")})
}

// notificationDismissCmd implements `winuse notification-dismiss <n>`.
func notificationDismissCmd(args []string) int {
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "winuse: notification-dismiss takes a notification's number\n%s\n", notificationDismissUsage)
		return 2
	}
	n, err := parseNotificationNumber(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, notificationDismissUsage)
		return 2
	}
	return runAndReport("notification-dismiss", protocol.NotificationDismissArgs{ID: n})
}

// parseNotificationNumber parses a notification's number, from 1.
func parseNotificationNumber(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("a notification is named by its number from winuse list-notifications (1, 2, ...), got %q", s)
	}
	return n, nil
}

// openShellCmd implements `winuse --headed open-shell <surface>`. headed
// is the global --headed flag: without it the helper refuses with nothing
// sent and the retry.
func openShellCmd(args []string, headed bool) int {
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "winuse: open-shell takes one surface: %s\n%s\n", strings.Join(protocol.ShellSurfaces, ", "), openShellUsage)
		return 2
	}
	surface := strings.ToLower(args[0])
	known := false
	for _, s := range protocol.ShellSurfaces {
		known = known || s == surface
	}
	if !known {
		fmt.Fprintf(os.Stderr, "winuse: open-shell opens one of %s, not %q\n%s\n", strings.Join(protocol.ShellSurfaces, ", "), args[0], openShellUsage)
		return 2
	}
	return runAndReport("open-shell", protocol.OpenShellArgs{Surface: surface, Headed: headed})
}

// runAndReport sends one command and prints its response, returning the
// exit code.
func runAndReport(cmd string, args any) int {
	if err := runWithArgs(cmd, args); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}
