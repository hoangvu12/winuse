//go:build e2e

// `winuse read` (.scratch/bladebro-ideas ticket 25).
//
// TestReadGoldens runs read.js (through page.ReadMarkdown, in process, as
// the helper runs it) on the saved pages in testdata/read in a headless
// Chrome or Edge on a throwaway profile (startIsolatedBrowser), and
// compares each result with its golden file, testdata/read/<case>.md: a
// Hacker News-like table page, an article, a search results page where
// every result is a link, a page with a misleading <main>, a table page, a
// docs page and a challenge page, plus the table page scoped to its table
// (--root) and the article with --urls full. WINUSE_E2E_UPDATE_GOLDEN=1
// writes the golden files instead.
//
// TestReadCommand runs the real `winuse read` on testdata/read/article.html
// in a browser `winuse browser launch` started: the whole page, --root on
// a Page Element, --max-chars with its cut line, --out, and the refusal on
// a native Window (the fixture app).
//
// User-run only (see the package comment); an agent must never start it.

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/win"
)

// readGoldenCases are the golden files: the page, and the element to
// scope to (a CSS selector, "" for the page's main content).
var readGoldenCases = []struct {
	name, page, root string
	full             bool
}{
	{name: "hn", page: "hn.html"},
	{name: "article", page: "article.html"},
	{name: "article-full-urls", page: "article.html", full: true},
	{name: "search", page: "search.html"},
	{name: "misleading-main", page: "misleading-main.html"},
	{name: "table", page: "table.html"},
	{name: "table-root", page: "table.html", root: "table.wikitable"},
	{name: "docs", page: "docs.html"},
	{name: "challenge", page: "challenge.html"},
}

func TestReadGoldens(t *testing.T) {
	optIn(t)
	dir, err := filepath.Abs(filepath.Join("testdata", "read"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.FileServer(http.Dir(dir)))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	conn := headlessBrowser(t, ctx)
	update := os.Getenv("WINUSE_E2E_UPDATE_GOLDEN") == "1"

	for _, tc := range readGoldenCases {
		t.Run(tc.name, func(t *testing.T) {
			got := readGolden(t, ctx, conn, srv.URL+"/"+tc.page, tc.root, tc.full)
			got = strings.ReplaceAll(got, srv.URL, "http://SERVER") // the port changes each run
			file := filepath.Join(dir, tc.name+".md")
			if update {
				if err := os.WriteFile(file, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("the golden file: %v (WINUSE_E2E_UPDATE_GOLDEN=1 writes it)", err)
			}
			if got != strings.ReplaceAll(string(want), "\r\n", "\n") {
				t.Errorf("%s differs from %s; got:\n%s", tc.page, file, got)
			}
		})
	}
}

// headlessBrowser starts a headless Chrome or Edge on a throwaway profile
// (startIsolatedBrowser), skipping the test when neither is installed, and
// returns its Browser Connection; the browser closes when the test ends.
func headlessBrowser(t *testing.T, ctx context.Context) *browser.Connection {
	t.Helper()
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
	port := freePort(t)
	pid := startIsolatedBrowser(t, b.Path, "--headless=new", "--window-size=1366,900", fmt.Sprintf("--remote-debugging-port=%d", port), "about:blank")
	var ws string
	var err error
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
		if ws, err = sys.VersionWS(ctx, port); err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("/json/version on port %d: %v", port, err)
	}
	tr, err := cdp.DialWS(ctx, ws, 20*time.Second)
	if err != nil {
		t.Fatalf("dialing %s: %v", ws, err)
	}
	c := cdp.NewClient(tr)
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 5*time.Second)
		c.Call(cctx, "Browser.close", nil)
		ccancel()
		c.Close()
		for deadline := time.Now().Add(10 * time.Second); pidAlive(pid) && time.Now().Before(deadline); {
			time.Sleep(100 * time.Millisecond)
		}
		time.Sleep(time.Second)
	})
	return &browser.Connection{Browser: b.Name, Mode: browser.ModeLaunch, WSURL: ws, Port: port, Client: c}
}

