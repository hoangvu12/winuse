package helper

// The Window Actions' offline tests (ADR 0011): restore-window,
// minimize-window, move-window, resize-window and close-window over a
// faked Window. They check the refusals (locked desktop, administrator
// Window, a maximized or minimized Window for move and resize), the
// read-back verification, the outputs, and that no Window Action ever
// activates, injects, raises the banner or shows the Agent Cursor.
// Everything Win32 is faked; no desktop is touched.

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// windowSpy fakes one Window's placement for the Window Actions. A call
// changes the faked Window only after settleAfter polls, so the tests see
// the verification poll at work; noEffect keeps the Window as it was.
type windowSpy struct {
	*actionSpy

	minimized, maximized, restoresMax, open bool
	rect                                    win.Rect // the visible frame, as move and resize set it

	foreground   uint64            // the foreground Window
	selfActivate uint64            // the Window the App activates when it is restored; 0: none
	pids         map[uint64]uint32 // each Window's process

	settleAfter int    // probes before a call's effect shows
	noEffect    bool   // the app ignores the call
	callErr     error  // the Win32 call itself fails
	after       func() // the call's effect, applied once settled
	probes      int    // probes since the call
	sleeps      []time.Duration

	cursor  *cursorSpy
	overlay *overlaySpy
	esc     *scopeEsc
	svc     Services

	moveX, moveY, width, height int32
}

func newWindowSpy() *windowSpy {
	// Window 101 and 102 belong to one App; 500, the user's foreground
	// Window, and 600 to others.
	s := &windowSpy{actionSpy: &actionSpy{}, open: true,
		rect: win.Rect{Left: 100, Top: 100, Right: 900, Bottom: 700}, settleAfter: 2,
		foreground: 500, pids: map[uint64]uint32{101: 7, 102: 7, 500: 9, 600: 11}}
	s.cursor = &cursorSpy{share: s.step}
	s.overlay = &overlaySpy{share: s.step}
	s.esc = &scopeEsc{}
	svc := s.actionSpy.services()
	svc.Cursor = s.cursor
	svc.Overlay = s.overlay
	svc.Esc = s.esc
	svc.Minimized = func(uint64) bool { s.settle(); return s.minimized }
	svc.Maximized = func(uint64) bool { s.settle(); return s.maximized }
	svc.RestoresMaximized = func(uint64) bool { return s.restoresMax }
	svc.WindowOpen = func(uint64) bool { s.settle(); return s.open }
	svc.WindowFrame = func(uint64) (win.Rect, error) { s.settle(); return s.rect, nil }
	svc.Foreground = func() uint64 { s.settle(); return s.foreground }
	svc.WindowPID = func(hwnd uint64) uint32 { return s.pids[hwnd] }
	svc.RestoreWindow = func(uint64) error {
		return s.call("restore", func() {
			s.minimized, s.maximized = false, false
			if s.selfActivate != 0 {
				s.foreground = s.selfActivate
			}
		})
	}
	svc.MinimizeWindow = func(uint64) error {
		return s.call("minimize", func() { s.minimized = true })
	}
	svc.MoveWindow = func(_ uint64, x, y int32) error {
		s.moveX, s.moveY = x, y
		return s.call("move", func() {
			w, h := s.rect.Width(), s.rect.Height()
			s.rect = win.Rect{Left: x, Top: y, Right: x + w, Bottom: y + h}
		})
	}
	svc.ResizeWindow = func(_ uint64, width, height int32) error {
		s.width, s.height = width, height
		return s.call("resize", func() {
			s.rect.Right, s.rect.Bottom = s.rect.Left+width, s.rect.Top+height
		})
	}
	svc.CloseWindow = func(uint64) error {
		return s.call("close", func() { s.open = false })
	}
	svc.MaximizeWindow = func(uint64) error {
		return s.call("maximize", func() { s.minimized, s.maximized = false, true })
	}
	svc.Sleep = func(d time.Duration) { s.sleeps = append(s.sleeps, d) }
	s.svc = svc
	return s
}

// call records one Window Action call and schedules its effect.
func (s *windowSpy) call(name string, effect func()) error {
	s.step(name)
	if s.callErr != nil {
		return s.callErr
	}
	if !s.noEffect {
		s.after, s.probes = effect, 0
	}
	return nil
}

