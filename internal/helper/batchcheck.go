package helper

// A Batch's upfront value checks (bladebro-ideas ticket 04): every check of
// a step that depends only on its arguments runs while the Batch prepares
// (batch.go), before any step sends anything, so a typo in the last step
// (a key name, a Secondary Action, an @x,y, a wait kind, a timeout) fails
// the Batch with nothing sent instead of after the steps before it acted.
// The checks are each command's own front half (clickInputOf,
// pressKeyChord, waitInput, ...), which the command runs again when the
// step's turn comes, so a step and the same command sent alone refuse a
// bad value with the same message.
//
// What stays at the step's turn is what depends on the Window or the
// Page: whether the Window resolves (the Batch's own and a step's `in
// <window>` resolve up front, batch.go), whether a name:/id: reference
// matches, whether the Element exists, is enabled or offers the Secondary
// Action, a route with no Headless form, an open dialog, the file chooser,
// a Tab id, whether a pause's length is an open Window's ID, and every
// read-back. The ticket lists both groups.

import (
	"encoding/json"

	"github.com/hoangvu12/winuse/internal/protocol"
)

// stepChecks are the static checks of every batchable Action
// (protocol.BatchStepKinds), by kind: each takes the step's prepared
// arguments (its Window and the Batch's headed injected). nil is a step
// with nothing to check but its Window.
var stepChecks = map[string]func(raw json.RawMessage) *Error{
	"click": argsCheck(func(a protocol.ClickArgs) *Error {
		_, e := clickInputOf(a)
		return e
	}),
	"type-text": argsCheck(func(a protocol.TypeTextArgs) *Error {
		_, e := typeTextInput(a)
		return e
	}),
	"press-key": argsCheck(func(a protocol.PressKeyArgs) *Error {
		_, e := pressKeyChord(a)
		return e
	}),
	"set-value": argsCheck(func(a protocol.SetValueArgs) *Error {
		_, e := setValueTarget(a)
		return e
	}),
	"perform-secondary-action": argsCheck(func(a protocol.SecondaryActionArgs) *Error {
		_, _, e := secondaryInput(a)
		return e
	}),
	"activate-window": nil,
	"scroll": argsCheck(func(a protocol.ScrollArgs) *Error {
		_, e := scrollInput(a)
		return e
	}),
	"drag": argsCheck(func(a protocol.DragArgs) *Error {
		_, e := dragInput(a)
		return e
	}),
	"hover":       argsCheck(hoverInput),
	"tab-new":     tabCheck("tab-new", false),
	"tab-close":   tabCheck("tab-close", true),
	"tab-switch":  tabCheck("tab-switch", true),
	"tab-list":    tabCheck("tab-list", false),
	"navigate":    navCheck("navigate"),
	"back":        navCheck("back"),
	"forward":     navCheck("forward"),
	"reload":      navCheck("reload"),
	"upload":      argsCheck(uploadInput),
	"dialog":      argsCheck(func(a protocol.DialogArgs) *Error { _, e := dialogInput(a); return e }),
	"wait":        argsCheck(waitInput),
	"eval":        argsCheck(func(a protocol.EvalArgs) *Error { _, e := evalInput(a); return e }),
	"select-text": argsCheck(func(a protocol.SelectTextArgs) *Error { _, _, e := selectTextInput(a); return e }),
	"invoke-menu": argsCheck(func(a protocol.InvokeMenuArgs) *Error { _, e := menuPath(a.Path); return e }),
	// The reads: a bad flag would fail the Batch after the steps before
	// it acted, too.
	"get-window-state": argsCheck(windowStateInput),
	"find":             argsCheck(findInput),
}

// checkStep runs one prepared step's static checks: nil when its values
// are fine, or the command's own refusal of the first bad one.
func checkStep(kind string, raw json.RawMessage) *Error {
	if check := stepChecks[kind]; check != nil {
		return check(raw)
	}
	return nil
}

// argsCheck decodes a step's arguments into its command's argument type
// (a value of the wrong JSON type is refused here, as the command refuses
// it) and runs check on them.
func argsCheck[T any](check func(T) *Error) func(json.RawMessage) *Error {
	return func(raw json.RawMessage) *Error {
		var a T
		if err := parseArgs(raw, &a); err != nil {
			return Fail(err.Error(), batchUsageHint)
		}
		return check(a)
	}
}

// tabCheck is a tab command's check: a tab id for close and switch
// (parseTabArgs).
func tabCheck(cmd string, needTab bool) func(json.RawMessage) *Error {
	return func(raw json.RawMessage) *Error {
		_, e := parseTabArgs(cmd, raw, needTab)
		return e
	}
}

// navCheck is navigate's, back's, forward's or reload's check (navInput).
func navCheck(cmd string) func(json.RawMessage) *Error {
	return argsCheck(func(a protocol.NavigateArgs) *Error {
		_, e := navInput(cmd, a)
		return e
	})
}
