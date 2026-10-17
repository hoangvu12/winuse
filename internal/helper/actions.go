package helper

// The Action commands (ticket 05): click by Element Index, and
// activate-window standalone. They establish the output contract every
// later Action ticket (06-08, 10, 11) reuses: `ok`, or a one-line failure
// with a recovery hint, non-zero exit on failure, the shared request
// timeouts.
//
// Every Action runs the same pipeline through Services.runAction: resolve
// the Window, refuse a locked desktop, refuse an administrator Window,
// activate the Window and verify it is in the foreground, then run the
// action's own step. Input is injected only after the pipeline passes.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// okOutput is what every successful Action prints (spec, output
// contract): just "ok".
const okOutput = "ok\n"

// clickUsageHint is the recovery hint for click's argument failures.
const clickUsageHint = "usage: winuse click <window> <element index | name:<text> | id:<id> | @x,y> [--count 1|2|3] [--button left|middle|right]"

// click runs `winuse click <window> <element index>`: the Window is
// activated and verified in the foreground, the Element behind the index
// is scrolled into view if needed, and the click (single, double, right)
// is injected at its clickable point or bounds centre.
//
// `winuse click <window> name:<text>` and `id:<AutomationId>` (ticket 11)
// name the Element by its Name or AutomationId instead of its Index: the
// reference resolves against the Window's latest Window State to one
// Element Index (several matches are an error listing the candidates,
// never a guess), and the click then runs exactly like the Index form —
// so a reference still fails as stale when its Element is gone.
//
// `winuse click <window> @x,y` (ticket 08) aims at a coordinate of the
// Window's latest screenshot instead: the Mapping turns the screenshot
// pixel into the Window's physical pixel, anchored at the extended frame
// bounds re-read at click time, so the click lands exactly where aimed at
// any display scaling, on any monitor, and survives the Window moving
// since the screenshot.
func click(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.ClickArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("click: "+err.Error(), clickUsageHint)
		}
		// Exactly one Element Reference form: an Element Index, a name:/id:
		// reference, or @x,y coordinates.
		if forms := countRefForms(a.Point != nil, a.ElementIndex != nil, a.Ref != ""); forms > 1 {
			return "", Fail("click: pass an element index, a name:/id: reference or @x,y coordinates, not several", clickUsageHint)
		}
		count := a.ClickCount
		if count == 0 {
			count = 1
		}
		if count < 1 || count > 3 {
			return "", Fail("click_count must be 1, 2 or 3", "1 clicks, 2 double-clicks, 3 triple-clicks")
		}
		button, ok := parseMouseButton(a.MouseButton)
		if !ok {
			return "", Fail("unsupported mouse_button: "+a.MouseButton,
				"mouse_button is one of left, right, middle (aliases: l, r, m)")
		}
		if button != win.MouseLeft && count > 1 {
			// Codex: "right double click is not supported" — extended to
			// the middle button and triple clicks.
			return "", Fail(fmt.Sprintf("%s %s click is not supported", buttonWord(button), clickWord(count)), "")
		}

		if a.Point != nil {
			// The coordinate form (@x,y, ticket 08).
			if a.Point.X < 0 || a.Point.Y < 0 {
				return "", Fail("click: @x,y coordinates must be non-negative integers", clickUsageHint)
			}
			x, y := a.Point.X, a.Point.Y
			return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
				pt, serr := shotPoint(s, w, x, y)
				if serr != nil {
					return "", serr
				}
				if err := s.Click(w.ID, int32(pt.X), int32(pt.Y), button, count); err != nil {
					return "", inputError(err)
				}
				return okOutput, nil
			})
		}

		// The Element forms: an Element Index (ticket 05) or a name:/id:
		// reference (ticket 11), resolved against the Window's latest
		// Window State to the Element Index to click.
		if a.ElementIndex == nil && a.Ref == "" {
			return "", Fail("click: an element index, a name:/id: reference or @x,y coordinates are required",
				"read a Window's Elements with winuse get-window-state <window> and click one by its Element Index, name: or id: reference, or click @x,y from its latest screenshot")
		}
		if a.ElementIndex != nil && *a.ElementIndex < 0 {
			return "", Fail("click: the element index must be a non-negative integer", clickUsageHint)
		}
		if s.Actions == nil {
			return "", Fail("click is unavailable: no UIA client", "")
		}

		index, ref := -1, a.Ref
		if a.ElementIndex != nil {
			index = *a.ElementIndex
		}
		return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
			if ref != "" {
				i, rerr := resolveElementRef(s, w, ref)
				if rerr != nil {
					return "", rerr
				}
				index = i
			}
			pt, err := s.Actions.ElementPoint(ctx, w.ID, index)
			if err != nil {
				return "", elementError(err, w, index)
			}
			if err := s.Click(w.ID, pt.X, pt.Y, button, count); err != nil {
				return "", inputError(err)
			}
			return okOutput, nil
		})
	}
}

