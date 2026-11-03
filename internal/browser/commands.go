package browser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// How long the lifecycle commands wait. Each stays inside its command's
// request budget (protocol.CommandTimeout).
const (
	launchWait  = 15 * time.Second // a started browser's endpoint (ub/browsers.go:526)
	cloneWait   = 20 * time.Second // a started clone's endpoint (ub/clone.go:627)
	connectWait = 55 * time.Second // the user turning on the inspect toggle (use-browser waited 90 s)
	stopWait    = 15 * time.Second // a closed browser releasing its profile
	pollEvery   = 250 * time.Millisecond
)

// Status answers `winuse browser status [browser]`: connect (or reuse the
// held connection) and say how, or fail with the one sentence and next
// command for whatever is wrong. Exit 0 means connected, so an agent can
// poll it. Other connections the helper holds (another process of the
// browser, another browser) follow as one "also connected" line each, and
// then every line names its process.
func (m *Manager) Status(ctx context.Context, name string) (string, error) {
	c, out, err := m.status(ctx, name)
	if err != nil {
		return "", err
	}
	var also []string
	for _, o := range m.open("") {
		if o != c {
			also = append(also, fmt.Sprintf("also connected: %s process %d, %s\n", o.Browser, o.PID, o.Mode.Describe()))
		}
	}
	if len(also) == 0 {
		return out, nil
	}
	out = strings.Replace(out, " connected: ", fmt.Sprintf(" process %d connected: ", c.PID), 1)
	return out + strings.Join(also, ""), nil
}

// status is Status for the one connection it resolves.
func (m *Manager) status(ctx context.Context, name string) (*Connection, string, error) {
	c, err := m.Connection(ctx, name)
	if err != nil {
		return nil, "", err
	}
	pages, err := c.Pages(ctx)
	if err != nil {
		if p, ok := AsProblem(err, c.Browser); ok {
			return nil, "", p // the user turned the extension off
		}
		if c.Mode == ModeExtension {
			return nil, "", &Problem{Kind: ExtUnreachable, Browser: c.Browser, Detail: err.Error()}
		}
		return nil, "", &Problem{Kind: Unreachable, Browser: c.Browser, Port: c.Port, Detail: err.Error()}
	}
	return c, fmt.Sprintf("ok %s connected: %s, %d %s\n", c.Browser, c.Mode.Describe(), len(pages), plural(len(pages), "tab", "tabs")), nil
}

// Use answers `winuse browser use [browser|auto]`: pin the browser every
// later command uses when none is named (it persists across helpers), or
// with no argument print the pin and the installed browsers. Pinning keeps
// a second debuggable browser from ever being picked instead.
func (m *Manager) Use(ctx context.Context, name string) (string, error) {
	name = normalizeName(name)
	if name == "" {
		var names []string
		for _, b := range m.sys.detect() {
			names = append(names, b.Name)
		}
		installed := strings.Join(names, " ")
		if installed == "" {
			installed = "none"
		}
		pin := m.sys.Pin()
		if pin == "" {
			return "browser: auto (no pin: the one debuggable browser; with two, winuse asks)\ninstalled: " + installed + "\n", nil
		}
		return "browser: " + pin + " (pinned)\ninstalled: " + installed + "\n", nil
	}
	if name == "auto" {
		if err := m.sys.SetPin(""); err != nil {
			return "", err
		}
		return "ok browser: auto\n", nil
	}
	b, err := m.installed(name, false)
	if err != nil {
		return "", err
	}
	if err := m.sys.SetPin(b.Name); err != nil {
		return "", err
	}
	return fmt.Sprintf("ok browser: %s (%s)\n", b.Name, b.Path), nil
}

// launchArgs are the flags of a browser winuse launches: a loopback debug
// port (Chromium binds 127.0.0.1 for it), a non-default user-data-dir
// (required since Chromium 136), no first-run pages
// (ub/browsers.go:514-519), and no native occlusion tracking.
//
// Chromium marks a covered window occluded
// (ui/aura/native_window_occlusion_tracker_win.cc) and stops rendering it;
// a cold, fully covered Chrome kept a 36-node UIA tree for 14 s, and
// settled in about 0.1 s with CalculateNativeWinOcclusion off. The Page
// goes through CDP, but the frame (tab strip, address bar, dialogs) and the
// UIA fallback still need the tree.
//
// msImplicitSignin is off too: Edge signs a new profile in to the Windows
// account on its own, turns on sync of that account's data, and puts a
// modal "Sync your profile" dialog over the page (the laptop's live run,
// 2026-11-03), so winuse's profile would hold the user's synced data and
// the Page would sit behind the dialog. The user can still sign in by
// hand. Chrome ignores the unknown feature name.
func launchArgs(port int, userDataDir string) []string {
	return disableFeatures([]string{
		"--remote-debugging-port=" + strconv.Itoa(port),
		"--user-data-dir=" + userDataDir,
		"--no-first-run",
		"--no-default-browser-check",
	}, "CalculateNativeWinOcclusion", "msImplicitSignin")
}

