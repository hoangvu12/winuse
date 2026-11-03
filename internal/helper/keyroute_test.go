package helper

// The keyboard routes' offline tests (headless-parity spec, "Headless
// Return and Escape in classic dialogs"; ADR 0011, Decision 4): the pure
// dialog key decision, press-key's routing by a faked Window probe
// (Headless only for Return and Escape on a classic dialog, `headed`
// forcing v1), the refusals that say "nothing was sent", the Agent
// Cursor's travel to the button, and the Batch's scope picked with the
// probe. Everything Win32 is faked; no desktop is touched.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/keys"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// Faked dialog controls: an OK default button, a Cancel button, an edit.
var (
	okButton     = win.Control{HWND: 0x501, ID: 1, Class: "Button", Style: 0x1, Enabled: true, Rect: win.Rect{Left: 500, Top: 600, Right: 580, Bottom: 630}, Text: "&OK"}
	cancelButton = win.Control{HWND: 0x502, ID: win.IDCancel, Class: "Button", Enabled: true, Rect: win.Rect{Left: 600, Top: 600, Right: 680, Bottom: 630}, Text: "Cancel"}
	noButton     = win.Control{HWND: 0x503, ID: 7, Class: "Button", Style: 0x1, Enabled: true, Rect: win.Rect{Left: 700, Top: 600, Right: 780, Bottom: 630}, Text: "&No"}
	plainEdit    = win.Control{HWND: 0x504, ID: 1001, Class: "Edit", Enabled: true, Rect: win.Rect{Left: 50, Top: 60, Right: 450, Bottom: 90}}
)

// dialogTarget is a probe reading of a classic dialog with OK as its
// default button and the focus in a plain edit.
func dialogTarget() win.KeyTarget {
	return win.KeyTarget{
		Dialog:     true,
		HasDefault: true,
		DefaultID:  1,
		Default:    okButton,
		Cancel:     cancelButton,
		Focus:      plainEdit,
	}
}

// keyRouteSpy is a headlessSpy whose Window probe, dialog command and
// edit messages are faked. The probe is counted apart from the step log,
// so the v1 step lists stay comparable.
type keyRouteSpy struct {
	*headlessSpy
	target win.KeyTarget
	probes int

	posted     []postedCommand
	commandErr error

	// The edit control (ticket 06): states are returned in order (before,
	// after), stateErrs alongside; the insert is recorded.
	states     []win.EditState
	stateErrs  []error
	stateReads int
	inserted   []string
	insertedTo []uint64
	replaceErr error

	// Posted Keys (ticket 07): what was posted where; postErr fails after
	// postN messages.
	postedKeys  []string
	postedText  []string
	postTargets []uint64
	postN       int
	postErr     error
}

type postedCommand struct {
	dialog  uint64
	id      uint16
	control uint64
}

func newKeyRouteSpy(target win.KeyTarget) *keyRouteSpy {
	s := &keyRouteSpy{headlessSpy: newHeadlessSpy(), target: target}
	s.cursor.esc = s.esc
	s.svc.KeyRoute = func(hwnd uint64) win.KeyTarget {
		s.probes++
		return s.target
	}
	s.svc.DialogCommand = func(dialog uint64, id uint16, control uint64) error {
		s.step("dialog-command")
		s.posted = append(s.posted, postedCommand{dialog, id, control})
		s.esc.pressIf("act")
		return s.commandErr
	}
	s.svc.EditState = func(edit uint64) (win.EditState, error) {
		s.step("edit-state")
		i := s.stateReads
		s.stateReads++
		var st win.EditState
		var err error
		if i < len(s.states) {
			st = s.states[i]
		}
		if i < len(s.stateErrs) {
			err = s.stateErrs[i]
		}
		return st, err
	}
	s.svc.ReplaceSel = func(edit uint64, text string) error {
		s.step("replace-sel")
		s.inserted = append(s.inserted, text)
		s.insertedTo = append(s.insertedTo, edit)
		s.esc.pressIf("act")
		return s.replaceErr
	}
	s.svc.PostKeys = func(target uint64, chord keys.Chord) (int, error) {
		s.step("post-keys")
		s.postedKeys = append(s.postedKeys, chord.String())
		s.postTargets = append(s.postTargets, target)
		s.esc.pressIf("act")
		if s.postErr != nil {
			return s.postN, s.postErr
		}
		return 2 * len(chord), nil
	}
	s.svc.PostText = func(target uint64, text string) (int, error) {
		s.step("post-text")
		s.postedText = append(s.postedText, text)
		s.postTargets = append(s.postTargets, target)
		s.esc.pressIf("act")
		if s.postErr != nil {
			return s.postN, s.postErr
		}
		return len(text), nil
	}
	return s
}

func chordOf(t *testing.T, key string) keys.Chord {
	t.Helper()
	c, err := keys.Parse(key)
	if err != nil {
		t.Fatalf("keys.Parse(%q): %v", key, err)
	}
	return c
}

