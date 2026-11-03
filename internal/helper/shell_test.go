package helper

// Offline tests for the shell surfaces (desktop-parity 09, shell.go):
// `list-windows --shell`, shell Window IDs resolving, the refusals that
// keep Start and the flyouts closed without --headed, the foreground
// note, the tray overflow's Headless Escape and the opened-Window note for
// Start. The desktop enumeration, the foreground, the Element side and
// the posted Escape are all faked, so no desktop is read or touched.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/wstate"
	"github.com/hoangvu12/winuse/internal/win"
)

const (
	shellTaskbarID  = 900
	shellOverflowID = 950
	shellStartID    = 960
	explorerPID     = 4100
)

// shellDesktop is the unfiltered enumeration of a desktop with the
// taskbar, Notepad and Chrome, a closed (cloaked) Start and, when
// overflowOpen, the open tray overflow; startOpen uncloaks Start.
func shellDesktop(overflowOpen, startOpen bool) []win.WindowInfo {
	b := win.Rect{Left: 0, Top: 1368, Right: 2560, Bottom: 1440}
	ws := []win.WindowInfo{
		{ID: shellTaskbarID, Class: "Shell_TrayWnd", App: "Windows Explorer", PID: explorerPID, Visible: true,
			ExStyle: 0x88, Bounds: b, ShellWindow: true, Shell: win.ShellTaskbar},
	}
	if overflowOpen {
		ws = append(ws, win.WindowInfo{ID: shellOverflowID, Class: "TopLevelWindowForOverflowXamlIsland",
			Title: "System tray overflow window.", App: "Windows Explorer", PID: explorerPID, Visible: true,
			ExStyle: 0x200088, Bounds: win.Rect{Left: 1984, Top: 1257, Right: 2335, Bottom: 1368}, Shell: win.ShellTrayOverflow})
	}
	ws = append(ws, fakeWindows()...)
	for i := range ws {
		if ws[i].Shell == "" {
			ws[i].Visible, ws[i].PID = true, uint32(ws[i].ID)
			if ws[i].Bounds == (win.Rect{}) {
				ws[i].Bounds = win.Rect{Right: 400, Bottom: 300}
			}
		}
	}
	return append(ws, win.WindowInfo{ID: shellStartID, Class: "Windows.UI.Core.CoreWindow", Title: "Start",
		App: "Start", PID: 5200, Visible: true, Cloaked: !startOpen,
		Bounds: win.Rect{Left: 700, Top: 400, Right: 1860, Bottom: 1340}, Shell: win.ShellStart})
}

// shellActor is the Headless spy with the taskbar's stored Elements: the
// root, the hidden-icons chevron, Start and a running App's button.
type shellActor struct {
	*headlessSpy
}

func (a shellActor) StoredElements(ctx context.Context, hwnd uint64) ([]wstate.Element, bool) {
	if hwnd != shellTaskbarID {
		return nil, false
	}
	return []wstate.Element{
		{Role: "pane", Name: "Taskbar"},
		{Depth: 1, Role: "button", Name: "Show Hidden Icons", AutomationId: "SystemTrayIcon"},
		{Depth: 1, Role: "button", Name: "Start", AutomationId: "StartButton"},
		{Depth: 1, Role: "button", Name: "Notepad - 1 running window", AutomationId: "Appid: Microsoft.Windows.Notepad"},
	}, true
}

// shellSpy wires the Headless spy to the shell desktop. The foreground is
// Notepad until fg changes it; surfaces is the enumeration of the moment.
type shellSpy struct {
	*headlessSpy
	mu       sync.Mutex
	fg       uint64
	surfaces func() []win.WindowInfo
	posted   []uint64
	postErr  error
	open     map[uint64]bool
}

