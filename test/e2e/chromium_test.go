//go:build e2e

// The ticket-14 seam-1 test: the Chromium path. A local HTML page opened in
// Edge (and Chrome, when installed) must expose its web content Elements in
// the Window State — links, buttons, inputs — even though Chromium enables
// its accessibility tree lazily, only when a UI Automation client first
// appears, so the first read of such a window is nearly empty and winuse's
// retry must surface the page's Elements (spec user story 24).
//
// User-run only (see the package comment and test/e2e/README.md); an agent
// must never start it, and never start Edge/Chrome outside it.

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// chromiumPageTitle is the fixture page's <title>: the browser Window's
// title, and the only unique handle the runner has on the Window.
const chromiumPageTitle = "winuse e2e page 14"

// chromiumBrowser is one installed Chromium-based browser to test against.
type chromiumBrowser struct {
	name string // subtest label: "edge", "chrome"
	exe  string // absolute executable path
	app  string // the executable file name, for the header's App line
}

// findChromiumBrowsers locates Edge and Chrome in their standard install
// locations. Anything missing is skipped (Edge ships with Windows; Chrome
// is optional).
func findChromiumBrowsers() []chromiumBrowser {
	roots := []string{
		os.Getenv("ProgramFiles"),
		os.Getenv("ProgramFiles(x86)"),
	}
	var browsers []chromiumBrowser
	for _, b := range []struct {
		name  string
		paths []string
	}{
		{"edge", []string{
			filepath.Join(roots[1], "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(roots[0], "Microsoft", "Edge", "Application", "msedge.exe"),
		}},
		{"chrome", []string{
			filepath.Join(roots[0], "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(roots[1], "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Google", "Chrome", "Application", "chrome.exe"),
		}},
	} {
		for _, p := range b.paths {
			if p == "" {
				continue
			}
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				browsers = append(browsers, chromiumBrowser{name: b.name, exe: p, app: filepath.Base(p)})
				break
			}
		}
	}
	return browsers
}

// TestChromiumWebContentInWindowState runs the Chromium fixture page through
// every installed Chromium browser: the Window State of the browser Window
// must expose the page's web content Elements. If accessibility has not
// switched on yet (the lazy tree), winuse's retry must make them appear
// within the same get-window-state call.
func TestChromiumWebContentInWindowState(t *testing.T) {
	optIn(t)
	browsers := findChromiumBrowsers()
	if len(browsers) == 0 {
		t.Skip("no Chromium browser (Edge or Chrome) found in the standard install locations")
	}
	for _, b := range browsers {
		b := b
		t.Run(b.name, func(t *testing.T) {
			chromiumWebContentInWindowState(t, b)
		})
	}
}

// chromiumWebContentInWindowState opens the fixture page in one browser and
// asserts the page's Elements appear in the Window State.
func chromiumWebContentInWindowState(t *testing.T, b chromiumBrowser) {
	t.Helper()

	// A helper left over from an earlier test may carry that test's
	// environment (a tiny WINUSE_TREE_BUDGET, say). Kill it so the helper
	// this test spawns inherits this test's environment. Process
	// management only: no input, no windows.
	killHelper(t)

	// An isolated profile in a temp dir: the run must not touch the user's
	// real browser profile (the Task Suite's scratch rule, applied early),
	// and a separate user-data-dir guarantees a fresh browser process even
	// when the user's own Edge/Chrome is running.
	profile := t.TempDir()
	page, err := filepath.Abs("testdata/chromium-fixture.html")
	if err != nil {
		t.Fatalf("resolving the fixture page: %v", err)
	}
	if _, err := os.Stat(page); err != nil {
		t.Fatalf("the fixture page is missing: %v", err)
	}
	url := "file:///" + strings.ReplaceAll(filepath.ToSlash(page), " ", "%20")

	cmd := exec.Command(b.exe,
		"--user-data-dir="+profile,
		"--no-first-run",
		"--no-default-browser-check",
		"--new-window", url,
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting %s: %v", b.exe, err)
	}
	// Cleanup order: kill the browser, then the temp profile (removed by
	// the test framework after this test's defers).
	defer terminatePID(t, uint32(cmd.Process.Pid))

	// Wait for the page's Window: poll list-windows (a read-only command)
	// until the fixture page's title shows up.
	windowID := findWindowByTitle(t, chromiumPageTitle, 30*time.Second)

	stdout, stderr, exit := runGetWindowState(t, strconv.FormatUint(windowID, 10))
	if exit != 0 {
		t.Fatalf("exit %d, stderr:\n%s", exit, stderr)
	}
	if stderr != "" {
		t.Errorf("unexpected stderr: %q", stderr)
	}

	// The header names the browser's executable.
	if !strings.Contains(stdout, ", App: "+b.app+".") {
		t.Errorf("header must name the App %q:\n%s", b.app, stdout)
	}

	// The page's web content Elements: links, buttons, inputs (spec user
	// story 24). Indexes and depth are not stable across browser versions,
	// so the lines are matched by role and name at any position.
	for _, w := range []struct{ role, name string }{
		{"link", "winuse e2e link"},
		{"button", "winuse e2e button"},
		{"edit", "winuse e2e input"},
		{"check box", "winuse e2e checkbox"},
	} {
		re := regexp.MustCompile(`(?m)^\t+\d+ ` + regexp.QuoteMeta(w.role) + ` ` + regexp.QuoteMeta(w.name) + `\b`)
		if !re.MatchString(stdout) {
			t.Errorf("Window State missing the %s %q Element line:\n%s", w.role, w.name, stdout)
		}
	}

	// Web content exposed: the sparse note must not appear.
	if strings.Contains(stdout, "nearly empty") {
		t.Errorf("the Window State says the tree is nearly empty; the web content did not appear (the lazy-accessibility retry failed):\n%s", stdout)
	}
}

// findWindowByTitle polls `winuse list-windows` (read-only) until a Window
// whose title contains title shows up, and returns its Window ID.
func findWindowByTitle(t *testing.T, title string, timeout time.Duration) uint64 {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		stdout, stderr, exit := runListWindows(t, "WINUSE_HELPER_IDLE="+e2eIdle)
		if exit == 0 {
			lastListWindowsOutput = stdout
			for _, line := range strings.Split(strings.TrimSuffix(stdout, "\n"), "\n") {
				parts := strings.SplitN(line, "\t", 3)
				if len(parts) != 3 || !strings.Contains(parts[2], title) {
					continue
				}
				id, err := strconv.ParseUint(parts[0], 10, 64)
				if err != nil {
					t.Fatalf("bad Window ID in list-windows line %q", line)
				}
				return id
			}
		} else if stderr != "" {
			t.Logf("list-windows: %s", stderr)
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("no Window titled %q appeared within %s; last list-windows output:\n%s", title, timeout, lastListWindowsOutput)
	return 0
}

// lastListWindowsOutput holds the most recent list-windows output for the
// timeout failure message.
var lastListWindowsOutput string
