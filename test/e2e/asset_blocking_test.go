//go:build e2e

// Asset blocking (bladebro-ideas ticket 28).
//
// TestAssetBlocking blocks images, fonts and media in process, through
// internal/page as the helper blocks them (page.BlockAssets:
// Network.enable, then Network.setBlockedURLs with urlPatterns), in a
// headless Chrome or Edge on a throwaway profile (startIsolatedBrowser). A
// fixture page holds images, a web font and a video, each served after
// assetDelay. In one Tab nothing is blocked; in another the block is set
// before the navigation. The blocked Tab's load event must arrive sooner,
// the server must never see its images, font or video, while its HTML,
// stylesheet and script still load, and an image with no file extension
// still loads (the documented gap). Then --block none (no classes) lets
// them load again.
//
// The note a blocking Tab's reads end with is the helper's, checked offline
// (internal/helper/assets_test.go): a headless browser has no Window for
// the winuse CLI to name.
//
// User-run only (see the package comment); an agent must never start it.

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/page"
)

// assetDelay is how long the server holds every image, font and video
// before it answers: what makes the load event wait for them.
const assetDelay = 800 * time.Millisecond

// assetPage is the fixture page: images of four extensions, one without an
// extension (answered at once), a web font, a video, a stylesheet and a
// script. Its paths are relative, so each run's folder is its own and
// nothing comes from the cache.
const assetPage = `<!doctype html>
<title>winuse e2e assets</title>
<link rel="stylesheet" href="style.css">
<script src="app.js"></script>
<h1 style="font-family: E2EFont, sans-serif">Assets</h1>
<img src="a.png" alt="A png"> <img src="b.jpg" alt="A jpg"> <img src="c.svg" alt="An svg"> <img src="d.webp" alt="A webp">
<img src="photo" alt="No extension">
<video src="clip.mp4" preload="auto" muted></video>
<p>Text stays.</p>`

// assetServer serves /e2e-assets/<run>/<file> and counts each file's hits
// by run.
type assetServer struct {
	mu   sync.Mutex
	hits map[string]int // "<run>/<file>"
}

func (s *assetServer) count(run, file string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[run+"/"+file]
}

func (s *assetServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rest, ok := strings.CutPrefix(r.URL.Path, "/e2e-assets/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	run, file := path.Dir(rest), path.Base(rest)
	s.mu.Lock()
	s.hits[run+"/"+file]++
	s.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	switch file {
	case "blank.html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<!doctype html><title>blank</title>")
		return
	case "page.html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, assetPage)
		return
	case "style.css":
		w.Header().Set("Content-Type", "text/css")
		fmt.Fprint(w, `@font-face { font-family: E2EFont; src: url(font.woff2) format("woff2"); } p { color: #333 }`)
		return
	case "app.js":
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprint(w, "window.e2eScriptRan = true;")
		return
	case "photo":
		w.Header().Set("Content-Type", "image/png") // no extension: not blocked, answered at once
		w.Write(tinyPNG)
		return
	}
	time.Sleep(assetDelay)
	switch path.Ext(file) {
	case ".svg":
		w.Header().Set("Content-Type", "image/svg+xml")
		fmt.Fprint(w, `<svg xmlns="http://www.w3.org/2000/svg" width="4" height="4"/>`)
	case ".png":
		w.Header().Set("Content-Type", "image/png")
		w.Write(tinyPNG)
	case ".woff2":
		w.Header().Set("Content-Type", "font/woff2")
		w.Write([]byte("not really a font"))
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write([]byte("bytes"))
	}
}

// tinyPNG is a 1x1 transparent PNG.
var tinyPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
	0x42, 0x60, 0x82,
}

// assetFiles are the fixture's blocked files, by class.
var assetFiles = []string{"a.png", "b.jpg", "c.svg", "d.webp", "font.woff2", "clip.mp4"}

