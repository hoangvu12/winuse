package helper

// The Action commands' offline tests (ticket 05): the output contract
// (`ok` or a one-line failure with a hint, exit 1), the refusal messages
// (locked desktop, administrator Window) and the stale messages, plus the
// pipeline's ordering guarantees: the checks refuse before any activation,
// and no input is injected once anything refuses. Everything Win32 is
// faked; no desktop is touched.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// actionSpy fakes the Action pipeline's Win32 steps and records the order
// they ran in, so tests can assert both the messages and "no input was
// sent".
type actionSpy struct {
	mu sync.Mutex

	steps []string // ordered pipeline steps

	// configurable outcomes
	locked        bool
	lockedDesktop string
	refuse        error // the integrity check's answer
	activateErr   error
	point         uia.Point
	pointErr      error
	clickErr      error

	// recorded calls
	clickTarget uint64
	clickX      int32
	clickY      int32
	clickButton win.MouseButton
	clickCount  int
	pointHwnd   uint64
	pointIndex  int

	// name:/id: reference resolution (ticket 11): the recorded reference
	// and the configurable outcome.
	refHwnd  uint64
	refRef   string
	refIndex int
	refErr   error
}

func (s *actionSpy) step(name string) {
	s.mu.Lock()
	s.steps = append(s.steps, name)
	s.mu.Unlock()
}

func (s *actionSpy) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.steps...)
}

// services builds Services wired to the spy over the standard fake
// Windows.
func (s *actionSpy) services() Services {
	svc := testServices(&fakeTrees{})
	svc.Locked = func() (bool, string) { s.step("locked"); return s.locked, s.lockedDesktop }
	svc.Integrity = func(w win.WindowInfo) error { s.step("integrity"); return s.refuse }
	svc.Activate = func(hwnd uint64) error { s.step("activate"); return s.activateErr }
	svc.Actions = s
	svc.Click = func(target uint64, x, y int32, button win.MouseButton, count int) error {
		s.step("click")
		s.mu.Lock()
		s.clickTarget, s.clickX, s.clickY, s.clickButton, s.clickCount = target, x, y, button, count
		err := s.clickErr
		s.mu.Unlock()
		return err
	}
	return svc
}

// ElementPoint implements ElementPointer.
func (s *actionSpy) ElementPoint(ctx context.Context, hwnd uint64, index int) (uia.Point, error) {
	s.step("point")
	s.mu.Lock()
	s.pointHwnd, s.pointIndex = hwnd, index
	p, e := s.point, s.pointErr
	s.mu.Unlock()
	return p, e
}

// clickArgs builds wire arguments for the click command. index < -1
// means "no element_index at all"; -1 itself is a real negative index.
func clickArgs(window string, index, count int, button string) json.RawMessage {
	var ip *int
	if index > -2 {
		ip = &index
	}
	raw, err := json.Marshal(protocol.ClickArgs{Window: window, ElementIndex: ip, ClickCount: count, MouseButton: button})
	if err != nil {
		panic(err)
	}
	return raw
}

