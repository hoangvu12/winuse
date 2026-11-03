package helper

// The keyboard Action commands (ticket 06): `winuse type-text` and
// `winuse press-key`. They plug into the Action pipeline ticket 05 built
// (resolve the Window, refuse a locked desktop and an administrator
// Window, activate and verify the foreground, then act) and keep the
// output contract: `ok`, or a one-line failure with a recovery hint. Key
// names and text are validated before the pipeline runs, so a bad key
// never activates the Window or sends input. The headless-parity tickets
// add the routes that need no foreground (keyroute.go).

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/hoangvu12/winuse/internal/keys"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// typeTextUsageHint and pressKeyUsageHint are the recovery hints for the
// commands' argument failures.
const (
	typeTextUsageHint = "usage: winuse type-text <window> [--post | --into <element>] <text>"
	pressKeyUsageHint = "usage: winuse press-key <window> <key> [--post] [--force]"
)

// typeText runs `winuse type-text <window> <text>`. With `headed`, the
// Window is activated and verified in the foreground, then the text is
// typed into
// whatever holds keyboard focus, as Unicode key events — any script and
// emoji, newline as a real Return, tab as a real Tab. The abort
// checkpoint rides along: typing aborts between its batches when the
// user pressed Esc — the abort reports the exit-130 stop — or when
// the request's deadline expires (finding 10).
//
// Text with no control characters into a focused classic edit control is
// Headless instead (ADR 0011, Decision 4; keyroute.go): the Window
// resolves, the probe reads its focused control, and the shared predicate
// decides the route before anything is sent. With `headed` the probe is
// skipped and the typing is v1's. With `post` the text is posted to the
// focused control as Posted Keys (Decision 5). With an Element (--into)
// the text goes into that Element instead (typeinto.go).
//
// Without `headed`, text with no Headless route is refused with nothing
// sent and the --headed retry (headless-max ticket 21); when the probe
// finds no focused control at all (a covered Windows 11 Notepad reports
// none), the refusal says so and points at --into (ticket 19).
func typeText(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.TypeTextArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("type-text: "+err.Error(), typeTextUsageHint)
		}
		if a.Text == "" {
			return "", Fail("type-text: text is required",
				"pass the text as one argument; quote it if it contains spaces")
		}
		if typesInto(a) {
			return s.typeTextInto(ctx, a)
		}
		if a.Post {
			// Posted Keys (ADR 0011, Decision 5): WM_CHAR per UTF-16 unit
			// to the focused control, no foreground.
			if a.Headed {
				return "", postFlagsError("type-text")
			}
			return s.postKeys(ctx, a.Window, "type", func(_ win.WindowInfo, target uint64) (int, error) {
				return s.PostText(target, a.Text)
			})
		}
		if !a.Headed {
			// The Page holds the focus: typing goes over the Browser
			// Connection (pageactions.go).
			if out, rerr, ok := s.pageTypeText(ctx, a.Window, a.Text); ok {
				return out, rerr
			}
		}
		if !a.Headed {
			w, rerr := resolveWindow(s, a.Window)
			if rerr != nil {
				return "", rerr
			}
			// headlessStep's type-text case for an unposted, unheaded
			// request: the route read once, then editTypeRoute. With no
			// Headless route the text is refused, never typed Headed
			// (headless-max ticket 21): the reason names a Window that
			// reports no focused control, such as a covered Windows 11
			// Notepad, and the alternatives are --into and --post.
			route := s.keyRoute(args, w)
			if route != nil {
				if edit, ok := editTypeRoute(a.Text, *route); ok {
					return s.typeIntoEdit(ctx, w, edit, a.Text)
				}
			}
			return "", headedRefusal(w, typeTextWhy(w, a.Text, route),
				func(id string) string { return "type-text " + id + " <text>" },
				typeTextAlt(strconv.FormatUint(w.ID, 10)))
		}
		return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
			// The abort checkpoint polls the Esc stop and the request's
			// deadline (finding 10): typing stops between its batches when
			// either fires.
			if err := s.TypeText(a.Text, func() bool { return s.Esc.Stopped() || ctx.Err() != nil }); err != nil {
				return "", inputAbortError(s, ctx, err, "while typing (the text injected so far landed)")
			}
			return okOutput, nil
		})
	}
}