// settle applies a pending effect once enough probes have passed.
func (s *windowSpy) settle() {
	if s.after == nil {
		return
	}
	s.probes++
	if s.probes > s.settleAfter {
		s.after()
		s.after = nil
	}
}

// assertNoForeground checks that a Window Action took nothing from the
// user: no activation, no input, no banner, no Agent Cursor.
func (s *windowSpy) assertNoForeground(t *testing.T) {
	t.Helper()
	steps := s.recorded()
	assertNoV1(t, steps)
	if hasStepPrefix(steps, "cursor-") {
		t.Errorf("a Window Action showed the Agent Cursor: %v", steps)
	}
}

// windowActionCalls are the recordings of the Window Actions' Win32 calls.
var windowActionCalls = []string{"restore", "minimize", "move", "resize", "close", "maximize"}

func (s *windowSpy) assertNothingSent(t *testing.T) {
	t.Helper()
	for _, c := range windowActionCalls {
		if containsStep(s.recorded(), c) {
			t.Errorf("a refused Window Action reached the %q call: %v", c, s.recorded())
		}
	}
}

func TestRestoreWindowRestoresWithoutActivating(t *testing.T) {
	spy := newWindowSpy()
	spy.minimized = true
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "restore-window", protocol.WindowActionArgs{Window: "101"})
	if cmdErr != nil {
		t.Fatalf("restore-window: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want ok", out)
	}
	if got := spy.recorded(); !equalStrings(got[:3], []string{"locked", "integrity", "restore"}) {
		t.Errorf("steps = %v, want the refusal checks, then the restore", got)
	}
	if len(spy.sleeps) == 0 {
		t.Error("the restore must be verified by polling the Window")
	}
	if b := spy.esc.brackets(); !equalStrings(b, []string{"arm-passive", "disarm-passive"}) {
		t.Errorf("Esc scopes = %v, want one passive scope", b)
	}
	spy.assertNoForeground(t)
}

func TestRestoreWindowNotesNormalSizeForAMaximizedWindow(t *testing.T) {
	spy := newWindowSpy()
	spy.minimized, spy.restoresMax = true, true
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "restore-window", protocol.WindowActionArgs{Window: "101"})
	if cmdErr != nil {
		t.Fatalf("restore-window: %v", cmdErr.Error())
	}
	if !strings.HasPrefix(out, "ok\nNote: ") || !strings.Contains(out, "restored at normal size") {
		t.Errorf("output = %q, want ok with the normal-size note", out)
	}
	spy.assertNoForeground(t)
}

func TestRestoreWindowReportsSelfActivation(t *testing.T) {
	const note = "ok\nNote: the App brought itself to the foreground\n"
	cases := []struct {
		name         string
		before       uint64 // the foreground before the restore
		selfActivate uint64 // the Window the App activates; 0: none
		want         string
	}{
		{"the restored Window took the foreground", 500, 101, note},
		{"another Window of its App took it", 500, 102, note},
		{"the foreground stayed", 500, 0, "ok\n"},
		{"another App took it", 500, 600, "ok\n"},
		{"the App had it already", 102, 101, "ok\n"},
		{"there was no foreground before", 0, 101, note},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := newWindowSpy()
			spy.minimized = true
			spy.foreground, spy.selfActivate = tc.before, tc.selfActivate
			out, cmdErr := runCommand(t, CommandsWith(spy.svc), "restore-window", protocol.WindowActionArgs{Window: "101"})
			if cmdErr != nil {
				t.Fatalf("restore-window: %v", cmdErr.Error())
			}
			if out != tc.want {
				t.Errorf("output = %q, want %q", out, tc.want)
			}
			// winuse never takes the foreground back.
			spy.assertNoForeground(t)
		})
	}
}

func TestRestoreWindowJoinsTheNotes(t *testing.T) {
	spy := newWindowSpy()
	spy.minimized, spy.restoresMax, spy.selfActivate = true, true, 101
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "restore-window", protocol.WindowActionArgs{Window: "101"})
	if cmdErr != nil {
		t.Fatalf("restore-window: %v", cmdErr.Error())
	}
	if !strings.HasPrefix(out, "ok\nNote: restored at normal size: ") || !strings.HasSuffix(out, "\nNote: the App brought itself to the foreground\n") {
		t.Errorf("output = %q, want both notes, a line each", out)
	}
}

