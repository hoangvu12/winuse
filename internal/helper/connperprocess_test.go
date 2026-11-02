package helper

// A Browser Connection per browser process, through the helper: the
// user's Chrome (pid 5151, through the fake winuse extension) and a Chrome
// winuse launched (pid 4242, pagetest) served by one browser.Manager over a
// faked System. Each Window reads its own Page, lists its own Tabs and
// navigates on its own connection, in either order, with the pin set and
// after a helper restart; a Window of a third Chrome process says which
// processes winuse can reach. No browser runs.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/bridge"
	"github.com/hoangvu12/winuse/internal/bridge/bridgetest"
	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/page/pagetest"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// The Windows of the two Chromes, and of a third one winuse cannot reach.
var (
	launchedWindow = tabTestWindows[0] // 301: pid 4242, browser window 1
	userWindow     = win.WindowInfo{ID: 401, Class: chromiumWindowClass, PID: 5151, App: "Google Chrome", ExePath: chromeExe, Title: "Mail - Google Chrome",
		Bounds: win.Rect{Left: 600, Top: 300, Right: 1400, Bottom: 800}, Visible: true}
	strayWindow = win.WindowInfo{ID: 501, Class: chromiumWindowClass, PID: 777, App: "Google Chrome", ExePath: chromeExe, Title: "Other - Google Chrome",
		Bounds: win.Rect{Left: 0, Top: 0, Right: 800, Bottom: 600}, Visible: true}
)

const (
	tMailUser = "M1000000000000000000000000000001"
	tBankUser = "M2000000000000000000000000000002"
)

// twoChromes is the machine: the System both helpers' Managers run on.
type twoChromes struct {
	sys      *browser.System
	launched *pagetest.Browser
	nav      *winRig // the launched Chrome's Page handlers
	ext      *bridgetest.Extension

	mu      sync.Mutex
	userURL string
	loader  string
}

func newTwoChromes(t *testing.T) *twoChromes {
	t.Helper()
	root := t.TempDir()
	env := browser.Env{
		LocalAppData:    filepath.Join(root, "Local"),
		AppData:         filepath.Join(root, "Roaming"),
		ProgramFiles:    filepath.Join(root, "Program Files"),
		ProgramFilesX86: filepath.Join(root, "Program Files (x86)"),
	}
	m := &twoChromes{launched: launchedBrowser(t), userURL: "https://mail.example/", loader: "ML1"}
	m.nav = &winRig{loader: "L1", url: "https://docs.test/", ready: "complete", entries: []page.HistoryEntry{{ID: 7, URL: "https://docs.test/"}}}
	m.nav.handlers(m.launched.Srv)
	port := m.launched.Srv.Port()
	sys := &browser.System{
		Env:      env,
		StateDir: filepath.Join(env.LocalAppData, "winuse", "browser"),
		Exists: func(p string) bool {
			_, err := os.Stat(p)
			return err == nil
		},
		VersionWS: func(ctx context.Context, p int) (string, error) {
			if p == port {
				return m.launched.Srv.URL(), nil
			}
			return "", errors.New("nothing listens")
		},
		PortAlive:    func(p int) bool { return p == port },
		ListenerPID:  func(p int) (uint32, bool) { return 4242, p == port },
		ProcessImage: func(pid uint32) (string, error) { return chromeExe, nil },
		Running:      func(string) bool { return true },
		Dial: func(ctx context.Context, u string, handshake time.Duration) (cdp.Transport, error) {
			return cdp.DialWS(ctx, u, handshake)
		},
		OpenBridge: func(ctx context.Context, i bridge.Info) (cdp.Transport, error) {
			return bridge.Dial(ctx, i.Addr(), i.Token)
		},
		Now:   time.Now,
		Sleep: time.Sleep,
	}
	sys.Bridges = func() []bridge.Info { return bridge.List(sys.BridgesDir(), nil) }
	m.sys = sys
	// winuse's launch profile records the launched Chrome's port.
	dir := sys.LaunchProfileDir("chrome")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "winuse-port"), []byte(strconv.Itoa(port)), 0o644); err != nil {
		t.Fatal(err)
	}
	// The pin names Chrome; it decides nothing for a Window.
	if err := sys.SetPin("chrome"); err != nil {
		t.Fatal(err)
	}
	m.ext = bridgetest.Start(t, sys.BridgesDir(), &bridgetest.Extension{
		Browser: "chrome", Instance: "ab12cd34", BrowserPID: 5151, BrowserExe: chromeExe,
		Tabs: []bridgetest.Tab{
			{TargetID: tMailUser, Title: "Mail", URL: "https://mail.example/", TabID: 31, WindowID: 7, Active: true},
			{TargetID: tBankUser, Title: "Bank", URL: "https://bank.example/", TabID: 32, WindowID: 7},
		},
		Windows: []bridgetest.Window{{WindowID: 7, Left: 600, Top: 300, Width: 800, Height: 500, State: "normal"}},
		Page:    m.userPage,
	})
	return m
}

