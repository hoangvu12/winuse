package helper

// Headless Element Actions (ADR 0010, .scratch/headless/spec.md): `click`
// on an Element (left, single), `set-value` and `perform-secondary-action`
// (except Raise) by Element Index or name:/id: reference run through UI
// Automation patterns alone, on a Window that stays where it is. They
// never activate the Window, move the pointer, inject input or raise the
// in-control banner; the Agent Cursor shows what is happening instead.
// Everything else — `@x,y` forms, double/right/middle clicks, Raise, the
// physical Actions — has only v1's runAction pipeline, and runs it only
// when sent with `headed`; any Action sent with `headed` runs it too.
//
// winuse never switches mode on its own: a Headless click whose Element
// offers no pattern route fails with nothing sent and a hint to pass
// --headed (taking the user's foreground is the agent's explicit choice),
// and so does every Action whose only route is Headed when it was sent
// without `headed` (headedgate.go, ADR 0013 rule 3).
//
// Routing is decided by one predicate, headlessStep, over the command's
// wire arguments: the standalone commands and the Batch (which must pick
// its Esc scope and banner before its first step runs) both ask it, so
// they can never disagree about which Actions are Headless. A helper
// whose Services.Actions is not a HeadlessActor (the v1 fakes of the
// offline tests) routes every Element Action Headed.
//
// press-key and type-text (ADR 0011, Decision 4; keyroute.go) route by
// the live Window instead: the predicate takes the Window probe's
// reading, read after the Window resolves and before anything is sent.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/keys"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// HeadlessActor is the element side of the Headless Actions, on the UIA
// worker: the pattern-only click route, the focus-free value write and
// the Secondary Actions, each with its read-back where the pattern has a
// readable outcome, plus the Element's live bounds and name for the
// Agent Cursor. The real implementation is *uia.Client; Services.Actions
// is type-asserted to it (like ElementActor), so a fake that implements
// only v1's seams keeps every Action Headed.
type HeadlessActor interface {
	// HeadlessClick clicks the Element through the first pattern it
	// offers (Invoke, Toggle, Select, Expand/Collapse, LegacyIAccessible
	// default action) and returns the route taken; uia.ErrNoClickRoute
	// when it offers none (nothing was sent).
	HeadlessClick(ctx context.Context, hwnd uint64, index int) (string, error)
	// SetValueHeadless writes the value without focusing the Element
	// first (UIA SetFocus moves the desktop foreground) and reads a
	// Value-pattern write back.
	SetValueHeadless(ctx context.Context, hwnd uint64, index int, value string) error
	// PerformSecondaryActionHeadless performs one canonical Secondary
	// Action other than Raise and reads Toggle/Select/Expand/Collapse back.
	PerformSecondaryActionHeadless(ctx context.Context, hwnd uint64, index int, action string) error
	// ElementBounds reads the Element's live bounds (physical screen
	// pixels) and name: the Agent Cursor's destination and label.
	ElementBounds(ctx context.Context, hwnd uint64, index int) (left, top, right, bottom int32, name string, err error)
	// ScrollElement scrolls the Element by steps through its own
	// ScrollPattern or its nearest scrollable ancestor's and reads the
	// scroll percent back (headless-parity spec, "Element scroll").
	ScrollElement(ctx context.Context, hwnd uint64, index int, dir uia.ScrollDirection, steps int) error
	// ScrollElementIntoView scrolls the Element into view through its
	// ScrollItem pattern and reads IsOffscreen back.
	ScrollElementIntoView(ctx context.Context, hwnd uint64, index int) error
}

var _ HeadlessActor = (*uia.Client)(nil)

// Cursor is the Agent Cursor (headless spec, ADR 0010): a
// presentation-only second cursor that travels to the Element a Headless
// Action targets, its Agent's name in a tag beside it, and presses on
// the click — so headless work is never invisible, without taking the
// user's pointer, keyboard or foreground. It is driven by screen
// rectangles and label text only (never UIA Elements), so other drivers
// can show the same cursor. The real one is *win.AgentCursor, wired by
// Main; nil means no cursor. Every call is best effort: a cursor failure
// never fails an Action.
type Cursor interface {
	Travel(origin, target win.Rect, label string) // show (starting at origin's centre when hidden) and glide to target's centre; returns when done or a bound passes
	Click(target win.Rect)                        // ripple at the cursor + brief outline of target; non-blocking
	HideWhile(f func())                           // run f with the cursor hidden
}

