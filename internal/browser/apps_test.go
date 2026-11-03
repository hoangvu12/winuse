package browser

// App Browser Connections' offline tests (headless-max 15): the Electron
// switches and the WebView2 environment variable, the Electron detection,
// the owner check (the launched process tree only), the wait for a port
// that never answers, and a registered App reached through its Window's
// process and only there. The machine is fakeMachine; no App starts.

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestElectronArgs(t *testing.T) {
	got := ElectronArgs(51234, []string{`C:\apps\demo`, "--enable-logging"})
	want := []string{"--remote-debugging-port=51234", `C:\apps\demo`, "--enable-logging", "--disable-features=CalculateNativeWinOcclusion"}
	if !slices.Equal(got, want) {
		t.Errorf("args = %q, want %q", got, want)
	}
	// The App's own --disable-features keeps its features, plus the one.
	got = ElectronArgs(51234, []string{"--disable-features=Foo"})
	want = []string{"--remote-debugging-port=51234", "--disable-features=Foo,CalculateNativeWinOcclusion"}
	if !slices.Equal(got, want) {
		t.Errorf("args = %q, want %q", got, want)
	}
}

func TestWebView2Env(t *testing.T) {
	const v = WebView2ArgsVar
	if got := WebView2Env(51234, []string{"A=1"}, ""); !slices.Equal(got, []string{"A=1", v + "=--remote-debugging-port=51234"}) {
		t.Errorf("plain: %q", got)
	}
	// A value the agent passed with --env wins over the inherited one, and
	// keeps its switches.
	got := WebView2Env(51234, []string{"webview2_additional_browser_arguments=--lang=fr", "B=2"}, "--inherited")
	if !slices.Equal(got, []string{"B=2", v + "=--lang=fr --remote-debugging-port=51234"}) {
		t.Errorf("--env value: %q", got)
	}
	if got := WebView2Env(51234, nil, "--inherited"); !slices.Equal(got, []string{v + "=--inherited --remote-debugging-port=51234"}) {
		t.Errorf("inherited: %q", got)
	}
}

func TestIsElectron(t *testing.T) {
	root := t.TempDir()
	for _, c := range []struct {
		file string
		want bool
	}{
		{"resources/app.asar", true},
		{"resources/electron.asar", true},
		{"resources/default_app.asar", true},
		{"resources/app/package.json", true},
		{"resources/other.pak", false},
	} {
		dir := filepath.Join(root, filepath.FromSlash(c.file))
		writeFile(t, dir, "x")
		exe := filepath.Join(root, "app.exe")
		if got := IsElectron(exe, nil); got != c.want {
			t.Errorf("%s: IsElectron = %v, want %v", c.file, got, c.want)
		}
		root = t.TempDir()
	}
}

// appMachine is a fake machine with a process tree: the launched App 500,
// its WebView2 child 510, and the user's own msedgewebview2.exe 900.
func appMachine(t *testing.T) (*fakeMachine, *Manager) {
	f := newFake(t)
	parents := map[uint32]uint32{500: 100, 510: 500, 520: 510, 900: 4}
	f.sys.ParentPID = func(pid uint32) (uint32, bool) {
		p, ok := parents[pid]
		return p, ok
	}
	f.sys.AppPort = func() (int, error) { return 51234, nil }
	return f, NewManager(f.sys)
}

func TestAwaitAppAcceptsOnlyTheLaunchedTree(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name  string
		owner uint32
		ok    bool
	}{
		{"the App itself (Electron)", 500, true},
		{"its child (WebView2)", 510, true},
		{"a grandchild", 520, true},
		{"the user's own process", 900, false},
	} {
		f, m := appMachine(t)
		f.listen(51234, "ws://127.0.0.1:51234/devtools/browser/x", c.owner)
		ep, ok := m.AwaitApp(ctx, AppWebView2, 500, 500, 51234, time.Second)
		if ok != c.ok {
			t.Errorf("%s: ok = %v, want %v", c.name, ok, c.ok)
			continue
		}
		if !ok {
			if m.AppKind(500) != "" {
				t.Errorf("%s: registered anyway", c.name)
			}
			continue
		}
		if ep.Owner != c.owner || ep.PID != 500 || ep.Port != 51234 || ep.WSURL != "ws://127.0.0.1:51234/devtools/browser/x" {
			t.Errorf("%s: %+v", c.name, ep)
		}
		if m.AppKind(500) != AppWebView2 || m.AppKind(510) != "" {
			t.Errorf("%s: registered for %q / %q", c.name, m.AppKind(500), m.AppKind(510))
		}
	}
}

func TestAwaitAppGivesUpAfterTheWait(t *testing.T) {
	f, m := appMachine(t)
	start := f.sys.Now()
	if _, ok := m.AwaitApp(context.Background(), AppElectron, 500, 500, 51234, 3*time.Second); ok {
		t.Fatal("no port answered, yet the App was registered")
	}
	if waited := f.sys.Now().Sub(start); waited < 3*time.Second || waited > 4*time.Second {
		t.Errorf("waited %s, want the 3 s wait", waited)
	}
	// A port that answers late, within the wait, is taken.
	f, m = appMachine(t)
	polls := 0
	sleep := f.sys.Sleep
	f.sys.Sleep = func(d time.Duration) {
		polls++
		if polls == 5 {
			f.listen(51234, "ws://late", 500)
		}
		sleep(d)
	}
	if _, ok := m.AwaitApp(context.Background(), AppElectron, 500, 500, 51234, 3*time.Second); !ok {
		t.Error("the late port was not taken")
	}
}

// A registered App is reached through its Window's process, never through
// the browser lookups.
func TestAppWindowConnection(t *testing.T) {
	f, m := appMachine(t)
	srv := f.serve(510) // the WebView2 child listens
	port := srv.Port()
	if _, ok := m.AwaitApp(context.Background(), AppWebView2, 500, 500, port, time.Second); !ok {
		t.Fatal("not registered")
	}
	c, err := m.WindowConnection(context.Background(), AppWebView2, 500)
	if err != nil {
		t.Fatalf("WindowConnection: %v", err)
	}
	if c.Mode != ModeApp || c.PID != 500 || c.Port != port || c.Browser != AppWebView2 {
		t.Errorf("connection %+v", c)
	}
	if again, _ := m.WindowConnection(context.Background(), AppWebView2, 500); again != c || f.dialCount() != 1 {
		t.Errorf("not reused: %d dials", f.dialCount())
	}
	if open := m.open(""); len(open) != 0 {
		t.Errorf("an App connection counts for the browser lookups: %+v", open)
	}
	if held := m.Held(); len(held) != 1 || held[0] != "webview2 process 500, an App winuse launched with --cdp" {
		t.Errorf("held = %q", held)
	}
	if DisplayName(AppWebView2) != "WebView2 App" || DisplayName(AppElectron) != "Electron App" || Known(AppElectron) {
		t.Error("App kinds are display names, not browsers")
	}
}