func TestDialogKeyRoute(t *testing.T) {
	withFocus := func(f win.Control) win.KeyTarget { d := dialogTarget(); d.Focus = f; return d }
	disabled := func(c win.Control) win.Control { c.Enabled = false; return c }
	cases := []struct {
		name    string
		key     string
		target  win.KeyTarget
		ok      bool
		id      uint16
		control win.Control
	}{
		{"Return presses the default button", "Return", dialogTarget(), true, 1, okButton},
		{"KP_Enter presses the default button", "KP_Enter", dialogTarget(), true, 1, okButton},
		{"Enter is Return", "enter", dialogTarget(), true, 1, okButton},
		{"Escape sends IDCANCEL with the Cancel button", "Escape", dialogTarget(), true, win.IDCancel, cancelButton},
		{"Escape with no Cancel control still sends IDCANCEL", "Escape",
			func() win.KeyTarget { d := dialogTarget(); d.Cancel = win.Control{}; return d }(), true, win.IDCancel, win.Control{}},
		{"Escape with a disabled Cancel button stays Headed", "Escape",
			func() win.KeyTarget { d := dialogTarget(); d.Cancel = disabled(cancelButton); return d }(), false, 0, win.Control{}},
		{"not a dialog", "Return", win.KeyTarget{Focus: plainEdit}, false, 0, win.Control{}},
		{"Return with no default button stays Headed", "Return",
			func() win.KeyTarget { d := dialogTarget(); d.HasDefault, d.Default = false, win.Control{}; return d }(), false, 0, win.Control{}},
		{"Return with a disabled default button stays Headed", "Return",
			func() win.KeyTarget { d := dialogTarget(); d.Default = disabled(okButton); return d }(), false, 0, win.Control{}},
		{"a default ID with no control still sends the ID", "Return",
			func() win.KeyTarget { d := dialogTarget(); d.Default = win.Control{}; return d }(), true, 1, win.Control{}},
		{"Return in a multi-line edit that wants Return stays Headed", "Return",
			withFocus(win.Control{HWND: 9, Class: "Edit", Style: win.ESMultiline | win.ESWantReturn, Enabled: true}), false, 0, win.Control{}},
		{"Return in a multi-line edit without ES_WANTRETURN presses the default", "Return",
			withFocus(win.Control{HWND: 9, Class: "Edit", Style: win.ESMultiline, Enabled: true}), true, 1, okButton},
		{"Escape in a multi-line edit that wants Return still cancels", "Escape",
			withFocus(win.Control{HWND: 9, Class: "Edit", Style: win.ESMultiline | win.ESWantReturn, Enabled: true}), true, win.IDCancel, cancelButton},
		{"Return on a focused push button presses that button", "Return", withFocus(noButton), true, 7, noButton},
		{"Return on the focused default button presses it", "Return", withFocus(okButton), true, 1, okButton},
		{"Return on a focused disabled push button stays Headed", "Return", withFocus(disabled(noButton)), false, 0, win.Control{}},
		{"a chord with a modifier stays Headed", "Control_L+Return", dialogTarget(), false, 0, win.Control{}},
		{"Shift+Escape stays Headed", "Shift_L+Escape", dialogTarget(), false, 0, win.Control{}},
		{"Tab stays Headed", "Tab", dialogTarget(), false, 0, win.Control{}},
		{"a letter stays Headed", "a", dialogTarget(), false, 0, win.Control{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dk, ok := dialogKeyRoute(chordOf(t, tc.key), tc.target)
			if ok != tc.ok {
				t.Fatalf("routed = %v, want %v (%+v)", ok, tc.ok, dk)
			}
			if !ok {
				return
			}
			if dk.id != tc.id || dk.button != tc.control {
				t.Errorf("route = id %d button %+v, want id %d button %+v", dk.id, dk.button, tc.id, tc.control)
			}
		})
	}
}

func TestMnemonicFree(t *testing.T) {
	for in, want := range map[string]string{
		"&OK":           "OK",
		"Save &As...":   "Save As...",
		"Fish && Chips": "Fish & Chips",
		"Cancel":        "Cancel",
		"":              "",
		"trailing&":     "trailing",
	} {
		if got := mnemonicFree(in); got != want {
			t.Errorf("mnemonicFree(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPressKeyReturnOnADialogIsHeadless(t *testing.T) {
	spy := newKeyRouteSpy(dialogTarget())
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "press-key", pressKeyArgs("101", "Return"))
	if cmdErr != nil {
		t.Fatalf("press-key: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want ok", out)
	}
	want := []string{"locked", "integrity", "ready", "cursor-travel", "dialog-command", "cursor-click"}
	if got := spy.recorded(); !equalStrings(got, want) {
		t.Errorf("steps = %v, want %v", got, want)
	}
	assertNoV1(t, spy.recorded())
	if len(spy.posted) != 1 || spy.posted[0] != (postedCommand{101, 1, okButton.HWND}) {
		t.Errorf("posted = %+v, want WM_COMMAND(1) to window 101 with the OK button", spy.posted)
	}
	if got := spy.esc.brackets(); !equalStrings(got, []string{"arm-passive", "disarm-passive"}) {
		t.Errorf("Esc brackets = %v, want one passive scope", got)
	}
	c := spy.cursor
	if len(c.labels) != 1 || c.labels[0] != `press Return "OK"` {
		t.Errorf("cursor labels = %q, want press Return \"OK\"", c.labels)
	}
	if len(c.targets) != 1 || c.targets[0] != okButton.Rect || len(c.clicks) != 1 || c.clicks[0] != okButton.Rect {
		t.Errorf("cursor targets %v clicks %v, want the OK button's rectangle", c.targets, c.clicks)
	}
	if spy.probes != 1 {
		t.Errorf("probed %d times, want once", spy.probes)
	}
}

func TestPressKeyEscapeOnADialogPressesCancel(t *testing.T) {
	spy := newKeyRouteSpy(dialogTarget())
	if _, cmdErr := runCommand(t, CommandsWith(spy.svc), "press-key", pressKeyArgs("101", "Escape")); cmdErr != nil {
		t.Fatalf("press-key: %v", cmdErr.Error())
	}
	if len(spy.posted) != 1 || spy.posted[0] != (postedCommand{101, win.IDCancel, cancelButton.HWND}) {
		t.Errorf("posted = %+v, want WM_COMMAND(IDCANCEL) with the Cancel button", spy.posted)
	}
	if c := spy.cursor; len(c.labels) != 1 || c.labels[0] != `press Escape "Cancel"` || c.targets[0] != cancelButton.Rect {
		t.Errorf("cursor labels %q targets %v, want the Cancel button", c.labels, c.targets)
	}

	// With no Cancel control the cursor goes to the dialog itself.
	d := dialogTarget()
	d.Cancel = win.Control{}
	spy = newKeyRouteSpy(d)
	if _, cmdErr := runCommand(t, CommandsWith(spy.svc), "press-key", pressKeyArgs("101", "Escape")); cmdErr != nil {
		t.Fatalf("press-key: %v", cmdErr.Error())
	}
	if len(spy.posted) != 1 || spy.posted[0] != (postedCommand{101, win.IDCancel, 0}) {
		t.Errorf("posted = %+v, want WM_COMMAND(IDCANCEL) with no control", spy.posted)
	}
	if c := spy.cursor; len(c.labels) != 1 || c.labels[0] != "press Escape" || c.targets[0] != fakeWindows()[0].Bounds {
		t.Errorf("cursor labels %q targets %v, want the Window's bounds", c.labels, c.targets)
	}
}

// TestPressKeyNeedsHeadedOffTheDialogRoute: off the dialog route a key
// has only the Headed route. Without --headed the route is read once and
// the key is refused with nothing sent (headless-max ticket 21); with it,
// the press is v1's and the probe is skipped.
func TestPressKeyNeedsHeadedOffTheDialogRoute(t *testing.T) {
	noDefault := dialogTarget()
	noDefault.HasDefault, noDefault.Default = false, win.Control{}
	wantsReturn := dialogTarget()
	wantsReturn.Focus = win.Control{HWND: 9, Class: "Edit", Style: win.ESMultiline | win.ESWantReturn, Enabled: true}
	cases := []struct {
		name   string
		target win.KeyTarget
		args   protocol.PressKeyArgs
		probes int // without --headed
	}{
		{"not a dialog", win.KeyTarget{}, protocol.PressKeyArgs{Window: "101", Key: "Return"}, 1},
		{"another key", dialogTarget(), protocol.PressKeyArgs{Window: "101", Key: "Tab"}, 1},
		{"a chord", dialogTarget(), protocol.PressKeyArgs{Window: "101", Key: "Alt_L+Return"}, 1},
		{"no default button", noDefault, protocol.PressKeyArgs{Window: "101", Key: "Return"}, 1},
		{"focus wants Return", wantsReturn, protocol.PressKeyArgs{Window: "101", Key: "Return"}, 1},
		{"headed", dialogTarget(), protocol.PressKeyArgs{Window: "101", Key: "Return", Headed: true}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.args.Headed {
				spy := newKeyRouteSpy(tc.target)
				out, cmdErr := runCommand(t, CommandsWith(spy.svc), "press-key", tc.args)
				wantHeadedRefusal(t, "press-key", out, cmdErr)
				if got := spy.recorded(); len(got) != 0 {
					t.Errorf("a refused press-key ran %v", got)
				}
				if len(spy.posted) != 0 {
					t.Errorf("posted %+v on a refused press-key", spy.posted)
				}
				if spy.probes != tc.probes {
					t.Errorf("probed %d times, want %d", spy.probes, tc.probes)
				}
				tc.args.Headed = true
			}
			spy := newKeyRouteSpy(tc.target)
			if _, cmdErr := runCommand(t, CommandsWith(spy.svc), "press-key", tc.args); cmdErr != nil {
				t.Fatalf("press-key: %v", cmdErr.Error())
			}
			if got, want := spy.recorded(), []string{"locked", "integrity", "activate", "press-key"}; !equalStrings(got, want) {
				t.Errorf("steps = %v, want v1's %v", got, want)
			}
			if len(spy.posted) != 0 {
				t.Errorf("posted %+v on a Headed route", spy.posted)
			}
			if spy.probes != 0 {
				t.Errorf("a headed press-key probed %d times, want 0", spy.probes)
			}
		})
	}
}

func TestPressKeyDialogRefusalsSayNothingWasSent(t *testing.T) {
	cases := []struct {
		name  string
		setup func(s *keyRouteSpy)
		want  string
	}{
		{"locked desktop", func(s *keyRouteSpy) { s.locked, s.lockedDesktop = true, "Winlogon" }, "locked"},
		{"administrator Window", func(s *keyRouteSpy) { s.refuse = &win.AdminWindowError{WindowID: 101} }, "administrator"},
		{"modal dialog", func(s *keyRouteSpy) { s.readyErr = &win.ModalDialogError{WindowID: 101, DialogID: 555} }, "modal dialog"},
		{"post failed", func(s *keyRouteSpy) { s.commandErr = errors.New("PostMessage(WM_COMMAND) failed") }, "PostMessage"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := newKeyRouteSpy(dialogTarget())
			tc.setup(spy)
			_, cmdErr := runCommand(t, CommandsWith(spy.svc), "press-key", pressKeyArgs("101", "Return"))
			if cmdErr == nil {
				t.Fatal("press-key succeeded, want a refusal")
			}
			if !strings.Contains(cmdErr.Msg, tc.want) || !strings.HasSuffix(cmdErr.Msg, nothingSentNote) {
				t.Errorf("message = %q, want %q and %q", cmdErr.Msg, tc.want, nothingSentNote)
			}
			assertNoV1(t, spy.recorded())
		})
	}
}

