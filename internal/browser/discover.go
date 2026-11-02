package browser

import (
	"context"
	"fmt"
	"maps"
	"net"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp"
)

// Mode is the route a Browser Connection takes to its browser.
type Mode string

const (
	ModeLaunch    Mode = "launch"    // winuse's own profile (browser launch)
	ModeClone     Mode = "clone"     // winuse's synced copy of the user's profile (browser clone)
	ModeToggle    Mode = "toggle"    // the user's real profile through the inspect toggle (browser connect)
	ModePort      Mode = "port"      // a debugging port found on 9222 to 9224
	ModeExtension Mode = "extension" // the winuse extension (ticket 02)
)

// Describe says in a few words how winuse reaches the browser.
func (m Mode) Describe() string {
	switch m {
	case ModeLaunch:
		return "winuse's own profile"
	case ModeClone:
		return "winuse's copy of your profile, with your logins"
	case ModeToggle:
		return "your real profile through the inspect toggle"
	case ModeExtension:
		return "your running browser through the winuse extension"
	}
	return "a debugging port on this machine"
}

// Endpoint is a verified way to one browser: its loopback WebSocket URL,
// the port's owner and the route winuse took to it.
type Endpoint struct {
	Browser string // short name
	Mode    Mode
	WSURL   string
	Port    int
	PID     uint32 // the browser process that owns the port
	Dir     string // the profile directory it was found through ("" for a port probe)

	// Open opens a transport that is not a DevTools WebSocket (ticket 02's
	// extension bridge). Nil dials WSURL through System.Dial.
	Open func(ctx context.Context, handshake time.Duration) (cdp.Transport, error)
}

// key names the browser process behind an endpoint for the manager: its
// pid, or the browser and URL when the owner is unknown (a bridge file
// with no browser_pid). Every route to one process shares the key, so it
// is held once and dialed once at a time.
func (e Endpoint) key() string {
	if e.PID != 0 {
		return "pid " + strconv.FormatUint(uint64(e.PID), 10)
	}
	return e.Browser + " " + e.WSURL
}

// commonPorts are probed last, when no profile directory names an endpoint
// (ub/cdp.go:232-239).
var commonPorts = []int{9222, 9223, 9224}

// profileDir is a directory that may hold a DevToolsActivePort or a
// winuse-port file, with the browser and route it belongs to.
type profileDir struct {
	path    string
	browser string
	mode    Mode
}

// profileDirs lists the directories to scan, winuse's own profiles first,
// then the browsers' real ones (ub/browsers.go:217-261). With a browser
// named, only its directories count, so another debuggable browser can
// never be picked up instead.
func (s *System) profileDirs(want string) []profileDir {
	real := s.Env.UserDataDirs()
	names := Order
	if want != "" {
		names = []string{want}
	}
	var dirs []profileDir
	for _, n := range names {
		dirs = append(dirs,
			profileDir{s.LaunchProfileDir(n), n, ModeLaunch},
			profileDir{s.CloneProfileDir(n), n, ModeClone})
	}
	for _, n := range names {
		if d := real[n]; d != "" {
			dirs = append(dirs, profileDir{d, n, ModeToggle})
		}
	}
	return dirs
}

// discover resolves the endpoint for want (a short name, or "" for
// whichever browser is debuggable), in use-browser's order
// (ub/cdp.go:154-241), minus what a helper-held connection no longer needs
// (the per-session state port and the remote-endpoint variables):
//
//  1. the extension bridge (ticket 02; System.Extension);
//  2. winuse's own launch and clone profiles, then the browsers' real
//     user-data-dirs (the inspect toggle), by winuse-port and
//     DevToolsActivePort;
//  3. ports 9222 to 9224.
//
// Every candidate is verified (verify) before it counts. With nothing
// named, two different debuggable browsers are an Ambiguous error that
// names both, never a guess.
func (s *System) discover(ctx context.Context, want string) (Endpoint, error) {
	var found []Endpoint
	var rejected []*Problem
	if s.Extension != nil {
		ep, err := s.Extension(ctx, want)
		if p, ok := fallsThrough(err); ok {
			// The user turned the extension off, or it is stuck: winuse
			// may still use a browser it launched or cloned, and says why
			// when there is none.
			rejected = append(rejected, p)
		} else if err != nil {
			return Endpoint{}, err
		} else if ep != nil {
			return *ep, nil
		}
	}
	for _, d := range s.profileDirs(want) {
		ep, prob := s.probeDir(ctx, d)
		if prob != nil {
			rejected = append(rejected, prob)
			continue
		}
		if ep == nil {
			continue
		}
		if want != "" {
			return *ep, nil
		}
		found = append(found, *ep)
	}
	if len(found) == 0 {
		for _, port := range commonPorts {
			ep, prob := s.probePort(ctx, port, want)
			if prob != nil {
				rejected = append(rejected, prob)
				continue
			}
			if ep != nil {
				found = append(found, *ep)
			}
		}
	}
	if len(found) > 0 {
		return choose(found)
	}
	return Endpoint{}, s.nothingFound(want, rejected)
}