var _ Cursor = (*win.AgentCursor)(nil)

// PassiveEscWatch is the optional passive half of the Esc service (ADR
// 0010: "A physical Esc during a Headless scope still stops winuse but is
// not swallowed"). ArmPassive and DisarmPassive bracket a Headless scope:
// a press latches (EscWatch.Stopped reports it) but reaches the user's
// app; a swallowing Arm scope open at the same time wins. The real
// *win.EscWatcher implements it; an EscWatch that does not (the v1 test
// fakes, the no-op) simply arms nothing passive.
type PassiveEscWatch interface {
	ArmPassive()
	DisarmPassive()
}

// armPassive enters a passive Esc scope on esc when it supports one, and
// returns the matching disarm (a no-op otherwise).
func armPassive(esc EscWatch) (disarm func()) {
	if p, ok := esc.(PassiveEscWatch); ok {
		p.ArmPassive()
		return p.DisarmPassive
	}
	return func() {}
}

// takesHeaded reports whether a command takes the protocol's `headed`
// flag: the ones with a Headless form, and the ones that are Headed only
// when the agent asks (drag and hover at a point: headless-max ticket 21).
func takesHeaded(kind string) bool {
	switch kind {
	case "click", "set-value", "perform-secondary-action", "scroll", "press-key", "type-text",
		"drag", "hover",
		"tab-switch": // tabs.go: the tab strip's Select, or the keys with headed
		return true
	}
	return false
}

// stepHeaded reports whether one step's prepared arguments carry `headed`
// (the Batch's own, injected by withHeaded, or the step's).
func stepHeaded(raw json.RawMessage) bool {
	var f struct {
		Headed bool `json:"headed"`
	}
	return json.Unmarshal(raw, &f) == nil && f.Headed
}

// headlessStep is the one routing predicate (spec, "Headless Action
// set"): whether the command kind with these wire arguments is a
// Headless Action — `click` on an Element Index or name:/id: reference
// with the left button and a single click; `set-value` and
// `perform-secondary-action` on an Element Index or reference, except the
// Raise Secondary Action; `scroll` on an Element Index or reference with a
// direction or into-view (headless-parity spec, "Element scroll");
// `select-text` on an Element Index or reference with a valid span, which
// has no headed form (headless-max ticket 10); `invoke-menu` with a valid
// path, which has no headed form either (headless-max ticket 08); never
// with `headed: true`.
// `@x,y` is never
// Headless: a screen point names whatever is topmost there, which for a
// covered Window is another Window's Element. Arguments that do not
// parse (or name an unknown Secondary Action) are not Headless: the
// command fails on them anyway, and a Batch then keeps v1's swallowing
// scope.
//
// press-key and type-text route by the live Window, not by their
// arguments alone (ADR 0011, Decision 4): route is the Window probe's
// reading (Services.KeyRoute), taken before anything is sent, and nil
// when there is none, which keeps every keyboard step Headed. Return or
// Escape with no modifiers on a classic dialog is Headless
// (dialogKeyRoute), and so is text with no control characters into a
// focused, writable classic edit control (editTypeRoute). type-text
// aimed at an Element (--into, typeinto.go) needs no probe: unheaded, it
// is Headless or sends nothing. The other kinds ignore route.
//
// Posted Keys (`post`, ADR 0011, Decision 5) are no Headless Action:
// Windows does not promise that an app handles them. They take no
// foreground, no banner and no real input either, so a posted step
// counts as Headless here, for the Batch's scope, without a probe. With
// `headed` as well the command fails, and the step is not Headless.
func headlessStep(kind string, args json.RawMessage, route *win.KeyTarget) bool {
	switch kind {
	case "type-text":
		var a protocol.TypeTextArgs
		if json.Unmarshal(args, &a) != nil || a.Headed {
			return false
		}
		if typesInto(a) {
			// Headless or nothing sent, never Headed (typeinto.go); with
			// post the command fails.
			return !a.Post
		}
		if a.Post {
			return true
		}
		if route == nil {
			return false
		}
		_, ok := editTypeRoute(a.Text, *route)
		return ok
	case "press-key":
		var a protocol.PressKeyArgs
		if json.Unmarshal(args, &a) != nil || a.Headed {
			return false
		}
		if a.Post {
			return true
		}
		if route == nil {
			return false
		}
		chord, err := keys.Parse(a.Key)
		if err != nil {
			return false
		}
		_, ok := dialogKeyRoute(chord, *route)
		return ok
	case "click":
		var a protocol.ClickArgs
		if json.Unmarshal(args, &a) != nil || a.Headed || !elementForm(a.ElementIndex, a.Ref, a.Point) {
			return false
		}
		if a.ClickCount != 0 && a.ClickCount != 1 {
			return false
		}
		button, ok := parseMouseButton(a.MouseButton)
		return ok && button == win.MouseLeft
	case "set-value":
		var a protocol.SetValueArgs
		return json.Unmarshal(args, &a) == nil && !a.Headed && elementForm(a.ElementIndex, a.Ref, a.Point)
	case "perform-secondary-action":
		var a protocol.SecondaryActionArgs
		if json.Unmarshal(args, &a) != nil || a.Headed || !elementForm(a.ElementIndex, a.Ref, a.Point) {
			return false
		}
		action, ok := uia.CanonicalSecondaryAction(a.Action)
		return ok && action != "Raise"
	case "scroll":
		var a protocol.ScrollArgs
		return json.Unmarshal(args, &a) == nil && headlessElementScroll(a)
	case "select-text":
		// Always Headless: there is no headed form (selecttext.go).
		var a protocol.SelectTextArgs
		return json.Unmarshal(args, &a) == nil && headlessSelectText(a)
	case "invoke-menu":
		// Always Headless: there is no headed form (invokemenu.go).
		var a protocol.InvokeMenuArgs
		return json.Unmarshal(args, &a) == nil && headlessInvokeMenu(a)
	}
	return false
}

