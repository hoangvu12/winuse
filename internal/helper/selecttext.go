package helper

// select-text (headless-max ticket 10, headless-100.md G8): `select-text
// <window> <element> <start> <length>` and `select-text <window> <element>
// all` select text in a field with no keys, no mouse and no foreground.
// It is always Headless; there is no headed form. Three routes, picked by
// the Element, never by a fallback after something was sent:
//
//   - a Page Element: over the Browser Connection (pageSelectText), an
//     input's setSelectionRange or a Selection over editable text;
//   - an Element whose own window (UIA NativeWindowHandle) is a classic
//     Edit or RichEdit control: EM_SETSEL, read back with EM_GETSEL
//     (selectInEdit). Measured cross-thread;
//   - any other Element: its UIA Text pattern (uia.Client.SelectText),
//     read back through TextPattern.GetSelection. Unmeasured headless.
//
// An Element with none of them fails with "nothing was sent". The span
// is UTF-16 code units, as type-text's verification counts, and `all` is
// 0 to the length. The classic route leaves the selection where
// type-text's EM_REPLACESEL replaces it, so select-text then type-text is
// a headless "replace this word".

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// selectTextUsageHint is the recovery hint for select-text's argument
// failures.
const selectTextUsageHint = "usage: winuse select-text <window> <element index | name:<text> | id:<id>> <start> <length> | all"

// TextSelector is the element side of select-text, on the UIA worker: the
// Element's own window, for the classic edit route, and the Text pattern
// route. The real implementation is *uia.Client; Services.Actions is
// type-asserted to it, like ElementActor.
type TextSelector interface {
	// ElementWindow reads the Element's NativeWindowHandle: its own HWND
	// for a classic control, 0 for an Element drawn inside another window.
	ElementWindow(ctx context.Context, hwnd uint64, index int) (uint64, error)
	// SelectText selects the span through the Element's Text pattern and
	// reads the selection back.
	SelectText(ctx context.Context, hwnd uint64, index int, span uia.TextSpan) error
}

var _ TextSelector = (*uia.Client)(nil)

// selectTextSpan checks select-text's span: a start and a length, or all,
// never both.
func selectTextSpan(a protocol.SelectTextArgs) (uia.TextSpan, *Error) {
	if a.All && (a.Start != 0 || a.Length != 0) {
		return uia.TextSpan{}, Fail("select-text: pass a start and a length, or all, not both", selectTextUsageHint)
	}
	span := uia.TextSpan{Start: a.Start, Length: a.Length, All: a.All}
	if err := uia.CheckTextSpan(span); err != nil {
		return uia.TextSpan{}, Fail("select-text: "+err.Error(), selectTextUsageHint)
	}
	return span, nil
}

// headlessSelectText is the routing predicate's half for select-text: an
// Element Index or name:/id: reference with a valid span.
func headlessSelectText(a protocol.SelectTextArgs) bool {
	if !elementForm(a.ElementIndex, a.Ref, nil) {
		return false
	}
	_, serr := selectTextSpan(a)
	return serr == nil
}

// selectText runs `winuse select-text <window> <element> <start> <length>
// | all`: a Page Element over the Browser Connection, anything else
// through runHeadless (the refusals, the passive Esc scope, the Agent
// Cursor) with the classic edit route or the Text pattern route as its
// act.
func selectText(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.SelectTextArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("select-text: "+err.Error(), selectTextUsageHint)
		}
		target, terr := parseElementTarget("select-text", a.ElementIndex, a.Ref, nil, selectTextUsageHint)
		if terr != nil {
			return "", terr
		}
		span, serr := selectTextSpan(a)
		if serr != nil {
			return "", serr
		}
		if out, rerr, ok := s.onPage(ctx, a.Window, target, func(w win.WindowInfo, index int, o *page.Origin) (string, *Error) {
			return s.pageSelectText(ctx, w, index, o, target, span)
		}); ok {
			return out, rerr
		}
		ha, ok := s.Actions.(HeadlessActor)
		ts, ok2 := s.Actions.(TextSelector)
		if !ok || !ok2 {
			return "", Fail("select-text is unavailable: no UIA client", "")
		}
		return s.runHeadless(ctx, a.Window, ha, headlessRun{
			cmd:       "select-text",
			verb:      "select text",
			target:    target,
			minimized: true,
			act: func(ctx context.Context, w win.WindowInfo, index int) error {
				native, err := ts.ElementWindow(ctx, w.ID, index)
				if err != nil {
					return err
				}
				if native != 0 {
					if c := s.EditControl(native); c.IsEdit() {
						return s.selectInEdit(c, span)
					}
				}
				return ts.SelectText(ctx, w.ID, index, span)
			},
			refusal: func(err error, w win.WindowInfo, index int) *Error {
				return selectTextRefusal(err, w, index, target)
			},
		})
	}
}

// editSendError is a failed EM_SETSEL: a timed-out message may still be
// processed, so the selection may have changed.
type editSendError struct{ err error }

func (e *editSendError) Error() string { return e.err.Error() }
func (e *editSendError) Unwrap() error { return e.err }

