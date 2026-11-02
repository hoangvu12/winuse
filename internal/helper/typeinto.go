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
//   - any other Element: with headed, v1's pipeline (activate, click the
//     Element, type with real keys); without, a refusal that says nothing
//     was sent and gives the --headed retry.
//
// So an unheaded --into is Headless or sends nothing: a Batch counts such
// a step as Headless for its scope without reading the Element.

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
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
	if out, rerr, ok := s.onPage(a.Window, target, func(w win.WindowInfo, index int, o *page.Origin) (string, *Error) {
		return s.pageTypeInto(ctx, w, index, o, target, a.Text, a.Headed)
	}); ok {
		return out, rerr
	}
	if a.Headed {
		return s.typeIntoHeaded(ctx, a.Window, target, a.Text)
	}
	w, rerr := resolveWindow(s, a.Window)
	if rerr != nil {
		return "", rerr
	}
	if locked, desktop := s.Locked(); locked {
		return "", nothingSent(lockedDesktopFailure(desktop))
	}
	index, rerr := target.resolve(s, w)
	if rerr != nil {
		return "", nothingSent(rerr)
	}
	edit, err := s.intoEdit(ctx, w, index)
	if err != nil {
		return "", nothingSent(elementError(err, w, index))
	}
	if edit.Writable() && !hasControlChars(a.Text) {
		return s.typeIntoEdit(ctx, w, edit, a.Text)
	}
	return "", intoRefusal(w, target, index, edit, a.Text)
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
	return nothingSent(Fail(fmt.Sprintf("type-text --into has no Headless route into element %d: it is neither a classic edit control nor a Page Element", index), retry))
}

// typeIntoHeaded is --into with headed: v1's pipeline (activate and
// verify the foreground), a left click on the Element's clickable point,
// then the text typed with real keys, as plain Headed type-text types it.
func (s Services) typeIntoHeaded(ctx context.Context, ref string, target elementTarget, text string) (string, *Error) {
	if s.Actions == nil {
		return "", Fail("type-text --into is unavailable: no UIA client", "")
	}
	return s.runAction(ctx, ref, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
		index, rerr := target.resolve(s, w)
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
		if err := s.TypeText(text, func() bool { return s.Esc.Stopped() || ctx.Err() != nil }); err != nil {
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