func TestClickPrintsOK(t *testing.T) {
	spy := &actionSpy{point: uia.Point{X: 100, Y: 200}}
	out, cmdErr := runCommand(t, CommandsWith(spy.services()), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr != nil {
		t.Fatalf("click: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want %q", out, "ok\n")
	}
	// The pipeline order: checks, activation, element resolution, input.
	want := []string{"locked", "integrity", "activate", "point", "click"}
	if got := spy.recorded(); !equalStrings(got, want) {
		t.Errorf("pipeline steps = %v, want %v", got, want)
	}
	if spy.clickTarget != 101 || spy.pointHwnd != 101 || spy.pointIndex != 2 {
		t.Errorf("clicked window %d element %d (resolved %d), want window 101 element 2", spy.clickTarget, spy.pointIndex, spy.pointHwnd)
	}
	if spy.clickX != 100 || spy.clickY != 200 {
		t.Errorf("clicked at (%d, %d), want the resolved point (100, 200)", spy.clickX, spy.clickY)
	}
	if spy.clickButton != win.MouseLeft || spy.clickCount != 1 {
		t.Errorf("button/count = %d/%d, want left/1", spy.clickButton, spy.clickCount)
	}
}

func TestClickForwardsButtonCountAndAliases(t *testing.T) {
	cases := []struct {
		button     string
		count      int
		wantButton win.MouseButton
		wantCount  int
	}{
		{"", 0, win.MouseLeft, 1}, // defaults
		{"left", 1, win.MouseLeft, 1},
		{"l", 1, win.MouseLeft, 1},
		{"LEFT", 1, win.MouseLeft, 1}, // case-insensitive like Codex keys
		{"right", 1, win.MouseRight, 1},
		{"r", 1, win.MouseRight, 1},
		{"middle", 1, win.MouseMiddle, 1},
		{"m", 1, win.MouseMiddle, 1},
		{"left", 2, win.MouseLeft, 2},
		{"left", 3, win.MouseLeft, 3},
	}
	for _, tc := range cases {
		spy := &actionSpy{}
		_, cmdErr := runCommand(t, CommandsWith(spy.services()), "click", clickArgs("notepad", 0, tc.count, tc.button))
		if cmdErr != nil {
			t.Fatalf("button %q count %d: %v", tc.button, tc.count, cmdErr.Error())
		}
		if spy.clickButton != tc.wantButton || spy.clickCount != tc.wantCount {
			t.Errorf("button %q count %d: clicked %d/%d, want %d/%d",
				tc.button, tc.count, spy.clickButton, spy.clickCount, tc.wantButton, tc.wantCount)
		}
	}
}

func TestClickRefusesLockedDesktopBeforeAnyInput(t *testing.T) {
	spy := &actionSpy{locked: true}
	_, cmdErr := runCommand(t, CommandsWith(spy.services()), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr == nil {
		t.Fatal("a locked desktop must refuse the click")
	}
	if cmdErr.Msg != "Windows desktop is locked; unlock before using winuse" {
		t.Errorf("message = %q", cmdErr.Msg)
	}
	if !strings.Contains(cmdErr.Hint, "unlock") {
		t.Errorf("hint = %q, want it to say unlock", cmdErr.Hint)
	}
	if cmdErr.Exit != 1 {
		t.Errorf("exit = %d, want 1", cmdErr.Exit)
	}
	// No input, no activation: the refusal must come first.
	if got := spy.recorded(); !equalStrings(got, []string{"locked"}) {
		t.Errorf("pipeline steps = %v, want only the lock check", got)
	}
}

func TestClickLockedDesktopNamesInputDesktop(t *testing.T) {
	// A non-Default input desktop (the lock screen, the UAC secure
	// desktop) is named in the message; a WTS-locked session still on the
	// Default desktop gets the plain message.
	for _, tc := range []struct {
		desktop string
		want    string
	}{
		{"LockApp", "Windows desktop is locked (input desktop is LockApp); unlock before using winuse"},
		{"Winlogon", "Windows desktop is locked (input desktop is Winlogon); unlock before using winuse"},
		{"Default", "Windows desktop is locked; unlock before using winuse"},
		{"", "Windows desktop is locked; unlock before using winuse"},
	} {
		spy := &actionSpy{locked: true, lockedDesktop: tc.desktop}
		_, cmdErr := runCommand(t, CommandsWith(spy.services()), "click", clickArgs("101", 2, 1, "left"))
		if cmdErr == nil || cmdErr.Msg != tc.want {
			t.Fatalf("desktop %q: message = %v, want %q", tc.desktop, cmdErr, tc.want)
		}
	}
}

func TestClickRefusesAdminWindowWithExplanation(t *testing.T) {
	spy := &actionSpy{refuse: &win.AdminWindowError{WindowID: 101, App: "Notepad.exe"}}
	_, cmdErr := runCommand(t, CommandsWith(spy.services()), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr == nil {
		t.Fatal("an administrator Window must refuse the click")
	}
	if cmdErr.Msg != "window 101 (Notepad.exe) is running as administrator; winuse cannot send it input" {
		t.Errorf("message = %q", cmdErr.Msg)
	}
	if !strings.Contains(cmdErr.Hint, "elevated") || !strings.Contains(cmdErr.Hint, "silently drops") {
		t.Errorf("hint = %q, want the elevated-terminal hint", cmdErr.Hint)
	}
	if cmdErr.Exit != 1 {
		t.Errorf("exit = %d, want 1", cmdErr.Exit)
	}
	// The integrity check refused before activating or sending input.
	if got := spy.recorded(); !equalStrings(got, []string{"locked", "integrity"}) {
		t.Errorf("pipeline steps = %v, want no activation and no input", got)
	}
}

func TestClickAdminWindowUnreadableSaysLikely(t *testing.T) {
	spy := &actionSpy{refuse: &win.AdminWindowError{WindowID: 101, App: "Notepad.exe", Unreadable: true}}
	_, cmdErr := runCommand(t, CommandsWith(spy.services()), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "likely running as administrator") {
		t.Fatalf("message = %v, want the likely-elevated wording", cmdErr)
	}
}

func TestClickIntegrityReadFailureSurfaces(t *testing.T) {
	spy := &actionSpy{refuse: errors.New("cannot read the process of window 101; run winuse list-windows again")}
	_, cmdErr := runCommand(t, CommandsWith(spy.services()), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "cannot read the process") {
		t.Fatalf("message = %v", cmdErr)
	}
}

func TestClickActivationFailure(t *testing.T) {
	spy := &actionSpy{activateErr: &win.NotForegroundError{WindowID: 101}}
	_, cmdErr := runCommand(t, CommandsWith(spy.services()), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr == nil || cmdErr.Msg != "failed to activate window 101" {
		t.Fatalf("message = %v", cmdErr)
	}
	if !strings.Contains(cmdErr.Hint, "list-windows") {
		t.Errorf("hint = %q, want a list-windows recovery hint", cmdErr.Hint)
	}
	if got := spy.recorded(); !containsStep(got, "activate") || containsStep(got, "click") {
		t.Errorf("pipeline steps = %v: activation attempted, no input sent", got)
	}
}

func TestClickModalDialogPointsAtTheDialog(t *testing.T) {
	spy := &actionSpy{activateErr: &win.ModalDialogError{WindowID: 101, DialogID: 202}}
	_, cmdErr := runCommand(t, CommandsWith(spy.services()), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr == nil || cmdErr.Msg != "window 101 has a modal dialog open (window 202); act on the dialog instead" {
		t.Fatalf("message = %v", cmdErr)
	}
	if !strings.Contains(cmdErr.Hint, "get-window-state 202") {
		t.Errorf("hint = %q, want it to point at the dialog's Window State", cmdErr.Hint)
	}
	if containsStep(spy.recorded(), "click") {
		t.Error("no input may be sent to a Window blocked by a modal dialog")
	}
}

func TestClickStaleElementIndex(t *testing.T) {
	spy := &actionSpy{pointErr: fmt.Errorf("%w: element 2 is not in the cached Window State of window 101", uia.ErrStale)}
	_, cmdErr := runCommand(t, CommandsWith(spy.services()), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr == nil || cmdErr.Msg != "element 2 is not available in the cached Window State for window 101" {
		t.Fatalf("message = %v", cmdErr)
	}
	if !strings.Contains(cmdErr.Hint, "get-window-state 101") || !strings.Contains(cmdErr.Hint, "fresh Element Indexes") {
		t.Errorf("hint = %q, want the read-the-state-again instruction", cmdErr.Hint)
	}
	if containsStep(spy.recorded(), "click") {
		t.Error("a stale index must not inject input")
	}
}

func TestClickWithoutCachedState(t *testing.T) {
	spy := &actionSpy{pointErr: fmt.Errorf("%w: window 101", uia.ErrNoState)}
	_, cmdErr := runCommand(t, CommandsWith(spy.services()), "click", clickArgs("101", 2, 1, "left"))
	if cmdErr == nil || cmdErr.Msg != "no cached Window State is available for window 101" {
		t.Fatalf("message = %v", cmdErr)
	}
	if !strings.Contains(cmdErr.Hint, "get-window-state 101") {
		t.Errorf("hint = %q, want the read-the-state instruction", cmdErr.Hint)
	}
}

func TestClickInputFailureMessages(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantMsg  string
		wantHint string
	}{
		{
			name:     "occluded point names the covering window",
			err:      &win.OcclusionError{X: 660, Y: 430, OverTitle: "Something Else"},
			wantMsg:  `point (660, 430) is over "Something Else", not the target Window`,
			wantHint: "covers the target Element",
		},
		{
			name:    "occluded point without a title",
			err:     &win.OcclusionError{X: 5, Y: 5},
			wantMsg: "point (5, 5) is over another window, not the target Window",
		},
		{
			name:    "pointer did not reach",
			err:     errors.New("the pointer did not reach (10, 20)"),
			wantMsg: "the pointer did not reach (10, 20)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := &actionSpy{clickErr: tc.err}
			_, cmdErr := runCommand(t, CommandsWith(spy.services()), "click", clickArgs("101", 2, 1, "left"))
			if cmdErr == nil || cmdErr.Msg != tc.wantMsg {
				t.Fatalf("message = %v, want %q", cmdErr, tc.wantMsg)
			}
			if tc.wantHint != "" && !strings.Contains(cmdErr.Hint, tc.wantHint) {
				t.Errorf("hint = %q, want it to mention %q", cmdErr.Hint, tc.wantHint)
			}
		})
	}
}

func TestClickValidatesArguments(t *testing.T) {
	cases := []struct {
		name    string
		args    json.RawMessage
		wantMsg string
	}{
		{"no arguments", nil, "click: arguments are required"},
		{"missing element index", clickArgs("101", -100, 1, "left"), "click: an element index, a name:/id: reference or @x,y coordinates are required"},
		{"negative element index", clickArgs("101", -1, 1, "left"), "click: the element index must be a non-negative integer"},
		{"unknown button", clickArgs("101", 1, 1, "side"), "unsupported mouse_button: side"},
		{"click count zero-ish too high", clickArgs("101", 1, 4, "left"), "click_count must be 1, 2 or 3"},
		{"click count negative", clickArgs("101", 1, -1, "left"), "click_count must be 1, 2 or 3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := &actionSpy{}
			_, cmdErr := runCommand(t, CommandsWith(spy.services()), "click", tc.args)
			if cmdErr == nil || !strings.Contains(cmdErr.Msg, tc.wantMsg) {
				t.Fatalf("message = %v, want %q", cmdErr, tc.wantMsg)
			}
			if cmdErr.Exit != 1 {
				t.Errorf("exit = %d, want 1", cmdErr.Exit)
			}
			if got := spy.recorded(); len(got) != 0 {
				t.Errorf("argument failures must not touch the desktop: steps %v", got)
			}
		})
	}
	// Malformed JSON is refused, not panicked on.
	_, cmdErr := CommandsWith((&actionSpy{}).services())["click"](context.Background(), json.RawMessage(`not json`))
	if cmdErr == nil {
		t.Fatal("malformed arguments must be refused")
	}
}

func TestClickRightDoubleAndTripleNotSupported(t *testing.T) {
	cases := []struct {
		button   string
		count    int
		wantHint string
	}{
		{"right", 2, "right double click is not supported"},
		{"r", 3, "right triple click is not supported"},
		{"middle", 2, "middle double click is not supported"},
		{"m", 3, "middle triple click is not supported"},
	}
	for _, tc := range cases {
		spy := &actionSpy{}
		_, cmdErr := runCommand(t, CommandsWith(spy.services()), "click", clickArgs("101", 1, tc.count, tc.button))
		if cmdErr == nil || !strings.Contains(cmdErr.Msg, tc.wantHint) {
			t.Fatalf("button %s count %d: message = %v, want %q", tc.button, tc.count, cmdErr, tc.wantHint)
		}
		if containsStep(spy.recorded(), "activate") {
			t.Errorf("button %s count %d: validation must refuse before activation", tc.button, tc.count)
		}
	}
}

func TestClickWindowResolutionFailuresReuseResolveErrors(t *testing.T) {
	spy := &actionSpy{}
	_, cmdErr := runCommand(t, CommandsWith(spy.services()), "click", clickArgs("Paint", 1, 1, "left"))
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, `app "Paint" has no open Windows`) {
		t.Fatalf("message = %v", cmdErr)
	}
	_, cmdErr = runCommand(t, CommandsWith(spy.services()), "click", clickArgs("Chrome", 1, 1, "left"))
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, `app "Chrome" has 2 open Windows`) || !strings.Contains(cmdErr.Hint, "202\tChrome\tOne") {
		t.Fatalf("ambiguous app: %v", cmdErr)
	}
	if got := spy.recorded(); len(got) != 0 {
		t.Errorf("a window that does not resolve must not activate or inject: steps %v", got)
	}
}

func TestActivateWindowStandalone(t *testing.T) {
	spy := &actionSpy{}
	out, cmdErr := runCommand(t, CommandsWith(spy.services()), "activate-window", protocol.ActivateArgs{Window: "notepad"})
	if cmdErr != nil {
		t.Fatalf("activate-window: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want %q", out, "ok\n")
	}
	// The pipeline ran its checks and the activation; there is no further
	// element step to run.
	want := []string{"locked", "integrity", "activate"}
	if got := spy.recorded(); !equalStrings(got, want) {
		t.Errorf("pipeline steps = %v, want %v", got, want)
	}
}

func TestActivateWindowRefusesLikeActions(t *testing.T) {
	locked := &actionSpy{locked: true}
	_, cmdErr := runCommand(t, CommandsWith(locked.services()), "activate-window", protocol.ActivateArgs{Window: "101"})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "desktop is locked") {
		t.Fatalf("locked: %v", cmdErr)
	}
	admin := &actionSpy{refuse: &win.AdminWindowError{WindowID: 101, App: "Notepad.exe"}}
	_, cmdErr = runCommand(t, CommandsWith(admin.services()), "activate-window", protocol.ActivateArgs{Window: "101"})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "administrator") {
		t.Fatalf("admin: %v", cmdErr)
	}
	if containsStep(admin.recorded(), "activate") {
		t.Error("an administrator Window must be refused before activation")
	}
	// A missing window argument is a usage failure.
	_, cmdErr = runCommand(t, CommandsWith((&actionSpy{}).services()), "activate-window", protocol.ActivateArgs{})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "a window reference is required") {
		t.Fatalf("missing window: %v", cmdErr)
	}
}

func TestActionCommandsRegistered(t *testing.T) {
	cmds := Commands()
	for _, name := range []string{"click", "activate-window"} {
		if _, ok := cmds[name]; !ok {
			t.Errorf("%s missing from the command registry: %v", name, cmds)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsStep(steps []string, step string) bool {
	for _, s := range steps {
		if s == step {
			return true
		}
	}
	return false
}