func TestPressKeyDialogEscStop(t *testing.T) {
	spy := newKeyRouteSpy(dialogTarget())
	spy.esc.pressOn = "travel"
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "press-key", pressKeyArgs("101", "Return"))
	if cmdErr == nil || cmdErr.Exit != protocol.ExitEsc || !strings.Contains(cmdErr.Hint, "nothing was sent") {
		t.Fatalf("err = %v, want the Esc stop before the post", cmdErr)
	}
	if len(spy.posted) != 0 {
		t.Errorf("posted %+v after an Esc stop during the travel", spy.posted)
	}

	spy = newKeyRouteSpy(dialogTarget())
	spy.esc.pressOn = "act"
	_, cmdErr = runCommand(t, CommandsWith(spy.svc), "press-key", pressKeyArgs("101", "Return"))
	if cmdErr == nil || cmdErr.Exit != protocol.ExitEsc || strings.Contains(cmdErr.Hint, "nothing was sent") {
		t.Fatalf("err = %v, want the plain Esc stop after the post", cmdErr)
	}
}

func TestHeadlessStepKeyRoutes(t *testing.T) {
	raw := func(v any) json.RawMessage { return batchStepArgs(v) }
	d := dialogTarget()
	cases := []struct {
		name  string
		kind  string
		args  json.RawMessage
		route *win.KeyTarget
		want  bool
	}{
		{"no probe", "press-key", raw(protocol.PressKeyArgs{Key: "Return"}), nil, false},
		{"Return on a dialog", "press-key", raw(protocol.PressKeyArgs{Key: "Return"}), &d, true},
		{"Escape on a dialog", "press-key", raw(protocol.PressKeyArgs{Key: "Escape"}), &d, true},
		{"headed", "press-key", raw(protocol.PressKeyArgs{Key: "Return", Headed: true}), &d, false},
		{"not a dialog", "press-key", raw(protocol.PressKeyArgs{Key: "Return"}), &win.KeyTarget{}, false},
		{"an unknown key", "press-key", raw(protocol.PressKeyArgs{Key: "Fly"}), &d, false},
		{"bad arguments", "press-key", json.RawMessage(`not json`), &d, false},
	}
	for _, tc := range cases {
		if got := headlessStep(tc.kind, tc.args, tc.route); got != tc.want {
			t.Errorf("%s: headlessStep = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestBatchPicksItsScopeWithTheKeyProbe(t *testing.T) {
	// Return on a dialog is a Headless step: with a Headless click the
	// Batch is one passive scope with no banner.
	spy := newKeyRouteSpy(dialogTarget())
	ov := &overlaySpy{share: spy.step}
	spy.svc.Overlay = ov
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
		Window: "101",
		Steps: []protocol.Step{
			stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(2)}),
			stepOf("press-key", protocol.PressKeyArgs{Key: "Return"}),
		},
	})
	if cmdErr != nil || out != "Action 1 (click): ok\nAction 2 (press-key): ok\n" {
		t.Fatalf("batch: %q, %v", out, cmdErr)
	}
	assertNoV1(t, spy.recorded())
	if b := spy.esc.brackets(); containsStep(b, "arm") || len(b) == 0 || b[0] != "arm-passive" {
		t.Errorf("Esc brackets = %v, want the passive scope", b)
	}
	// Once to pick the scope, once when the step routes.
	if spy.probes != 2 {
		t.Errorf("probed %d times, want 2", spy.probes)
	}

	// The same Batch on a Window that is no dialog: press-key has only the
	// Headed route. Without --headed it is refused at its turn with nothing
	// sent (headless-max ticket 21), so the Batch stays one passive scope
	// with no banner.
	spy = newKeyRouteSpy(win.KeyTarget{})
	ov = &overlaySpy{share: spy.step}
	spy.svc.Overlay = ov
	_, cmdErr = runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
		Window: "101",
		Steps: []protocol.Step{
			stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(2)}),
			stepOf("press-key", protocol.PressKeyArgs{Key: "Return"}),
		},
	})
	if msg, hint, ok := failedStep(cmdErr, 2, "press-key"); !ok || !strings.HasSuffix(msg, "; nothing was sent") || !strings.Contains(hint, "winuse --headed ") {
		t.Errorf("unheaded press-key step: %+v, want the --headed refusal", cmdErr)
	}
	assertNoV1(t, spy.recorded())
	if b := spy.esc.brackets(); containsStep(b, "arm") || len(b) == 0 || b[0] != "arm-passive" {
		t.Errorf("Esc brackets = %v, want the passive scope", b)
	}

	// Sent with `headed`, the press-key step is Headed, so the whole Batch
	// swallows Esc under the banner.
	spy = newKeyRouteSpy(win.KeyTarget{})
	ov = &overlaySpy{share: spy.step}
	spy.svc.Overlay = ov
	if _, cmdErr := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
		Window: "101",
		Steps: []protocol.Step{
			stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(2)}),
			stepOf("press-key", protocol.PressKeyArgs{Key: "Return", Headed: true}),
		},
	}); cmdErr != nil {
		t.Fatalf("batch: %v", cmdErr.Error())
	}
	steps := spy.recorded()
	if b := spy.esc.brackets(); len(b) == 0 || b[0] != "arm" {
		t.Errorf("Esc brackets = %v, want v1's swallowing scope", b)
	}
	if len(steps) == 0 || !strings.HasPrefix(steps[0], "overlay-show(") || !containsStep(steps, "press-key") {
		t.Errorf("steps = %v, want the banner and a Headed press-key", steps)
	}

	// A headed Batch injects headed into press-key and never probes.
	spy = newKeyRouteSpy(dialogTarget())
	if _, cmdErr := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
		Window: "101",
		Headed: true,
		Steps:  []protocol.Step{stepOf("press-key", protocol.PressKeyArgs{Key: "Return"})},
	}); cmdErr != nil {
		t.Fatalf("headed batch: %v", cmdErr.Error())
	}
	if got, want := spy.recorded(), []string{"locked", "integrity", "activate", "press-key"}; !equalStrings(got, want) {
		t.Errorf("steps = %v, want %v", got, want)
	}
	if spy.probes != 0 {
		t.Errorf("a headed Batch probed %d times, want 0", spy.probes)
	}
}