// disableFeatures adds features to the --disable-features switch in args,
// or appends one. One switch, not two: Chromium's command line keeps one
// value per switch, so a repeated --disable-features can drop the first
// one's features.
func disableFeatures(args []string, features ...string) []string {
	const prefix = "--disable-features="
	for i, a := range args {
		if !strings.HasPrefix(a, prefix) {
			continue
		}
		have := strings.Split(strings.TrimPrefix(a, prefix), ",")
		for _, f := range features {
			if !slices.Contains(have, f) {
				have = append(have, f)
			}
		}
		have = slices.DeleteFunc(have, func(s string) bool { return s == "" })
		out := slices.Clone(args)
		out[i] = prefix + strings.Join(have, ",")
		return out
	}
	return append(args, prefix+strings.Join(features, ","))
}

// Launch answers `winuse browser launch [browser]`: start the browser with
// winuse's own persistent profile and connect to it. The browser opens a
// normal, headed window. It pins the browser.
func (m *Manager) Launch(ctx context.Context, name string) (string, error) {
	b, err := m.pick(name, false)
	if err != nil {
		return "", err
	}
	if err := m.sys.SetPin(b.Name); err != nil {
		return "", err
	}
	// Only an instance winuse started counts as already running: adopting
	// the user's browser behind the inspect toggle would put every command
	// behind its approval prompt, which launch exists to avoid.
	if ep := m.ownEndpoints(ctx, b.Name); len(ep) > 0 {
		if _, err := m.dial(ctx, ep[0]); err != nil {
			return "", err
		}
		return fmt.Sprintf("ok %s already running from winuse's %s profile (port %d)\n", b.Name, ep[0].Mode, ep[0].Port), nil
	}
	dir := m.sys.LaunchProfileDir(b.Name)
	if m.profileInUse(dir) {
		return "", &Problem{Kind: ProfileBusy, Browser: b.Name, Mode: ModeLaunch, Dir: dir}
	}
	port := m.sys.FreePort()
	if err := writePortFile(dir, port); err != nil {
		return "", err
	}
	fgBefore := m.foreground()
	pid, err := m.sys.Start(b.Path, launchArgs(port, dir))
	if err != nil {
		return "", fmt.Errorf("starting %s: %w", b.Path, err)
	}
	ep, err := m.awaitOwn(ctx, b.Name, ModeLaunch, dir, launchWait, pid, port)
	if err != nil {
		return "", err
	}
	if _, err := m.dial(ctx, ep); err != nil {
		return "", err
	}
	note := ""
	if m.tookForeground(fgBefore, pid) {
		note = " (the browser took the foreground)"
	}
	return fmt.Sprintf("ok %s launched: pid %d, port %d, profile %s%s\n", b.Name, pid, port, dir, note), nil
}

// foreground is the foreground Window and its process, or zeros without a
// probe.
func (m *Manager) foreground() foregroundSnapshot {
	if m.sys.Foreground == nil {
		return foregroundSnapshot{}
	}
	w, pid := m.sys.Foreground()
	return foregroundSnapshot{window: w, pid: pid}
}

// foregroundSnapshot is the foreground Window and its process at one
// moment.
type foregroundSnapshot struct {
	window uint64
	pid    uint32
}

// tookForeground reports whether the browser started as pid took the
// foreground during the launch (headless-max ticket 01): the foreground
// changed since before, to a Window of that process. The browser starts
// with a non-activating show state, but the helper can hold the right to
// set the foreground (after a Headed Action's input, until the user's next
// input) and its child inherits it. winuse never takes the foreground back.
func (m *Manager) tookForeground(before foregroundSnapshot, pid int) bool {
	if m.sys.Foreground == nil || pid <= 0 {
		return false
	}
	after := m.foreground()
	return after.window != 0 && after.window != before.window && after.pid == uint32(pid)
}

