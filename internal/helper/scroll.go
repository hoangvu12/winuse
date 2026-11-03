package helper

// Element scroll (headless-parity spec, "Element scroll"; ADR 0011): the
// Element forms of `scroll`. `scroll <window> <element> <up|down|left|right>
// [steps]` scrolls through the Element's ScrollPattern, or its nearest
// scrollable ancestor's, and `scroll <window> <element> into-view` through
// its ScrollItem pattern; both are Headless (runHeadless) and read back.
// With `headed` they run v1's pipeline (runAction): activate, resolve the
// Element's point the way a Headed click does, then the wheel at that
// point, steps x 120; into-view makes the same ScrollItem call after the
// activation. The `@x,y dx dy` form (pointer.go) is unchanged and Headed.

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// intoViewWord is the direction word of a scroll into view.
const intoViewWord = "into-view"

// wheelNotch is one wheel notch in wheel units.
const wheelNotch = 120

// elementScrollPlan is a parsed Element scroll: the target and either a
// direction with steps or into-view.
type elementScrollPlan struct {
	target   elementTarget
	intoView bool
	dir      uia.ScrollDirection
	steps    int
}

// isElementScroll reports whether scroll's arguments name an Element (the
// Element forms) rather than a point.
func isElementScroll(a protocol.ScrollArgs) bool {
	return a.ElementIndex != nil || a.Ref != ""
}

// parseElementScroll checks an Element form's arguments: one Element
// Reference (an Element Index or name:/id:), no point or amounts, and a
// direction (up, down, left or right with steps 1 to 50, default 3; or
// into-view with no steps). The command and the routing predicate both
// read it, so they agree on what is an Element scroll.
func parseElementScroll(a protocol.ScrollArgs) (elementScrollPlan, *Error) {
	var p elementScrollPlan
	if a.X != 0 || a.Y != 0 || a.ScrollX != 0 || a.ScrollY != 0 {
		return p, Fail("scroll: pass an element reference with a direction, or an @x,y point with two amounts, not both", scrollUsageHint)
	}
	target, terr := parseElementTarget("scroll", a.ElementIndex, a.Ref, nil, scrollUsageHint)
	if terr != nil {
		return p, terr
	}
	p.target = target
	word := strings.TrimSpace(a.Direction)
	switch {
	case word == "":
		return p, Fail("scroll: a direction is required: up, down, left, right or into-view", scrollUsageHint)
	case strings.EqualFold(word, intoViewWord):
		if a.Steps != nil {
			return p, Fail("scroll: into-view takes no steps", scrollUsageHint)
		}
		p.intoView = true
		return p, nil
	}
	dir, err := uia.ParseScrollDirection(word)
	if err != nil {
		return p, Fail("scroll: "+err.Error()+", or into-view", scrollUsageHint)
	}
	p.dir, p.steps = dir, uia.DefaultScrollSteps
	if a.Steps != nil {
		p.steps = *a.Steps
	}
	if err := uia.CheckScrollSteps(p.steps); err != nil {
		return p, Fail("scroll: "+err.Error(), scrollUsageHint)
	}
	return p, nil
}

// headlessElementScroll is the routing predicate's half for scroll: an
// Element form that parses, without `headed`.
func headlessElementScroll(a protocol.ScrollArgs) bool {
	if a.Headed || !isElementScroll(a) || !elementForm(a.ElementIndex, a.Ref, nil) {
		return false
	}
	_, perr := parseElementScroll(a)
	return perr == nil
}

// verb is the Agent Cursor label's verb: `scroll down`, `scroll into view`.
func (p elementScrollPlan) verb() string {
	if p.intoView {
		return "scroll into view"
	}
	return "scroll " + p.dir.String()
}

// token renders the direction (and steps, when not the default) the way
// the agent types them, for a retry hint.
func (p elementScrollPlan) token() string {
	if p.intoView {
		return intoViewWord
	}
	if p.steps == uia.DefaultScrollSteps {
		return p.dir.String()
	}
	return p.dir.String() + " " + strconv.Itoa(p.steps)
}

// wheelDelta is the Headed wheel scroll for a direction and steps, in
// scroll's agent-facing signs (positive dy scrolls down, positive dx
// right): one notch per step.
func wheelDelta(dir uia.ScrollDirection, steps int) (dx, dy int32) {
	units := int32(steps * wheelNotch)
	switch dir {
	case uia.ScrollUp:
		return 0, -units
	case uia.ScrollDown:
		return 0, units
	case uia.ScrollLeft:
		return -units, 0
	}
	return units, 0
}

