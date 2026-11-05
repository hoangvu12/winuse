//go:build e2e

// The Page read's live comparison (speed issue 07): the same Tab read
// through DOMSnapshot.captureSnapshot and through the clickables.js pass it
// replaced, on a real browser, comparing the marks and the rendered
// Elements and timing both. It reads in process, through internal/page,
// with no winuse binary and no helper.
//
// TestPageReadSnapshot starts each installed Chrome and Edge itself,
// headless on a throwaway profile (startIsolatedBrowser), and reads
// testdata/page-marks.html (every rule of clickables.js), browser-pages.html
// with its cross-origin iframe, and any page listed in
// WINUSE_E2E_PAGE_URLS (comma separated; real sites, so opt-in).
//
// TestPageReadSnapshotExtension does the same through every connected
// winuse extension bridge (Chrome, Edge, Brave), in one background tab it
// opens and closes in the user's own browser, so it also needs
// WINUSE_E2E_USER_BROWSER=1.
//
// User-run only (see the package comment); an agent must never start it.

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/bridge"
	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// pageReadRuns is how many times each way reads each page for the timing.
const pageReadRuns = 7

func TestPageReadSnapshot(t *testing.T) {
	optIn(t)
	sys := browser.Live()
	srv := startPageServers(t)
	ran := 0
	for _, name := range []string{"chrome", "edge"} {
		b, ok := installedBrowsers(sys)[name]
		if !ok {
			t.Logf("%s is not installed in a standard location; skipped", browser.DisplayName(name))
			continue
		}
		ran++
		t.Run(name, func(t *testing.T) {
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
			// Closed the browser's own way before startIsolatedBrowser's cleanup
			// terminates it, so its helper processes let go of the profile.
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
			conn := &browser.Connection{Browser: name, Mode: browser.ModeLaunch, WSURL: ws, Port: port, Client: c}
			for _, u := range pageReadURLs(srv) {
				comparePageReads(t, ctx, conn, u)
			}
		})
	}
	if ran == 0 {
		t.Skip("neither Chrome nor Edge is installed; nothing was run")
	}
}

func TestPageReadSnapshotExtension(t *testing.T) {
	optIn(t)
	if !userBrowserOptIn() {
		t.Skip("opens a tab in the user's own browser: set WINUSE_E2E_USER_BROWSER=1 only when the user agreed to it")
	}
	sys := browser.Live()
	srv := startPageServers(t)
	infos := sys.Bridges()
	if len(infos) == 0 {
		t.Skip("no winuse extension is connected (winuse browser ext status); nothing was run")
	}
	for _, info := range infos {
		name := sys.Env.IdentifyImage(info.BrowserExe)
		t.Run(fmt.Sprintf("%s-%d", name, info.BrowserPID), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			bc, err := bridge.Dial(ctx, info.Addr(), info.Token)
			if err != nil {
				t.Skipf("the %s extension bridge did not answer: %v", name, err)
			}
			c := cdp.NewClient(bc)
			t.Cleanup(func() { c.Close() })
			conn := &browser.Connection{Browser: name, Mode: browser.ModeExtension, PID: uint32(info.BrowserPID), Client: c}
			// Each page in a background tab of the agent's group, closed after;
			// the user's own tab stays selected.
			for _, u := range pageReadURLs(srv) {
				comparePageReads(t, ctx, conn, u)
			}
		})
	}
}

// pageReadURLs are the pages both tests read.
func pageReadURLs(srv *pageServers) []string {
	urls := []string{srv.a.URL + "/page-marks.html", srv.mainURL(true, "")}
	for _, u := range strings.Split(os.Getenv("WINUSE_E2E_PAGE_URLS"), ",") {
		if u = strings.TrimSpace(u); u != "" {
			urls = append(urls, u)
		}
	}
	return urls
}

