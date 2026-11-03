package browser

// App Browser Connections (headless-max ticket 15, ADR 0013 "Reversals",
// ADR 0012 Decision 6): `launch-app --cdp` starts an Electron or WebView2
// App with a loopback debugging port, and its Windows then get a Browser
// Connection like a Chromium browser's. Measured in probe round 5
// (`.scratch/headless-gaps/probes-round5.md` section 3): Electron takes
// `--remote-debugging-port`, and its main process (electron.exe, which owns
// the App's Windows) listens; WebView2 takes the port through
// WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS in the launched App's environment,
// and an msedgewebview2.exe child of the App listens.
//
// winuse picks the port itself (an ephemeral loopback port, never one of
// the 9222 to 9224 ports browser discovery probes) and accepts it only when
// its listener is the launched process or one of its descendants: never
// another process of the user's. The endpoint is registered under the
// process that owns the App's Windows, so a Window of that process reaches
// it through WindowConnection.

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// The App kinds, used where a browser's short name goes.
const (
	AppElectron = "electron"
	AppWebView2 = "webview2"
)

// ModeApp is an App winuse launched with --cdp.
const ModeApp Mode = "app"

// appDisplayNames are the App kinds' display forms.
var appDisplayNames = map[string]string{AppElectron: "Electron App", AppWebView2: "WebView2 App"}

// IsApp reports whether a short name is an App kind rather than a browser.
func IsApp(name string) bool {
	_, ok := appDisplayNames[name]
	return ok
}

// IsElectron reports whether the executable at path is an Electron App: an
// app.asar, electron.asar or default_app.asar in the resources folder next
// to it (default_app.asar is a bare electron.exe).
func IsElectron(exePath string, exists func(string) bool) bool {
	if exists == nil {
		exists = fileExists
	}
	res := filepath.Join(filepath.Dir(exePath), "resources")
	for _, f := range []string{"app.asar", "electron.asar", "default_app.asar"} {
		if exists(filepath.Join(res, f)) {
			return true
		}
	}
	return exists(filepath.Join(res, "app", "package.json"))
}

// ElectronArgs puts the debugging port in front of an Electron App's own
// arguments, and turns native occlusion tracking off as browser launch does
// (launchArgs): a covered Electron Window otherwise keeps a thin UI
// Automation tree, and the Page is spliced into that tree.
func ElectronArgs(port int, args []string) []string {
	return disableFeatures(append([]string{"--remote-debugging-port=" + strconv.Itoa(port)}, args...), "CalculateNativeWinOcclusion")
}

// WebView2ArgsVar is the environment variable a WebView2 App's browser
// processes read their extra switches from.
const WebView2ArgsVar = "WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS"

// WebView2Env adds the debugging port to WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS
// in the launched App's own environment entries (KEY=VALUE, over the
// helper's environment), after any value the agent passed with --env or,
// failing that, the helper's own (inherited). Never the machine-wide
// registry policy.
func WebView2Env(port int, env []string, inherited string) []string {
	flag := "--remote-debugging-port=" + strconv.Itoa(port)
	out := make([]string, 0, len(env)+1)
	value := inherited
	for _, e := range env {
		if k, v, ok := strings.Cut(e, "="); ok && strings.EqualFold(k, WebView2ArgsVar) {
			value = v
			continue
		}
		out = append(out, e)
	}
	if value = strings.TrimSpace(value); value != "" {
		flag = value + " " + flag
	}
	return append(out, WebView2ArgsVar+"="+flag)
}

// AppEndpoint is a verified debugging endpoint of an App winuse launched.
type AppEndpoint struct {
	Kind  string // AppElectron or AppWebView2
	PID   uint32 // the process that owns the App's Windows
	Owner uint32 // the process that listens on the port
	Port  int
	WSURL string
}

// appPollEvery paces AwaitApp's checks.
const appPollEvery = 200 * time.Millisecond

// maxAppTreeDepth bounds the walk from the port's listener up to the
// launched process.
const maxAppTreeDepth = 8

// AppPort is a free ephemeral loopback port for an App's debugging port.
func (m *Manager) AppPort() (int, error) {
	if m.sys.AppPort == nil {
		return 0, fmt.Errorf("no free loopback port could be picked")
	}
	return m.sys.AppPort()
}

// AwaitApp waits until wait passes for the App winuse launched (pid
// launched) to answer on its debugging port, verifies that the port's
// listener is the launched process or one of its descendants, and
// registers the endpoint for the Windows of windowPID. False when no
// verified endpoint appeared: the App stays on UI Automation.
func (m *Manager) AwaitApp(ctx context.Context, kind string, launched, windowPID uint32, port int, wait time.Duration) (AppEndpoint, bool) {
	deadline := m.appNow().Add(wait)
	for {
		if ep, ok := m.probeApp(ctx, kind, launched, windowPID, port); ok {
			m.mu.Lock()
			if m.apps == nil {
				m.apps = map[uint32]AppEndpoint{}
			}
			m.apps[windowPID] = ep
			m.mu.Unlock()
			return ep, true
		}
		if ctx.Err() != nil || !m.appNow().Before(deadline) {
			return AppEndpoint{}, false
		}
		m.appSleep(appPollEvery)
	}
}

// probeApp is one check of an App's port: something answers
// /json/version, and its listener belongs to the launched process tree.
func (m *Manager) probeApp(ctx context.Context, kind string, launched, windowPID uint32, port int) (AppEndpoint, bool) {
	if m.sys.ListenerPID == nil || m.sys.VersionWS == nil {
		return AppEndpoint{}, false
	}
	owner, ok := m.sys.ListenerPID(port)
	if !ok || !m.inTree(owner, launched) {
		return AppEndpoint{}, false
	}
	ws, err := m.sys.VersionWS(ctx, port)
	if err != nil {
		return AppEndpoint{}, false
	}
	return AppEndpoint{Kind: kind, PID: windowPID, Owner: owner, Port: port, WSURL: ws}, true
}

// inTree reports whether pid is root or one of its descendants.
func (m *Manager) inTree(pid, root uint32) bool {
	for i := 0; i < maxAppTreeDepth && pid != 0; i++ {
		if pid == root {
			return true
		}
		if m.sys.ParentPID == nil {
			return false
		}
		parent, ok := m.sys.ParentPID(pid)
		if !ok || parent == pid {
			return false
		}
		pid = parent
	}
	return false
}

// AppKind is the App kind registered for the process that owns a Window,
// or "" when winuse launched no App with --cdp there.
func (m *Manager) AppKind(pid uint32) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.apps[pid].Kind
}

// appEndpoint is the registered endpoint of a Window's process.
func (m *Manager) appEndpoint(pid uint32) (AppEndpoint, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ep, ok := m.apps[pid]
	return ep, ok
}

// appNow and appSleep are the System's clock, or the real one.
func (m *Manager) appNow() time.Time {
	if m.sys.Now != nil {
		return m.sys.Now()
	}
	return time.Now()
}

func (m *Manager) appSleep(d time.Duration) {
	if m.sys.Sleep != nil {
		m.sys.Sleep(d)
		return
	}
	time.Sleep(d)
}
