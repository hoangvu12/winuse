//go:build e2e

// Headless Element Actions (ADR 0010) against real web content: the
// Chromium fixture page in an isolated-profile browser, covered by a
// fixture Window that holds the foreground. A Headless click on the page's
// checkbox and a Headless set-value on its input must take effect (winuse
// reads both back, so exit 0 proves the effect) while the covering Window
// stays the foreground window. It pins IUIAutomation2's AutoSetFocus being
// off on a real provider: with the default (on), UI Automation focuses the
// Element before every pattern call, which brings the browser forward.
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

	"github.com/hoangvu12/winuse/internal/win"
)

// webElementIndex finds the Element Index of the Window State line with the
// given role and name (labels share their inputs' names, so the role
// disambiguates).
func webElementIndex(t *testing.T, state, role, name string) int {
	t.Helper()
	re := regexp.MustCompile(`(?m)^\t+(\d+) ` + regexp.QuoteMeta(role) + `( \([^)]*\))? ` + regexp.QuoteMeta(name) + `\b`)
	m := re.FindStringSubmatch(state)
	if m == nil {
		t.Fatalf("Window State has no %s %q line:\n%s", role, name, state)
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

func TestHeadlessChromiumKeepsTheForeground(t *testing.T) {
	optIn(t)
	browsers := findChromiumBrowsers()
	if len(browsers) == 0 {
		t.Skip("no Chromium browser (Edge or Chrome) found in the standard install locations")
	}
	b := browsers[0]
	killHelper(t)

	profile := t.TempDir()
	page, err := filepath.Abs("testdata/chromium-fixture.html")
	if err != nil {
		t.Fatalf("resolving the fixture page: %v", err)
	}
	if _, err := os.Stat(page); err != nil {
		t.Fatalf("the fixture page is missing: %v", err)
	}
	url := "file:///" + strings.ReplaceAll(filepath.ToSlash(page), " ", "%20")
	cmd := exec.Command(b.exe, "--user-data-dir="+profile, "--no-first-run", "--no-default-browser-check", "--new-window", url)
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting %s: %v", b.exe, err)
	}
	defer terminatePID(t, uint32(cmd.Process.Pid))

	window := strconv.FormatUint(findWindowByTitle(t, chromiumPageTitle, 30*time.Second), 10)
	cover := coverWindow(t, window)

	state, stderr, exit := runGetWindowState(t, window)
	if exit != 0 {
		t.Fatalf("get-window-state: exit %d, stderr:\n%s", exit, stderr)
	}
	checkbox := webElementIndex(t, state, "check box", "winuse e2e checkbox")
	input := webElementIndex(t, state, "edit", "winuse e2e input")

	stdout, stderr, exit := runWinuse(t, []string{"click", window, strconv.Itoa(checkbox)}, "WINUSE_HELPER_IDLE="+e2eIdle)
	assertActionOK(t, "Headless click (web checkbox)", stdout, stderr, exit)
	if fg := win.ForegroundWindow(); fg != cover {
		t.Errorf("the foreground window is %s after a Headless click on web content, want the covering Window %d", describeWindow(t, fg), cover)
	}

	stdout, stderr, exit = runSetvalue(t, window, input, "set headless")
	assertActionOK(t, "Headless set-value (web input)", stdout, stderr, exit)
	if fg := win.ForegroundWindow(); fg != cover {
		t.Errorf("the foreground window is %s after a Headless set-value on web content, want the covering Window %d", describeWindow(t, fg), cover)
	}
}
