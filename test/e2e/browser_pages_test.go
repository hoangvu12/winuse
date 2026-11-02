//go:build e2e

// The browser page tests (ticket 09 of .scratch/browser-merge): every Page
// Action against real Chrome and Edge, driven only through the winuse CLI
// as an agent would drive it, with a fixture Window covering the browser
// and holding the foreground. After each Action the test checks that the
// foreground is still the fixture, that the fixture kept its place in the
// z-order, that no browser Window rose above it and that no native dialog
// Window appeared, then reads the Page back with `winuse eval` to check
// the Action took effect.
//
// User-run only (see the package comment and test/e2e/README.md); an agent
// must never start it.
//
// TestBrowserPages runs each installed browser in winuse's own launch
// profile, started through the real `winuse browser launch` (as the probe
// in browser_probe_test.go does), so the user's own browser and profile
// are never driven. The pages (testdata/browser-pages*.html) are served by
// two loopback httptest servers: the second one is addressed as
// "localhost", another site than 127.0.0.1, so its iframe is cross-origin
// and out of process.
//
// TestBrowserPagesExtension runs a short subset against the user's real
// browser through the winuse extension, only when `winuse browser ext
// status <browser>` exits 0. It opens one background tab there, switches
// to it, acts on it, switches back to the user's tab and closes its own.

package e2e

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/bridge"
	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/win"
)

const (
	// pagesTitle and pagesNextTitle are the <title>s of
	// testdata/browser-pages.html and browser-pages-next.html.
	pagesTitle     = "winuse e2e page"
	pagesNextTitle = "winuse e2e next"

	// pagesIdle keeps one helper, and so one Browser Connection, for a
	// whole browser's run: a dialog the helper saw open, a held file
	// chooser and the download tracking live in that helper. Each run kills
	// the helper at its end.
	pagesIdle = "10m"

	// pagesSettle is how long the test waits after an Action before it
	// checks the desktop, so a late activation is seen.
	pagesSettle = 600 * time.Millisecond

	// downloadPrefix starts the name of every file the download step asks
	// for; the server refuses other names.
	downloadPrefix = "winuse-e2e-download-"
	downloadBody   = "winuse e2e download\n"
	uploadName     = "winuse-e2e-upload.txt"
)

// TestBrowserPages runs every Page Action against Chrome, then Edge, each
// launched by winuse. A browser that is not installed is skipped with a
// log line.
func TestBrowserPages(t *testing.T) {
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
		t.Run(name, func(t *testing.T) { runBrowserPages(t, sys, b, srv) })
	}
	if ran == 0 {
		t.Skip("neither Chrome nor Edge is installed; nothing was run")
	}
}

func runBrowserPages(t *testing.T, sys *browser.System, b browser.Browser, srv *pageServers) {
	// A fresh helper, so the long idle applies and the launch below is the
	// connection every Page Action uses.
	killHelper(t)
	t.Cleanup(func() { killHelper(t) }) // registered first: runs last

	p := &pageRun{name: b.Name, srv: srv, dl: fmt.Sprintf("%s%s-%d.txt", downloadPrefix, b.Name, time.Now().UnixNano())}
	p.launch(t, sys)
	p.cover = coverWindow(t, p.window)
	coverOver(t, p.cover, p.main)

	t.Run("page-actions", p.pageActions)
	t.Run("drag-and-pointer-capture", p.dragAndCapture)
	t.Run("dialogs", p.dialogs)
	t.Run("cross-origin-iframe", p.crossOriginIframe)
	t.Run("upload", p.upload)
	t.Run("download", p.download)
	t.Run("navigation-wait-eval", p.navigation)
	t.Run("tabs", p.tabs)
}

// TestBrowserPagesExtension runs a short subset through the winuse
// extension in the user's real Chrome and Edge, in a background tab the
// test opens and closes. It skips a browser whose extension is not
// connected, with a log line saying how to connect it.
func TestBrowserPagesExtension(t *testing.T) {
	optIn(t)
	if err := win.SetPerMonitorV2DPIAwareness(); err != nil {
		t.Logf("per-monitor DPI awareness failed (bounds may be scaled): %v", err)
	}
	sys := browser.Live()
	installed := installedBrowsers(sys)
	startFG := win.ForegroundWindow()
	pin := currentBrowserPin(t)
	t.Cleanup(func() {
		restoreBrowserPin(t, pin)
		restoreForeground(t, startFG)
	})
	srv := startPageServers(t)

	for _, name := range []string{"chrome", "edge"} {
		if _, ok := installed[name]; !ok {
			t.Logf("%s is not installed in a standard location; skipped", browser.DisplayName(name))
			continue
		}
		t.Run(name, func(t *testing.T) { runExtensionPages(t, sys, name, srv) })
	}
}

