package helper

// The Action commands (ticket 05): click by Element Index, and
// activate-window standalone. They establish the output contract every
// later Action ticket (06-08, 10, 11) reuses: `ok`, or a one-line failure
// with a recovery hint, non-zero exit on failure, the shared request
// timeouts.
//
// Every Headed Action runs the same pipeline through Services.runAction:
// resolve the Window, refuse a locked desktop, refuse an administrator
// Window, activate the Window and verify it is in the foreground, then
// run the action's own step. Input is injected only after the pipeline
// passes. Element Actions are Headless by default instead (ADR 0010,
// headless.go): a click on an Element Index or name:/id: reference (left,
// single) runs Services.runHeadless — the same refusals, no activation,
// no input, a pattern call. A left single click at `@x,y` resolves the
// point to the target Window's own Element and clicks it the same way
// (pointroute.go, headless-max ticket 11). Double/triple and right/middle
// clicks sent with `headed`, any click with `headed`, and activate-window
// keep runAction; without `headed` the others are refused with nothing
// sent (headedgate.go, ADR 0013 rule 3).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// okOutput is what every successful Action prints (spec, output
// contract): just "ok".
const okOutput = "ok\n"

// clickUsageHint is the recovery hint for click's argument failures.
const clickUsageHint = "usage: winuse click <window> <element index | name:<text> | id:<id> | @x,y> [--count 1|2|3] [--button left|middle|right]"

// escStopMessage is the single user-facing line every Esc stop reports
// (orchestrator decision, decisions.md: the message says the user stopped
// winuse with the physical Escape key, and tells the agent to stop and
// ask — Codex's USER_INTERRUPT_MESSAGE with winuse's wording).
const escStopMessage = "winuse was stopped by the user with the physical Escape key"

// escStopHint tells the agent what to do with a stop: stop and ask, and
// treat the interrupted Action's effect as unknown (input already sent
// before the press landed).
const escStopHint = "stop your current task and ask the user how to proceed before sending more winuse commands; the interrupted Action may have partly taken effect, so re-read a Window State before acting again"

// escStopError is the failure every Esc stop reports: Codex's interrupt
// exit code (130) with the user-stopped-winuse message.
func escStopError() *Error {
	return &Error{Msg: escStopMessage, Hint: escStopHint, Exit: protocol.ExitEsc}
}

