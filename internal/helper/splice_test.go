package helper

// type-text --into's caret splice (headless-max ticket 13): an Element with
// no classic edit control behind it goes to the Text-pattern route, which
// runs Headless through runHeadless, says in its output that no key
// events fired, and refuses with nothing sent where it cannot splice. The
// element side is faked; no desktop is touched.

import (
	"context"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// spliceSpy is a headlessSpy that is also a TextSplicer.
type spliceSpy struct {
	*headlessSpy
	text   string
	index  int
	result uia.SpliceResult
	err    error
}

func (s *spliceSpy) SpliceText(ctx context.Context, hwnd uint64, index int, text string) (uia.SpliceResult, error) {
	s.step("splice")
	s.text, s.index = text, index
	s.esc.pressIf("act")
	return s.result, s.err
}

// newSpliceSpy is a spliceSpy whose Element 2 is drawn inside its Window
// (no window of its own, so no classic edit control).
func newSpliceSpy() *spliceSpy {
	s := &spliceSpy{headlessSpy: newHeadlessSpy(), result: uia.SpliceResult{CaretPlaced: true}}
	s.headlessSpy.text.native = 0
	s.svc.EditControl = func(hwnd uint64) win.Control { s.step("edit-control"); return win.Control{} }
	s.svc.Actions = s
	return s
}

func TestTypeTextIntoATextPatternFieldSplices(t *testing.T) {
	spy := newSpliceSpy()
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "type-text", intoArgs(2, "héllo 😀"))
	if cmdErr != nil {
		t.Fatalf("type-text --into: %v", cmdErr.Error())
	}
	if out != "ok\nNote: the text was set as the field's value at the caret: no key events fired, and the field's undo may not hold it\n" {
		t.Errorf("output = %q, want ok with the no-key-events note", out)
	}
	if spy.text != "héllo 😀" || spy.index != 2 {
		t.Errorf("spliced %q into element %d, want the text into 2", spy.text, spy.index)
	}
	steps := spy.recorded()
	assertNoV1(t, steps)
	if !containsStep(steps, "splice") || !containsStep(steps, "cursor-travel") || !containsStep(steps, "cursor-click") {
		t.Errorf("steps = %v, want the splice between the cursor's travel and its click", steps)
	}
	if b := spy.esc.brackets(); !equalStrings(b, []string{"arm-passive", "disarm-passive"}) {
		t.Errorf("Esc brackets = %v, want one passive scope", b)
	}
	if c := spy.cursor; len(c.labels) != 1 || strings.Contains(c.labels[0], "héllo") {
		t.Errorf("cursor labels = %q; the text never shows", c.labels)
	}

	// The caret that could not be placed is reported, not failed.
	spy = newSpliceSpy()
	spy.result.CaretPlaced = false
	out, cmdErr = runCommand(t, CommandsWith(spy.svc), "type-text", intoArgs(2, "x"))
	if cmdErr != nil || !strings.HasPrefix(out, "ok\nNote: ") || !strings.HasSuffix(out, "; the caret could not be placed after the text\n") {
		t.Errorf("output = %q, %v, want the caret note", out, cmdErr)
	}
}

func TestTypeTextIntoSpliceRefusals(t *testing.T) {
	cases := []struct {
		name string
		err  error
		msg  string
		hint string
	}{
		{"no Text pattern", uia.ErrNoTextPattern, "has no Headless route into element 2", "winuse --headed type-text 101 --into 2 <text>"},
		{"no Value pattern", uia.ErrSpliceNoValue, "offers no Value pattern", "--headed"},
		{"rich text", uia.ErrSpliceRichText, "rich text document", "--headed"},
		{"text and value differ", uia.ErrSpliceMismatch, "text and its value differ", "--headed"},
		{"no caret", uia.ErrSpliceNoCaret, "no caret", "--headed"},
		{"password", uia.ErrSplicePassword, "password field", "--headed"},
		{"read-only", uia.ErrNotSettable, "it is read-only", "pick another Element"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := newSpliceSpy()
			spy.err = tc.err
			_, cmdErr := runCommand(t, CommandsWith(spy.svc), "type-text", intoArgs(2, "secret-text"))
			if cmdErr == nil {
				t.Fatal("type-text --into succeeded, want a refusal")
			}
			if !strings.Contains(cmdErr.Msg, tc.msg) || !strings.HasSuffix(cmdErr.Msg, nothingSentNote) {
				t.Errorf("message = %q, want %q and %q", cmdErr.Msg, tc.msg, nothingSentNote)
			}
			if !strings.Contains(cmdErr.Hint, tc.hint) {
				t.Errorf("hint = %q, want %q", cmdErr.Hint, tc.hint)
			}
			if strings.Contains(cmdErr.Msg+cmdErr.Hint, "secret-text") {
				t.Error("a refusal echoed the text")
			}
			if containsStep(spy.recorded(), "cursor-click") {
				t.Error("a refusal rippled the cursor")
			}
		})
	}

	// A failed write may have taken effect; a failed read-back was
	// delivered. Neither says nothing was sent.
	for _, err := range []error{&uia.WriteError{What: "setting the field's value", HR: -1},
		&uia.VerifyError{Action: "type-text", Expected: "the text at the caret", Observed: "another value"}} {
		spy := newSpliceSpy()
		spy.err = err
		_, cmdErr := runCommand(t, CommandsWith(spy.svc), "type-text", intoArgs(2, "x"))
		if cmdErr == nil || strings.HasSuffix(cmdErr.Msg, nothingSentNote) {
			t.Errorf("%T: err = %v, want a failure that does not say nothing was sent", err, cmdErr)
		}
	}
}

func TestTypeTextIntoKeepsItsOtherRoutesFirst(t *testing.T) {
	// Control characters are keys, not text: refused before the splice.
	spy := newSpliceSpy()
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "type-text", intoArgs(2, "line\nbreak"))
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "control character") || containsStep(spy.recorded(), "splice") {
		t.Errorf("text with a newline: %v, steps %v; want the control-character refusal and no splice", cmdErr, spy.recorded())
	}
	// A classic edit control behind the Element keeps EM_REPLACESEL.
	spy = newSpliceSpy()
	spy.headlessSpy.text.native = 555
	spy.svc.EditControl = func(hwnd uint64) win.Control {
		return win.Control{HWND: 555, Class: "Edit", Enabled: true, Style: win.ESReadOnly}
	}
	_, cmdErr = runCommand(t, CommandsWith(spy.svc), "type-text", intoArgs(2, "x"))
	if cmdErr == nil || containsStep(spy.recorded(), "splice") {
		t.Errorf("a read-only classic edit: %v, steps %v; want its own refusal, never the splice", cmdErr, spy.recorded())
	}
}