func TestRestoreWindowAlreadyRestoredSendsNothing(t *testing.T) {
	spy := newWindowSpy()
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "restore-window", protocol.WindowActionArgs{Window: "101"})
	if cmdErr != nil || out != "ok\n" {
		t.Fatalf("restore-window = %q, %v; want ok", out, cmdErr)
	}
	spy.assertNothingSent(t)
}

func TestRestoreWindowUnmaximizes(t *testing.T) {
	spy := newWindowSpy()
	spy.maximized = true
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "restore-window", protocol.WindowActionArgs{Window: "101"})
	if cmdErr != nil || out != "ok\n" {
		t.Fatalf("restore-window = %q, %v; want ok", out, cmdErr)
	}
	if !containsStep(spy.recorded(), "restore") {
		t.Error("a maximized Window must be restored to its normal size")
	}
}

func TestRestoreWindowUnverified(t *testing.T) {
	spy := newWindowSpy()
	spy.minimized, spy.noEffect = true, true
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "restore-window", protocol.WindowActionArgs{Window: "101"})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "window 101 is still minimized") {
		t.Fatalf("message = %v, want the still-minimized failure", cmdErr)
	}
	if strings.Contains(cmdErr.Msg, "nothing was sent") || !strings.Contains(cmdErr.Hint, "delivered") {
		t.Errorf("a failed read-back was delivered: %q / %q", cmdErr.Msg, cmdErr.Hint)
	}
	if !strings.Contains(cmdErr.Hint, "winuse activate-window 101") {
		t.Errorf("hint = %q, want the foreground alternative", cmdErr.Hint)
	}
	spy.assertNoForeground(t)
}

func TestMinimizeWindow(t *testing.T) {
	spy := newWindowSpy()
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "minimize-window", protocol.WindowActionArgs{Window: "notepad"})
	if cmdErr != nil || out != "ok\n" {
		t.Fatalf("minimize-window = %q, %v; want ok", out, cmdErr)
	}
	if !spy.minimized || !containsStep(spy.recorded(), "minimize") {
		t.Errorf("the Window was not minimized: %v", spy.recorded())
	}
	spy.assertNoForeground(t)

	// Already minimized: nothing to send.
	spy = newWindowSpy()
	spy.minimized = true
	if out, cmdErr := runCommand(t, CommandsWith(spy.svc), "minimize-window", protocol.WindowActionArgs{Window: "101"}); cmdErr != nil || out != "ok\n" {
		t.Fatalf("minimize-window (minimized) = %q, %v; want ok", out, cmdErr)
	}
	spy.assertNothingSent(t)

	// The app ignores it: a delivered failure.
	spy = newWindowSpy()
	spy.noEffect = true
	_, cmdErr = runCommand(t, CommandsWith(spy.svc), "minimize-window", protocol.WindowActionArgs{Window: "101"})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "window 101 is not minimized") || !strings.Contains(cmdErr.Hint, "delivered") {
		t.Fatalf("unverified minimize = %v", cmdErr)
	}
}

func TestMoveWindowVerifiesThePlacement(t *testing.T) {
	spy := newWindowSpy()
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "move-window", protocol.MoveWindowArgs{Window: "101", X: -1920, Y: 40})
	if cmdErr != nil || out != "ok\n" {
		t.Fatalf("move-window = %q, %v; want ok", out, cmdErr)
	}
	if spy.moveX != -1920 || spy.moveY != 40 {
		t.Errorf("moved to %d,%d, want -1920,40", spy.moveX, spy.moveY)
	}
	if spy.rect.Width() != 800 || spy.rect.Height() != 600 {
		t.Errorf("a move changed the size: %+v", spy.rect)
	}
	spy.assertNoForeground(t)
}