// editTarget is a probe reading of a Window whose focus is a writable
// classic edit control.
func editTarget(edit win.Control) win.KeyTarget {
	return win.KeyTarget{Focus: edit}
}

func TestInsertLengthOK(t *testing.T) {
	sel := func(length, start, end int) win.EditState {
		return win.EditState{Length: length, SelStart: start, SelEnd: end, SelKnown: true}
	}
	unknown := win.EditState{Length: 70000}
	cases := []struct {
		name   string
		before win.EditState
		after  int
		units  int
		want   bool
	}{
		{"insert at the caret", sel(10, 4, 4), 15, 5, true},
		{"insert into an empty control", sel(0, 0, 0), 3, 3, true},
		{"replace a selection", sel(10, 2, 6), 11, 5, true},
		{"replace everything", sel(10, 0, 10), 2, 2, true},
		{"a reversed selection counts the same", sel(10, 6, 2), 11, 5, true},
		{"nothing arrived", sel(10, 4, 4), 10, 5, false},
		{"truncated by the control's limit", sel(10, 4, 4), 12, 5, false},
		{"more than the text", sel(10, 4, 4), 16, 5, false},
		{"the selection was not replaced", sel(10, 2, 6), 15, 5, false},
		{"unknown selection: grown by the text", unknown, 70005, 5, true},
		{"unknown selection: the selection replaced", unknown, 69000, 5, true},
		{"unknown selection: grown past the text", unknown, 70006, 5, false},
		{"unknown selection: shorter than the text", unknown, 4, 5, false},
	}
	for _, tc := range cases {
		if got := insertLengthOK(tc.before, tc.after, tc.units); got != tc.want {
			t.Errorf("%s: insertLengthOK = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestEditTypeRoute(t *testing.T) {
	readOnly := plainEdit
	readOnly.Style = win.ESReadOnly
	disabled := plainEdit
	disabled.Enabled = false
	password := plainEdit
	password.Style = win.ESPassword
	cases := []struct {
		name   string
		text   string
		target win.KeyTarget
		want   bool
	}{
		{"plain text into an edit", "hello world", editTarget(plainEdit), true},
		{"any script and emoji", "Xin chào 世界 🎉", editTarget(plainEdit), true},
		{"a password edit", "s3cret", editTarget(password), true},
		{"a dialog's edit", "x", dialogTarget(), true},
		{"a newline routes Headed", "a\nb", editTarget(plainEdit), false},
		{"a carriage return routes Headed", "a\rb", editTarget(plainEdit), false},
		{"a tab routes Headed", "a\tb", editTarget(plainEdit), false},
		{"another C0 character routes Headed", "a\x1bb", editTarget(plainEdit), false},
		{"NUL routes Headed", "a\x00b", editTarget(plainEdit), false},
		{"a read-only edit", "x", editTarget(readOnly), false},
		{"a disabled edit", "x", editTarget(disabled), false},
		{"no focus", "x", win.KeyTarget{}, false},
		{"a non-edit focus", "x", editTarget(okButton), false},
		{"empty text", "", editTarget(plainEdit), false},
	}
	for _, tc := range cases {
		edit, ok := editTypeRoute(tc.text, tc.target)
		if ok != tc.want {
			t.Errorf("%s: routed = %v, want %v", tc.name, ok, tc.want)
			continue
		}
		if ok && edit != tc.target.Focus {
			t.Errorf("%s: edit = %+v, want the focused control", tc.name, edit)
		}
	}
}

func TestTypeTextIntoAnEditIsHeadless(t *testing.T) {
	spy := newKeyRouteSpy(editTarget(plainEdit))
	spy.states = []win.EditState{
		{Length: 5, SelStart: 5, SelEnd: 5, SelKnown: true},
		{Length: 10, SelStart: 10, SelEnd: 10, SelKnown: true},
	}
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "type-text", typeTextArgs("101", "世界 🎉"))
	if cmdErr != nil {
		t.Fatalf("type-text: %v", cmdErr.Error())
	}
	if out != "ok\n" {
		t.Errorf("output = %q, want ok", out)
	}
	want := []string{"locked", "integrity", "ready", "cursor-travel", "edit-state", "replace-sel", "edit-state", "cursor-click"}
	if got := spy.recorded(); !equalStrings(got, want) {
		t.Errorf("steps = %v, want %v", got, want)
	}
	assertNoV1(t, spy.recorded())
	if len(spy.inserted) != 1 || spy.inserted[0] != "世界 🎉" || spy.insertedTo[0] != plainEdit.HWND {
		t.Errorf("inserted %q into %v, want the text verbatim into the edit", spy.inserted, spy.insertedTo)
	}
	if got := spy.esc.brackets(); !equalStrings(got, []string{"arm-passive", "disarm-passive"}) {
		t.Errorf("Esc brackets = %v, want one passive scope", got)
	}
	c := spy.cursor
	if len(c.labels) != 1 || c.labels[0] != "type" || c.targets[0] != plainEdit.Rect {
		t.Errorf("cursor labels %q targets %v, want type at the edit", c.labels, c.targets)
	}
}

// TestTypeTextNeedsHeadedOffTheEditRoute: off the edit route text has only
// the Headed route. Without --headed the route is read once and the text
// is refused with nothing sent (headless-max ticket 21); with it, the
// typing is v1's and the probe is skipped.
func TestTypeTextNeedsHeadedOffTheEditRoute(t *testing.T) {
	cases := []struct {
		name   string
		target win.KeyTarget
		args   protocol.TypeTextArgs
		probes int // without --headed
	}{
		{"no edit focus", win.KeyTarget{}, protocol.TypeTextArgs{Window: "101", Text: "hi"}, 1},
		{"a newline", editTarget(plainEdit), protocol.TypeTextArgs{Window: "101", Text: "hi\nthere"}, 1},
		{"a tab", editTarget(plainEdit), protocol.TypeTextArgs{Window: "101", Text: "a\tb"}, 1},
		{"headed", editTarget(plainEdit), protocol.TypeTextArgs{Window: "101", Text: "hi", Headed: true}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.args.Headed {
				spy := newKeyRouteSpy(tc.target)
				out, cmdErr := runCommand(t, CommandsWith(spy.svc), "type-text", tc.args)
				wantHeadedRefusal(t, "type-text", out, cmdErr)
				if got := spy.recorded(); len(got) != 0 {
					t.Errorf("a refused type-text ran %v", got)
				}
				if len(spy.inserted) != 0 {
					t.Errorf("inserted %q on a refused type-text", spy.inserted)
				}
				if spy.probes != tc.probes {
					t.Errorf("probed %d times, want %d", spy.probes, tc.probes)
				}
				tc.args.Headed = true
			}
			spy := newKeyRouteSpy(tc.target)
			if _, cmdErr := runCommand(t, CommandsWith(spy.svc), "type-text", tc.args); cmdErr != nil {
				t.Fatalf("type-text: %v", cmdErr.Error())
			}
			if got, want := spy.recorded(), []string{"locked", "integrity", "activate", "type-text"}; !equalStrings(got, want) {
				t.Errorf("steps = %v, want v1's %v", got, want)
			}
			if len(spy.inserted) != 0 {
				t.Errorf("inserted %q on a Headed route", spy.inserted)
			}
			if spy.probes != 0 {
				t.Errorf("a headed type-text probed %d times, want 0", spy.probes)
			}
		})
	}
}

func TestTypeTextIntoAnEditFailureWording(t *testing.T) {
	const secret = "hunter2-secret"
	password := plainEdit
	password.Style = win.ESPassword
	before := win.EditState{Length: 0, SelKnown: true}
	cases := []struct {
		name        string
		setup       func(s *keyRouteSpy)
		nothingSent bool
		hint        string
		inserted    bool
	}{
		{"the state before cannot be read", func(s *keyRouteSpy) {
			s.stateErrs = []error{errors.New("reading the length of edit control 1284 failed")}
		}, true, "", false},
		{"the insert failed", func(s *keyRouteSpy) {
			s.states = []win.EditState{before}
			s.replaceErr = errors.New("EM_REPLACESEL into edit control 1284 failed: timeout")
		}, false, "may have taken effect", true},
		{"the length does not add up", func(s *keyRouteSpy) {
			s.states = []win.EditState{before, {Length: 3}}
		}, false, "delivered; do not repeat", true},
		{"the state after cannot be read", func(s *keyRouteSpy) {
			s.states = []win.EditState{before}
			s.stateErrs = []error{nil, errors.New("gone")}
		}, false, "delivered; do not repeat", true},
		{"administrator Window", func(s *keyRouteSpy) { s.refuse = &win.AdminWindowError{WindowID: 101} }, true, "", false},
		{"locked desktop", func(s *keyRouteSpy) { s.locked, s.lockedDesktop = true, "Winlogon" }, true, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := newKeyRouteSpy(editTarget(password))
			tc.setup(spy)
			_, cmdErr := runCommand(t, CommandsWith(spy.svc), "type-text", typeTextArgs("101", secret))
			if cmdErr == nil {
				t.Fatal("type-text succeeded, want a failure")
			}
			if got := strings.HasSuffix(cmdErr.Msg, nothingSentNote); got != tc.nothingSent {
				t.Errorf("message = %q, nothing-sent note %v, want %v", cmdErr.Msg, got, tc.nothingSent)
			}
			if tc.hint != "" && !strings.Contains(cmdErr.Hint, tc.hint) {
				t.Errorf("hint = %q, want %q", cmdErr.Hint, tc.hint)
			}
			if strings.Contains(cmdErr.Msg+cmdErr.Hint, secret) {
				t.Errorf("the failure echoes the password: %q / %q", cmdErr.Msg, cmdErr.Hint)
			}
			if got := len(spy.inserted) > 0; got != tc.inserted {
				t.Errorf("inserted = %v, want %v", got, tc.inserted)
			}
			for _, l := range spy.cursor.labels {
				if strings.Contains(l, secret) {
					t.Errorf("the cursor label shows the text: %q", l)
				}
			}
			assertNoV1(t, spy.recorded())
		})
	}
}