// activateWindow runs `winuse activate-window <window>`: the shared
// Action pipeline with the activation itself as the step — the same
// activation every other Action performs first (spec: an explicit escape
// hatch for recovering focus).
func activateWindow(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.ActivateArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("activate-window: "+err.Error(), "usage: winuse activate-window <window>")
		}
		return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
			return okOutput, nil
		})
	}
}

// runAction is the Action pipeline every Action command runs: resolve the
// Window (a Window ID, or an App name when that App has exactly one
// Window); refuse a locked desktop; refuse an administrator Window
// (Windows would silently drop the input); activate the Window with the
// standard foreground-lock workarounds and verify it is in the
// foreground; then run the action's own step with the resolved Window.
// Later Action tickets plug their step in through act.
func (s Services) runAction(ctx context.Context, ref string, act func(ctx context.Context, w win.WindowInfo) (string, *Error)) (string, *Error) {
	w, rerr := resolveWindow(s, ref)
	if rerr != nil {
		return "", rerr
	}
	if locked, desktop := s.Locked(); locked {
		msg := "Windows desktop is locked; unlock before using winuse"
		// A WTS-locked session may still sit on the Default desktop; the
		// parenthetical names the desktop only when it is not Default (the
		// lock screen's LockApp, the UAC secure desktop).
		if desktop != "" && desktop != "Default" {
			msg = fmt.Sprintf("Windows desktop is locked (input desktop is %s); unlock before using winuse", desktop)
		}
		return "", Fail(msg, "stop and ask the user to unlock the desktop, then read a fresh Window State before acting")
	}
	if err := s.Integrity(w); err != nil {
		var admin *win.AdminWindowError
		if errors.As(err, &admin) {
			return "", Fail(err.Error(),
				"Windows silently drops input from a lower-integrity process; start winuse from an elevated terminal to control administrator Windows, or ask the user to operate this Window")
		}
		return "", Fail(err.Error(), "")
	}
	if err := s.Activate(w.ID); err != nil {
		return "", activationError(err)
	}
	return act(ctx, w)
}

// elementError maps an element-side Action failure to the user-facing
// line. Stale Element Indexes instruct the agent to read the Window State
// again (the spec's stale contract, user story 43).
func elementError(err error, w win.WindowInfo, index int) *Error {
	switch {
	case errors.Is(err, uia.ErrNoState):
		return Fail(fmt.Sprintf("no cached Window State is available for window %d", w.ID),
			"run winuse get-window-state "+strconv.FormatUint(w.ID, 10)+" to read the Window's Elements before acting on them")
	case errors.Is(err, uia.ErrStale):
		return Fail(fmt.Sprintf("element %d is not available in the cached Window State for window %d", index, w.ID),
			"the Window State has changed; run winuse get-window-state "+strconv.FormatUint(w.ID, 10)+" again and use fresh Element Indexes")
	}
	return Fail(err.Error(), "")
}

// activationError maps a foreground-activation failure to the user-facing
// line; a blocked modal dialog points at the dialog's Window.
func activationError(err error) *Error {
	var modal *win.ModalDialogError
	if errors.As(err, &modal) {
		return Fail(err.Error(),
			"run winuse get-window-state "+strconv.FormatUint(modal.DialogID, 10)+" to read the dialog's Elements and act on the dialog")
	}
	var fg *win.NotForegroundError
	if errors.As(err, &fg) {
		return Fail(err.Error(),
			"another window may hold the foreground; run winuse list-windows to check for a dialog of the same App, then retry")
	}
	return Fail(err.Error(), "")
}

// inputError maps an input-injection failure to the user-facing line; a
// click aimed at a covered point tells the agent what covers it.
func inputError(err error) *Error {
	var occluded *win.OcclusionError
	if errors.As(err, &occluded) {
		return Fail(err.Error(), "another window covers the target Element; dismiss or move it, then read a fresh Window State and retry")
	}
	return Fail(err.Error(), "")
}

// countRefForms counts how many Element Reference forms were supplied
// (an Action taking one must reject any combination).
func countRefForms(point, index, ref bool) int {
	n := 0
	for _, b := range []bool{point, index, ref} {
		if b {
			n++
		}
	}
	return n
}

// parseMouseButton canonicalizes Codex's mouse_button values; "" means
// the default, left.
func parseMouseButton(s string) (win.MouseButton, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "left", "l":
		return win.MouseLeft, true
	case "right", "r":
		return win.MouseRight, true
	case "middle", "m":
		return win.MouseMiddle, true
	}
	return 0, false
}

// buttonWord names a button in user-facing messages.
func buttonWord(b win.MouseButton) string {
	switch b {
	case win.MouseRight:
		return "right"
	case win.MouseMiddle:
		return "middle"
	}
	return "left"
}

// clickWord names a click count in user-facing messages.
func clickWord(count int) string {
	if count == 2 {
		return "double"
	}
	return "triple"
}