// comparePageReads opens u in a new tab (openAnsweringTab), then reads it
// both ways and closes the tab: the
// clickables and passwords must match, and so must the rendered Elements.
// A password field only the snapshot sees (in a shadow tree or an iframe
// in the Tab's tree) is logged, not failed: speed issue 07 adds those on
// purpose.
func comparePageReads(t *testing.T, ctx context.Context, conn *browser.Connection, u string) {
	t.Helper()
	start := time.Now()
	targetID := openAnsweringTab(t, ctx, conn, u)
	t.Logf("%s: loaded in tab %s at %v", u, targetID, time.Since(start).Round(time.Millisecond))
	defer func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer ccancel()
		if _, err := conn.Client.Call(cctx, "Target.closeTarget", map[string]any{"targetId": targetID}); err != nil {
			t.Errorf("closing the test's tab %s: %v", targetID, err)
		}
	}()
	tab := page.Tab{TargetID: targetID, URL: u}

	read := func(js bool) (*page.Raw, []time.Duration) {
		r := page.NewReader()
		r.JSMarks = js
		var raw *page.Raw
		var times []time.Duration
		for i := 0; i < pageReadRuns+1; i++ {
			start := time.Now()
			rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			got, err := r.ReadTab(rctx, conn, tab)
			cancel()
			if err != nil {
				t.Fatalf("%s read %d of %s (js=%v) after %v: %v", conn.Mode, i, u, js, time.Since(start).Round(time.Millisecond), err)
			}
			if i > 0 { // the first attaches and turns the domains on
				times = append(times, time.Since(start))
			}
			raw = got
		}
		return raw, times
	}
	snap, st := read(false)
	pass, pt := read(true)
	t.Logf("%s %s: snapshot read median %v (%v), clickables.js read median %v (%v)", conn.Mode, u, median(st), st, median(pt), pt)

	docs := func(raw *page.Raw) map[string]*page.Doc {
		m := map[string]*page.Doc{"main": &raw.Main}
		for i := range raw.Frames {
			m[fmt.Sprintf("iframe %d", raw.Frames[i].Owner)] = &raw.Frames[i].Doc
		}
		return m
	}
	sd, pd := docs(snap), docs(pass)
	if len(sd) != len(pd) {
		t.Errorf("%s: %d documents through the snapshot, %d through the pass", u, len(sd), len(pd))
	}
	for k, p := range pd {
		s := sd[k]
		if s == nil {
			continue
		}
		if s.Marks == nil || p.Marks == nil {
			t.Errorf("%s %s: marks snapshot=%v pass=%v", u, k, s.Marks != nil, p.Marks != nil)
			continue
		}
		if !s.Snapshot {
			// The read fell back to the pass: this compares nothing.
			t.Errorf("%s %s: the read's marks did not come from DOMSnapshot.captureSnapshot (refused or failed over %s)", u, k, conn.Mode)
		}
		if !reflect.DeepEqual(s.Marks.Clickables, p.Marks.Clickables) {
			t.Errorf("%s %s: the clickables differ\n%s", u, k, clickableDiff(s.Marks.Clickables, p.Marks.Clickables))
		}
		extra, missing := setDiff(s.Marks.Passwords, p.Marks.Passwords)
		if len(missing) > 0 {
			t.Errorf("%s %s: password fields the pass found and the snapshot did not: %v", u, k, missing)
		}
		if len(extra) > 0 {
			t.Logf("%s %s: password fields only the snapshot found (shadow trees, iframes in the tree): %v", u, k, extra)
		}
	}
	lim := page.Limits{MaxNodes: page.MaxNodes - 1, MaxDepth: page.MaxDepth - 1, MaxChildren: page.MaxChildren, NodeLabel: page.MaxNodes, DepthLabel: page.MaxDepth}
	a := wstate.Render(&wstate.Tree{Elements: page.Build(snap, lim).Elements})
	b := wstate.Render(&wstate.Tree{Elements: page.Build(pass, lim).Elements})
	if a != b {
		t.Errorf("%s: the Window State's Page differs between the snapshot and the pass\n%s", u, lineDiff(a, b))
	}
}