// readGolden opens u in a new tab, runs read.js on its main content or on
// the element root selects, closes the tab and returns the golden text:
// the element read, any wall, then the Markdown.
func readGolden(t *testing.T, ctx context.Context, conn *browser.Connection, u, root string, full bool) string {
	t.Helper()
	targetID := openAnsweringTab(t, ctx, conn, u)
	defer func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer ccancel()
		conn.Client.Call(cctx, "Target.closeTarget", map[string]any{"targetId": targetID})
	}()
	sess, err := conn.AttachPage(ctx, targetID)
	if err != nil {
		t.Fatalf("attaching %s: %v", u, err)
	}
	var node int64
	if root != "" {
		res, err := conn.Client.CallSession(ctx, sess, "Runtime.evaluate", map[string]any{"expression": "document.querySelector(" + jsString(root) + ")"})
		if err != nil {
			t.Fatal(err)
		}
		var ev struct {
			Result struct {
				ObjectID string `json:"objectId"`
			} `json:"result"`
		}
		json.Unmarshal(res, &ev)
		if ev.Result.ObjectID == "" {
			t.Fatalf("%s has no %s", u, root)
		}
		res, err = conn.Client.CallSession(ctx, sess, "DOM.describeNode", map[string]any{"objectId": ev.Result.ObjectID})
		if err != nil {
			t.Fatal(err)
		}
		var dn struct {
			Node struct {
				BackendNodeID int64 `json:"backendNodeId"`
			} `json:"node"`
		}
		json.Unmarshal(res, &dn)
		node = dn.Node.BackendNodeID
	}
	md, err := page.ReadMarkdown(ctx, conn.Client, sess, "", node, page.ReadOptions{FullURLs: full})
	if err != nil {
		t.Fatalf("reading %s: %v", u, err)
	}
	block := md.Wall
	if block == "" {
		block = "none"
	}
	return fmt.Sprintf("main: %s\nblock: %s\n\n%s\n", md.Main, block, md.Text)
}

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestReadCommand(t *testing.T) {
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
	p.open(t, srv.a.URL+"/read/article.html", "Rivers return")

	out := p.act(t, "read", p.window)
	expectContains(t, "read", out, "# Rivers return to the valley after forty dry years")
	expectContains(t, "read", out, "[2025 feasibility study](environment-agency.example.gov/reports/2025/")
	expectContains(t, "read", out, "[Maria Okafor](/people/maria-okafor)")
	for _, junk := range []string{"[0] ", "Advertisement", "Why wetlands matter", "Privacy", "river-valley-hero-image-large.jpg"} {
		if strings.Contains(out, junk) {
			t.Errorf("read printed %q:\n%s", junk, out)
		}
	}

	full := p.act(t, "read", p.window, "--urls", "full")
	expectContains(t, "read --urls full", full, "(https://cdn.example.net/images/2026/11/river-valley-hero-image-large.jpg)")

	// --root: one list item, by its Element Index in the latest read.
	st := p.state(t)
	item := pageIndex(t, st, "list item", "Salmon counts start in March.")
	scoped := p.act(t, "read", p.window, "--root", item)
	expectContains(t, "read --root", scoped, "- Salmon counts start in March.")
	if strings.Contains(scoped, "What changed") {
		t.Errorf("read --root printed more than the item:\n%s", scoped)
	}

	cut := p.act(t, "read", p.window, "--max-chars", "300")
	if !regexp.MustCompile(`\(cut at 300 of \d+ characters`).MatchString(cut) {
		t.Errorf("read --max-chars 300 printed no cut line:\n%s", cut)
	}

	file := filepath.Join(t.TempDir(), "article.md")
	short := p.act(t, "read", p.window, "--out", file)
	expectContains(t, "read --out", short, file)
	data, err := os.ReadFile(file)
	if err != nil || !strings.Contains(string(data), "## What happens next") {
		t.Errorf("read --out wrote %q, %v", data, err)
	}

	// A native Window (the cover, a fixture Window): refused, pointing at
	// get-window-state.
	stdout, stderr, exit := p.run(t, "read", strconv.FormatUint(p.cover, 10))
	if exit == 0 || !strings.Contains(stderr, "get-window-state") {
		t.Errorf("read on a native Window: exit %d, %q %q", exit, stdout, stderr)
	}
}
