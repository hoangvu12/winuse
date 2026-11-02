package browser

// The extension route over the fake machine: discovery preferring the
// extension connected to the pinned browser and never guessing between
// two, self-update, `ext install` against a fake registry, and one
// `ext status` test per state. The bridges are real native hosts
// (bridge.ServeHost) with fake extensions (bridgetest) on in-memory pipes
// and ephemeral loopback ports; no browser, real registry key or real
// bridge file is touched.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/bridge"
	"github.com/hoangvu12/winuse/internal/bridge/bridgetest"
	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/win"
)

type fakeRegistry struct {
	mu   sync.Mutex
	keys map[string]string
}

func (r *fakeRegistry) SetDefault(key, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keys[key] = value
	return nil
}

func (r *fakeRegistry) Default(key string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.keys[key]
	if !ok {
		return "", errors.New("the system cannot find the file specified")
	}
	return v, nil
}

// extFake is the fake machine with the extension route wired: bridge
// files in its own state dir, dials straight to the test hosts, a fake
// registry and a fake winuse binary.
type extFake struct {
	*fakeMachine
	reg *fakeRegistry
	exe string
}

func newExtFake(t *testing.T) *extFake {
	f := &extFake{fakeMachine: newFake(t), reg: &fakeRegistry{keys: map[string]string{}}}
	f.exe = filepath.Join(f.sys.Env.LocalAppData, "winuse", "bin", "winuse.exe")
	writeFile(t, f.exe, "exe")
	f.sys.Bridges = func() []bridge.Info { return bridge.List(f.sys.BridgesDir(), nil) }
	f.sys.OpenBridge = func(ctx context.Context, b bridge.Info) (cdp.Transport, error) {
		return bridge.Dial(ctx, b.Addr(), b.Token)
	}
	f.sys.Registry = f.reg
	f.sys.Executable = func() (string, error) { return f.exe, nil }
	f.sys.Extension = f.sys.extensionEndpoint
	// Real sleeps long enough for a reloaded fake's host to come up
	// even on a loaded machine (the 15 s update wait is 1.2 s real).
	f.sys.Sleep = func(d time.Duration) {
		f.mu.Lock()
		f.now = f.now.Add(d)
		f.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	return f
}

// connect starts a fake extension in a browser, its host started by that
// browser's process.
func (f *extFake) connect(name, instance string, e *bridgetest.Extension) *bridgetest.Extension {
	f.t.Helper()
	if e == nil {
		e = &bridgetest.Extension{}
	}
	e.Browser = name
	if name == "chromium" {
		e.Browser = "chrome" // Chromium has no brand of its own; the process tells them apart
	}
	e.Instance = instance
	e.BrowserPID = 500
	e.BrowserExe = f.sys.Env.CandidatePaths()[name][0]
	return bridgetest.Start(f.t, f.sys.BridgesDir(), e)
}

func TestDiscoverPrefersTheExtensionConnectedToThePinnedBrowser(t *testing.T) {
	f := newExtFake(t)
	f.install("chrome")
	f.process("chrome", 100)
	f.ownProfile("chrome", ModeLaunch, 9301)
	f.listen(9301, "ws://127.0.0.1:9301/devtools/browser/a", 100)
	focus := make(chan string, 1)
	ext := f.connect("chrome", "ab12cd34", &bridgetest.Extension{
		Tabs: []bridgetest.Tab{{TargetID: "T1", URL: "https://a.test/"}, {TargetID: "T2", URL: "https://b.test/"}, {TargetID: "T3", URL: "chrome://newtab/"}},
		Page: func(sid, method string, params json.RawMessage) (any, error) {
			if method == "Emulation.setFocusEmulationEnabled" {
				focus <- sid
			}
			return nil, nil
		},
	})
	ep, err := f.sys.discover(context.Background(), "chrome")
	if err != nil {
		t.Fatal(err)
	}
	if ep.Mode != ModeExtension || ep.Browser != "chrome" || ep.Open == nil || ep.PID != 500 {
		t.Fatalf("endpoint = %+v, want the extension first", ep)
	}

	m := NewManager(f.sys)
	defer m.Close()
	out, err := m.Status(context.Background(), "chrome")
	if err != nil {
		t.Fatal(err)
	}
	if out != "ok chrome connected: your running browser through the winuse extension, 2 tabs\n" {
		t.Errorf("status = %q", out)
	}
	c, err := m.Connection(context.Background(), "chrome")
	if err != nil {
		t.Fatal(err)
	}
	sid, err := c.AttachPage(context.Background(), "T1")
	if err != nil || sid != "T1" {
		t.Fatalf("attach = %q, %v", sid, err)
	}
	if got := <-focus; got != "T1" {
		t.Errorf("focus emulation went to %q", got)
	}
	if f.dialCount() != 0 {
		t.Errorf("a WebSocket was dialed %d times beside the extension", f.dialCount())
	}
	// A refused browser-level method is a Problem with the next command.
	_, err = c.Client.Call(context.Background(), "Browser.setDownloadBehavior", map[string]any{"behavior": "allow"})
	p, ok := AsProblem(err, "chrome")
	if !ok || p.Kind != ExtCannot || p.Sentence() != "Browser.setDownloadBehavior needs a launched or cloned browser; the winuse extension cannot do it" || p.Next() != "run winuse browser launch chrome or winuse browser clone chrome, which take browser-level commands" {
		t.Errorf("refusal = %v, %+v", err, p)
	}
	for _, cmd := range ext.Commands() {
		if strings.HasPrefix(cmd.Method, "Browser.") {
			t.Errorf("%s reached the extension", cmd.Method)
		}
	}

	// The user turns winuse off: the held connection says so.
	ext.SetPaused(true)
	_, err = m.Status(context.Background(), "chrome")
	problemOf(t, err, ExtPaused)
	// The browser closes: the host exits and the next dial has nowhere to go.
	ext.Close()
	select {
	case <-c.Client.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("the held connection outlived its host")
	}
	// Discovery moves on to winuse's launch profile.
	if ep, err := f.sys.discover(context.Background(), "chrome"); err != nil || ep.Mode != ModeLaunch {
		t.Errorf("discover after the host exited = %+v, %v", ep, err)
	}
}

func TestADroppedBridgeDialIsExtUnreachable(t *testing.T) {
	err := dialProblem(Endpoint{Browser: "edge", Mode: ModeExtension}, errors.New("connection refused"))
	if p := problemOf(t, err, ExtUnreachable); p.Detail != "connection refused" {
		t.Errorf("problem = %+v", p)
	}
}

func TestDiscoverIgnoresTheExtensionOfAnotherBrowser(t *testing.T) {
	f := newExtFake(t)
	f.install("chrome")
	f.install("edge")
	f.process("chrome", 100)
	f.ownProfile("chrome", ModeLaunch, 9301)
	f.listen(9301, "ws://127.0.0.1:9301/devtools/browser/a", 100)
	f.connect("edge", "e1e1e1e1", nil)
	ep, err := f.sys.discover(context.Background(), "chrome")
	if err != nil || ep.Mode != ModeLaunch || ep.Browser != "chrome" {
		t.Errorf("discover chrome = %+v, %v; want its launch profile, not Edge's extension", ep, err)
	}
	ep, err = f.sys.discover(context.Background(), "edge")
	if err != nil || ep.Mode != ModeExtension || ep.Browser != "edge" {
		t.Errorf("discover edge = %+v, %v", ep, err)
	}
}

func TestDiscoverNamesTheBrowserByTheProcessThatStartedTheHost(t *testing.T) {
	f := newExtFake(t)
	f.install("chromium")
	f.connect("chromium", "c1c1c1c1", nil) // the extension says "chrome"
	ep, err := f.sys.discover(context.Background(), "")
	if err != nil || ep.Browser != "chromium" {
		t.Errorf("discover = %+v, %v; want chromium from the host's parent process", ep, err)
	}
}

func TestDiscoverRefusesToGuessBetweenTwoExtensionBrowsers(t *testing.T) {
	f := newExtFake(t)
	f.install("chrome")
	f.install("edge")
	f.connect("edge", "e1e1e1e1", nil)
	f.connect("chrome", "ab12cd34", nil)
	_, err := f.sys.discover(context.Background(), "")
	p := problemOf(t, err, Ambiguous)
	if strings.Join(p.Others, ",") != "chrome,edge" {
		t.Errorf("Others = %v", p.Others)
	}
	if ep, err := f.sys.discover(context.Background(), "edge"); err != nil || ep.Browser != "edge" {
		t.Errorf("pinned edge = %+v, %v", ep, err)
	}

	g := newExtFake(t)
	g.install("chrome")
	g.connect("chrome", "aaaa1111", nil)
	g.connect("chrome", "bbbb2222", nil)
	_, err = g.sys.discover(context.Background(), "chrome")
	if p := problemOf(t, err, ExtSeveralProfiles); len(p.Others) != 2 {
		t.Errorf("instances = %v", p.Others)
	}
}

func TestDiscoverFallsBackWhileTheUserHasTheExtensionOff(t *testing.T) {
	f := newExtFake(t)
	f.install("chrome")
	ext := f.connect("chrome", "ab12cd34", nil)
	ext.SetPaused(true)
	_, err := f.sys.discover(context.Background(), "chrome")
	problemOf(t, err, ExtPaused)

	// A browser winuse launched is still fine to use.
	f.process("chrome", 100)
	f.ownProfile("chrome", ModeLaunch, 9301)
	f.listen(9301, "ws://127.0.0.1:9301/devtools/browser/a", 100)
	ep, err := f.sys.discover(context.Background(), "chrome")
	if err != nil || ep.Mode != ModeLaunch {
		t.Errorf("discover = %+v, %v; want the launch profile", ep, err)
	}
}

func TestDiscoverUpdatesAnOutdatedExtension(t *testing.T) {
	f := newExtFake(t)
	f.install("chrome")
	var old *bridgetest.Extension
	old = f.connect("chrome", "ab12cd34", &bridgetest.Extension{Build: "0ldb0ldb0ldb", OnReload: func() {
		old.Close() // the extension reloads: its host exits, a new one starts
		f.connect("chrome", "ab12cd34", nil)
	}})
	ep, err := f.sys.discover(context.Background(), "chrome")
	if err != nil || ep.Mode != ModeExtension {
		t.Fatalf("discover = %+v, %v", ep, err)
	}
	if old.Reloads() != 1 {
		t.Errorf("reloads = %d, want 1", old.Reloads())
	}
	if strings.Contains(ep.WSURL, old.Info.Addr()) {
		t.Errorf("endpoint %q is the old host's", ep.WSURL)
	}
	b, err := os.ReadFile(filepath.Join(f.sys.ExtensionDir(), "manifest.json"))
	if err != nil || bridge.StampedBuild(b) != bridge.Build() {
		t.Errorf("the extension folder was not rewritten: %s, %v", b, err)
	}
}

// TestSelfUpdateJudgesTheLoadedExtension reproduces a bug seen live:
// Chrome ran extension 0.1.0 while its folder held newer files, and ext
// status said connected with no update, because the extension read its
// build from the folder at every connect (build.json). The build now comes
// from the manifest the browser loaded, so after the folder is rewritten
// under the running extension and it reconnects, ext status and discovery
// still see it outdated and reload it.
func TestSelfUpdateJudgesTheLoadedExtension(t *testing.T) {
	for _, route := range []string{"ext status", "discovery"} {
		t.Run(route, func(t *testing.T) {
			f := newExtFake(t)
			f.install("chrome")
			dir := f.sys.ExtensionDir()
			// An older winuse wrote the folder, unstamped, and Chrome loaded it.
			writeFile(t, filepath.Join(dir, "manifest.json"), `{"manifest_version":3,"name":"winuse","version":"0.1.0"}`)
			old := f.connect("chrome", "ab12cd34", &bridgetest.Extension{Dir: dir})
			// A newer winuse rewrites the folder; nothing reloads the extension.
			if err := bridge.WriteExtension(dir); err != nil {
				t.Fatal(err)
			}
			// The host restarts and the old code says hello again.
			stale := old.Reconnect()
			if stale.Info.Build != "" || stale.Info.Version != "0.1.0" {
				t.Fatalf("reconnected bridge = version %q build %q, want what Chrome loaded: 0.1.0, no build", stale.Info.Version, stale.Info.Build)
			}

			var addr string
			switch route {
			case "ext status":
				out, err := NewManager(f.sys).ExtStatus(context.Background(), "chrome")
				want := "ok chrome connected through the winuse extension (version " + bridge.Version() + ", instance ab12cd34)\nnote: updated the winuse extension in Chrome to build " + bridge.Build() + "\n"
				if err != nil || out != want {
					t.Fatalf("status = %q, %v\nwant %q", out, err, want)
				}
			case "discovery":
				ep, err := f.sys.discover(context.Background(), "chrome")
				if err != nil || ep.Mode != ModeExtension {
					t.Fatalf("discover = %+v, %v", ep, err)
				}
				addr = ep.WSURL
			}
			if stale.Reloads() != 1 {
				t.Errorf("Winuse.reload calls = %d, want 1", stale.Reloads())
			}
			bridges := bridge.List(f.sys.BridgesDir(), nil)
			if len(bridges) != 1 || bridges[0].Build != bridge.Build() || bridges[0].Version != bridge.Version() || bridges[0].Port == stale.Info.Port {
				t.Errorf("bridges after the update = %+v", bridges)
			}
			if addr != "" && strings.Contains(addr, stale.Info.Addr()) {
				t.Errorf("endpoint %q is the stale host's", addr)
			}
		})
	}
}

func TestExtInstall(t *testing.T) {
	f := newExtFake(t)
	f.install("chrome")
	f.install("edge")
	f.install("opera")
	m := NewManager(f.sys)
	out, err := m.ExtInstall(context.Background(), "chrome")
	if err != nil {
		t.Fatal(err)
	}
	dir := f.sys.ExtensionDir()
	mp := f.sys.HostManifestPath()
	want := fmt.Sprintf(`ok extension written to %s (id mjolifepdnkcigohjhadbidbojmfhcog, build %s)
ok native host com.hoangvu12.winuse registered for chrome (%s)
only the user can do the last step, once per browser. Tell them:
  In Chrome:
  1. Type chrome://extensions in the address bar.
  2. Turn on "Developer mode" (top right).
  3. Click "Load unpacked" and choose this folder:
     %s
then run: winuse browser ext status chrome (exit 0 once it is connected)
`, dir, bridge.Build(), f.exe, dir)
	if out != want {
		t.Errorf("output =\n%s\nwant\n%s", out, want)
	}
	for _, file := range []string{"manifest.json", "background.js"} {
		if !exists(filepath.Join(dir, file)) {
			t.Errorf("%s not written", file)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "manifest.json")); bridge.StampedBuild(b) != bridge.Build() {
		t.Errorf("manifest not stamped with build %s:\n%s", bridge.Build(), b)
	}
	var hm struct {
		Name    string   `json:"name"`
		Path    string   `json:"path"`
		Origins []string `json:"allowed_origins"`
	}
	b, _ := os.ReadFile(mp)
	if json.Unmarshal(b, &hm) != nil || hm.Name != "com.hoangvu12.winuse" || hm.Path != f.exe || hm.Origins[0] != "chrome-extension://mjolifepdnkcigohjhadbidbojmfhcog/" {
		t.Errorf("host manifest = %s", b)
	}
	if len(f.reg.keys) != 1 || f.reg.keys[`Software\Google\Chrome\NativeMessagingHosts\com.hoangvu12.winuse`] != mp {
		t.Errorf("registry = %v", f.reg.keys)
	}

	out, err = m.ExtInstall(context.Background(), "all")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		`Software\Google\Chrome\NativeMessagingHosts\com.hoangvu12.winuse`,
		`Software\Microsoft\Edge\NativeMessagingHosts\com.hoangvu12.winuse`,
	} {
		if f.reg.keys[key] != mp {
			t.Errorf("%s = %q", key, f.reg.keys[key])
		}
	}
	if len(f.reg.keys) != 2 { // Opera reads Chrome's key
		t.Errorf("registry = %v", f.reg.keys)
	}
	for _, s := range []string{"registered for chrome, edge, opera", "In Edge:", "edge://extensions", "In Opera:", "opera://extensions", "winuse browser ext status opera"} {
		if !strings.Contains(out, s) {
			t.Errorf("install all lacks %q:\n%s", s, out)
		}
	}
	if strings.ContainsAny(out, "\u2013\u2014\u201c\u201d") {
		t.Error("install output has a dash or curly quote")
	}

	// A browser already connected is said so, without the steps.
	f.connect("chrome", "ab12cd34", nil)
	out, err = m.ExtInstall(context.Background(), "chrome")
	if err != nil || !strings.Contains(out, "ok chrome is already connected through the winuse extension\n") || strings.Contains(out, "Load unpacked") {
		t.Errorf("connected install = %q, %v", out, err)
	}

	f.exe = `C:\Users\u\AppData\Local\Temp\go-build123\b001\exe\winuse.exe`
	if _, err := m.ExtInstall(context.Background(), "chrome"); err == nil || !strings.Contains(err.Error(), "temporary go run binary") {
		t.Errorf("go run binary = %v", err)
	}
	if _, err := m.ExtInstall(context.Background(), "vivaldi"); err == nil {
		t.Error("installed for a browser that is not installed")
	}
}