func TestAssetBlocking(t *testing.T) {
	optIn(t)
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
	as := &assetServer{hits: map[string]int{}}
	srv := httptest.NewServer(as)
	t.Cleanup(srv.Close)

	port := freePort(t)
	pid := startIsolatedBrowser(t, b.Path, "--headless=new", fmt.Sprintf("--remote-debugging-port=%d", port), "about:blank")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
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
	conn := &browser.Connection{Browser: b.Name, Mode: browser.ModeLaunch, WSURL: ws, Port: port, Client: c}

	// load opens a run's page in a Tab (one session per Tab, as the helper
	// keeps), after setting the classes it blocks when block is true (an
	// empty list is --block none), and returns its load event's time since
	// the navigation started.
	sessions := map[string]string{}
	load := func(tab, run string, classes []string, block bool) float64 {
		t.Helper()
		sess, ok := sessions[tab]
		if !ok {
			target := openAnsweringTab(t, ctx, conn, srv.URL+"/e2e-assets/"+run+"-blank/blank.html")
			if sess, err = conn.AttachPage(ctx, target); err != nil {
				t.Fatalf("attaching tab %s: %v", tab, err)
			}
			sessions[tab] = sess
		}
		if block {
			if err := page.BlockAssets(ctx, c, sess, classes); err != nil {
				t.Fatalf("%s: BlockAssets(%v): %v", run, classes, err)
			}
		}
		u := srv.URL + "/e2e-assets/" + run + "/page.html"
		loader, err := page.Navigate(ctx, c, sess, u)
		if err != nil {
			t.Fatalf("%s: navigate: %v", run, err)
		}
		committed := func(ctx context.Context) bool {
			l, _, err := page.MainFrame(ctx, c, sess)
			return err == nil && l == loader
		}
		wctx, wcancel := context.WithTimeout(ctx, 30*time.Second)
		err = page.WaitLoad(wctx, c, sess, page.LoadComplete, committed, nil)
		wcancel()
		if err != nil {
			t.Fatalf("%s: waiting for the load: %v", run, err)
		}
		var at float64
		for deadline := time.Now().Add(10 * time.Second); at <= 0 && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
			at = evalNumber(t, ctx, c, sess, "performance.getEntriesByType('navigation')[0].loadEventStart")
		}
		if got := evalNumber(t, ctx, c, sess, "window.e2eScriptRan === true ? 1 : 0"); got != 1 {
			t.Errorf("%s: the page's script did not run", run)
		}
		time.Sleep(assetDelay + 500*time.Millisecond) // the font and the video ask late
		return at
	}

	plain := load("plain", "plain", nil, false)
	blocked := load("blocked", "blocked", []string{"images", "fonts", "media"}, true)
	t.Logf("load event: %.0f ms without blocking, %.0f ms blocking images, fonts and media", plain, blocked)

	if plain < float64(assetDelay.Milliseconds()) {
		t.Errorf("without blocking the load event came at %.0f ms, before the images (%s): the fixture is wrong", plain, assetDelay)
	}
	if blocked >= plain-float64(assetDelay.Milliseconds())/2 {
		t.Errorf("blocking did not bring the load event sooner: %.0f ms, against %.0f ms without", blocked, plain)
	}
	for _, f := range assetFiles {
		if n := as.count("blocked", f); n != 0 {
			t.Errorf("the blocking Tab fetched %s %d times", f, n)
		}
	}
	for _, f := range []string{"a.png", "b.jpg", "c.svg", "d.webp"} {
		if as.count("plain", f) == 0 {
			t.Errorf("the plain Tab never fetched %s: the fixture is wrong", f)
		}
	}
	for _, f := range []string{"page.html", "style.css", "app.js"} {
		if as.count("blocked", f) == 0 {
			t.Errorf("the blocking Tab never fetched %s: only images, fonts and media may be blocked", f)
		}
	}
	if as.count("blocked", "photo") == 0 {
		t.Errorf("the image without a file extension was blocked too; the help says it loads")
	}

	// --block none: the same Tab loads them again.
	again := load("blocked", "unblocked", []string{}, true)
	t.Logf("load event after --block none: %.0f ms", again)
	for _, f := range []string{"a.png", "b.jpg", "c.svg", "d.webp"} {
		if as.count("unblocked", f) == 0 {
			t.Errorf("after --block none the Tab still did not fetch %s", f)
		}
	}
}

// evalNumber evaluates a number in a session's Page (no Runtime.enable).
func evalNumber(t *testing.T, ctx context.Context, c *cdp.Client, sess, expr string) float64 {
	t.Helper()
	ectx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	res, err := c.CallSession(ectx, sess, "Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true})
	if err != nil {
		t.Fatalf("eval %s: %v", expr, err)
	}
	var v struct {
		Result struct {
			Value float64 `json:"value"`
		} `json:"result"`
	}
	json.Unmarshal(res, &v)
	return v.Result.Value
}
