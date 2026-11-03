package helper

// Page Actions (browser merge ticket 05, spec "Routing and page Actions",
// ADR 0012). An Action routes by its target Element's origin: a UIA
// Element keeps today's routes; a Page Element (an Element Index of a
// browser Window's Page, pagestate.go) goes over the Browser Connection
// with trusted CDP input, Headless: no activation, no pointer, no real
// keys, no banner, in the passive Esc scope, shown by the Agent Cursor.
//
//	command                     UIA Element          Page Element                    Page Element, --headed
//	click (any button, count)   today's routes       CDP mouse events, Headless      v1 click at the transformed point
//	set-value                   today's routes       DOM.focus + insertText          refused before activating
//	scroll <el> <dir> [n]       today's routes       CDP mouseWheel, Headless        v1 wheel at the transformed point
//	scroll <el> into-view       today's routes       DOM.scrollIntoViewIfNeeded      the same, after the activation
//	perform-secondary-action    today's routes       refused (click it instead)      refused
//	scroll @x,y in the Page     -                    CDP mouseWheel, Headless        (no --headed form)
//	type-text, press-key        today's routes       CDP insertText and key events   v1 (--headed skips the probe)
//	select-text                 selecttext.go        DOM.focus + a selection         (no --headed form)
//
// type-text and press-key reach the Page when the Window's latest Window
// State holds its Page, its Tab is still the active one, and the Page's
// focused element is not the body (pageHasFocus). Otherwise, the address
// bar included, they keep today's routes.
//
// Every refusal raised before the first input event says "nothing was
// sent". Scrolling the Element into view (DOM.scrollIntoViewIfNeeded)
// comes before the checks and is not input. Input whose handler opens a
// JavaScript dialog was delivered: the Action's `ok` is followed by a
// note naming the dialog and the dialog command to answer it, and a new
// Tab it opened gets a note too (desktop-parity 02, opened.go).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/hoangvu12/winuse/internal/keys"
	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
	"github.com/hoangvu12/winuse/internal/wstate"
)

// pageProbeBudget bounds the reads that only decide a route (the Batch's
// scope, the keyboard's focus probe): a slow Browser Connection keeps
// today's route instead of holding the request.
const pageProbeBudget = 2 * time.Second

// pageTabMatcher is the Pages service's live Tab check: the target id of
// the Tab the browser Window shows now. *livePages implements it; a fake
// that does not skips the check.
type pageTabMatcher interface {
	PageActiveTab(ctx context.Context, w win.WindowInfo) (string, error)
}

// PageActiveTab maps the browser Window to its active Tab now, with the
// same rule the Window State read uses (page.FindTab).
func (p *livePages) PageActiveTab(ctx context.Context, w win.WindowInfo) (string, error) {
	name, conn, err := p.open(ctx, w)
	switch {
	case name == "":
		return "", &page.Unavailable{Reason: "this Window is not a browser Window winuse knows"}
	case err != nil:
		return "", &page.Unavailable{Reason: fmt.Sprintf("no Browser Connection is open to %s", name)}
	}
	tab, err := page.FindTab(ctx, conn, p.geometry(w))
	if err != nil {
		return "", err
	}
	if tab.Internal {
		return "", &page.InternalPageError{URL: tab.URL}
	}
	return tab.TargetID, nil
}

// pageStoredElements reads the Elements of a Window's latest Window State
// (*uia.Client), for the Agent Cursor's label.
type pageStoredElements interface {
	StoredElements(ctx context.Context, hwnd uint64) ([]wstate.Element, bool)
}

// pageCapable reports whether this helper keeps Page Elements at all.
func (s Services) pageCapable() bool {
	_, ok := s.Actions.(PageSplicer)
	return ok
}

// pageRoute resolves an Element form to a Page Element: the Window, the
// Element Index and its origin. ok is false for a UIA Element, the @x,y
// form, and anything that does not resolve; the command's own path then
// runs and reports any failure exactly as before.
func (s Services) pageRoute(ctx context.Context, ref string, t elementTarget) (w win.WindowInfo, index int, o *page.Origin, ok bool) {
	w, index, o, refused := s.pageResolve(ctx, ref, t)
	return w, index, o, o != nil && refused == nil
}

// onPage runs act on the Page Element an Element form names (handled), and
// refuses a Page Element it has no route to. Anything else is not handled,
// and the command's own routes run. The commands whose other routes may
// activate the Window go through here, so a Page Element is refused before
// any activation.
func (s Services) onPage(ctx context.Context, ref string, t elementTarget, act func(w win.WindowInfo, index int, o *page.Origin) (string, *Error)) (out string, rerr *Error, handled bool) {
	w, index, o, refused := s.pageResolve(ctx, ref, t)
	switch {
	case refused != nil:
		return "", refused, true
	case o == nil:
		return "", nil, false
	}
	out, rerr = act(w, index, o)
	return out, rerr, true
}