func TestHeadlessStepTypeText(t *testing.T) {
	raw := func(v any) json.RawMessage { return batchStepArgs(v) }
	e := editTarget(plainEdit)
	cases := []struct {
		name  string
		args  json.RawMessage
		route *win.KeyTarget
		want  bool
	}{
		{"no probe", raw(protocol.TypeTextArgs{Text: "hi"}), nil, false},
		{"an edit focus", raw(protocol.TypeTextArgs{Text: "hi"}), &e, true},
		{"headed", raw(protocol.TypeTextArgs{Text: "hi", Headed: true}), &e, false},
		{"a control character", raw(protocol.TypeTextArgs{Text: "hi\n"}), &e, false},
		{"no edit focus", raw(protocol.TypeTextArgs{Text: "hi"}), &win.KeyTarget{}, false},
		{"bad arguments", json.RawMessage(`not json`), &e, false},
	}
	for _, tc := range cases {
		if got := headlessStep("type-text", tc.args, tc.route); got != tc.want {
			t.Errorf("%s: headlessStep = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestBatchTypeTextIntoAnEditKeepsTheHeadlessScope(t *testing.T) {
	spy := newKeyRouteSpy(editTarget(plainEdit))
	spy.states = []win.EditState{{SelKnown: true}, {Length: 5}}
	if _, cmdErr := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
		Window: "101",
		Steps: []protocol.Step{
			stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(2)}),
			stepOf("type-text", protocol.TypeTextArgs{Text: "hello"}),
		},
	}); cmdErr != nil {
		t.Fatalf("batch: %v", cmdErr.Error())
	}
	assertNoV1(t, spy.recorded())
	if b := spy.esc.brackets(); containsStep(b, "arm") || len(b) == 0 || b[0] != "arm-passive" {
		t.Errorf("Esc brackets = %v, want the passive scope", b)
	}
	if len(spy.inserted) != 1 || spy.inserted[0] != "hello" {
		t.Errorf("inserted %q, want hello", spy.inserted)
	}

	// Text with a newline has only the Headed route. Without --headed the
	// step is refused with nothing sent (headless-max ticket 21), so the
	// scope stays passive.
	spy = newKeyRouteSpy(editTarget(plainEdit))
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
		Window: "101",
		Steps:  []protocol.Step{stepOf("type-text", protocol.TypeTextArgs{Text: "a\nb"})},
	})
	if msg, hint, ok := failedStep(cmdErr, 1, "type-text"); !ok || !strings.HasSuffix(msg, "; nothing was sent") || !strings.Contains(hint, "winuse --headed ") {
		t.Errorf("unheaded newline step: %+v, want the --headed refusal", cmdErr)
	}
	if got := spy.recorded(); len(got) != 0 {
		t.Errorf("a refused type-text ran %v", got)
	}
	if b := spy.esc.brackets(); containsStep(b, "arm") || len(b) == 0 || b[0] != "arm-passive" {
		t.Errorf("Esc brackets = %v, want the passive scope", b)
	}

	// Sent with `headed` it is a Headed step: the Batch swallows Esc.
	spy = newKeyRouteSpy(editTarget(plainEdit))
	if _, cmdErr := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
		Window: "101",
		Steps:  []protocol.Step{stepOf("type-text", protocol.TypeTextArgs{Text: "a\nb", Headed: true})},
	}); cmdErr != nil {
		t.Fatalf("batch: %v", cmdErr.Error())
	}
	if b := spy.esc.brackets(); len(b) == 0 || b[0] != "arm" {
		t.Errorf("Esc brackets = %v, want v1's swallowing scope", b)
	}
}