// elementForm reports whether an Element Reference is exactly an Element
// Index or a name:/id: reference (not @x,y, not several forms).
func elementForm(index *int, ref string, point *protocol.Point) bool {
	return point == nil && countRefForms(false, index != nil, ref != "") == 1
}

// headless reports whether this Element Action runs Headless on this
// helper: the shared predicate holds and the element side can act
// headless. The keyboard commands route through keyRoute instead: they
// need the live Window, not the element side.
func (s Services) headless(kind string, args json.RawMessage) (HeadlessActor, bool) {
	actor, ok := s.Actions.(HeadlessActor)
	if !ok || !headlessStep(kind, args, nil) {
		return nil, false
	}
	return actor, true
}

// keyRoute probes the resolved Window for the keyboard routing (ADR 0011,
// Decision 4). It returns nil, and the keyboard commands stay Headed,
// when there is no probe (Services built without defaultServices), or
// when the step is headed or posted: neither route needs the probe.
func (s Services) keyRoute(args json.RawMessage, w win.WindowInfo) *win.KeyTarget {
	if s.KeyRoute == nil {
		return nil
	}
	var flags struct {
		Headed bool `json:"headed"`
		Post   bool `json:"post"`
	}
	if json.Unmarshal(args, &flags) != nil || flags.Headed || flags.Post {
		return nil
	}
	t := s.KeyRoute(w.ID)
	return &t
}

// stepHeadless is the Batch's per-step scope question: the same predicate
// each step's command routes on. Element Actions need the element side;
// press-key and type-text need the Batch's Window (w nil when it did not
// resolve: the first step then fails on it anyway) and probe it.
func (s Services) stepHeadless(ctx context.Context, kind string, args json.RawMessage, w *win.WindowInfo) bool {
	if readStep(kind) {
		return !shotRead(args) || !stepHeaded(args) // batch.go: a read activates only for a headed screenshot
	}
	if w != nil && s.pageStep(ctx, kind, args, *w) {
		return true // a Page step (pageactions.go)
	}
	if chromiumStep(kind, args, w) {
		return true // chromium.go: window messages, no foreground or banner
	}
	switch kind {
	case "press-key", "type-text":
		if w == nil {
			return false
		}
		if kind == "press-key" && overflowEscapeStep(args, *w) {
			return true // Escape on the tray overflow (shell.go)
		}
		if kind == "press-key" && menuEscapeStep(args, *w) {
			return true // Escape on an open menu (contextmenu.go)
		}
		return headlessStep(kind, args, s.keyRoute(args, *w))
	case "tab-new", "tab-close", "tab-switch":
		return s.tabStepHeadless(kind, args, w) // tabs.go: a headed tab-switch is Headless only through the extension
	case "invoke-menu":
		return headlessStep(kind, args, nil) // the menu route needs no element side
	}
	if postedStep(kind, args) {
		return true // postclick.go: posted, no foreground or banner
	}
	if _, _, ok := s.pointActor(); ok && pointStep(kind, args) {
		return true // pointroute.go: Headless, or refused with nothing sent
	}
	_, ok := s.headless(kind, args)
	return ok
}

