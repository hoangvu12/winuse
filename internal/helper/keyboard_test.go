package helper

// The keyboard Action commands' offline tests (ticket 06): the output
// contract (`ok` or a one-line failure with a hint, exit 1), the key-name
// failures (unknown, the forbidden Windows key, the empty key — all
// before the pipeline runs, so no Window is ever activated for a bad
// key), the pipeline's refusals reusing ticket 05's messages, and the
// argument validation. Everything Win32 is faked; no desktop is touched,
// no input is injected.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/hoangvu12/winuse/internal/keys"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// keySpy fakes the keyboard Action pipeline's Win32 steps and records the
// order they ran in, so tests can assert both the messages and "no input
// was sent".
type keySpy struct {
	mu sync.Mutex

	steps []string // ordered pipeline steps

	// configurable outcomes
	locked        bool
	lockedDesktop string
	refuse        error
	activateErr   error
	typeErr       error
	pressErr      error

	// recorded calls
	typedText   string
	stopped     func() bool // the Esc-stop checkpoint type-text was called with
	pressChords []keys.Chord
}

func (s *keySpy) step(name string) {
	s.mu.Lock()
	s.steps = append(s.steps, name)
	s.mu.Unlock()
}

func (s *keySpy) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.steps...)
}

// services builds Services wired to the spy over the standard fake
// Windows.
func (s *keySpy) services() Services {
	svc := testServices(&fakeTrees{})
	svc.Locked = func() (bool, string) { s.step("locked"); return s.locked, s.lockedDesktop }
	svc.Integrity = func(w win.WindowInfo) error { s.step("integrity"); return s.refuse }
	svc.Activate = func(hwnd uint64) error { s.step("activate"); return s.activateErr }
	svc.TypeText = func(text string, stopped func() bool) error {
		s.step("type")
		s.mu.Lock()
		s.typedText = text
		s.stopped = stopped
		err := s.typeErr
		s.mu.Unlock()
		return err
	}
	svc.PressKeys = func(chord keys.Chord) error {
		s.step("press")
		s.mu.Lock()
		s.pressChords = append(s.pressChords, chord)
		err := s.pressErr
		s.mu.Unlock()
		return err
	}
	return svc
}

func typeTextArgs(window, text string) json.RawMessage {
	raw, err := json.Marshal(protocol.TypeTextArgs{Window: window, Text: text})
	if err != nil {
		panic(err)
	}
	return raw
}

func pressKeyArgs(window, key string) json.RawMessage {
	raw, err := json.Marshal(protocol.PressKeyArgs{Window: window, Key: key})
	if err != nil {
		panic(err)
	}
	return raw
}

func TestTypeTextPrintsOK(t *testing.T) {
	spy := &keySpy{}
	out, cmdErr := runCommand(t, CommandsWith(spy.services()), "type-text", typeTextArgs("101", "Hello, 世界! 🎉"))
	if cmdErr != nil {
		t.Fatalf("type-text: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want %q", out, "ok\n")
	}
	// The pipeline order: checks, activation, then input.
	want := []string{"locked", "integrity", "activate", "type"}
	if got := spy.recorded(); !equalStrings(got, want) {
		t.Errorf("pipeline steps = %v, want %v", got, want)
	}
	if spy.typedText != "Hello, 世界! 🎉" {
		t.Errorf("typed text = %q, want the text forwarded verbatim", spy.typedText)
	}
}

func TestTypeTextRequiresTextBeforeThePipeline(t *testing.T) {
	// Empty text is a value failure, not a usage failure: it refuses
	// before anything touches the desktop. Missing arguments entirely is
	// the shared arguments-are-required refusal.
	cases := []struct {
		args    json.RawMessage
		wantMsg string
	}{
		{typeTextArgs("101", ""), "type-text: text is required"},
		{nil, "type-text: arguments are required"},
	}
	for _, tc := range cases {
		spy := &keySpy{}
		_, cmdErr := runCommand(t, CommandsWith(spy.services()), "type-text", tc.args)
		if cmdErr == nil || cmdErr.Msg != tc.wantMsg {
			t.Fatalf("args %s: message = %v, want %q", tc.args, cmdErr, tc.wantMsg)
		}
		if got := spy.recorded(); len(got) != 0 {
			t.Errorf("args %s: an argument failure must not touch the desktop: steps %v", tc.args, got)
		}
	}
}

func TestPressKeyPrintsOKAndForwardsTheChord(t *testing.T) {
	spy := &keySpy{}
	out, cmdErr := runCommand(t, CommandsWith(spy.services()), "press-key", pressKeyArgs("notepad", "Control_L+a"))
	if cmdErr != nil {
		t.Fatalf("press-key: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want %q", out, "ok\n")
	}
	want := []string{"locked", "integrity", "activate", "press"}
	if got := spy.recorded(); !equalStrings(got, want) {
		t.Errorf("pipeline steps = %v, want %v", got, want)
	}
	if len(spy.pressChords) != 1 || len(spy.pressChords[0]) != 2 {
		t.Fatalf("pressed %v, want one chord of two keys", spy.pressChords)
	}
	c := spy.pressChords[0]
	if c[0].VK != 0xA2 || c[1].VK != 0x41 {
		t.Errorf("chord = %s, want Control_L (0xA2) then a (0x41)", c)
	}
}

