package helper

// The ticket-07 Action commands: set-value and perform-secondary-action.
// They reuse ticket 05's pipeline (Services.runAction: resolve the Window,
// refuse a locked desktop, refuse an administrator Window, activate and
// verify the foreground) and plug their step into it, then run the
// element-side work on the UIA worker (internal/uia value.go: live pattern
// probes). The output contract is the shared one: `ok`, or a one-line
// failure with a recovery hint, non-zero exit.
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
// reference | @x,y> <value>`: the shared Action pipeline, then the
// Element's value is written through its value pattern (focused first,
// Codex's set_value order). A name:/id: reference (ticket 11) resolves
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
		actor, ok := s.Actions.(ElementActor)
		if !ok {
			return "", Fail("set-value is unavailable: no UIA client", "")
		}

		if target.point != nil {
			// The @x,y form: the point is mapped from the latest
			// screenshot, and the Element under it is set.
			x, y := target.point.X, target.point.Y
			return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
				pt, serr := shotPoint(s, w, x, y, "set-value")
				if serr != nil {
					return "", serr
				}
				if err := actor.SetValueAtPoint(ctx, int32(pt.X), int32(pt.Y), a.Value); err != nil {
					return "", valueError(err, w, -1)
				}
				return okOutput, nil
			})
		}
		return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
			index, rerr := target.resolve(s, w)
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
// probed live and the requested one is performed through its pattern. A
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
		actor, ok := s.Actions.(ElementActor)
		if !ok {
			return "", Fail("perform-secondary-action is unavailable: no UIA client", "")
		}

		if target.point != nil {
			// The @x,y form: the point is mapped from the latest
			// screenshot, and the Secondary Action runs on the Element
			// under it.
			x, y := target.point.X, target.point.Y
			return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
				pt, serr := shotPoint(s, w, x, y, "perform-secondary-action")
				if serr != nil {
					return "", serr
				}
				if err := actor.PerformSecondaryActionAtPoint(ctx, int32(pt.X), int32(pt.Y), action); err != nil {
					return "", secondaryError(err, w, -1)
				}
				return okOutput, nil
			})
		}
		return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
			index, rerr := target.resolve(s, w)
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
// not-settable refusal points at type-text (the ticket's contract);
// anything element-side falls through to the shared stale/state mapping.
func valueError(err error, w win.WindowInfo, index int) *Error {
	switch {
	case errors.Is(err, uia.ErrNotSettable):
		return Fail(err.Error(),
			"use winuse click to focus the Element, then winuse type-text to type into it instead")
	case errors.Is(err, uia.ErrRangeValue):
		return Fail(err.Error(), "this Element takes a numeric value (a slider or spinner); pass a number")
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