func TestMoveAndResizeTolerance(t *testing.T) {
	cases := []struct {
		name    string
		cmd     string
		args    any
		landed  win.Rect
		wantErr string
	}{
		{"move within 8 px", "move-window", protocol.MoveWindowArgs{Window: "101", X: 0, Y: 0},
			win.Rect{Left: -8, Top: 8, Right: 792, Bottom: 608}, ""},
		{"move off by 9 px", "move-window", protocol.MoveWindowArgs{Window: "101", X: 0, Y: 0},
			win.Rect{Left: 9, Top: 0, Right: 809, Bottom: 600}, "window 101 is at 9,0 (800x600) after the move to 0,0"},
		{"resize within 8 px", "resize-window", protocol.ResizeWindowArgs{Window: "101", Width: 1024, Height: 768},
			win.Rect{Left: 100, Top: 100, Right: 1132, Bottom: 860}, ""},
		{"resize clamped by the app", "resize-window", protocol.ResizeWindowArgs{Window: "101", Width: 200, Height: 100},
			win.Rect{Left: 100, Top: 100, Right: 600, Bottom: 400}, "window 101 is 500x300 (at 100,100) after the resize to 200x100"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := newWindowSpy()
			spy.noEffect = true
			spy.rect = tc.landed
			out, cmdErr := runCommand(t, CommandsWith(spy.svc), tc.cmd, tc.args)
			if tc.wantErr == "" {
				if cmdErr != nil || out != "ok\n" {
					t.Fatalf("%s = %q, %v; want ok", tc.cmd, out, cmdErr)
				}
				if len(spy.sleeps) != 0 {
					t.Errorf("a placement already in tolerance needs no poll: %v", spy.sleeps)
				}
				return
			}
			if cmdErr == nil || !strings.Contains(cmdErr.Msg, tc.wantErr) {
				t.Fatalf("message = %v, want %q", cmdErr, tc.wantErr)
			}
			if strings.Contains(cmdErr.Msg, "nothing was sent") || !strings.Contains(cmdErr.Hint, "delivered") {
				t.Errorf("a failed read-back was delivered: %q / %q", cmdErr.Msg, cmdErr.Hint)
			}
			if !strings.Contains(cmdErr.Hint, "get-window-state 101 --screenshot") || !strings.Contains(cmdErr.Hint, "window at X,Y WxH") {
				t.Errorf("hint = %q, want the screenshot line's window at rectangle as the way to read the frame", cmdErr.Hint)
			}
			if len(spy.sleeps) == 0 {
				t.Error("the placement must be polled before it fails")
			}
		})
	}
}

func TestResizeWindowVerifiesThePlacement(t *testing.T) {
	spy := newWindowSpy()
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "resize-window", protocol.ResizeWindowArgs{Window: "101", Width: 1280, Height: 720})
	if cmdErr != nil || out != "ok\n" {
		t.Fatalf("resize-window = %q, %v; want ok", out, cmdErr)
	}
	if spy.width != 1280 || spy.height != 720 {
		t.Errorf("resized to %dx%d, want 1280x720", spy.width, spy.height)
	}
	if spy.rect.Left != 100 || spy.rect.Top != 100 {
		t.Errorf("a resize moved the Window: %+v", spy.rect)
	}
	spy.assertNoForeground(t)
}

func TestMoveAndResizeRefuseAMaximizedOrMinimizedWindow(t *testing.T) {
	for _, tc := range []struct {
		cmd  string
		args any
	}{
		{"move-window", protocol.MoveWindowArgs{Window: "101", X: 10, Y: 10}},
		{"resize-window", protocol.ResizeWindowArgs{Window: "101", Width: 640, Height: 480}},
	} {
		for _, state := range []string{"maximized", "minimized"} {
			t.Run(tc.cmd+" "+state, func(t *testing.T) {
				spy := newWindowSpy()
				spy.maximized, spy.minimized = state == "maximized", state == "minimized"
				_, cmdErr := runCommand(t, CommandsWith(spy.svc), tc.cmd, tc.args)
				want := "window 101 is " + state + "; nothing was sent"
				if cmdErr == nil || cmdErr.Msg != want {
					t.Fatalf("message = %v, want %q", cmdErr, want)
				}
				if !strings.Contains(cmdErr.Hint, "winuse restore-window 101") {
					t.Errorf("hint = %q, want restore-window", cmdErr.Hint)
				}
				spy.assertNothingSent(t)
				spy.assertNoForeground(t)
			})
		}
	}
}

