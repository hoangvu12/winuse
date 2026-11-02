package status

import (
	"strings"
	"testing"
	"time"
)

func TestFormatUptime(t *testing.T) {
	cases := map[time.Duration]string{
		0:                                  "0s",
		500 * time.Millisecond:             "0s",
		4*time.Minute + 12*time.Second + 9: "4m12s",
		26*time.Hour + 3*time.Second:       "26h0m3s",
	}
	for d, want := range cases {
		if got := FormatUptime(d); got != want {
			t.Errorf("FormatUptime(%s) = %q, want %q", d, got, want)
		}
	}
}

func TestFormatLines(t *testing.T) {
	r := Report{
		CLIVersion:    "1.0.0",
		Helper:        Helper{State: HelperRunning, Version: "1.0.0", PID: 7, Uptime: time.Minute},
		Locked:        true,
		Source:        SourceHelper,
		Browsers:      []string{"chrome process 1, a", "edge process 2, b"},
		BrowsersKnown: true,
	}
	got := r.Format()
	for _, want := range []string{
		"winuse: 1.0.0\n",
		"helper: running, 1.0.0, pid 7, up 1m0s\n",
		"desktop: locked; unlock before using winuse\n",
		"integrity: unknown (the helper's)\n",
		"dpi awareness: unknown (the helper's)\n",
		"browser connections: chrome process 1, a; edge process 2, b\n",
		"windows: unknown\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Format() = %q, missing %q", got, want)
		}
	}
	if n := strings.Count(got, "\n"); n != 7 {
		t.Errorf("Format() has %d lines, want 7", n)
	}
}

func TestFormatUnknownBrowsersFromARunningHelper(t *testing.T) {
	r := Report{Helper: Helper{State: HelperRunning}, Source: SourceHelper}
	if !strings.Contains(r.Format(), "browser connections: unknown\n") {
		t.Errorf("Format() = %q", r.Format())
	}
}
