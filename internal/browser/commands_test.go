package browser

// The browser commands over the fake machine: launch, connect, stop, use
// and clone start nothing real; Start and CloseApp are recorded, and a
// "started" browser is a cdptest server the fake lists on the chosen port.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
	"github.com/hoangvu12/winuse/internal/win"
)

// startsServer makes the fake's Start bring up s as the browser's endpoint
// on the port the command chose (FreePort answers s's port).
func startsServer(f *fakeMachine, s *cdptest.Server, pid uint32) {
	f.free = s.Port()
	f.onStart = func(call startCall) int {
		f.listen(s.Port(), s.URL(), pid)
		return int(pid)
	}
}

func TestLaunchStartsAWinuseProfileAndConnects(t *testing.T) {
	f := newFake(t)
	path := f.install("edge")
	f.process("edge", 200)
	s := cdptest.NewServer()
	defer s.Close()
	startsServer(f, s, 200)
	m := NewManager(f.sys)
	defer m.Close()

	out, err := m.Launch(context.Background(), "edge")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	dir := f.sys.LaunchProfileDir("edge")
	want := "ok edge launched: pid 200, port " + strconv.Itoa(s.Port()) + ", profile " + dir + "\n"
	if out != want {
		t.Errorf("out = %q, want %q", out, want)
	}
	if !strings.HasPrefix(dir, filepath.Join(f.sys.Env.LocalAppData, "winuse")) {
		t.Errorf("profile %s is not under %%LOCALAPPDATA%%\\winuse", dir)
	}
	starts := f.startCalls()
	if len(starts) != 1 || starts[0].path != path {
		t.Fatalf("starts = %+v", starts)
	}
	wantArgs := []string{"--remote-debugging-port=" + strconv.Itoa(s.Port()), "--user-data-dir=" + dir, "--no-first-run", "--no-default-browser-check", "--disable-features=CalculateNativeWinOcclusion"}
	if !slices.Equal(starts[0].args, wantArgs) {
		t.Errorf("args = %q, want %q", starts[0].args, wantArgs)
	}
	if f.sys.Pin() != "edge" {
		t.Errorf("pin = %q, want edge", f.sys.Pin())
	}
	if s.Upgrades() != 1 {
		t.Errorf("handshakes = %d, want the launch to connect", s.Upgrades())
	}

	// A second launch finds winuse's own Edge and starts nothing.
	out, err = m.Launch(context.Background(), "edge")
	if err != nil || !strings.HasPrefix(out, "ok edge already running from winuse's launch profile") {
		t.Errorf("second launch = %q, %v", out, err)
	}
	if len(f.startCalls()) != 1 || s.Upgrades() != 1 {
		t.Errorf("second launch started %d browsers and made %d handshakes", len(f.startCalls()), s.Upgrades())
	}
}

func TestLaunchArgsTurnOffNativeOcclusion(t *testing.T) {
	const flag = "--disable-features=CalculateNativeWinOcclusion"
	for _, args := range [][]string{launchArgs(9222, `C:\p`), cloneArgs(9222, `C:\p`, "Default", true)} {
		n := 0
		for _, a := range args {
			if strings.HasPrefix(a, "--disable-features=") {
				n++
			}
		}
		if n != 1 || !slices.Contains(args, flag) {
			t.Errorf("args = %q, want one %s", args, flag)
		}
	}
}

func TestDisableFeaturesMergesIntoAnExistingSwitch(t *testing.T) {
	tests := []struct {
		in   []string
		want []string
	}{
		{[]string{"--a"}, []string{"--a", "--disable-features=X"}},
		{[]string{"--disable-features=Y", "--a"}, []string{"--disable-features=Y,X", "--a"}},
		{[]string{"--disable-features=X,Y"}, []string{"--disable-features=X,Y"}},
		{[]string{"--disable-features="}, []string{"--disable-features=X"}},
	}
	for _, tt := range tests {
		in := slices.Clone(tt.in)
		if got := disableFeatures(in, "X"); !slices.Equal(got, tt.want) {
			t.Errorf("disableFeatures(%q) = %q, want %q", tt.in, got, tt.want)
		}
		if !slices.Equal(in, tt.in) {
			t.Errorf("disableFeatures changed its input to %q", in)
		}
	}
}

func TestLaunchReportsAnEndpointThatNeverCameUp(t *testing.T) {
	f := newFake(t)
	f.install("chrome")
	m := NewManager(f.sys)
	defer m.Close()
	_, err := m.Launch(context.Background(), "chrome")
	p := problemOf(t, err, NotStarted)
	if p.PID != 4242 || p.Port != 9301 || p.Mode != ModeLaunch {
		t.Errorf("problem = %+v", p)
	}
}