// click runs `winuse click <window> <element index>`. A left single click
// on an Element is Headless (ADR 0010): the Element is clicked through
// its first offered pattern (Invoke, Toggle, Select, Expand/Collapse,
// LegacyIAccessible default action) on a Window that stays where it is,
// and an Element with none fails with nothing sent and a --headed hint.
// Headed (`headed`), the Window is activated and verified in the
// foreground, the Element behind the index is scrolled into view if
// needed, and the click is injected at its clickable point or bounds
// centre. A double/triple or right/middle click outside a Page, and the
// @x,y form, have no other route: without `headed` they are refused with
// nothing sent and the --headed retry (headless-max ticket 21).
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

		// Modifiers held during the click (desktop-parity ticket 06,
		// modifiers.go).
		mods, merr := win.ParseModifiers(a.Modifiers)
		if merr != nil {
			return "", Fail("click: "+merr.Error(), clickUsageHint)
		}

		// Exactly one Element Reference form: an Element Index, a name:/id:
		// reference, or @x,y coordinates (the shared guard, finding 16).
		target, terr := parseElementTarget("click", a.ElementIndex, a.Ref, a.Point, clickUsageHint)
		if terr != nil {
			return "", terr
		}
		if a.Post {
			// Posted Clicks to the classic control there, opt-in
			// (headless-max ticket 12, postclick.go).
			return s.postClick(ctx, a, target, button, count, mods)
		}
		// A Page Element routes over the Browser Connection (browser merge
		// ticket 05, pageactions.go).
		if out, rerr, ok := s.onPage(ctx, a.Window, target, func(w win.WindowInfo, index int, o *page.Origin) (string, *Error) {
			return s.pageClick(ctx, w, index, o, target, button, count, a.Headed, mods)
		}); ok {
			return out, rerr
		}
		// An item of an open classic menu: its mnemonic or WM_COMMAND, never
		// UIA Invoke (headless-max ticket 09, menuitem.go).
		if mods.None() {
			if out, rerr, ok := s.classicMenuClick(ctx, a, target, button, count); ok {
				return out, rerr
			}
		}
		if !mods.None() && !a.Headed {
			// Ctrl+click or Shift+click on a multi-select list item, Headless;
			// anything else with modifiers needs the real keys.
			if out, rerr, ok := s.multiSelectClick(ctx, a.Window, target, button, count, mods); ok {
				return out, rerr
			}
			at := target.token()
			if target.point != nil {
				at = pointToken(target.point.X, target.point.Y)
			}
			return "", s.modifierRefusal(a.Window, "click", mods,
				func(id string) string {
					return "click " + id + " " + at + clickOptions(button, count) + modsOption(mods)
				})
		}

		if target.point != nil {
			// The coordinate form (@x,y, ticket 08): input at the point
			// itself.
			x, y := target.point.X, target.point.Y
			retry := func(id string) string { return "click " + id + " " + pointToken(x, y) + clickOptions(button, count) }
			if actor, pr, ok := s.pointActor(); ok && !a.Headed && button == win.MouseLeft && count == 1 {
				// A left single click at a point clicks the target Window's
				// own Element there, Headless (headless-max ticket 11).
				return s.runAtPoint(ctx, a.Window, actor, pr, pointRun{
					x: x, y: y, need: uia.NeedClick, done: "clicked", retry: retry,
					run: headlessRun{
						cmd:  "click",
						verb: "click",
						act: func(ctx context.Context, w win.WindowInfo, index int) error {
							_, err := actor.HeadlessClick(ctx, w.ID, index)
							return err
						},
						refusal: clickRefusal,
					},
				})
			}
			if !a.Headed {
				// A Chromium or Electron Window: mouse messages at the point
				// (headless-max ticket 20, chromium.go).
				if out, rerr, ok := s.chromiumClick(ctx, a.Window, target, button, count); ok {
					return out, rerr
				}
				return "", s.headedOnly(a.Window,
					fmt.Sprintf("click at %s has no background route: a click at a point needs the real pointer", pointToken(x, y)),
					retry,
					"to click without the foreground, name the Element by its Element Index, name: or id: from winuse get-window-state")
			}
			return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
				pt, serr := shotPoint(ctx, s, w, x, y, "click")
				if serr != nil {
					return "", serr
				}
				if err := s.heldInput(mods, func() error { return s.Click(w.ID, int32(pt.X), int32(pt.Y), button, count) }); err != nil {
					return "", inputError(err)
				}
				return okOutput, nil
			})
		}

		// The Element forms: an Element Index (ticket 05) or a name:/id:
		// reference (ticket 11), resolved against the Window's latest
		// Window State to the Element Index to click — Headless when the
		// shared predicate says so (headlessStep), Headed otherwise.
		if actor, ok := s.headless("click", args); ok {
			return s.runHeadless(ctx, a.Window, actor, headlessRun{
				cmd:       "click",
				verb:      "click",
				target:    target,
				minimized: true,
				act: func(ctx context.Context, w win.WindowInfo, index int) error {
					_, err := actor.HeadlessClick(ctx, w.ID, index)
					return err
				},
				refusal: clickRefusal,
			})
		}
		if s.Actions == nil {
			return "", Fail("click is unavailable: no UIA client", "")
		}
		if !a.Headed && (count > 1 || button != win.MouseLeft) {
			// A Chromium or Electron Window: mouse messages at the
			// Element's centre (headless-max ticket 20, chromium.go).
			if out, rerr, ok := s.chromiumClick(ctx, a.Window, target, button, count); ok {
				return out, rerr
			}
		}
		if !a.Headed {
			return "", s.headedOnly(a.Window,
				clickDescription(button, count)+" on an Element has no background route outside a Page: it needs the real pointer",
				func(id string) string { return "click " + id + " " + target.token() + clickOptions(button, count) }, "")
		}
		return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
			index, rerr := target.resolve(ctx, s, w)
			if rerr != nil {
				return "", rerr
			}
			pt, err := s.Actions.ElementPoint(ctx, w.ID, index)
			if err != nil {
				return "", elementError(err, w, index)
			}
			if err := s.heldInput(mods, func() error { return s.Click(w.ID, pt.X, pt.Y, button, count) }); err != nil {
				return "", inputError(err)
			}
			return okOutput, nil
		})
	}
}

// activateWindow runs `winuse activate-window <window>`: the shared
// Action pipeline with the activation itself as the step — the same
// activation every Headed Action performs first (spec: an explicit escape
// hatch for recovering focus; the headless spec's recovery for a
// minimized Window). Always Headed.
func activateWindow(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.ActivateArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("activate-window: "+err.Error(), "usage: winuse activate-window <window>")
		}
		return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
			keepForegroundHere(ctx) // the point of activate-window
			return okOutput, nil
		})
	}
}

