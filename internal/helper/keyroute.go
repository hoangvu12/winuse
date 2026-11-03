package helper

// The keyboard routes (headless-parity spec, "Headless Return and Escape
// in classic dialogs" and "Headless typing into classic edit controls";
// ADR 0011, Decision 4). press-key routes by the live Window, not by its
// arguments alone: Return (or KP_Enter) and Escape, with no modifiers, on
// a classic dialog (#32770) are Headless. The
// dialog manager answers those keys with a WM_COMMAND it sends itself, so
// winuse posts that same message: the default button's ID for Return,
// IDCANCEL for Escape. type-text routes the same way: text with no
// control characters into a focused classic edit control is inserted with
// the control's own EM_REPLACESEL and verified by its length. Every other
// key and text, and every other Window, stays Headed exactly as before.
//
// The route is decided from the Window probe (Services.KeyRoute) after the
// Window resolves and before anything is sent; winuse never switches mode
// after something was sent, and never retries in the other mode. A
// Headless key route runs runKeyHeadless: runHeadless's refusals (locked
// desktop, administrator Window, Ready), the passive Esc scope and the
// Agent Cursor travelling to a screen rectangle (the button or the edit
// control) rather than an Element.
//
// Posted Keys (`post`, ADR 0011, Decision 5) ride the same pipeline, Ready
// check included (a posted key would land in an owner disabled behind its
// modal dialog): the agent opts in per command, and winuse posts
// WM_KEYDOWN/WM_KEYUP or WM_CHAR to the Window's focused control. They are
// neither Headless nor Headed: best effort, reported as posted.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/hoangvu12/winuse/internal/keys"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// dialogKey is a classic dialog's Headless Return or Escape: the command
// ID to post and the button it presses (HWND 0 when the dialog has no
// control for that ID).
type dialogKey struct {
	key    string // the canonical key name, for the cursor label
	id     uint16
	button win.Control
}

// dialogKeyRoute is the pure dialog key decision: whether chord on the
// probed Window is a Headless Return or Escape, and what it presses. It
// mirrors the dialog manager (IsDialogMessage):
//
//   - Only a single key with no modifiers: Return, KP_Enter or Escape.
//   - Only on a classic dialog (#32770).
//   - Return with the focus in a multi-line edit that wants Return
//     (ES_WANTRETURN) stays Headed: the edit takes the key as a new line.
//   - Return with the focus on an enabled push button presses that button
//     (the dialog manager makes a focused push button the default while
//     it has the focus); otherwise the default button from DM_GETDEFID.
//     No default button, or a disabled one, stays Headed.
//   - Escape sends IDCANCEL, with the Cancel control when there is one; a
//     disabled Cancel button stays Headed (the dialog manager ignores
//     Escape then).
func dialogKeyRoute(chord keys.Chord, t win.KeyTarget) (dialogKey, bool) {
	if !t.Dialog || len(chord) != 1 {
		return dialogKey{}, false
	}
	k := chord[0]
	switch k.Name {
	case "Return", "KP_Enter":
		if t.Focus.WantsReturn() {
			return dialogKey{}, false
		}
		if t.Focus.PushButton() {
			if !t.Focus.Enabled {
				return dialogKey{}, false
			}
			return dialogKey{key: k.Name, id: t.Focus.ID, button: t.Focus}, true
		}
		if !t.HasDefault || (t.Default.HWND != 0 && !t.Default.Enabled) {
			return dialogKey{}, false
		}
		return dialogKey{key: k.Name, id: t.DefaultID, button: t.Default}, true
	case "Escape":
		if t.Cancel.HWND != 0 && !t.Cancel.Enabled {
			return dialogKey{}, false
		}
		return dialogKey{key: k.Name, id: win.IDCancel, button: t.Cancel}, true
	}
	return dialogKey{}, false
}

// mnemonicFree drops a button caption's mnemonic markers: "&OK" reads
// "OK", "&&" is a literal "&".
func mnemonicFree(text string) string {
	var b strings.Builder
	for i := 0; i < len(text); i++ {
		if text[i] == '&' {
			if i+1 < len(text) && text[i+1] == '&' {
				b.WriteByte('&')
				i++
			}
			continue
		}
		b.WriteByte(text[i])
	}
	return b.String()
}

// keyRun is one keyboard route's own part of runKeyHeadless.
type keyRun struct {
	label  string   // the Agent Cursor's label
	target win.Rect // where the cursor travels: the button or control; empty means the Window itself
	act    func() error
	// posted marks Posted Keys: the posted output instead of `ok`.
	posted bool
	// prepare, when set, runs after the refusals and before the cursor's
	// travel: it fills in the label, the target and the act from the live
	// Window, or refuses with an Error that says nothing was sent
	// (invoke-menu reads the menu there, invokemenu.go).
	prepare func(run *keyRun) *Error
}