// headlessRun is one Headless Action's own part of runHeadless.
type headlessRun struct {
	cmd    string        // the command name (the --headed retry hint)
	verb   string        // the Agent Cursor label's lowercase verb: "click", "set value", "toggle", "scroll down"
	target elementTarget // the Element Index or name:/id: reference
	// act performs the pattern call on the Element behind index.
	act func(ctx context.Context, w win.WindowInfo, index int) error
	// refusal is the command's v1 element-side failure mapping
	// (elementError, valueError, secondaryError), reused for the
	// refusals both modes share.
	refusal func(err error, w win.WindowInfo, index int) *Error
	// minimized lets the act reach a minimized Window whose class still
	// takes UI Automation pattern calls (headless-max ticket 14,
	// win.MinimizedPatterns): click, set-value, the Secondary Actions and
	// select-text. Element scroll stays refused: its into-view read-back
	// needs the Element on screen, and scrolling minimized is unmeasured.
	minimized bool
}

// runHeadless is the Headless Action pipeline beside runAction (ADR
// 0010): resolve the Window; refuse a locked desktop and an administrator
// Window (UIA writes across the integrity boundary fail, agent-desktop
// A24-16); refuse a Window that cannot take a pattern call without the
// foreground (Services.Ready: a modal dialog blocks it, or it is
// minimized, unless run.minimized and its class takes pattern calls
// minimized); then, inside a passive Esc scope, resolve the Element
// Index, glide the Agent Cursor to the Element, act through its
// patterns, and ripple the cursor on a delivered Action. A minimized
// Window has no rectangle on screen, so the cursor stays hidden there. No
// activation, no banner, no pointer, no keys.
//
// Every refusal before the pattern call says "nothing was sent", so the
// agent knows a retry is safe; a failed write says it may have taken
// effect, a failed read-back that the Action was delivered. The Esc
// checkpoints sit after the cursor's travel (a stop there sent nothing)
// and after the act (a stop there outranks the outcome, as in v1).
func (s Services) runHeadless(ctx context.Context, ref string, actor HeadlessActor, run headlessRun) (string, *Error) {
	ctx = withCursorTrace(ctx, run.cmd) // cursortrace.go
	w, rerr := resolveWindow(s, ref)
	if rerr != nil {
		return "", rerr
	}
	noteRoute(ctx, w, protocol.RouteHeadless) // trace.go
	iconic, rerr := s.refusals(w, adminPatternHint, run.minimized)
	if rerr != nil {
		return "", rerr
	}

	// The passive Esc scope: from here the user's Esc stops this Action
	// but still reaches their own app.
	esc := s.Esc
	if esc == nil {
		esc = noopEscWatch{}
	}
	defer armPassive(esc)()

	index, rerr := run.target.resolve(ctx, s, w)
	if rerr != nil {
		return "", nothingSent(rerr)
	}
	if rerr := s.shellElementRefusal(ctx, w, index, run); rerr != nil {
		return "", rerr // an Element of a shell surface (shell.go)
	}
	var rect win.Rect
	var shown bool
	if !iconic {
		rect, shown = s.cursorTravel(ctx, actor, w, index, run.verb)
	}
	if esc.Stopped() {
		// The user pressed Esc during the cursor's travel: stop before the
		// pattern call.
		return "", escStopBeforeActError()
	}
	// The identity re-check (identity.go). A pattern call is bound to its
	// Element, which a reused handle does not inherit, but some acts post
	// to the Window's controls (select-text's EM_SETSEL, the ContextMenu
	// Secondary Action), and the refusal names the reuse either way.
	if rerr := s.sameWindow(ctx, w.ID); rerr != nil {
		return "", rerr
	}
	err := run.act(ctx, w, index)
	cursorTrace(ctx, "acted")
	var verify *uia.VerifyError
	if shown && (err == nil || errors.As(err, &verify)) {
		// Delivered (a failed read-back was still delivered): the ripple.
		// An act that refuses sends nothing, so the ripple waits for the
		// act; a click's Invoke reads nothing back, so it follows the
		// delivery closely (browser-ux 03).
		s.cursorClick(ctx, w, rect)
	}
	if esc.Stopped() {
		return "", escStopError()
	}
	if err != nil {
		return "", headlessFailure(err, w, index, run)
	}
	return okOutput, nil
}

