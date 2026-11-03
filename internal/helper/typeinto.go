package helper

// type-text --into (desktop-parity ticket 23, headless-max ticket 19;
// ADR 0011, Decision 4 as amended 2026-11-03): `type-text <window> --into
// <element> <text>` types into the Element it names instead of whatever
// holds the keyboard focus. The route is picked by the Element, before
// anything is sent, never by a fallback after:
//
//   - a Page Element: over the Browser Connection (pageTypeInto), DOM.focus
//     then the text as type-text types it into the Page's focus;
//   - an Element whose own window (UIA NativeWindowHandle) is a writable
//     classic edit control (Edit, RichEdit, RichEditD2DPT), with text that
//     has no control characters: EM_REPLACESEL into that window
//     (typeIntoEdit), at the control's own selection, which survives
//     deactivation. A covered Windows 11 Notepad reports no focused
//     control on its thread, so plain type-text has no Headless route
//     there; its editor's Element still names its window;
//   - an Element with the Text and Value patterns and no classic edit
//     control behind it (WPF, WinUI), with text that has no control
//     characters: the caret splice (spliceInto, headless-max ticket 13),
//     the Value rewritten as prefix + text + suffix at the field's own
//     selection, read back, and the caret placed after the text. No key
//     events fire, and the output says so;
//   - any other Element: with headed, v1's pipeline (activate, click the
//     Element, type with real keys); without, a refusal that says nothing
//     was sent and gives the --headed retry.
//
// So an unheaded --into is Headless or sends nothing: a Batch counts such
// a step as Headless for its scope without reading the Element.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// typeIntoUsageHint is the recovery hint for --into's argument failures.
const typeIntoUsageHint = "usage: winuse type-text <window> --into <element index | name:<text> | id:<id>> <text>"

// typesInto reports whether type-text names an Element (--into).
func typesInto(a protocol.TypeTextArgs) bool {
	return a.IntoIndex != nil || a.IntoRef != ""
}

// typeTextInto runs `winuse type-text <window> --into <element> <text>`:
// the Page route, then, with headed, v1's click and type; otherwise the
// classic edit route or the refusal.
func (s Services) typeTextInto(ctx context.Context, a protocol.TypeTextArgs) (string, *Error) {
	if a.Post {
		return "", Fail("type-text: --into and --post cannot be combined",
			"--into types into the Element you name; --post posts to whatever holds the focus; pass one of them")
	}
	target, terr := parseElementTarget("type-text --into", a.IntoIndex, a.IntoRef, nil, typeIntoUsageHint)
	if terr != nil {
		return "", terr
	}
	if out, rerr, ok := s.onPage(ctx, a.Window, target, func(w win.WindowInfo, index int, o *page.Origin) (string, *Error) {
		return s.pageTypeInto(ctx, w, index, o, target, a.Text, a.Headed)
	}); ok {
		return out, rerr
	}
	if a.Headed {
		interval, _ := intervalOf(a) // checked by typeText
		return s.typeIntoHeaded(ctx, a.Window, target, a.Text, interval)
	}
	w, rerr := resolveWindow(s, a.Window)
	if rerr != nil {
		return "", rerr
	}
	if locked, desktop := s.Locked(); locked {
		return "", nothingSent(lockedDesktopFailure(desktop))
	}
	index, rerr := target.resolve(ctx, s, w)
	if rerr != nil {
		return "", nothingSent(rerr)
	}
	edit, err := s.intoEdit(ctx, w, index)
	if err != nil {
		return "", nothingSent(elementError(err, w, index))
	}
	if edit.Writable() && !hasControlChars(a.Text) {
		out, rerr := s.typeIntoEdit(ctx, w, edit, a.Text)
		if rerr == nil {
			s.showCaret(ctx, w, index)
		}
		return out, rerr
	}
	if sp, ok := s.Actions.(TextSplicer); ok && !edit.IsEdit() && !hasControlChars(a.Text) {
		return s.spliceInto(ctx, a.Window, target, a.Text, sp)
	}
	return "", intoRefusal(w, target, index, edit, a.Text)
}