func newShellSpy() *shellSpy {
	s := &shellSpy{headlessSpy: newHeadlessSpy(), fg: 101, open: map[uint64]bool{}}
	s.surfaces = func() []win.WindowInfo { return shellDesktop(false, false) }
	svc := &s.svc
	svc.Actions = shellActor{s.headlessSpy}
	svc.Surfaces = func() ([]win.WindowInfo, error) { return s.surfaces(), nil }
	svc.Foreground = func() uint64 { s.mu.Lock(); defer s.mu.Unlock(); return s.fg }
	svc.Sleep = func(time.Duration) {}
	svc.WindowOpen = func(hwnd uint64) bool { s.mu.Lock(); defer s.mu.Unlock(); return s.open[hwnd] }
	svc.ShellKind = func(hwnd uint64) string {
		for _, w := range s.surfaces() {
			if w.ID == hwnd {
				return w.Shell
			}
		}
		return ""
	}
	svc.PostOverflowEscape = func(hwnd uint64) (int, error) {
		s.step("post-escape")
		s.mu.Lock()
		defer s.mu.Unlock()
		s.posted = append(s.posted, hwnd)
		if s.postErr != nil {
			return 0, s.postErr
		}
		return 4, nil
	}
	return s
}

func (s *shellSpy) setForeground(id uint64) {
	s.mu.Lock()
	s.fg = id
	s.mu.Unlock()
}

func hasStep(steps []string, name string) bool {
	for _, st := range steps {
		if st == name {
			return true
		}
	}
	return false
}

func TestListWindowsShellAddsTheShellSurfaces(t *testing.T) {
	s := newShellSpy()
	cmds := CommandsWith(s.svc)
	out, cmdErr := runCommand(t, cmds, "list-windows", protocol.ListWindowsArgs{Shell: true})
	if cmdErr != nil {
		t.Fatalf("list-windows --shell: %v", cmdErr.Error())
	}
	// z-order kept, the untitled taskbar named, the closed Start left out.
	want := "900\tWindows Explorer\tTaskbar\n101\tNotepad\tUntitled - Notepad\n202\tChrome\tOne\n203\tChrome\tTwo\n"
	if out != want {
		t.Errorf("output:\n got %q\nwant %q", out, want)
	}

	plain, cmdErr := runCommand(t, cmds, "list-windows", protocol.ListWindowsArgs{})
	if cmdErr != nil {
		t.Fatalf("list-windows: %v", cmdErr.Error())
	}
	if strings.Contains(plain, "Taskbar") {
		t.Errorf("plain list-windows lists the taskbar:\n%s", plain)
	}

	// The open overflow and Start join the listing.
	s.surfaces = func() []win.WindowInfo { return shellDesktop(true, true) }
	out, _ = runCommand(t, cmds, "list-windows", protocol.ListWindowsArgs{Shell: true})
	for _, line := range []string{"950\tWindows Explorer\tSystem tray overflow window.\n", "960\tStart\tStart\n"} {
		if !strings.Contains(out, line) {
			t.Errorf("list-windows --shell misses %q:\n%s", line, out)
		}
	}
}

func TestListWindowsShellWithApp(t *testing.T) {
	s := newShellSpy()
	out, cmdErr := runCommand(t, CommandsWith(s.svc), "list-windows", protocol.ListWindowsArgs{Shell: true, App: "Windows Explorer"})
	if cmdErr != nil {
		t.Fatalf("list-windows --shell --app: %v", cmdErr.Error())
	}
	if out != "900\tWindows Explorer\tTaskbar\n" {
		t.Errorf("output = %q, want the taskbar alone", out)
	}
}

func TestShellSurfaceResolvesByWindowIDOnly(t *testing.T) {
	cmds := CommandsWith(newShellSpy().svc)
	out, cmdErr := runCommand(t, cmds, "get-window", protocol.GetWindowArgs{Window: "900"})
	if cmdErr != nil {
		t.Fatalf("get-window 900: %v", cmdErr.Error())
	}
	if out != "900\tWindows Explorer\tTaskbar\n" {
		t.Errorf("get-window 900 = %q", out)
	}
	// An App name names a Window of list-windows, never the shell.
	if _, cmdErr := runCommand(t, cmds, "get-window", protocol.GetWindowArgs{Window: "Windows Explorer"}); cmdErr == nil {
		t.Error("get-window by the App name resolved the taskbar")
	}
	// A closed Start does not resolve.
	if _, cmdErr := runCommand(t, cmds, "get-window", protocol.GetWindowArgs{Window: "960"}); cmdErr == nil {
		t.Error("get-window resolved a closed (cloaked) Start")
	}
}