func TestPressKeyParsesBeforeThePipeline(t *testing.T) {
	// A bad key must fail before anything touches the desktop: no lock
	// check, no activation, and above all no injected input.
	cases := []struct {
		label    string
		key      string
		wantMsg  string
		wantHint string
	}{
		{"unknown key", "NotAKey", "unsupported key: NotAKey", "keysym"},
		{"unknown key in a chord", "Control_L+NotAKey", "unsupported key: NotAKey", "keysym"},
		{"too-high function key", "F21", "unsupported key: F21", "keysym"},
		{"windows key", "Win", "unsupported key: Win (the Windows key is never sent)", "never sends the Windows key"},
		{"windows key alias", "Meta_L", "unsupported key: Meta_L (the Windows key is never sent)", "never sends the Windows key"},
		{"windows key in a chord", "Win+e", "unsupported key: Win (the Windows key is never sent)", "never sends the Windows key"},
		{"empty key", "", "key is required", "Return, Tab"},
		{"blank key", "   ", "key is required", "Return, Tab"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			spy := &keySpy{}
			_, cmdErr := runCommand(t, CommandsWith(spy.services()), "press-key", pressKeyArgs("101", tc.key))
			if cmdErr == nil || cmdErr.Msg != tc.wantMsg {
				t.Fatalf("message = %v, want %q", cmdErr, tc.wantMsg)
			}
			if !contains(cmdErr.Hint, tc.wantHint) {
				t.Errorf("hint = %q, want it to mention %q", cmdErr.Hint, tc.wantHint)
			}
			if cmdErr.Exit != 1 {
				t.Errorf("exit = %d, want 1", cmdErr.Exit)
			}
			if got := spy.recorded(); len(got) != 0 {
				t.Errorf("a key-name failure must not touch the desktop: steps %v", got)
			}
			if len(spy.pressChords) != 0 {
				t.Errorf("a key-name failure must not inject input: pressed %v", spy.pressChords)
			}
		})
	}
}

func TestKeyboardCommandsReuseThePipelineRefusals(t *testing.T) {
	// The same refusal messages and ordering guarantees as click (ticket
	// 05): locked first, administrator second, never any input after a
	// refusal.
	locked := &keySpy{locked: true, lockedDesktop: "LockApp"}
	_, cmdErr := runCommand(t, CommandsWith(locked.services()), "type-text", typeTextArgs("101", "hi"))
	if cmdErr == nil || cmdErr.Msg != "Windows desktop is locked (input desktop is LockApp); unlock before using winuse" {
		t.Fatalf("locked type-text: %v", cmdErr)
	}
	locked = &keySpy{locked: true, lockedDesktop: "LockApp"}
	_, cmdErr = runCommand(t, CommandsWith(locked.services()), "press-key", pressKeyArgs("101", "Return"))
	if cmdErr == nil || !contains(cmdErr.Msg, "desktop is locked") {
		t.Fatalf("locked press-key: %v", cmdErr)
	}
	if got := locked.recorded(); !equalStrings(got, []string{"locked"}) {
		t.Errorf("steps = %v, want only the lock check", got)
	}

	admin := &keySpy{refuse: &win.AdminWindowError{WindowID: 101, App: "Notepad.exe"}}
	_, cmdErr = runCommand(t, CommandsWith(admin.services()), "type-text", typeTextArgs("101", "hi"))
	if cmdErr == nil || !contains(cmdErr.Msg, "administrator") {
		t.Fatalf("admin type-text: %v", cmdErr)
	}
	admin = &keySpy{refuse: &win.AdminWindowError{WindowID: 101, App: "Notepad.exe"}}
	_, cmdErr = runCommand(t, CommandsWith(admin.services()), "press-key", pressKeyArgs("101", "Return"))
	if cmdErr == nil || !contains(cmdErr.Msg, "administrator") {
		t.Fatalf("admin press-key: %v", cmdErr)
	}
	if got := admin.recorded(); !equalStrings(got, []string{"locked", "integrity"}) {
		t.Errorf("steps = %v, want no activation and no input", got)
	}

	// An activation failure keeps the output contract too.
	fg := &keySpy{activateErr: &win.NotForegroundError{WindowID: 101}}
	_, cmdErr = runCommand(t, CommandsWith(fg.services()), "press-key", pressKeyArgs("101", "Return"))
	if cmdErr == nil || cmdErr.Msg != "failed to activate window 101" {
		t.Fatalf("activation failure: %v", cmdErr)
	}
	if containsStep(fg.recorded(), "press") {
		t.Error("no input may be sent when activation failed")
	}
}