// installedFor sets up the extension for a browser ("all" for every
// installed one) as ext install would, and marks it loaded (and on) in
// each one's Default profile preferences.
func (f *extFake) installedFor(name string, loaded, enabled bool) {
	f.t.Helper()
	if _, err := NewManager(f.sys).ExtInstall(context.Background(), name); err != nil {
		f.t.Fatal(err)
	}
	if !loaded {
		return
	}
	reasons := "[]"
	if !enabled {
		reasons = "[1]"
	}
	names := []string{name}
	if name == "all" {
		names = nil
		for _, b := range f.sys.detect() {
			names = append(names, b.Name)
		}
	}
	for _, n := range names {
		writeFile(f.t, filepath.Join(f.sys.Env.UserDataDirs()[n], "Default", "Secure Preferences"),
			`{"extensions":{"settings":{"`+bridge.ExtensionID+`":{"disable_reasons":`+reasons+`}}}}`)
	}
}

func TestExtStatusStates(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		setup func(f *extFake)
		kind  Kind
		ok    string
	}{
		{"not installed", func(f *extFake) {}, ExtNotInstalled, ""},
		{"host binary gone", func(f *extFake) {
			f.installedFor("chrome", false, false)
			os.Remove(f.exe)
		}, ExtNotInstalled, ""},
		{"not registered for this browser", func(f *extFake) {
			f.installedFor("chrome", false, false)
			delete(f.reg.keys, HostKey("chrome"))
		}, ExtNotRegistered, ""},
		{"registered elsewhere", func(f *extFake) {
			f.installedFor("chrome", false, false)
			f.reg.keys[HostKey("chrome")] = `C:\other\com.hoangvu12.winuse.json`
		}, ExtNotRegistered, ""},
		{"not loaded", func(f *extFake) { f.installedFor("chrome", false, false) }, ExtNotLoaded, ""},
		{"loaded but off", func(f *extFake) { f.installedFor("chrome", true, false) }, ExtDisabled, ""},
		{"browser closed", func(f *extFake) { f.installedFor("chrome", true, true) }, ExtBrowserClosed, ""},
		{"not connected", func(f *extFake) {
			f.installedFor("chrome", true, true)
			f.process("chrome", 100)
			writeFile(f.t, filepath.Join(f.sys.BridgesDir(), "host.log"), "09:00:00 pid 9: started\n09:00:01 pid 9: no hello from the extension: EOF\n")
		}, ExtNotConnected, ""},
		{"outdated, update failed", func(f *extFake) {
			f.installedFor("chrome", true, true)
			f.connect("chrome", "ab12cd34", &bridgetest.Extension{Build: "0ldb0ldb0ldb"})
		}, ExtOutdated, ""},
		{"paused by the user", func(f *extFake) {
			f.installedFor("chrome", true, true)
			f.connect("chrome", "ab12cd34", nil).SetPaused(true)
		}, ExtPaused, ""},
		{"not answering", func(f *extFake) {
			f.installedFor("chrome", true, true)
			ext := f.connect("chrome", "ab12cd34", nil)
			// The bridge file stays, but its token no longer opens the host.
			info := ext.Info
			info.Token = "ffffffffffffffffffffffffffffffff"
			b, _ := json.Marshal(info)
			writeFile(f.t, info.Path, string(b))
		}, ExtUnreachable, ""},
		{"several profiles", func(f *extFake) {
			f.connect("chrome", "aaaa1111", nil)
			f.connect("chrome", "bbbb2222", nil)
		}, ExtSeveralProfiles, ""},
		{"connected", func(f *extFake) {
			f.installedFor("chrome", true, true)
			f.connect("chrome", "ab12cd34", nil)
		}, 0, "ok chrome connected through the winuse extension (version " + bridge.Version() + ", instance ab12cd34)\n"},
		{"outdated, then updated", func(f *extFake) {
			f.installedFor("chrome", true, true)
			var old *bridgetest.Extension
			old = f.connect("chrome", "ab12cd34", &bridgetest.Extension{Build: "0ldb0ldb0ldb", OnReload: func() {
				old.Close()
				f.connect("chrome", "ab12cd34", nil)
			}})
		}, 0, "ok chrome connected through the winuse extension (version " + bridge.Version() + ", instance ab12cd34)\nnote: updated the winuse extension in Chrome to build " + bridge.Build() + "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newExtFake(t)
			f.install("chrome")
			tc.setup(f)
			out, err := NewManager(f.sys).ExtStatus(ctx, "chrome")
			if tc.kind == 0 {
				if err != nil || out != tc.ok {
					t.Errorf("status = %q, %v\nwant %q", out, err, tc.ok)
				}
				return
			}
			p := problemOf(t, err, tc.kind)
			if p.Browser != "chrome" {
				t.Errorf("problem browser = %q", p.Browser)
			}
			switch tc.kind {
			case ExtNotLoaded:
				if p.Dir != f.sys.ExtensionDir() || !strings.Contains(p.Next(), "Load unpacked") {
					t.Errorf("next = %q", p.Next())
				}
			case ExtDisabled:
				if p.Detail != "Default" {
					t.Errorf("profiles = %q", p.Detail)
				}
			case ExtNotConnected:
				if p.Detail != "09:00:01 pid 9: no hello from the extension: EOF" {
					t.Errorf("host log = %q", p.Detail)
				}
			case ExtNotInstalled:
				if tc.name == "host binary gone" && p.Detail != f.exe {
					t.Errorf("detail = %q", p.Detail)
				}
			}
		})
	}
}

