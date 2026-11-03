//go:build e2e

// The browser merge probe (ticket 03 of .scratch/browser-merge): it
// measures, against real Chrome and Edge, what tickets 04 to 07 need to
// know before they are built. It is a measurement, not an assertion: every
// finding is logged on a line starting "MEASURED:" and written to a report
// file, and the test fails only when its own setup breaks (no launch, no
// connection, no window).
//
// User-run only (see the package comment and test/e2e/README.md); an agent
// must never start it.
//
// Each browser runs in winuse's own launch profile, started through the
// real `winuse browser launch` (ticket 01), so the user's own browser and
// profile are never driven. The probe connects to that browser itself, over
// a CDP connection of its own, to make the raw calls it measures. The
// extension questions use the user's real browser, and only when the winuse
// extension is connected to it: they open one background tab there, switch
// to it and back, and close it.
//
// Run it at each display scale (100%, 150%, 175%); every run writes its own
// self-describing report (see probeReportPath).

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/hoangvu12/winuse/internal/bridge"
	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// probePageTitle is testdata/browser-probe.html's <title>.
const probePageTitle = "winuse browser probe"

// probeIDs are the probe page's Elements with known CSS positions, by DOM
// id, with the accessible name to fall back on when the browser does not
// expose the id as the AutomationId.
var probeIDs = []struct{ id, name string }{
	{"probe-link", "probe link"},
	{"probe-button", "probe button"},
	{"probe-input", "probe input"},
	{"probe-corner", "probe corner"},
}

// probeScaleReminder is printed at the start and written in every report.
const probeScaleReminder = "Run this probe once at each display scale: 100%, 150% and 175% " +
	"(Settings > System > Display > Scale), with no other change between runs. Each run writes its own report."

var (
	procProbeGetDpiForWindow = user32.NewProc("GetDpiForWindow")
	procProbeGetWindowRect   = user32.NewProc("GetWindowRect")
)

// TestBrowserProbe runs the probe against Chrome, then Edge. A browser that
// is not installed is skipped with a log line.
func TestBrowserProbe(t *testing.T) {
	optIn(t)
	if err := win.SetPerMonitorV2DPIAwareness(); err != nil {
		t.Logf("per-monitor DPI awareness failed (bounds may be scaled): %v", err)
	}
	t.Log(probeScaleReminder)

	sys := browser.Live()
	installed := map[string]browser.Browser{}
	for _, b := range sys.Env.Detect(sys.Exists) {
		installed[b.Name] = b
	}

	// The run must leave the pin and the foreground as it found them:
	// `winuse browser launch` pins the browser it launches.
	startFG := win.ForegroundWindow()
	pin := currentBrowserPin(t)
	t.Cleanup(func() {
		restoreBrowserPin(t, pin)
		restoreForeground(t, startFG)
	})

	ran := 0
	for _, name := range []string{"chrome", "edge"} {
		b, ok := installed[name]
		if !ok {
			t.Logf("%s is not installed in a standard location; skipped", browser.DisplayName(name))
			continue
		}
		ran++
		t.Run(name, func(t *testing.T) { probeBrowser(t, sys, b) })
	}
	if ran == 0 {
		t.Skip("neither Chrome nor Edge is installed; nothing was probed")
	}
}

// browserProbe is the state of one browser's probe run.
type browserProbe struct {
	name, path, app string
	sys             *browser.System
	rep             *probeReport

	pid      uint32 // the launched browser's main process
	port     int
	launched bool // false when winuse's launch profile was already running
	conn     *browser.Connection
	main     uint64 // the browser Window the probe page shows in
	target   string // the probe page's target
	sess     string // the probe page's session
	pageURL  string
	cover    uint64 // the fixture Window that covers main and holds the foreground
	ui       *uia.Client
	ext      *cdp.Client // the winuse extension in the user's browser, or nil
	extInfo  bridge.Info
	extState string
}

func probeBrowser(t *testing.T, sys *browser.System, b browser.Browser) {
	p := &browserProbe{name: b.Name, path: b.Path, app: filepath.Base(b.Path), sys: sys}
	p.rep = &probeReport{browser: b.Name, exe: b.Path, started: time.Now()}
	t.Cleanup(func() { p.rep.write(t) }) // registered first: runs after every other cleanup

	page, err := filepath.Abs("testdata/browser-probe.html")
	if err != nil {
		t.Fatalf("resolving the probe page: %v", err)
	}
	if _, err := os.Stat(page); err != nil {
		t.Fatalf("the probe page is missing: %v", err)
	}
	p.pageURL = "file:///" + strings.ReplaceAll(filepath.ToSlash(page), " ", "%20")

	// A helper left over from an earlier run may carry another environment;
	// the launch below goes through a fresh one.
	killHelper(t)
	p.ui = uia.New()
	t.Cleanup(p.ui.Close)

	p.launch(t)
	p.connect(t)
	p.openProbePage(t)
	p.findExtension(t)

	// The fixture covers the browser Window and holds the foreground for
	// every question: it is the user's "own" Window.
	p.cover = coverWindow(t, strconv.FormatUint(p.main, 10))
	p.coverFully(t)

	t.Run("1-window-to-tab", p.windowToTab)
	t.Run("2-foreground", p.foreground)
	t.Run("3-screen-coordinates", p.coordinates)
	t.Run("4-covered-and-minimized", p.coveredAndMinimized)
	t.Run("5-posted-keys-address-bar", p.postedKeys)
	t.Run("6-connection-risks", p.connectionRisks)
}

// ---- setup ----

// launch starts the browser through `winuse browser launch` and records
// whether that took the foreground.
func (p *browserProbe) launch(t *testing.T) {
	r := p.rep
	r.section(t, "Launch (winuse browser launch)")
	profile := p.sys.LaunchProfileDir(p.name)
	_, statErr := os.Stat(profile)
	profileExisted := statErr == nil

	fgBefore := win.ForegroundWindow()
	stdout, stderr, exit := runWinuse(t, []string{"browser", "launch", p.name}, "WINUSE_HELPER_IDLE="+e2eIdle)
	r.note(t, "`winuse browser launch %s`: exit %d, output %q, stderr %q", p.name, exit, strings.TrimSpace(stdout), strings.TrimSpace(stderr))
	if exit != 0 {
		t.Fatalf("winuse browser launch %s failed (exit %d): %s", p.name, exit, stderr)
	}
	if m := regexp.MustCompile(`launched: pid (\d+), port (\d+)`).FindStringSubmatch(stdout); m != nil {
		pid, _ := strconv.ParseUint(m[1], 10, 32)
		p.pid = uint32(pid)
		p.port, _ = strconv.Atoi(m[2])
		p.launched = true
	} else if m := regexp.MustCompile(`already running from winuse's \w+ profile \(port (\d+)\)`).FindStringSubmatch(stdout); m != nil {
		p.port, _ = strconv.Atoi(m[1])
		pid, ok := p.sys.ListenerPID(p.port)
		if !ok {
			t.Fatalf("no process listens on port %d", p.port)
		}
		p.pid = pid
		r.note(t, "winuse's launch profile was already running before the probe: the probe uses it and leaves it running")
	} else {
		t.Fatalf("unexpected launch output %q", stdout)
	}

	if p.launched {
		t.Cleanup(func() { p.closeBrowser(t, profile, profileExisted) })
	}
	p.main = findWindowOf(t, p.pid, "Chrome_WidgetWin_1", "", 30*time.Second)
	time.Sleep(1500 * time.Millisecond) // let a late activation land
	fgAfter := win.ForegroundWindow()
	took := win.WindowPID(fgAfter) == p.pid
	if !p.launched {
		r.measured(t, "launch took the foreground: not measured (the browser was already running)")
	} else {
		r.measured(t, "launch took the foreground: %v (foreground before %s, after %s)", yesNo(took), describeWindow(t, fgBefore), describeWindow(t, fgAfter))
	}
	r.note(t, "browser pid %d, port %d, main Window %s, DPI %d", p.pid, p.port, describeWindow(t, p.main), windowDPI(p.main))
	p.rep.dpi = windowDPI(p.main)
}