// pageResolve is pageRoute's and onPage's resolution: o is nil for anything
// but a Page Element, and refused is set for a Page Element whose origin is
// missing from the latest Window State (refusePageElement).
func (s Services) pageResolve(ctx context.Context, ref string, t elementTarget) (w win.WindowInfo, index int, o *page.Origin, refused *Error) {
	if t.point != nil || !s.pageCapable() {
		return w, 0, nil, nil
	}
	w, rerr := resolveWindow(s, ref)
	if rerr != nil {
		return w, 0, nil, nil
	}
	switch {
	case t.ref != "":
		i, rerr := resolveElementRef(ctx, s, w, t.ref)
		if rerr != nil {
			return w, 0, nil, nil
		}
		index = i
	case t.index != nil:
		index = *t.index
	default:
		return w, 0, nil, nil
	}
	o, perr := pageOrigin(ctx, s, w, index)
	if perr != nil {
		return w, index, nil, refusePageElement(ctx, s, w, index)
	}
	return w, index, o, nil
}

// pageDoc finds the Page of a Window's latest Window State: the origin of
// its first Page Element (its Tab, session and document Element), or nil
// when that Window State holds no Page.
func (s Services) pageDoc(ctx context.Context, w win.WindowInfo) *page.Origin {
	splicer, ok := s.Actions.(PageSplicer)
	if !ok {
		return nil
	}
	for i := 0; ; i++ {
		o, err := splicer.ElementOrigin(ctx, w.ID, i)
		if err != nil && !errors.Is(err, uia.ErrPageElement) {
			return nil // no Window State, or past its end
		}
		if po, ok := o.(*page.Origin); ok && po != nil {
			return po
		}
	}
}

// pageTabCheck refuses a Page whose Tab is no longer the one the Window
// shows. index is the Element Index for the stale message, -1 for none.
func (s Services) pageTabCheck(ctx context.Context, w win.WindowInfo, index int, o *page.Origin) *Error {
	m, ok := s.Pages.(pageTabMatcher)
	if !ok {
		return nil
	}
	id := strconv.FormatUint(w.ID, 10)
	target, err := m.PageActiveTab(ctx, w)
	var internal *page.InternalPageError
	switch {
	case errors.As(err, &internal):
		// The Tab now shows a browser page, which winuse never acts in:
		// stale, like a switched Tab.
	case err != nil:
		return Fail(fmt.Sprintf("the active Tab of window %d could not be matched: %s", w.ID, page.Reason(err)),
			"run winuse get-window-state "+id+" again")
	case target == o.TargetID:
		return nil
	}
	if index < 0 {
		return Fail(fmt.Sprintf("window %d shows another Tab than its cached Window State", w.ID),
			"run winuse get-window-state "+id+" again")
	}
	return elementError(fmt.Errorf("%w: the Window shows another Tab", uia.ErrStale), w, index)
}

// pageFresh is the staleness check of a Page Element before an Action:
// its Tab is still the active one, and its node is still the one the
// Window State showed.
func (s Services) pageFresh(ctx context.Context, w win.WindowInfo, index int, o *page.Origin) *Error {
	if rerr := s.pagePrelude(ctx, w, o); rerr != nil { // an open dialog fails at once (ticket 06)
		return rerr
	}
	if rerr := s.pageTabCheck(ctx, w, index, o); rerr != nil {
		return rerr
	}
	return checkPageOrigin(ctx, w, index, o)
}

// pageDocTopLeft reads the UIA document Element's live top-left: the
// Agent Cursor transform's origin.
func (s Services) pageDocTopLeft(ctx context.Context, w win.WindowInfo, o *page.Origin) (left, top, right, bottom int32, err error) {
	actor, ok := s.Actions.(HeadlessActor)
	if !ok {
		return 0, 0, 0, 0, errors.New("no UIA client to read the Page's document Element")
	}
	l, t, r, b, _, err := actor.ElementBounds(ctx, w.ID, o.DocIndex)
	return l, t, r, b, err
}

// pageScreenRect turns a CSS rectangle of the Page into physical screen
// pixels (page.ToScreen); false when the document Element cannot be read.
func (s Services) pageScreenRect(ctx context.Context, w win.WindowInfo, o *page.Origin, r page.Rect) (win.Rect, bool) {
	l, t, _, _, err := s.pageDocTopLeft(ctx, w, o)
	if err != nil {
		log.Printf("reading the document Element %d of window %d for the Agent Cursor: %v", o.DocIndex, w.ID, err)
		return win.Rect{}, false
	}
	left, top, right, bottom := page.ToScreen(l, t, r)
	return win.Rect{Left: left, Top: top, Right: right, Bottom: bottom}, true
}

// pageElementName is the Element's name in the latest Window State, for
// the Agent Cursor's label; fallback when it has none there.
func (s Services) pageElementName(ctx context.Context, w win.WindowInfo, index int, fallback string) string {
	if st, ok := s.Actions.(pageStoredElements); ok {
		if els, ok := st.StoredElements(ctx, w.ID); ok && index >= 0 && index < len(els) && els[index].Name != "" {
			return els[index].Name
		}
	}
	return fallback
}