// TextSplicer is the caret splice's element side (headless-max ticket
// 13): *uia.Client. A helper whose Services.Actions is not one refuses
// --into on Elements with no classic edit control, as before.
type TextSplicer interface {
	// SpliceText puts text at the Element's caret through its Text and
	// Value patterns, reads the value back and places the caret after it.
	SpliceText(ctx context.Context, hwnd uint64, index int, text string) (uia.SpliceResult, error)
}

var _ TextSplicer = (*uia.Client)(nil)

// spliceOutput is the splice's success: the text is in, written as the
// field's value, which is not typing; the agent learns what that means.
func spliceOutput(r uia.SpliceResult) string {
	out := "ok (set as the field's value at the caret: no key events fired, and the field's undo may not hold it"
	if !r.CaretPlaced {
		out += "; the caret could not be placed after the text"
	}
	return out + ")\n"
}

// spliceInto runs the caret splice through runHeadless (its refusals, the
// passive Esc scope, the Agent Cursor). Measured on minimized WPF and
// WinUI 3 Windows too (headless-max ticket 14), so it reaches them.
func (s Services) spliceInto(ctx context.Context, ref string, target elementTarget, text string, sp TextSplicer) (string, *Error) {
	ha, ok := s.Actions.(HeadlessActor)
	if !ok {
		return "", Fail("type-text --into is unavailable: no UIA client", "")
	}
	var res uia.SpliceResult
	if _, rerr := s.runHeadless(ctx, ref, ha, headlessRun{
		cmd:       "type-text",
		verb:      "type", // never the text: it could be secret
		target:    target,
		minimized: true,
		act: func(ctx context.Context, w win.WindowInfo, index int) error {
			r, err := sp.SpliceText(ctx, w.ID, index, text)
			res = r
			if err == nil {
				s.showCaret(ctx, w, index)
			}
			return err
		},
		refusal: func(err error, w win.WindowInfo, index int) *Error {
			return spliceRefusal(err, w, index, target)
		},
	}); rerr != nil {
		return "", rerr
	}
	return spliceOutput(res), nil
}

// spliceRefusal maps the splice's refusals (raised before the write; see
// refusedBeforeCall), each with the --headed retry where real keys can do
// what the splice cannot.
func spliceRefusal(err error, w win.WindowInfo, index int, target elementTarget) *Error {
	id := strconv.FormatUint(w.ID, 10)
	retry := "to click it and type with real keys (this brings the Window to the foreground): winuse --headed type-text " +
		id + " --into " + target.token() + " <text>"
	switch {
	case errors.Is(err, uia.ErrNoTextPattern), errors.Is(err, uia.ErrSpliceNoValue), errors.Is(err, uia.ErrSpliceRichText),
		errors.Is(err, uia.ErrSpliceMismatch), errors.Is(err, uia.ErrSpliceNoCaret), errors.Is(err, uia.ErrTextUnreadable):
		return Fail(fmt.Sprintf("type-text --into has no Headless route into element %d: %v", index, err), retry)
	case errors.Is(err, uia.ErrSplicePassword):
		return Fail(fmt.Sprintf("type-text --into does not rewrite element %d: %v", index, err), retry)
	case errors.Is(err, uia.ErrNotSettable):
		return Fail(fmt.Sprintf("type-text --into cannot type into element %d: it is read-only", index),
			"a read-only field takes no text; read a fresh Window State (winuse get-window-state "+id+") and pick another Element")
	case errors.Is(err, uia.ErrDisabled):
		return Fail(err.Error(), "a disabled Element cannot be acted on; read a fresh Window State and pick another Element")
	}
	return elementError(err, w, index)
}

// intoEdit reads the window behind the Element (its NativeWindowHandle)
// as a control: a classic edit control when it is one, otherwise a control
// of no edit class (an Element drawn inside another window has none). A
// helper with no TextSelector reads none.
func (s Services) intoEdit(ctx context.Context, w win.WindowInfo, index int) (win.Control, error) {
	ts, ok := s.Actions.(TextSelector)
	if !ok || s.EditControl == nil {
		return win.Control{}, nil
	}
	native, err := ts.ElementWindow(ctx, w.ID, index)
	if err != nil || native == 0 {
		return win.Control{}, err
	}
	return s.EditControl(native), nil
}