// postedOK is the output of every Posted Keys success.
const postedOK = "ok\nNote: posted; the app may ignore posted keys, read the Window State to check\n"

// postedKeysStep is a Posted Keys success as a Batch step's lines.
const postedKeysStep = "ok\n  Note: posted; the app may ignore posted keys, read the Window State to check\n"

func TestPressKeyPostPostsToTheFocusedControl(t *testing.T) {
	spy := newKeyRouteSpy(editTarget(plainEdit))
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "press-key", protocol.PressKeyArgs{Window: "101", Key: "Return", Post: true})
	if cmdErr != nil {
		t.Fatalf("press-key --post: %v", cmdErr.Error())
	}
	if out != postedOK {
		t.Errorf("output = %q, want %q", out, postedOK)
	}
	// The Ready check runs too: a posted key must not land in an owner disabled behind its modal dialog.
	want := []string{"locked", "integrity", "ready", "cursor-travel", "post-keys", "cursor-click"}
	if got := spy.recorded(); !equalStrings(got, want) {
		t.Errorf("steps = %v, want %v", got, want)
	}
	assertNoV1(t, spy.recorded())
	if len(spy.postedKeys) != 1 || spy.postedKeys[0] != "Return" || spy.postTargets[0] != plainEdit.HWND {
		t.Errorf("posted %q to %v, want Return to the focused edit", spy.postedKeys, spy.postTargets)
	}
	if got := spy.esc.brackets(); !equalStrings(got, []string{"arm-passive", "disarm-passive"}) {
		t.Errorf("Esc brackets = %v, want one passive scope", got)
	}
	if c := spy.cursor; len(c.labels) != 1 || c.labels[0] != "press Return" || c.targets[0] != plainEdit.Rect {
		t.Errorf("cursor labels %q targets %v, want press Return at the focused control", c.labels, c.targets)
	}

	// With no focused control inside the Window, the keys go to the Window
	// itself and the cursor to its bounds.
	spy = newKeyRouteSpy(win.KeyTarget{})
	if _, cmdErr := runCommand(t, CommandsWith(spy.svc), "press-key", protocol.PressKeyArgs{Window: "101", Key: "Tab", Post: true}); cmdErr != nil {
		t.Fatalf("press-key --post: %v", cmdErr.Error())
	}
	if len(spy.postTargets) != 1 || spy.postTargets[0] != 101 {
		t.Errorf("posted to %v, want the Window 101", spy.postTargets)
	}
	if c := spy.cursor; len(c.targets) != 1 || c.targets[0] != fakeWindows()[0].Bounds {
		t.Errorf("cursor targets %v, want the Window's bounds", c.targets)
	}
}

