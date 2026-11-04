package cli

// Offline tests for the browser commands' CLI side: subcommand and flag
// parsing, usage errors (exit 2, the helper never dialed), the usage
// text, and the budgets of the long commands. The helper registry test
// (internal/helper/registry_test.go) checks the same wire names.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestParseBrowserArgs(t *testing.T) {
	cases := []struct {
		args    []string
		cmd     string
		want    any
		wantErr string
	}{
		{args: []string{"status"}, cmd: "browser-status", want: protocol.BrowserArgs{}},
		{args: []string{"status", "Edge"}, cmd: "browser-status", want: protocol.BrowserArgs{Browser: "edge"}},
		{args: []string{"use", "auto"}, cmd: "browser-use", want: protocol.BrowserArgs{Browser: "auto"}},
		{args: []string{"use"}, cmd: "browser-use", want: protocol.BrowserArgs{}},
		{args: []string{"launch", "chrome"}, cmd: "browser-launch", want: protocol.BrowserArgs{Browser: "chrome"}},
		{args: []string{"connect"}, cmd: "browser-connect", want: protocol.BrowserArgs{}},
		{args: []string{"stop", "brave"}, cmd: "browser-stop", want: protocol.BrowserArgs{Browser: "brave"}},
		{args: []string{"clone"}, cmd: "browser-clone", want: protocol.BrowserCloneArgs{}},
		{args: []string{"clone", "--close-source", "chrome", "--profile", "Profile 1", "--fresh"}, cmd: "browser-clone",
			want: protocol.BrowserCloneArgs{Browser: "chrome", Profile: "Profile 1", Fresh: true, CloseSource: true}},
		{args: nil, wantErr: "browser needs a subcommand"},
		{args: []string{"open"}, wantErr: `unknown browser subcommand "open"`},
		{args: []string{"ext", "install"}, cmd: "browser-ext-install", want: protocol.BrowserExtArgs{}},
		{args: []string{"ext", "install", "All"}, cmd: "browser-ext-install", want: protocol.BrowserExtArgs{Browser: "all"}},
		{args: []string{"ext", "install", "edge"}, cmd: "browser-ext-install", want: protocol.BrowserExtArgs{Browser: "edge"}},
		{args: []string{"ext", "status"}, cmd: "browser-ext-status", want: protocol.BrowserExtArgs{}},
		{args: []string{"ext", "status", "Brave"}, cmd: "browser-ext-status", want: protocol.BrowserExtArgs{Browser: "brave"}},
		{args: []string{"ext", "install", "brave", "--open"}, cmd: "browser-ext-install", want: protocol.BrowserExtArgs{Browser: "brave", Open: true}},
		{args: []string{"ext", "status", "--wait", "120", "brave"}, cmd: "browser-ext-status", want: protocol.BrowserExtArgs{Browser: "brave", Wait: 120}},
		{args: []string{"ext", "status", "--wait"}, wantErr: "--wait needs a number of seconds"},
		{args: []string{"ext", "status", "--wait", "0"}, wantErr: "--wait takes 1 to 600 seconds"},
		{args: []string{"ext", "install", "--wait", "5"}, wantErr: "browser ext install has no option --wait"},
		{args: []string{"ext", "status", "--open"}, wantErr: "browser ext status has no option --open"},
		{args: []string{"ext"}, wantErr: "browser ext needs install or status"},
		{args: []string{"ext", "uninstall"}, wantErr: `unknown browser ext subcommand "uninstall"`},
		{args: []string{"ext", "install", "chrome", "edge"}, wantErr: "browser ext install takes at most one browser name"},
		{args: []string{"ext", "status", "--no-wait"}, wantErr: "browser ext status has no option --no-wait"},
		{args: []string{"ext", "status", "all"}, wantErr: "browser ext status takes one browser name"},
		{args: []string{"launch", "chrome", "edge"}, wantErr: "browser launch takes at most one browser name"},
		{args: []string{"stop", "--all"}, wantErr: "browser stop has no option --all"},
		{args: []string{"clone", "--profile"}, wantErr: "--profile needs a profile folder"},
		{args: []string{"clone", "--port", "9222"}, wantErr: "browser clone has no option --port"},
		{args: []string{"clone", "chrome", "edge"}, wantErr: "browser clone takes at most one browser name"},
	}
	for _, tc := range cases {
		cmd, req, err := parseBrowserArgs(tc.args)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("%q: err = %v, want %q", tc.args, err, tc.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: unexpected error %v", tc.args, err)
			continue
		}
		if cmd != tc.cmd || req != tc.want {
			t.Errorf("%q: = %s %+v, want %s %+v", tc.args, cmd, req, tc.cmd, tc.want)
		}
	}
}