// pageRun is one Page Action's own part of runPage.
type pageRun struct {
	noun   string // the Action in messages: "the click was delivered"
	done   string // its past participle: "element 4 cannot be clicked"
	verb   string // the Agent Cursor label's verb
	w      win.WindowInfo
	index  int
	origin *page.Origin
	target elementTarget
	// check runs after the staleness check and before the aim: a
	// refusal there sends nothing (set-value's field check).
	check func(ctx context.Context) error
	aim   page.AimOptions
	// intoView marks scroll into-view: the aim's scroll is the Action,
	// so a node still outside the viewport afterwards is a failed
	// read-back, and there is no input.
	intoView bool
	// act sends the input; nil for into-view.
	act func(ctx context.Context, in *page.Input, aim page.Aim) error
}

// runPage is the Headless Page Action pipeline: refuse a locked desktop;
// then, inside the passive Esc scope, check the Element is fresh, run the
// Action's own check, scroll it into view and run the actionability
// checks (every refusal so far says "nothing was sent"), glide the Agent
// Cursor to the Element's screen rectangle (the transform), send the
// input with the Esc checkpoint before each event and the dialog race on
// each, and ripple the cursor once something was delivered. No
// activation, no banner, no pointer, no real keys. A minimized, covered
// or administrator Window takes CDP input all the same (probe-results.md
// section 4), so there is no Ready or integrity refusal.
func (s Services) runPage(ctx context.Context, run pageRun) (string, *Error) {
	w, o := run.w, run.origin
	noteRoute(ctx, w, protocol.RouteCDP) // trace.go
	if locked, desktop := s.Locked(); locked {
		return "", nothingSent(lockedDesktopFailure(desktop))
	}
	esc := s.Esc
	if esc == nil {
		esc = noopEscWatch{}
	}
	defer armPassive(esc)()

	if rerr := s.pageFresh(ctx, w, run.index, o); rerr != nil {
		return "", nothingSent(rerr)
	}
	if run.check != nil {
		if err := run.check(ctx); err != nil {
			return "", pageFailure(err, run, 0)
		}
	}
	aim, err := o.Aim(ctx, run.aim)
	if err != nil {
		var na *page.NotActionableError
		if run.intoView && errors.As(err, &na) && na.Why == page.WhyOutside {
			err = &page.VerifyError{Msg: fmt.Sprintf("the Page scrolled, but element %d is still outside its visible part", run.index)}
		}
		return "", pageFailure(err, run, 0)
	}
	label := cursorLabel(run.verb, s.pageElementName(ctx, w, run.index, aim.Name))
	rect, _ := s.pageScreenRect(ctx, w, o, aim.Rect)
	rect, shown := s.cursorTravelRect(ctx, w, rect, label)
	if esc.Stopped() {
		return "", escStopBeforeActError()
	}
	if run.act == nil {
		if shown {
			s.cursorClick(ctx, w, rect)
		}
		if esc.Stopped() {
			return "", escStopError()
		}
		return okOutput, nil
	}
	in, err := page.NewInput(ctx, o.Client, o.TopSession(), esc.Stopped, o.SessionID)
	if err != nil {
		return "", pageFailure(err, run, 0)
	}
	defer in.Close()
	defer s.holdChoosers(ctx, w, o)() // file chooser interception for this Action (ticket 06)
	s.watchNewTabs(ctx, o)
	err = run.act(ctx, in, aim)
	if shown && in.Sent > 0 {
		s.cursorClick(ctx, w, rect)
	}
	if esc.Stopped() {
		if in.Sent == 0 {
			return "", escStopBeforeActError()
		}
		return "", escStopError()
	}
	if note, ok := dialogNote(err, w); ok {
		return okOutput + note, nil
	}
	if err != nil {
		return "", pageFailure(err, run, in.Sent)
	}
	return okOutput, nil
}

// dialogNote turns a JavaScript dialog that the Action's input opened
// into a note after its `ok` (desktop-parity 02): the input was
// delivered, and the agent answers the dialog next. false for any other
// outcome.
func dialogNote(err error, w win.WindowInfo) (string, bool) {
	var dialog *page.DialogError
	if !errors.As(err, &dialog) {
		return "", false
	}
	id := strconv.FormatUint(w.ID, 10)
	return notePrefix + dialog.Error() + "; the Page takes no other input until it is answered: " + dialogHint(id) + "\n", true
}