func TestLaunchRefusesABusyProfile(t *testing.T) {
	f := newFake(t)
	f.install("chrome")
	dir := f.sys.LaunchProfileDir("chrome")
	writeFile(t, filepath.Join(dir, "DevToolsActivePort"), "9555\n/devtools/browser/x")
	f.listen(9555, "", 0) // alive, but no owner and no /json/version
	m := NewManager(f.sys)
	defer m.Close()
	_, err := m.Launch(context.Background(), "chrome")
	p := problemOf(t, err, ProfileBusy)
	if p.Dir != dir || len(f.startCalls()) != 0 {
		t.Errorf("problem = %+v with %d starts, want the busy profile and no start", p, len(f.startCalls()))
	}
}

func TestStopClosesOnlyBrowsersWinuseStarted(t *testing.T) {
	// The user's Chrome through the inspect toggle: connected, never closed.
	f := newFake(t)
	f.install("chrome")
	f.process("chrome", 100)
	s := f.serve(100)
	f.listen(s.Port(), "", 100)
	f.toggle("chrome", s.Port(), "/devtools/browser/fake")
	m := NewManager(f.sys)
	defer m.Close()
	ctx := context.Background()
	if _, err := m.Connect(ctx, "chrome"); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	out, err := m.Stop(ctx, "chrome")
	if err != nil || out != "ok chrome already stopped (winuse stops only browsers it launched or cloned)\n" {
		t.Errorf("Stop = %q, %v", out, err)
	}
	if slices.Contains(s.Methods(), "Browser.close") {
		t.Fatal("stop closed the user's own browser")
	}

	// winuse's launched Chrome: closed cleanly, profile kept.
	f2, s2, m2 := launchedChrome(t)
	s2.Handle("Browser.close", func(c *cdptest.Conn, msg cdptest.Message) (any, error) {
		f2.unlisten(s2.Port())
		return nil, nil
	})
	if _, err := m2.Connection(ctx, "chrome"); err != nil {
		t.Fatal(err)
	}
	out, err = m2.Stop(ctx, "chrome")
	dir := f2.sys.LaunchProfileDir("chrome")
	if err != nil || out != "ok chrome stopped; its winuse launch profile is kept at "+dir+"\n" {
		t.Errorf("Stop = %q, %v", out, err)
	}
	if !slices.Contains(s2.Methods(), "Browser.close") {
		t.Error("Browser.close was not sent")
	}
	if !exists(dir) {
		t.Error("the profile was deleted")
	}
}

func TestConnectOpensTheInspectPageAndWaitsForTheToggle(t *testing.T) {
	f := newFake(t)
	path := f.install("brave")
	f.process("brave", 300)
	s := f.serve(300)
	f.unlisten(s.Port())
	f.onStart = func(call startCall) int {
		// The user turns the toggle on: Brave writes DevToolsActivePort and
		// listens, with /json/version disabled.
		f.toggle("brave", s.Port(), "/devtools/browser/fake")
		f.listen(s.Port(), "", 300)
		return 300
	}
	m := NewManager(f.sys)
	defer m.Close()
	out, err := m.Connect(context.Background(), "")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if out != "ok brave connected: your real profile through the inspect toggle\n" {
		t.Errorf("out = %q", out)
	}
	starts := f.startCalls()
	if len(starts) != 1 || starts[0].path != path || !slices.Equal(starts[0].args, []string{"brave://inspect/#remote-debugging"}) {
		t.Errorf("starts = %+v", starts)
	}
	if c := m.held("brave"); c == nil || c.Mode != ModeToggle {
		t.Errorf("held = %+v", c)
	}
}

func TestConnectReportsTheToggleStillOff(t *testing.T) {
	f := newFake(t)
	f.install("chrome")
	m := NewManager(f.sys)
	defer m.Close()
	_, err := m.Connect(context.Background(), "chrome")
	p := problemOf(t, err, ToggleOff)
	if p.Next() != `turn on "Allow remote debugging for this browser instance" at chrome://inspect/#remote-debugging, then run winuse browser connect chrome` {
		t.Errorf("next = %q", p.Next())
	}
}