// openAnsweringTab opens u in a new background tab and waits for it to
// load, every call bounded. A new headless Chrome tab now and then never
// answers its session at all (the 2026-11-05 runs: the attach's setup timed
// out, every Runtime.evaluate hung): such a tab is closed and another one
// opened, with a log line, three tries in all.
func openAnsweringTab(t *testing.T, ctx context.Context, conn *browser.Connection, u string) string {
	t.Helper()
	c := conn.Client
	for try := 1; try <= 3; try++ {
		res, err := c.Call(ctx, "Target.createTarget", map[string]any{"url": u})
		if err != nil {
			t.Fatalf("Target.createTarget %s: %v", u, err)
		}
		var target struct {
			TargetID string `json:"targetId"`
		}
		json.Unmarshal(res, &target)
		if tabLoaded(ctx, conn, target.TargetID, u, 10*time.Second) {
			time.Sleep(time.Second) // late script, web fonts and the iframe's attach
			return target.TargetID
		}
		t.Logf("%s: the new tab %s did not answer within 10s (try %d of 3); closing it and opening another", u, target.TargetID, try)
		cctx, ccancel := context.WithTimeout(ctx, 5*time.Second)
		c.Call(cctx, "Target.closeTarget", map[string]any{"targetId": target.TargetID})
		ccancel()
	}
	t.Fatalf("%s: three new tabs in a row did not answer", u)
	return ""
}

// tabLoaded attaches a session of its own to the tab and polls its
// readyState, each call bounded, until it shows u complete or limit
// passes.
func tabLoaded(ctx context.Context, conn *browser.Connection, targetID, u string, limit time.Duration) bool {
	c := conn.Client
	lctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	sess, err := conn.AttachPage(lctx, targetID)
	if err != nil {
		return false
	}
	defer func() {
		dctx, dcancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer dcancel()
		c.Call(dctx, "Target.detachFromTarget", map[string]any{"sessionId": sess})
	}()
	for lctx.Err() == nil {
		ectx, ecancel := context.WithTimeout(lctx, time.Second)
		res, err := c.CallSession(ectx, sess, "Runtime.evaluate", map[string]any{"expression": "document.readyState + ' ' + location.href", "returnByValue": true})
		ecancel()
		var v struct {
			Result struct {
				Value string `json:"value"`
			} `json:"result"`
		}
		if err == nil && json.Unmarshal(res, &v) == nil && strings.HasPrefix(v.Result.Value, "complete ") && !strings.HasSuffix(v.Result.Value, "about:blank") {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func clickableDiff(snap, pass []page.Clickable) string {
	s := map[int64]string{}
	for _, c := range snap {
		s[c.BackendNodeID] = c.Text
	}
	p := map[int64]string{}
	for _, c := range pass {
		p[c.BackendNodeID] = c.Text
	}
	var out []string
	for id, text := range p {
		if st, ok := s[id]; !ok {
			out = append(out, fmt.Sprintf("  only the pass: %d %q", id, text))
		} else if st != text {
			out = append(out, fmt.Sprintf("  text of %d: snapshot %q, pass %q", id, st, text))
		}
	}
	for id, text := range s {
		if _, ok := p[id]; !ok {
			out = append(out, fmt.Sprintf("  only the snapshot: %d %q", id, text))
		}
	}
	if len(out) == 0 {
		out = append(out, "  same set, another order")
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

func setDiff(a, b []int64) (onlyA, onlyB []int64) {
	in := func(s []int64, v int64) bool {
		for _, x := range s {
			if x == v {
				return true
			}
		}
		return false
	}
	for _, v := range a {
		if !in(b, v) {
			onlyA = append(onlyA, v)
		}
	}
	for _, v := range b {
		if !in(a, v) {
			onlyB = append(onlyB, v)
		}
	}
	return
}

// lineDiff lists the lines one rendering has and the other lacks, compared
// without their Element Indexes, so one extra line does not shift every
// line after it into the diff.
func lineDiff(a, b string) string {
	index := regexp.MustCompile(`^(\t*)\d+ |(element is )\d+ `)
	count := func(s string) map[string]int {
		m := map[string]int{}
		for _, l := range strings.Split(s, "\n") {
			m[index.ReplaceAllString(l, "$1$2")]++
		}
		return m
	}
	ca, cb := count(a), count(b)
	var out []string
	for l, n := range ca {
		if n > cb[l] {
			out = append(out, "  snapshot only: "+l)
		}
	}
	for l, n := range cb {
		if n > ca[l] {
			out = append(out, "  pass only:     "+l)
		}
	}
	sort.Strings(out)
	if len(out) > 60 {
		out = append(out[:60], fmt.Sprintf("  ... %d more", len(out)-60))
	}
	return strings.Join(out, "\n")
}

func median(d []time.Duration) time.Duration {
	if len(d) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