func TestExtStatusWithNothingNamed(t *testing.T) {
	f := newExtFake(t)
	f.install("chrome")
	f.install("edge")
	f.installedFor("all", true, true)
	f.connect("edge", "e1e1e1e1", nil)
	out, err := NewManager(f.sys).ExtStatus(context.Background(), "")
	if err != nil || !strings.HasPrefix(out, "ok edge connected through the winuse extension") {
		t.Errorf("status = %q, %v; want the connected browser", out, err)
	}
	g := newExtFake(t)
	g.install("chrome")
	g.install("edge")
	g.process("edge", 200)
	_, err = NewManager(g.sys).ExtStatus(context.Background(), "")
	if p := problemOf(t, err, ExtNotInstalled); p.Browser != "edge" {
		t.Errorf("diagnosed %q, want the running browser", p.Browser)
	}
}

func TestAsProblem(t *testing.T) {
	if _, ok := AsProblem(errors.New("plain"), "chrome"); ok {
		t.Error("a plain error became a Problem")
	}
	p, ok := AsProblem(fmt.Errorf("wrapped: %w", &cdp.Error{Method: "Page.navigate", Code: bridge.CodePaused}), "edge")
	if !ok || p.Kind != ExtPaused || p.Browser != "edge" {
		t.Errorf("paused = %+v", p)
	}
	if _, ok := AsProblem(&cdp.Error{Method: "Page.bringToFront", Code: bridge.CodeForeground, Message: "x"}, ""); ok {
		t.Error("a foreground refusal is a programming error, not a user Problem")
	}
	in := &Problem{Kind: NotRunning, Browser: "chrome"}
	if p, ok := AsProblem(fmt.Errorf("x: %w", in), ""); !ok || p != in {
		t.Error("a Problem was not found")
	}
}