func TestUsePinsAndClears(t *testing.T) {
	f := newFake(t)
	f.install("chrome")
	edge := f.install("edge")
	m := NewManager(f.sys)
	defer m.Close()
	ctx := context.Background()
	out, err := m.Use(ctx, "")
	if err != nil || out != "browser: auto (no pin: the one debuggable browser; with two, winuse asks)\ninstalled: chrome edge\n" {
		t.Errorf("Use() = %q, %v", out, err)
	}
	out, err = m.Use(ctx, "Edge")
	if err != nil || out != "ok browser: edge ("+edge+")\n" {
		t.Errorf("Use(Edge) = %q, %v", out, err)
	}
	if out, _ := m.Use(ctx, ""); !strings.HasPrefix(out, "browser: edge (pinned)\n") {
		t.Errorf("Use() after the pin = %q", out)
	}
	_, err = m.Use(ctx, "opera")
	if p := problemOf(t, err, NotInstalled); p.Detail != "chrome, edge" {
		t.Errorf("problem = %+v", p)
	}
	_, err = m.Use(ctx, "firefox")
	problemOf(t, err, UnknownBrowser)
	if out, err := m.Use(ctx, "auto"); err != nil || out != "ok browser: auto\n" || f.sys.Pin() != "" {
		t.Errorf("Use(auto) = %q, %v, pin %q", out, err, f.sys.Pin())
	}
}

func TestAPinToAnUninstalledBrowserSaysSo(t *testing.T) {
	f := newFake(t)
	f.install("chrome")
	writeFile(t, filepath.Join(f.sys.StateDir, "state.json"), "\xEF\xBB\xBF"+`{"browser":"vivaldi"}`) // a BOM, as PowerShell writes
	m := NewManager(f.sys)
	defer m.Close()
	_, err := m.Status(context.Background(), "")
	p := problemOf(t, err, NotInstalled)
	if !p.Pinned || p.Sentence() != "The pinned browser, Vivaldi, is not installed (installed: chrome)." {
		t.Errorf("problem = %q", p.Sentence())
	}
}

// realProfile writes a small real user-data-dir for clone.
func realProfile(t *testing.T, f *fakeMachine, name string) string {
	t.Helper()
	src := f.sys.Env.UserDataDirs()[name]
	writeFile(t, filepath.Join(src, "Local State"), `{"os_crypt":{"encrypted_key":"k"}}`)
	writeFile(t, filepath.Join(src, "Default", "Preferences"), "{}")
	writeFile(t, filepath.Join(src, "Default", "Network", "Cookies"), "cookies")
	writeFile(t, filepath.Join(src, "Default", "Cache", "Cache_Data", "f_000001"), "cache")
	writeFile(t, filepath.Join(src, "Profile 1", "Preferences"), "{}")
	return src
}

func TestCloneCopiesTheProfileAndLaunchesIt(t *testing.T) {
	f := newFake(t)
	f.install("chrome")
	realProfile(t, f, "chrome")
	s := cdptest.NewServer()
	defer s.Close()
	startsServer(f, s, 100)
	f.images[100] = f.sys.Env.CandidatePaths()["chrome"][0]
	m := NewManager(f.sys)
	defer m.Close()

	out, err := m.Clone(context.Background(), CloneOptions{Browser: "chrome"})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if want := "ok chrome clone started: pid 100, port " + strconv.Itoa(s.Port()) + ", profile \"Default\" (your logins)\n"; out != want {
		t.Errorf("out = %q, want %q", out, want)
	}
	clone := f.sys.CloneProfileDir("chrome")
	for _, p := range []string{"Local State", "Default/Preferences", "Default/Network/Cookies", "winuse-clone.json", "winuse-port"} {
		if !exists(filepath.Join(clone, p)) {
			t.Errorf("%s missing from the clone", p)
		}
	}
	if exists(filepath.Join(clone, "Default", "Cache")) {
		t.Error("the cache was copied")
	}
	if exists(filepath.Join(clone, "Profile 1")) {
		t.Error("another profile was copied")
	}
	args := f.startCalls()[0].args
	if !slices.Contains(args, "--profile-directory=Default") || !slices.Contains(args, "--user-data-dir="+clone) || slices.Contains(args, "--restore-last-session") || !slices.Contains(args, "--disable-features=CalculateNativeWinOcclusion") {
		t.Errorf("args = %q", args)
	}
}