// intoRefusal refuses an unheaded --into that has no Headless route, with
// nothing sent: why, and the --headed retry (never the text, which could
// be a password).
func intoRefusal(w win.WindowInfo, target elementTarget, index int, edit win.Control, text string) *Error {
	id := strconv.FormatUint(w.ID, 10)
	retry := "to click it and type with real keys (this brings the Window to the foreground): winuse --headed type-text " +
		id + " --into " + target.token() + " <text>"
	switch {
	case edit.IsEdit() && !edit.Writable():
		return nothingSent(Fail(fmt.Sprintf("type-text --into cannot type into element %d: its edit control is disabled or read-only", index),
			"a disabled or read-only field takes no text; read a fresh Window State (winuse get-window-state "+id+") and pick another Element"))
	case edit.IsEdit():
		return nothingSent(Fail(fmt.Sprintf("type-text --into has no Headless route for this text into element %d: a newline, a tab or another control character is typed as a real key", index),
			"type the text without it, or press the key with winuse press-key; "+retry))
	}
	if hasControlChars(text) {
		return nothingSent(Fail(fmt.Sprintf("type-text --into has no Headless route for this text into element %d: a newline, a tab or another control character is typed as a real key", index),
			"type the text without it, or press the key with winuse press-key; "+retry))
	}
	return nothingSent(Fail(fmt.Sprintf("type-text --into has no Headless route into element %d: it is neither a classic edit control, a Text-pattern field nor a Page Element", index), retry))
}

// typeIntoHeaded is --into with headed: v1's pipeline (activate and
// verify the foreground), a left click on the Element's clickable point,
// then the text typed with real keys, as plain Headed type-text types it.
func (s Services) typeIntoHeaded(ctx context.Context, ref string, target elementTarget, text string, interval time.Duration) (string, *Error) {
	if s.Actions == nil {
		return "", Fail("type-text --into is unavailable: no UIA client", "")
	}
	return s.runAction(ctx, ref, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
		index, rerr := target.resolve(ctx, s, w)
		if rerr != nil {
			return "", rerr
		}
		pt, err := s.Actions.ElementPoint(ctx, w.ID, index)
		if err != nil {
			return "", elementError(err, w, index)
		}
		if err := s.Click(w.ID, pt.X, pt.Y, win.MouseLeft, 1); err != nil {
			return "", inputError(err)
		}
		if err := s.typeKeys(text, interval, func() bool { return s.Esc.Stopped() || ctx.Err() != nil }); err != nil {
			return "", inputAbortError(s, ctx, err, "while typing (the text injected so far landed)")
		}
		return okOutput, nil
	})
}

// pageTypeInto types into a Page Element (page.Origin.TypeInto): the
// field is read first, and a node that takes no typed text, a disabled
// or a read-only one is refused with nothing sent; then DOM.focus and the
// text, in runPage's passive Esc scope with the Agent Cursor. --headed
// has no UIA Element to click here, so it is refused before anything is
// activated, as set-value --headed is.
func (s Services) pageTypeInto(ctx context.Context, w win.WindowInfo, index int, o *page.Origin, target elementTarget, text string, headed bool) (string, *Error) {
	id := strconv.FormatUint(w.ID, 10)
	if headed {
		return "", nothingSent(Fail(fmt.Sprintf("type-text --headed --into does not reach element %d: it is in the Page of window %d and has no UI Automation Element", index, w.ID),
			"drop --headed: type-text --into types into Page Elements through the Browser Connection, with no foreground needed"))
	}
	return s.runPage(ctx, pageRun{
		noun: "text", done: "typed into", verb: "type", w: w, index: index, origin: o, target: target,
		check: func(ctx context.Context) error {
			f, err := o.ReadField(ctx)
			if err != nil {
				return err
			}
			switch {
			case f.Kind == "":
				return &actRefusal{fmt.Sprintf("element %d takes no typed text (it is not a text field, a text area or editable text)", index),
					"pick a text field from a fresh Window State: winuse get-window-state " + id}
			case f.ReadOnly && !f.Disabled:
				return &actRefusal{fmt.Sprintf("element %d is read-only", index),
					"a read-only field takes no text; pick another Element from a fresh Window State: winuse get-window-state " + id}
			}
			return f.Refusal() // disabled: the shared not-actionable refusal
		},
		aim: page.AimOptions{Scroll: true},
		act: func(ctx context.Context, in *page.Input, aim page.Aim) error {
			return o.TypeInto(ctx, in, text)
		},
	})
}
