package helper

// launch-app --cdp's offline tests (headless-max 15): an Electron
// executable gets the port switch, any other executable the WebView2
// variable; the port is awaited for the launched process and the Window's
// process; the note says whether a Browser Connection reaches the App;
// --cdp on a shortcut or without Browser Connections starts nothing; and a
// registered App's Window is read through its Page. Nothing starts and no
// port is opened.

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/browser"
	"github.com/hoangvu12/winuse/internal/win"
)

// appBrowsers is a browser service with the App side: the port it hands
// out, whether the App's port answers, and the AwaitApp calls.
type appBrowsers struct {
	fakeBrowsers
	answers bool
	awaits  []appAwait
}

type appAwait struct {
	kind                string
	launched, windowPID uint32
	port                int
	wait                time.Duration
}

func (b *appBrowsers) AppPort() (int, error) { return 51234, nil }

func (b *appBrowsers) AwaitApp(ctx context.Context, kind string, launched, windowPID uint32, port int, wait time.Duration) (browser.AppEndpoint, bool) {
	b.awaits = append(b.awaits, appAwait{kind, launched, windowPID, port, wait})
	if !b.answers {
		return browser.AppEndpoint{}, false
	}
	return browser.AppEndpoint{Kind: kind, PID: windowPID, Owner: launched, Port: port, WSURL: "ws://x"}, true
}

// electronApp is an Electron App's executable in a temp folder: an
// app.asar sits in resources\ next to it.
func electronApp(t *testing.T) string {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "resources", "app.asar"))
	exe := filepath.Join(dir, "demo.exe")
	writeTestFile(t, exe)
	return exe
}

func TestLaunchAppCDPOnElectron(t *testing.T) {
	l := newLaunchSpy()
	exe := electronApp(t)
	b := &appBrowsers{answers: true}
	svc := l.services()
	svc.Browser = b
	out, e := CommandsWith(svc)["launch-app"](context.Background(), []byte(`{"app":`+jsonString(exe)+`,"cdp":true,"args":[".","--x"]}`))
	if e != nil {
		t.Fatalf("launch-app --cdp: %+v", e)
	}
	want := "300\tTool\tTool\nNote: a Browser Connection reaches the App's pages (Electron App, port 51234): get-window-state 300 reads them through it\n"
	if out != want {
		t.Errorf("out = %q, want %q", out, want)
	}
	opts := l.starts[0].opts
	if !slices.Equal(opts.Args, []string{"--remote-debugging-port=51234", ".", "--x", "--disable-features=CalculateNativeWinOcclusion"}) || opts.Env != nil {
		t.Errorf("launcher got %+v", opts)
	}
	if len(b.awaits) != 1 || b.awaits[0].kind != browser.AppElectron || b.awaits[0].launched != 77 || b.awaits[0].windowPID != 77 || b.awaits[0].port != 51234 {
		t.Errorf("awaits = %+v", b.awaits)
	}
	if w := b.awaits[0].wait; w <= 0 || w > launchPollBudget {
		t.Errorf("waited %s, want what is left of the launch budget", w)
	}
}

func TestLaunchAppCDPOnWebView2WithoutAPort(t *testing.T) {
	l := newLaunchSpy()
	b := &appBrowsers{}
	svc := l.services()
	svc.Browser = b
	out, e := CommandsWith(svc)["launch-app"](context.Background(), []byte(`{"app":`+jsonString(fakeAppPath)+`,"cdp":true,"env":["A=1"]}`))
	if e != nil {
		t.Fatalf("launch-app --cdp: %+v", e)
	}
	if want := "300\tTool\tTool\nNote: no debugging port appeared, so the App stays on UI Automation\n"; out != want {
		t.Errorf("out = %q, want %q", out, want)
	}
	opts := l.starts[0].opts
	if opts.Args != nil || len(opts.Env) != 2 || opts.Env[0] != "A=1" ||
		!strings.HasPrefix(opts.Env[1], browser.WebView2ArgsVar+"=") || !strings.HasSuffix(opts.Env[1], "--remote-debugging-port=51234") {
		t.Errorf("launcher got %+v", opts)
	}
	if len(b.awaits) != 1 || b.awaits[0].kind != browser.AppWebView2 {
		t.Errorf("awaits = %+v", b.awaits)
	}
}

func TestLaunchAppCDPRefusalsStartNothing(t *testing.T) {
	l := newLaunchSpy()
	svc := l.services()
	svc.Browser = &fakeBrowsers{} // no App side
	_, e := CommandsWith(svc)["launch-app"](context.Background(), []byte(`{"app":`+jsonString(fakeAppPath)+`,"cdp":true}`))
	if e == nil || e.Msg != "--cdp is unavailable: this winuse helper has no Browser Connections; nothing was started" {
		t.Errorf("no App side: %+v", e)
	}
	svc.Browser = &appBrowsers{}
	_, e = CommandsWith(svc)["launch-app"](context.Background(), []byte(`{"app":"C:\\nowhere\\App.lnk","cdp":true}`))
	if e == nil || e.Msg != `--cdp needs an executable, and C:\nowhere\App.lnk starts a shortcut (C:\nowhere\App.lnk); nothing was started` {
		t.Errorf("shortcut: %+v", e)
	}
	if len(l.starts) != 0 {
		t.Errorf("started %+v", l.starts)
	}
	// Without --cdp nothing changes.
	out, e := CommandsWith(svc)["launch-app"](context.Background(), []byte(`{"app":`+jsonString(fakeAppPath)+`}`))
	if e != nil || out != "300\tTool\tTool\n" || l.starts[0].opts.Env != nil {
		t.Errorf("plain launch: %q, %+v, %+v", out, e, l.starts)
	}
}

// appPages is a pageBrowsers that knows one App process.
type appPages struct {
	pageConns
	kinds map[uint32]string
}

func (a appPages) AppKind(pid uint32) string { return a.kinds[pid] }

// A Window of a process launched with --cdp is read through its Page,
// whatever its window class; the App's other processes and every other
// non-browser Window stay on UI Automation.
func TestAppWindowIsAPageWindow(t *testing.T) {
	lp := &livePages{browsers: appPages{pageConns: pageConns{identify: map[string]string{chromeExe: "chrome"}}, kinds: map[uint32]string{77: browser.AppWebView2}}}
	for _, c := range []struct {
		w    win.WindowInfo
		want string
	}{
		{win.WindowInfo{ID: 300, PID: 77, Class: "WindowsForms10.Window.8.app.0.1", ExePath: fakeAppPath}, browser.AppWebView2},
		{win.WindowInfo{ID: 301, PID: 78, Class: "WindowsForms10.Window.8.app.0.1", ExePath: fakeAppPath}, ""},
		{win.WindowInfo{ID: 302, PID: 4242, Class: chromiumWindowClass, ExePath: chromeExe}, "chrome"},
		{win.WindowInfo{ID: 303, PID: 0, Class: "Notepad"}, ""},
	} {
		if got := lp.Browser(c.w); got != c.want {
			t.Errorf("window %d: Browser = %q, want %q", c.w.ID, got, c.want)
		}
	}
}

// writeTestFile creates an empty file and its folders.
func writeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}
