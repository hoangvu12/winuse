//go:build e2e

// The wall check (.scratch/bladebro-ideas ticket 05).
//
// TestWallCheck runs wall.js (page.WallKind, in process, as the helper
// runs it after a navigation) on the pages in testdata/wall in a headless
// Chrome or Edge on a throwaway profile (headlessBrowser): one page per
// wall kind (a Cloudflare interstitial, a bare Turnstile, DataDome,
// PerimeterX, reCAPTCHA, hCaptcha, Akamai, a 429), and two that are no wall (a long
// docs page about rate limits, a login form with a Turnstile widget), plus
// read's article and docs pages. read.js and extract.js name the same
// wall on each, as they carry the same check.
//
// TestNavigateNotesAWall runs the real `winuse navigate` onto a wall in a
// browser `winuse browser launch` started: ok with the note, the --json
// wall field, and no note on an ordinary page.
//
// User-run only (see the package comment); an agent must never start it.

package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// wallCases are testdata pages and the wall each shows ("" for none).
var wallCases = []struct{ page, want string }{
	{"wall/cloudflare.html", "Cloudflare"},
	{"wall/turnstile.html", "Cloudflare"},
	{"wall/datadome.html", "DataDome"},
	{"wall/perimeterx.html", "PerimeterX"},
	{"wall/recaptcha.html", "reCAPTCHA"},
	{"wall/hcaptcha.html", "hCaptcha"},
	{"wall/akamai.html", "Akamai"},
	{"wall/rate-limit.html", "rate-limit"},
	{"wall/docs-rate-limit.html", ""},
	{"wall/login-turnstile.html", ""},
	{"read/article.html", ""},
	{"read/docs.html", ""},
	{"read/challenge.html", "Cloudflare"},
}

func TestWallCheck(t *testing.T) {
	optIn(t)
	dir, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.FileServer(http.Dir(dir)))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	conn := headlessBrowser(t, ctx)

	for _, tc := range wallCases {
		t.Run(tc.page, func(t *testing.T) {
			targetID := openAnsweringTab(t, ctx, conn, srv.URL+"/"+tc.page)
			defer func() {
				cctx, ccancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer ccancel()
				conn.Client.Call(cctx, "Target.closeTarget", map[string]any{"targetId": targetID})
			}()
			sess, err := conn.AttachPage(ctx, targetID)
			if err != nil {
				t.Fatalf("attaching %s: %v", tc.page, err)
			}
			kind, err := page.WallKind(ctx, conn.Client, sess)
			if err != nil || kind != tc.want {
				t.Errorf("WallKind on %s: %q, %v; want %q", tc.page, kind, err, tc.want)
			}
			// The check after a Page Action that navigated (it waits for the
			// body first) names the same wall.
			if kind, err := page.WallKindOnceLoaded(ctx, conn.Client, sess); err != nil || kind != tc.want {
				t.Errorf("WallKindOnceLoaded on %s: %q, %v; want %q", tc.page, kind, err, tc.want)
			}
			md, err := page.ReadMarkdown(ctx, conn.Client, sess, "", 0, page.ReadOptions{})
			if err != nil || md.Wall != tc.want {
				t.Errorf("read.js on %s named %q, %v; want %q", tc.page, md.Wall, err, tc.want)
			}
			ex, err := page.Extract(ctx, conn.Client, sess, nil, page.ExtractOptions{})
			if err != nil || ex.Wall != tc.want {
				t.Errorf("extract.js on %s named %q, %v; want %q", tc.page, ex.Wall, err, tc.want)
			}
		})
	}
}

func TestNavigateNotesAWall(t *testing.T) {
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

	out := p.act(t, "navigate", p.window, srv.a.URL+"/wall/rate-limit.html")
	expectContains(t, "navigate onto a 429 page", out, "ok "+srv.a.URL+"/wall/rate-limit.html\n"+protocol.WallNote("rate-limit")+"\n")

	out = p.act(t, "navigate", p.window, srv.a.URL+"/read/article.html")
	if strings.Contains(out, "Note: the Page looks like") {
		t.Errorf("navigate onto an article printed a wall Note:\n%s", out)
	}

	out = p.act(t, "--json", "back", p.window)
	var line struct {
		OK   bool   `json:"ok"`
		Out  string `json:"out"`
		Wall string `json:"wall"`
	}
	if err := json.Unmarshal([]byte(out), &line); err != nil || !line.OK || line.Wall != "rate-limit" || !strings.Contains(line.Out, protocol.WallNote("rate-limit")) {
		t.Errorf("--json back onto the 429 page: %q (%v), want ok with wall rate-limit", out, err)
	}
}
