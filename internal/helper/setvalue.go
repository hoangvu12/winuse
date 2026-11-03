package helper

// The ticket-07 Action commands: set-value and perform-secondary-action.
// On an Element Index or name:/id: reference they are Headless by default
// (ADR 0010, headless.go: Services.runHeadless — the refusals, no
// activation, a focus-free pattern call read back where it can be);
// Raise, the @x,y forms and anything sent with `headed` reuse ticket 05's
// Headed pipeline (Services.runAction: resolve the Window, refuse a
// locked desktop, refuse an administrator Window, activate and verify the
// foreground) and plug their step into it. Either way the element-side
// work runs on the UIA worker (internal/uia value.go, headless.go: live
// pattern probes). The output contract is the shared one: `ok`, or a
// one-line failure with a recovery hint, non-zero exit.
//
// The commands reach the element side through ElementActor, a type
// assertion on Services.Actions — ticket 05's ElementPointer stays
// untouched, so later Action tickets can extend the same way.
//
// The Element Reference front half (the guard, the argument validation,
// the name:/id: resolution) is the shared elementTarget (refs.go,
// finding 16); the @x,y form (finding 8) resolves the point through the
// Window's latest screenshot Mapping (shotPoint) and acts on the Element
// the UIA worker finds under it (ElementFromPoint).

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

// setValueUsageHint is the recovery hint for set-value's argument failures.
const setValueUsageHint = "usage: winuse set-value <window> <element index | name:<text> | id:<id> | @x,y> <value>"

// secondaryUsageHint is the recovery hint for perform-secondary-action's
// argument failures.
const secondaryUsageHint = "usage: winuse perform-secondary-action <window> <element index | name:<text> | id:<id> | @x,y> <action>"

// ElementActor is the element-side half of ticket 07's Actions: setting an
// Element's value and performing its Secondary Actions, on the UIA worker.
// The real implementation is *uia.Client; Services.Actions is type-asserted
// to it, so tests fake it like the click pipeline's ElementPointer.
type ElementActor interface {
	// SetValue writes value to the Element behind the index of the
	// Window's latest Window State (Value pattern, or RangeValue for
	// range Elements).
	SetValue(ctx context.Context, hwnd uint64, index int, value string) error
	// PerformSecondaryAction performs one Secondary Action (already
	// canonicalized) on the Element behind the index.
	PerformSecondaryAction(ctx context.Context, hwnd uint64, index int, action string) error
	// SetValueAtPoint writes value to the Element at the screen point
	// (an @x,y Element Reference): UIA's ElementFromPoint under the
	// mapped point, then the same write as SetValue.
	SetValueAtPoint(ctx context.Context, x, y int32, value string) error
	// PerformSecondaryActionAtPoint performs one Secondary Action (already
	// canonicalized) on the Element at the screen point.
	PerformSecondaryActionAtPoint(ctx context.Context, x, y int32, action string) error
}

