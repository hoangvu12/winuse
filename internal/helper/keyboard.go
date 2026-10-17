package helper

// The keyboard Action commands (ticket 06): `winuse type-text` and
// `winuse press-key`. They plug into the Action pipeline ticket 05 built
// (resolve the Window, refuse a locked desktop and an administrator
// Window, activate and verify the foreground, then act) and keep the
// output contract: `ok`, or a one-line failure with a recovery hint. Key
// names and text are validated before the pipeline runs, so a bad key
// never activates the Window or sends input.

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/hoangvu12/winuse/internal/keys"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// typeTextUsageHint and pressKeyUsageHint are the recovery hints for the
// commands' argument failures.
const (
	typeTextUsageHint = "usage: winuse type-text <window> <text>"
	pressKeyUsageHint = "usage: winuse press-key <window> <key>"
)

// typeText runs `winuse type-text <window> <text>`: the Window is
// activated and verified in the foreground, then the text is typed into
// whatever holds keyboard focus, as Unicode key events — any script and
// emoji, newline as a real Return, tab as a real Tab. The Esc-stop
// checkpoint rides along: typing aborts between its batches when the
// user pressed Esc, and the abort reports the exit-130 stop.
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
		return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
			if err := s.TypeText(a.Text, s.Esc.Stopped); err != nil {
				return "", inputError(err)
			}
			return okOutput, nil
		})
	}
}

// pressKey runs `winuse press-key <window> <key>`: the Window is activated
// and verified in the foreground, then the key (or chord, keys down in
// order and up in reverse) is injected.
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
		return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
			if err := s.PressKeys(chord); err != nil {
				return "", inputError(err)
			}
			return okOutput, nil
		})
	}
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