// watchNewTabs registers a probe that finds the Tabs a Page Action opens
// (desktop-parity 02): the connection's Tabs now, compared with the Tabs
// at each poll of the reported Action's settle (opened.go). Outside a
// reported Action, or when the Tabs cannot be listed, it does nothing.
func (s Services) watchNewTabs(ctx context.Context, o *page.Origin) {
	if !reportingOpened(ctx) {
		return
	}
	lctx, cancel := context.WithTimeout(ctx, pageProbeBudget)
	before, err := page.ListTabs(lctx, o.Client)
	cancel()
	if err != nil {
		return
	}
	known := make(map[string]bool, len(before))
	for _, t := range before {
		known[t.TargetID] = true
	}
	addOpenedProbe(ctx, func(ctx context.Context) []string {
		lctx, cancel := context.WithTimeout(ctx, pageProbeBudget)
		defer cancel()
		now, err := page.ListTabs(lctx, o.Client)
		if err != nil {
			return nil
		}
		var lines []string
		for _, t := range now {
			if !known[t.TargetID] {
				lines = append(lines, fmt.Sprintf("a new Tab opened: %s %q %s", page.TabID(t.TargetID), tabTitle(t.Title), t.URL))
			}
		}
		return lines
	})
}

// pageFailure maps a Page Action's failure. sent is how many input events
// had gone out: with none, the failure says "nothing was sent".
func pageFailure(err error, run pageRun, sent int) *Error {
	w, index := run.w, run.index
	id := strconv.FormatUint(w.ID, 10)
	what := fmt.Sprintf("element %d", index)
	if index < 0 {
		what = "the focused element of the Page"
	}
	var na *page.NotActionableError
	var ns *page.NotSettableError
	var dialog *page.DialogError
	var send *page.SendError
	var verify *page.VerifyError
	var open *page.DialogOpenError
	var refusal *actRefusal
	switch {
	case errors.As(err, &open): // ticket 06: the dialog watch
		return nothingSent(dialogOpenError(w, open.Dialog))
	case errors.As(err, &refusal) && sent == 0:
		return nothingSent(Fail(refusal.msg, refusal.hint))
	case errors.Is(err, page.ErrStale):
		if index < 0 {
			return nothingSentIf(sent, Fail(fmt.Sprintf("the Page of window %d changed", w.ID), "run winuse get-window-state "+id+" again"))
		}
		return nothingSentIf(sent, elementError(fmt.Errorf("%w: %v", uia.ErrStale, err), w, index))
	case errors.As(err, &na):
		return nothingSent(Fail(fmt.Sprintf("%s cannot be %s: %s", what, run.done, na.Error()), notActionableHint(na, id, run.target)))
	case errors.As(err, &ns):
		return nothingSent(Fail(fmt.Sprintf("cannot set a value for %s: %s", what, ns.Why),
			"click it instead (winuse click "+id+" "+run.target.token()+"), or set a text field"))
	case errors.As(err, &dialog):
		return Fail(fmt.Sprintf("the %s was delivered, and %s", run.noun, dialog.Error()),
			"the Page takes no other input until the dialog is answered: run winuse dialog "+id+" accept or winuse dialog "+id+
				" dismiss (a prompt takes its text: winuse dialog "+id+" accept <text>); do not repeat the "+run.noun)
	case errors.As(err, &verify):
		return Fail(err.Error(), "the Action was delivered; do not repeat it, read the Window State: winuse get-window-state "+id)
	case errors.As(err, &send) && send.Sent == 0 && !send.NoReply && sent <= 1:
		return nothingSent(Fail(err.Error(), "the Browser Connection may have ended; run winuse browser status"))
	}
	if sent == 0 {
		return nothingSent(Fail(err.Error(), "read winuse get-window-state "+id+" and retry; run winuse browser status if it keeps failing"))
	}
	return Fail(err.Error(), "the Action may have taken effect; read the Window State (winuse get-window-state "+id+") before repeating it")
}

// nothingSentIf adds "nothing was sent" when no input event went out.
func nothingSentIf(sent int, e *Error) *Error {
	if sent == 0 {
		return nothingSent(e)
	}
	return e
}

// notActionableHint is the recovery for a refused Page Element.
func notActionableHint(na *page.NotActionableError, id string, target elementTarget) string {
	switch na.Why {
	case page.WhyCovered:
		return "close or dismiss what covers it (read the Window State again: winuse get-window-state " + id + "), then retry"
	case page.WhyDisabled:
		return "a disabled Element cannot be acted on; read a fresh Window State and pick another Element"
	case page.WhyHidden:
		return "it may sit in a closed menu or a collapsed section; open that first, then read the Window State again"
	}
	return "it may sit in a fixed or clipped area; look at a screenshot: winuse get-window-state " + id + " --screenshot"
}

// pageMouseButton maps winuse's button to CDP's.
func pageMouseButton(b win.MouseButton) page.MouseButton {
	switch b {
	case win.MouseRight:
		return page.ButtonRight
	case win.MouseMiddle:
		return page.ButtonMiddle
	}
	return page.ButtonLeft
}

