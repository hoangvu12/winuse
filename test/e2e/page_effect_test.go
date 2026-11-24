//go:build e2e

// What a Page click or key did (.scratch/bladebro-ideas ticket 06).
//
// TestPageActionEffects runs the real `winuse click` and `press-key` on
// testdata/page-effect.html in a browser `winuse browser launch` started,
// its Window covered: a button with no handler prints the "no change seen"
// note (and was clicked once), a button that changes only text prints the
// text note, a button inside an open shadow root that adds an element
// prints a plain `ok` (and under --json `"effect":"changed"`), and a link
// prints the navigated note with its URL.
//
// User-run only (see the package comment); an agent must never start it.

package e2e

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/win"
)

func TestPageActionEffects(t *testing.T) {
	optIn(t)
	if err := win.SetPerMonitorV2DPIAwareness(); err != nil {
		t.Logf("per-monitor DPI awareness failed (bounds may be scaled): %v", err)
	}
	sys := browser.Live()
	var b browser.Browser
	found := false
	for _, name := range []string{"chrome", "edge"} {
		if b, found = installedBrowsers(sys)[name]; found {
			break
		}
	}
	if !found {
		t.Skip("neither Chrome nor Edge is installed; nothing was run")
	}
	startFG := win.ForegroundWindow()
	pin := currentBrowserPin(t)
	t.Cleanup(func() {
		restoreBrowserPin(t, pin)
		restoreForeground(t, startFG)
	})
	srv := startPageServers(t)
	killHelper(t)
	t.Cleanup(func() { killHelper(t) })

	p := &pageRun{name: b.Name, srv: srv}
	p.launch(t, sys)
	p.cover = coverWindow(t, p.window)
	coverOver(t, p.cover, p.main)
	st := p.open(t, srv.a.URL+"/page-effect.html", "e2e effect nothing")

	// A button with no handler: delivered once, nothing changed.
	nothing := pageIndex(t, st, "button", "e2e effect nothing")
	out := p.act(t, "click", p.window, nothing)
	if out != "ok\nNote: no change seen in the Page; the click was delivered, so read the Page before repeating it\n" {
		t.Errorf("the button with no handler printed %q", out)
	}
	p.expectEval(t, "String(window.e2eNothingClicks)", "1")

	// A button that changes only a text node.
	text := pageIndex(t, st, "button", "e2e effect text")
	out = p.act(t, "click", p.window, text)
	if out != "ok\nNote: the Page changed (text only)\n" {
		t.Errorf("the text button printed %q", out)
	}
	p.expectEval(t, "document.getElementById('status').textContent", "clicked 1")

	// A button inside an open shadow root that adds an element: a change,
	// so a plain ok; --json says changed.
	shadow := pageIndex(t, st, "button", "e2e effect shadow")
	stdout, stderr, exit := p.run(t, "--json", "click", p.window, shadow)
	var line struct {
		OK     bool   `json:"ok"`
		Out    string `json:"out"`
		Effect string `json:"effect"`
	}
	if err := json.Unmarshal([]byte(stdout), &line); err != nil || exit != 0 || !line.OK || line.Out != "ok\n" || line.Effect != "changed" {
		t.Errorf("the shadow button under --json: exit %d, %q %q (%v)", exit, stdout, stderr, err)
	}
	p.expectEval(t, "String(document.querySelector('effect-host').shadowRoot.querySelectorAll('li').length)", "1")

	// A link: the Page navigated, and the note names where.
	link := pageIndex(t, st, "link", "e2e effect link")
	out = p.act(t, "click", p.window, link)
	if !strings.HasPrefix(out, "ok\nNote: the Page navigated to "+srv.a.URL+"/browser-pages-next.html") {
		t.Errorf("the link printed %q", out)
	}
	p.expectEval(t, "document.title", pagesNextTitle)
}