func runExtensionPages(t *testing.T, sys *browser.System, name string, srv *pageServers) {
	killHelper(t)
	t.Cleanup(func() { killHelper(t) })
	p := &pageRun{name: name, srv: srv}

	stdout, stderr, exit := p.run(t, "browser", "ext", "status", name)
	if exit != 0 {
		t.Skipf("the winuse extension is not connected to %s (exit %d: %s %s); skipped. To include it: run winuse browser ext install %s, "+
			"follow the printed steps, and check that winuse browser ext status %s exits 0",
			browser.DisplayName(name), exit, strings.TrimSpace(stdout), strings.TrimSpace(stderr), name, name)
	}
	var hits []bridge.Info
	for _, i := range sys.Bridges() {
		if sys.Env.IdentifyImage(i.BrowserExe) == name {
			hits = append(hits, i)
		}
	}
	if len(hits) != 1 {
		t.Skipf("%d extension bridges for %s (several profiles, or none listed); skipped", len(hits), browser.DisplayName(name))
	}
	p.pid = uint32(hits[0].BrowserPID)
	wins := browserWindows(t, p.pid)
	if len(wins) == 0 {
		t.Skipf("the user's %s (pid %d) shows no titled Window; skipped", browser.DisplayName(name), p.pid)
	}
	p.main = wins[0].ID // topmost
	p.window = strconv.FormatUint(p.main, 10)
	t.Logf("acting in the user's %s Window %s through the extension", browser.DisplayName(name), describeWindow(t, p.main))

	p.cover = coverWindow(t, p.window)
	coverOver(t, p.cover, p.main)

	_, userTab := p.tabList(t)
	if userTab == "" {
		t.Fatalf("tab list of Window %s shows no active tab", p.window)
	}
	out := p.act(t, "tab", "new", p.window, srv.mainURL(false, ""))
	tab := strings.TrimSpace(strings.TrimPrefix(out, "ok "))
	if tab == "" || strings.ContainsAny(tab, " \n") {
		t.Fatalf("tab new printed %q, want ok <id>", out)
	}
	closed := false
	t.Cleanup(func() {
		if closed {
			return
		}
		// Back to the user's tab first, so closing ours never makes the
		// browser pick some other tab of theirs.
		if _, active := p.tabList(t); active == tab {
			p.run(t, "tab", "switch", p.window, userTab)
		}
		stdout, stderr, exit := p.run(t, "tab", "close", p.window, tab)
		t.Logf("cleanup: closed tab %s: exit %d, %q %q", tab, exit, strings.TrimSpace(stdout), strings.TrimSpace(stderr))
	})

	p.actOK(t, "tab", "switch", p.window, tab) // Headless through the extension
	if _, active := p.tabList(t); active != tab {
		t.Fatalf("tab %s is not the active tab after tab switch", tab)
	}
	p.actOK(t, "wait", p.window, "load")
	st := p.state(t)
	count := pageIndex(t, st, "button", "e2e count")
	text := pageIndex(t, st, "edit", "e2e text")

	p.actOK(t, "click", p.window, count)
	p.expectEval(t, "document.getElementById('count').dataset.count", "1")
	p.actOK(t, "set-value", p.window, text, "through the extension")
	p.expectEval(t, "document.getElementById('text').value", "through the extension")

	p.actOK(t, "tab", "switch", p.window, userTab)
	p.actOK(t, "tab", "close", p.window, tab)
	closed = true
	ids, active := p.tabList(t)
	if active != userTab {
		t.Errorf("the active tab is %q after switching back, want the user's %q", active, userTab)
	}
	for _, id := range ids {
		if id == tab {
			t.Errorf("tab %s is still listed after tab close", tab)
		}
	}
}

// ---- the subtests ----

// pageActions: click (single and double), set-value on a text field, a
// select and a check box, type-text and press-key with a chord, scroll by
// steps and into view, and hover.
func (p *pageRun) pageActions(t *testing.T) {
	st := p.open(t, p.srv.mainURL(false, ""), "")
	count := pageIndex(t, st, "button", "e2e count")
	text := pageIndex(t, st, "edit", "e2e text")
	fruit := pageIndex(t, st, "", "e2e fruit")
	agree := pageIndex(t, st, "check box", "e2e agree")
	hover := pageIndex(t, st, "button", "e2e hover")
	first := pageIndex(t, st, "list item", "e2e row 1")
	last := pageIndex(t, st, "list item", "e2e row 30")

	p.actOK(t, "click", p.window, count)
	p.expectEval(t, "document.getElementById('count').dataset.count", "1")
	p.actOK(t, "click", p.window, count, "--count", "2")
	p.expectEval(t, "document.getElementById('count').dataset.count", "3")

	p.actOK(t, "set-value", p.window, text, "e2e value")
	p.expectEval(t, "document.getElementById('text').value", "e2e value")
	p.actOK(t, "set-value", p.window, fruit, "Banana")
	p.expectEval(t, "document.getElementById('fruit').value", "banana")
	p.actOK(t, "set-value", p.window, agree, "on")
	p.expectEval(t, "String(document.getElementById('agree').checked)", "true")
	p.actOK(t, "set-value", p.window, agree, "off")
	p.expectEval(t, "String(document.getElementById('agree').checked)", "false")

	// Keys go to the Page while it holds the focus: the click focuses the
	// field, Control+a selects its text and the typing replaces it.
	p.actOK(t, "click", p.window, text)
	p.actOK(t, "press-key", p.window, "Control_L+a")
	p.actOK(t, "type-text", p.window, "typed text")
	p.expectEval(t, "document.getElementById('text').value", "typed text")

	// select-text (headless-max ticket 10): a span, read back by the
	// field's own selection, and the typing that replaces it; then all.
	p.actOK(t, "select-text", p.window, text, "6", "4")
	p.expectEval(t, "(e => e.selectionStart + ',' + e.selectionEnd)(document.getElementById('text'))", "6,10")
	p.actOK(t, "type-text", p.window, "words")
	p.expectEval(t, "document.getElementById('text').value", "typed words")
	p.actOK(t, "select-text", p.window, text, "all")
	p.expectEval(t, "(e => e.selectionStart + ',' + e.selectionEnd)(document.getElementById('text'))", "0,11")
	p.actOK(t, "press-key", p.window, "Control_L+Shift_L+k")
	if keys := p.eval(t, "window.e2eKeys.join(',')"); !strings.Contains(keys, "Control+Shift+KeyK") {
		t.Errorf("the Page saw the keys %q, want Control+Shift+KeyK among them", keys)
	}

	p.actOK(t, "scroll", p.window, first, "down", "2")
	p.expectEval(t, "String(document.getElementById('rows').scrollTop > 0)", "true")
	p.actOK(t, "scroll", p.window, last, "into-view")
	p.expectEval(t, `(() => { const b = document.getElementById('rows').getBoundingClientRect();
		const r = document.querySelector('[aria-label="e2e row 30"]').getBoundingClientRect();
		return String(r.top >= b.top - 1 && r.bottom <= b.bottom + 1); })()`, "true")

	p.actOK(t, "hover", p.window, hover)
	p.expectEval(t, "String(document.getElementById('hover').dataset.hovered)", "yes")
}