// pageClick clicks a Page Element: Headless over CDP for every button and
// click count (CDP needs no pointer), or with --headed the v1 click at the
// Element's transformed screen point. The headed form runs every check
// before activating the Window, so it never activates and then refuses.
func (s Services) pageClick(ctx context.Context, w win.WindowInfo, index int, o *page.Origin, target elementTarget, button win.MouseButton, count int, headed bool, mods win.Modifiers) (string, *Error) {
	opts := page.AimOptions{Scroll: true, Enabled: true, Visible: true, HitTest: true}
	run := pageRun{noun: "click", done: "clicked", verb: pageClickVerb(button, count), w: w, index: index, origin: o, target: target, aim: opts}
	if !headed {
		run.act = func(ctx context.Context, in *page.Input, aim page.Aim) error {
			return in.ClickWith(ctx, aim.Point, pageMouseButton(button), count, mods.CDP())
		}
		return s.pageClickRun(ctx, run, o, button) // pageelements.go: chooser, download, context menu
	}
	if rerr := s.pageFresh(ctx, w, index, o); rerr != nil {
		return "", nothingSent(rerr)
	}
	aim, err := o.Aim(ctx, opts)
	if err != nil {
		return "", pageFailure(err, run, 0)
	}
	return s.runAction(ctx, strconv.FormatUint(w.ID, 10), func(ctx context.Context, w win.WindowInfo) (string, *Error) {
		x, y, rerr := s.pageScreenPoint(ctx, w, o, aim)
		if rerr != nil {
			return "", rerr
		}
		if err := s.heldInput(mods, func() error { return s.Click(w.ID, x, y, button, count) }); err != nil {
			return "", inputError(err)
		}
		return okOutput, nil
	})
}

// pageScreenPoint is the aim's point on the screen, from the document
// Element read now (after the activation of a Headed Action).
func (s Services) pageScreenPoint(ctx context.Context, w win.WindowInfo, o *page.Origin, aim page.Aim) (int32, int32, *Error) {
	l, t, _, _, err := s.pageDocTopLeft(ctx, w, o)
	if err != nil {
		return 0, 0, Fail("reading the Page's place on the screen failed: "+err.Error(),
			"run winuse get-window-state "+strconv.FormatUint(w.ID, 10)+" again")
	}
	x, y := page.PointToScreen(l, t, aim.Rect.DPR, aim.Point)
	return x, y, nil
}

// pageClickVerb is the Agent Cursor's verb for a click.
func pageClickVerb(button win.MouseButton, count int) string {
	switch {
	case button != win.MouseLeft:
		return buttonWord(button) + " click"
	case count == 2:
		return "double click"
	case count == 3:
		return "triple click"
	}
	return "click"
}

// pageSetValue writes a Page text field (page.Origin.SetValue): Headless,
// read back, never echoing a password. --headed has no UIA Element to
// write here, so it is refused before anything is activated.
func (s Services) pageSetValue(ctx context.Context, w win.WindowInfo, index int, o *page.Origin, target elementTarget, value string, headed bool) (string, *Error) {
	if headed {
		return "", nothingSent(Fail(fmt.Sprintf("set-value --headed does not reach element %d: it is in the Page of window %d and has no UI Automation Element", index, w.ID),
			"drop --headed: set-value writes Page Elements through the Browser Connection, with no foreground needed"))
	}
	if out, rerr, ok := s.pageSetChoice(ctx, w, index, o, target, value); ok { // a list or a check box (ticket 06)
		return out, rerr
	}
	var field page.Field
	return s.runPage(ctx, pageRun{
		noun: "value", done: "set", verb: "set value", w: w, index: index, origin: o, target: target,
		check: func(ctx context.Context) error {
			f, err := o.ReadField(ctx)
			if err != nil {
				return err
			}
			field = f
			return f.Refusal()
		},
		aim: page.AimOptions{Scroll: true},
		act: func(ctx context.Context, in *page.Input, aim page.Aim) error {
			return o.SetValue(ctx, in, field, value)
		},
	})
}

// pageSecondaryRefusal refuses a Secondary Action on a Page Element: the
// Page has no UIA patterns, and a click does what Toggle, Select, Expand
// and Collapse do there.
func pageSecondaryRefusal(w win.WindowInfo, index int, target elementTarget, action string) *Error {
	id := strconv.FormatUint(w.ID, 10)
	msg := fmt.Sprintf("element %d is in the Page of window %d, and Secondary Actions do not reach Page Elements", index, w.ID)
	if action == "Raise" {
		return nothingSent(Fail(msg, "a Page Element has nothing to raise; winuse activate-window "+id+" brings the Window to the foreground"))
	}
	return nothingSent(Fail(msg, "for a check box or a list, winuse set-value "+id+" "+target.token()+" on|off or <option> sets the state you name; otherwise click it: winuse click "+id+" "+target.token()+
		" (a click selects a tab, and opens or closes a menu or a disclosure)"))
}

// pageWheelDelta is an Element scroll's wheel in CSS px: one step is
// Chrome's own wheel notch (page.WheelPxPerStep).
func pageWheelDelta(dir uia.ScrollDirection, steps int) (dx, dy float64) {
	ux, uy := wheelDelta(dir, steps)
	return float64(ux) * page.WheelPxPerStep / wheelNotch, float64(uy) * page.WheelPxPerStep / wheelNotch
}