func TestShellKeysNeedHeaded(t *testing.T) {
	s := newShellSpy()
	cmds := CommandsWith(s.svc)
	_, cmdErr := runCommand(t, cmds, "press-key", protocol.PressKeyArgs{Window: "900", Key: "Control_L+Escape"})
	if cmdErr == nil {
		t.Fatal("press-key Control_L+Escape on the taskbar without --headed ran")
	}
	if !strings.HasSuffix(cmdErr.Msg, nothingSentNote) || !strings.Contains(cmdErr.Hint, "winuse --headed press-key 900 Control_L+Escape") {
		t.Errorf("refusal = %q / %q", cmdErr.Msg, cmdErr.Hint)
	}
	if steps := s.recorded(); hasStep(steps, "activate") || hasStep(steps, "press-key") {
		t.Errorf("the refusal sent something: %v", steps)
	}
	// Typing and a point click have no Headless route there either.
	if _, cmdErr := runCommand(t, cmds, "type-text", protocol.TypeTextArgs{Window: "900", Text: "notepad"}); cmdErr == nil ||
		!strings.Contains(cmdErr.Hint, "--headed") {
		t.Errorf("type-text on the taskbar without --headed: %v", cmdErr)
	}
	if _, cmdErr := runCommand(t, cmds, "click", protocol.ClickArgs{Window: "900", Point: &protocol.Point{X: 5, Y: 5}}); cmdErr == nil {
		t.Error("click @x,y on the taskbar without --headed ran")
	}

	// With --headed, Start opens the Headed way: the taskbar is activated
	// and the chord pressed, and the output says the foreground moved.
	s.svc.Activate = func(hwnd uint64) error { s.step("activate"); s.setForeground(hwnd); return nil }
	out, cmdErr := runCommand(t, CommandsWith(s.svc), "press-key", protocol.PressKeyArgs{Window: "900", Key: "Control_L+Escape", Headed: true})
	if cmdErr != nil {
		t.Fatalf("--headed press-key: %v", cmdErr.Error())
	}
	if got := s.pressed; len(got) != 1 || got[0] != "Control_L+Escape" {
		t.Errorf("pressed = %v", got)
	}
	if !strings.Contains(out, `Note: the foreground moved to 900 Windows Explorer "Taskbar"`) {
		t.Errorf("output misses the foreground note:\n%s", out)
	}
}

func TestShellFlyoutButtonsRefuseHeadless(t *testing.T) {
	for _, tc := range []struct {
		index int
		hint  string
	}{
		{1, "winuse --headed click 900 1"},
		{2, "winuse --headed press-key 900 Control_L+Escape"},
	} {
		s := newShellSpy()
		_, cmdErr := runCommand(t, CommandsWith(s.svc), "click", clickArgs("900", tc.index, 0, ""))
		if cmdErr == nil {
			t.Fatalf("click 900 %d ran Headless", tc.index)
		}
		if !strings.HasSuffix(cmdErr.Msg, nothingSentNote) || !strings.Contains(cmdErr.Hint, tc.hint) {
			t.Errorf("click 900 %d: refusal = %q / %q", tc.index, cmdErr.Msg, cmdErr.Hint)
		}
		if steps := s.recorded(); hasStep(steps, "headless-click") || hasStep(steps, "bounds") {
			t.Errorf("click 900 %d: the refusal reached the Element: %v", tc.index, steps)
		}
	}
	// The Secondary Actions too (Start is a ToggleButton).
	s := newShellSpy()
	if _, cmdErr := runCommand(t, CommandsWith(s.svc), "perform-secondary-action", secondaryArgs("900", 2, "Toggle")); cmdErr == nil {
		t.Error("Toggle on the Start button ran Headless")
	}
	if hasStep(s.recorded(), "headless-secondary") {
		t.Error("the Toggle reached the Element")
	}
}

func TestTaskbarAppButtonClickIsHeadlessAndSaysWhereTheForegroundWent(t *testing.T) {
	s := newShellSpy()
	s.fg = 202 // the user works in Chrome
	s.svc.Actions = shellActorActivating{shellActor{s.headlessSpy}, s}
	out, cmdErr := runCommand(t, CommandsWith(s.svc), "click", clickArgs("900", 3, 0, ""))
	if cmdErr != nil {
		t.Fatalf("click on the App's button: %v", cmdErr.Error())
	}
	steps := s.recorded()
	if !hasStep(steps, "headless-click") || hasStep(steps, "activate") {
		t.Errorf("steps = %v, want the Headless click and no activation", steps)
	}
	if !strings.HasPrefix(out, "ok\n") || !strings.Contains(out, `Note: the foreground moved to 101 Notepad "Untitled - Notepad"`) {
		t.Errorf("output:\n%s", out)
	}
}