func TestLiveExtensionSeamsRefuseInTests(t *testing.T) {
	t.Setenv("WINUSE_E2E", "")
	sys := Live()
	if _, err := sys.OpenBridge(context.Background(), bridge.Info{Port: 1, Token: "x"}); !errors.Is(err, win.ErrLiveDesktopInTest) {
		t.Errorf("OpenBridge = %v", err)
	}
	if err := sys.Registry.SetDefault(`Software\winuse-test-never-written`, "x"); !errors.Is(err, win.ErrLiveDesktopInTest) {
		t.Errorf("Registry.SetDefault = %v", err)
	}
	if got := sys.Bridges(); got != nil {
		t.Errorf("Bridges read the real bridge files: %+v", got)
	}
	if sys.Extension == nil || sys.Executable == nil {
		t.Error("Live does not wire the extension route")
	}
	if code := bridge.HostMain(bridge.Origin, t.TempDir()); code != 1 {
		t.Errorf("HostMain = %d, want a refusal", code)
	}
	if !strings.HasSuffix(sys.BridgesDir(), filepath.Join("winuse", "browser", "bridges")) ||
		!strings.HasSuffix(sys.ExtensionDir(), filepath.Join("winuse", "browser", "extension")) ||
		!strings.HasSuffix(sys.HostManifestPath(), filepath.Join("winuse", "browser", "native-host", "com.hoangvu12.winuse.json")) {
		t.Errorf("paths = %s, %s, %s", sys.BridgesDir(), sys.ExtensionDir(), sys.HostManifestPath())
	}
}
