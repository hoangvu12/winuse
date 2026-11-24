//go:build e2e

// Say when a Page scroll moved nothing (.scratch/bladebro-ideas ticket 03).
//
// TestPageScrollResult runs the real `winuse scroll` on
// testdata/page-scroll.html in a browser `winuse browser launch` started,
// its Window covered. The page's body is short (it does not scroll) and
// holds a nested scroller, div#list, of 40 rows:
//   - into-view on a button already on screen says it was already in view;
//   - a wheel over the body says the Page does not scroll;
//   - a wheel up over the list at its top says it is at its start;
//   - a wheel down over the list moves it, with a plain `ok`;
//   - a wheel down over the list at its end says it is at its end.
//
// User-run only (see the package comment); an agent must never start it.

package e2e

import (
	"strconv"
	"testing"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/win"
)

func TestPageScrollResult(t *testing.T) {
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
	st := p.open(t, srv.a.URL+"/page-scroll.html", "e2e scroll top")
	const list = `<div id="list">`

	// into-view on a button on screen: nothing scrolled.
	top := pageIndex(t, st, "button", "e2e scroll top")
	if out := p.act(t, "scroll", p.window, top, "into-view"); out != "ok\nNote: element "+top+" was already in view; nothing scrolled\n" {
		t.Errorf("into-view on a button on screen printed %q", out)
	}

	// A wheel over the short body.
	if out := p.act(t, "scroll", p.window, top, "down"); out != "ok\nNote: nothing scrolled; the Page does not scroll\n" {
		t.Errorf("a wheel over the short body printed %q", out)
	}

	// A wheel up over the list at its top.
	row := pageIndex(t, st, "button", "e2e row 1")
	if out := p.act(t, "scroll", p.window, row, "up"); out != "ok\nNote: nothing scrolled; "+list+" is at its start, and the Page does not scroll\n" {
		t.Errorf("a wheel up over the list at its top printed %q", out)
	}

	// A wheel down over the list: it moved, a plain ok.
	if out := p.act(t, "scroll", p.window, row, "down", "1"); out != "ok\n" {
		t.Errorf("a wheel down over the list printed %q", out)
	}
	if got, err := strconv.ParseFloat(p.eval(t, "String(document.getElementById('list').scrollTop)"), 64); err != nil || got <= 0 {
		t.Errorf("the list's scrollTop after the wheel = %v (%v), want > 0", got, err)
	}

	// The list at its end: a wheel down there moves nothing.
	p.act(t, "eval", p.window, "document.getElementById('list').scrollTop = 1e6, 'done'")
	st = p.state(t)
	last := pageIndex(t, st, "button", "e2e row 40")
	if out := p.act(t, "scroll", p.window, last, "down"); out != "ok\nNote: nothing scrolled; "+list+" is at its end, and the Page does not scroll\n" {
		t.Errorf("a wheel down over the list at its end printed %q", out)
	}
}