// dragAndCapture: an HTML5 drag and drop between two Elements, and a
// pointer-capture slider dragged to an Element outside its track.
func (p *pageRun) dragAndCapture(t *testing.T) {
	st := p.open(t, p.srv.mainURL(false, ""), "")
	src := pageIndex(t, st, "button", "e2e drag source")
	dst := pageIndex(t, st, "button", "e2e drop target")
	thumb := pageIndex(t, st, "slider", "e2e slider")
	end := pageIndex(t, st, "button", "e2e slider end")

	p.actOK(t, "drag", p.window, src, dst)
	p.waitEval(t, "String(window.e2eDropped)", "e2e-payload")

	p.actOK(t, "drag", p.window, thumb, end)
	p.waitEval(t, "String(window.e2eCaptured)", "true")
	v, err := strconv.Atoi(p.eval(t, "String(window.e2eSlider)"))
	if err != nil || v < 90 {
		t.Errorf("the slider is at %d (%v) after the drag to its end marker, want 90 or more", v, err)
	}
}

// dialogs: alert, confirm and prompt through `winuse dialog`, other
// Actions failing fast with nothing sent while one is open, and the
// Window State's note.
func (p *pageRun) dialogs(t *testing.T) {
	st := p.open(t, p.srv.mainURL(false, ""), "")
	// A dialog left open would block every later subtest.
	t.Cleanup(func() { p.run(t, "dialog", p.window, "dismiss") })
	count := pageIndex(t, st, "button", "e2e count")
	text := pageIndex(t, st, "edit", "e2e text")
	alertB := pageIndex(t, st, "button", "e2e alert")

	out, _ := p.failing(t, "click", p.window, alertB)
	expectContains(t, "the click that opened an alert", out, "alert dialog")
	expectContains(t, "the click that opened an alert", out, "e2e alert message")

	// Before any new Window State read: the Page indexes above stay
	// valid only until the next read, which shows the frame alone.
	for _, args := range [][]string{
		{"click", p.window, count},
		{"set-value", p.window, text, "blocked"},
		{"eval", p.window, "1"},
		{"navigate", p.window, p.srv.nextURL()},
	} {
		out, took := p.failing(t, args...)
		what := "winuse " + args[0] + " while the alert shows"
		expectContains(t, what, out, "shows an alert dialog")
		expectContains(t, what, out, "nothing was sent")
		if took > 5*time.Second {
			t.Errorf("%s took %s, want a fast failure", what, took)
		}
	}

	var state, stateErr string
	var exit int
	p.guard(t, "get-window-state while the alert shows", func() { state, stateErr, exit = p.run(t, "get-window-state", p.window) })
	if exit != 0 {
		t.Errorf("get-window-state while the alert shows: exit %d, stderr %q", exit, stateErr)
	}
	expectContains(t, "the Window State while the alert shows", state,
		"Note: the Page is read through UI Automation because it shows an alert dialog")

	p.actOK(t, "dialog", p.window, "accept")
	st = p.state(t)
	p.expectEval(t, "String(document.getElementById('count').dataset.count || 0)", "0")
	p.expectEval(t, "document.getElementById('text').value", "")

	out, _ = p.failing(t, "click", p.window, pageIndex(t, st, "button", "e2e confirm"))
	expectContains(t, "the click that opened a confirm", out, "confirm dialog")
	p.actOK(t, "dialog", p.window, "dismiss")
	p.expectEval(t, "String(window.e2eConfirm)", "false")

	out, _ = p.failing(t, "click", p.window, pageIndex(t, st, "button", "e2e prompt"))
	expectContains(t, "the click that opened a prompt", out, "prompt dialog")
	p.actOK(t, "dialog", p.window, "accept", "e2e answer")
	p.expectEval(t, "String(window.e2ePrompt)", "e2e answer")

	out, _ = p.failing(t, "dialog", p.window, "accept")
	expectContains(t, "dialog with none open", out, "no JavaScript dialog")
}

