//go:build e2e

// The HTTP status on navigate (bladebro-ideas ticket 02).
//
// TestHTTPStatus reads the main document's status in process, through
// internal/page as the helper reads it, in a headless Chrome or Edge on a
// throwaway profile (startIsolatedBrowser): 200, 404 and 429 with a body,
// 503, a redirect chain that ends in 404, and 429 and 503 with an empty
// body, where Page.navigate fails with ERR_HTTP_RESPONSE_CODE_FAILURE and
// the Tab shows Chrome's error page.
//
// The http-status subtest of TestBrowserPages (pageRun.httpStatus) runs the
// real navigate, back, forward and reload on the same pages and checks the
// note, the empty-body case, and --json's http_status.
//
// User-run only (see the package comment); an agent must never start it.

package e2e

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/page"
)

// statusHandlers adds the status pages: /e2e-status?code=N answers N with
// a small page (no body with empty=1, a Retry-After header with retry=S),
// and /e2e-redirect?n=K redirects K times, then to /e2e-status?code=404.
func statusHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/e2e-status", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		code, err := strconv.Atoi(q.Get("code"))
		if err != nil || code < 200 || code > 599 {
			http.Error(w, "code must be 200-599", http.StatusBadRequest)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if s := q.Get("retry"); s != "" {
			w.Header().Set("Retry-After", s)
		}
		if q.Get("empty") == "1" {
			w.Header().Set("Content-Length", "0")
			w.WriteHeader(code)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(code)
		fmt.Fprintf(w, "<!doctype html><title>e2e status %d</title><h1>e2e status %d</h1><p>The server answered %d.</p>", code, code, code)
	})
	mux.HandleFunc("/e2e-redirect", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.URL.Query().Get("n"))
		to := "/e2e-status?code=404"
		if n > 1 {
			to = "/e2e-redirect?n=" + strconv.Itoa(min(n, 5)-1)
		}
		http.Redirect(w, r, to, http.StatusFound)
	})
}

func TestHTTPStatus(t *testing.T) {
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
	mux := http.NewServeMux()
	statusHandlers(mux)
	srv := httptest.NewServer(mux)
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

	targetID := openAnsweringTab(t, ctx, conn, srv.URL+"/e2e-status?code=200")
	sess, err := conn.AttachPage(ctx, targetID)
	if err != nil {
		t.Fatalf("attaching the tab: %v", err)
	}

	cases := []struct {
		path      string
		code      int
		errorPage bool // an empty body: Chrome's error page
	}{
		{"/e2e-status?code=200", 200, false},
		{"/e2e-status?code=404", 404, false},
		{"/e2e-status?code=429&retry=45", 429, false},
		{"/e2e-status?code=503", 503, false},
		{"/e2e-redirect?n=2", 404, false},
		{"/e2e-status?code=429&empty=1&retry=45", 429, true},
		{"/e2e-status?code=503&empty=1", 503, true},
		{"/e2e-status?code=200", 200, false}, // and back from the error page
	}
	for _, tc := range cases {
		u := srv.URL + tc.path
		loader, err := page.Navigate(ctx, c, sess, u)
		var nav *page.NavigateError
		switch {
		case errors.As(err, &nav) && nav.HTTPError():
			if !tc.errorPage {
				t.Errorf("%s: Page.navigate failed with %v; it has a body", tc.path, err)
			}
			loader = nav.LoaderID
		case err != nil:
			t.Fatalf("%s: navigate: %v", tc.path, err)
		case tc.errorPage:
			t.Errorf("%s: Page.navigate did not fail with %s on an empty body", tc.path, page.ErrHTTPResponseCode)
		}
		var committed func(context.Context) bool
		if loader != "" {
			committed = func(ctx context.Context) bool {
				l, _, err := page.MainFrame(ctx, c, sess)
				return err == nil && l == loader
			}
		}
		wctx, wcancel := context.WithTimeout(ctx, 15*time.Second)
		err = page.WaitLoad(wctx, c, sess, page.LoadComplete, committed, nil)
		wcancel()
		if err != nil {
			t.Errorf("%s: waiting for the load: %v", tc.path, err)
			continue
		}
		st, err := page.ReadHTTPStatus(ctx, c, sess)
		if err != nil {
			t.Errorf("%s: reading the status: %v", tc.path, err)
			continue
		}
		if st.Code != tc.code || st.ErrorPage != tc.errorPage {
			t.Errorf("%s: %+v, want code %d, error page %v", tc.path, st, tc.code, tc.errorPage)
		}
		if tc.errorPage && st.URL != u {
			t.Errorf("%s: the error page's address is %q, want %q", tc.path, st.URL, u)
		}
	}
}

// httpStatus: navigate, back, forward and reload onto 4xx/5xx pages carry
// the note and stay ok; an empty-body 503 lands on Chrome's error page and
// is noted with the address asked for; --json carries http_status; a 200
// has no note.
func (p *pageRun) httpStatus(t *testing.T) {
	base := p.srv.a.URL
	p.open(t, p.srv.mainURL(false, ""), "")

	u404, u429 := base+"/e2e-status?code=404", base+"/e2e-status?code=429&retry=45"
	out := p.act(t, "navigate", p.window, u404)
	expectContains(t, "navigate 404", out, "ok "+u404+"\nNote: the Page answered HTTP 404 Not Found\n")
	out = p.act(t, "navigate", p.window, u429)
	expectContains(t, "navigate 429", out, "Note: the Page answered HTTP 429 Too Many Requests\n")
	if strings.Contains(strings.ToLower(out), "retry-after") {
		t.Errorf("navigate printed Retry-After, which needs Network.enable:\n%s", out)
	}
	out = p.act(t, "back", p.window)
	expectContains(t, "back to the 404", out, "Note: the Page answered HTTP 404 Not Found")
	out = p.act(t, "reload", p.window)
	expectContains(t, "reload the 404", out, "Note: the Page answered HTTP 404 Not Found")
	out = p.act(t, "forward", p.window)
	expectContains(t, "forward to the 429", out, "Note: the Page answered HTTP 429 Too Many Requests")

	empty := base + "/e2e-status?code=503&empty=1"
	out = p.act(t, "navigate", p.window, empty)
	expectContains(t, "navigate to an empty 503", out, "ok "+empty+"\nNote: the Page answered HTTP 503 Service Unavailable with an empty body; Chrome shows its error page\n")

	out = p.act(t, "--json", "navigate", p.window, base+"/e2e-redirect?n=2")
	expectContains(t, "--json navigate through redirects to a 404", out, `"http_status":404`)

	out = p.act(t, "navigate", p.window, p.srv.nextURL())
	if strings.Contains(out, "answered HTTP") {
		t.Errorf("a 200 page got the status note:\n%s", out)
	}
	p.expectEval(t, "document.title", pagesNextTitle)
}