// discoverFor resolves the endpoint of one browser process: the one that
// owns a Window. The user's Chrome (through the extension) and a Chrome
// winuse launched are two processes with two endpoints, and a Window is
// read and driven through its own. The routes are discover's, each kept
// only when its endpoint belongs to pid:
//
//  1. the extension bridge whose browser_pid is pid (the user's choice of
//     route, as in discover: it wins over a debugging port of the same
//     process);
//  2. winuse's launch and clone profiles and the real user-data-dir,
//     whose verified port owner is pid;
//  3. ports 9222 to 9224 owned by pid, when no profile names one.
//
// The pin plays no part: a Window belongs to exactly one process. Two
// endpoints of that process are an error naming both, never a guess, and
// none is NoWindowEndpoint, naming the processes winuse can reach instead.
func (s *System) discoverFor(ctx context.Context, want string, pid uint32) (Endpoint, error) {
	ext, err := s.extensionEndpointFor(ctx, want, pid)
	extProb, fell := fallsThrough(err)
	switch {
	case err != nil && !fell:
		return Endpoint{}, err
	case ext != nil:
		return *ext, nil
	}
	others := map[uint32]bool{}
	var mine []Endpoint
	keep := func(ep *Endpoint) {
		if ep.PID != pid {
			others[ep.PID] = true
			return
		}
		for _, m := range mine {
			if m.Port == ep.Port {
				return // one port found through two directories
			}
		}
		mine = append(mine, *ep)
	}
	for _, d := range s.profileDirs(want) {
		if ep, _ := s.probeDir(ctx, d); ep != nil {
			keep(ep)
		}
	}
	if len(mine) == 0 {
		for _, port := range commonPorts {
			if ep, _ := s.probePort(ctx, port, want); ep != nil {
				keep(ep)
			}
		}
	}
	switch {
	case len(mine) == 1:
		return mine[0], nil
	case len(mine) > 1:
		var names []string
		for _, m := range mine {
			names = append(names, fmt.Sprintf("port %d (%s)", m.Port, m.Mode.Describe()))
		}
		return Endpoint{}, &Problem{Kind: SeveralEndpoints, Browser: want, PID: int(pid), Others: names}
	case extProb != nil:
		return Endpoint{}, extProb
	}
	for _, b := range s.liveBridges(want) {
		if p := uint32(b.info.BrowserPID); p != 0 && p != pid {
			others[p] = true
		}
	}
	pids := slices.Sorted(maps.Keys(others))
	var reach []string
	for _, p := range pids {
		reach = append(reach, strconv.FormatUint(uint64(p), 10))
	}
	return Endpoint{}, &Problem{Kind: NoWindowEndpoint, Browser: want, PID: int(pid), Others: reach}
}

// choose picks the one endpoint among several, or refuses to guess
// between different browsers (ub/cdp.go:268-277). Several endpoints of one
// browser resolve to the first, the most specific route.
func choose(found []Endpoint) (Endpoint, error) {
	if names := distinctBrowsers(found); len(names) > 1 {
		return Endpoint{}, &Problem{Kind: Ambiguous, Others: names}
	}
	return found[0], nil
}

// distinctBrowsers lists the browsers behind endpoints, in Order.
func distinctBrowsers(found []Endpoint) []string {
	seen := map[string]bool{}
	for _, f := range found {
		seen[f.Browser] = true
	}
	var names []string
	for _, n := range Order {
		if seen[n] {
			names = append(names, n)
		}
	}
	return names
}

