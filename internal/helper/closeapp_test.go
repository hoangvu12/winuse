package helper

// close-app's offline tests (desktop-parity ticket 10) over faked Windows
// and processes: every Window of the App closed and its processes waited
// for, an App still running after the wait, --force ending it, and the
// refusals (locked desktop, administrator Window, protected processes,
// --force on a process winuse cannot name or that hosts other Apps), each
// with nothing sent. Everything Win32 is faked; no desktop is touched and
// no process is opened or ended.

import (
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// closeAppSpy fakes an App's Windows and processes. A posted close shuts
// the Window and its process, unless keepRunning (the App asks to save).
type closeAppSpy struct {
	mu    sync.Mutex
	steps []string

	windows     []win.WindowInfo
	open        map[uint64]bool
	running     map[uint32]bool
	keepRunning bool
	locked      bool
	admin       uint64 // the Window the integrity check refuses; 0: none
	closeErr    error  // the posted close fails
	termErr     error  // TerminateProcess fails
	gone        map[uint32]bool
	terminate   map[uint32]bool // the terminate right each process was opened with
	handles     int             // process handles open
	sleeps      []time.Duration

	esc *scopeEsc
	svc Services
}

// newCloseAppSpy fakes the App "Fixture" with Windows 101 and 102 in
// process 7 and Window 201 in process 8 (a second instance), and the
// user's Notepad, Window 500 in process 9, which close-app must not touch.
func newCloseAppSpy() *closeAppSpy {
	fixture := `C:\winuse\fixture.exe`
	s := &closeAppSpy{
		windows: []win.WindowInfo{
			{ID: 101, App: "Fixture", ExePath: fixture, PID: 7, Title: "One"},
			{ID: 500, App: "Notepad", ExePath: `C:\Windows\notepad.exe`, PID: 9, Title: "Untitled - Notepad"},
			{ID: 102, App: "Fixture", ExePath: fixture, PID: 7, Title: "Two"},
			{ID: 201, App: "Fixture", ExePath: fixture, PID: 8, Title: "Three"},
		},
		open:      map[uint64]bool{101: true, 102: true, 201: true, 500: true},
		running:   map[uint32]bool{7: true, 8: true, 9: true},
		gone:      map[uint32]bool{},
		terminate: map[uint32]bool{},
		esc:       &scopeEsc{},
	}
	svc := testServices(&fakeTrees{})
	svc.Windows = func() ([]win.WindowInfo, error) { return s.windows, nil }
	svc.Locked = func() (bool, string) { s.step("locked"); return s.locked, "Winlogon" }
	svc.Integrity = func(w win.WindowInfo) error {
		if w.ID == s.admin {
			return &win.AdminWindowError{WindowID: w.ID, App: "fixture.exe"}
		}
		return nil
	}
	svc.CloseWindow = func(hwnd uint64) error {
		s.step("close " + itoa(hwnd))
		s.esc.pressIf("close")
		if s.closeErr != nil {
			return s.closeErr
		}
		if !s.keepRunning {
			s.mu.Lock()
			s.open[hwnd] = false
			for _, w := range s.windows {
				if w.ID == hwnd {
					s.running[w.PID] = false
				}
			}
			s.mu.Unlock()
		}
		return nil
	}
	svc.WindowOpen = func(hwnd uint64) bool { s.mu.Lock(); defer s.mu.Unlock(); return s.open[hwnd] }
	svc.OpenProcess = func(pid uint32, terminate bool) (AppProcess, error) {
		s.step("open " + itoa(uint64(pid)))
		if s.gone[pid] {
			return nil, win.ErrProcessGone
		}
		s.mu.Lock()
		s.terminate[pid] = terminate
		s.handles++
		s.mu.Unlock()
		return &fakeAppProcess{spy: s, pid: pid}, nil
	}
	svc.Sleep = func(d time.Duration) { s.sleeps = append(s.sleeps, d) }
	svc.Esc = s.esc
	s.svc = svc
	return s
}

func (s *closeAppSpy) step(name string) {
	s.mu.Lock()
	s.steps = append(s.steps, name)
	s.mu.Unlock()
}

func (s *closeAppSpy) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.steps...)
}

