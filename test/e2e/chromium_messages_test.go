//go:build e2e

// Typing, keys and clicks into a Chromium or Electron Window without a
// Browser Connection (headless-max ticket 20): window messages sent to the
// covered Window's Chrome_WidgetWin_1. The browser (or Electron) is started
// directly on the fixture page, with no debugging port, so winuse has no
// Browser Connection to it. A double click focuses the page's input, text
// is typed, Ctrl+A selects it and new text replaces it; the Window State
// shows each value, and the cover keeps the foreground throughout.
//
// User-run only (see the package comment and test/e2e/README.md); an agent
// must never start them. The Electron leg needs WINUSE_E2E_ELECTRON (the
// prebuilt electron.exe, whose default app opens the page given on its
// command line) and skips without it.

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// chromiumMessagesOn drives the route on a browser or Electron executable.
func chromiumMessagesOn(t *testing.T, exe string, args ...string) {
	t.Helper()
	page, err := filepath.Abs("testdata/chromium-fixture.html")
	if err != nil {
		t.Fatalf("resolving the fixture page: %v", err)
	}
	url := "file:///" + strings.ReplaceAll(filepath.ToSlash(page), " ", "%20")
	cmd := exec.Command(exe, append(args, url)...)
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting %s: %v", exe, err)
	}
	defer terminatePID(t, uint32(cmd.Process.Pid))

	window := strconv.FormatUint(findWindowByTitle(t, chromiumPageTitle, 30*time.Second), 10)
	cover := coverWindow(t, window)
	state, stderr, exit := runGetWindowState(t, window)
	if exit != 0 {
		t.Fatalf("get-window-state: exit %d, stderr:\n%s", exit, stderr)
	}
	if !strings.Contains(state, "the Page is read through UI Automation because") {
		t.Fatalf("the Window State does not say the Page is read without a Browser Connection; this test needs none:\n%s", state)
	}
	input := strconv.Itoa(webElementIndex(t, state, "edit", "winuse e2e input"))

	step := func(what string, args []string, wantPrefix string) {
		t.Helper()
		res, overlaySeen := runWatched(t, args)
		if res.exit != 0 || !strings.HasPrefix(res.stdout, wantPrefix) {
			t.Fatalf("%s: exit %d, stdout %q, stderr %q", what, res.exit, res.stdout, res.stderr)
		}
		if overlaySeen {
			t.Errorf("%s: the in-control banner was shown", what)
		}
		assertForeground(t, what, cover)
	}
	valueIs := func(want string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		var state string
		for time.Now().Before(deadline) {
			state, _, _ = runGetWindowState(t, window)
			if strings.Contains(state, "Value: "+want) {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Errorf("the input does not show %q:\n%s", want, state)
	}

	// The double click (mouse messages, no activation pair) focuses the
	// input; the typing then has a field to land in.
	step("double click on the input", []string{"click", window, input, "--count", "2"}, "ok (clicked as window messages)")
	step("type-text", []string{"type-text", window, "typed by messages"}, "ok (typed as window messages")
	valueIs("typed by messages")
	step("press-key Control_L+a", []string{"press-key", window, "Control_L+a"}, "ok (pressed as window messages")
	step("type-text over the selection", []string{"type-text", window, "replaced"}, "ok (typed as window messages")
	valueIs("replaced")
}

func TestChromiumMessagesOnACoveredBrowser(t *testing.T) {
	optIn(t)
	browsers := findChromiumBrowsers()
	if len(browsers) == 0 {
		t.Skip("no Chromium browser (Edge or Chrome) found in the standard install locations")
	}
	chromiumMessagesOn(t, browsers[0].exe, "--user-data-dir="+t.TempDir(), "--no-first-run", "--no-default-browser-check", "--new-window")
}

func TestChromiumMessagesOnACoveredElectron(t *testing.T) {
	optIn(t)
	exe := os.Getenv("WINUSE_E2E_ELECTRON")
	if exe == "" {
		t.Skip("WINUSE_E2E_ELECTRON is not set: point it at electron.exe from an Electron release to run this test")
	}
	chromiumMessagesOn(t, exe)
}
