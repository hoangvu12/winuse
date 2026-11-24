package page

// Golden files for extract.js (ticket 26): each case loads a fixture page
// from testdata/extract into a headless browser of its own (a temporary
// profile, no window, file:// pages only), runs Extract on it as `winuse
// extract` does (in an isolated world; with --root through a node's
// backendNodeId) and compares extract.Render's text with
// testdata/extract/<case>.golden. `go test ./internal/page -run
// TestExtractGolden -update` rewrites the golden files.
//
// The browser is Chrome or Edge from their usual places, or the one
// WINUSE_TEST_BROWSER names; with none, or under -short, the cases skip.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/extract"
	"github.com/hoangvu12/winuse/internal/win"
)

// extractCases are the golden cases: a fixture, the flags, and the CSS
// selector of the --root node ("" for none).
var extractCases = []struct {
	name, fixture, as, root string
	limit                   int
}{
	{name: "search", fixture: "search"},
	{name: "search-root-list", fixture: "search", root: "#results"},
	{name: "search-root-ancestor", fixture: "search", root: "main"},
	{name: "search-root-item", fixture: "search", root: "#results > li:nth-child(3)"},
	{name: "search-root-link", fixture: "search", root: "#results > li:nth-child(5) h3 a"},
	{name: "search-limit", fixture: "search", limit: 4},
	{name: "grid", fixture: "grid"},
	{name: "table", fixture: "table"},
	{name: "table-as-table", fixture: "table", as: "table"},
	{name: "hn", fixture: "hn"},
	{name: "article", fixture: "article"},
	{name: "recipe", fixture: "recipe"},
	{name: "recipe-as-jsonld", fixture: "recipe", as: "jsonld"},
	{name: "recipe-root-ingredients", fixture: "recipe", root: "#ingredients"},
	{name: "wall", fixture: "wall"},
	{name: "qa", fixture: "qa"},
	{name: "repos", fixture: "repos"},
}

func TestExtractGolden(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: no browser")
	}
	exe := testBrowser()
	if exe == "" {
		t.Skip("no Chrome or Edge found; set WINUSE_TEST_BROWSER to run the extract golden cases")
	}
	c, session := headlessBrowser(t, exe)
	dir, _ := filepath.Abs(filepath.Join("testdata", "extract"))
	loaded := ""
	for _, tc := range extractCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if loaded != tc.fixture {
				loadFixture(t, ctx, c, session, filepath.Join(dir, tc.fixture+".html"))
				loaded = tc.fixture
			}
			var root *Origin
			if tc.root != "" {
				root = nodeOrigin(t, ctx, c, session, tc.root)
			}
			res, err := Extract(ctx, c, session, root, ExtractOptions{As: tc.as, Limit: tc.limit})
			if err != nil {
				t.Fatal(err)
			}
			hint := ""
			if root == nil {
				hint = "winuse get-window-state 301"
			}
			got, err := extract.Render(res, extract.Options{RootHint: hint})
			if err != nil {
				t.Fatal(err)
			}
			golden := filepath.Join(dir, tc.name+".golden")
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run with -update to write it)", err)
			}
			if got != strings.ReplaceAll(string(want), "\r\n", "\n") {
				t.Errorf("%s differs.\n--- got\n%s--- want\n%s", golden, got, want)
			}
		})
	}
}

// testBrowser is the browser the golden cases run in, "" for none.
func testBrowser() string {
	if p := os.Getenv("WINUSE_TEST_BROWSER"); p != "" {
		return p
	}
	for _, p := range []string{
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// headless starts the browser headless with a temporary profile and
// returns a client and the session of its one page.
func headlessBrowser(t *testing.T, exe string) (*cdp.Client, string) {
	t.Helper()
	profile := t.TempDir()
	// A GUI program; NoConsoleWindow keeps any console child of it windowless.
	cmd := win.NoConsoleWindow(exec.Command(exe, "--headless=new", "--user-data-dir="+profile, "--remote-debugging-port=0",
		"--no-first-run", "--no-default-browser-check", "--disable-sync", "--disable-extensions",
		"--disable-background-networking", "--window-size=1280,900", "about:blank"))
	if err := cmd.Start(); err != nil {
		t.Skipf("starting %s: %v", exe, err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
	portFile := filepath.Join(profile, "DevToolsActivePort")
	var lines []string
	for end := time.Now().Add(15 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		data, err := os.ReadFile(portFile)
		if err == nil {
			if lines = strings.Split(strings.TrimSpace(string(data)), "\n"); len(lines) == 2 {
				break
			}
		}
		if time.Now().After(end) {
			t.Fatalf("%s wrote no DevToolsActivePort", exe)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ws, err := cdp.DialWS(ctx, "ws://127.0.0.1:"+strings.TrimSpace(lines[0])+strings.TrimSpace(lines[1]), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c := cdp.NewClient(ws)
	t.Cleanup(func() { c.Close() })
	res, err := c.Call(ctx, "Target.createTarget", map[string]any{"url": "about:blank"})
	if err != nil {
		t.Fatal(err)
	}
	var tg struct {
		TargetID string `json:"targetId"`
	}
	json.Unmarshal(res, &tg)
	res, err = c.Call(ctx, "Target.attachToTarget", map[string]any{"targetId": tg.TargetID, "flatten": true})
	if err != nil {
		t.Fatal(err)
	}
	var at struct {
		SessionID string `json:"sessionId"`
	}
	json.Unmarshal(res, &at)
	return c, at.SessionID
}

// load opens a fixture and waits for it to load.
func loadFixture(t *testing.T, ctx context.Context, c *cdp.Client, session, file string) {
	t.Helper()
	u := url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(file)}
	if _, err := c.CallSession(ctx, session, "Page.navigate", map[string]any{"url": u.String()}); err != nil {
		t.Fatal(err)
	}
	for {
		var state string
		if v, err := fixtureEval(ctx, c, session, "document.readyState+'|'+location.href"); err == nil {
			json.Unmarshal(v, &state)
		}
		if strings.HasPrefix(state, "complete|file:") {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("%s did not load (%s)", file, state)
		case <-time.After(30 * time.Millisecond):
		}
	}
}

func fixtureEval(ctx context.Context, c *cdp.Client, session, expr string) (json.RawMessage, error) {
	res, err := c.CallSession(ctx, session, "Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true})
	if err != nil {
		return nil, err
	}
	var r struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
	}
	return r.Result.Value, json.Unmarshal(res, &r)
}

// nodeOrigin is the Origin of the node a selector finds, as a Window
// State would record it.
func nodeOrigin(t *testing.T, ctx context.Context, c *cdp.Client, session, sel string) *Origin {
	t.Helper()
	res, err := c.CallSession(ctx, session, "Runtime.evaluate", map[string]any{"expression": fmt.Sprintf("document.querySelector(%q)", sel)})
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Result struct {
			ObjectID string `json:"objectId"`
		} `json:"result"`
	}
	json.Unmarshal(res, &r)
	if r.Result.ObjectID == "" {
		t.Fatalf("no node %s", sel)
	}
	res, err = c.CallSession(ctx, session, "DOM.describeNode", map[string]any{"objectId": r.Result.ObjectID})
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Node struct {
			BackendNodeID int64  `json:"backendNodeId"`
			FrameID       string `json:"frameId"`
		} `json:"node"`
	}
	json.Unmarshal(res, &d)
	frame, _, err := mainFrameOf(ctx, c, session)
	if err != nil {
		t.Fatal(err)
	}
	return &Origin{Client: c, SessionID: session, FrameID: frame, BackendNodeID: d.Node.BackendNodeID}
}