// adminPatternHint and adminMessageHint are the administrator-Window
// refusal's hints: UIPI blocks UI Automation writes and posted or sent
// window messages alike.
const (
	adminPatternHint = "UI Automation writes into a higher-integrity Window fail; start winuse from an elevated terminal to control administrator Windows, or ask the user to operate this Window"
	adminMessageHint = "Windows blocks messages into a higher-integrity Window; start winuse from an elevated terminal to control administrator Windows, or ask the user to operate this Window"
)

// headlessRefusals are runHeadless's refusals on a resolved Window, in
// order, each saying "nothing was sent": a locked desktop, an
// administrator Window (adminHint), and a Window that cannot take the
// Action without the foreground (Services.Ready: a modal dialog blocks it,
// or it is minimized).
func (s Services) headlessRefusals(w win.WindowInfo, adminHint string) *Error {
	_, rerr := s.refusals(w, adminHint, false)
	return rerr
}

// refusals are headlessRefusals for an Action that may reach a minimized
// Window (headless-max ticket 14): with minimizedOK, a minimized Window
// whose class still takes UI Automation pattern calls
// (win.MinimizedPatterns) passes, and iconic reports it. A Store app,
// Chromium and the unmeasured frameworks stay refused.
func (s Services) refusals(w win.WindowInfo, adminHint string, minimizedOK bool) (iconic bool, rerr *Error) {
	if locked, desktop := s.Locked(); locked {
		return false, nothingSent(lockedDesktopFailure(desktop))
	}
	if err := s.Integrity(w); err != nil {
		var admin *win.AdminWindowError
		if errors.As(err, &admin) {
			return false, nothingSent(Fail(err.Error(), adminHint))
		}
		return false, nothingSent(Fail(err.Error(), ""))
	}
	check := s.Ready
	if check == nil {
		check = win.HeadlessReady
	}
	if err := check(w.ID); err != nil {
		var minimized *win.MinimizedError
		if minimizedOK && errors.As(err, &minimized) && win.MinimizedPatterns(minimized.Class) {
			return true, nil
		}
		return false, readyError(err, w)
	}
	return false, nil
}

// cursorTravelRect glides the Agent Cursor from the Window to a screen
// rectangle (a dialog's button, an edit control) labelled with what is
// about to happen, and returns where it went for the click effect. An
// empty target falls back to the Window's own bounds. Best effort, like
// cursorTravel.
func (s Services) cursorTravelRect(ctx context.Context, w win.WindowInfo, target win.Rect, label string) (win.Rect, bool) {
	if s.Cursor == nil {
		return win.Rect{}, false
	}
	if bt := tabTargetOf(ctx); bt != nil && (!bt.active || target.Width() <= 0 || target.Height() <= 0) {
		// A background Tab has nothing on the screen to point at, and a shown
		// one is pointed at only where its Page is (ADR 0017).
		return win.Rect{}, false
	}
	if target.Width() <= 0 || target.Height() <= 0 {
		target = w.Bounds
	}
	if target.Width() <= 0 || target.Height() <= 0 {
		return win.Rect{}, false
	}
	cursorTrace(ctx, "travel")
	cursorBestEffort("travelling", func() { s.cursorFor(ctx, w).Travel(w.Bounds, target, label) })
	cursorTrace(ctx, "landed")
	return target, true
}

