//go:build e2e

// launch-app --cdp (headless-max 15): an Electron App started with a
// loopback debugging port gets a Browser Connection, so its covered
// Window's State shows the Page read over CDP, and a set-value on a Page
// field is Headless and reads back through eval. A WebView2 App does the
// same through WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS. The foreground stays
// with the cover throughout.
//
// User-run only (see the package comment and test/e2e/README.md); an agent
// must never start them. The Electron App is the prebuilt electron.exe
// named by WINUSE_E2E_ELECTRON, whose default app opens the fixture page
// given on its command line. The WebView2 App is any executable that
// hosts a WebView2 control, named by WINUSE_E2E_WEBVIEW2. Each test skips
// without its variable.

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

	"github.com/hoangvu12/winuse/internal/win"
)

// cdpNote is the start of launch-app --cdp's note when a Browser
// Connection reaches the App.
const cdpNote = "Note: a Browser Connection reaches the App's pages"

// launchCDP runs launch-app --cdp and returns the Window it reported and
// the whole output. The App is ended when the test ends.
func launchCDP(t *testing.T, exe string, args ...string) (uint64, string) {
	t.Helper()
	cmd := []string{"launch-app", exe, "--cdp"}
	for _, a := range args {
		cmd = append(cmd, "--arg", a)
	}
	stdout, stderr, exit := runWinuse(t, cmd, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		t.Fatalf("launch-app --cdp %s: exit %d, stderr:\n%s", exe, exit, stderr)
	}
	first, _, _ := strings.Cut(stdout, "\n")
	idField, _, _ := strings.Cut(first, "\t")
	id, err := strconv.ParseUint(idField, 10, 64)
	if err != nil {
		t.Fatalf("launch-app --cdp printed %q, want a Window line first", stdout)
	}
	if pid := win.WindowPID(id); pid != 0 {
		t.Cleanup(func() { terminatePID(t, pid) })
	}
	return id, stdout
}