func TestCloneSyncsAndWarnsAboutLockedCookies(t *testing.T) {
	f := newFake(t)
	f.install("chrome")
	src := realProfile(t, f, "chrome")
	s := cdptest.NewServer()
	defer s.Close()
	startsServer(f, s, 100)
	f.images[100] = f.sys.Env.CandidatePaths()["chrome"][0]
	m := NewManager(f.sys)
	defer m.Close()
	ctx := context.Background()
	if _, err := m.Clone(ctx, CloneOptions{Browser: "chrome"}); err != nil {
		t.Fatal(err)
	}
	// The clone exits; the real profile gains a bookmark file and loses
	// nothing; the second clone syncs.
	f.unlisten(s.Port())
	m.drop("chrome", nil)
	s.DropAll()
	writeFile(t, filepath.Join(src, "Default", "Bookmarks"), "new")
	out, err := m.Clone(ctx, CloneOptions{Browser: "chrome"})
	if err != nil {
		t.Fatalf("second Clone: %v", err)
	}
	if !strings.Contains(out, "\nnote: synced 1 file(s)") {
		t.Errorf("out = %q, want a sync note", out)
	}
	if !exists(filepath.Join(f.sys.CloneProfileDir("chrome"), "Default", "Bookmarks")) {
		t.Error("the new file was not synced")
	}
}

func TestCloneNamesTheProfilesThere(t *testing.T) {
	f := newFake(t)
	f.install("chrome")
	realProfile(t, f, "chrome")
	m := NewManager(f.sys)
	defer m.Close()
	_, err := m.Clone(context.Background(), CloneOptions{Browser: "chrome", Profile: "Profile 9"})
	p := problemOf(t, err, NoSuchProfile)
	if p.Sentence() != `Chrome has no profile folder "Profile 9"; it has Default, Profile 1.` {
		t.Errorf("sentence = %q", p.Sentence())
	}
	if len(f.startCalls()) != 0 {
		t.Error("a browser was started")
	}

	f2 := newFake(t)
	f2.install("edge")
	_, err = NewManager(f2.sys).Clone(context.Background(), CloneOptions{Browser: "edge"})
	problemOf(t, err, NoUserData)
}

func TestCloneCloseSourceClosesCleanlyAndRestoresTheSession(t *testing.T) {
	f := newFake(t)
	f.install("chrome")
	realProfile(t, f, "chrome")
	f.process("chrome", 100)
	s := cdptest.NewServer()
	defer s.Close()
	startsServer(f, s, 100)
	f.onClose = func(exe string) {
		f.mu.Lock()
		f.running[exe] = false
		f.mu.Unlock()
	}
	m := NewManager(f.sys)
	defer m.Close()
	if _, err := m.Clone(context.Background(), CloneOptions{Browser: "chrome", CloseSource: true}); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if !slices.Equal(f.closes, []string{"chrome.exe"}) {
		t.Errorf("closes = %q, want one clean close of chrome.exe", f.closes)
	}
	if args := f.startCalls()[0].args; !slices.Contains(args, "--restore-last-session") {
		t.Errorf("args = %q, want --restore-last-session", args)
	}
}

func TestTaskkillNeverForces(t *testing.T) {
	args := taskkillArgs("msedge.exe")
	if !slices.Equal(args, []string{"/IM", "msedge.exe"}) {
		t.Errorf("taskkill args = %q", args)
	}
	for _, a := range args {
		if strings.EqualFold(a, "/F") {
			t.Fatal("taskkill /F would tear the browser's databases")
		}
	}
}

func TestVersionWSReadsToContentLength(t *testing.T) {
	// cdptest serves /json/version with a Content-Length on a kept-alive
	// connection, as DevTools does: reading to EOF would hang.
	s := cdptest.NewServer()
	defer s.Close()
	got, err := versionWS(context.Background(), "http://127.0.0.1:"+strconv.Itoa(s.Port()))
	if err != nil || got != s.URL() {
		t.Errorf("versionWS = %q, %v, want %q", got, err, s.URL())
	}
}

func TestLiveSystemRefusesInTests(t *testing.T) {
	t.Setenv("WINUSE_E2E", "")
	sys := Live()
	ctx := context.Background()
	checks := map[string]error{
		"Start":     func() error { _, err := sys.Start(`C:\nowhere\chrome.exe`, nil); return err }(),
		"CloseApp":  sys.CloseApp("nothing-winuse-would-close.exe"),
		"Dial":      func() error { _, err := sys.Dial(ctx, "ws://127.0.0.1:1/devtools/browser/x", 0); return err }(),
		"VersionWS": func() error { _, err := sys.VersionWS(ctx, 1); return err }(),
	}
	for name, err := range checks {
		if !errors.Is(err, win.ErrLiveDesktopInTest) {
			t.Errorf("%s: err = %v, want ErrLiveDesktopInTest", name, err)
		}
	}
	if sys.PortAlive(1) {
		t.Error("PortAlive probed a real port")
	}
	if !strings.HasSuffix(sys.StateDir, filepath.Join("winuse", "browser")) || os.Getenv("LOCALAPPDATA") == "" {
		t.Errorf("state dir = %q", sys.StateDir)
	}
}
