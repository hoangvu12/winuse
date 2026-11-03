package helper

// type-text --into's offline tests (desktop-parity ticket 23, headless-max
// ticket 19): the classic edit route by the Element's own window on a
// Window that reports no focus (a covered Windows 11 Notepad), the
// refusals that say nothing was sent with the --headed retry, the Headed
// click and type, the Page route through the Browser Connection, the
// routing predicate and the Batch step, and plain type-text's note when
// it goes Headed for want of a focused control. Everything Win32,
// element-side and CDP is faked; no desktop is touched.

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// notepadEditor is a faked Windows 11 Notepad editor: the window behind
// its Element, which the thread does not report as focused when covered.
var notepadEditor = win.Control{HWND: 0x777, Class: "RichEditD2DPT", Style: win.ESMultiline, Enabled: true,
	Rect: win.Rect{Left: 20, Top: 80, Right: 800, Bottom: 480}}

// newIntoSpy is a keyRouteSpy whose probe finds no focused control and
// whose Element 2 is backed by edit's window.
func newIntoSpy(edit win.Control) *keyRouteSpy {
	s := newKeyRouteSpy(win.KeyTarget{})
	s.text.native = edit.HWND
	s.text.edit = edit
	s.svc.EditControl = func(hwnd uint64) win.Control {
		s.step("edit-control")
		return s.text.edit
	}
	s.states = []win.EditState{
		{Length: 5, SelStart: 5, SelEnd: 5, SelKnown: true},
		{Length: 10, SelStart: 10, SelEnd: 10, SelKnown: true},
	}
	return s
}

func intoArgs(index int, text string) protocol.TypeTextArgs {
	return protocol.TypeTextArgs{Window: "101", Text: text, IntoIndex: intPtrBatch(index)}
}

func TestTypeTextIntoAClassicEditWithNoFocus(t *testing.T) {
	spy := newIntoSpy(notepadEditor)
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "type-text", intoArgs(2, "hello"))
	if cmdErr != nil || out != okOutput {
		t.Fatalf("type-text --into: %q, %v", out, cmdErr)
	}
	want := []string{"locked", "element-window", "edit-control",
		"locked", "integrity", "ready", "cursor-travel", "edit-state", "replace-sel", "edit-state", "cursor-click"}
	if got := spy.recorded(); !equalStrings(got, want) {
		t.Errorf("steps = %v, want %v", got, want)
	}
	assertNoV1(t, spy.recorded())
	if spy.probes != 0 {
		t.Errorf("probed the Window's focus %d times; --into routes by the Element", spy.probes)
	}
	if spy.text.index != 2 {
		t.Errorf("read the window of element %d, want 2", spy.text.index)
	}
	if len(spy.inserted) != 1 || spy.inserted[0] != "hello" || spy.insertedTo[0] != notepadEditor.HWND {
		t.Errorf("inserted %q into %v, want hello into the editor", spy.inserted, spy.insertedTo)
	}
	if c := spy.cursor; len(c.labels) != 1 || c.labels[0] != "type" || c.targets[0] != notepadEditor.Rect {
		t.Errorf("cursor labels %q targets %v, want type at the editor", c.labels, c.targets)
	}
	if b := spy.esc.brackets(); !equalStrings(b, []string{"arm-passive", "disarm-passive"}) {
		t.Errorf("Esc brackets = %v, want one passive scope", b)
	}
}

func TestTypeTextIntoByReference(t *testing.T) {
	spy := newIntoSpy(plainEdit)
	spy.refIndex = 4
	if _, cmdErr := runCommand(t, CommandsWith(spy.svc), "type-text",
		protocol.TypeTextArgs{Window: "101", Text: "hello", IntoRef: "id:1001"}); cmdErr != nil {
		t.Fatalf("type-text --into id:1001: %v", cmdErr.Error())
	}
	if spy.text.index != 4 || len(spy.inserted) != 1 || spy.insertedTo[0] != plainEdit.HWND {
		t.Errorf("element %d, inserted %q into %v; want element 4's edit", spy.text.index, spy.inserted, spy.insertedTo)
	}
}