func TestTypeTextPostPostsCharacters(t *testing.T) {
	spy := newKeyRouteSpy(editTarget(plainEdit))
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "type-text", protocol.TypeTextArgs{Window: "101", Text: "hi\nthere", Post: true})
	if cmdErr != nil {
		t.Fatalf("type-text --post: %v", cmdErr.Error())
	}
	if out != postedOK {
		t.Errorf("output = %q, want %q", out, postedOK)
	}
	want := []string{"locked", "integrity", "ready", "cursor-travel", "post-text", "cursor-click"}
	if got := spy.recorded(); !equalStrings(got, want) {
		t.Errorf("steps = %v, want %v", got, want)
	}
	if len(spy.postedText) != 1 || spy.postedText[0] != "hi\nthere" || spy.postTargets[0] != plainEdit.HWND {
		t.Errorf("posted %q to %v, want the text to the focused edit", spy.postedText, spy.postTargets)
	}
	if len(spy.inserted) != 0 {
		t.Errorf("--post must not take the EM_REPLACESEL route: inserted %q", spy.inserted)
	}
	if c := spy.cursor; len(c.labels) != 1 || c.labels[0] != "type" {
		t.Errorf("cursor labels %q, want type", c.labels)
	}
}

// chordSpy fakes PostChord on a keyRouteSpy and records each plan.
func chordSpy(target win.KeyTarget, err error) (*keyRouteSpy, *[]win.ChordPlan, *[][2]uint64) {
	spy := newKeyRouteSpy(target)
	var plans []win.ChordPlan
	var to [][2]uint64
	spy.svc.PostChord = func(top, target uint64, plan win.ChordPlan) (int, error) {
		spy.step("post-chord")
		plans = append(plans, plan)
		to = append(to, [2]uint64{top, target})
		if err != nil {
			return 0, err
		}
		return 2, nil
	}
	return spy, &plans, &to
}

func TestPostChordsThroughAttachedKeyState(t *testing.T) {
	// headless-max ticket 07: a chord with Control, Shift or Alt is posted
	// through attached key state to the focused control, as Posted Keys.
	for _, c := range []struct {
		key string
		vk  uint16
		sys bool
	}{{"Control_L+a", 'A', false}, {"Shift_L+Tab", 0x09, false}, {"Alt_L+F5", 0x74, true}, {"a+Control_R", 'A', false}} {
		spy, plans, to := chordSpy(editTarget(plainEdit), nil)
		out, cmdErr := runCommand(t, CommandsWith(spy.svc), "press-key", protocol.PressKeyArgs{Window: "101", Key: c.key, Post: true})
		if cmdErr != nil || out != postedOutput {
			t.Fatalf("%s: %q, %v, want the posted output", c.key, out, cmdErr)
		}
		if len(*plans) != 1 || (*plans)[0].Key.VK != c.vk || (*plans)[0].Sys != c.sys || (*plans)[0].Activate != c.sys {
			t.Errorf("%s: plans = %+v", c.key, *plans)
		}
		if (*to)[0] != [2]uint64{101, plainEdit.HWND} {
			t.Errorf("%s: posted to %v, want window 101's focused edit", c.key, (*to)[0])
		}
		if len(spy.postedKeys) != 0 {
			t.Errorf("%s: the plain Posted Keys route ran too: %v", c.key, spy.postedKeys)
		}
		assertNoV1(t, spy.recorded())
	}
}

func TestPostChordRefusals(t *testing.T) {
	// Chords the route cannot post are refused before anything is touched.
	for key, want := range map[string]string{
		"Control_L+Alt_L+x": "Control and Alt together",
		"Control_L":         "only modifiers",
		"Alt_L+F4":          "press-key refuses Alt_L+F4", // the Blocked Combo refusal; with --force, blockedcombos_test
	} {
		spy, plans, _ := chordSpy(editTarget(plainEdit), nil)
		_, cmdErr := runCommand(t, CommandsWith(spy.svc), "press-key", protocol.PressKeyArgs{Window: "101", Key: key, Post: true})
		if cmdErr == nil || !strings.Contains(cmdErr.Msg, want) || !strings.HasSuffix(cmdErr.Msg, nothingSentNote) {
			t.Errorf("%s: err = %v, want %q and nothing sent", key, cmdErr, want)
		}
		if len(*plans) != 0 || len(spy.recorded()) != 0 {
			t.Errorf("%s: plans %v steps %v, want nothing touched", key, *plans, spy.recorded())
		}
	}
	// The thread that owns the foreground: nothing sent, the --headed way.
	spy, _, _ := chordSpy(editTarget(plainEdit), win.ErrChordForeground)
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "press-key", protocol.PressKeyArgs{Window: "101", Key: "Control_L+s", Post: true})
	if cmdErr == nil || !strings.HasSuffix(cmdErr.Msg, nothingSentNote) || !strings.Contains(cmdErr.Hint, "winuse --headed press-key 101 Control_L+s") {
		t.Errorf("foreground thread: %v, want nothing sent and the --headed press", cmdErr)
	}
}

func TestPostRefusalsAndFailures(t *testing.T) {
	cases := []struct {
		name        string
		setup       func(s *keyRouteSpy)
		nothingSent bool
		hint        string
	}{
		{"administrator Window", func(s *keyRouteSpy) { s.refuse = &win.AdminWindowError{WindowID: 101} }, true, "elevated"},
		{"locked desktop", func(s *keyRouteSpy) { s.locked, s.lockedDesktop = true, "Winlogon" }, true, ""},
		{"nothing could be posted", func(s *keyRouteSpy) { s.postErr = errors.New("PostMessage(0x100) failed") }, true, ""},
		{"a partial post", func(s *keyRouteSpy) { s.postN, s.postErr = 1, errors.New("PostMessage(0x101) failed") }, false, "may have taken effect"},
	}
	for _, tc := range cases {
		for _, kind := range []string{"press-key", "type-text"} {
			t.Run(tc.name+"/"+kind, func(t *testing.T) {
				spy := newKeyRouteSpy(editTarget(plainEdit))
				tc.setup(spy)
				var args any = protocol.PressKeyArgs{Window: "101", Key: "Return", Post: true}
				if kind == "type-text" {
					args = protocol.TypeTextArgs{Window: "101", Text: "hello", Post: true}
				}
				_, cmdErr := runCommand(t, CommandsWith(spy.svc), kind, args)
				if cmdErr == nil {
					t.Fatal("succeeded, want a failure")
				}
				if got := strings.HasSuffix(cmdErr.Msg, nothingSentNote); got != tc.nothingSent {
					t.Errorf("message = %q, nothing-sent note %v, want %v", cmdErr.Msg, got, tc.nothingSent)
				}
				if tc.hint != "" && !strings.Contains(cmdErr.Hint, tc.hint) {
					t.Errorf("hint = %q, want %q", cmdErr.Hint, tc.hint)
				}
				assertNoV1(t, spy.recorded())
			})
		}
	}
}