// waited sums the poll's sleeps.
func (s *closeAppSpy) waited() time.Duration {
	var d time.Duration
	for _, x := range s.sleeps {
		d += x
	}
	return d
}

// assertNothingSent checks a refusal: no close posted, no process opened.
func (s *closeAppSpy) assertNothingSent(t *testing.T) {
	t.Helper()
	for _, st := range s.recorded() {
		if strings.HasPrefix(st, "close ") || strings.HasPrefix(st, "open ") || strings.HasPrefix(st, "terminate ") {
			t.Errorf("a refused close-app reached %q: %v", st, s.recorded())
		}
	}
	if b := s.esc.brackets(); len(b) != 0 {
		t.Errorf("a refusal must arm nothing: %v", b)
	}
}

// fakeAppProcess is one faked process of the App.
type fakeAppProcess struct {
	spy *closeAppSpy
	pid uint32
}

func (p *fakeAppProcess) Exited() bool {
	p.spy.mu.Lock()
	defer p.spy.mu.Unlock()
	return !p.spy.running[p.pid]
}

func (p *fakeAppProcess) Terminate() error {
	p.spy.step("terminate " + itoa(uint64(p.pid)))
	if p.spy.termErr != nil {
		return p.spy.termErr
	}
	p.spy.mu.Lock()
	p.spy.running[p.pid] = false
	p.spy.mu.Unlock()
	return nil
}

func (p *fakeAppProcess) Close() error {
	p.spy.mu.Lock()
	p.spy.handles--
	p.spy.mu.Unlock()
	return nil
}

func itoa(n uint64) string { return strconv.FormatUint(n, 10) }

func TestCloseAppClosesEveryWindowAndWaitsForTheExit(t *testing.T) {
	spy := newCloseAppSpy()
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "close-app", protocol.CloseAppArgs{App: "fixture"})
	if cmdErr != nil || out != "ok\n" {
		t.Fatalf("close-app = %q, %v; want ok", out, cmdErr)
	}
	want := []string{"locked", "open 7", "open 8", "close 101", "close 102", "close 201"}
	if got := spy.recorded(); !equalStrings(got, want) {
		t.Errorf("steps = %v, want %v (the refusal checks, the App's processes, a close per Window)", got, want)
	}
	if !spy.open[500] || !spy.running[9] {
		t.Error("close-app touched another App's Window")
	}
	if spy.terminate[7] || spy.terminate[8] {
		t.Error("without --force, the processes are opened without the terminate right")
	}
	if spy.handles != 0 {
		t.Errorf("%d process handles left open", spy.handles)
	}
	if b := spy.esc.brackets(); !equalStrings(b, []string{"arm-passive", "disarm-passive"}) {
		t.Errorf("Esc scopes = %v, want one passive scope", b)
	}
}

func TestCloseAppStillRunningIsOKWithANote(t *testing.T) {
	spy := newCloseAppSpy()
	spy.keepRunning = true // the App asks to save
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "close-app", protocol.CloseAppArgs{App: "Fixture"})
	if cmdErr != nil {
		t.Fatalf("close-app: %v", cmdErr.Error())
	}
	want := "ok (the close was requested; Fixture is still running, it may be asking to save: read winuse list-windows --app Fixture or the Window State next)\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	if w := spy.waited(); w < closeAppWait || w > closeAppWait+100*time.Millisecond {
		t.Errorf("waited %v for the App to exit, want %v", w, closeAppWait)
	}
	for _, st := range spy.recorded() {
		if strings.HasPrefix(st, "terminate ") {
			t.Errorf("close-app without --force ended a process: %v", spy.recorded())
		}
	}
}

func TestCloseAppQuotesAnAppNameWithASpace(t *testing.T) {
	spy := newCloseAppSpy()
	spy.keepRunning = true
	for i := range spy.windows {
		if spy.windows[i].App == "Fixture" {
			spy.windows[i].App = "Fixture App"
		}
	}
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "close-app", protocol.CloseAppArgs{App: "fixture app"})
	if cmdErr != nil || !strings.Contains(out, `Fixture App is still running`) || !strings.Contains(out, `list-windows --app "fixture app"`) {
		t.Errorf("close-app = %q, %v; want the App's name and a quoted --app", out, cmdErr)
	}
}

