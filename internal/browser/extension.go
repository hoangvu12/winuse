package browser

// The winuse extension route (browser merge ticket 02): discovery through
// the extension bridge, its self-update, and `browser ext install|status`.
// The bridge itself (the native host, the framing, the transport and the
// embedded extension) lives in internal/bridge. Ported from use-browser
// v0.9.0's ext.go (see third_party/use-browser/NOTICE), without its
// clipboard copy, its setup page opened in the user's browser and its
// five-minute wait: install prints the steps for the agent to relay.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/bridge"
	"github.com/hoangvu12/winuse/internal/cdp"
)

// How long the extension route waits.
const (
	probeWait  = 5 * time.Second  // one Winuse.status round trip
	updateWait = 15 * time.Second // a reloaded extension's new bridge (ub/ext.go:648)
)

// Registry is where Chromium browsers on Windows look up native messaging
// hosts: the default value of a key under HKCU. Live writes the real
// registry (and refuses to in a test binary); tests fake it.
type Registry interface {
	SetDefault(key, value string) error
	Default(key string) (string, error)
}

// chromeHostKey is Chrome's native messaging hosts key under HKCU, which
// Chromium forks may read instead of their own.
const chromeHostKey = `Software\Google\Chrome\NativeMessagingHosts\`

// hostKeys are the native messaging hosts keys under HKCU each browser may
// read (ub/ext.go:449-458), its own first. Chrome documents only its own.
// Brave on Windows reads Chrome's and not its vendor key (browser-ux 01:
// registered under BraveSoftware only, the extension never connected), so
// the forks get both; Opera reads Chrome's. Our host's manifest allows the
// winuse extension alone, so the extra key opens nothing else.
var hostKeys = map[string][]string{
	"chrome":   {chromeHostKey},
	"chromium": {`Software\Chromium\NativeMessagingHosts\`, chromeHostKey},
	"edge":     {`Software\Microsoft\Edge\NativeMessagingHosts\`},
	"brave":    {`Software\BraveSoftware\Brave-Browser\NativeMessagingHosts\`, chromeHostKey},
	"vivaldi":  {`Software\Vivaldi\NativeMessagingHosts\`, chromeHostKey},
	"opera":    {chromeHostKey},
}

// HostKey is the browser's own registry key (under HKCU) that registers
// winuse's native host.
func HostKey(name string) string { return HostKeys(name)[0] }

// HostKeys are every registry key (under HKCU) that registers winuse's
// native host for a browser: `ext install` writes them all.
func HostKeys(name string) []string {
	var out []string
	for _, k := range hostKeys[name] {
		out = append(out, k+bridge.HostName)
	}
	if len(out) == 0 {
		out = []string{bridge.HostName} // an unknown name: never reached, Known guards it
	}
	return out
}

// ExtensionDir is where `ext install` writes the unpacked extension the
// user loads.
func (s *System) ExtensionDir() string { return filepath.Join(s.StateDir, "extension") }

// HostManifestPath is the native host manifest the registry keys name.
func (s *System) HostManifestPath() string {
	return filepath.Join(s.StateDir, "native-host", bridge.HostName+".json")
}

// BridgesDir holds the running hosts' bridge files and host.log.
func (s *System) BridgesDir() string { return filepath.Join(s.StateDir, "bridges") }

// liveBridge is a bridge file with the browser it belongs to.
type liveBridge struct {
	info    bridge.Info
	browser string
}

// bridgeBrowser names the browser behind a bridge: the process that
// started the host decides (Chrome and Chromium share a user agent brand),
// then the extension's own guess.
func (s *System) bridgeBrowser(i bridge.Info) string {
	if name := s.Env.IdentifyImage(i.BrowserExe); name != "" {
		return name
	}
	if Known(i.Browser) {
		return i.Browser
	}
	return ""
}

// liveBridges lists the bridges of one browser (every browser for "").
func (s *System) liveBridges(want string) []liveBridge {
	if s.Bridges == nil {
		return nil
	}
	var out []liveBridge
	for _, i := range s.Bridges() {
		name := s.bridgeBrowser(i)
		if name == "" || (want != "" && name != want) {
			continue
		}
		out = append(out, liveBridge{info: i, browser: name})
	}
	return out
}

// extState is the extension's answer to Winuse.status.
type extState struct {
	Browser  string `json:"browser"`
	Version  string `json:"version"`
	Build    string `json:"build"`
	Instance string `json:"instance"`
	Paused   bool   `json:"paused"`
}

// bridgeCall makes one call through a bridge on a connection of its own.
func (s *System) bridgeCall(ctx context.Context, i bridge.Info, method string) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, probeWait)
	defer cancel()
	t, err := s.OpenBridge(ctx, i)
	if err != nil {
		return nil, err
	}
	c := cdp.NewClient(t)
	defer c.Close()
	return c.Call(ctx, method, nil)
}

// probeBridge asks the extension for its state. It is how a bridge that
// is up but turned off by the user shows itself.
func (s *System) probeBridge(ctx context.Context, i bridge.Info) (extState, error) {
	var st extState
	res, err := s.bridgeCall(ctx, i, bridge.MethodStatus)
	if err != nil {
		return st, err
	}
	err = json.Unmarshal(res, &st)
	return st, err
}

// currentBridge brings an extension whose build differs from the one in
// this binary up to date: rewrite its folder, have it reload itself from
// there, and wait for the bridge it comes back with. Unpacked extensions
// reload from disk, which is what makes a release reach the browser with
// nobody clicking (ub/ext.go:629-674).
func (s *System) currentBridge(ctx context.Context, b liveBridge) (liveBridge, error) {
	want := bridge.Build()
	if b.info.Build == want {
		return b, nil
	}
	if err := bridge.WriteExtension(s.ExtensionDir()); err != nil {
		return b, err
	}
	if _, err := s.bridgeCall(ctx, b.info, bridge.MethodReload); err != nil {
		return b, fmt.Errorf("asking it to reload: %w", err)
	}
	var found *liveBridge
	s.poll(ctx, updateWait, pollEvery, func() bool {
		for _, nb := range s.liveBridges(b.browser) {
			if nb.info.Instance == b.info.Instance && nb.info.Build == want {
				found = &nb
				return true
			}
		}
		return false
	})
	if found == nil {
		return b, fmt.Errorf("it did not reconnect within %s after reloading", updateWait)
	}
	return *found, nil
}

// extensionEndpoint is System.Extension: the endpoint of the extension
// connected to want (any browser for ""), or nil when there is none.
// Like the other routes it never guesses: the extension connected to two
// browsers with nothing named is Ambiguous, and two profiles of one
// browser is ExtSeveralProfiles. An extension the user turned off, or one
// that does not answer, is a Problem discovery remembers while it tries
// the other routes. An outdated extension is updated on the spot; when
// that fails, the old one keeps working.
func (s *System) extensionEndpoint(ctx context.Context, want string) (*Endpoint, error) {
	hits := s.liveBridges(want)
	if len(hits) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	var instances []string
	for _, h := range hits {
		seen[h.browser] = true
		instances = append(instances, h.info.Instance)
	}
	if len(seen) > 1 {
		var names []string
		for _, n := range Order {
			if seen[n] {
				names = append(names, n)
			}
		}
		return nil, &Problem{Kind: Ambiguous, Others: names}
	}
	if len(hits) > 1 {
		return nil, &Problem{Kind: ExtSeveralProfiles, Browser: hits[0].browser, Others: instances}
	}
	return s.readyBridge(ctx, hits[0])
}

// extensionEndpointFor is the endpoint of the extension running in one
// browser process (its bridge's browser_pid), or nil when none does. Two
// bridges of that process are two of its profiles: ExtSeveralProfiles,
// naming both, as in extensionEndpoint.
func (s *System) extensionEndpointFor(ctx context.Context, want string, pid uint32) (*Endpoint, error) {
	var hits []liveBridge
	var instances []string
	for _, b := range s.liveBridges(want) {
		if b.info.BrowserPID > 0 && uint32(b.info.BrowserPID) == pid {
			hits = append(hits, b)
			instances = append(instances, b.info.Instance)
		}
	}
	switch len(hits) {
	case 0:
		return nil, nil
	case 1:
		return s.readyBridge(ctx, hits[0])
	}
	return nil, &Problem{Kind: ExtSeveralProfiles, Browser: hits[0].browser, Others: instances}
}

// readyBridge brings a bridge's extension up to date and checks that it
// answers and is on: its endpoint, or the Problem discovery remembers.
func (s *System) readyBridge(ctx context.Context, b liveBridge) (*Endpoint, error) {
	if nb, err := s.currentBridge(ctx, b); err == nil {
		b = nb
	}
	st, err := s.probeBridge(ctx, b.info)
	if err != nil {
		return nil, &Problem{Kind: ExtUnreachable, Browser: b.browser, Detail: err.Error()}
	}
	if st.Paused {
		return nil, &Problem{Kind: ExtPaused, Browser: b.browser}
	}
	return s.bridgeEndpoint(b), nil
}

// bridgeEndpoint is the Endpoint of a bridge. Its WSURL is a label, which
// changes when the host restarts, so the manager dials again then.
func (s *System) bridgeEndpoint(b liveBridge) *Endpoint {
	info := b.info
	return &Endpoint{
		Browser: b.browser,
		Mode:    ModeExtension,
		WSURL:   fmt.Sprintf("winuse extension %s (instance %s)", info.Addr(), info.Instance),
		PID:     uint32(info.BrowserPID),
		Open: func(ctx context.Context, _ time.Duration) (cdp.Transport, error) {
			return s.OpenBridge(ctx, info)
		},
	}
}

// fallsThrough reports whether discovery tries the other routes after
// this extension problem: winuse may still use a browser it launched or
// cloned while the user has the extension off, or while it is stuck.
func fallsThrough(err error) (*Problem, bool) {
	var p *Problem
	if errors.As(err, &p) && (p.Kind == ExtPaused || p.Kind == ExtUnreachable) {
		return p, true
	}
	return nil, false
}

// AsProblem finds the Problem behind a Browser Connection error: a
// *Problem, or an extension bridge's answer to a call it does not run
// (ExtCannot) or cannot run now (ExtPaused). browser names the browser
// for the next command ("" for none).
func AsProblem(err error, browser string) (*Problem, bool) {
	var p *Problem
	if errors.As(err, &p) {
		return p, true
	}
	var ce *cdp.Error
	if errors.As(err, &ce) {
		switch ce.Code {
		case bridge.CodeRefused:
			return &Problem{Kind: ExtCannot, Browser: browser, Detail: ce.Method}, true
		case bridge.CodePaused:
			return &Problem{Kind: ExtPaused, Browser: browser}, true
		}
	}
	return nil, false
}

// ---- winuse browser ext install|status ----

// ExtInstall answers `winuse browser ext install [browser|all]`: write the
// unpacked extension and the native host manifest, register the host for
// each browser, and print the one step only the user can do: load the
// extension. It opens nothing in the user's browser (that would take the
// foreground); `--open` adds ExtOpen. With no browser it installs for the
// pinned one, else for every installed one.
func (m *Manager) ExtInstall(ctx context.Context, name string) (string, error) {
	targets, err := m.extTargets(name)
	if err != nil {
		return "", err
	}
	s := m.sys
	exe, err := s.Executable()
	if err != nil {
		return "", fmt.Errorf("finding the winuse binary: %w", err)
	}
	if strings.Contains(strings.ToLower(exe), "go-build") {
		return "", fmt.Errorf("%s is a temporary go run binary; build winuse and run winuse browser ext install from the built binary", exe)
	}
	if err := bridge.WriteExtension(s.ExtensionDir()); err != nil {
		return "", err
	}
	mp := s.HostManifestPath()
	if err := os.MkdirAll(filepath.Dir(mp), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(mp, bridge.HostManifest(exe), 0o644); err != nil {
		return "", err
	}
	var names []string
	for _, b := range targets {
		for _, k := range HostKeys(b.Name) {
			if err := s.Registry.SetDefault(k, mp); err != nil {
				return "", fmt.Errorf("registering the native host for %s: %w", b.Display(), err)
			}
		}
		names = append(names, b.Name)
	}

	var out strings.Builder
	fmt.Fprintf(&out, "ok extension written to %s (id %s, build %s)\n", s.ExtensionDir(), bridge.ExtensionID, bridge.Build())
	fmt.Fprintf(&out, "ok native host %s registered for %s (%s)\n", bridge.HostName, strings.Join(names, ", "), exe)
	var waiting []Browser
	for _, b := range targets {
		if hits := m.sys.liveBridges(b.Name); len(hits) > 0 {
			note := ""
			if _, err := s.currentBridge(ctx, hits[0]); err != nil {
				note = fmt.Sprintf("; it is out of date and did not reload itself (%v), so ask the user to click its reload icon at %s://extensions", err, Scheme(b.Name))
			}
			fmt.Fprintf(&out, "ok %s is already connected through the winuse extension%s\n", b.Name, note)
			continue
		}
		waiting = append(waiting, b)
	}
	if len(waiting) == 0 {
		return out.String(), nil
	}
	out.WriteString("only the user can do the last step, once per browser. Tell them:\n")
	for _, b := range waiting {
		out.WriteString(LoadSteps(b.Name, s.ExtensionDir()))
	}
	for _, b := range waiting {
		fmt.Fprintf(&out, "then run: winuse browser ext status %s --wait 120 (exit 0 once it is connected)\n", b.Name)
	}
	return out.String(), nil
}

// ExtOpen answers `winuse browser ext install [browser] --open` after the
// install (browser-ux 02): for each browser the extension is not yet
// connected to, it opens <scheme>://extensions in that browser (a new Tab
// of the running browser, which takes the foreground: only on request),
// then one Explorer window with the extension folder selected, so the user
// can drag the folder onto the page or copy its path. Nothing is clicked
// for the user.
func (m *Manager) ExtOpen(ctx context.Context, name string) (string, error) {
	targets, err := m.extTargets(name)
	if err != nil {
		return "", err
	}
	s := m.sys
	var out strings.Builder
	opened := 0
	for _, b := range targets {
		if len(s.liveBridges(b.Name)) > 0 {
			continue // already connected: nothing to load
		}
		page := Scheme(b.Name) + "://extensions"
		if _, err := s.Start(b.Path, []string{page}); err != nil {
			fmt.Fprintf(&out, "note: could not open %s in %s (%v); the user types it in the address bar\n", page, b.Display(), err)
			continue
		}
		fmt.Fprintf(&out, "ok opened %s in %s\n", page, b.Display())
		opened++
	}
	if opened == 0 {
		return out.String(), nil
	}
	explorer := filepath.Join(os.Getenv("WINDIR"), "explorer.exe")
	if _, err := s.Start(explorer, []string{"/select," + s.ExtensionDir()}); err != nil {
		fmt.Fprintf(&out, "note: could not show the extension folder in Explorer (%v)\n", err)
	} else {
		fmt.Fprintf(&out, "ok showed the extension folder in Explorer: with Developer mode on, the user can drag it onto the extensions page instead of using Load unpacked\n")
	}
	return out.String(), nil
}

// LoadSteps is the manual step, worded for a person: an agent pastes it
// as is.
func LoadSteps(name, dir string) string {
	return fmt.Sprintf("  In %s:\n  1. Type %s://extensions in the address bar.\n  2. Turn on \"Developer mode\" (top right).\n  3. Click \"Load unpacked\" and choose this folder:\n     %s\n",
		DisplayName(name), Scheme(name), dir)
}

// extTargets resolves the browsers `ext install` acts on: "all" or no
// name and no pin is every installed browser; a name or the pin is that
// one.
func (m *Manager) extTargets(name string) ([]Browser, error) {
	name = normalizeName(name)
	pinned := false
	if name == "" {
		name, pinned = m.sys.Pin(), true
	}
	if name == "" || name == "all" {
		found := m.sys.detect()
		if len(found) == 0 {
			return nil, &Problem{Kind: NoBrowser}
		}
		return found, nil
	}
	b, err := m.installed(name, pinned)
	if err != nil {
		return nil, err
	}
	return []Browser{b}, nil
}

// ExtStatus answers `winuse browser ext status [browser]`: whether a
// browser is reachable through the extension and, when it is not, which
// step is missing, in one sentence and the next command. It succeeds
// (exit 0) only when connected, so an agent can poll it. With no browser
// and no pin it reports every browser the extension is connected to, or
// diagnoses the first running installed browser.
func (m *Manager) ExtStatus(ctx context.Context, name string) (string, error) {
	want, err := m.target(name)
	if err != nil {
		return "", err
	}
	if want != "" {
		return m.diagnoseExt(ctx, want)
	}
	installed := m.sys.detect()
	if len(installed) == 0 {
		return "", &Problem{Kind: NoBrowser}
	}
	var connected []string
	for _, b := range installed {
		if len(m.sys.liveBridges(b.Name)) > 0 {
			connected = append(connected, b.Name)
		}
	}
	if len(connected) == 0 {
		target := installed[0]
		for _, b := range installed {
			if m.sys.running(b) {
				target = b
				break
			}
		}
		return m.diagnoseExt(ctx, target.Name)
	}
	var out strings.Builder
	var firstErr error
	for _, n := range connected {
		line, err := m.diagnoseExt(ctx, n)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		out.WriteString(line)
	}
	if out.Len() == 0 {
		return "", firstErr
	}
	return out.String(), nil
}

// diagnoseExt says one browser's extension state, the most specific
// cause first (ub/ext.go:748-783).
func (m *Manager) diagnoseExt(ctx context.Context, name string) (string, error) {
	s := m.sys
	hits := s.liveBridges(name)
	note := ""
	if len(hits) == 0 {
		if ended := s.endStrayHosts(name); len(ended) > 0 {
			// An older winuse's host held the extension's port with no bridge
			// file any winuse reads; with it ended, the extension starts the
			// registered host again.
			wait := s.HostReconnectWait
			if wait <= 0 {
				wait = hostReconnectWait
			}
			s.poll(ctx, wait, pollEvery, func() bool {
				hits = s.liveBridges(name)
				return len(hits) > 0
			})
			if len(hits) == 0 {
				return "", &Problem{Kind: ExtStrayHost, Browser: name, Detail: strings.Join(ended, ", ")}
			}
			note = fmt.Sprintf("note: ended a winuse native host no winuse could reach (%s), which held the extension in %s; it connected again\n", strings.Join(ended, ", "), DisplayName(name))
		}
	}
	out, err := m.diagnoseHits(ctx, name, hits)
	if err != nil {
		return "", err
	}
	return out + note, nil
}

// hostReconnectWait is how long ext status waits for the extension to
// connect again after it ended a stray host: the extension's reconnect
// alarm fires every 30 s.
const hostReconnectWait = 35 * time.Second

// endStrayHosts ends the winuse native hosts that a browser started and no
// bridge file names, and returns them as "pid N (exe)". Such a host is an
// older winuse's whose bridge file is gone: it keeps the extension's port,
// so the extension never starts the current host, and no helper reaches
// it (the upgrade case: the running winuse.exe renamed, browser-ux).
func (s *System) endStrayHosts(name string) []string {
	if s.NativeHosts == nil || s.EndHost == nil {
		return nil
	}
	var exe string
	for _, b := range s.detect() {
		if b.Name == name {
			exe = filepath.Base(b.Path)
		}
	}
	if exe == "" {
		return nil
	}
	named := map[int]bool{}
	if s.Bridges != nil {
		for _, i := range s.Bridges() {
			named[i.PID] = true
		}
	}
	var ended []string
	for _, h := range s.NativeHosts() {
		if named[h.PID] || !strings.EqualFold(filepath.Base(h.BrowserExe), exe) {
			continue
		}
		if err := s.EndHost(h.PID); err == nil {
			ended = append(ended, fmt.Sprintf("pid %d, %s", h.PID, h.Exe))
		}
	}
	return ended
}

// diagnoseHits is diagnoseExt once the browser's bridges are known.
func (m *Manager) diagnoseHits(ctx context.Context, name string, hits []liveBridge) (string, error) {
	s := m.sys
	if len(hits) > 1 {
		var instances []string
		for _, h := range hits {
			instances = append(instances, h.info.Instance)
		}
		return "", &Problem{Kind: ExtSeveralProfiles, Browser: name, Others: instances}
	}
	if len(hits) == 1 {
		b := hits[0]
		updated := false
		if b.info.Build != bridge.Build() {
			nb, err := s.currentBridge(ctx, b)
			if err != nil {
				return "", &Problem{Kind: ExtOutdated, Browser: name, Detail: err.Error()}
			}
			b, updated = nb, true
		}
		st, err := s.probeBridge(ctx, b.info)
		if err != nil {
			return "", &Problem{Kind: ExtUnreachable, Browser: name, Detail: err.Error()}
		}
		if st.Paused {
			return "", &Problem{Kind: ExtPaused, Browser: name}
		}
		out := fmt.Sprintf("ok %s connected through the winuse extension (version %s, instance %s)\n", name, st.Version, st.Instance)
		if updated {
			out += fmt.Sprintf("note: updated the winuse extension in %s to build %s\n", DisplayName(name), bridge.Build())
		}
		return out, nil
	}
	if p := s.hostProblem(name); p != nil {
		return "", p
	}
	loaded, enabled := s.extInProfiles(name)
	switch {
	case len(loaded) == 0:
		return "", &Problem{Kind: ExtNotLoaded, Browser: name, Dir: s.ExtensionDir()}
	case len(enabled) == 0:
		return "", &Problem{Kind: ExtDisabled, Browser: name, Detail: strings.Join(loaded, ", ")}
	}
	for _, b := range s.detect() {
		if b.Name == name && !s.running(b) {
			return "", &Problem{Kind: ExtBrowserClosed, Browser: name}
		}
	}
	return "", &Problem{Kind: ExtNotConnected, Browser: name, Detail: bridge.LastLogLine(s.BridgesDir())}
}

// hostProblem checks what a browser needs to start winuse's host: the
// extension folder, the host manifest, the binary it names, and the
// browser's registry key naming the manifest.
func (s *System) hostProblem(name string) *Problem {
	if !s.Exists(filepath.Join(s.ExtensionDir(), "manifest.json")) {
		return &Problem{Kind: ExtNotInstalled, Browser: name}
	}
	b, err := os.ReadFile(s.HostManifestPath())
	if err != nil {
		return &Problem{Kind: ExtNotInstalled, Browser: name}
	}
	var hm struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF}), &hm) != nil || hm.Path == "" {
		return &Problem{Kind: ExtNotInstalled, Browser: name}
	}
	if !s.Exists(hm.Path) {
		return &Problem{Kind: ExtNotInstalled, Browser: name, Detail: hm.Path}
	}
	for _, k := range HostKeys(name) {
		v, err := s.Registry.Default(k)
		if err != nil || !strings.EqualFold(filepath.Clean(v), filepath.Clean(s.HostManifestPath())) {
			return &Problem{Kind: ExtNotRegistered, Browser: name}
		}
	}
	return nil
}

// extInProfiles reads the browser's own records of which profiles have
// the extension, and which of those have it enabled (ub/ext.go:819-862).
// Opera keeps its one profile in the user-data-dir itself.
func (s *System) extInProfiles(name string) (loaded, enabled []string) {
	root := s.Env.UserDataDirs()[name]
	if root == "" {
		return nil, nil // an unknown name; "" would read the working directory
	}
	dirs, _ := filepath.Glob(filepath.Join(root, "*"))
	dirs = append([]string{root}, dirs...)
	for _, d := range dirs {
		base := filepath.Base(d)
		if d != root && base != "Default" && !strings.HasPrefix(base, "Profile ") {
			continue
		}
		for _, f := range []string{"Secure Preferences", "Preferences"} {
			b, err := os.ReadFile(filepath.Join(d, f))
			if err != nil {
				continue
			}
			var prefs struct {
				Extensions struct {
					Settings map[string]struct {
						DisableReasons json.RawMessage `json:"disable_reasons"`
						State          *int            `json:"state"`
					} `json:"settings"`
				} `json:"extensions"`
			}
			if json.Unmarshal(b, &prefs) != nil {
				continue
			}
			st, ok := prefs.Extensions.Settings[bridge.ExtensionID]
			if !ok {
				continue
			}
			loaded = append(loaded, base)
			// disable_reasons is a bit mask in older builds, a list in newer.
			r := strings.TrimSpace(string(st.DisableReasons))
			off := r != "" && r != "0" && r != "[]" && r != "null"
			if st.State != nil && *st.State == 0 {
				off = true
			}
			if !off {
				enabled = append(enabled, base)
			}
			break
		}
	}
	return loaded, enabled
}
