package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/bridge"
	"github.com/hoangvu12/winuse/internal/cdp"
)

// System is everything the package does to the machine: the folders, the
// process table, the DevTools endpoints, process starts and dials. Live
// fills the real ones (system_windows.go); tests fake every field, so an
// offline test never probes a real port, starts a browser or dials one.
type System struct {
	Env      Env
	StateDir string // %LOCALAPPDATA%\winuse\browser: the pin, the launch and clone profiles

	Exists func(path string) bool

	// VersionWS asks http://127.0.0.1:<port>/json/version for the
	// browser-level WebSocket URL; an error when nothing answers or the
	// endpoint is disabled (the inspect toggle answers 404).
	VersionWS func(ctx context.Context, port int) (string, error)
	// PortAlive reports whether something listens on a loopback port.
	PortAlive func(port int) bool
	// ListenerPID is the process listening on a loopback TCP port.
	ListenerPID func(port int) (pid uint32, ok bool)
	// ProcessImage is a process's full image path.
	ProcessImage func(pid uint32) (string, error)
	// Running reports whether a process with this image name runs.
	Running func(exe string) bool

	// Start starts a browser process detached from the helper (it must
	// outlive the helper's idle exit) and returns its pid.
	Start func(path string, args []string) (pid int, err error)
	// Foreground is the foreground Window and the process that owns it:
	// browser launch compares it before and after the start. Nil skips the
	// check.
	Foreground func() (window uint64, pid uint32)
	// CloseApp asks every process with this image name to close, the
	// clean way (taskkill without /F).
	CloseApp func(exe string) error
	// FreePort is the first free loopback port from 9222 up.
	FreePort func() int
	// AppPort is a free ephemeral loopback port, for an App launched with
	// --cdp (apps.go): never one of the ports browser discovery probes.
	AppPort func() (int, error)
	// ParentPID is a process's parent: AwaitApp checks that an App's
	// port belongs to the process tree winuse launched.
	ParentPID func(pid uint32) (uint32, bool)

	// Dial opens the WebSocket to a verified endpoint; handshake bounds the
	// opening handshake (the Chrome 144+ approval prompt holds it).
	Dial func(ctx context.Context, wsURL string, handshake time.Duration) (cdp.Transport, error)

	// Extension finds the extension bridge connected to the pinned browser
	// (any browser when pin is ""), or nil. Live sets it to
	// extensionEndpoint; discovery tries it before every port. Nil means
	// no bridge.
	Extension func(ctx context.Context, pin string) (*Endpoint, error)

	// The winuse extension (extension.go, ticket 02).
	//
	// Bridges lists the running native hosts' bridge files, stale ones
	// removed.
	Bridges func() []bridge.Info
	// OpenBridge connects to a native host with its token.
	OpenBridge func(ctx context.Context, b bridge.Info) (cdp.Transport, error)
	// Registry holds the native host registrations.
	Registry Registry
	// Executable is the winuse binary the host manifest names.
	Executable func() (string, error)

	Now   func() time.Time
	Sleep func(d time.Duration)
}

// ---- winuse's own directories and the pin ----

// LaunchProfileDir is the dedicated profile `browser launch` uses.
func (s *System) LaunchProfileDir(name string) string {
	return filepath.Join(s.StateDir, "profile-"+name)
}

// CloneProfileDir is the copy of the user's real profile `browser clone`
// keeps in sync.
func (s *System) CloneProfileDir(name string) string {
	return filepath.Join(s.StateDir, "profile-"+name+"-clone")
}

// portFileName records the port winuse launched a profile on: Chrome and
// Brave often never write DevToolsActivePort (ub/browsers.go:142-159).
const portFileName = "winuse-port"

func writePortFile(dir string, port int) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, portFileName), []byte(strconv.Itoa(port)), 0o644)
}

func readPortFile(dir string) int {
	b, err := os.ReadFile(filepath.Join(dir, portFileName))
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || n <= 0 || n > 65535 {
		return 0
	}
	return n
}

// activePort reads a DevToolsActivePort file: line 1 is the port, line 2
// the browser WebSocket path (ub/cdp.go:78-92). The inspect toggle disables
// /json/*, so this file is the only record of its endpoint.
func activePort(profileDir string) (port int, wsPath string) {
	b, err := os.ReadFile(filepath.Join(profileDir, "DevToolsActivePort"))
	if err != nil {
		return 0, ""
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	n, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || n <= 0 || n > 65535 {
		return 0, ""
	}
	if len(lines) >= 2 {
		wsPath = strings.TrimSpace(lines[1])
	}
	return n, wsPath
}

// pinState is the pin file: the browser every command uses when none is
// named. Empty means none.
type pinState struct {
	Browser string `json:"browser,omitempty"`
}

func (s *System) pinFile() string { return filepath.Join(s.StateDir, "state.json") }

// Pin is the pinned browser, or "". An unreadable file counts as no pin.
func (s *System) Pin() string {
	b, err := os.ReadFile(s.pinFile())
	if err != nil {
		return ""
	}
	// PowerShell's Set-Content and > write a UTF-8 BOM (ub/cdp.go:731-734).
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	var st pinState
	if json.Unmarshal(b, &st) != nil {
		return ""
	}
	name := strings.ToLower(strings.TrimSpace(st.Browser))
	if !Known(name) {
		return ""
	}
	return name
}

// SetPin records the pinned browser; "" removes the pin.
func (s *System) SetPin(name string) error {
	if err := os.MkdirAll(s.StateDir, 0o755); err != nil {
		return err
	}
	b, _ := json.Marshal(pinState{Browser: name})
	return os.WriteFile(s.pinFile(), b, 0o644)
}

// detect lists the installed browsers.
func (s *System) detect() []Browser { return s.Env.Detect(s.Exists) }

// running reports whether a browser's process runs.
func (s *System) running(b Browser) bool { return s.Running != nil && s.Running(b.Exe()) }

// poll checks cond until it holds or d passes (ctx's end stops it early),
// pausing between checks with the System's clock.
func (s *System) poll(ctx context.Context, d time.Duration, every time.Duration, cond func() bool) bool {
	deadline := s.Now().Add(d)
	for {
		if cond() {
			return true
		}
		if ctx.Err() != nil || !s.Now().Before(deadline) {
			return false
		}
		s.Sleep(every)
	}
}