func TestCloseAppForceEndsTheProcessesStillRunning(t *testing.T) {
	spy := newCloseAppSpy()
	spy.keepRunning = true
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "close-app", protocol.CloseAppArgs{App: "fixture", Force: true})
	if cmdErr != nil {
		t.Fatalf("close-app --force: %v", cmdErr.Error())
	}
	if want := "ok (Fixture was still running after the close; --force ended 2 processes)\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
	want := []string{"locked", "open 7", "open 8", "close 101", "close 102", "close 201", "terminate 7", "terminate 8"}
	if got := spy.recorded(); !equalStrings(got, want) {
		t.Errorf("steps = %v, want %v (the posted close first, then the terminations)", got, want)
	}
	if !spy.terminate[7] || !spy.terminate[8] {
		t.Error("--force opens the processes with the terminate right")
	}
	if spy.running[9] != true {
		t.Error("--force ended another App's process")
	}
	if spy.handles != 0 {
		t.Errorf("%d process handles left open", spy.handles)
	}
}

func TestCloseAppForceEndsNothingWhenTheAppExits(t *testing.T) {
	spy := newCloseAppSpy()
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "close-app", protocol.CloseAppArgs{App: "fixture", Force: true})
	if cmdErr != nil || out != "ok\n" {
		t.Fatalf("close-app --force = %q, %v; want ok", out, cmdErr)
	}
	for _, st := range spy.recorded() {
		if strings.HasPrefix(st, "terminate ") {
			t.Errorf("an App that exited on the close must not be ended: %v", spy.recorded())
		}
	}
}

func TestCloseAppForceTerminateFailure(t *testing.T) {
	spy := newCloseAppSpy()
	spy.keepRunning = true
	spy.termErr = errors.New("TerminateProcess failed for process 7: Access is denied.")
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "close-app", protocol.CloseAppArgs{App: "fixture", Force: true})
	if cmdErr == nil || cmdErr.Msg != "TerminateProcess failed for process 7: Access is denied." {
		t.Fatalf("message = %v, want the terminate failure", cmdErr)
	}
	if strings.Contains(cmdErr.Msg, "nothing was sent") || !strings.Contains(cmdErr.Hint, "close was delivered") {
		t.Errorf("the close was delivered: %q / %q", cmdErr.Msg, cmdErr.Hint)
	}
}

func TestCloseAppEscStopEndsNothing(t *testing.T) {
	spy := newCloseAppSpy()
	spy.keepRunning = true
	spy.esc.pressOn = "close"
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "close-app", protocol.CloseAppArgs{App: "fixture", Force: true})
	if cmdErr == nil || cmdErr.Exit != protocol.ExitEsc {
		t.Fatalf("close-app --force after Esc = %v, want the Esc stop", cmdErr)
	}
	for _, st := range spy.recorded() {
		if strings.HasPrefix(st, "terminate ") {
			t.Errorf("the user stopped winuse, so --force must end nothing: %v", spy.recorded())
		}
	}
}

func TestCloseAppSkipsAProcessThatAlreadyExited(t *testing.T) {
	spy := newCloseAppSpy()
	spy.gone[8] = true
	spy.running[8] = false
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "close-app", protocol.CloseAppArgs{App: "fixture"})
	if cmdErr != nil || out != "ok\n" {
		t.Fatalf("close-app = %q, %v; want ok", out, cmdErr)
	}
}

func TestCloseAppCloseFailureSaysNothingSent(t *testing.T) {
	spy := newCloseAppSpy()
	spy.closeErr = errors.New("PostMessage(WM_CLOSE) failed: access denied")
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "close-app", protocol.CloseAppArgs{App: "fixture"})
	if cmdErr == nil || cmdErr.Msg != "PostMessage(WM_CLOSE) failed: access denied; nothing was sent" {
		t.Fatalf("message = %v, want the call failure with nothing sent", cmdErr)
	}
}