// cursorTravel glides the Agent Cursor from the Window to the Element,
// labelled with what is about to happen, and returns the Element's
// bounds for the click effect. Best effort: no cursor, an Element whose
// bounds cannot be read (the act then reports the real error) or an empty
// one skips the cursor, never the Action.
func (s Services) cursorTravel(ctx context.Context, actor HeadlessActor, w win.WindowInfo, index int, verb string) (win.Rect, bool) {
	if s.Cursor == nil {
		return win.Rect{}, false
	}
	l, t, r, b, name, err := actor.ElementBounds(ctx, w.ID, index)
	if err != nil {
		log.Printf("reading element %d of window %d for the Agent Cursor: %v", index, w.ID, err)
		return win.Rect{}, false
	}
	rect := win.Rect{Left: l, Top: t, Right: r, Bottom: b}
	if rect.Width() <= 0 || rect.Height() <= 0 {
		return win.Rect{}, false
	}
	cursorTrace(ctx, "travel")
	cursorBestEffort("travelling", func() { s.cursorFor(ctx, w).Travel(w.Bounds, rect, cursorLabel(verb, name)) })
	cursorTrace(ctx, "landed")
	return rect, true
}

// cursorClick plays the Agent Cursor's click effect over the Element.
func (s Services) cursorClick(ctx context.Context, w win.WindowInfo, rect win.Rect) {
	if s.Cursor == nil {
		return
	}
	cursorBestEffort("clicking", func() { s.cursorFor(ctx, w).Click(rect) })
}

// cursorBestEffort runs one cursor call so that it can never fail the
// Action: a panic inside presentation code is logged and dropped.
func cursorBestEffort(what string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("the Agent Cursor failed while %s: %v", what, r)
		}
	}()
	f()
}

// cursorLabel is the Agent Cursor's label: the lowercase verb and the
// Element's quoted name (whitespace collapsed), or the verb alone for an
// unnamed Element — `click "Save"`, `set value "Search"`. Never the value
// being set: it could be a password. The cursor ellipsizes long labels.
func cursorLabel(verb, name string) string {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return verb
	}
	return verb + ` "` + name + `"`
}

// nothingSentNote is appended to every refusal raised before any pattern
// call or input (headless spec, "Retry safety in failures"), so the agent
// knows a retry is safe.
const nothingSentNote = "; nothing was sent"

// nothingSent marks a refusal as raised before anything reached the app.
func nothingSent(e *Error) *Error {
	out := *e
	out.Delivery = protocol.DeliveryNotDelivered
	if strings.HasSuffix(out.Msg, nothingSentNote) {
		return &out // already said
	}
	out.Msg += nothingSentNote
	return &out
}

// escStopBeforeActError is the Esc stop of a Headless Action stopped
// before its pattern call: the same message and exit 130 as every stop,
// with a hint that says nothing was sent.
func escStopBeforeActError() *Error {
	return &Error{
		Msg:      escStopMessage,
		Hint:     "stop your current task and ask the user how to proceed before sending more winuse commands; nothing was sent, so the stopped Action did not take effect",
		Exit:     protocol.ExitEsc,
		Delivery: protocol.DeliveryNotDelivered,
	}
}

// readyError maps Services.Ready's refusal: a modal dialog points at the
// dialog (activationError's hint); a minimized Window points at
// restore-window (headless, ADR 0011), activate-window (the foreground
// way) or --headed (restoring it is the agent's call, not winuse's). A
// Store app comes to the front when restored (probes round 5), and the
// hint says so.
func readyError(err error, w win.WindowInfo) *Error {
	var modal *win.ModalDialogError
	if errors.As(err, &modal) {
		return waitsFor(nothingSent(activationError(err)), refusalWait{kind: waitDialog, window: w}) // briefwait.go: it may be closing
	}
	var minimized *win.MinimizedError
	if errors.As(err, &minimized) {
		id := strconv.FormatUint(w.ID, 10)
		restore := " (headless)"
		if win.StoreAppFrame(minimized.Class) {
			restore = " (a Store app comes to the front when restored, so tell the user first)"
		}
		return nothingSent(Fail(err.Error(),
			"this Action cannot reach a minimized Window headless; restore it with winuse restore-window "+id+
				restore+", or with winuse activate-window "+id+
				" (this takes the foreground), or pass --headed to act the v1 way"))
	}
	return nothingSent(Fail(err.Error(), ""))
}