// setValue runs `winuse set-value <window> <element index | name:/id:
// reference | @x,y> <value>`. On an Element Index or reference it is
// Headless (ADR 0010): the value is written through the Element's value
// pattern without focusing it (UIA SetFocus moves the desktop foreground,
// agent-desktop A3-4) and a Value write is read back. Headed (`headed`):
// the shared Action pipeline, then the Element's value is written through
// its value pattern (focused first, Codex's set_value order). The @x,y
// form has no other route: without `headed` it is refused with nothing
// sent and the --headed retry (headless-max ticket 21). A name:/id: reference (ticket 11) resolves
// against the Window's latest Window State to the Element Index to set;
// an @x,y reference (finding 8) resolves through the Window's latest
// screenshot to the screen point, and the Element the UIA worker finds
// under it is set — so a Window whose tree is poor but whose screenshot
// is readable can still be filled by coordinates.
func setValue(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.SetValueArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("set-value: "+err.Error(), setValueUsageHint)
		}
		target, terr := parseElementTarget("set-value", a.ElementIndex, a.Ref, a.Point, setValueUsageHint)
		if terr != nil {
			return "", terr
		}
		// A Page Element routes over the Browser Connection (pageactions.go).
		if out, rerr, ok := s.onPage(ctx, a.Window, target, func(w win.WindowInfo, index int, o *page.Origin) (string, *Error) {
			return s.pageSetValue(ctx, w, index, o, target, a.Value, a.Headed)
		}); ok {
			return out, rerr
		}
		if ha, ok := s.headless("set-value", args); ok {
			return s.runHeadless(ctx, a.Window, ha, headlessRun{
				cmd:       "set-value",
				verb:      "set value", // never the value: it could be a password
				target:    target,
				minimized: true,
				act: func(ctx context.Context, w win.WindowInfo, index int) error {
					return ha.SetValueHeadless(ctx, w.ID, index, a.Value)
				},
				refusal: valueError,
			})
		}
		actor, ok := s.Actions.(ElementActor)
		if !ok {
			return "", Fail("set-value is unavailable: no UIA client", "")
		}

		if target.point != nil {
			// The @x,y form: the point is mapped from the latest
			// screenshot, and the Element under it is set.
			x, y := target.point.X, target.point.Y
			if !a.Headed {
				return "", s.headedOnly(a.Window,
					fmt.Sprintf("set-value at %s has no background route: winuse finds the Element under a point only after bringing the Window to the foreground", pointToken(x, y)),
					func(id string) string { return "set-value " + id + " " + pointToken(x, y) + " <value>" },
					"to set it without the foreground, name the Element by its Element Index, name: or id: from winuse get-window-state")
			}
			return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
				pt, serr := shotPoint(ctx, s, w, x, y, "set-value")
				if serr != nil {
					return "", serr
				}
				if err := actor.SetValueAtPoint(ctx, int32(pt.X), int32(pt.Y), a.Value); err != nil {
					return "", valueError(err, w, -1)
				}
				return okOutput, nil
			})
		}
		if !a.Headed {
			// Unreachable on a real helper (its UIA client is a
			// HeadlessActor): an element side that cannot write headless.
			return "", s.headedOnly(a.Window, "set-value has no background route on this helper",
				func(id string) string { return "set-value " + id + " " + target.token() + " <value>" }, "")
		}
		return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
			index, rerr := target.resolve(ctx, s, w)
			if rerr != nil {
				return "", rerr
			}
			if err := actor.SetValue(ctx, w.ID, index, a.Value); err != nil {
				return "", valueError(err, w, index)
			}
			return okOutput, nil
		})
	}
}

// performSecondaryAction runs `winuse perform-secondary-action <window>
// <element index | name:/id: reference | @x,y> <action>`: the action name
// is validated against winuse's Secondary Actions (case-insensitive)
// before the pipeline runs, then the Element's own Secondary Actions are
// probed live and the requested one is performed through its pattern —
// Headless (ADR 0010) on an Element Index or reference, with Toggle,
// Select, Expand and Collapse read back; Headed with `headed`. Raise (it
// moves the foreground by definition) and @x,y have no other route:
// without `headed` they are refused with nothing sent and the --headed
// retry (headless-max ticket 21). A
// name:/id: reference (ticket 11) resolves against the Window's latest
// Window State to the Element Index to act on; an @x,y reference
// (finding 8) acts on the Element the UIA worker finds under the mapped
// point.
func performSecondaryAction(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.SecondaryActionArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("perform-secondary-action: "+err.Error(), secondaryUsageHint)
		}
		target, terr := parseElementTarget("perform-secondary-action", a.ElementIndex, a.Ref, a.Point, secondaryUsageHint)
		if terr != nil {
			return "", terr
		}
		if a.Action == "" {
			return "", Fail("perform-secondary-action: an action is required", secondaryUsageHint)
		}
		action, ok := uia.CanonicalSecondaryAction(a.Action)
		if !ok {
			return "", Fail(fmt.Sprintf("unsupported secondary action: %s; expected %s", a.Action, uia.ExpectedSecondaryActions()),
				"the Secondary Actions each Element offers are listed in its Window State line")
		}
		if out, rerr, ok := s.onPage(ctx, a.Window, target, func(w win.WindowInfo, index int, _ *page.Origin) (string, *Error) {
			return "", pageSecondaryRefusal(w, index, target, action)
		}); ok {
			return out, rerr
		}
		if ha, ok := s.headless("perform-secondary-action", args); ok {
			return s.runHeadless(ctx, a.Window, ha, headlessRun{
				cmd:       "perform-secondary-action",
				verb:      strings.ToLower(action),
				target:    target,
				minimized: true,
				act: func(ctx context.Context, w win.WindowInfo, index int) error {
					return ha.PerformSecondaryActionHeadless(ctx, w.ID, index, action)
				},
				refusal: secondaryError,
			})
		}
		actor, ok := s.Actions.(ElementActor)
		if !ok {
			return "", Fail("perform-secondary-action is unavailable: no UIA client", "")
		}

		if target.point != nil {
			// The @x,y form: the point is mapped from the latest
			// screenshot, and the Secondary Action runs on the Element
			// under it.
			x, y := target.point.X, target.point.Y
			if !a.Headed {
				return "", s.headedOnly(a.Window,
					fmt.Sprintf("perform-secondary-action at %s has no background route: winuse finds the Element under a point only after bringing the Window to the foreground", pointToken(x, y)),
					func(id string) string {
						return "perform-secondary-action " + id + " " + pointToken(x, y) + " " + secondaryToken(action)
					},
					"to act without the foreground, name the Element by its Element Index, name: or id: from winuse get-window-state")
			}
			return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
				pt, serr := shotPoint(ctx, s, w, x, y, "perform-secondary-action")
				if serr != nil {
					return "", serr
				}
				if err := actor.PerformSecondaryActionAtPoint(ctx, int32(pt.X), int32(pt.Y), action); err != nil {
					return "", secondaryError(err, w, -1)
				}
				return okOutput, nil
			})
		}
		if !a.Headed {
			why := "perform-secondary-action " + action + " has no background route on this helper"
			alt := ""
			if action == "Raise" {
				// Raise brings the Element's Window forward: it is the
				// foreground by definition (ADR 0013, "What stays
				// impossible").
				why = "perform-secondary-action Raise has no background route: raising brings the Window to the foreground"
				alt = "to bring the whole Window forward, run winuse activate-window"
			}
			return "", s.headedOnly(a.Window, why,
				func(id string) string {
					return "perform-secondary-action " + id + " " + target.token() + " " + secondaryToken(action)
				}, alt)
		}
		return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
			index, rerr := target.resolve(ctx, s, w)
			if rerr != nil {
				return "", rerr
			}
			if err := actor.PerformSecondaryAction(ctx, w.ID, index, action); err != nil {
				return "", secondaryError(err, w, index)
			}
			return okOutput, nil
		})
	}
}