// crossOriginIframe: the Elements of an out-of-process iframe appear in
// the Window State and take a click and a set-value.
func (p *pageRun) crossOriginIframe(t *testing.T) {
	st := p.open(t, p.srv.mainURL(true, ""), "frame ready")
	btn := pageIndex(t, st, "button", "frame button")
	field := pageIndex(t, st, "edit", "frame text")
	if pane, ok := findPageIndex(st, "pane", "e2e frame"); ok {
		if n, _ := strconv.Atoi(btn); n <= pane {
			t.Errorf("the frame's button (%s) is not listed under the iframe's pane (%d)", btn, pane)
		}
	} else {
		t.Logf("no pane line named \"e2e frame\" for the iframe (not required)")
	}

	p.actOK(t, "click", p.window, btn)
	p.waitEval(t, "String(window.e2eFrameClicks)", "1")
	p.actOK(t, "set-value", p.window, field, "hello frame")
	p.waitEval(t, "String(window.e2eFrameText)", "hello frame")
}

// upload: `winuse upload` on a file input, then a click that opens a file
// chooser, which the browser holds back (no native dialog Window) and
// `winuse upload` answers.
func (p *pageRun) upload(t *testing.T) {
	file := filepath.Join(t.TempDir(), uploadName)
	if err := os.WriteFile(file, []byte("winuse e2e upload\n"), 0o644); err != nil {
		t.Fatalf("writing the upload file: %v", err)
	}
	st := p.open(t, p.srv.mainURL(false, ""), "")
	input := pageIndex(t, st, "", "e2e file")
	choose := pageIndex(t, st, "button", "e2e choose file")

	p.actOK(t, "upload", p.window, input, file)
	p.waitEval(t, "String(window.e2eFile)", uploadName)

	out := p.act(t, "click", p.window, choose) // the guard fails on a native dialog Window
	expectContains(t, "the click that opens a file chooser", out, "file chooser")
	p.actOK(t, "upload", p.window, file)
	p.waitEval(t, "String(window.e2eChosen)", uploadName)
}

// download: a click that downloads, then `winuse wait download`, which
// prints the file's path. The file is removed afterwards.
func (p *pageRun) download(t *testing.T) {
	// Where it should land, removed even if the wait fails.
	if home := os.Getenv("USERPROFILE"); home != "" {
		guess := filepath.Join(home, "Downloads", "winuse", p.dl)
		t.Cleanup(func() { removeDownload(t, guess) })
	}
	st := p.open(t, p.srv.mainURL(false, p.dl), "")
	link := pageIndex(t, st, "link", "e2e download")

	out := p.act(t, "click", p.window, link)
	expectContains(t, "the click on the download link", out, "started a download")
	out = p.act(t, "wait", p.window, "download", "--timeout", "20")
	path := strings.TrimSpace(strings.TrimPrefix(strings.SplitN(out, "\n", 2)[0], "ok "))
	if path == "" || !filepath.IsAbs(path) {
		t.Fatalf("wait download printed %q, want ok <path>", out)
	}
	t.Cleanup(func() { removeDownload(t, path) })
	if filepath.Base(path) != p.dl {
		t.Errorf("the download is named %q, want %q", filepath.Base(path), p.dl)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the download: %v", err)
	}
	if string(data) != downloadBody {
		t.Errorf("the download holds %q, want %q", data, downloadBody)
	}
	t.Logf("the download landed in %s", filepath.Dir(path))
}

// navigation: navigate, back, forward and reload, then wait for a
// selector, text, a URL pattern and the load, and eval.
func (p *pageRun) navigation(t *testing.T) {
	main := p.srv.mainURL(false, "")
	p.open(t, main, "")

	out := p.act(t, "navigate", p.window, p.srv.nextURL())
	expectContains(t, "navigate", out, "browser-pages-next.html")
	p.expectEval(t, "document.title", pagesNextTitle)
	out = p.act(t, "back", p.window)
	expectContains(t, "back", out, "browser-pages.html")
	p.expectEval(t, "document.title", pagesTitle)
	out = p.act(t, "forward", p.window)
	expectContains(t, "forward", out, "browser-pages-next.html")
	p.expectEval(t, "document.title", pagesNextTitle)
	p.expectEval(t, "window.e2eMark = 'marked'", "marked")
	p.act(t, "reload", p.window)
	p.expectEval(t, "typeof window.e2eMark", "undefined")

	st := p.open(t, main, "")
	p.actOK(t, "click", p.window, pageIndex(t, st, "button", "e2e later"))
	p.actOK(t, "wait", p.window, "selector", "#late")
	p.actOK(t, "wait", p.window, "text", "e2e late text")
	p.actOK(t, "wait", p.window, "url", "*browser-pages.html*")
	p.actOK(t, "wait", p.window, "load")
	p.expectEval(t, "6 * 7", "42")
	p.expectEval(t, "document.getElementById('late').textContent", "e2e late text")
}