// userPage answers the user's Chrome's Tab session: the reads navigate
// makes, and the navigation itself.
func (m *twoChromes) userPage(sid, method string, params json.RawMessage) (any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch method {
	case "Page.navigate":
		var p struct {
			URL string `json:"url"`
		}
		json.Unmarshal(params, &p)
		m.userURL, m.loader = p.URL, "ML2"
		return map[string]any{"frameId": "MF", "loaderId": "ML2"}, nil
	case "Page.getFrameTree":
		return map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "MF", "loaderId": m.loader, "url": m.userURL}}}, nil
	case "Runtime.evaluate":
		var p struct {
			Expression string `json:"expression"`
		}
		json.Unmarshal(params, &p)
		switch p.Expression {
		case "document.readyState":
			return map[string]any{"result": map[string]any{"type": "string", "value": "complete"}}, nil
		case "location.href":
			return map[string]any{"result": map[string]any{"type": "string", "value": m.userURL}}, nil
		}
	}
	return nil, nil
}

// helper is one helper process over the machine: its own Manager.
func (m *twoChromes) helper(t *testing.T) (*tabRig, *livePages, *browser.Manager) {
	t.Helper()
	mgr := browser.NewManager(m.sys)
	t.Cleanup(func() { mgr.Close() })
	lp := &livePages{browsers: mgr, reader: page.NewReader(), geometry: tabGeometry}
	lp.reader.Settle = time.Millisecond
	r := newTabRig(t, nil, nil, m.launched, nil)
	r.svc.Pages = lp
	r.svc.Windows = func() ([]win.WindowInfo, error) {
		return []win.WindowInfo{launchedWindow, userWindow, strayWindow}, nil
	}
	r.cmds = CommandsWith(r.svc)
	t.Cleanup(func() {
		for _, w := range []struct {
			w       win.WindowInfo
			session string
		}{{launchedWindow, "session-" + tDocs}, {userWindow, tMailUser}} {
			if c, err := mgr.WindowConnection(context.Background(), "chrome", w.w.PID); err == nil {
				page.Unwatch(c.Client, w.session)
			}
		}
	})
	return r, lp, mgr
}

// userCommands lists what reached the user's Chrome, as "method@session".
func (m *twoChromes) userCommands() []string {
	var out []string
	for _, c := range m.ext.Commands() {
		out = append(out, c.Method+"@"+c.SessionID)
	}
	return out
}

// launchedCommands lists what reached the launched Chrome, likewise.
func (m *twoChromes) launchedCommands() []string {
	var out []string
	for _, c := range m.launched.Srv.Received() {
		out = append(out, c.Method+"@"+c.SessionID)
	}
	return out
}

const (
	launchedTabs = "  cccc0000 \"News\" https://news.test/\n* bbbb0000 \"Docs\" https://docs.test/\n  aaaa0000 \"Inbox\" https://mail.test/\n"
	userTabs     = "* m1000000 \"Mail\" https://mail.example/\n  m2000000 \"Bank\" https://bank.example/\n"
)

func TestEachWindowUsesItsOwnBrowserProcess(t *testing.T) {
	for _, first := range []win.WindowInfo{userWindow, launchedWindow} {
		name := "the user's Chrome first"
		if first.ID == launchedWindow.ID {
			name = "the launched Chrome first"
		}
		t.Run(name, func(t *testing.T) {
			m := newTwoChromes(t)
			r, lp, mgr := m.helper(t)
			order := []win.WindowInfo{first, userWindow}
			if first.ID == userWindow.ID {
				order[1] = launchedWindow
			}
			ctx := context.Background()
			for _, w := range order {
				id := strconv.FormatUint(w.ID, 10)
				want, wantTab := launchedTabs, tDocs
				if w.ID == userWindow.ID {
					want, wantTab = userTabs, tMailUser
				}
				// The Page read finds the Window's active Tab on its own
				// connection.
				if got, err := lp.PageActiveTab(ctx, w); err != nil || got != wantTab {
					t.Fatalf("window %s's active Tab = %q, %v", id, got, err)
				}
				if out, e := r.run(t, "tab-list", protocol.TabArgs{Window: id}); e != nil || out != want {
					t.Fatalf("tab list %s = %q, %+v", id, out, e)
				}
				if out, e := r.run(t, "navigate", protocol.NavigateArgs{Window: id, URL: "https://w" + id + ".test/"}); e != nil || out != "ok https://w"+id+".test/\n" {
					t.Fatalf("navigate %s = %q, %+v", id, out, e)
				}
				rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
				lp.ReadPage(rctx, w, "chrome") // the reads land where they belong; the fakes render no Page
				cancel()
			}

			// No cross-talk: each browser got only its own Tab's commands.
			for _, c := range m.launchedCommands() {
				if strings.Contains(c, tMailUser) || strings.HasPrefix(c, "Winuse.") {
					t.Errorf("the launched Chrome got %s", c)
				}
			}
			for _, c := range m.userCommands() {
				if strings.Contains(c, tDocs) || strings.HasPrefix(c, "Browser.") {
					t.Errorf("the user's Chrome got %s", c)
				}
			}
			for _, want := range []string{"Page.navigate@session-" + tDocs, "Accessibility.getFullAXTree@session-" + tDocs} {
				if !slices.Contains(m.launchedCommands(), want) {
					t.Errorf("the launched Chrome never got %s: %v", want, m.launchedCommands())
				}
			}
			for _, want := range []string{"Page.navigate@" + tMailUser, "Accessibility.getFullAXTree@" + tMailUser} {
				if !slices.Contains(m.userCommands(), want) {
					t.Errorf("the user's Chrome never got %s: %v", want, m.userCommands())
				}
			}
			if got := m.nav.sent("Page.navigate"); len(got) != 1 || !strings.Contains(got[0], "w301.test") {
				t.Errorf("the launched Chrome navigated %v", got)
			}

			// The two connections: one per process, both still held.
			a, err := mgr.WindowConnection(ctx, "chrome", launchedWindow.PID)
			b, err2 := mgr.WindowConnection(ctx, "chrome", userWindow.PID)
			if err != nil || err2 != nil || a.Mode != browser.ModeLaunch || b.Mode != browser.ModeExtension || !a.Client.Alive() || !b.Client.Alive() {
				t.Errorf("connections = %+v %v, %+v %v", a, err, b, err2)
			}
			if n := m.launched.Srv.Upgrades(); n != 1 {
				t.Errorf("handshakes to the launched Chrome = %d, want 1", n)
			}

			// A helper restart: a new Manager maps each Window again.
			r2, _, _ := m.helper(t)
			for _, w := range order {
				id := strconv.FormatUint(w.ID, 10)
				want := launchedTabs
				if w.ID == userWindow.ID {
					want = userTabs
				}
				if out, e := r2.run(t, "tab-list", protocol.TabArgs{Window: id}); e != nil || out != want {
					t.Errorf("after the restart, tab list %s = %q, %+v", id, out, e)
				}
			}
		})
	}
}