func TestKeyboardInputFailuresSurface(t *testing.T) {
	press := &keySpy{pressErr: errors.New("injecting the key press failed: SendInput rejected 1 of 2 events")}
	_, cmdErr := runCommand(t, CommandsWith(press.services()), "press-key", pressKeyArgs("101", "Control_L+a"))
	if cmdErr == nil || !contains(cmdErr.Msg, "injecting the key press failed") {
		t.Fatalf("message = %v", cmdErr)
	}
	typee := &keySpy{typeErr: errors.New("injecting the text failed: SendInput rejected 3 of 4 events")}
	_, cmdErr = runCommand(t, CommandsWith(typee.services()), "type-text", typeTextArgs("101", "hi"))
	if cmdErr == nil || !contains(cmdErr.Msg, "injecting the text failed") {
		t.Fatalf("message = %v", cmdErr)
	}
}

func TestKeyboardWindowResolutionFailures(t *testing.T) {
	// A Window that does not resolve reuses the shared resolve errors and
	// touches nothing.
	spy := &keySpy{}
	_, cmdErr := runCommand(t, CommandsWith(spy.services()), "type-text", typeTextArgs("Paint", "hi"))
	if cmdErr == nil || !contains(cmdErr.Msg, `app "Paint" has no open Windows`) {
		t.Fatalf("message = %v", cmdErr)
	}
	_, cmdErr = runCommand(t, CommandsWith(spy.services()), "press-key", pressKeyArgs("Paint", "Return"))
	if cmdErr == nil || !contains(cmdErr.Msg, `app "Paint" has no open Windows`) {
		t.Fatalf("message = %v", cmdErr)
	}
	if got := spy.recorded(); len(got) != 0 {
		t.Errorf("an unresolved window must not activate or inject: steps %v", got)
	}
}

func TestKeyboardArgumentsValidated(t *testing.T) {
	// Malformed JSON is refused, not panicked on; arguments are required.
	for _, cmd := range []string{"type-text", "press-key"} {
		_, cmdErr := CommandsWith((&keySpy{}).services())[cmd](context.Background(), json.RawMessage(`not json`))
		if cmdErr == nil {
			t.Fatalf("%s: malformed arguments must be refused", cmd)
		}
		if cmdErr.Exit != 1 {
			t.Errorf("%s: exit = %d, want 1", cmd, cmdErr.Exit)
		}
	}
}

func TestKeyboardCommandsRegistered(t *testing.T) {
	cmds := Commands()
	for _, name := range []string{"type-text", "press-key"} {
		if _, ok := cmds[name]; !ok {
			t.Errorf("%s missing from the command registry: %v", name, cmds)
		}
	}
}

func TestPressKeyAcceptsCodexDocumentedChords(t *testing.T) {
	// The chord shapes straight out of Codex's api.md and guidance.
	for _, key := range []string{
		"a", "space", "Return", "Tab", "Control_L+a", "Control_L+Shift_L+period",
		"KP_0", "Numpad_0", "Ctrl+a", "Alt_L+space", "Shift_L", "Down", "q",
		"Control_L+v", "period", "greater", "less", "comma", "slash", "question",
		"Numpad_Add", "Numpad_Subtract", "Numpad_Multiply", "Numpad_Divide",
		"Numpad_Decimal", "Numpad_Enter", "page-up", "PAGE_DOWN",
	} {
		spy := &keySpy{}
		_, cmdErr := runCommand(t, CommandsWith(spy.services()), "press-key", pressKeyArgs("101", key))
		if cmdErr != nil {
			t.Errorf("press-key %q: %v (must be accepted)", key, cmdErr.Msg)
			continue
		}
		if len(spy.pressChords) != 1 {
			t.Errorf("press-key %q: pressed %v", key, spy.pressChords)
		}
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func TestTypeTextStopsOnTheRequestDeadline(t *testing.T) {
	// Finding 10: typing's between-batches checkpoint also returns on the
	// request's deadline, so a timed-out request stops injecting text.
	// The failure reports the budget (the server's central timeout
	// policy), never a fake Esc stop; a real Esc press still wins.
	spy := &keySpy{}
	spy.typeErr = win.ErrEscStopped // typing aborted at its checkpoint

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the budget is already spent
	cmds := CommandsWith(spy.services())
	c := cmds["type-text"]
	_, cmdErr := c(ctx, typeTextArgs("101", "hello"))
	if cmdErr == nil {
		t.Fatal("typing past the request's deadline must fail")
	}
	if !strings.Contains(cmdErr.Msg, "the request's budget expired while typing") {
		t.Fatalf("message = %q, want the budget-expired failure", cmdErr.Msg)
	}
	if cmdErr.Exit == protocol.ExitEsc {
		t.Errorf("exit = 130, want 1: the deadline stopped the typing, not the user")
	}
	// The checkpoint type-text was called with reports the deadline.
	if spy.stopped == nil || !spy.stopped() {
		t.Error("the typing checkpoint must report the expired request deadline")
	}
}
