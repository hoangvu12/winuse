package cli

import (
	"strings"
	"testing"
)

func TestParseScreenshotArgs(t *testing.T) {
	for args, want := range map[string]int{"--display 0": 0, "--display=2": 2, "--display 10": 10} {
		a, err := parseScreenshotArgs(strings.Fields(args))
		if err != nil || a.Display == nil || *a.Display != want {
			t.Errorf("%q = %+v, %v; want display %d", args, a, err, want)
		}
	}
	for _, bad := range []string{"", "--display", "--display -1", "--display x", "--display 0 --display 1", "101", "--window 101"} {
		if _, err := parseScreenshotArgs(strings.Fields(bad)); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	_, err := parseScreenshotArgs(nil)
	if err == nil || !strings.Contains(err.Error(), "get-window-state <window> --screenshot") {
		t.Errorf("no --display: %v, want the pointer to a Window's screenshot", err)
	}
}

// TestParseScreenshotScreenAlias: --screen is agent-desktop's spelling of
// --display, parsed and validated the same.
func TestParseScreenshotScreenAlias(t *testing.T) {
	for args, want := range map[string]int{"--screen 0": 0, "--screen=2": 2, "--screen 10": 10} {
		a, err := parseScreenshotArgs(strings.Fields(args))
		if err != nil || a.Display == nil || *a.Display != want {
			t.Errorf("%q = %+v, %v; want display %d", args, a, err, want)
		}
	}
	for args, want := range map[string]string{
		"--screen":               "--screen takes a display number",
		"--screen=":              `--screen takes a display number from 0, got ""`,
		"--screen -1":            `--screen takes a display number from 0, got "-1"`,
		"--screen x":             `--screen takes a display number from 0, got "x"`,
		"--screen 0 --screen 1":  "--screen is given twice",
		"--display 0 --screen 0": "--display and --screen are the same option; give one",
		"--screen=1 --display 1": "--screen and --display are the same option; give one",
		"--screens 0":            `unexpected argument "--screens"`,
	} {
		_, err := parseScreenshotArgs(strings.Fields(args))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", args, err, want)
		}
	}
}

// TestDisplayUsageErrorsExitTwo: usage failures never dial the helper.
func TestDisplayUsageErrorsExitTwo(t *testing.T) {
	for args, want := range map[string]string{
		"screenshot":                        "usage: winuse screenshot --display <n>",
		"screenshot 101":                    "usage: winuse screenshot --display <n>",
		"list-displays all":                 "list-displays takes no arguments",
		"screenshot --screen x":             "--screen takes a display number from 0",
		"screenshot --display 0 --screen 0": "--display and --screen are the same option",
	} {
		var code int
		stderr := captureStderr(t, func() { code = Main(strings.Fields(args)) })
		if code != 2 || !strings.Contains(stderr, want) {
			t.Errorf("%q: exit %d, stderr %q; want 2 and %q", args, code, stderr, want)
		}
	}
	for _, cmd := range []string{"list-displays", "screenshot --display"} {
		if !strings.Contains(Usage, "\n  "+cmd) {
			t.Errorf("usage text does not document %q", cmd)
		}
	}
	if !strings.Contains(Usage, "--screen <n>") {
		t.Errorf("usage text does not document the --screen alias")
	}
}