func TestResizeWindowRefusesAnEmptySize(t *testing.T) {
	for _, args := range []protocol.ResizeWindowArgs{
		{Window: "101", Width: 0, Height: 480},
		{Window: "101", Width: 640, Height: -1},
		{Window: "101", Width: 2_000_000, Height: 480},
	} {
		spy := newWindowSpy()
		_, cmdErr := runCommand(t, CommandsWith(spy.svc), "resize-window", args)
		if cmdErr == nil || !strings.Contains(cmdErr.Msg, "width and height must be between 1 and 1000000") {
			t.Errorf("resize %dx%d: %v, want the size refusal", args.Width, args.Height, cmdErr)
		}
		spy.assertNothingSent(t)
	}
	spy := newWindowSpy()
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "move-window", protocol.MoveWindowArgs{Window: "101", X: 5_000_000})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "x and y must be between -1000000 and 1000000") {
		t.Errorf("move far away: %v, want the coordinate refusal", cmdErr)
	}
	spy.assertNothingSent(t)
}

func TestCloseWindow(t *testing.T) {
	spy := newWindowSpy()
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "close-window", protocol.WindowActionArgs{Window: "101"})
	if cmdErr != nil || out != "ok\n" {
		t.Fatalf("close-window = %q, %v; want ok", out, cmdErr)
	}
	if spy.open {
		t.Error("the Window must be closed")
	}
	spy.assertNoForeground(t)
}

func TestCloseWindowStillOpenIsOKWithANote(t *testing.T) {
	spy := newWindowSpy()
	spy.noEffect = true // the app asks to save
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "close-window", protocol.WindowActionArgs{Window: "101"})
	if cmdErr != nil {
		t.Fatalf("close-window: %v", cmdErr.Error())
	}
	for _, want := range []string{"ok\nNote: ", "close was requested", "still open", "asking to save", "list-windows", "get-window-state 101"} {
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want it to contain %q", out, want)
		}
	}
	// The wait is bounded at 2 s.
	var waited time.Duration
	for _, d := range spy.sleeps {
		waited += d
	}
	if waited < 2*time.Second || waited > 2*time.Second+100*time.Millisecond {
		t.Errorf("waited %v for the Window to go away, want 2 s", waited)
	}
}

func TestWindowActionCallFailureSaysNothingSent(t *testing.T) {
	spy := newWindowSpy()
	spy.callErr = errors.New("PostMessage(WM_CLOSE) failed: access denied")
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "close-window", protocol.WindowActionArgs{Window: "101"})
	if cmdErr == nil || cmdErr.Msg != "PostMessage(WM_CLOSE) failed: access denied; nothing was sent" {
		t.Fatalf("message = %v, want the call failure with nothing sent", cmdErr)
	}
}

func TestWindowActionsRefuseLockedDesktopAndAdministratorWindows(t *testing.T) {
	cmds := []struct {
		cmd  string
		args any
	}{
		{"restore-window", protocol.WindowActionArgs{Window: "101"}},
		{"minimize-window", protocol.WindowActionArgs{Window: "101"}},
		{"move-window", protocol.MoveWindowArgs{Window: "101", X: 1, Y: 1}},
		{"resize-window", protocol.ResizeWindowArgs{Window: "101", Width: 640, Height: 480}},
		{"close-window", protocol.WindowActionArgs{Window: "101"}},
	}
	for _, c := range cmds {
		t.Run(c.cmd, func(t *testing.T) {
			locked := newWindowSpy()
			locked.minimized = true // restore-window would otherwise act
			locked.locked = true
			_, cmdErr := runCommand(t, CommandsWith(locked.svc), c.cmd, c.args)
			if cmdErr == nil || !strings.Contains(cmdErr.Msg, "desktop is locked") || !strings.HasSuffix(cmdErr.Msg, "; nothing was sent") {
				t.Fatalf("locked: %v", cmdErr)
			}
			locked.assertNothingSent(t)

			admin := newWindowSpy()
			admin.minimized = true
			admin.refuse = &win.AdminWindowError{WindowID: 101, App: "Notepad.exe"}
			_, cmdErr = runCommand(t, CommandsWith(admin.svc), c.cmd, c.args)
			if cmdErr == nil || !strings.Contains(cmdErr.Msg, "administrator") || !strings.HasSuffix(cmdErr.Msg, "; nothing was sent") {
				t.Fatalf("admin: %v", cmdErr)
			}
			if !strings.Contains(cmdErr.Hint, "elevated") {
				t.Errorf("hint = %q, want the elevated-terminal advice", cmdErr.Hint)
			}
			admin.assertNothingSent(t)
			if b := admin.esc.brackets(); len(b) != 0 {
				t.Errorf("a refusal must arm nothing: %v", b)
			}

			// A missing Window is a usage failure.
			noWindow := map[string]any{"x": 1, "y": 1, "width": 640, "height": 480}
			_, cmdErr = runCommand(t, CommandsWith(newWindowSpy().svc), c.cmd, noWindow)
			if cmdErr == nil || !strings.Contains(cmdErr.Msg, "a window reference is required") {
				t.Fatalf("missing window: %v", cmdErr)
			}
		})
	}
}