func TestAWindowOfAChromeProcessWinuseCannotReach(t *testing.T) {
	m := newTwoChromes(t)
	r, lp, _ := m.helper(t)
	_, e := r.run(t, "tab-list", protocol.TabArgs{Window: "501"})
	if e == nil || e.Msg != "window 501 belongs to Chrome process 777, which has no Browser Connection (winuse can reach Chrome processes 4242 and 5151)" ||
		e.Hint != "if it is the user's own Chrome, run winuse browser ext install chrome to reach it through the winuse extension; otherwise work in a window of Chrome processes 4242 and 5151" {
		t.Errorf("tab list: %+v", e)
	}
	_, e = r.run(t, "navigate", protocol.NavigateArgs{Window: "501", URL: "a.test"})
	if e == nil || !strings.HasPrefix(e.Msg, "window 501 belongs to Chrome process 777") || !strings.HasSuffix(e.Msg, nothingSentNote) {
		t.Errorf("navigate: %+v", e)
	}
	_, err := lp.ReadPage(context.Background(), strayWindow, "chrome")
	want := "this Chrome window belongs to Chrome process 777, which has no debugging connection winuse can use (winuse can reach Chrome processes 4242 and 5151); " +
		"if it is the user's own Chrome, run winuse browser ext install chrome to reach it through the winuse extension; otherwise work in a window of Chrome processes 4242 and 5151"
	if got := page.Reason(err); got != want {
		t.Errorf("note reason = %q", got)
	}
	// The two Chromes winuse reaches are still served.
	if out, e := r.run(t, "tab-list", protocol.TabArgs{Window: "401"}); e != nil || out != userTabs {
		t.Errorf("the user's Chrome = %q, %+v", out, e)
	}
}

// TestAWindowStateNamesTheApprovalPrompt: the first read of a Chrome that
// holds its handshake for the "Allow remote debugging?" prompt (longer than
// the Page read's budget, shorter than browser.ApprovalWait) falls back to
// UI Automation with a note that says the user must approve it, not that
// the read took too long; and the read does not wait for the approval.
func TestAWindowStateNamesTheApprovalPrompt(t *testing.T) {
	m := newTwoChromes(t)
	release := m.launched.Srv.Hold()
	defer release()
	r, _, mgr := m.helper(t)
	if mgr.ApprovalWait <= pageBudget {
		t.Fatalf("ApprovalWait %s is within the Page budget %s: the case this covers is gone", mgr.ApprovalWait, pageBudget)
	}
	ctx, cancel := context.WithTimeout(context.Background(), pageMargin+300*time.Millisecond)
	defer cancel()
	start := time.Now()
	tree, e := readWindowTree(r.svc, ctx, launchedWindow)
	if e != nil {
		t.Fatal(e.Error())
	}
	if d := time.Since(start); d > pageMargin+300*time.Millisecond {
		t.Errorf("the read took %s, past its request budget", d)
	}
	p := &browser.Problem{Kind: browser.AwaitingApproval, Browser: "chrome"}
	want := page.FallbackNote(p.Sentence() + "; " + p.Next())
	if got := tree.Notes[len(tree.Notes)-1]; got != want || !strings.Contains(got, "Allow remote debugging?") {
		t.Errorf("note = %q\nwant %q", got, want)
	}
}