// Connect answers `winuse browser connect [browser]`: use the user's
// running browser and real profile through its inspect toggle. It opens
// <browser>://inspect/#remote-debugging in that browser (a new tab, or the
// browser starts), waits for the user to turn on "Allow remote debugging
// for this browser instance", then connects; Chrome 144+ asks the user to
// approve the connection, once per helper. It pins the browser.
func (m *Manager) Connect(ctx context.Context, name string) (string, error) {
	b, err := m.pick(name, true)
	if err != nil {
		return "", err
	}
	if err := m.sys.SetPin(b.Name); err != nil {
		return "", err
	}
	d := profileDir{path: m.sys.Env.UserDataDirs()[b.Name], browser: b.Name, mode: ModeToggle}
	ep, prob := m.sys.probeDir(ctx, d)
	if prob != nil {
		return "", prob
	}
	if ep == nil {
		if _, err := m.sys.Start(b.Path, []string{InspectURL(b.Name)}); err != nil {
			return "", fmt.Errorf("opening %s: %w", InspectURL(b.Name), err)
		}
		m.sys.poll(ctx, connectWait, 500*time.Millisecond, func() bool {
			ep, prob = m.sys.probeDir(ctx, d)
			return ep != nil || prob != nil
		})
		if prob != nil {
			return "", prob
		}
		if ep == nil {
			return "", &Problem{Kind: ToggleOff, Browser: b.Name}
		}
	}
	if _, err := m.dial(ctx, *ep); err != nil {
		return "", err
	}
	return fmt.Sprintf("ok %s connected: %s\n", b.Name, ModeToggle.Describe()), nil
}

// Stop answers `winuse browser stop [browser]`: close a browser winuse
// launched or cloned, cleanly (Browser.close), and keep its profile for
// next time. The user's own browser is never closed: only winuse's
// profile directories are looked at.
func (m *Manager) Stop(ctx context.Context, name string) (string, error) {
	b, err := m.pick(name, false)
	if err != nil {
		return "", err
	}
	live := m.ownEndpoints(ctx, b.Name)
	if len(live) == 0 {
		m.drop(b.Name, func(c *Connection) bool { return c.Mode == ModeLaunch || c.Mode == ModeClone })
		return fmt.Sprintf("ok %s already stopped (winuse stops only browsers it launched or cloned)\n", b.Name), nil
	}
	if len(live) > 1 {
		return "", &Problem{Kind: SeveralOwned, Browser: b.Name}
	}
	ep := live[0]
	c, err := m.dial(ctx, ep)
	if err != nil {
		return "", err
	}
	// The browser closes the socket as it exits, often before it answers.
	c.Client.Call(ctx, "Browser.close", nil)
	stopped := m.sys.poll(ctx, stopWait, pollEvery, func() bool {
		still, _ := m.sys.probeDir(ctx, profileDir{ep.Dir, b.Name, ep.Mode})
		return still == nil && !m.profileInUse(ep.Dir)
	})
	m.dropEndpoint(ep)
	if !stopped {
		return "", fmt.Errorf("%s did not stop within %s", b.Display(), stopWait)
	}
	return fmt.Sprintf("ok %s stopped; its winuse %s profile is kept at %s\n", b.Name, ep.Mode, ep.Dir), nil
}

// ownEndpoints lists the live, verified endpoints of the browsers winuse
// started for a browser: its launch profile, then its clone.
func (m *Manager) ownEndpoints(ctx context.Context, name string) []Endpoint {
	var out []Endpoint
	for _, d := range []profileDir{
		{m.sys.LaunchProfileDir(name), name, ModeLaunch},
		{m.sys.CloneProfileDir(name), name, ModeClone},
	} {
		if ep, _ := m.sys.probeDir(ctx, d); ep != nil {
			out = append(out, *ep)
		}
	}
	return out
}

// awaitOwn waits for a browser winuse just started to serve DevTools from
// its profile directory. Waiting on that directory, not on any endpoint,
// keeps a toggle-mode browser from passing for the one just started.
func (m *Manager) awaitOwn(ctx context.Context, name string, mode Mode, dir string, wait time.Duration, pid, port int) (Endpoint, error) {
	var ep *Endpoint
	var prob *Problem
	m.sys.poll(ctx, wait, pollEvery, func() bool {
		ep, prob = m.sys.probeDir(ctx, profileDir{dir, name, mode})
		return ep != nil || prob != nil
	})
	if prob != nil {
		return Endpoint{}, prob
	}
	if ep == nil {
		return Endpoint{}, &Problem{Kind: NotStarted, Browser: name, Mode: mode, PID: pid, Port: port}
	}
	return *ep, nil
}

// profileInUse reports whether a browser holds a user-data-dir. Writing
// into a live profile corrupts it, and a second browser started on it
// hands its command line to the first and exits. DevToolsActivePort alone
// is not enough (Chrome and Brave often never write it), so Chromium's
// "lockfile", held exclusively while it runs, is the real signal; a stale
// one left by a crash opens again (ub/clone.go:371-399).
func (m *Manager) profileInUse(dir string) bool {
	if port, _ := activePort(dir); port != 0 && m.sys.PortAlive(port) {
		return true
	}
	f, err := os.OpenFile(filepath.Join(dir, "lockfile"), os.O_RDWR, 0)
	if err == nil {
		f.Close()
		return false
	}
	return !os.IsNotExist(err)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