// scrollElement runs an Element form of scroll: Headless through runHeadless,
// or with `headed` through runAction.
func scrollElement(ctx context.Context, s Services, args json.RawMessage, a protocol.ScrollArgs) (string, *Error) {
	plan, perr := parseElementScroll(a)
	if perr != nil {
		return "", perr
	}
	// A Page Element routes over the Browser Connection (pageactions.go).
	if out, rerr, ok := s.onPage(ctx, a.Window, plan.target, func(w win.WindowInfo, index int, o *page.Origin) (string, *Error) {
		return s.pageScroll(ctx, w, index, o, plan, a.Headed)
	}); ok {
		return out, rerr
	}
	run := headlessRun{
		cmd:    "scroll",
		verb:   plan.verb(),
		target: plan.target,
	}
	run.refusal = func(err error, w win.WindowInfo, index int) *Error {
		return scrollRefusal(err, w, index, run.target, plan)
	}
	if ha, ok := s.headless("scroll", args); ok {
		run.act = func(ctx context.Context, w win.WindowInfo, index int) error {
			if plan.intoView {
				return ha.ScrollElementIntoView(ctx, w.ID, index)
			}
			return ha.ScrollElement(ctx, w.ID, index, plan.dir, plan.steps)
		}
		return s.runHeadless(ctx, a.Window, ha, run)
	}
	if !a.Headed {
		// Unreachable on a real helper (its UIA client is a HeadlessActor):
		// an element side that cannot scroll headless.
		return "", s.headedOnly(a.Window, "scroll has no background route on this helper",
			func(id string) string { return "scroll " + id + " " + plan.target.token() + " " + plan.token() }, "")
	}

	if plan.intoView {
		// The ScrollItem call a Headed click makes, after the activation.
		ha, ok := s.Actions.(HeadlessActor)
		if !ok {
			return "", Fail("scroll into-view is unavailable: no UIA client", "")
		}
		return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
			index, rerr := plan.target.resolve(ctx, s, w)
			if rerr != nil {
				return "", rerr
			}
			if err := ha.ScrollElementIntoView(ctx, w.ID, index); err != nil {
				return "", headlessFailure(err, w, index, run)
			}
			return okOutput, nil
		})
	}

	if s.Actions == nil {
		return "", Fail("scroll is unavailable: no UIA client", "")
	}
	dx, dy := wheelDelta(plan.dir, plan.steps)
	return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
		index, rerr := plan.target.resolve(ctx, s, w)
		if rerr != nil {
			return "", rerr
		}
		// The Element's point the way a Headed click resolves it (scrolled
		// into view first when it offers ScrollItem).
		pt, err := s.Actions.ElementPoint(ctx, w.ID, index)
		if err != nil {
			return "", elementError(err, w, index)
		}
		if err := s.Scroll(w.ID, pt.X, pt.Y, dx, dy); err != nil {
			return "", inputError(err)
		}
		return okOutput, nil
	})
}

// scrollRefusal is Element scroll's element-side mapping for the refusals
// raised before any pattern call (headlessFailure adds "nothing was
// sent"): nothing up the ancestor chain scrolls that way, or the Element
// has no ScrollItem; the rest is the shared stale and state mapping.
func scrollRefusal(err error, w win.WindowInfo, index int, target elementTarget, p elementScrollPlan) *Error {
	id := strconv.FormatUint(w.ID, 10)
	switch {
	case errors.Is(err, uia.ErrNotScrollable):
		return Fail(err.Error(),
			"nothing here scrolls that way through UI Automation; to wheel-scroll at the Element with the real pointer (this brings the Window to the foreground): winuse --headed scroll "+
				id+" "+target.token()+" "+p.token())
	case errors.Is(err, uia.ErrNoScrollItem):
		return Fail(err.Error(),
			"scroll its list or page by steps instead, e.g. winuse scroll "+id+" "+target.token()+" down")
	case errors.Is(err, uia.ErrDisabled):
		return Fail(err.Error(), "a disabled Element cannot be acted on; read a fresh Window State and pick another Element")
	}
	return elementError(err, w, index)
}
