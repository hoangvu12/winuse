package cli

import (
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestParseNotificationFilters(t *testing.T) {
	cases := map[string]protocol.WaitArgs{
		"":                          {},
		"--app Signal":              {App: "Signal"},
		"--app=Signal --text=Mum":   {App: "Signal", Value: "Mum"},
		"--text Mum --app Signal":   {App: "Signal", Value: "Mum"},
		"--text=build --app=GitHub": {App: "GitHub", Value: "build"},
	}
	for args, want := range cases {
		got, err := parseNotificationFilters("list-notifications", strings.Fields(args))
		if err != nil || got != want {
			t.Errorf("%q = %+v, %v; want %+v", args, got, err, want)
		}
	}
	for args, want := range map[string]string{
		"--app":                 "--app needs a value",
		"--app=":                "--app needs a value",
		"--app a --app b":       "--app is given twice",
		"--text a --text=b":     "--text is given twice",
		"Signal":                `takes --app and --text, not "Signal"`,
		"--limit 3":             `not "--limit"`,
		"--timeout 5 --app a":   "not --timeout",
		"--app a --timeout=0.5": "not --timeout",
	} {
		_, err := parseNotificationFilters("list-notifications", strings.Fields(args))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", args, err, want)
		}
	}
}

func TestParseWaitNotification(t *testing.T) {
	a, err := parseWaitCommand(strings.Fields("notification --app Signal --text Mum --timeout 30"))
	want := protocol.WaitArgs{For: protocol.WaitNotification, App: "Signal", Value: "Mum", TimeoutMS: 30000}
	if err != nil || a != want {
		t.Errorf("wait notification = %+v, %v; want %+v", a, err, want)
	}
	if a, err := parseWaitCommand([]string{"notification"}); err != nil || a.For != protocol.WaitNotification {
		t.Errorf("wait notification alone = %+v, %v", a, err)
	}
	if waitTimeout(want) != 30*1e9+protocol.WaitMargin {
		t.Errorf("the request budget is %v, want the wait plus the margin", waitTimeout(want))
	}
	for args, want := range map[string]string{
		"notification --timeout 0":   "--timeout must be a number of seconds",
		"notification --timeout 301": "--timeout must be a number of seconds",
		"notification Signal":        `not "Signal"`,
	} {
		_, err := parseWaitCommand(strings.Fields(args))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("wait %s: %v, want %q", args, err, want)
		}
	}
}

// TestNotificationUsageErrorsExitTwo: usage failures never dial the
// helper.
func TestNotificationUsageErrorsExitTwo(t *testing.T) {
	for args, want := range map[string]string{
		"list-notifications Signal":        "usage: winuse list-notifications",
		"notification-action":              "usage: winuse notification-action <n> <button>",
		"notification-action 1":            "takes a notification's number and a button's name",
		"notification-action 0 Reply":      "its number from winuse list-notifications",
		"notification-action one Reply":    "its number from winuse list-notifications",
		"notification-dismiss":             "usage: winuse notification-dismiss <n>",
		"notification-dismiss 1 2":         "takes a notification's number",
		"notification-dismiss -1":          "its number from winuse list-notifications",
		"open-shell":                       "usage: winuse --headed open-shell",
		"open-shell taskbar":               `not "taskbar"`,
		"--headed open-shell start extra":  "takes one surface",
		"open-shell notifications --force": "takes one surface",
	} {
		var code int
		stderr := captureStderr(t, func() { code = Main(strings.Fields(args)) })
		if code != 2 || !strings.Contains(stderr, want) {
			t.Errorf("%q: exit %d, stderr %q; want 2 and %q", args, code, stderr, want)
		}
	}
	for _, cmd := range []string{"list-notifications", "notification-action <n> <button>", "notification-dismiss <n>", "wait notification", "open-shell start|notifications|quick-settings|overflow"} {
		if !strings.Contains(Usage, "\n  "+cmd) {
			t.Errorf("usage text does not document %q", cmd)
		}
	}
}

// TestOpenShellCarriesHeaded: --headed before or after the command reaches
// open-shell's request (headed.go strips it everywhere outside text).
func TestOpenShellCarriesHeaded(t *testing.T) {
	for _, line := range []string{"--headed open-shell notifications", "open-shell notifications --headed", "open-shell --headed notifications"} {
		args, headed := extractHeaded(strings.Fields(line))
		if !headed || strings.Join(args, " ") != "open-shell notifications" {
			t.Errorf("%q: args %q, headed %v", line, args, headed)
		}
	}
}