func TestWindowActionsAreSerializedAndNotBatchSteps(t *testing.T) {
	cmds := CommandsWith(Services{Trees: &fakeTrees{}})
	for _, name := range []string{"restore-window", "minimize-window", "move-window", "resize-window", "close-window"} {
		if _, ok := cmds[name]; !ok {
			t.Errorf("%s is not registered", name)
		}
		if observationOnlyCommand(name) {
			t.Errorf("%s must run on the action gate", name)
		}
		for _, k := range protocol.BatchStepKinds {
			if k == name {
				t.Errorf("%s must not be a Batch step kind", name)
			}
		}
		if headlessStep(name, nil, nil) || takesHeaded(name) {
			t.Errorf("%s has no headed form and no Element routing", name)
		}
	}
}

// maximize-window (desktop-parity ticket 11) is the one Headed Window
// Action: without headed it sends nothing; with it, the Headed pipeline
// activates the Window and then maximizes it.

func TestMaximizeWindowWithoutHeadedSendsNothing(t *testing.T) {
	for _, state := range []string{"normal", "minimized"} {
		t.Run(state, func(t *testing.T) {
			spy := newWindowSpy()
			spy.minimized = state == "minimized"
			_, cmdErr := runCommand(t, CommandsWith(spy.svc), "maximize-window", protocol.MaximizeWindowArgs{Window: "101"})
			want := "window 101 cannot be maximized headless: Windows has no maximize that leaves the foreground alone; nothing was sent"
			if cmdErr == nil || cmdErr.Msg != want {
				t.Fatalf("message = %v, want %q", cmdErr, want)
			}
			for _, hint := range []string{"winuse --headed maximize-window 101", "move-window", "resize-window", "work area"} {
				if !strings.Contains(cmdErr.Hint, hint) {
					t.Errorf("hint = %q, want it to contain %q", cmdErr.Hint, hint)
				}
			}
			spy.assertNothingSent(t)
			spy.assertNoForeground(t)
			if b := spy.esc.brackets(); len(b) != 0 {
				t.Errorf("a refusal must arm nothing: %v", b)
			}
		})
	}
}

func TestMaximizeWindowHeadedActivatesThenMaximizes(t *testing.T) {
	for _, state := range []string{"normal", "minimized"} {
		t.Run(state, func(t *testing.T) {
			spy := newWindowSpy()
			spy.minimized = state == "minimized"
			out, cmdErr := runCommand(t, CommandsWith(spy.svc), "maximize-window", protocol.MaximizeWindowArgs{Window: "notepad", Headed: true})
			if cmdErr != nil || out != "ok\n" {
				t.Fatalf("maximize-window = %q, %v; want ok", out, cmdErr)
			}
			if !spy.maximized || spy.minimized {
				t.Errorf("the Window is not maximized: %v", spy.recorded())
			}
			steps := spy.recorded()
			activated, maximized := -1, -1
			for i, s := range steps {
				switch s {
				case "activate":
					activated = i
				case "maximize":
					maximized = i
				}
			}
			if activated < 0 || maximized < activated {
				t.Errorf("steps = %v, want the activation before the maximize", steps)
			}
			if !hasStepPrefix(steps, "overlay-show") {
				t.Errorf("a Headed Window Action shows the in-control banner: %v", steps)
			}
			if b := spy.esc.brackets(); !equalStrings(b, []string{"arm", "disarm"}) {
				t.Errorf("Esc scopes = %v, want the Headed swallowing scope", b)
			}
			if len(spy.sleeps) == 0 {
				t.Error("the maximize must be verified by polling the Window")
			}
		})
	}
}