// connect checks the port's owner, reads /json/version while watching for
// a new window, and opens the probe's own CDP connection.
func (p *browserProbe) connect(t *testing.T) {
	r := p.rep
	r.section(t, "Connection (ticket 01 risks)")
	owner, ok := p.sys.ListenerPID(p.port)
	img, imgErr := "", error(nil)
	if ok {
		img, imgErr = p.sys.ProcessImage(owner)
	}
	r.measured(t, "port owner check: listener found %v, owner pid %d (launched pid %d, same %v), image %q (err %v), identified as %q (want %q)",
		yesNo(ok), owner, p.pid, yesNo(owner == p.pid), img, imgErr, p.sys.Env.IdentifyImage(img), p.name)

	before := windowIDs(browserWindows(t, p.pid))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ws, err := p.sys.VersionWS(ctx, p.port)
	if err != nil {
		t.Fatalf("/json/version on port %d: %v", p.port, err)
	}
	time.Sleep(time.Second)
	r.measured(t, "/json/version raised a new browser window (a prompt): %s", newWindowsText(t, before, browserWindows(t, p.pid)))

	before = windowIDs(browserWindows(t, p.pid))
	tr, err := cdp.DialWS(ctx, ws, 20*time.Second)
	if err != nil {
		t.Fatalf("dialing %s: %v", ws, err)
	}
	client := cdp.NewClient(tr)
	p.conn = &browser.Connection{Browser: p.name, Mode: browser.ModeLaunch, WSURL: ws, Port: p.port, PID: p.pid, Client: client}
	t.Cleanup(func() { client.Close() })
	time.Sleep(time.Second)
	r.measured(t, "the WebSocket dial raised a new browser window (a prompt): %s", newWindowsText(t, before, browserWindows(t, p.pid)))

	var v struct {
		Product   string `json:"product"`
		UserAgent string `json:"userAgent"`
	}
	if res, err := p.call("Browser.getVersion", nil); err == nil {
		json.Unmarshal(res, &v)
	}
	p.rep.product = v.Product
	r.note(t, "version: %s", v.Product)
}