func TestTypeTextIntoRefusals(t *testing.T) {
	const secret = "hunter2-secret"
	readOnly := plainEdit
	readOnly.Style = win.ESReadOnly
	cases := []struct {
		name  string
		setup func(s *keyRouteSpy)
		text  string
		msg   string
		hint  string
	}{
		{"no edit window", func(s *keyRouteSpy) { s.text.native = 0 }, secret,
			"type-text --into has no Headless route into element 2: it is neither a classic edit control nor a Page Element; nothing was sent",
			"winuse --headed type-text 101 --into 2 <text>"},
		{"a window of another class", func(s *keyRouteSpy) { s.text.edit = okButton }, secret,
			"it is neither a classic edit control nor a Page Element", "winuse --headed type-text 101 --into 2 <text>"},
		{"a control character", nil, "a\nb",
			"a newline, a tab or another control character is typed as a real key", "winuse --headed type-text 101 --into 2 <text>"},
		{"a read-only edit", func(s *keyRouteSpy) { s.text.edit = readOnly }, secret,
			"its edit control is disabled or read-only", "pick another Element"},
		{"a stale element", func(s *keyRouteSpy) { s.text.nativeErr = uia.ErrStale }, secret, "", ""},
		{"locked desktop", func(s *keyRouteSpy) { s.locked, s.lockedDesktop = true, "Winlogon" }, secret, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := newIntoSpy(plainEdit)
			if tc.setup != nil {
				tc.setup(spy)
			}
			_, cmdErr := runCommand(t, CommandsWith(spy.svc), "type-text", intoArgs(2, tc.text))
			if cmdErr == nil {
				t.Fatal("type-text --into succeeded, want a refusal")
			}
			if !strings.HasSuffix(cmdErr.Msg, nothingSentNote) {
				t.Errorf("message = %q, want the nothing-sent note", cmdErr.Msg)
			}
			if tc.msg != "" && !strings.Contains(cmdErr.Msg, tc.msg) {
				t.Errorf("message = %q, want %q", cmdErr.Msg, tc.msg)
			}
			if tc.hint != "" && !strings.Contains(cmdErr.Hint, tc.hint) {
				t.Errorf("hint = %q, want %q", cmdErr.Hint, tc.hint)
			}
			if strings.Contains(cmdErr.Msg+cmdErr.Hint, secret) {
				t.Errorf("the refusal echoes the text: %q / %q", cmdErr.Msg, cmdErr.Hint)
			}
			if len(spy.inserted) != 0 {
				t.Errorf("inserted %q on a refusal", spy.inserted)
			}
			assertNoV1(t, spy.recorded())
		})
	}
}

func TestTypeTextIntoHeadedClicksThenTypes(t *testing.T) {
	spy := newIntoSpy(plainEdit)
	a := intoArgs(2, "hi\nthere")
	a.Headed = true
	if _, cmdErr := runCommand(t, CommandsWith(spy.svc), "type-text", a); cmdErr != nil {
		t.Fatalf("type-text --headed --into: %v", cmdErr.Error())
	}
	if got, want := spy.recorded(), []string{"locked", "integrity", "activate", "point", "click", "type-text"}; !equalStrings(got, want) {
		t.Errorf("steps = %v, want v1's %v", got, want)
	}
	if spy.clickX != 100 || spy.clickY != 200 || spy.clickButton != win.MouseLeft || spy.clickCount != 1 {
		t.Errorf("clicked %v at %d,%d x%d, want a left click at the Element's point", spy.clickButton, spy.clickX, spy.clickY, spy.clickCount)
	}
	if got := spy.typedTexts(); !reflect.DeepEqual(got, []string{"hi\nthere"}) {
		t.Errorf("typed %q", got)
	}
	if len(spy.inserted) != 0 || spy.probes != 0 {
		t.Errorf("inserted %q, probed %d: --headed takes neither Headless route", spy.inserted, spy.probes)
	}
}