// notSentError marks a keyboard route's failure raised before anything
// reached the app (a message that could not be posted, a state read before
// the insert): the failure says "nothing was sent".
type notSentError struct {
	err  error
	hint string
}

func (e *notSentError) Error() string { return e.err.Error() }
func (e *notSentError) Unwrap() error { return e.err }

// runKeyHeadless is the Headless keyboard pipeline on a resolved Window:
// runHeadless's refusals (locked desktop, administrator Window, Ready),
// the run's prepare step when it has one, then, inside a passive Esc
// scope, the Agent Cursor's travel to the
// target, the act, and the ripple on a delivered Action. No activation,
// no banner, no SendInput. The Esc checkpoints match runHeadless's.
func (s Services) runKeyHeadless(ctx context.Context, w win.WindowInfo, run keyRun) (string, *Error) {
	noteRoute(ctx, w, keyRunRoute(run.posted)) // trace.go
	if rerr := s.headlessRefusals(w, adminMessageHint); rerr != nil {
		return "", rerr
	}
	if run.prepare != nil {
		if perr := run.prepare(&run); perr != nil {
			return "", perr
		}
	}
	esc := s.Esc
	if esc == nil {
		esc = noopEscWatch{}
	}
	defer armPassive(esc)()

	rect, shown := s.cursorTravelRect(w, run.target, run.label)
	if esc.Stopped() {
		return "", escStopBeforeActError()
	}
	err := run.act()
	var notSent *notSentError
	if shown && !errors.As(err, &notSent) {
		// Delivered, or may have been: the ripple. A route that sent
		// nothing shows none.
		s.cursorClick(rect)
	}
	if esc.Stopped() {
		return "", escStopError()
	}
	if err != nil {
		return "", keyFailure(err, w)
	}
	if run.posted {
		return postedOutput, nil
	}
	return okOutput, nil
}

// keyFailure maps a keyboard route's failure: nothing was sent (a
// notSentError), delivered with a failed read-back (a uia.VerifyError:
// do not repeat), or anything else, which may have taken effect.
func keyFailure(err error, w win.WindowInfo) *Error {
	id := strconv.FormatUint(w.ID, 10)
	var notSent *notSentError
	if errors.As(err, &notSent) {
		return nothingSent(Fail(err.Error(), notSent.hint))
	}
	var verify *uia.VerifyError
	if errors.As(err, &verify) {
		return Fail(err.Error(), "the Action was delivered; do not repeat it, read the Window State: winuse get-window-state "+id)
	}
	return Fail(err.Error(), "the Action may have taken effect; read the Window State (winuse get-window-state "+id+") before repeating it")
}

// pressDialogKey runs a Headless Return or Escape on a classic dialog:
// the Agent Cursor travels to the button, labelled `press Return "OK"`,
// and the dialog's WM_COMMAND is posted. There is nothing to read back:
// the output is `ok`.
func (s Services) pressDialogKey(ctx context.Context, w win.WindowInfo, dk dialogKey) (string, *Error) {
	return s.runKeyHeadless(ctx, w, keyRun{
		label:  cursorLabel("press "+dk.key, mnemonicFree(dk.button.Text)),
		target: dk.button.Rect,
		act: func() error {
			if err := s.DialogCommand(w.ID, dk.id, dk.button.HWND); err != nil {
				return &notSentError{err: err, hint: "the dialog may have closed; run winuse list-windows"}
			}
			return nil
		},
	})
}

// editTypeRoute is the pure type-text decision (headless-parity spec,
// "Headless typing into classic edit controls"): the text goes Headless
// into the probed Window's focused control when that control is an
// enabled, writable classic edit control (Edit, RichEdit or
// RichEditD2DPT, not read-only) and the text has no control characters.
// A newline, a tab or any other C0 character routes Headed, where it is
// typed as the real key it stands for.
func editTypeRoute(text string, t win.KeyTarget) (win.Control, bool) {
	if text == "" || hasControlChars(text) || !t.Focus.Writable() {
		return win.Control{}, false
	}
	return t.Focus, true
}

// hasControlChars reports whether text holds a C0 control character
// (U+0000 to U+001F: \n, \r, \t and the rest).
func hasControlChars(text string) bool {
	for _, r := range text {
		if r < 0x20 {
			return true
		}
	}
	return false
}

// insertLengthOK is the pure verification of a Headless insert: after
// EM_REPLACESEL, the control's length must be the length before, minus
// the selection it replaced, plus the text's length in UTF-16 units.
// When EM_GETSEL could not tell the selection (either end past 65,535),
// the check is the bound that still holds: the length grew by at most the
// text and is at least the text.
func insertLengthOK(before win.EditState, after, units int) bool {
	if !before.SelKnown {
		return after <= before.Length+units && after >= units
	}
	return after == expectedLength(before, units)
}