func TestMaximizeWindowAlreadyMaximizedSendsNothing(t *testing.T) {
	for _, headed := range []bool{false, true} {
		spy := newWindowSpy()
		spy.maximized = true
		out, cmdErr := runCommand(t, CommandsWith(spy.svc), "maximize-window", protocol.MaximizeWindowArgs{Window: "101", Headed: headed})
		if cmdErr != nil || out != "ok\n" {
			t.Fatalf("maximize-window (maximized, headed %v) = %q, %v; want ok", headed, out, cmdErr)
		}
		spy.assertNothingSent(t)
		spy.assertNoForeground(t)
	}
}

func TestMaximizeWindowUnverified(t *testing.T) {
	spy := newWindowSpy()
	spy.noEffect = true
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "maximize-window", protocol.MaximizeWindowArgs{Window: "101", Headed: true})
	if cmdErr == nil || cmdErr.Msg != "window 101 is not maximized after the maximize" {
		t.Fatalf("message = %v, want the not-maximized failure", cmdErr)
	}
	if !strings.Contains(cmdErr.Hint, "delivered") {
		t.Errorf("a failed read-back was delivered: %q", cmdErr.Hint)
	}

	// The call itself fails after the activation: the foreground moved, so
	// the failure does not say nothing was sent.
	spy = newWindowSpy()
	spy.callErr = errors.New("ShowWindowAsync(SW_MAXIMIZE) failed for window 101: invalid window handle")
	_, cmdErr = runCommand(t, CommandsWith(spy.svc), "maximize-window", protocol.MaximizeWindowArgs{Window: "101", Headed: true})
	if cmdErr == nil || strings.Contains(cmdErr.Msg, "nothing was sent") || !strings.Contains(cmdErr.Hint, "foreground") {
		t.Fatalf("call failure = %v, want it without nothing-was-sent and a hint about the foreground", cmdErr)
	}
}

func TestMaximizeWindowRefusalsComeBeforeTheActivation(t *testing.T) {
	locked := newWindowSpy()
	locked.locked = true
	_, cmdErr := runCommand(t, CommandsWith(locked.svc), "maximize-window", protocol.MaximizeWindowArgs{Window: "101", Headed: true})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "desktop is locked") {
		t.Fatalf("locked: %v", cmdErr)
	}
	locked.assertNothingSent(t)
	locked.assertNoForeground(t)

	admin := newWindowSpy()
	admin.refuse = &win.AdminWindowError{WindowID: 101, App: "Notepad.exe"}
	_, cmdErr = runCommand(t, CommandsWith(admin.svc), "maximize-window", protocol.MaximizeWindowArgs{Window: "101", Headed: true})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "administrator") || !strings.Contains(cmdErr.Hint, "elevated") {
		t.Fatalf("admin: %v", cmdErr)
	}
	admin.assertNothingSent(t)
	admin.assertNoForeground(t)

	_, cmdErr = runCommand(t, CommandsWith(newWindowSpy().svc), "maximize-window", map[string]any{"headed": true})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "a window reference is required") {
		t.Fatalf("missing window: %v", cmdErr)
	}
}

func TestMaximizeWindowIsSerializedAndNoBatchStep(t *testing.T) {
	cmds := CommandsWith(Services{Trees: &fakeTrees{}})
	if _, ok := cmds["maximize-window"]; !ok {
		t.Fatal("maximize-window is not registered")
	}
	if observationOnlyCommand("maximize-window") {
		t.Error("maximize-window must run on the action gate")
	}
	for _, k := range protocol.BatchStepKinds {
		if k == "maximize-window" {
			t.Error("maximize-window must not be a Batch step kind")
		}
	}
	if headlessStep("maximize-window", nil, nil) {
		t.Error("maximize-window has no Headless form")
	}
}

func TestReadyErrorMinimizedHintNamesRestoreWindow(t *testing.T) {
	e := readyError(&win.MinimizedError{WindowID: 101}, win.WindowInfo{ID: 101})
	for _, want := range []string{"winuse restore-window 101", "winuse activate-window 101", "--headed"} {
		if !strings.Contains(e.Hint, want) {
			t.Errorf("hint = %q, want it to contain %q", e.Hint, want)
		}
	}
}