// pageScroll runs an Element form of scroll on a Page Element: by steps,
// a mouse wheel at the Element's centre; into-view,
// DOM.scrollIntoViewIfNeeded, read back. With --headed: the v1 wheel at
// the transformed point (checked before the activation), and into-view
// runs the same CDP scroll after the activation.
func (s Services) pageScroll(ctx context.Context, w win.WindowInfo, index int, o *page.Origin, plan elementScrollPlan, headed bool) (string, *Error) {
	run := pageRun{noun: "scroll", done: "scrolled", verb: plan.verb(), w: w, index: index, origin: o, target: plan.target}
	if plan.intoView {
		run.aim = page.AimOptions{Scroll: true, Visible: true}
		run.intoView = true
		if !headed {
			return s.runPage(ctx, run)
		}
		if rerr := s.pageFresh(ctx, w, index, o); rerr != nil {
			return "", nothingSent(rerr)
		}
		return s.runAction(ctx, strconv.FormatUint(w.ID, 10), func(ctx context.Context, w win.WindowInfo) (string, *Error) {
			if _, err := o.Aim(ctx, run.aim); err != nil {
				var na *page.NotActionableError
				if errors.As(err, &na) && na.Why == page.WhyOutside {
					err = &page.VerifyError{Msg: fmt.Sprintf("the Page scrolled, but element %d is still outside its visible part", index)}
				}
				return "", pageFailure(err, run, 1)
			}
			return okOutput, nil
		})
	}
	run.aim = page.AimOptions{Scroll: true, Visible: true, HitTest: true}
	if !headed {
		dx, dy := pageWheelDelta(plan.dir, plan.steps)
		run.act = func(ctx context.Context, in *page.Input, aim page.Aim) error {
			return in.Wheel(ctx, aim.Point, dx, dy)
		}
		return s.runPage(ctx, run)
	}
	if rerr := s.pageFresh(ctx, w, index, o); rerr != nil {
		return "", nothingSent(rerr)
	}
	aim, err := o.Aim(ctx, run.aim)
	if err != nil {
		return "", pageFailure(err, run, 0)
	}
	ux, uy := wheelDelta(plan.dir, plan.steps)
	return s.runAction(ctx, strconv.FormatUint(w.ID, 10), func(ctx context.Context, w win.WindowInfo) (string, *Error) {
		x, y, rerr := s.pageScreenPoint(ctx, w, o, aim)
		if rerr != nil {
			return "", rerr
		}
		if err := s.Scroll(w.ID, x, y, ux, uy); err != nil {
			return "", inputError(err)
		}
		return okOutput, nil
	})
}

// pagePoint maps an @x,y point of the latest screenshot to the Page under
// it: the Page's origin, the screen point and the point in CSS px. ok is
// false when the Window's latest Window State holds no Page or the point
// is outside the Page's document Element; @x,y then keeps today's route.
func (s Services) pagePoint(ctx context.Context, w win.WindowInfo, x, y int) (o *page.Origin, screen win.Rect, ok bool) {
	o = s.pageDoc(ctx, w)
	if o == nil || s.Shots == nil {
		return nil, screen, false
	}
	pt, serr := shotPoint(ctx, s, w, x, y, "scroll")
	if serr != nil {
		return nil, screen, false
	}
	l, t, r, b, err := s.pageDocTopLeft(ctx, w, o)
	if err != nil || int32(pt.X) < l || int32(pt.Y) < t || int32(pt.X) >= r || int32(pt.Y) >= b {
		return nil, screen, false
	}
	return o, win.Rect{Left: int32(pt.X), Top: int32(pt.Y), Right: int32(pt.X) + 1, Bottom: int32(pt.Y) + 1}, true
}

// pageScrollAt runs `scroll @x,y dx dy` on a point inside a browser
// Window's Page: a mouse wheel there over CDP, Headless. The point is the
// screenshot pixel mapped to the screen (shotPoint), then to CSS px of the
// Tab's viewport by the inverse transform (page.ToCSS); dx and dy are
// wheel units (120 a notch), sent as Chrome's own notch in CSS px.
// handled is false when the point is not on a Page: today's route runs.
func (s Services) pageScrollAt(ctx context.Context, ref string, x, y, dx, dy int, mods win.Modifiers) (out string, rerr *Error, handled bool) {
	if !s.pageCapable() {
		return "", nil, false
	}
	w, werr := resolveWindow(s, ref)
	if werr != nil {
		return "", nil, false
	}
	o, at, ok := s.pagePoint(ctx, w, x, y)
	if !ok {
		return "", nil, false
	}
	out, rerr = s.runPageInput(ctx, w, o, pageInput{
		noun:   "scroll",
		label:  "scroll",
		target: win.Rect{Left: at.Left - 6, Top: at.Top - 6, Right: at.Left + 6, Bottom: at.Top + 6},
		act: func(ctx context.Context, in *page.Input) error {
			l, t, _, _, err := s.pageDocTopLeft(ctx, w, o)
			if err != nil {
				return err
			}
			dpr, err := page.ReadDPR(ctx, o.Client, o.TopSession())
			if err != nil {
				return err
			}
			p := page.ToCSS(l, t, dpr, at.Left, at.Top)
			return in.WheelWith(ctx, p, float64(dx)*page.WheelPxPerStep/wheelNotch, float64(dy)*page.WheelPxPerStep/wheelNotch, mods.CDP())
		},
	})
	return out, rerr, true
}