func TestCloseAppRefusals(t *testing.T) {
	cases := []struct {
		name    string
		args    protocol.CloseAppArgs
		setup   func(*closeAppSpy)
		wantMsg string
		hint    string
	}{
		{"an App with no Windows", protocol.CloseAppArgs{App: "paint"}, nil,
			`app "paint" has no open Windows; nothing was sent`, "list-windows"},
		{"a locked desktop", protocol.CloseAppArgs{App: "fixture"}, func(s *closeAppSpy) { s.locked = true },
			"desktop is locked", ""},
		{"an administrator Window", protocol.CloseAppArgs{App: "fixture"}, func(s *closeAppSpy) { s.admin = 102 },
			"administrator", "elevated"},
		{"an administrator Window under --force", protocol.CloseAppArgs{App: "fixture", Force: true}, func(s *closeAppSpy) { s.admin = 201 },
			"administrator", "elevated"},
		{"a protected process", protocol.CloseAppArgs{App: "explorer"}, protectedExplorer,
			"close-app refuses explorer.exe: it is a protected Windows process; nothing was sent", "winuse close-window 301"},
		{"a protected process under --force", protocol.CloseAppArgs{App: "explorer", Force: true}, protectedExplorer,
			"close-app refuses explorer.exe: it is a protected Windows process; nothing was sent", "winuse close-window 301"},
		{"--force on a process winuse cannot name", protocol.CloseAppArgs{App: "Mystery", Force: true}, func(s *closeAppSpy) {
			s.windows = append(s.windows, win.WindowInfo{ID: 401, App: "Mystery", PID: 12})
		}, "close-app --force cannot read which program owns window 401, so it ends nothing; nothing was sent", "without --force"},
		{"--force on the packaged Apps' host", protocol.CloseAppArgs{App: "ApplicationFrameHost", Force: true}, func(s *closeAppSpy) {
			s.windows = append(s.windows, win.WindowInfo{ID: 402, App: "Application Frame Host",
				ExePath: `C:\Windows\System32\ApplicationFrameHost.exe`, PID: 13})
		}, "close-app --force refuses ApplicationFrameHost.exe", "without --force"},
		{"no App", protocol.CloseAppArgs{App: " "}, nil, "close-app: an App name is required", "usage: winuse close-app <app> [--force]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := newCloseAppSpy()
			if tc.setup != nil {
				tc.setup(spy)
			}
			_, cmdErr := runCommand(t, CommandsWith(spy.svc), "close-app", tc.args)
			if cmdErr == nil || !strings.Contains(cmdErr.Msg, tc.wantMsg) {
				t.Fatalf("message = %v, want it to contain %q", cmdErr, tc.wantMsg)
			}
			if tc.name != "no App" && !strings.HasSuffix(cmdErr.Msg, "; nothing was sent") {
				t.Errorf("message = %q, want nothing was sent", cmdErr.Msg)
			}
			if !strings.Contains(cmdErr.Hint, tc.hint) {
				t.Errorf("hint = %q, want it to contain %q", cmdErr.Hint, tc.hint)
			}
			spy.assertNothingSent(t)
		})
	}
}

// protectedExplorer adds a File Explorer Window, which belongs to the
// protected explorer.exe.
func protectedExplorer(s *closeAppSpy) {
	s.windows = append(s.windows, win.WindowInfo{ID: 301, App: "Windows Explorer",
		ExePath: `C:\Windows\explorer.exe`, PID: 4, Title: "Downloads"})
}

func TestCloseAppIsSerializedAndNoBatchStep(t *testing.T) {
	cmds := CommandsWith(Services{Trees: &fakeTrees{}})
	if _, ok := cmds["close-app"]; !ok {
		t.Fatal("close-app is not registered")
	}
	if observationOnlyCommand("close-app") {
		t.Error("close-app must run on the action gate")
	}
	for _, k := range protocol.BatchStepKinds {
		if k == "close-app" {
			t.Error("close-app must not be a Batch step kind")
		}
	}
	if headlessStep("close-app", nil, nil) || takesHeaded("close-app") {
		t.Error("close-app has no headed form and no Element routing")
	}
}