// runAction is the Headed Action pipeline (every Action that is not
// Headless, ADR 0010; runHeadless in headless.go is the other): resolve the
// Window (a Window ID, or an App name when that App has exactly one
// Window); refuse a locked desktop; refuse an administrator Window
// (Windows would silently drop the input); activate the Window with the
// standard foreground-lock workarounds and verify it is in the
// foreground; then run the action's own step with the resolved Window.
// Later Action tickets plug their step in through act.
//
// The whole pipeline from the activation on is one armed Esc scope
// (ticket 12): while winuse is in control the physical Esc key is
// swallowed and latched, and the checkpoints — after the activation,
// around the act step's own abort points, and after it — turn a latched
// press into the exit-130 stop. The refusal checks run unarmed: winuse
// is not in control when it refuses.
func (s Services) runAction(ctx context.Context, ref string, act func(ctx context.Context, w win.WindowInfo) (string, *Error)) (result string, failure *Error) {
	w, rerr := resolveWindow(s, ref)
	if rerr != nil {
		return "", rerr
	}
	noteRoute(ctx, w, protocol.RouteHeaded) // trace.go
	if locked, desktop := s.Locked(); locked {
		return "", lockedDesktopFailure(desktop)
	}
	if err := s.Integrity(w); err != nil {
		var admin *win.AdminWindowError
		if errors.As(err, &admin) {
			return "", Fail(err.Error(),
				"Windows silently drops input from a lower-integrity process; start winuse from an elevated terminal to control administrator Windows, or ask the user to operate this Window")
		}
		return "", Fail(err.Error(), "")
	}
	// Polite real input (polite.go, multi-agent ticket 08): wait for the
	// user's input to go quiet, save their foreground and cursor (or join
	// the Batch's save), and on every exit path release what winuse holds
	// down and give the foreground and cursor back (a Note when it keeps
	// the foreground).
	if rerr := s.waitForQuietInput(ctx); rerr != nil {
		return "", rerr
	}
	ctx, polite, own := s.beginPolite(ctx, w)
	defer func() {
		s.releaseHeld()
		if !own {
			return
		}
		if note := s.giveBack(polite); failure == nil {
			result = withNotes(result, note)
		}
	}()
	// The in-control indicator (ticket 13): raised over the target Window
	// for as long as winuse controls it — the activation, the injected
	// input, the action's own step — then hidden again, whatever the
	// outcome. Best effort: an Action never fails over the indicator.
	s.showOverlay(w)
	defer s.hideOverlay()
	// The armed Esc scope: from here (winuse takes control) to the defer's
	// Disarm, the physical Esc key stops this Action.
	s.Esc.Arm()
	defer s.Esc.Disarm()
	markHeaded(ctx) // selfactivation.go: this foreground is winuse's own doing
	if err := s.Activate(w.ID); err != nil {
		return "", activationError(err)
	}
	// Confirm the Window's thread took the activation before any input
	// goes in (a WM_NULL round trip).
	if s.SyncWindow != nil {
		if err := s.SyncWindow(w.ID); err != nil {
			return "", nothingSent(Fail(err.Error()+" after it was brought to the front",
				"the App may be busy or hung; read the Window State, wait, and retry once"))
		}
	}
	if s.Esc.Stopped() {
		// The user pressed Esc during the activation: stop before any
		// input is injected.
		return "", stopErrorFor(s.Esc)
	}
	out, err := act(ctx, w)
	// The drain: the injected input handled before anything is given
	// back; then where winuse left the cursor.
	if s.SyncWindow != nil {
		_ = s.SyncWindow(w.ID)
	}
	s.noteLeft(polite)
	if s.Esc.Stopped() {
		// A press during the act step (its own abort points, or its tail):
		// the stop outranks the step's outcome — the user's signal is what
		// the agent must act on.
		return "", stopErrorFor(s.Esc)
	}
	return out, err
}

// lockedDesktopFailure builds the locked-desktop refusal (runAction's and
// the observation commands' screenshot paths share it): the shared
// message and stop-and-ask hint from win.LockedDesktopError, so the
// agent sees one locked-desktop contract everywhere — Action refusals,
// the helper's startup check and the CLI's early exit alike.
func lockedDesktopFailure(desktop string) *Error {
	return &Error{
		Msg:  (&win.LockedDesktopError{Desktop: desktop}).Error(),
		Hint: win.LockedDesktopHint,
		Exit: 1,
	}
}

// elementError maps an element-side Action failure to the user-facing
// line. Stale Element Indexes instruct the agent to read the Window State
// again (the spec's stale contract, user story 43).
func elementError(err error, w win.WindowInfo, index int) *Error {
	switch {
	case errors.Is(err, uia.ErrNoState):
		return noStateError(err, w)
	case errors.Is(err, uia.ErrStale):
		return Fail(fmt.Sprintf("element %d is not available in the cached Window State for window %d", index, w.ID),
			"the Window State has changed; run winuse get-window-state "+strconv.FormatUint(w.ID, 10)+" again and use fresh Element Indexes")
	case errors.Is(err, uia.ErrPageElement):
		return pageElementError(w, index)
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

// inputAbortError maps an input side that aborted mid-way — typing
// between its batches, a drag between its interpolation steps — on the
// Esc stop or the request's deadline (finding 10): the user's Esc
// outranks the deadline (the stop keeps its exit 130 even when the
// budget expired in the same instant, the server's policy), and a
// deadline abort is reported in the shared style (the server's central
// timeout policy rewrites it to "winuse request timed out: <command>").
// during names the input side ("while typing ...") for the message.
func inputAbortError(s Services, ctx context.Context, err error, during string) *Error {
	if s.Esc.Stopped() {
		return inputError(err)
	}
	if ctx.Err() != nil {
		return Fail("the request's budget expired "+during,
			"re-observe the Window and continue in a fresh request")
	}
	return inputError(err)
}

// inputError maps an input-injection failure to the user-facing line; a
// click aimed at a covered point tells the agent what covers it, and an
// injection aborted by the user's Esc press becomes the exit-130 stop.
func inputError(err error) *Error {
	if errors.Is(err, win.ErrEscStopped) {
		return escStopError()
	}
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