// pageInput is a Page Action aimed at the Page rather than one Element:
// typing, a key, a wheel at a point.
type pageInput struct {
	noun   string // the Action in messages: "text", "key", "scroll"
	label  string
	target win.Rect // the Agent Cursor's destination, in screen px
	act    func(ctx context.Context, in *page.Input) error
	// tabChecked skips the Tab check the route already made.
	tabChecked bool
}

// runPageInput is runPage for input aimed at the Page itself: the locked
// desktop refusal, the passive Esc scope, the Tab check, the Agent
// Cursor, the input with its Esc checkpoints and dialog race, and the
// ripple once something was delivered.
func (s Services) runPageInput(ctx context.Context, w win.WindowInfo, o *page.Origin, p pageInput) (string, *Error) {
	noteRoute(ctx, w, protocol.RouteCDP) // trace.go
	if locked, desktop := s.Locked(); locked {
		return "", nothingSent(lockedDesktopFailure(desktop))
	}
	esc := s.Esc
	if esc == nil {
		esc = noopEscWatch{}
	}
	defer armPassive(esc)()
	if rerr := s.pagePrelude(ctx, w, o); rerr != nil { // an open dialog fails at once (ticket 06)
		return "", nothingSent(rerr)
	}
	if !p.tabChecked {
		if rerr := s.pageTabCheck(ctx, w, -1, o); rerr != nil {
			return "", nothingSent(rerr)
		}
	}
	rect, shown := s.cursorTravelRect(ctx, w, p.target, p.label)
	if esc.Stopped() {
		return "", escStopBeforeActError()
	}
	run := pageRun{noun: p.noun, done: "sent", w: w, index: -1, origin: o}
	in, err := page.NewInput(ctx, o.Client, o.TopSession(), esc.Stopped)
	if err != nil {
		return "", pageFailure(err, run, 0)
	}
	defer in.Close()
	defer s.holdChoosers(ctx, w, o)() // Enter or Space on a file input opens a chooser too
	s.watchNewTabs(ctx, o)
	err = p.act(ctx, in)
	if shown && in.Sent > 0 {
		s.cursorClick(ctx, w, rect)
	}
	if esc.Stopped() {
		if in.Sent == 0 {
			return "", escStopBeforeActError()
		}
		return "", escStopError()
	}
	if note, ok := dialogNote(err, w); ok {
		return okOutput + note, nil
	}
	if err != nil {
		return "", pageFailure(err, run, in.Sent)
	}
	return okOutput, nil
}

// pageHasFocus is the pure keyboard decision: the Page holds the focus
// when its focused element is a field, any other element, or a
// cross-origin iframe; not when it is the body or nothing. The address
// bar case keeps today's routes through this too: the Page's focus is the
// body after a navigation and on a new Tab.
func pageHasFocus(f page.Focus) bool {
	switch f.Kind {
	case "editable", "other", "frame":
		return true
	}
	return false
}

// pageKeyTarget decides whether type-text and press-key go to the Page:
// the Window's latest Window State holds its Page, the Tab is still the
// active one, and the Page holds the focus. Any failure keeps today's
// route.
func (s Services) pageKeyTarget(ctx context.Context, w win.WindowInfo) (*page.Origin, page.Focus, bool) {
	o := s.pageDoc(ctx, w)
	if o == nil {
		return nil, page.Focus{}, false
	}
	if page.OpenDialog(o.Client, o.TopSession()) != nil {
		return o, page.Focus{}, true // the Page's route fails at once on the dialog (ticket 06)
	}
	pctx, cancel := context.WithTimeout(ctx, pageProbeBudget)
	defer cancel()
	if s.pageTabCheck(pctx, w, -1, o) != nil {
		return nil, page.Focus{}, false
	}
	f, err := page.ReadFocus(pctx, o.Client, o.TopSession())
	if err != nil || !pageHasFocus(f) {
		return nil, page.Focus{}, false
	}
	return o, f, true
}

// pageKeys runs type-text or press-key into the Page's focused element,
// when the Page holds the focus; handled is false otherwise.
func (s Services) pageKeys(ctx context.Context, ref, noun, label string, act func(ctx context.Context, in *page.Input) error) (out string, rerr *Error, handled bool) {
	if !s.pageCapable() {
		return "", nil, false
	}
	w, werr := resolveWindow(s, ref)
	if werr != nil {
		return "", nil, false
	}
	o, f, ok := s.pageKeyTarget(ctx, w)
	if !ok {
		return "", nil, false
	}
	target, _ := s.pageScreenRect(ctx, w, o, f.Rect())
	out, rerr = s.runPageInput(ctx, w, o, pageInput{noun: noun, label: label, target: target, act: act, tabChecked: true})
	return out, rerr, true
}