// headlessFailure maps a Headless act's failure. The click with no
// pattern route and the refusals (stale or missing Window State, a
// disabled Element, a Secondary Action it does not offer, a value it
// cannot take) were raised before any pattern call: nothing was sent. A
// failed pattern write may have taken effect; a failed read-back was
// delivered — repeating either could double-submit.
func headlessFailure(err error, w win.WindowInfo, index int, run headlessRun) *Error {
	id := strconv.FormatUint(w.ID, 10)
	switch {
	case errors.Is(err, uia.ErrNoClickRoute):
		return nothingSent(Fail(err.Error(),
			"retry with --headed to click it with the real pointer (this brings the Window to the foreground): winuse --headed "+
				run.cmd+" "+id+" "+run.target.token()))
	case errors.Is(err, uia.ErrRaiseHeadless):
		return nothingSent(Fail(err.Error(), "pass --headed to Raise the Element"))
	}
	var verify *uia.VerifyError
	if errors.As(err, &verify) {
		return wasDelivered(Fail(err.Error(), "the Action was delivered; do not repeat it, read the Window State: winuse get-window-state "+id))
	}
	var write *uia.WriteError
	if errors.As(err, &write) {
		return mayHaveActed(Fail(err.Error(), "the Action may have taken effect; read the Window State (winuse get-window-state "+id+") before repeating it"))
	}
	e := run.refusal(err, w, index)
	if refusedBeforeCall(err) {
		e = nothingSent(e)
	}
	if errors.Is(err, uia.ErrDisabled) && index >= 0 {
		e = waitsFor(e, refusalWait{kind: waitEnable, window: w, index: index}) // briefwait.go: it may enable
	}
	return e
}

// clickRefusal is the Headless click's element-side mapping: v1's click
// only resolves a point (elementError), but a pattern call can also meet a
// disabled Element.
func clickRefusal(err error, w win.WindowInfo, index int) *Error {
	if errors.Is(err, uia.ErrDisabled) {
		return Fail(err.Error(), "a disabled Element cannot be acted on; read a fresh Window State and pick another Element")
	}
	return elementError(err, w, index)
}

// refusedBeforeCall reports whether an element-side failure is a refusal
// raised before any pattern call. A select by name that matched no item,
// or several, sent nothing unless it expanded the list for the search
// (it then says the list was opened and closed).
func refusedBeforeCall(err error) bool {
	var sel *uia.SelectItemError
	if errors.As(err, &sel) {
		return !sel.Opened
	}
	var unavail *uia.UnavailableActionError
	switch {
	case errors.Is(err, uia.ErrStale), errors.Is(err, uia.ErrNoState), errors.Is(err, uia.ErrDisabled),
		errors.Is(err, uia.ErrNotSettable), errors.Is(err, uia.ErrRangeValue),
		errors.Is(err, uia.ErrToggleValue), errors.Is(err, uia.ErrToggleUnreadable), errors.As(err, &unavail),
		errors.Is(err, uia.ErrNotScrollable), errors.Is(err, uia.ErrNoScrollItem), errors.Is(err, uia.ErrScrollSteps),
		errors.Is(err, uia.ErrNoTextPattern), errors.Is(err, uia.ErrNoTextSelection), errors.Is(err, uia.ErrTextUnreadable),
		errors.Is(err, uia.ErrTextRange), errors.Is(err, uia.ErrTextSpan),
		errors.Is(err, uia.ErrSplicePassword), errors.Is(err, uia.ErrSpliceNoValue), errors.Is(err, uia.ErrSpliceRichText),
		errors.Is(err, uia.ErrSpliceMismatch), errors.Is(err, uia.ErrSpliceNoCaret):
		return true
	}
	return false
}

// token renders the target the way the agent typed it, for a retry hint:
// the Element Index, or the name:/id: reference (quoted when it has
// spaces, so the shell keeps it one argument).
func (t elementTarget) token() string {
	switch {
	case t.index != nil:
		return strconv.Itoa(*t.index)
	case strings.ContainsAny(t.ref, " \t\""):
		return fmt.Sprintf("%q", t.ref)
	}
	return t.ref
}