// probeDir finds a live endpoint recorded in one directory: winuse's port
// file first, then DevToolsActivePort. Its WebSocket-path fallback (the
// inspect toggle disables /json/version) only counts while the port is
// alive; a file left by a closed browser must not fake a connection.
func (s *System) probeDir(ctx context.Context, d profileDir) (*Endpoint, *Problem) {
	port, ws := 0, ""
	if p := readPortFile(d.path); p != 0 {
		if u, err := s.VersionWS(ctx, p); err == nil {
			port, ws = p, u
		}
	}
	if ws == "" {
		p, wsPath := activePort(d.path)
		if p == 0 {
			return nil, nil
		}
		if u, err := s.VersionWS(ctx, p); err == nil {
			port, ws = p, u
		} else if wsPath != "" && s.PortAlive(p) {
			port, ws = p, "ws://127.0.0.1:"+strconv.Itoa(p)+wsPath
		}
	}
	if ws == "" {
		return nil, nil
	}
	return s.verify(Endpoint{Browser: d.browser, Mode: d.mode, WSURL: ws, Port: port, Dir: d.path})
}

// probePort checks one common port; want narrows it to one browser.
func (s *System) probePort(ctx context.Context, port int, want string) (*Endpoint, *Problem) {
	if !s.PortAlive(port) {
		return nil, nil
	}
	ws, err := s.VersionWS(ctx, port)
	if err != nil {
		return nil, nil
	}
	return s.verify(Endpoint{Browser: want, Mode: ModePort, WSURL: ws, Port: port})
}

// verify checks an endpoint before winuse trusts it (agent-desktop's
// checks, agent-desktop-research.md section 5): the WebSocket URL is
// loopback and on the probed port, and the port's listener is the expected
// browser's process. An endpoint found without a browser (a port probe
// with nothing named) takes the owner's browser, and a process that is
// not a known browser is refused.
func (s *System) verify(ep Endpoint) (*Endpoint, *Problem) {
	if err := checkLoopbackWS(ep.WSURL, ep.Port); err != nil {
		return nil, &Problem{Kind: NotLoopback, Browser: ep.Browser, URL: ep.WSURL, Port: ep.Port}
	}
	pid, ok := s.ListenerPID(ep.Port)
	if !ok {
		return nil, &Problem{Kind: UnknownOwner, Browser: ep.Browser, Port: ep.Port}
	}
	image, err := s.ProcessImage(pid)
	if err != nil || image == "" {
		return nil, &Problem{Kind: UnknownOwner, Browser: ep.Browser, Port: ep.Port}
	}
	owner := s.Env.IdentifyImage(image)
	if owner == "" || (ep.Browser != "" && owner != ep.Browser) {
		return nil, &Problem{Kind: WrongOwner, Browser: ep.Browser, Owner: filepath.Base(image), Port: ep.Port}
	}
	ep.Browser = owner
	ep.PID = pid
	return &ep, nil
}

// checkLoopbackWS accepts only a ws:// URL on a loopback host and on the
// port that was verified: a /json/version answer pointing anywhere else is
// not the endpoint whose owner winuse checked.
func checkLoopbackWS(raw string, port int) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme != "ws" {
		return fmt.Errorf("scheme %q is not ws", u.Scheme)
	}
	host := u.Hostname()
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("host %q is not loopback", host)
		}
	}
	if u.Port() != strconv.Itoa(port) {
		return fmt.Errorf("port %q is not the verified port %d", u.Port(), port)
	}
	if !strings.HasPrefix(u.Path, "/devtools/") {
		return fmt.Errorf("path %q is not a DevTools path", u.Path)
	}
	return nil
}

// nothingFound explains an empty discovery with the most specific cause:
// nothing installed, a rejected endpoint, or the target browser running
// without debugging or not running at all.
func (s *System) nothingFound(want string, rejected []*Problem) error {
	installed := s.detect()
	if len(installed) == 0 {
		return &Problem{Kind: NoBrowser}
	}
	if len(rejected) > 0 {
		return rejected[0]
	}
	target := installed[0]
	if want != "" {
		for _, b := range installed {
			if b.Name == want {
				target = b
			}
		}
	} else {
		for _, b := range installed {
			if s.running(b) {
				target = b
				break
			}
		}
	}
	if s.running(target) {
		return &Problem{Kind: NoDebugging, Browser: target.Name}
	}
	return &Problem{Kind: NotRunning, Browser: target.Name}
}