func TestTypeTextIntoArgumentChecks(t *testing.T) {
	spy := newIntoSpy(plainEdit)
	a := intoArgs(2, "hi")
	a.Post = true
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "type-text", a)
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "--into and --post cannot be combined") {
		t.Errorf("--into with --post: %+v", cmdErr)
	}
	a = protocol.TypeTextArgs{Window: "101", Text: "hi", IntoIndex: intPtrBatch(2), IntoRef: "id:1001"}
	if _, cmdErr = runCommand(t, CommandsWith(spy.svc), "type-text", a); cmdErr == nil || !strings.Contains(cmdErr.Msg, "not several") {
		t.Errorf("two references: %+v", cmdErr)
	}
	if got := spy.recorded(); len(got) != 0 {
		t.Errorf("argument failures reached %v", got)
	}
}

func TestHeadlessStepTypeTextInto(t *testing.T) {
	cases := []struct {
		name string
		args protocol.TypeTextArgs
		want bool
	}{
		{"into", protocol.TypeTextArgs{Text: "hi", IntoIndex: intPtrBatch(2)}, true},
		{"into a reference with a newline", protocol.TypeTextArgs{Text: "a\nb", IntoRef: "id:1001"}, true},
		{"into headed", protocol.TypeTextArgs{Text: "hi", IntoIndex: intPtrBatch(2), Headed: true}, false},
		{"into posted", protocol.TypeTextArgs{Text: "hi", IntoIndex: intPtrBatch(2), Post: true}, false},
	}
	for _, tc := range cases {
		// No probe: --into does not need the Window's focus.
		if got := headlessStep("type-text", batchStepArgs(tc.args), nil); got != tc.want {
			t.Errorf("%s: headlessStep = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestBatchTypeTextIntoKeepsTheHeadlessScope(t *testing.T) {
	spy := newIntoSpy(notepadEditor)
	if _, cmdErr := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
		Window: "101",
		Steps: []protocol.Step{
			stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(3)}),
			stepOf("type-text", protocol.TypeTextArgs{Text: "hello", IntoIndex: intPtrBatch(2)}),
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

	// A refused step stops the Batch with nothing sent, still passive.
	spy = newIntoSpy(okButton)
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
		Window: "101",
		Steps:  []protocol.Step{stepOf("type-text", protocol.TypeTextArgs{Text: "hello", IntoIndex: intPtrBatch(2)})},
	})
	if cmdErr == nil || !strings.HasPrefix(cmdErr.Msg, "batch stopped at Action 1 (type-text): type-text --into has no Headless route") {
		t.Errorf("batch refusal: %+v", cmdErr)
	}
	if b := spy.esc.brackets(); len(b) == 0 || b[0] != "arm-passive" {
		t.Errorf("Esc brackets = %v, want the passive scope", b)
	}
}

// TestTypeTextSaysWhyItHasNoBackgroundRoute: plain type-text with no
// Headless route is refused with nothing sent, the reason the probe read,
// the --into hint and the --headed retry; it never goes Headed on its own
// (headless-max tickets 19 and 21).
func TestTypeTextSaysWhyItHasNoBackgroundRoute(t *testing.T) {
	// No focused control: refused, why, and the --into hint.
	spy := newKeyRouteSpy(win.KeyTarget{})
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "type-text", typeTextArgs("101", "hi"))
	wantHeadedRefusal(t, "type-text with no focused control", out, cmdErr)
	if !strings.Contains(cmdErr.Msg, "window 101 reports no focused control") {
		t.Errorf("message = %q, want it to say the Window reports no focused control", cmdErr.Msg)
	}
	if !strings.Contains(cmdErr.Hint, "winuse type-text 101 --into <element> <text>") {
		t.Errorf("hint = %q, want the --into form", cmdErr.Hint)
	}
	if got := spy.recorded(); len(got) != 0 {
		t.Errorf("a refused type-text ran %v", got)
	}

	// No focused control, and the Window is already the foreground: still
	// refused, nothing sent.
	spy = newKeyRouteSpy(win.KeyTarget{})
	spy.svc.Foreground = func() uint64 { return 101 }
	out, cmdErr = runCommand(t, CommandsWith(spy.svc), "type-text", typeTextArgs("101", "hi"))
	wantHeadedRefusal(t, "type-text in the foreground Window", out, cmdErr)
	if got := spy.recorded(); len(got) != 0 {
		t.Errorf("a refused type-text ran %v", got)
	}

	// A focused control that is no edit: refused, naming its class.
	spy = newKeyRouteSpy(win.KeyTarget{Focus: okButton})
	out, cmdErr = runCommand(t, CommandsWith(spy.svc), "type-text", typeTextArgs("101", "hi"))
	wantHeadedRefusal(t, "type-text into a button's focus", out, cmdErr)
	if !strings.Contains(cmdErr.Msg, "(class Button) is not a classic edit control") {
		t.Errorf("message = %q, want it to name the focused control's class", cmdErr.Msg)
	}
	if got := spy.recorded(); len(got) != 0 {
		t.Errorf("a refused type-text ran %v", got)
	}

	// --headed skips the probe and says nothing.
	spy = newKeyRouteSpy(win.KeyTarget{})
	if out, _ := runCommand(t, CommandsWith(spy.svc), "type-text", protocol.TypeTextArgs{Window: "101", Text: "hi", Headed: true}); out != okOutput {
		t.Errorf("--headed: output = %q, want ok", out)
	}
}