// typeIntoEdit runs a Headless type-text into a classic edit control: the
// Agent Cursor travels to the control labelled `type` (never the text,
// which could be a password), the state before is read, the text is
// inserted with EM_REPLACESEL, and the length is read back. A failed read
// before sends nothing; a failed insert may have taken effect; a failed or
// mismatched read-back was delivered. No failure echoes the text.
func (s Services) typeIntoEdit(ctx context.Context, w win.WindowInfo, edit win.Control, text string) (string, *Error) {
	units := len(utf16.Encode([]rune(text)))
	return s.runKeyHeadless(ctx, w, keyRun{
		label:  "type",
		target: edit.Rect,
		act: func() error {
			before, err := s.EditState(edit.HWND)
			if err != nil {
				return &notSentError{err: err, hint: "the edit control may have closed or be busy; read the Window State and retry"}
			}
			if err := s.ReplaceSel(edit.HWND, text); err != nil {
				return err
			}
			after, err := s.EditState(edit.HWND)
			if err != nil {
				return &uia.VerifyError{Msg: "the text was delivered but the edit control's length could not be read back"}
			}
			if !insertLengthOK(before, after.Length, units) {
				return &uia.VerifyError{Msg: fmt.Sprintf(
					"the text was delivered but the edit control holds %d characters afterwards, not the %d expected (it may limit its length)",
					after.Length, expectedLength(before, units))}
			}
			return nil
		},
	})
}

// expectedLength is the length insertLengthOK expects, for the message
// (with an unknown selection, the length before plus the text).
func expectedLength(before win.EditState, units int) int {
	if !before.SelKnown {
		return before.Length + units
	}
	sel := before.SelEnd - before.SelStart
	if sel < 0 {
		sel = -sel
	}
	return before.Length - sel + units
}

// postedOutput is what every Posted Keys success prints: a posted message
// has no readable outcome, so the agent checks the Window State.
const postedOutput = "ok (posted; the app may ignore posted keys, read the Window State to check)\n"

// postFlagsError is the refusal of `post` together with `headed`: the
// CLI catches it as a usage error first; the helper refuses it for any
// other client, and a Batch refuses such a step while it prepares its
// steps (batch.go), before any step acts. The commands' own check stays
// for standalone requests and as defence in depth.
func postFlagsError(cmd string) *Error {
	return Fail(cmd+": --post and --headed cannot be combined",
		"--post posts keys to the Window without the foreground; --headed sends real keys in the foreground; pass one of them")
}

// modifierRefusal refuses a posted chord with a modifier before anything
// is posted: posted messages do not change the keyboard state that apps
// read with GetKeyState, so Control_L+a would arrive as a plain a.
func modifierRefusal(chord keys.Chord) *Error {
	return nothingSent(Fail("press-key --post cannot send "+chord.String()+": posted keys carry no modifiers (Control, Alt, Shift)",
		"send the chord without --post (this takes the foreground), or use set-value or perform-secondary-action on the Element instead"))
}

// postedModifier reports whether a chord holds a modifier key.
func postedModifier(chord keys.Chord) bool {
	for _, k := range chord {
		if k.Modifier() {
			return true
		}
	}
	return false
}

// postKeys runs Posted Keys (ADR 0011, Decision 5; headless-parity spec,
// "Posted keys, opt-in"): the Window resolves, its focused control is
// read (GetGUIThreadInfo hwndFocus through the probe, falling back to the
// Window itself), and post sends the messages to it inside
// runKeyHeadless's refusals and passive Esc scope. The Agent Cursor shows
// the focused control with label. A post that sent no message says
// "nothing was sent"; a partial post may have taken effect. The output is
// postedOutput: there is nothing to read back.
func (s Services) postKeys(ctx context.Context, ref, label string, post func(target uint64) (int, error)) (string, *Error) {
	w, rerr := resolveWindow(s, ref)
	if rerr != nil {
		return "", rerr
	}
	target, rect := w.ID, win.Rect{}
	if s.KeyRoute != nil {
		if f := s.KeyRoute(w.ID).Focus; f.HWND != 0 {
			target, rect = f.HWND, f.Rect
		}
	}
	return s.runKeyHeadless(ctx, w, keyRun{
		label:  label,
		target: rect,
		posted: true,
		act: func() error {
			n, err := post(target)
			if err != nil && n == 0 {
				return &notSentError{err: err, hint: "the Window or its focused control may have closed; run winuse list-windows"}
			}
			return err
		},
	})
}
