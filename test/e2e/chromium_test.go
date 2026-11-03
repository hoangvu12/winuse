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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/win"
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

// startIsolatedBrowser starts a Chromium browser the way every e2e test
// that starts one itself must: on a throwaway profile in a temp dir, so the
// run never touches the user's real profile (the Task Suite's scratch rule)
// and, a separate user-data-dir being a separate browser process, the launch
// is never handed to the user's running Edge or Chrome. Edge signs a fresh
// profile in to the Windows account on its own (the msImplicitSignin
// feature), turns on sync of that account's data and puts a modal "Sync
// your profile" dialog over the page (the 0693039 live run on the laptop),
// so implicit sign-in and sync are off. args are added flags and the URL; a
// --disable-features among them joins the one switch, and a profile flag is
// refused. It returns the browser's pid; a cleanup terminates it.
func startIsolatedBrowser(t *testing.T, exe string, args ...string) uint32 {
	t.Helper()
	features := []string{"msImplicitSignin"}
	var rest []string
	for _, a := range args {
		if f, ok := strings.CutPrefix(a, "--disable-features="); ok {
			features = append(features, strings.Split(f, ",")...)
			continue
		}
		if strings.HasPrefix(a, "--user-data-dir") || strings.HasPrefix(a, "--profile-directory") {
			t.Fatalf("startIsolatedBrowser picks the profile itself; got %q", a)
		}
		rest = append(rest, a)
	}
	flags := append([]string{
		"--user-data-dir=" + t.TempDir(),
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-sync",
		"--disable-features=" + strings.Join(features, ","),
	}, rest...)
	cmd := exec.Command(exe, flags...)
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting %s: %v", exe, err)
	}
	pid := uint32(cmd.Process.Pid)
	// Cleanup order: kill the browser, then the temp profile (registered
	// first, so removed after).
	t.Cleanup(func() {
		if pidAlive(pid) {
			terminatePID(t, pid)
		}
	})
	return pid
}

// findIsolatedWindow waits for a visible browser Window of the process pid
// (startIsolatedBrowser) whose title contains title. A Window with that
// title in any other process never counts: had the launch been handed to
// another browser, the test fails here, before anything acts on it.
func findIsolatedWindow(t *testing.T, pid uint32, title string, timeout time.Duration) uint64 {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var others []string
	for time.Now().Before(deadline) {
		all, err := win.Collect()
		if err != nil {
			t.Fatalf("enumerating windows: %v", err)
		}
		others = others[:0]
		for _, w := range all {
			if !w.Visible || w.Class != win.ChromiumClass || !strings.Contains(w.Title, title) {
				continue
			}
			if w.PID == pid {
				return w.ID
			}
			others = append(others, fmt.Sprintf("%d %q (pid %d)", w.ID, w.Title, w.PID))
		}
		if !pidAlive(pid) {
			t.Fatalf("the browser this test started (pid %d) exited before showing %q; Windows with that title in other processes: %v. Nothing was acted on", pid, title, others)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("no Window titled %q of the browser this test started (pid %d) appeared within %s; Windows with that title in other processes: %v", title, pid, timeout, others)
	return 0
}

// signInSigns are what Edge's implicit sign-in put in the Window State at
// 0693039: the "Sync your profile" dialog over the page, its
// edge://sync-confirmation/ document, and the Window disabled under it.
var signInSigns = []*regexp.Regexp{
	regexp.MustCompile(`Sync your profile`),
	regexp.MustCompile(`sync-confirmation`),
	regexp.MustCompile(`signed you in|has signed in on this device|syncing your browsing data`),
	regexp.MustCompile(`(?m)^\t0 window \([^)]*disabled`),
}

// TestIsolatedEdgeStaysSignedOut is the one-launch check of
// startIsolatedBrowser on Edge: one Edge on a temp profile, the fixture
// page, and the Window State read every half second for 10 s (the dialog
// came about 2 s after launch at 0693039). Any sign of the implicit
// sign-in fails it. The browser is then terminated by its pid and the
// profile removed with the temp dir.
func TestIsolatedEdgeStaysSignedOut(t *testing.T) {
	optIn(t)
	var edge *chromiumBrowser
	for _, b := range findChromiumBrowsers() {
		if b.name == "edge" {
			b := b
			edge = &b
		}
	}
	if edge == nil {
		t.Skip("Edge is not installed in a standard location")
	}
	killHelper(t)

	pid := startIsolatedBrowser(t, edge.exe, "--new-window", chromiumFixtureURL(t))
	id := findIsolatedWindow(t, pid, chromiumPageTitle, 30*time.Second)
	window := strconv.FormatUint(id, 10)

	deadline := time.Now().Add(10 * time.Second)
	reads := 0
	for time.Now().Before(deadline) {
		state, stderr, exit := runGetWindowState(t, window)
		if exit != 0 {
			t.Fatalf("get-window-state: exit %d, stderr:\n%s", exit, stderr)
		}
		reads++
		for _, re := range signInSigns {
			if re.MatchString(state) {
				t.Fatalf("Edge (pid %d) shows its implicit sign-in (%q) after %d reads:\n%s", pid, re.String(), reads, state)
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	w, _ := windowByID(t, id)
	t.Logf("Edge (pid %d) Window %d, title %q: no sign-in in %d reads over 10 s", pid, id, w.Title, reads)

	terminatePID(t, pid)
	for end := time.Now().Add(5 * time.Second); pidAlive(pid) && time.Now().Before(end); {
		time.Sleep(100 * time.Millisecond)
	}
	if pidAlive(pid) {
		t.Errorf("Edge (pid %d) still runs 5 s after it was terminated", pid)
	}
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

	page, err := filepath.Abs("testdata/chromium-fixture.html")
	if err != nil {
		t.Fatalf("resolving the fixture page: %v", err)
	}
	if _, err := os.Stat(page); err != nil {
		t.Fatalf("the fixture page is missing: %v", err)
	}
	url := "file:///" + strings.ReplaceAll(filepath.ToSlash(page), " ", "%20")

	pid := startIsolatedBrowser(t, b.exe, "--new-window", url)
	windowID := findIsolatedWindow(t, pid, chromiumPageTitle, 30*time.Second)

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
	// so the lines are matched by role and name at any position; the
	// optional parenthesised state list (ticket 07's rich state — e.g. an
	// HTML input renders as `edit (settable, string)`) sits between the
	// role and the name.
	for _, w := range []struct{ role, name string }{
		{"link", "winuse e2e link"},
		{"button", "winuse e2e button"},
		{"edit", "winuse e2e input"},
		{"check box", "winuse e2e checkbox"},
	} {
		re := regexp.MustCompile(`(?m)^\t+\d+ ` + regexp.QuoteMeta(w.role) + `( \([^)]*\))? ` + regexp.QuoteMeta(w.name) + `\b`)
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