// selectInEdit is the classic edit route: the length is read (nothing is
// sent if that fails), the span is checked against it, EM_SETSEL selects
// it (`all` sends 0 to -1, the control's own end), and EM_GETSEL reads it
// back.
func (s Services) selectInEdit(edit win.Control, span uia.TextSpan) error {
	before, err := s.EditState(edit.HWND)
	if err != nil {
		return &notSentError{err: err, hint: "the edit control may have closed or be busy; read the Window State and retry"}
	}
	start, end, err := span.Bounds(before.Length)
	if err != nil {
		return err
	}
	send := end
	if span.All {
		send = -1
	}
	if err := s.SetEditSel(edit.HWND, start, send); err != nil {
		return &editSendError{err: err}
	}
	after, err := s.EditState(edit.HWND)
	if err != nil {
		return &uia.VerifyError{Msg: "the selection was delivered but the edit control's selection could not be read back"}
	}
	if !editSelectionOK(start, end, span.All, edit.RichEdit(), after) {
		return &uia.VerifyError{Msg: fmt.Sprintf("the selection was delivered but the edit control selects characters %d to %d afterwards, not %d to %d",
			min(after.SelStart, after.SelEnd), max(after.SelStart, after.SelEnd), start, end)}
	}
	return nil
}

// editSelectionOK is the pure read-back of the classic edit route:
// EM_GETSEL must show start to end. A RichEdit control counts a paragraph
// break as two units in WM_GETTEXTLENGTH but one in EM_GETSEL, so `all`
// there reads back as 0 to at most the length (and past 0 when there is
// text). A selection EM_GETSEL cannot tell (an end past 65,535) is not
// judged.
func editSelectionOK(start, end int, all, rich bool, got win.EditState) bool {
	if !got.SelKnown {
		return true
	}
	gs, ge := min(got.SelStart, got.SelEnd), max(got.SelStart, got.SelEnd)
	if all && rich {
		return gs == 0 && ge <= end && (ge > 0 || end == 0)
	}
	return gs == start && ge == end
}

// selectTextRefusal is select-text's element-side mapping for runHeadless
// (headlessFailure adds "nothing was sent" to the refusals in
// refusedBeforeCall): no route, a span past the end, the edit control's
// own read before the selection; the rest is the shared mapping.
func selectTextRefusal(err error, w win.WindowInfo, index int, target elementTarget) *Error {
	id := strconv.FormatUint(w.ID, 10)
	var notSent *notSentError
	if errors.As(err, &notSent) {
		return nothingSent(Fail(err.Error(), notSent.hint))
	}
	var sent *editSendError
	if errors.As(err, &sent) {
		return Fail(err.Error(), "the selection may have changed; read the Window State (winuse get-window-state "+id+") before repeating it")
	}
	switch {
	case errors.Is(err, uia.ErrNoTextPattern), errors.Is(err, uia.ErrNoTextSelection), errors.Is(err, uia.ErrTextUnreadable):
		return Fail(err.Error(),
			"select-text has no headed form; to select with real keys (this brings the Window to the foreground): winuse --headed click "+
				id+" "+target.token()+", then winuse press-key "+id+" Control_L+a, or Shift_L+Right to extend")
	case errors.Is(err, uia.ErrTextRange):
		return Fail(err.Error(), textRangeHint(id))
	case errors.Is(err, uia.ErrTextSpan):
		return Fail(err.Error(), selectTextUsageHint)
	case errors.Is(err, uia.ErrDisabled):
		return Fail(err.Error(), "a disabled Element cannot be acted on; read a fresh Window State and pick another Element")
	}
	return elementError(err, w, index)
}

// textRangeHint is the recovery for a span past the end of the text.
func textRangeHint(id string) string {
	return "offsets count UTF-16 units from 0; read the field's text again (winuse get-window-state " + id + ") and count, or pass all"
}

// pageSelectText selects text in a Page Element (page.Origin.SelectText):
// the field is read first, and a node that holds no text, an input type
// that takes no selection (email, number) or a span past its end is
// refused with nothing sent; then DOM.focus, the selection and its
// read-back, in runPage's passive Esc scope with the Agent Cursor.
func (s Services) pageSelectText(ctx context.Context, w win.WindowInfo, index int, o *page.Origin, target elementTarget, span uia.TextSpan) (string, *Error) {
	id := strconv.FormatUint(w.ID, 10)
	return s.runPage(ctx, pageRun{
		noun: "selection", done: "selected", verb: "select text", w: w, index: index, origin: o, target: target,
		check: func(ctx context.Context) error {
			f, err := o.ReadTextField(ctx)
			if err != nil {
				return err
			}
			switch {
			case f.Kind == "":
				return &actRefusal{fmt.Sprintf("element %d holds no text to select (it is not a text field, a text area or editable text)", index),
					"pick a text field from a fresh Window State: winuse get-window-state " + id}
			case !f.Selectable:
				return &actRefusal{fmt.Sprintf("element %d is an input whose type takes no text selection (such as email or number)", index),
					"set its whole value instead: winuse set-value " + id + " " + target.token() + " <value>"}
			}
			if _, _, err := span.Bounds(f.Length); err != nil {
				return &actRefusal{err.Error(), textRangeHint(id)}
			}
			return nil
		},
		aim: page.AimOptions{Scroll: true},
		act: func(ctx context.Context, in *page.Input, aim page.Aim) error {
			return o.SelectText(ctx, in, span.Start, span.Length, span.All)
		},
	})
}