// openProbePage navigates the launched browser's first tab to the probe
// page and attaches the probe's session to it (focus emulation on).
func (p *browserProbe) openProbePage(t *testing.T) {
	pages, err := listPages(p.conn.Client)
	if err != nil || len(pages) == 0 {
		t.Fatalf("listing the launched browser's tabs: %v (%d pages)", err, len(pages))
	}
	p.target = pages[0].TargetID
	for _, pg := range pages {
		if strings.Contains(pg.URL, "newtab") || pg.URL == "about:blank" {
			p.target = pg.TargetID
			break
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	p.sess, err = p.conn.AttachPage(ctx, p.target)
	if err != nil {
		t.Fatalf("attaching to the first tab: %v", err)
	}
	if _, err := p.callSession(p.sess, "Page.navigate", map[string]any{"url": p.pageURL}, 10*time.Second); err != nil {
		t.Fatalf("navigating to the probe page: %v", err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		var ready bool
		err := p.eval(p.sess, `document.readyState === "complete" && document.title === `+strconv.Quote(probePageTitle), &ready, 3*time.Second)
		if err == nil && ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the probe page did not load within 15 s (last error %v)", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	// The page is in the main Window's active tab: find that Window by
	// its title, which now starts with the page title.
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, w := range browserWindows(t, p.pid) {
			if strings.HasPrefix(w.Title, probePageTitle) {
				p.main = w.ID
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("no %s Window titled %q appeared", p.name, probePageTitle)
}

// findExtension asks `winuse browser ext status` whether the winuse
// extension is connected to this browser (the user's own browser), and if
// so opens a bridge connection of the probe's own.
func (p *browserProbe) findExtension(t *testing.T) {
	r := p.rep
	r.section(t, "Extension")
	if !userBrowserOptIn() {
		p.extState = "not opted in"
		r.measured(t, "extension skipped: its questions open a tab in the user's own %s; set WINUSE_E2E_USER_BROWSER=1 only when the user agreed to it", browser.DisplayName(p.name))
		return
	}
	stdout, stderr, exit := runWinuse(t, []string{"browser", "ext", "status", p.name}, "WINUSE_HELPER_IDLE="+e2eIdle)
	r.note(t, "`winuse browser ext status %s`: exit %d, output %q, stderr %q", p.name, exit, strings.TrimSpace(stdout), strings.TrimSpace(stderr))
	if exit != 0 {
		p.extState = "not connected"
		r.measured(t, "extension not connected, skipped. To measure the extension questions: run `winuse browser ext install %s`, "+
			"follow the printed steps in %s (Developer mode, Load unpacked), check with `winuse browser ext status %s` (exit 0), then run the probe again",
			p.name, browser.DisplayName(p.name), p.name)
		return
	}
	var hits []bridge.Info
	for _, i := range p.sys.Bridges() {
		if p.sys.Env.IdentifyImage(i.BrowserExe) == p.name {
			hits = append(hits, i)
		}
	}
	if len(hits) != 1 {
		p.extState = fmt.Sprintf("%d bridges", len(hits))
		r.measured(t, "extension skipped: %d bridges for %s (several profiles, or none listed)", len(hits), p.name)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := bridge.Dial(ctx, hits[0].Addr(), hits[0].Token)
	if err != nil {
		p.extState = "bridge dial failed"
		r.measured(t, "extension skipped: dialing its bridge failed: %v", err)
		return
	}
	p.ext = cdp.NewClient(conn)
	p.extInfo = hits[0]
	p.extState = fmt.Sprintf("connected (instance %s, version %s, browser pid %d)", hits[0].Instance, hits[0].Version, hits[0].BrowserPID)
	t.Cleanup(func() { p.ext.Close() })
	r.note(t, "extension %s; its windows belong to the user's own %s (pid %d), not the launched one", p.extState, browser.DisplayName(p.name), hits[0].BrowserPID)
}

// coverFully stretches the covering fixture over the whole browser Window
// (same top-left, same frame size), so "covered" means fully covered.
func (p *browserProbe) coverFully(t *testing.T) {
	fb, err := win.FrameBounds(p.main)
	if err != nil {
		t.Fatalf("browser frame bounds: %v", err)
	}
	if err := win.MoveNoActivate(p.cover, fb.Left, fb.Top); err != nil {
		t.Logf("moving the cover: %v", err)
	}
	if err := win.ResizeNoActivate(p.cover, fb.Width(), fb.Height()); err != nil {
		t.Logf("resizing the cover: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
}

// coverage says how the cover lies over the browser Window: its frame
// contains the browser's, it is above in z-order, and what the system
// hit-tests at the page centre.
func (p *browserProbe) coverage(t *testing.T) string {
	fb, err1 := win.FrameBounds(p.main)
	cb, err2 := win.FrameBounds(p.cover)
	if err1 != nil || err2 != nil {
		return fmt.Sprintf("unknown (%v, %v)", err1, err2)
	}
	contains := cb.Left <= fb.Left && cb.Top <= fb.Top && cb.Right >= fb.Right && cb.Bottom >= fb.Bottom
	s := takeSnap(t)
	hit := rootWindowAt((fb.Left+fb.Right)/2, (fb.Top+fb.Bottom)/2)
	who := "another Window"
	switch hit {
	case p.cover:
		who = "the cover"
	case p.main:
		who = "the browser"
	}
	return fmt.Sprintf("cover frame contains the browser frame %v, cover z %d, browser z %d, hit-test at the centre: %s",
		yesNo(contains), s.z(p.cover), s.z(p.main), who)
}

// closeBrowser closes the browser the probe launched (Browser.close over a
// fresh connection, then terminate if it lingers) and removes the launch
// profile when the probe created it.
func (p *browserProbe) closeBrowser(t *testing.T, profile string, profileExisted bool) {
	closed := false
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if ws, err := p.sys.VersionWS(ctx, p.port); err == nil {
		if tr, err := cdp.DialWS(ctx, ws, 5*time.Second); err == nil {
			c := cdp.NewClient(tr)
			c.Call(ctx, "Browser.close", nil)
			c.Close()
		}
	}
	if waitProcessExit(t, p.pid, 15*time.Second) {
		closed = true
	} else {
		terminatePID(t, p.pid)
		closed = waitProcessExit(t, p.pid, 5*time.Second)
	}
	p.rep.note(t, "cleanup: the launched browser (pid %d) closed: %v", p.pid, yesNo(closed))
	if profileExisted || !closed {
		return
	}
	var err error
	for i := 0; i < 20; i++ {
		if err = os.RemoveAll(profile); err == nil {
			p.rep.note(t, "cleanup: removed the launch profile the probe created (%s)", profile)
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	p.rep.note(t, "cleanup: could not remove the launch profile %s: %v", profile, err)
}

// ---- 1: Window to Tab mapping ----

func (p *browserProbe) windowToTab(t *testing.T) {
	r := p.rep
	r.section(t, "1. Window to Tab mapping")
	// Two more windows, plus a background tab whose title repeats window
	// 2's, so the title alone is ambiguous for window 2.
	var created []string
	t.Cleanup(func() {
		for _, id := range created {
			p.closeTarget(id)
		}
	})
	for _, title := range []string{"winuse probe window 2", "winuse probe window 3"} {
		known := windowIDs(browserWindows(t, p.pid))
		id, err := p.createTarget(dataPage(title), true)
		if err != nil {
			t.Fatalf("Target.createTarget newWindow: %v", err)
		}
		created = append(created, id)
		if w, ok := waitNewBrowserWindow(t, p.pid, known, 10*time.Second); ok {
			r.note(t, "created %q: Window %s", title, describeWindow(t, w.ID))
		} else {
			r.note(t, "created %q: no new visible browser Window within 10 s", title)
		}
	}
	if id, err := p.createTarget(dataPage("winuse probe window 2"), false); err == nil {
		created = append(created, id)
	} else {
		r.note(t, "creating the duplicate-title background tab failed: %v", err)
	}
	time.Sleep(time.Second) // titles settle

	pages, err := listPages(p.conn.Client)
	if err != nil {
		t.Fatalf("Target.getTargets: %v", err)
	}
	type tw struct {
		target probeTarget
		win    cdpWindow
		err    error
	}
	var tws []tw
	winRows := [][]string{}
	for _, pg := range pages {
		w, err := p.windowForTarget(pg.TargetID)
		tws = append(tws, tw{pg, w, err})
		if err != nil {
			winRows = append(winRows, []string{short(pg.TargetID), pg.Title, "error: " + err.Error(), "", ""})
			continue
		}
		b := w.Bounds
		winRows = append(winRows, []string{short(pg.TargetID), pg.Title, strconv.Itoa(w.WindowID),
			fmt.Sprintf("%d,%d %dx%d", b.Left, b.Top, b.Width, b.Height), b.WindowState})
	}
	r.table(t, "Browser.getWindowForTarget per page target", []string{"target", "title", "windowId", "bounds (left,top width x height)", "state"}, winRows)

	var matches, misses, ambiguous, boundsUnique, titleUnique int
	hwndRows := [][]string{}
	hwnds := browserWindows(t, p.pid)
	for _, h := range hwnds {
		dpi := windowDPI(h.ID)
		scale := float64(dpi) / 96
		wr := windowRect(h.ID)
		boundsWins := map[int]string{}
		for _, x := range tws {
			if x.err != nil {
				continue
			}
			if how := boundsMatch(x.win, scale, h.Bounds, wr); how != "" {
				boundsWins[x.win.WindowID] = how
			}
		}
		var titleTargets, both []string
		for _, x := range tws {
			if !titleMatches(h.Title, x.target.Title) {
				continue
			}
			titleTargets = append(titleTargets, short(x.target.TargetID))
			if x.err == nil {
				if _, ok := boundsWins[x.win.WindowID]; ok {
					both = append(both, short(x.target.TargetID))
				}
			}
		}
		result := "match"
		switch {
		case len(both) == 0:
			result = "miss"
			misses++
		case len(both) > 1:
			result = "ambiguous"
			ambiguous++
		default:
			matches++
		}
		if len(boundsWins) == 1 {
			boundsUnique++
		}
		if len(titleTargets) == 1 {
			titleUnique++
		}
		var bw []string
		for id, how := range boundsWins {
			bw = append(bw, fmt.Sprintf("%d (%s)", id, how))
		}
		sort.Strings(bw)
		hwndRows = append(hwndRows, []string{strconv.FormatUint(h.ID, 10), h.Title, strconv.Itoa(int(dpi)),
			rectText(h.Bounds), rectText(wr), strings.Join(bw, "; "), strings.Join(titleTargets, ", "), result})
	}
	r.table(t, "Each Chrome_WidgetWin_1 Window of the browser", []string{"HWND", "title", "DPI", "DWM frame", "GetWindowRect", "windowIds by bounds", "targets by title", "bounds+title"}, hwndRows)
	r.measured(t, "bounds plus title: %d match, %d miss, %d ambiguous of %d Windows; bounds alone unique for %d, title alone unique for %d",
		matches, misses, ambiguous, len(hwnds), boundsUnique, titleUnique)

	p.extensionWindows(t)
}

// extensionWindows records the extension's chrome.windows ids for the
// user's browser and whether the active tab's title picks one HWND each.
// Titles of the user's tabs are not written to the report, only lengths.
func (p *browserProbe) extensionWindows(t *testing.T) {
	r := p.rep
	if p.ext == nil {
		r.measured(t, "extension windowIds: extension not connected, skipped")
		return
	}
	first, err := listPages(p.ext)
	if err != nil {
		r.measured(t, "extension Target.getTargets failed: %v", err)
		return
	}
	time.Sleep(time.Second)
	second, _ := listPages(p.ext)
	stable := true
	byTarget := map[string]int{}
	for _, pg := range first {
		byTarget[pg.TargetID] = pg.WindowID
	}
	for _, pg := range second {
		if w, ok := byTarget[pg.TargetID]; ok && w != pg.WindowID {
			stable = false
		}
	}
	active := map[int]probeTarget{}
	tabs := map[int]int{}
	for _, pg := range first {
		tabs[pg.WindowID]++
		if pg.Active {
			active[pg.WindowID] = pg
		}
	}
	hwnds := browserWindows(t, uint32(p.extInfo.BrowserPID))
	var rows [][]string
	unique := 0
	for _, h := range hwnds {
		var ids []string
		for wid, pg := range active {
			if titleMatches(h.Title, pg.Title) {
				ids = append(ids, strconv.Itoa(wid))
			}
		}
		sort.Strings(ids)
		if len(ids) == 1 {
			unique++
		}
		rows = append(rows, []string{strconv.FormatUint(h.ID, 10), fmt.Sprintf("%d chars", len([]rune(h.Title))), strings.Join(ids, ", ")})
	}
	var wrows [][]string
	for wid, n := range tabs {
		a, ok := active[wid]
		wrows = append(wrows, []string{strconv.Itoa(wid), strconv.Itoa(n), fmt.Sprintf("%v, title %d chars", yesNo(ok), len([]rune(a.Title)))})
	}
	sort.Slice(wrows, func(i, j int) bool { return wrows[i][0] < wrows[j][0] })
	r.table(t, "Extension windowIds (the user's own browser)", []string{"windowId", "tabs", "active tab"}, wrows)
	r.table(t, "The user's browser Windows by active-tab title", []string{"HWND", "title", "windowIds whose active tab title matches"}, rows)
	r.measured(t, "extension: %d windowIds, %d HWNDs, %d HWNDs matched exactly one windowId by title; windowIds stable over 1 s: %v; the extension reports no bounds (chrome.windows is never called)",
		len(tabs), len(hwnds), unique, yesNo(stable))
}

// ---- 2: foreground safety ----

func (p *browserProbe) foreground(t *testing.T) {
	r := p.rep
	r.section(t, "2. Foreground safety")
	r.note(t, "the cover (fixture Window %d) holds the foreground; coverage: %s", p.cover, p.coverage(t))
	var created []string
	t.Cleanup(func() {
		for _, id := range created {
			p.closeTarget(id)
		}
	})
	watch := []uint64{p.cover, p.main}
	p.watchOp(t, "Target.createTarget {background:true}", p.pid, watch, func() (string, error) {
		id, err := p.createTarget(dataPage("winuse probe background tab"), false)
		if err == nil {
			created = append(created, id)
		}
		return "", err
	})
	p.watchOp(t, "Target.createTarget {newWindow:true, background:true}", p.pid, watch, func() (string, error) {
		id, err := p.createTarget(dataPage("winuse probe background window"), true)
		if err == nil {
			created = append(created, id)
		}
		return "", err
	})

	if p.ext == nil {
		r.measured(t, "extension tab create and activate: extension not connected, skipped")
		return
	}
	// The user's browser: a background tab, switch to it, switch back,
	// close it. The user sees their tab change for about two seconds.
	userWins := browserWindows(t, uint32(p.extInfo.BrowserPID))
	ewatch := []uint64{p.cover}
	for _, w := range userWins {
		ewatch = append(ewatch, w.ID)
	}
	var tabID string
	p.watchOp(t, "extension Target.createTarget (chrome.tabs.create active:false, grouped)", uint32(p.extInfo.BrowserPID), ewatch, func() (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		res, err := p.ext.Call(ctx, "Target.createTarget", map[string]any{"url": dataPage("winuse probe extension tab")})
		if err != nil {
			return "", err
		}
		var v struct {
			TargetID string `json:"targetId"`
		}
		json.Unmarshal(res, &v)
		tabID = v.TargetID
		return "target " + short(tabID), nil
	})
	if tabID == "" {
		return
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := p.ext.Call(ctx, "Target.closeTarget", map[string]any{"targetId": tabID}); err != nil {
			r.note(t, "closing the extension tab failed: %v", err)
		}
	}()
	pages, _ := listPages(p.ext)
	var mine probeTarget
	for _, pg := range pages {
		if pg.TargetID == tabID {
			mine = pg
		}
	}
	var previous string
	for _, pg := range pages {
		if pg.WindowID == mine.WindowID && pg.Active {
			previous = pg.TargetID
		}
	}
	activate := func(id string) func() (string, error) {
		return func() (string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, err := p.ext.Call(ctx, bridge.MethodActivateTab, map[string]any{"targetId": id})
			if err != nil {
				return "", err
			}
			pages, _ := listPages(p.ext)
			for _, pg := range pages {
				if pg.TargetID == id {
					return fmt.Sprintf("the tab is now active: %v", yesNo(pg.Active)), nil
				}
			}
			return "the tab is gone", nil
		}
	}
	p.watchOp(t, "extension Winuse.activateTab (chrome.tabs.update active:true) on the new tab", uint32(p.extInfo.BrowserPID), ewatch, activate(tabID))
	if previous != "" {
		p.watchOp(t, "extension Winuse.activateTab back to the user's tab", uint32(p.extInfo.BrowserPID), ewatch, activate(previous))
	} else {
		r.note(t, "no previously active tab found in window %d; the new tab is closed instead", mine.WindowID)
	}
	r.note(t, "not measured: tab activation while the user's window is minimized (the probe never minimizes the user's browser)")
}

// watchOp runs one operation and records the foreground, the z-order of
// the watched Windows and any new browser Window, before and after.
func (p *browserProbe) watchOp(t *testing.T, label string, pid uint32, watch []uint64, op func() (string, error)) {
	before := takeSnap(t)
	known := windowIDs(browserWindows(t, pid))
	detail, err := op()
	time.Sleep(1500 * time.Millisecond)
	after := takeSnap(t)
	var z []string
	for _, id := range watch {
		z = append(z, fmt.Sprintf("%d: %d -> %d", id, before.z(id), after.z(id)))
	}
	var fresh []string
	for _, w := range browserWindows(t, pid) {
		if !known[w.ID] {
			pos := "below the cover"
			if after.z(w.ID) < after.z(p.cover) {
				pos = "ABOVE the cover"
			}
			fresh = append(fresh, fmt.Sprintf("%s, minimized %v, %s", describeWindow(t, w.ID), yesNo(w.Minimized), pos))
		}
	}
	if len(fresh) == 0 {
		fresh = []string{"none"}
	}
	res := "ok"
	if err != nil {
		res = "error: " + err.Error()
	}
	if detail != "" {
		res += ", " + detail
	}
	p.rep.measured(t, "%s: %s; foreground unchanged: %v (before %s, after %s); z-order (topmost 0): %s; new browser Windows: %s",
		label, res, yesNo(before.fg == after.fg), describeWindow(t, before.fg), describeWindow(t, after.fg), strings.Join(z, ", "), strings.Join(fresh, "; "))
}

// ---- 3: screen coordinates ----

func (p *browserProbe) coordinates(t *testing.T) {
	r := p.rep
	r.section(t, "3. Screen coordinates")
	dpi := windowDPI(p.main)
	r.note(t, "monitor scale %d%% (DPI %d on the browser Window's monitor)", dpi*100/96, dpi)
	r.note(t, "screen = UIA document top-left + CSS rect x devicePixelRatio, compared with UIA BoundingRectangle of the same Element (AutomationId = DOM id)")
	p.measureCoords(t, "page zoom 100%")

	defer func() {
		// Back to 100% (Chrome remembers zoom per site), and the cover in
		// front again: the zoom keys were Headed on the browser Window.
		stdout, stderr, exit := runWinuse(t, []string{"--headed", "press-key", strconv.FormatUint(p.main, 10), "Control_L+0"}, "WINUSE_HELPER_IDLE="+e2eIdle)
		if exit != 0 {
			r.note(t, "resetting the zoom failed: exit %d, %q %q", exit, stdout, stderr)
		}
		time.Sleep(500 * time.Millisecond)
		r.note(t, "page zoom after reset: %.0f%%", p.pageZoom(t)*100)
		coverAgain(t, p.cover)
		p.coverFully(t)
	}()
	for _, want := range []float64{1.25, 1.5} {
		got := p.zoomTo(t, want)
		if got < want-0.02 || got > want+0.02 {
			r.note(t, "page zoom: wanted %.0f%%, reached %.0f%% (measured anyway)", want*100, got*100)
		}
		p.measureCoords(t, fmt.Sprintf("page zoom %.0f%%", got*100))
	}
}

// pageZoom is devicePixelRatio over the monitor scale: the browser zoom.
func (p *browserProbe) pageZoom(t *testing.T) float64 {
	var dpr float64
	if err := p.eval(p.sess, "window.devicePixelRatio", &dpr, 5*time.Second); err != nil {
		t.Logf("reading devicePixelRatio: %v", err)
		return 0
	}
	return dpr / (float64(windowDPI(p.main)) / 96)
}

// zoomTo presses Control+equal (Headed, on the probe's own browser
// Window) until the page zoom reaches want, and returns the zoom reached.
func (p *browserProbe) zoomTo(t *testing.T, want float64) float64 {
	z := p.pageZoom(t)
	for i := 0; i < 8 && z < want-0.02; i++ {
		stdout, stderr, exit := runWinuse(t, []string{"--headed", "press-key", strconv.FormatUint(p.main, 10), "Control_L+equal"}, "WINUSE_HELPER_IDLE="+e2eIdle)
		if exit != 0 {
			p.rep.note(t, "zoom key failed: exit %d, %q %q", exit, stdout, stderr)
			return z
		}
		time.Sleep(700 * time.Millisecond)
		z = p.pageZoom(t)
	}
	return z
}

// measureCoords records the CSS-to-screen error for each probe Element.
func (p *browserProbe) measureCoords(t *testing.T, label string) {
	r := p.rep
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tree, err := p.ui.Capture(ctx, p.main, "", p.app, 10*time.Second)
	if err != nil {
		r.measured(t, "%s: reading the UIA tree failed: %v", label, err)
		return
	}
	var page struct {
		DPR   float64               `json:"dpr"`
		IW    float64               `json:"iw"`
		IH    float64               `json:"ih"`
		VV    float64               `json:"vv"`
		Rects map[string][4]float64 `json:"rects"`
	}
	var ids []string
	for _, e := range probeIDs {
		ids = append(ids, strconv.Quote(e.id))
	}
	expr := `(() => { const out = {dpr: devicePixelRatio, iw: innerWidth, ih: innerHeight, vv: window.visualViewport ? visualViewport.scale : 0, rects: {}};
		for (const id of [` + strings.Join(ids, ",") + `]) { const r = document.getElementById(id).getBoundingClientRect(); out.rects[id] = [r.left, r.top, r.right, r.bottom]; }
		return out; })()`
	if err := p.eval(p.sess, expr, &page, 5*time.Second); err != nil {
		r.measured(t, "%s: reading the CSS rectangles failed: %v", label, err)
		return
	}
	scale := float64(windowDPI(p.main)) / 96

	var rows [][]string
	maxDPR, maxScale := 0.0, 0.0
	var docNote string
	for _, e := range probeIDs {
		idx, how := findProbeElement(tree.Elements, e.id, e.name)
		if idx < 0 {
			rows = append(rows, []string{e.id, "not in the UIA tree", "", "", "", ""})
			continue
		}
		doc := documentAbove(tree.Elements, idx)
		if doc < 0 {
			rows = append(rows, []string{e.id, "no document Element above it", "", "", "", ""})
			continue
		}
		dl, dt, dr, db, _, err := p.ui.ElementBounds(ctx, p.main, doc)
		if err != nil {
			rows = append(rows, []string{e.id, "document bounds: " + err.Error(), "", "", "", ""})
			continue
		}
		if docNote == "" {
			docNote = fmt.Sprintf("document Element %d,%d %dx%d physical; innerWidth %.0f x innerHeight %.0f CSS; document width / innerWidth = %.3f; devicePixelRatio %.3f; visualViewport.scale %.3f",
				dl, dt, dr-dl, db-dt, page.IW, page.IH, float64(dr-dl)/page.IW, page.DPR, page.VV)
		}
		l, tp, rt, bt, _, err := p.ui.ElementBounds(ctx, p.main, idx)
		if err != nil {
			rows = append(rows, []string{e.id, "bounds: " + err.Error(), "", "", "", ""})
			continue
		}
		css := page.Rects[e.id]
		truth := [4]float64{float64(l), float64(tp), float64(rt), float64(bt)}
		viaDPR := [4]float64{float64(dl) + css[0]*page.DPR, float64(dt) + css[1]*page.DPR, float64(dl) + css[2]*page.DPR, float64(dt) + css[3]*page.DPR}
		viaScale := [4]float64{float64(dl) + css[0]*scale, float64(dt) + css[1]*scale, float64(dl) + css[2]*scale, float64(dt) + css[3]*scale}
		eDPR, eScale := maxEdgeError(viaDPR, truth), maxEdgeError(viaScale, truth)
		if eDPR > maxDPR {
			maxDPR = eDPR
		}
		if eScale > maxScale {
			maxScale = eScale
		}
		rows = append(rows, []string{e.id + " (" + how + ")",
			fmt.Sprintf("%.1f,%.1f,%.1f,%.1f", css[0], css[1], css[2], css[3]),
			fmt.Sprintf("%d,%d,%d,%d", l, tp, rt, bt),
			fmt.Sprintf("%.1f,%.1f,%.1f,%.1f", viaDPR[0], viaDPR[1], viaDPR[2], viaDPR[3]),
			fmt.Sprintf("%.1f", eDPR), fmt.Sprintf("%.1f", eScale)})
	}
	if docNote != "" {
		r.note(t, "%s: %s", label, docNote)
	}
	r.table(t, label+": CSS rect (left,top,right,bottom) against UIA", []string{"Element", "CSS", "UIA physical", "predicted via dpr", "max error px (dpr)", "max error px (monitor scale only)"}, rows)
	verdict := "within 2 px"
	if maxDPR > 2 {
		verdict = "OVER 2 px"
	}
	r.measured(t, "%s, display scale %d%%: max error %.1f px via devicePixelRatio (%s), %.1f px via the monitor scale alone",
		label, int(scale*100+0.5), maxDPR, verdict, maxScale)
}

// findProbeElement finds a probe Element in the UIA tree by AutomationId,
// else by name.
func findProbeElement(els []wstate.Element, id, name string) (int, string) {
	for i, e := range els {
		if e.AutomationId == id {
			return i, "by AutomationId"
		}
	}
	for i, e := range els {
		if e.Name == name && e.Role != "text" {
			return i, "by name; AutomationId missing"
		}
	}
	return -1, ""
}

// documentAbove is the nearest document Element that contains idx in the
// depth-first list.
func documentAbove(els []wstate.Element, idx int) int {
	depth := els[idx].Depth
	for i := idx - 1; i >= 0; i-- {
		if els[i].Depth < depth {
			if els[i].Role == wstate.RoleName(wstate.ControlTypeDocument) {
				return i
			}
			depth = els[i].Depth
		}
	}
	return -1
}

func maxEdgeError(a, b [4]float64) float64 {
	m := 0.0
	for i := range a {
		d := a[i] - b[i]
		if d < 0 {
			d = -d
		}
		if d > m {
			m = d
		}
	}
	return m
}

// ---- 4: covered and minimized ----

func (p *browserProbe) coveredAndMinimized(t *testing.T) {
	r := p.rep
	r.section(t, "4. Covered and minimized windows (focus emulation on)")
	fb, err := win.FrameBounds(p.main)
	if err != nil {
		t.Fatalf("browser frame bounds: %v", err)
	}
	var rows [][]string
	run := func(label string) {
		rows = append(rows, p.pageInputCase(t, label))
	}

	// Uncovered: the cover moves aside (it keeps the foreground).
	if err := win.MoveNoActivate(p.cover, fb.Right+40, fb.Top); err != nil {
		r.note(t, "moving the cover aside failed: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	r.note(t, "uncovered: %s", p.coverage(t))
	run("uncovered (not foreground)")

	p.coverFully(t)
	r.note(t, "covered: %s", p.coverage(t))
	run("fully covered")

	if err := win.MinimizeNoActivate(p.main); err != nil {
		r.note(t, "minimizing the browser failed: %v", err)
	}
	time.Sleep(time.Second)
	r.note(t, "minimized: IsIconic %v", yesNo(win.IsMinimized(p.main)))
	run("minimized")
	if err := win.RestoreNoActivate(p.main); err != nil {
		r.note(t, "restoring the browser failed: %v", err)
	}
	time.Sleep(time.Second)
	p.coverFully(t)

	r.table(t, "CDP on the probe page per case", []string{"case", "visibilityState, hasFocus", "getFullAXTree nodes", "Input.insertText", "Input.dispatchMouseEvent click", "foreground unchanged"}, rows)
	for _, row := range rows {
		r.measured(t, "%s: AX %s; insertText %s; click %s; foreground unchanged %s", row[0], row[2], row[3], row[4], row[5])
	}
}

// pageInputCase runs the AX read, insertText and a click on the probe page
// and returns one table row.
func (p *browserProbe) pageInputCase(t *testing.T, label string) []string {
	fgBefore := win.ForegroundWindow()
	var vis string
	if err := p.eval(p.sess, `document.visibilityState + ", " + document.hasFocus()`, &vis, 5*time.Second); err != nil {
		vis = "error: " + err.Error()
	}

	ax := "fail"
	if res, err := p.callSession(p.sess, "Accessibility.getFullAXTree", map[string]any{}, 10*time.Second); err != nil {
		ax = "fail: " + err.Error()
	} else {
		var v struct {
			Nodes []json.RawMessage `json:"nodes"`
		}
		json.Unmarshal(res, &v)
		ax = fmt.Sprintf("pass, %d nodes", len(v.Nodes))
	}

	want := "probe " + strings.Fields(label)[0]
	insert := "fail"
	var focused bool
	if err := p.eval(p.sess, `(() => { const e = document.getElementById('probe-input'); e.focus(); e.value = ''; return document.activeElement === e; })()`, &focused, 5*time.Second); err != nil {
		insert = "fail: focusing: " + err.Error()
	} else if _, err := p.callSession(p.sess, "Input.insertText", map[string]any{"text": want}, 5*time.Second); err != nil {
		insert = "fail: " + err.Error()
	} else {
		var got string
		p.eval(p.sess, `document.getElementById('probe-input').value`, &got, 5*time.Second)
		if got == want {
			insert = "pass"
		} else {
			insert = fmt.Sprintf("fail: value %q, want %q (DOM focus %v)", got, want, focused)
		}
	}

	click := "fail"
	var c struct{ X, Y float64 }
	var before, after int
	if err := p.eval(p.sess, `(() => { const b = document.getElementById('probe-button').getBoundingClientRect(); return {X: b.left + b.width / 2, Y: b.top + b.height / 2}; })()`, &c, 5*time.Second); err != nil {
		click = "fail: " + err.Error()
	} else {
		p.eval(p.sess, `Number(document.getElementById('probe-button').dataset.count || 0)`, &before, 5*time.Second)
		var sendErr error
		for _, typ := range []string{"mouseMoved", "mousePressed", "mouseReleased"} {
			params := map[string]any{"type": typ, "x": c.X, "y": c.Y, "button": "none"}
			if typ != "mouseMoved" {
				params["button"] = "left"
				params["clickCount"] = 1
			}
			if _, err := p.callSession(p.sess, "Input.dispatchMouseEvent", params, 5*time.Second); err != nil {
				sendErr = fmt.Errorf("%s: %w", typ, err)
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
		p.eval(p.sess, `Number(document.getElementById('probe-button').dataset.count || 0)`, &after, 5*time.Second)
		switch {
		case sendErr != nil:
			click = "fail: " + sendErr.Error()
		case after == before+1:
			click = "pass"
		default:
			click = fmt.Sprintf("fail: count %d -> %d", before, after)
		}
	}
	fgAfter := win.ForegroundWindow()
	fg := yesNo(fgBefore == fgAfter)
	if fgBefore != fgAfter {
		fg += fmt.Sprintf(" (UNEXPECTED: %s -> %s)", describeWindow(t, fgBefore), describeWindow(t, fgAfter))
	}
	return []string{label, vis, ax, insert, click, fg}
}

// ---- 5: posted keys in the address bar ----

func (p *browserProbe) postedKeys(t *testing.T) {
	r := p.rep
	r.section(t, "5. Posted keys in the address bar (information only)")
	known := windowIDs(browserWindows(t, p.pid))
	target, err := p.createTarget("about:blank", true)
	if err != nil {
		r.measured(t, "creating the about:blank window failed: %v", err)
		return
	}
	t.Cleanup(func() { p.closeTarget(target) })
	w, ok := waitNewBrowserWindow(t, p.pid, known, 10*time.Second)
	if !ok {
		r.measured(t, "no new browser Window appeared for the about:blank target; nothing measured")
		return
	}
	window := strconv.FormatUint(w.ID, 10)
	k := win.ProbeKeyTarget(w.ID)
	r.note(t, "posting into Window %s; key route probe: focus class %q (style 0x%x, enabled %v)", describeWindow(t, w.ID), k.Focus.Class, k.Focus.Style, k.Focus.Enabled)

	navigated := func(marker string) (bool, string) {
		deadline := time.Now().Add(5 * time.Second)
		var u string
		for time.Now().Before(deadline) {
			u = p.targetURL(target)
			if strings.Contains(u, marker) {
				return true, u
			}
			time.Sleep(200 * time.Millisecond)
		}
		return false, u
	}
	step := func(what string, args []string) {
		fg := win.ForegroundWindow()
		stdout, stderr, exit := runWinuse(t, args, "WINUSE_HELPER_IDLE="+e2eIdle)
		r.note(t, "%s: exit %d, output %q, stderr %q, foreground unchanged %v", what, exit, strings.TrimSpace(stdout), strings.TrimSpace(stderr), yesNo(win.ForegroundWindow() == fg))
	}

	// A: everything posted (the text, then Return), into whatever has the
	// focus in a background-created window.
	urlA := p.pageURL + "?posted=a"
	step("type-text --post (the URL)", []string{"type-text", window, "--post", urlA})
	r.note(t, "address bar value after the posted text: %q", p.addressBarValue(t, w.ID))
	step("press-key Return --post", []string{"press-key", window, "Return", "--post"})
	ok, u := navigated("posted=a")
	r.measured(t, "posted text plus posted Return: navigated %v (tab URL %q)", yesNo(ok), u)

	// B: set-value on the address bar (UIA), then a posted Return.
	state, stderr, exit := runGetWindowState(t, window)
	if exit != 0 {
		r.measured(t, "set-value plus posted Return: get-window-state failed (exit %d, %q); not measured", exit, stderr)
		return
	}
	m := regexp.MustCompile(`(?m)^\t+(\d+) edit( \([^)]*\))? [^\n]*[Aa]ddress`).FindStringSubmatch(state)
	if m == nil {
		r.measured(t, "set-value plus posted Return: no address bar Element in the Window State; not measured")
		return
	}
	urlB := p.pageURL + "?posted=b"
	step("set-value (address bar)", []string{"set-value", window, m[1], urlB})
	step("press-key Return --post", []string{"press-key", window, "Return", "--post"})
	ok, u = navigated("posted=b")
	r.measured(t, "set-value plus posted Return: navigated %v (tab URL %q)", yesNo(ok), u)
}

// addressBarValue reads the address bar edit's value through UIA.
func (p *browserProbe) addressBarValue(t *testing.T, hwnd uint64) string {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tree, err := p.ui.Capture(ctx, hwnd, "", p.app, 8*time.Second)
	if err != nil {
		return "error: " + err.Error()
	}
	for _, e := range tree.Elements {
		if e.Role == "edit" && strings.Contains(strings.ToLower(e.Name), "address") {
			return e.Value
		}
	}
	return "(no address bar Element)"
}

// ---- 6: connection risks ----

func (p *browserProbe) connectionRisks(t *testing.T) {
	r := p.rep
	r.section(t, "6. Connection lifetime (ticket 01 risks)")
	p.conn.Client.Close()
	time.Sleep(time.Second)
	r.measured(t, "after closing the probe's CDP connection: browser alive %v, port answering %v", yesNo(pidAlive(p.pid)), yesNo(p.sys.PortAlive(p.port)))
	killHelper(t)
	time.Sleep(2 * time.Second)
	r.measured(t, "after the winuse helper exited (it started the browser): browser alive %v (CREATE_BREAKAWAY_FROM_JOB held), port answering %v", yesNo(pidAlive(p.pid)), yesNo(p.sys.PortAlive(p.port)))
	r.note(t, "not measured here: the Chrome 144+ approval prompt (it belongs to `browser connect` on the user's real profile), clone, `--restore-last-session`, and the extension's child sessions, debugging bar and self-update")
}

// ---- CDP helpers ----

// probeTarget is one page target; the extension adds tabId, windowId and
// active.
type probeTarget struct {
	TargetID string `json:"targetId"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	TabID    int    `json:"tabId"`
	WindowID int    `json:"windowId"`
	Active   bool   `json:"active"`
}

func listPages(c *cdp.Client) ([]probeTarget, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := c.Call(ctx, "Target.getTargets", nil)
	if err != nil {
		return nil, err
	}
	var v struct {
		TargetInfos []probeTarget `json:"targetInfos"`
	}
	if err := json.Unmarshal(res, &v); err != nil {
		return nil, err
	}
	var out []probeTarget
	for _, x := range v.TargetInfos {
		if x.Type == "page" && !strings.HasPrefix(x.URL, "devtools://") && !strings.HasPrefix(x.URL, "chrome-extension://") {
			out = append(out, x)
		}
	}
	return out, nil
}

// cdpWindow is Browser.getWindowForTarget's answer.
type cdpWindow struct {
	WindowID int `json:"windowId"`
	Bounds   struct {
		Left        int    `json:"left"`
		Top         int    `json:"top"`
		Width       int    `json:"width"`
		Height      int    `json:"height"`
		WindowState string `json:"windowState"`
	} `json:"bounds"`
}

func (p *browserProbe) windowForTarget(id string) (cdpWindow, error) {
	var w cdpWindow
	res, err := p.call("Browser.getWindowForTarget", map[string]any{"targetId": id})
	if err != nil {
		return w, err
	}
	return w, json.Unmarshal(res, &w)
}

func (p *browserProbe) call(method string, params any) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return p.conn.Client.Call(ctx, method, params)
}

func (p *browserProbe) callSession(sess, method string, params any, d time.Duration) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return p.conn.Client.CallSession(ctx, sess, method, params)
}

// eval runs a JS expression in the page and decodes its value into out.
func (p *browserProbe) eval(sess, expr string, out any, d time.Duration) error {
	res, err := p.callSession(sess, "Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true, "awaitPromise": true}, d)
	if err != nil {
		return err
	}
	var v struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(res, &v); err != nil {
		return err
	}
	if v.ExceptionDetails != nil {
		return errors.New("page exception: " + v.ExceptionDetails.Text)
	}
	if len(v.Result.Value) == 0 {
		return errors.New("no value")
	}
	return json.Unmarshal(v.Result.Value, out)
}

// createTarget opens a background tab, or a background window.
func (p *browserProbe) createTarget(u string, newWindow bool) (string, error) {
	params := map[string]any{"url": u, "background": true}
	if newWindow {
		params["newWindow"] = true
	}
	res, err := p.call("Target.createTarget", params)
	if err != nil {
		return "", err
	}
	var v struct {
		TargetID string `json:"targetId"`
	}
	if err := json.Unmarshal(res, &v); err != nil || v.TargetID == "" {
		return "", fmt.Errorf("no target id in %s", res)
	}
	return v.TargetID, nil
}

// closeTarget closes a target the probe opened, over a fresh connection
// when the probe's own was closed already.
func (p *browserProbe) closeTarget(id string) {
	if p.conn.Client.Alive() {
		p.call("Target.closeTarget", map[string]any{"targetId": id})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if ws, err := p.sys.VersionWS(ctx, p.port); err == nil {
		if tr, err := cdp.DialWS(ctx, ws, 5*time.Second); err == nil {
			c := cdp.NewClient(tr)
			c.Call(ctx, "Target.closeTarget", map[string]any{"targetId": id})
			c.Close()
		}
	}
}

func (p *browserProbe) targetURL(id string) string {
	res, err := p.call("Target.getTargetInfo", map[string]any{"targetId": id})
	if err != nil {
		return "error: " + err.Error()
	}
	var v struct {
		TargetInfo probeTarget `json:"targetInfo"`
	}
	json.Unmarshal(res, &v)
	return v.TargetInfo.URL
}

// dataPage is a data: URL page whose title is title.
func dataPage(title string) string {
	return "data:text/html," + url.PathEscape("<title>"+title+"</title><p>"+title+"</p>")
}

// ---- desktop helpers ----

// browserWindows lists the visible, titled Chrome_WidgetWin_1 Windows of a
// browser process, topmost first.
func browserWindows(t *testing.T, pid uint32) []win.WindowInfo {
	t.Helper()
	all, err := win.Collect()
	if err != nil {
		t.Fatalf("enumerating windows: %v", err)
	}
	var out []win.WindowInfo
	for _, w := range all {
		if w.PID == pid && w.Class == "Chrome_WidgetWin_1" && w.Visible && !w.Cloaked && w.Title != "" {
			out = append(out, w)
		}
	}
	return out
}

func windowIDs(ws []win.WindowInfo) map[uint64]bool {
	m := map[uint64]bool{}
	for _, w := range ws {
		m[w.ID] = true
	}
	return m
}

func waitNewBrowserWindow(t *testing.T, pid uint32, known map[uint64]bool, timeout time.Duration) (win.WindowInfo, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, w := range browserWindows(t, pid) {
			if !known[w.ID] {
				return w, true
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return win.WindowInfo{}, false
}

func newWindowsText(t *testing.T, before map[uint64]bool, after []win.WindowInfo) string {
	var fresh []string
	for _, w := range after {
		if !before[w.ID] {
			fresh = append(fresh, describeWindow(t, w.ID))
		}
	}
	if len(fresh) == 0 {
		return "no"
	}
	return "YES: " + strings.Join(fresh, "; ")
}

// deskSnap is the foreground and the z-order of the visible windows.
type deskSnap struct {
	fg    uint64
	order []uint64
}

func takeSnap(t *testing.T) deskSnap {
	all, err := win.Collect()
	if err != nil {
		t.Fatalf("enumerating windows: %v", err)
	}
	s := deskSnap{fg: win.ForegroundWindow()}
	for _, w := range all {
		if w.Visible && !w.Cloaked {
			s.order = append(s.order, w.ID)
		}
	}
	return s
}

// z is a window's position in z-order among the visible windows, topmost
// 0, or -1.
func (s deskSnap) z(id uint64) int {
	for i, w := range s.order {
		if w == id {
			return i
		}
	}
	return -1
}

func windowDPI(hwnd uint64) uint32 {
	if procProbeGetDpiForWindow.Find() != nil {
		return 96
	}
	r, _, _ := procProbeGetDpiForWindow.Call(uintptr(hwnd))
	return uint32(r)
}

func windowRect(hwnd uint64) win.Rect {
	var r win.Rect
	procProbeGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
	return r
}

func rectText(r win.Rect) string {
	return fmt.Sprintf("%d,%d %dx%d", r.Left, r.Top, r.Width(), r.Height())
}

// boundsMatch says how a CDP window's bounds match an HWND, within 8 px
// on every edge: scaled from DIPs or raw, against the DWM frame or the
// window rect. "" when nothing matches.
func boundsMatch(w cdpWindow, scale float64, dwm, wr win.Rect) string {
	b := w.Bounds
	for _, s := range []struct {
		label string
		f     float64
	}{{"DIP x scale", scale}, {"raw", 1}} {
		got := [4]float64{float64(b.Left) * s.f, float64(b.Top) * s.f, float64(b.Left+b.Width) * s.f, float64(b.Top+b.Height) * s.f}
		for _, h := range []struct {
			label string
			r     win.Rect
		}{{"DWM frame", dwm}, {"window rect", wr}} {
			want := [4]float64{float64(h.r.Left), float64(h.r.Top), float64(h.r.Right), float64(h.r.Bottom)}
			if maxEdgeError(got, want) <= 8 {
				return fmt.Sprintf("%s vs %s, off %.0f px", s.label, h.label, maxEdgeError(got, want))
			}
		}
	}
	return ""
}

// titleMatches reports whether a browser Window title shows a tab title:
// Chromium titles a Window "<tab title> - <browser>" (Edge adds the
// profile and "and N more pages").
func titleMatches(windowTitle, tabTitle string) bool {
	if tabTitle == "" {
		return false
	}
	return windowTitle == tabTitle || strings.HasPrefix(windowTitle, tabTitle+" ")
}

func pidAlive(pid uint32) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// currentBrowserPin reads the pin through `winuse browser use`: a browser
// name, or "auto".
func currentBrowserPin(t *testing.T) string {
	stdout, _, exit := runWinuse(t, []string{"browser", "use"}, "WINUSE_HELPER_IDLE="+e2eIdle)
	if exit != 0 {
		return ""
	}
	first, _, _ := strings.Cut(stdout, "\n")
	name := strings.Fields(strings.TrimPrefix(first, "browser: "))
	if len(name) == 0 {
		return ""
	}
	return name[0]
}

func restoreBrowserPin(t *testing.T, pin string) {
	if pin == "" {
		return
	}
	if now := currentBrowserPin(t); now == pin {
		return
	}
	stdout, stderr, exit := runWinuse(t, []string{"browser", "use", pin}, "WINUSE_HELPER_IDLE="+e2eIdle)
	t.Logf("restored the browser pin to %s: exit %d, %q %q", pin, exit, strings.TrimSpace(stdout), strings.TrimSpace(stderr))
}

// restoreForeground gives the foreground back to the Window that had it
// when the run started, if that Window still exists and the foreground is
// not already there.
func restoreForeground(t *testing.T, fg uint64) {
	if fg == 0 || !win.WindowOpen(fg) || win.ForegroundWindow() == fg {
		return
	}
	stdout, stderr, exit := runActivateWindow(t, strconv.FormatUint(fg, 10))
	t.Logf("gave the foreground back to %s: exit %d, %q %q", describeWindow(t, fg), exit, strings.TrimSpace(stdout), strings.TrimSpace(stderr))
}

// ---- the report ----

// probeReport collects one browser's findings for the report file.
type probeReport struct {
	browser, exe, product string
	dpi                   uint32
	started               time.Time
	body                  strings.Builder
}

func (r *probeReport) section(t *testing.T, title string) {
	t.Logf("== %s ==", title)
	fmt.Fprintf(&r.body, "\n## %s\n\n", title)
}

func (r *probeReport) note(t *testing.T, format string, args ...any) {
	s := fmt.Sprintf(format, args...)
	t.Log(s)
	r.body.WriteString("- " + s + "\n")
}

// measured is a finding: logged with the "MEASURED:" prefix the suite uses
// for measurements, and in bold in the report.
func (r *probeReport) measured(t *testing.T, format string, args ...any) {
	s := fmt.Sprintf(format, args...)
	t.Log("MEASURED: " + s)
	r.body.WriteString("- **Measured:** " + s + "\n")
}

func (r *probeReport) table(t *testing.T, caption string, header []string, rows [][]string) {
	cell := func(s string) string { return strings.ReplaceAll(s, "|", `\|`) }
	var b strings.Builder
	fmt.Fprintf(&b, "\n%s:\n\n| %s |\n|", caption, strings.Join(header, " | "))
	for range header {
		b.WriteString("---|")
	}
	b.WriteString("\n")
	for _, row := range rows {
		var cells []string
		for _, c := range row {
			cells = append(cells, cell(c))
		}
		fmt.Fprintf(&b, "| %s |\n", strings.Join(cells, " | "))
	}
	t.Log(b.String())
	r.body.WriteString(b.String() + "\n")
}

func (r *probeReport) scaleLabel() string {
	if r.dpi == 0 {
		return "unknown"
	}
	return fmt.Sprintf("%dpct", r.dpi*100/96)
}

// write saves the report (see probeReportPath) and logs its path.
func (r *probeReport) write(t *testing.T) {
	path, err := probeReportPath(r.browser, r.scaleLabel(), r.started)
	if err != nil {
		t.Logf("no report written: %v", err)
		return
	}
	v := windows.RtlGetVersion()
	var b strings.Builder
	fmt.Fprintf(&b, "# winuse browser probe: %s at %s display scale\n\n", browser.DisplayName(r.browser), strings.TrimSuffix(r.scaleLabel(), "pct")+"%")
	fmt.Fprintf(&b, "- Browser: %s (%s), %s\n", r.browser, r.product, r.exe)
	fmt.Fprintf(&b, "- Display scale: %s (DPI %d on the browser Window's monitor)\n", strings.TrimSuffix(r.scaleLabel(), "pct")+"%", r.dpi)
	fmt.Fprintf(&b, "- Date: %s\n", r.started.Format("2006-01-02 15:04:05 -0700"))
	fmt.Fprintf(&b, "- Windows: %d.%d.%d\n", v.MajorVersion, v.MinorVersion, v.BuildNumber)
	fmt.Fprintf(&b, "- Ticket: .scratch/browser-merge/issues/03-probe-unknowns.md; copy the results into .scratch/browser-merge/probe-results.md\n")
	fmt.Fprintf(&b, "- %s\n", probeScaleReminder)
	b.WriteString(r.body.String())
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Logf("no report written: %v", err)
		return
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Logf("no report written: %v", err)
		return
	}
	t.Logf("probe report written to %s", path)
}

// probeReportPath is where a run's report goes. WINUSE_PROBE_REPORT may
// name a folder, or a .md file (the browser name is added before the
// extension, since one run probes two browsers). By default the report
// goes to .scratch/browser-merge/probe-run-<browser>-<scale>-<time>.md at
// the repo root.
func probeReportPath(browserName, scale string, at time.Time) (string, error) {
	name := fmt.Sprintf("probe-run-%s-%s-%s.md", browserName, scale, at.Format("20060102-150405"))
	v := os.Getenv("WINUSE_PROBE_REPORT")
	if v == "" {
		root, err := filepath.Abs(filepath.Join("..", ".."))
		if err != nil {
			return "", err
		}
		return filepath.Join(root, ".scratch", "browser-merge", name), nil
	}
	if ext := filepath.Ext(v); strings.EqualFold(ext, ".md") {
		return strings.TrimSuffix(v, ext) + "-" + browserName + ext, nil
	}
	return filepath.Join(v, name), nil
}