// tabs: tab new opens a background tab (the Window keeps showing its
// page), tab list lists it, tab close closes it. tab switch on a launched
// browser is Headed (ticket 07), so it is not run here;
// TestBrowserPagesExtension runs the Headless one.
func (p *pageRun) tabs(t *testing.T) {
	p.open(t, p.srv.mainURL(false, ""), "")
	_, before := p.tabList(t)

	out := p.act(t, "tab", "new", p.window, p.srv.nextURL())
	tab := strings.TrimSpace(strings.TrimPrefix(out, "ok "))
	if tab == "" || strings.ContainsAny(tab, " \n") {
		t.Fatalf("tab new printed %q, want ok <id>", out)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			p.run(t, "tab", "close", p.window, tab)
		}
	})
	ids, active := p.tabList(t)
	if !contains(ids, tab) {
		t.Errorf("tab list does not list the new tab %s: %v", tab, ids)
	}
	if active != before {
		t.Errorf("the active tab changed from %q to %q: tab new must open in the background", before, active)
	}
	if title := windowTitle(t, p.main); !strings.HasPrefix(title, pagesTitle) {
		t.Errorf("the Window's title is %q after tab new, want it to start with %q", title, pagesTitle)
	}
	t.Log("tab switch on a launched browser is Headed (ticket 07): not run here; TestBrowserPagesExtension runs the Headless one")

	p.actOK(t, "tab", "close", p.window, tab)
	closed = true
	if ids, _ := p.tabList(t); contains(ids, tab) {
		t.Errorf("tab %s is still listed after tab close", tab)
	}
}

// ---- one browser's run ----

// pageRun is the state of one browser's run.
type pageRun struct {
	name   string
	srv    *pageServers
	pid    uint32 // the browser process the Window belongs to
	main   uint64 // the browser Window the pages show in
	window string // main as a Window ID argument
	cover  uint64 // the fixture Window that covers main and holds the foreground
	dl     string // the file name the download step asks for
}

// launch starts the browser through `winuse browser launch` and shows the
// main page in its first tab, which shows the browser's own New Tab page:
// `winuse navigate` leaves it (a Tab that shows a browser page). Should
// that fail, the failure is reported and the step goes over a CDP
// connection of the test's own instead, as the probe does, so the later
// subtests still run; every later step goes through winuse.
func (p *pageRun) launch(t *testing.T, sys *browser.System) {
	profile := sys.LaunchProfileDir(p.name)
	_, statErr := os.Stat(profile)
	profileExisted := statErr == nil

	stdout, stderr, exit := p.run(t, "browser", "launch", p.name)
	if exit != 0 {
		t.Fatalf("winuse browser launch %s failed (exit %d): %s", p.name, exit, stderr)
	}
	if strings.Contains(stdout, "already running") {
		t.Skipf("winuse's own %s profile is already running (%s); skipped so a session in use is not driven. "+
			"Close it with winuse browser stop %s and run again", browser.DisplayName(p.name), strings.TrimSpace(stdout), p.name)
	}
	m := regexp.MustCompile(`launched: pid (\d+), port (\d+)`).FindStringSubmatch(stdout)
	if m == nil {
		t.Fatalf("unexpected launch output %q", stdout)
	}
	pid, _ := strconv.ParseUint(m[1], 10, 32)
	port, _ := strconv.Atoi(m[2])
	p.pid = uint32(pid)
	// The probe's own cleanup: Browser.close, terminate if it lingers, and
	// remove the launch profile when this run created it.
	t.Cleanup(func() {
		(&browserProbe{sys: sys, pid: p.pid, port: port, rep: &probeReport{}}).closeBrowser(t, profile, profileExisted)
	})

	var first string
	u := p.srv.mainURL(false, "")
	exit = -1
	// The Window's title may still be settling on the New Tab page's, or a
	// cold Chrome may have replaced its first Window: a few tries, picking
	// the Window again each time, before the fallback.
	for try := 0; try < 3 && exit != 0; try++ {
		if try > 0 {
			time.Sleep(time.Second)
		}
		first = strconv.FormatUint(launchedBrowserWindow(t, p.pid, 30*time.Second), 10)
		_, stderr, exit = p.run(t, "navigate", first, u)
	}
	if exit != 0 {
		t.Errorf("winuse navigate %s %s from the New Tab page failed (exit %d): %s; navigating over CDP instead", first, u, exit, strings.TrimSpace(stderr))
		navigateFirstTab(t, sys, p.name, p.pid, port, u)
	}
	p.main = waitBrowserWindowTitled(t, p.pid, pagesTitle, 20*time.Second)
	p.window = strconv.FormatUint(p.main, 10)
	t.Logf("%s: pid %d, port %d, Window %s", p.name, p.pid, port, describeWindow(t, p.main))
}