// shellActorActivating is the shell actor whose Headless click activates
// the App, as a taskbar button's Invoke does.
type shellActorActivating struct {
	shellActor
	spy *shellSpy
}

func (a shellActorActivating) HeadlessClick(ctx context.Context, hwnd uint64, index int) (string, error) {
	route, err := a.shellActor.HeadlessClick(ctx, hwnd, index)
	a.spy.setForeground(101) // Notepad comes to the front
	return route, err
}

func TestOverflowEscapeIsHeadless(t *testing.T) {
	s := newShellSpy()
	s.surfaces = func() []win.WindowInfo { return shellDesktop(true, false) }
	s.fg = shellOverflowID
	s.open[shellOverflowID] = false // hidden at the first read-back
	out, cmdErr := runCommand(t, CommandsWith(s.svc), "press-key", protocol.PressKeyArgs{Window: "950", Key: "Escape"})
	if cmdErr != nil {
		t.Fatalf("press-key 950 Escape: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want ok", out)
	}
	if len(s.posted) != 1 || s.posted[0] != shellOverflowID {
		t.Errorf("posted to %v, want the overflow", s.posted)
	}
	if steps := s.recorded(); hasStep(steps, "activate") || hasStep(steps, "press-key") {
		t.Errorf("the Escape went Headed: %v", steps)
	}

	// Still open after the read-back: delivered, not to be repeated.
	s = newShellSpy()
	s.surfaces = func() []win.WindowInfo { return shellDesktop(true, false) }
	s.open[shellOverflowID] = true
	_, cmdErr = runCommand(t, CommandsWith(s.svc), "press-key", protocol.PressKeyArgs{Window: "950", Key: "Escape"})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "still open") || !strings.Contains(cmdErr.Hint, "do not repeat") {
		t.Errorf("an overflow that stays open: %v", cmdErr)
	}

	// Nothing posted: nothing was sent.
	s = newShellSpy()
	s.surfaces = func() []win.WindowInfo { return shellDesktop(true, false) }
	s.postErr = errors.New("no island input window")
	_, cmdErr = runCommand(t, CommandsWith(s.svc), "press-key", protocol.PressKeyArgs{Window: "950", Key: "Escape"})
	if cmdErr == nil || !strings.HasSuffix(cmdErr.Msg, nothingSentNote) {
		t.Errorf("a failed post: %v", cmdErr)
	}

	// Another key on the overflow has no Headless route.
	s = newShellSpy()
	s.surfaces = func() []win.WindowInfo { return shellDesktop(true, false) }
	if _, cmdErr = runCommand(t, CommandsWith(s.svc), "press-key", protocol.PressKeyArgs{Window: "950", Key: "Return"}); cmdErr == nil {
		t.Error("press-key Return on the overflow without --headed ran")
	}
	if len(s.posted) != 0 {
		t.Errorf("Return was posted: %v", s.posted)
	}
}

func TestShellActionReportsStartOpening(t *testing.T) {
	s := newShellSpy()
	s.svc.Activate = func(hwnd uint64) error { s.step("activate"); return nil }
	s.surfaces = func() []win.WindowInfo { return shellDesktop(false, len(s.pressed) > 0) }
	out, cmdErr := runCommand(t, CommandsWith(s.svc), "press-key", protocol.PressKeyArgs{Window: "900", Key: "Control_L+Escape", Headed: true})
	if cmdErr != nil {
		t.Fatalf("--headed press-key: %v", cmdErr.Error())
	}
	if !strings.Contains(out, `Note: a new Window opened: 960 Start "Start"`) {
		t.Errorf("output misses Start opening:\n%s", out)
	}
}

func TestBatchStepsOnTheShellAreGuarded(t *testing.T) {
	s := newShellSpy()
	_, cmdErr := runCommand(t, CommandsWith(s.svc), "batch", protocol.BatchArgs{Window: "900",
		Steps: []protocol.Step{{Kind: "press-key", Args: []byte(`{"key":"Control_L+Escape"}`)}}})
	if cmdErr == nil {
		t.Fatal("a Batch pressed Control_L+Escape on the taskbar without --headed")
	}
	if hasStep(s.recorded(), "press-key") {
		t.Error("the Batch's step was pressed")
	}
}
