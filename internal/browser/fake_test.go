package browser

// The fake machine every offline test runs on: browsers "installed" as
// empty files under t.TempDir, a fake listener table and process table,
// and DevTools endpoints that are either made-up URLs (discovery only) or
// cdptest servers on ephemeral loopback ports (anything that dials). No
// real browser, port or process is touched.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp"
	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
)

type fakePort struct {
	ws      string // what /json/version answers; "" when it answers 404 (the inspect toggle)
	pid     uint32 // the listener's owner; 0 when it cannot be read
	refuses bool   // nothing listens (a stale DevToolsActivePort)
}

type startCall struct {
	path string
	args []string
}

type fakeMachine struct {
	t   *testing.T
	sys *System

	mu      sync.Mutex
	ports   map[int]fakePort
	images  map[uint32]string
	running map[string]bool
	starts  []startCall
	closes  []string
	dials   int
	now     time.Time
	free    int

	onStart func(call startCall) (pid int)
	onClose func(exe string)
}

func newFake(t *testing.T) *fakeMachine {
	t.Helper()
	root := t.TempDir()
	f := &fakeMachine{
		t:       t,
		ports:   map[int]fakePort{},
		images:  map[uint32]string{},
		running: map[string]bool{},
		now:     time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC),
		free:    9301,
	}
	env := Env{
		LocalAppData:    filepath.Join(root, "Local"),
		AppData:         filepath.Join(root, "Roaming"),
		ProgramFiles:    filepath.Join(root, "Program Files"),
		ProgramFilesX86: filepath.Join(root, "Program Files (x86)"),
	}
	f.sys = &System{
		Env:      env,
		StateDir: filepath.Join(env.LocalAppData, "winuse", "browser"),
		Exists:   fileExists,
		VersionWS: func(ctx context.Context, port int) (string, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			p, ok := f.ports[port]
			if !ok || p.refuses || p.ws == "" {
				return "", errors.New("no /json/version")
			}
			return p.ws, nil
		},
		PortAlive: func(port int) bool {
			f.mu.Lock()
			defer f.mu.Unlock()
			p, ok := f.ports[port]
			return ok && !p.refuses
		},
		ListenerPID: func(port int) (uint32, bool) {
			f.mu.Lock()
			defer f.mu.Unlock()
			p, ok := f.ports[port]
			return p.pid, ok && !p.refuses && p.pid != 0
		},
		ProcessImage: func(pid uint32) (string, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if img, ok := f.images[pid]; ok {
				return img, nil
			}
			return "", errors.New("access denied")
		},
		Running: func(exe string) bool {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.running[exe]
		},
		Start: func(path string, args []string) (int, error) {
			call := startCall{path, args}
			f.mu.Lock()
			f.starts = append(f.starts, call)
			on := f.onStart
			f.mu.Unlock()
			if on != nil {
				return on(call), nil
			}
			return 4242, nil
		},
		CloseApp: func(exe string) error {
			f.mu.Lock()
			f.closes = append(f.closes, exe)
			on := f.onClose
			f.mu.Unlock()
			if on != nil {
				on(exe)
			}
			return nil
		},
		FreePort: func() int {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.free
		},
		Dial: func(ctx context.Context, wsURL string, handshake time.Duration) (cdp.Transport, error) {
			f.mu.Lock()
			f.dials++
			f.mu.Unlock()
			return cdp.DialWS(ctx, wsURL, handshake)
		},
		Now: func() time.Time {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.now
		},
		Sleep: func(d time.Duration) {
			f.mu.Lock()
			f.now = f.now.Add(d)
			f.mu.Unlock()
			time.Sleep(time.Millisecond) // let background goroutines run
		},
	}
	return f
}

// install puts a browser's executable at its first candidate path and
// returns its path.
func (f *fakeMachine) install(name string) string {
	f.t.Helper()
	p := f.sys.Env.CandidatePaths()[name][0]
	writeFile(f.t, p, "exe")
	return p
}

// process registers a running browser process with a pid.
func (f *fakeMachine) process(name string, pid uint32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.images[pid] = f.sys.Env.CandidatePaths()[name][0]
	f.running[exeNames[name]] = true
}

// listen makes a port answer as a DevTools endpoint owned by pid.
func (f *fakeMachine) listen(port int, ws string, pid uint32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ports[port] = fakePort{ws: ws, pid: pid}
}

// unlisten makes a port refuse connections (the browser exited).
func (f *fakeMachine) unlisten(port int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.ports, port)
}

// serve starts a cdptest server and lists it as the endpoint on its own
// port, owned by pid.
func (f *fakeMachine) serve(pid uint32) *cdptest.Server {
	f.t.Helper()
	s := cdptest.NewServer()
	f.t.Cleanup(s.Close)
	f.listen(s.Port(), s.URL(), pid)
	return s
}

// ownProfile records a port file in winuse's launch or clone profile.
func (f *fakeMachine) ownProfile(name string, mode Mode, port int) string {
	f.t.Helper()
	dir := f.sys.LaunchProfileDir(name)
	if mode == ModeClone {
		dir = f.sys.CloneProfileDir(name)
	}
	if err := writePortFile(dir, port); err != nil {
		f.t.Fatal(err)
	}
	return dir
}

// toggle writes a DevToolsActivePort into a browser's real user-data-dir.
func (f *fakeMachine) toggle(name string, port int, wsPath string) {
	f.t.Helper()
	writeFile(f.t, filepath.Join(f.sys.Env.UserDataDirs()[name], "DevToolsActivePort"), strconv.Itoa(port)+"\n"+wsPath)
}

func (f *fakeMachine) startCalls() []startCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]startCall(nil), f.starts...)
}

func (f *fakeMachine) dialCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dials
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// problemOf asserts an error is a *Problem of a kind.
func problemOf(t *testing.T, err error, kind Kind) *Problem {
	t.Helper()
	var p *Problem
	if !errors.As(err, &p) {
		t.Fatalf("err = %v (%T), want a *Problem of kind %d", err, err, kind)
	}
	if p.Kind != kind {
		t.Fatalf("problem kind = %d (%q), want %d", p.Kind, p.Sentence(), kind)
	}
	return p
}
