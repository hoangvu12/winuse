//go:build e2e

// The cold Chromium settle test (ticket 18 of .scratch/headless-max): an
// Electron App has no Browser Connection, so its Window State comes from
// UI Automation alone, and a cold Chromium builds that tree lazily. Read
// cold while fully covered, get-window-state settles the tree for up to
// 5 s and then either shows the page or says the tree is still thin; it
// never uncovers or activates the Window to make it build.
//
// User-run only (see the package comment and test/e2e/README.md); an agent
// must never start it.
//
// The Electron App is the prebuilt Electron binary (electron.exe from an
// Electron release), named by WINUSE_E2E_ELECTRON; its default app opens
// the fixture page given on the command line. The test skips without it.

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

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// TestCoveredElectronReadCold starts Electron on the fixture page, covers
// its Window as soon as it exists, and reads its Window State once, cold.
// The read must answer within the request budget with either the page's
// web content or the thin-page note, never both, and the cover must still
// be in front afterwards.
func TestCoveredElectronReadCold(t *testing.T) {
	optIn(t)
	exe := os.Getenv("WINUSE_E2E_ELECTRON")
	if exe == "" {
		t.Skip("WINUSE_E2E_ELECTRON is not set: point it at electron.exe from an Electron release to run this test")
	}
	if fi, err := os.Stat(exe); err != nil || fi.IsDir() {
		t.Fatalf("WINUSE_E2E_ELECTRON=%q is not an executable file: %v", exe, err)
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
	if _, err := os.Stat(page); err != nil {
		t.Fatalf("the fixture page is missing: %v", err)
	}
	url := "file:///" + strings.ReplaceAll(filepath.ToSlash(page), " ", "%20")

	// An isolated profile, so a running Electron App's state is never
	// shared and the App starts cold.
	cmd := exec.Command(exe, "--user-data-dir="+t.TempDir(), url)
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting %s: %v", exe, err)
	}
	pid := uint32(cmd.Process.Pid)
	defer terminatePID(t, pid)

	// Cover the Window as soon as it exists, before it has shown anything.
	target := findWindowOf(t, pid, "Chrome_WidgetWin_1", "", 30*time.Second)
	cover := coverWindow(t, strconv.FormatUint(target, 10))
	coverOver(t, cover, target)

	start := time.Now()
	stdout, stderr, exit := runGetWindowState(t, strconv.FormatUint(target, 10))
	elapsed := time.Since(start)
	if exit != 0 {
		t.Fatalf("get-window-state exit %d, stderr:\n%s", exit, stderr)
	}
	if elapsed > protocol.RequestTimeout {
		t.Errorf("get-window-state took %s, more than the %s request budget", elapsed.Round(10*time.Millisecond), protocol.RequestTimeout)
	}
	if fg := win.ForegroundWindow(); fg != cover {
		t.Errorf("the read changed the foreground to %s; the cover %d must stay in front", describeWindow(t, fg), cover)
	}

	button := regexp.MustCompile(`(?m)^\t+\d+ button( \([^)]*\))? winuse e2e button\b`)
	built := button.MatchString(stdout)
	thin := strings.Contains(stdout, "Note: the page's accessibility tree is still thin")
	switch {
	case built && thin:
		t.Errorf("the Window State shows the page and also says it is thin:\n%s", stdout)
	case built:
		t.Logf("the covered Electron Window built its web tree within %s", elapsed.Round(10*time.Millisecond))
	case thin:
		t.Logf("the covered Electron Window stayed thin; the note said so after %s", elapsed.Round(10*time.Millisecond))
		if elapsed < 4*time.Second {
			t.Errorf("the thin note came after %s; the settle should have waited about 5 s", elapsed.Round(10*time.Millisecond))
		}
	default:
		t.Errorf("the Window State shows neither the page's button nor the thin-page note:\n%s", stdout)
	}
}