// valueError maps a set-value failure to the user-facing line: the
// not-settable refusal points at type-text (the ticket's contract); a
// combo or list with no item, or several, by the name asked for points at
// the names it lists (desktop-parity 12); anything element-side falls
// through to the shared stale/state mapping.
func valueError(err error, w win.WindowInfo, index int) *Error {
	var sel *uia.SelectItemError
	if errors.As(err, &sel) {
		id := strconv.FormatUint(w.ID, 10)
		switch {
		case errors.Is(err, uia.ErrAmbiguousItem):
			return Fail(err.Error(), "name one item exactly as the list spells it: an exact name wins over one that differs only in case or spacing")
		case errors.Is(err, uia.ErrNoSuchItem):
			return Fail(err.Error(), "pass one of the list's item names exactly; read a fresh Window State (winuse get-window-state "+id+") if the list has changed")
		case sel.Opened:
			// The search failed after the Expand: keep the opened-and-closed
			// wording the shared mapping would drop.
			return Fail(err.Error(), "read a fresh Window State (winuse get-window-state "+id+") and retry")
		}
	}
	switch {
	case errors.Is(err, uia.ErrNotSettable):
		return Fail(err.Error(),
			"use winuse click to focus the Element, then winuse type-text to type into it instead")
	case errors.Is(err, uia.ErrRangeValue):
		return Fail(err.Error(), "this Element takes a numeric value (a slider or spinner); pass a number")
	case errors.Is(err, uia.ErrToggleValue):
		return Fail(err.Error(), "this Element is a check box or toggle; pass on or off")
	case errors.Is(err, uia.ErrToggleUnreadable):
		toggle := "perform-secondary-action with the Toggle action"
		if index >= 0 {
			toggle = fmt.Sprintf("winuse perform-secondary-action %d %d Toggle", w.ID, index)
		}
		return Fail(err.Error(), "read a fresh Window State; to flip the Element whatever its state, use "+toggle)
	case errors.Is(err, uia.ErrFocusBeforeValue):
		return Fail(err.Error(), "the Element may not be focusable; read a fresh Window State and retry")
	case errors.Is(err, uia.ErrDisabled):
		return Fail(err.Error(), "a disabled Element cannot be acted on; read a fresh Window State and pick another Element")
	}
	return elementError(err, w, index)
}

// secondaryError maps a perform-secondary-action failure to the
// user-facing line: an action the Element does not offer lists the ones
// it does; anything element-side falls through to the shared mapping.
func secondaryError(err error, w win.WindowInfo, index int) *Error {
	var unavail *uia.UnavailableActionError
	if errors.As(err, &unavail) {
		return Fail(err.Error(),
			"run winuse get-window-state "+fmt.Sprint(w.ID)+" again and use one of the Element's listed Secondary Actions")
	}
	if errors.Is(err, uia.ErrDisabled) {
		return Fail(err.Error(), "a disabled Element cannot be acted on; read a fresh Window State and pick another Element")
	}
	return elementError(err, w, index)
}