// TestPageTypeTextInto: a Page field is focused and typed into over the
// Browser Connection, with the Agent Cursor and no foreground; refusals
// send nothing.
func TestPageTypeTextInto(t *testing.T) {
	r := newPageRig(t)
	out, e := r.run(t, "type-text", protocol.TypeTextArgs{Window: "301", Text: "a@b.c\n", IntoIndex: idx(8)})
	if e != nil || out != okOutput {
		t.Fatalf("type-text --into: %q, %+v", out, e)
	}
	if got := r.sent(); !reflect.DeepEqual(got, []string{"focus", "insertText a@b.c", "dispatchKeyEvent keyDown Enter \r", "dispatchKeyEvent keyUp Enter"}) {
		t.Errorf("sent %q", got)
	}
	r.noForeground(t)
	if containsStep(r.recorded(), "element-window") || containsStep(r.recorded(), "replace-sel") {
		t.Errorf("a Page Element took a UIA route: %v", r.recorded())
	}
	if len(r.cursor.labels) != 1 || r.cursor.labels[0] != `type "Email"` || len(r.cursor.clicks) != 1 {
		t.Errorf("cursor labels %q, ripples %d", r.cursor.labels, len(r.cursor.clicks))
	}

	refusals := []struct {
		name  string
		field map[string]any
		want  string
	}{
		{"no text", map[string]any{"kind": ""}, "element 8 takes no typed text (it is not a text field, a text area or editable text); nothing was sent"},
		{"read-only", map[string]any{"kind": "text", "readonly": true}, "element 8 is read-only; nothing was sent"},
		{"disabled", map[string]any{"kind": "text", "disabled": true}, "element 8 cannot be typed into: "},
	}
	for _, c := range refusals {
		r := newPageRig(t)
		r.set(func() { r.field = c.field })
		_, e := r.run(t, "type-text", protocol.TypeTextArgs{Window: "301", Text: "x", IntoIndex: idx(8)})
		if e == nil || !strings.HasPrefix(e.Msg, c.want) || !strings.HasSuffix(e.Msg, nothingSentNote) || e.Hint == "" {
			t.Errorf("%s: %+v", c.name, e)
		}
		if got := r.sent(); len(got) != 0 {
			t.Errorf("%s sent %v", c.name, got)
		}
	}

	r = newPageRig(t)
	_, e = r.run(t, "type-text", protocol.TypeTextArgs{Window: "301", Text: "x", IntoIndex: idx(8), Headed: true})
	if e == nil || !strings.Contains(e.Msg, "type-text --headed --into does not reach element 8") || !strings.HasSuffix(e.Msg, nothingSentNote) {
		t.Errorf("headed: %+v", e)
	}
	if containsStep(r.recorded(), "activate") || len(r.sent()) != 0 {
		t.Errorf("headed on a Page Element activated or sent: %v %v", r.recorded(), r.sent())
	}

	if !r.svc.stepHeadless(context.Background(), "type-text", batchStepArgs(protocol.TypeTextArgs{Window: "301", Text: "x", IntoIndex: idx(8)}), &win.WindowInfo{ID: browserWindowID}) {
		t.Error("a type-text --into step on a Page Element must be Headless")
	}
}