// pressKey runs `winuse press-key <window> <key>`. With `headed`, the
// Window is activated and verified in the foreground, then the key (or
// chord, keys down in order and up in reverse) is injected.
//
// Return and Escape on a classic dialog are Headless instead (ADR 0011,
// Decision 4; keyroute.go): the Window resolves, the probe reads it, and
// the shared predicate decides the route before anything is sent; any
// other key without `headed` is refused with nothing sent and the --headed
// retry (headless-max ticket 21). With `headed` the probe is skipped and
// the press is v1's. With `post` the key is posted to the focused control
// as Posted Keys (Decision 5), refused with modifiers. A Blocked Combo is refused before any of these routes
// unless `force` (blockedComboError).
func pressKey(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.PressKeyArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("press-key: "+err.Error(), pressKeyUsageHint)
		}
		chord, err := keys.Parse(a.Key)
		if err != nil {
			return "", keyError(err)
		}
		if e := blockedComboError(chord, a.Force); e != nil {
			return "", e
		}
		if a.Post {
			// Posted Keys (ADR 0011, Decision 5): WM_KEYDOWN and WM_KEYUP
			// to the focused control, no foreground; a chord with
			// modifiers through attached key state (postChord).
			if a.Headed {
				return "", postFlagsError("press-key")
			}
			if postedModifier(chord) {
				// A chord with Control, Shift or Alt: the attached key
				// state route (headless-max ticket 07), planned once here
				// so a chord it cannot post is refused before anything. A
				// Blocked Combo is never posted, --force or not: a posted
				// Alt+F4 would close the Window behind the user's back.
				if blockedComboError(chord, false) != nil {
					return "", blockedPostRefusal(chord)
				}
				if _, err := win.PlanPostedChord(chord, ""); err != nil {
					return "", modifierRefusal(chord, err)
				}
				return s.postKeys(ctx, a.Window, "press "+chord.String(), func(w win.WindowInfo, target uint64) (int, error) {
					return s.postChord(w, target, chord)
				})
			}
			return s.postKeys(ctx, a.Window, "press "+chord.String(), func(_ win.WindowInfo, target uint64) (int, error) {
				return s.PostKeys(target, chord)
			})
		}
		if out, rerr, ok := s.pressOverflowEscape(ctx, args, a.Window); ok {
			return out, rerr // Escape on the tray overflow (shell.go)
		}
		if !a.Headed {
			// The Page holds the focus: the key goes over the Browser
			// Connection (pageactions.go).
			if out, rerr, ok := s.pagePressKey(ctx, a.Window, chord); ok {
				return out, rerr
			}
		}
		if !a.Headed {
			w, rerr := resolveWindow(s, a.Window)
			if rerr != nil {
				return "", rerr
			}
			// headlessStep's press-key case for an unposted, unheaded
			// request: the route read once, then dialogKeyRoute. With no
			// Headless route the key is refused, never pressed Headed
			// (headless-max ticket 21); a key with no modifier can be posted.
			if route := s.keyRoute(args, w); route != nil {
				if dk, ok := dialogKeyRoute(chord, *route); ok {
					return s.pressDialogKey(ctx, w, dk)
				}
			}
			force := ""
			if a.Force {
				force = " --force"
			}
			return "", headedRefusal(w, pressKeyWhy(w, chord),
				func(id string) string { return "press-key " + id + " " + chord.String() + force },
				pressKeyAlt(strconv.FormatUint(w.ID, 10), chord))
		}
		return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
			if err := s.PressKeys(chord); err != nil {
				return "", inputError(err)
			}
			return okOutput, nil
		})
	}
}

// blockedCombos are the Blocked Combos' effects and recovery hints, by
// keys.Blocked's name.
var blockedCombos = map[string]struct{ does, hint string }{
	"Alt+F4": {"closes the foreground window",
		"close the Window with winuse close-window <window>; pass --force only to send Alt+F4 itself"},
	"Alt+Tab": {"switches the user's foreground window",
		"use winuse activate-window <window> to bring a Window forward; pass --force only to send Alt+Tab itself"},
	"Ctrl+Shift+Escape": {"opens Task Manager",
		"pass --force only when the user asked for Task Manager"},
}

// blockedComboError refuses a Blocked Combo (Alt+F4, Alt+Tab,
// Ctrl+Shift+Escape or a superset, keys.Blocked) unless force, before any
// route is chosen: Headed, Posted or the Page alike, since on a Page Alt+F4
// and Ctrl+Shift+Escape reach the browser frame, not the Page. nil when the
// chord may be sent.
func blockedComboError(chord keys.Chord, force bool) *Error {
	name, blocked := keys.Blocked(chord)
	if !blocked || force {
		return nil
	}
	c := blockedCombos[name]
	return nothingSent(Fail("press-key refuses "+chord.String()+": "+name+" "+c.does, c.hint))
}

// keyError maps a key-name parse failure to the user-facing line with its
// recovery hint. The messages are Codex's: an empty key is "key is
// required" and an unknown name is "unsupported key: <name>".
func keyError(err error) *Error {
	var ke *keys.Error
	if !errors.As(err, &ke) {
		return Fail(err.Error(), "")
	}
	switch ke.Kind {
	case keys.ErrEmpty:
		return Fail("key is required", "pass a key name such as Return, Tab or Escape, or a chord such as Control_L+a")
	case keys.ErrWindowsKey:
		return Fail(err.Error(),
			"winuse never sends the Windows key (it would open the user's Start menu); Control_L+Escape opens Start instead")
	default: // ErrUnknown
		return Fail(err.Error(),
			"key names are X keysym-style: a, 3, space, Return, Tab, Escape, F1-F20, period, Control_L+a, Numpad_0; whitespace around + is ignored")
	}
}