func TestLaunchAppCDPOnCoveredElectron(t *testing.T) {
	optIn(t)
	exe := os.Getenv("WINUSE_E2E_ELECTRON")
	if exe == "" {
		t.Skip("WINUSE_E2E_ELECTRON is not set: point it at electron.exe from an Electron release to run this test")
	}
	if err := win.SetPerMonitorV2DPIAwareness(); err != nil {
		t.Logf("per-monitor DPI awareness failed (bounds may be scaled): %v", err)
	}
	killHelper(t)
	t.Cleanup(func() { killHelper(t) }) // registered first: runs last
	startFG := win.ForegroundWindow()
	t.Cleanup(func() { restoreForeground(t, startFG) })

	page, err := filepath.Abs("testdata/chromium-fixture.html")
	if err != nil {
		t.Fatalf("resolving the fixture page: %v", err)
	}
	url := "file:///" + strings.ReplaceAll(filepath.ToSlash(page), " ", "%20")
	id, out := launchCDP(t, exe, "--user-data-dir="+t.TempDir(), url)
	if !strings.Contains(out, cdpNote+" (Electron App, port ") {
		t.Fatalf("launch-app --cdp output %q: want the Browser Connection note", out)
	}
	window := strconv.FormatUint(id, 10)
	cover := coverWindow(t, window)
	coverOver(t, cover, id)

	state, stderr, exit := runGetWindowState(t, window)
	if exit != 0 {
		t.Fatalf("get-window-state: exit %d, stderr:\n%s", exit, stderr)
	}
	field := regexp.MustCompile(`(?m)^\t+(\d+) edit\b.*winuse e2e input`)
	m := field.FindStringSubmatch(state)
	if m == nil || strings.Contains(state, "Note: the Page is read through UI Automation") {
		t.Fatalf("the Window State does not show the Page read over CDP:\n%s", state)
	}

	stdout, stderr, exit := runWinuse(t, []string{"set-value", window, m[1], "typed over CDP"}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 || !strings.HasPrefix(stdout, "ok") {
		t.Fatalf("set-value on the Page field: exit %d, stdout %q, stderr %q", exit, stdout, stderr)
	}
	value, stderr, exit := runWinuse(t, []string{"eval", window, "document.getElementById('e2e-input').value"}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 || strings.TrimSpace(value) != "typed over CDP" {
		t.Errorf("eval read %q (exit %d, stderr %q), want the value set", value, exit, stderr)
	}
	if fg := win.ForegroundWindow(); fg != cover {
		t.Errorf("the foreground is %s; the cover %d must stay in front", describeWindow(t, fg), cover)
	}
}

func TestLaunchAppCDPOnWebView2(t *testing.T) {
	optIn(t)
	exe := os.Getenv("WINUSE_E2E_WEBVIEW2")
	if exe == "" {
		t.Skip("WINUSE_E2E_WEBVIEW2 is not set: point it at an executable that hosts a WebView2 control to run this test")
	}
	killHelper(t)
	t.Cleanup(func() { killHelper(t) })
	startFG := win.ForegroundWindow()
	t.Cleanup(func() { restoreForeground(t, startFG) })

	id, out := launchCDP(t, exe)
	if !strings.Contains(out, cdpNote+" (WebView2 App, port ") {
		t.Fatalf("launch-app --cdp output %q: want the Browser Connection note", out)
	}
	window := strconv.FormatUint(id, 10)
	cover := coverWindow(t, window)
	state, stderr, exit := runGetWindowState(t, window)
	if exit != 0 {
		t.Fatalf("get-window-state: exit %d, stderr:\n%s", exit, stderr)
	}
	if strings.Contains(state, "Note: the Page is read through UI Automation") {
		t.Errorf("the WebView2 Window's Page was not read over CDP:\n%s", state)
	}
	t.Logf("the WebView2 App's Window State:\n%s", state)
	if fg := win.ForegroundWindow(); fg != cover {
		t.Errorf("the foreground is %s; the cover %d must stay in front", describeWindow(t, fg), cover)
	}
}

// TestAttachToRunningElectron starts Electron the plain way (no debugging
// port), covers it, and attaches with winuse browser attach: the Window
// State then reads the Page over CDP, a set-value is read back through
// eval, and browser detach ends it (eval fails again). The foreground stays
// with the cover.
func TestAttachToRunningElectron(t *testing.T) {
	optIn(t)
	exe := os.Getenv("WINUSE_E2E_ELECTRON")
	if exe == "" {
		t.Skip("WINUSE_E2E_ELECTRON is not set: point it at electron.exe from an Electron release to run this test")
	}
	if err := win.SetPerMonitorV2DPIAwareness(); err != nil {
		t.Logf("per-monitor DPI awareness failed (bounds may be scaled): %v", err)
	}
	killHelper(t)
	t.Cleanup(func() { killHelper(t) })
	startFG := win.ForegroundWindow()
	t.Cleanup(func() { restoreForeground(t, startFG) })

	page, err := filepath.Abs("testdata/chromium-fixture.html")
	if err != nil {
		t.Fatalf("resolving the fixture page: %v", err)
	}
	url := "file:///" + strings.ReplaceAll(filepath.ToSlash(page), " ", "%20")
	cmd := exec.Command(exe, "--user-data-dir="+t.TempDir(), "--disable-features=CalculateNativeWinOcclusion", url)
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting %s: %v", exe, err)
	}
	pid := uint32(cmd.Process.Pid)
	defer terminatePID(t, pid)
	id := findWindowOf(t, pid, "Chrome_WidgetWin_1", "", 30*time.Second)
	window := strconv.FormatUint(id, 10)
	cover := coverWindow(t, window)
	coverOver(t, cover, id)

	run := func(args ...string) (string, string, int) {
		return runWinuse(t, args, "WINUSE_HELPER_IDLE="+e2eIdle)
	}
	out, stderr, exit := run("browser", "attach", window)
	if exit != 0 || !strings.HasPrefix(out, "ok\nNote: attached to ") {
		t.Fatalf("browser attach: exit %d, stdout %q, stderr %q", exit, out, stderr)
	}
	state, stderr, exit := runGetWindowState(t, window)
	if exit != 0 {
		t.Fatalf("get-window-state: exit %d, stderr:\n%s", exit, stderr)
	}
	m := regexp.MustCompile(`(?m)^\t+(\d+) edit\b.*winuse e2e input`).FindStringSubmatch(state)
	if m == nil || strings.Contains(state, "Note: the Page is read through UI Automation") {
		t.Fatalf("the Window State does not show the Page read over CDP:\n%s", state)
	}
	if out, stderr, exit := run("set-value", window, m[1], "typed through the attach"); exit != 0 || !strings.HasPrefix(out, "ok") {
		t.Fatalf("set-value: exit %d, stdout %q, stderr %q", exit, out, stderr)
	}
	if v, stderr, exit := run("eval", window, "document.getElementById('e2e-input').value"); exit != 0 || strings.TrimSpace(v) != "typed through the attach" {
		t.Errorf("eval read %q (exit %d, stderr %q)", v, exit, stderr)
	}
	if out, stderr, exit := run("browser", "detach", window); exit != 0 || !strings.HasPrefix(out, "ok\nNote: detached") {
		t.Errorf("browser detach: exit %d, stdout %q, stderr %q", exit, out, stderr)
	}
	if _, _, exit := run("eval", window, "1"); exit == 0 {
		t.Error("eval still reached the App after the detach")
	}
	if fg := win.ForegroundWindow(); fg != cover {
		t.Errorf("the foreground is %s; the cover %d must stay in front", describeWindow(t, fg), cover)
	}
}