func TestPostWithHeadedIsAUsageFailure(t *testing.T) {
	for kind, args := range map[string]any{
		"press-key": protocol.PressKeyArgs{Window: "101", Key: "Return", Post: true, Headed: true},
		"type-text": protocol.TypeTextArgs{Window: "101", Text: "hi", Post: true, Headed: true},
	} {
		spy := newKeyRouteSpy(editTarget(plainEdit))
		_, cmdErr := runCommand(t, CommandsWith(spy.svc), kind, args)
		if cmdErr == nil || !strings.Contains(cmdErr.Msg, "--post and --headed") {
			t.Errorf("%s: err = %v, want the --post and --headed refusal", kind, cmdErr)
		}
		if got := spy.recorded(); len(got) != 0 {
			t.Errorf("%s: steps = %v, want nothing touched", kind, got)
		}
	}
}

// TestBatchRefusesPostWithHeadedBeforeAnyStep: a posted step that is
// headed, by its own flag or the Batch's, fails while the Batch is
// prepared, so the steps before it never act.
func TestBatchRefusesPostWithHeadedBeforeAnyStep(t *testing.T) {
	cases := []struct {
		name   string
		headed bool
		step   protocol.Step
	}{
		{"step headed", false, stepOf("type-text", protocol.TypeTextArgs{Text: "hi", Post: true, Headed: true})},
		{"batch headed", true, stepOf("type-text", protocol.TypeTextArgs{Text: "hi", Post: true})},
		{"batch headed press-key", true, stepOf("press-key", protocol.PressKeyArgs{Key: "Tab", Post: true})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := newKeyRouteSpy(editTarget(plainEdit))
			_, cmdErr := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
				Window: "101",
				Headed: tc.headed,
				Steps: []protocol.Step{
					stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(2)}),
					tc.step,
				},
			})
			if cmdErr == nil || !strings.Contains(cmdErr.Msg, "Action 2") || !strings.Contains(cmdErr.Msg, "--post and --headed cannot be combined") {
				t.Fatalf("err = %v, want Action 2's --post and --headed refusal", cmdErr)
			}
			if got := spy.recorded(); len(got) != 0 {
				t.Errorf("steps = %v, want nothing run", got)
			}
			if spy.probes != 0 {
				t.Errorf("probes = %d, want none", spy.probes)
			}
		})
	}
}

func TestHeadlessStepPostedKeys(t *testing.T) {
	raw := func(v any) json.RawMessage { return batchStepArgs(v) }
	cases := []struct {
		name string
		kind string
		args json.RawMessage
		want bool
	}{
		{"press-key --post", "press-key", raw(protocol.PressKeyArgs{Key: "Tab", Post: true}), true},
		{"type-text --post", "type-text", raw(protocol.TypeTextArgs{Text: "a\nb", Post: true}), true},
		{"press-key --post --headed", "press-key", raw(protocol.PressKeyArgs{Key: "Tab", Post: true, Headed: true}), false},
		{"type-text --post --headed", "type-text", raw(protocol.TypeTextArgs{Text: "a", Post: true, Headed: true}), false},
	}
	for _, tc := range cases {
		// A posted step needs no probe: its route is the flag.
		if got := headlessStep(tc.kind, tc.args, nil); got != tc.want {
			t.Errorf("%s: headlessStep = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestBatchPostedStepsKeepTheHeadlessScope(t *testing.T) {
	spy := newKeyRouteSpy(win.KeyTarget{})
	ov := &overlaySpy{share: spy.step}
	spy.svc.Overlay = ov
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
		Window: "101",
		Steps: []protocol.Step{
			stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(2)}),
			stepOf("type-text", protocol.TypeTextArgs{Text: "hello", Post: true}),
			stepOf("press-key", protocol.PressKeyArgs{Key: "Return", Post: true}),
		},
	})
	if cmdErr != nil || out != "Action 1 (click): ok\nAction 2 (type-text): "+postedKeysStep+"Action 3 (press-key): "+postedKeysStep {
		t.Fatalf("batch: %q, %v", out, cmdErr)
	}
	assertNoV1(t, spy.recorded())
	if b := spy.esc.brackets(); containsStep(b, "arm") || len(b) == 0 || b[0] != "arm-passive" {
		t.Errorf("Esc brackets = %v, want the passive scope", b)
	}
	if len(spy.postedText) != 1 || len(spy.postedKeys) != 1 {
		t.Errorf("posted text %q keys %q, want one of each", spy.postedText, spy.postedKeys)
	}
	// The scope needs no probe for a posted step; each step reads the
	// focus once when it runs.
	if spy.probes != 2 {
		t.Errorf("probed %d times, want 2 (one per posted step at run time)", spy.probes)
	}

	// A headed Batch with a posted step fails at that step: the two flags
	// do not combine.
	spy = newKeyRouteSpy(win.KeyTarget{})
	_, cmdErr = runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
		Window: "101",
		Headed: true,
		Steps:  []protocol.Step{stepOf("press-key", protocol.PressKeyArgs{Key: "Return", Post: true})},
	})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "--post and --headed") {
		t.Errorf("headed batch: err = %v, want the --post and --headed refusal", cmdErr)
	}
}

// A posted chord that failed before any key message went out sent
// nothing, unless the activation pair had: then the outcome is uncertain.
func TestPostChordFailureAfterActivationIsUncertain(t *testing.T) {
	spy, _, _ := chordSpy(editTarget(plainEdit), errors.New("PostMessage failed"))
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "press-key", protocol.PressKeyArgs{Window: "101", Key: "Alt_L+F5", Post: true})
	if cmdErr == nil || !strings.HasSuffix(cmdErr.Msg, nothingSentNote) {
		t.Errorf("a chord that sent nothing = %v, want nothing was sent", cmdErr)
	}
	spy, _, _ = chordSpy(editTarget(plainEdit), afterActivationErr{errors.New("PostMessage failed")})
	_, cmdErr = runCommand(t, CommandsWith(spy.svc), "press-key", protocol.PressKeyArgs{Window: "101", Key: "Alt_L+F5", Post: true})
	if cmdErr == nil || strings.Contains(cmdErr.Msg, "nothing was sent") || !strings.Contains(cmdErr.Hint, "may have taken effect") {
		t.Errorf("a chord failing after the activation pair = %v, want the uncertain hint", cmdErr)
	}
}