// navigateFirstTab points the launched browser's first tab at u over a CDP
// connection of the test's own, then detaches and closes it.
func navigateFirstTab(t *testing.T, sys *browser.System, name string, pid uint32, port int, u string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ws, err := sys.VersionWS(ctx, port)
	if err != nil {
		t.Fatalf("/json/version on port %d: %v", port, err)
	}
	tr, err := cdp.DialWS(ctx, ws, 20*time.Second)
	if err != nil {
		t.Fatalf("dialing %s: %v", ws, err)
	}
	c := cdp.NewClient(tr)
	defer c.Close()
	pages, err := listPages(c)
	if err != nil || len(pages) == 0 {
		t.Fatalf("listing the launched browser's tabs: %v (%d pages)", err, len(pages))
	}
	conn := &browser.Connection{Browser: name, Mode: browser.ModeLaunch, WSURL: ws, Port: port, PID: pid, Client: c}
	sess, err := conn.AttachPage(ctx, pages[0].TargetID)
	if err != nil {
		t.Fatalf("attaching to the first tab: %v", err)
	}
	if _, err := c.CallSession(ctx, sess, "Page.navigate", map[string]any{"url": u}); err != nil {
		t.Fatalf("navigating the first tab to %s: %v", u, err)
	}
	c.Call(ctx, "Target.detachFromTarget", map[string]any{"sessionId": sess})
}

// open navigates the Window's Tab to u through winuse, waits for text when
// it is not empty, and returns a fresh Window State.
func (p *pageRun) open(t *testing.T, u, text string) string {
	t.Helper()
	p.recover(t)
	p.act(t, "navigate", p.window, u)
	if text != "" {
		p.actOK(t, "wait", p.window, "text", text, "--timeout", "20")
	}
	return p.state(t)
}

// recover puts the cover back in front when an earlier failure left
// something else there, so one failure does not fail every later subtest.
func (p *pageRun) recover(t *testing.T) {
	t.Helper()
	if fg := win.ForegroundWindow(); fg != p.cover {
		t.Logf("the foreground is %s, not the cover: bringing the cover back", describeWindow(t, fg))
		coverAgain(t, p.cover)
		coverOver(t, p.cover, p.main)
	}
}

// run runs winuse with the run's helper idle time.
func (p *pageRun) run(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	return runWinuse(t, args, "WINUSE_HELPER_IDLE="+pagesIdle)
}

// act runs an Action that must succeed (exit 0), with the desktop checks,
// and returns its stdout.
func (p *pageRun) act(t *testing.T, args ...string) string {
	t.Helper()
	label := "winuse " + strings.Join(args, " ")
	var stdout, stderr string
	var exit int
	p.guard(t, label, func() { stdout, stderr, exit = p.run(t, args...) })
	if exit != 0 {
		t.Fatalf("%s: exit %d, stdout %q, stderr:\n%s", label, exit, stdout, stderr)
	}
	return stdout
}

// actOK is act for an Action whose whole output is "ok".
func (p *pageRun) actOK(t *testing.T, args ...string) {
	t.Helper()
	if out := p.act(t, args...); out != "ok\n" {
		t.Errorf("winuse %s: stdout %q, want %q", strings.Join(args, " "), out, "ok\n")
	}
}

// failing runs an Action that must fail with exit 1, with the desktop
// checks, and returns its stdout plus stderr and how long it took.
func (p *pageRun) failing(t *testing.T, args ...string) (string, time.Duration) {
	t.Helper()
	label := "winuse " + strings.Join(args, " ")
	var stdout, stderr string
	var exit int
	var took time.Duration
	p.guard(t, label, func() {
		start := time.Now()
		stdout, stderr, exit = p.run(t, args...)
		took = time.Since(start)
	})
	if exit != 1 {
		t.Errorf("%s: exit %d, want 1 (stdout %q, stderr %q)", label, exit, stdout, stderr)
	}
	return stdout + stderr, took
}

// eval reads a value from the Page through `winuse eval`.
func (p *pageRun) eval(t *testing.T, expr string) string {
	t.Helper()
	return strings.TrimSuffix(p.act(t, "eval", p.window, expr), "\n")
}

func (p *pageRun) expectEval(t *testing.T, expr, want string) {
	t.Helper()
	if got := p.eval(t, expr); got != want {
		t.Errorf("eval %s = %q, want %q", expr, got, want)
	}
}