// TestExtStatusWait (browser-ux 02): --wait polls once a second while the
// failure is one the user's next step in the browser clears, returns the
// first success, and the last failure when the time is up; a failure that
// needs a command returns at once.
func TestExtStatusWait(t *testing.T) {
	now := time.Unix(0, 0)
	clock := func() time.Time { return now }
	sleep := func(d time.Duration) { now = now.Add(d) }
	notLoaded := &exitError{msg: "the winuse extension is not loaded in Brave yet", code: 1}
	notConnected := &exitError{msg: "the winuse extension is loaded in Brave but has not connected to winuse", code: 1}

	calls := 0
	err := extStatusWait(protocol.BrowserExtArgs{Wait: 30}, func() error {
		calls++
		switch {
		case calls < 3:
			return notLoaded
		case calls < 5:
			return notConnected
		}
		return nil
	}, sleep, clock)
	if err != nil || calls != 5 {
		t.Errorf("connected on the 5th call: err %v, calls %d", err, calls)
	}

	calls, now = 0, time.Unix(0, 0)
	err = extStatusWait(protocol.BrowserExtArgs{Wait: 3}, func() error { calls++; return notLoaded }, sleep, clock)
	if err != notLoaded || calls != 4 {
		t.Errorf("time up: err %v, calls %d", err, calls)
	}

	calls = 0
	notRegistered := &exitError{msg: "the winuse native host is not registered for Brave, so Brave cannot start it", code: 1}
	err = extStatusWait(protocol.BrowserExtArgs{Wait: 30}, func() error { calls++; return notRegistered }, sleep, clock)
	if err != notRegistered || calls != 1 {
		t.Errorf("not registered: err %v, calls %d", err, calls)
	}
}

func TestBrowserUsageErrorsExit2(t *testing.T) {
	for _, args := range [][]string{{"browser"}, {"browser", "nope"}, {"browser", "use", "a", "b"}, {"browser", "ext", "path"}} {
		path := filepath.Join(t.TempDir(), "stderr")
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		oldStderr, oldStdout := os.Stderr, os.Stdout
		os.Stderr, os.Stdout = f, f
		code := Main(args)
		os.Stderr, os.Stdout = oldStderr, oldStdout
		f.Close()
		if code != 2 {
			t.Errorf("%q: exit = %d, want 2", args, code)
		}
		got, _ := os.ReadFile(path)
		if !strings.Contains(string(got), "usage: winuse browser status [browser]") {
			t.Errorf("%q: stderr = %q, want the browser usage", args, got)
		}
	}
}

func TestUsageMentionsTheBrowserCommands(t *testing.T) {
	for _, sub := range []string{"status", "use", "launch", "clone", "connect", "stop"} {
		if !strings.Contains(Usage, "browser "+sub+" [browser") {
			t.Errorf("usage text missing browser %s", sub)
		}
		if !strings.Contains(browserUsage, "winuse browser "+sub) {
			t.Errorf("browser usage missing %s", sub)
		}
	}
	for _, s := range []string{"browser ext install [browser|all]", "browser ext status [browser]"} {
		if !strings.Contains(Usage, s) || !strings.Contains(browserUsage, "winuse "+s) {
			t.Errorf("usage text missing %s", s)
		}
	}
	for _, s := range []string{Usage, browserUsage} {
		if strings.ContainsAny(s, "–—") {
			t.Error("usage text has an en or em dash")
		}
	}
}

func TestLongBrowserCommandsHaveTheirBudgets(t *testing.T) {
	for cmd, want := range map[string]int{"browser-status": 30, "browser-launch": 30, "browser-stop": 30, "browser-connect": 90, "browser-clone": 180, "browser-ext-install": 30, "browser-ext-status": 30} {
		d, ok := protocol.CommandTimeout(cmd)
		if !ok || int(d.Seconds()) != want {
			t.Errorf("%s budget = %v (%v), want %ds", cmd, d, ok, want)
		}
	}
	if _, ok := protocol.CommandTimeout("browser-use"); ok {
		t.Error("browser use needs no longer budget")
	}
}

// browser attach and detach (headless-max 15) take exactly one Window.
func TestParseBrowserAttach(t *testing.T) {
	for _, sub := range []string{"attach", "detach"} {
		cmd, req, err := parseBrowserArgs([]string{sub, "700"})
		if err != nil || cmd != "browser-"+sub || req.(protocol.AttachArgs).Window != "700" {
			t.Errorf("%s: %s %+v, %v", sub, cmd, req, err)
		}
		for _, bad := range [][]string{{sub}, {sub, "700", "701"}, {sub, "--force"}} {
			if _, _, err := parseBrowserArgs(bad); err == nil {
				t.Errorf("%q parsed", bad)
			}
		}
	}
	if d, ok := protocol.CommandTimeout("browser-attach"); !ok || d != protocol.BrowserTimeout {
		t.Errorf("browser-attach budget %v, %v", d, ok)
	}
}
