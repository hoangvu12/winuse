//go:build e2e

// The occlusion flag test (ticket 04 of .scratch/headless-max): a browser
// winuse launches gets --disable-features=CalculateNativeWinOcclusion, so
// a Window covered from the moment it opens keeps rendering and builds its
// UIA tree. Without the flag, the Laptop probe saw a cold, fully covered
// Chrome keep a 36-node tree for 14 s; with it, the tree settled in about
// 0.1 s.
//
// User-run only (see the package comment and test/e2e/README.md); an agent
// must never start it.
//
// Each installed browser runs in winuse's own launch profile, started
// through the real `winuse browser launch`, so the user's own browser and
// profile are never driven. A browser whose launch profile is already
// running is skipped: a warm browser says nothing about a cold one.

package e2e

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/win"
)

// occludedSettle is how long a covered, freshly launched browser may take
// to show its frame and Page in the Window State: well under the 14 s an
// occluded Chrome needed, with room for a slow machine.
const occludedSettle = 5 * time.Second

// addressBarLine matches the browser frame's address bar in a Window State.
var addressBarLine = regexp.MustCompile(`(?m)^\t+\d+ edit( \([^)\n]*\))? [^\n]*[Aa]ddress`)

// TestLaunchedBrowserCoveredKeepsItsTree launches Chrome, then Edge, covers
// the new Window at once, opens the test page through winuse and checks
// that the Window State shows the frame (the address bar) and the Page
// within occludedSettle, with the cover still in front.
func TestLaunchedBrowserCoveredKeepsItsTree(t *testing.T) {
	optIn(t)
	if err := win.SetPerMonitorV2DPIAwareness(); err != nil {
		t.Logf("per-monitor DPI awareness failed (bounds may be scaled): %v", err)
	}
	sys := browser.Live()
	installed := installedBrowsers(sys)

	// `winuse browser launch` pins the browser it launches; the run puts
	// the pin and the foreground back as it found them.
	startFG := win.ForegroundWindow()
	pin := currentBrowserPin(t)
	t.Cleanup(func() {
		restoreBrowserPin(t, pin)
		restoreForeground(t, startFG)
	})
	srv := startPageServers(t)

	ran := 0
	for _, name := range []string{"chrome", "edge"} {
		b, ok := installed[name]
		if !ok {
			t.Logf("%s is not installed in a standard location; skipped", browser.DisplayName(name))
			continue
		}
		ran++
		t.Run(name, func(t *testing.T) { runCoveredLaunch(t, sys, b, srv) })
	}
	if ran == 0 {
		t.Skip("neither Chrome nor Edge is installed; nothing was run")
	}
}

func runCoveredLaunch(t *testing.T, sys *browser.System, b browser.Browser, srv *pageServers) {
	killHelper(t)
	t.Cleanup(func() { killHelper(t) }) // registered first: runs last

	p := &pageRun{name: b.Name, srv: srv}
	profile := sys.LaunchProfileDir(p.name)
	_, statErr := os.Stat(profile)
	profileExisted := statErr == nil

	stdout, stderr, exit := p.run(t, "browser", "launch", p.name)
	if exit != 0 {
		t.Fatalf("winuse browser launch %s failed (exit %d): %s", p.name, exit, stderr)
	}
	if strings.Contains(stdout, "already running") {
		t.Skipf("winuse's own %s profile is already running (%s); a warm browser is not a cold one. "+
			"Close it with winuse browser stop %s and run again", browser.DisplayName(p.name), strings.TrimSpace(stdout), p.name)
	}
	m := regexp.MustCompile(`launched: pid (\d+), port (\d+)`).FindStringSubmatch(stdout)
	if m == nil {
		t.Fatalf("unexpected launch output %q", stdout)
	}
	pid, _ := strconv.ParseUint(m[1], 10, 32)
	port, _ := strconv.Atoi(m[2])
	p.pid = uint32(pid)
	t.Cleanup(func() {
		(&browserProbe{sys: sys, pid: p.pid, port: port, rep: &probeReport{}}).closeBrowser(t, profile, profileExisted)
	})

	// Cover the Window as soon as it exists, before it has shown anything.
	first := findWindowOf(t, p.pid, "Chrome_WidgetWin_1", "", 30*time.Second)
	p.cover = coverWindow(t, strconv.FormatUint(first, 10))
	coverOver(t, p.cover, first)

	p.main = first
	p.window = strconv.FormatUint(first, 10)
	p.act(t, "navigate", p.window, srv.mainURL(false, ""))
	p.main = waitBrowserWindowTitled(t, p.pid, pagesTitle, 20*time.Second)
	p.window = strconv.FormatUint(p.main, 10)
	coverOver(t, p.cover, p.main)

	start := time.Now()
	var state string
	for {
		state = p.state(t)
		frame := addressBarLine.MatchString(state)
		_, page := findPageIndex(state, "", "e2e count")
		if frame && page {
			t.Logf("%s: the covered Window showed its frame and Page %s after the navigation", p.name, time.Since(start).Round(10*time.Millisecond))
			return
		}
		if time.Since(start) > occludedSettle {
			t.Fatalf("after %s covered, the Window State shows the address bar %v and the Page's \"e2e count\" button %v, want both:\n%s",
				occludedSettle, frame, page, state)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