// waitEval polls eval for a value an event sets a moment after the Action
// (a postMessage from the iframe, a drop, a change event).
func (p *pageRun) waitEval(t *testing.T, expr, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var got string
	for {
		if got = p.eval(t, expr); got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("eval %s = %q after 5 s, want %q", expr, got, want)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// state reads the Window State, which must show the Page through the
// Browser Connection (no fallback note).
func (p *pageRun) state(t *testing.T) string {
	t.Helper()
	var stdout, stderr string
	var exit int
	p.guard(t, "winuse get-window-state "+p.window, func() { stdout, stderr, exit = p.run(t, "get-window-state", p.window) })
	if exit != 0 {
		t.Fatalf("get-window-state %s: exit %d, stderr:\n%s", p.window, exit, stderr)
	}
	if strings.Contains(stdout, "Note: the Page is read through UI Automation") {
		t.Fatalf("the Page was not read through the Browser Connection:\n%s", stdout)
	}
	return stdout
}

// tabList runs `winuse tab list` and returns the tab ids and the active
// one.
func (p *pageRun) tabList(t *testing.T) (ids []string, active string) {
	t.Helper()
	out := p.act(t, "tab", "list", p.window)
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 3 {
			continue
		}
		f := strings.Fields(line[1:])
		if len(f) == 0 {
			continue
		}
		ids = append(ids, f[0])
		if line[0] == '*' {
			active = f[0]
		}
	}
	return ids, active
}

// ---- the desktop checks ----

// pageDesk is what the guard compares before and after an Action.
type pageDesk struct {
	fg        uint64
	coverRank int      // the cover's place in the z-order (see desk)
	above     []string // titled browser Windows above the cover
	over      []string // every Window counted above the cover, for the rank's failure
	dialogs   []uint64 // native dialog Windows (#32770) of the browser, such as a file dialog
}

// desk reads the foreground and the z-order. The cover's rank leaves out
// winuse's own windows (the Agent Cursor, the overlay), the browser's
// untitled popups (tooltips) and the taskbars, which come and go with the
// Actions.
func (p *pageRun) desk(t *testing.T) pageDesk {
	t.Helper()
	all, err := win.Collect()
	if err != nil {
		t.Fatalf("enumerating windows: %v", err)
	}
	d := pageDesk{fg: win.ForegroundWindow(), coverRank: -1}
	rank := 0
	for _, w := range all { // topmost first
		if !w.Visible || w.Cloaked || strings.HasPrefix(w.Class, "winuse.") {
			continue
		}
		if w.Class == "Shell_TrayWnd" || w.Class == "Shell_SecondaryTrayWnd" {
			continue
		}
		if w.PID == p.pid && w.Class == "#32770" {
			d.dialogs = append(d.dialogs, w.ID)
		}
		if w.PID == p.pid && w.Title == "" {
			continue
		}
		if w.ID == p.cover {
			d.coverRank = rank
		}
		if d.coverRank < 0 && w.PID == p.pid && w.Class == "Chrome_WidgetWin_1" {
			d.above = append(d.above, describeWindow(t, w.ID))
		}
		if d.coverRank < 0 {
			d.over = append(d.over, describeWindow(t, w.ID))
		}
		rank++
	}
	return d
}

// guard runs fn and checks the desktop afterwards: the cover still holds
// the foreground and its place in the z-order, no browser Window rose
// above it, and no native dialog Window appeared (one that did is closed).
func (p *pageRun) guard(t *testing.T, label string, fn func()) {
	t.Helper()
	before := p.desk(t)
	fn()
	time.Sleep(pagesSettle)
	after := p.desk(t)
	if after.fg != p.cover {
		t.Errorf("%s: the foreground is %s, want the covering fixture %d", label, describeWindow(t, after.fg), p.cover)
	}
	if after.coverRank != before.coverRank {
		t.Errorf("%s: the covering fixture moved in the z-order from %d to %d (above it before: %s; after: %s)", label, before.coverRank, after.coverRank, strings.Join(before.over, "; "), strings.Join(after.over, "; "))
	}
	if len(after.above) > 0 {
		t.Errorf("%s: browser Windows rose above the covering fixture: %s", label, strings.Join(after.above, "; "))
	}
	known := map[uint64]bool{}
	for _, id := range before.dialogs {
		known[id] = true
	}
	for _, id := range after.dialogs {
		if known[id] {
			continue
		}
		t.Errorf("%s: a native dialog Window appeared: %s", label, describeWindow(t, id))
		stdout, stderr, exit := runWinuse(t, []string{"close-window", strconv.FormatUint(id, 10)}, "WINUSE_HELPER_IDLE="+pagesIdle)
		t.Logf("closed it: exit %d, %q %q", exit, strings.TrimSpace(stdout), strings.TrimSpace(stderr))
	}
}

// coverOver stretches the cover over the whole target Window (same
// top-left, same frame size) without activating anything, as the probe's
// coverFully does.
func coverOver(t *testing.T, cover, target uint64) {
	t.Helper()
	fb, err := win.FrameBounds(target)
	if err != nil {
		t.Fatalf("browser frame bounds: %v", err)
	}
	if err := win.MoveNoActivate(cover, fb.Left, fb.Top); err != nil {
		t.Logf("moving the cover: %v", err)
	}
	if err := win.ResizeNoActivate(cover, fb.Width(), fb.Height()); err != nil {
		t.Logf("resizing the cover: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
}

// ---- the pages and their servers ----

// pageServers serve testdata to the browser: a is the pages' origin
// (127.0.0.1), b the iframe's (as localhost, another site).
type pageServers struct{ a, b *httptest.Server }

func startPageServers(t *testing.T) *pageServers {
	t.Helper()
	dir, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatalf("resolving testdata: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "browser-pages.html")); err != nil {
		t.Fatalf("the test page is missing: %v", err)
	}
	h := pagesHandler(dir)
	a := httptest.NewServer(h)
	t.Cleanup(a.Close)
	b := httptest.NewServer(h)
	t.Cleanup(b.Close)
	return &pageServers{a: a, b: b}
}

// pagesHandler serves testdata, plus /e2e-download?name=..., which answers
// with an attachment of that name (only names starting downloadPrefix).
func pagesHandler(dir string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/e2e-download", func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Query().Get("name"))
		if !strings.HasPrefix(name, downloadPrefix) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		io.WriteString(w, downloadBody)
	})
	mux.Handle("/", http.FileServer(http.Dir(dir)))
	return mux
}

// mainURL is browser-pages.html, with the cross-origin iframe when frame
// is set and the download link asking for dl when it is not empty.
func (s *pageServers) mainURL(frame bool, dl string) string {
	q := url.Values{}
	if frame {
		q.Set("frame", s.frameURL())
	}
	if dl != "" {
		q.Set("dl", dl)
	}
	u := s.a.URL + "/browser-pages.html"
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// frameURL is the iframe page on the second server, addressed as
// localhost: another site than 127.0.0.1, so Chrome runs it out of
// process (two ports of one host would be the same site).
func (s *pageServers) frameURL() string {
	return strings.Replace(s.b.URL, "127.0.0.1", "localhost", 1) + "/browser-pages-frame.html"
}

func (s *pageServers) nextURL() string { return s.a.URL + "/browser-pages-next.html" }

// ---- small helpers ----

func installedBrowsers(sys *browser.System) map[string]browser.Browser {
	installed := map[string]browser.Browser{}
	for _, b := range sys.Env.Detect(sys.Exists) {
		installed[b.Name] = b
	}
	return installed
}

// pageIndex is the Element Index of the one Window State line with the
// role (any role when "") and the name. It fails the test unless exactly
// one line matches.
func pageIndex(t *testing.T, state, role, name string) string {
	t.Helper()
	m := pageLines(state, role, name)
	if len(m) != 1 {
		t.Fatalf("the Window State has %d lines with role %q named %q, want 1:\n%s", len(m), role, name, state)
	}
	return m[0]
}

// findPageIndex is pageIndex without failing.
func findPageIndex(state, role, name string) (int, bool) {
	m := pageLines(state, role, name)
	if len(m) != 1 {
		return 0, false
	}
	n, err := strconv.Atoi(m[0])
	return n, err == nil
}

func pageLines(state, role, name string) []string {
	r := regexp.QuoteMeta(role)
	if role == "" {
		r = `[^\n]+?`
	}
	re := regexp.MustCompile(`(?m)^\t+(\d+) ` + r + `( \([^)\n]*\))? ` + regexp.QuoteMeta(name) + `( |$)`)
	var out []string
	for _, m := range re.FindAllStringSubmatch(state, -1) {
		out = append(out, m[1])
	}
	return out
}

// waitBrowserWindowTitled waits for a browser Window whose title starts
// with the page title (the browser adds " - Google Chrome" and the like).
// windowSteady is how long a launched browser's Window must stay the only
// one before a test acts on it.
const windowSteady = 500 * time.Millisecond

// launchedBrowserWindow waits for the one Window of browser process pid
// that winuse lists (win.KeepWindow, the set Window IDs resolve against)
// and returns it once it has stayed the only one for windowSteady. A full
// run once saw both browser tests fail with "no open Window with ID N" on
// a cold Chrome's first Window, a Window findWindowOf took (it checks only
// the class, visibility and title) that winuse no longer, or never,
// listed; the tests also pick it again on each retry.
func launchedBrowserWindow(t *testing.T, pid uint32, timeout time.Duration) uint64 {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var id uint64
	var since time.Time
	for time.Now().Before(deadline) {
		var kept []uint64
		for _, w := range browserWindows(t, pid) {
			if win.KeepWindow(w) {
				kept = append(kept, w.ID)
			}
		}
		switch {
		case len(kept) != 1:
			id = 0
		case kept[0] != id:
			id, since = kept[0], time.Now()
		case time.Since(since) >= windowSteady:
			return id
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("browser process %d showed no single, steady Window that winuse lists within %s", pid, timeout)
	return 0
}

func waitBrowserWindowTitled(t *testing.T, pid uint32, title string, timeout time.Duration) uint64 {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, w := range browserWindows(t, pid) {
			if strings.HasPrefix(w.Title, title) {
				return w.ID
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("no browser Window titled %q appeared within %s", title, timeout)
	return 0
}

func windowTitle(t *testing.T, id uint64) string {
	t.Helper()
	all, err := win.Collect()
	if err != nil {
		t.Fatalf("enumerating windows: %v", err)
	}
	for _, w := range all {
		if w.ID == id {
			return w.Title
		}
	}
	return ""
}

func expectContains(t *testing.T, what, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("%s: output %q does not contain %q", what, got, want)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// removeDownload removes a file the download step made, if it is there.
func removeDownload(t *testing.T, path string) {
	if err := os.Remove(path); err == nil {
		t.Logf("cleanup: removed the download %s", path)
	} else if !os.IsNotExist(err) {
		t.Logf("cleanup: could not remove the download %s: %v", path, err)
	}
}