// pageTypeText types text into the Page's focused element: runs of text
// through Input.insertText, a newline as Return and a tab as Tab. The
// cursor's label never shows the text.
func (s Services) pageTypeText(ctx context.Context, ref, text string) (string, *Error, bool) {
	return s.pageKeys(ctx, ref, "text", "type", func(ctx context.Context, in *page.Input) error {
		return in.TypeText(ctx, text)
	})
}

// pagePressKey presses a key or chord into the Page's focused element,
// modifiers included, through Input.dispatchKeyEvent.
func (s Services) pagePressKey(ctx context.Context, ref string, chord keys.Chord) (string, *Error, bool) {
	return s.pageKeys(ctx, ref, "key", "press "+chord.String(), func(ctx context.Context, in *page.Input) error {
		return in.PressKeys(ctx, chord)
	})
}

// pageStep is the Batch's scope question for a step that would reach the
// Page (headless.go's stepHeadless asks it first): an Element form on a
// Page Element without --headed, scroll @x,y on the Page, and press-key
// or type-text, unposted and unheaded, on a Window whose latest Window
// State holds its Page. The keyboard steps do not probe the Page's focus
// here: an earlier step (a click in a field) usually moves it, and each
// step still decides its own route when it runs.
func (s Services) pageStep(ctx context.Context, kind string, args json.RawMessage, w win.WindowInfo) bool {
	if pageOnlyStep(kind, args) { // ticket 06: the page-only Actions (pagecommands.go)
		return true
	}
	if !s.pageCapable() {
		return false
	}
	ref := strconv.FormatUint(w.ID, 10)
	switch kind {
	case "click":
		var a protocol.ClickArgs
		if json.Unmarshal(args, &a) != nil || a.Headed || !elementForm(a.ElementIndex, a.Ref, a.Point) {
			return false
		}
		_, _, _, ok := s.pageRoute(ctx, ref, elementTarget{index: a.ElementIndex, ref: a.Ref})
		return ok
	case "set-value":
		var a protocol.SetValueArgs
		if json.Unmarshal(args, &a) != nil || a.Headed || !elementForm(a.ElementIndex, a.Ref, a.Point) {
			return false
		}
		_, _, _, ok := s.pageRoute(ctx, ref, elementTarget{index: a.ElementIndex, ref: a.Ref})
		return ok
	case "select-text":
		var a protocol.SelectTextArgs
		if json.Unmarshal(args, &a) != nil || !headlessSelectText(a) {
			return false
		}
		_, _, _, ok := s.pageRoute(ctx, ref, elementTarget{index: a.ElementIndex, ref: a.Ref})
		return ok
	case "scroll":
		var a protocol.ScrollArgs
		if json.Unmarshal(args, &a) != nil {
			return false
		}
		if isElementScroll(a) {
			if !headlessElementScroll(a) {
				return false
			}
			_, _, _, ok := s.pageRoute(ctx, ref, elementTarget{index: a.ElementIndex, ref: a.Ref})
			return ok
		}
		ctx, cancel := context.WithTimeout(withAgent(context.Background(), AgentOf(ctx)), pageProbeBudget) // the Agent's store (multi-agent 03)
		defer cancel()
		_, _, ok := s.pagePoint(ctx, w, a.X, a.Y)
		return ok
	case "hover":
		// Desktop-parity 13: a UIA Element form is Headed. The Page form
		// ignores headed.
		var a protocol.HoverArgs
		if json.Unmarshal(args, &a) != nil || !elementForm(a.ElementIndex, a.Ref, nil) {
			return false
		}
		_, _, _, ok := s.pageRoute(ctx, ref, elementTarget{index: a.ElementIndex, ref: a.Ref})
		return ok
	case "drag":
		var a protocol.DragArgs
		if json.Unmarshal(args, &a) != nil || !elementForm(a.FromIndex, a.FromRef, nil) {
			return false
		}
		_, _, _, ok := s.pageRoute(ctx, ref, elementTarget{index: a.FromIndex, ref: a.FromRef})
		return ok
	case "press-key", "type-text":
		var f struct {
			Headed bool `json:"headed"`
			Post   bool `json:"post"`
		}
		if json.Unmarshal(args, &f) != nil || f.Headed || f.Post {
			return false
		}
		o := s.pageDoc(ctx, w)
		if o == nil {
			return false
		}
		ctx, cancel := context.WithTimeout(withAgent(context.Background(), AgentOf(ctx)), pageProbeBudget)
		defer cancel()
		return s.pageTabCheck(ctx, w, -1, o) == nil
	}
	return false
}
