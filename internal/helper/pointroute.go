package helper

// Point forms resolved to an Element in the target Window (headless-max
// ticket 11, ADR 0013 rule 1 and its reversal of ADR 0010 Decision 1).
// `click @x,y` (left, single), `set-value @x,y`, `perform-secondary-action
// @x,y` (except Raise) and `scroll @x,y` outside a Page map the point from
// the latest screenshot to the screen as before, then resolve it against
// the Window's own Elements (PointResolver), never the desktop, and run
// the Element's Headless route. The output names the Element acted on, so
// the agent sees a mismatch. A point that names no Element the Action can
// use is refused with nothing sent and the --headed retry (headedgate.go).

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

// PointResolver resolves a screen point to the Element of a Window's
// latest Window State that an Action acts on, reading live bounds inside
// that Window only. The real implementation is *uia.Client; Services.Actions
// is type-asserted to it, so a fake without it keeps the point forms
// Headed-only.
type PointResolver interface {
	ElementAtPoint(ctx context.Context, hwnd uint64, x, y int32, need uia.PointNeed, action string) (uia.PointHit, error)
}

var _ PointResolver = (*uia.Client)(nil)

// pointActor is the helper's element side when it can act at a point
// Headless: a HeadlessActor that resolves points.
func (s Services) pointActor() (HeadlessActor, PointResolver, bool) {
	actor, ok := s.Actions.(HeadlessActor)
	if !ok {
		return nil, nil, false
	}
	pr, ok := s.Actions.(PointResolver)
	return actor, pr, ok
}

// pointStep is the routing predicate's half for the point forms, the
// Batch's scope question: an unheaded left single click, set-value,
// Secondary Action other than Raise, or scroll by at most MaxScrollSteps
// per axis, at @x,y. Such a step runs Headless at the point or is refused
// with nothing sent; it never goes Headed.
func pointStep(kind string, args json.RawMessage) bool {
	switch kind {
	case "click":
		var a protocol.ClickArgs
		if json.Unmarshal(args, &a) != nil || a.Headed || a.Point == nil || a.ElementIndex != nil || a.Ref != "" {
			return false
		}
		button, ok := parseMouseButton(a.MouseButton)
		return ok && button == win.MouseLeft && (a.ClickCount == 0 || a.ClickCount == 1)
	case "set-value":
		var a protocol.SetValueArgs
		return json.Unmarshal(args, &a) == nil && !a.Headed && a.Point != nil && a.ElementIndex == nil && a.Ref == ""
	case "perform-secondary-action":
		var a protocol.SecondaryActionArgs
		if json.Unmarshal(args, &a) != nil || a.Headed || a.Point == nil || a.ElementIndex != nil || a.Ref != "" {
			return false
		}
		action, ok := uia.CanonicalSecondaryAction(a.Action)
		return ok && action != "Raise"
	case "scroll":
		var a protocol.ScrollArgs
		if json.Unmarshal(args, &a) != nil || a.Headed || isElementScroll(a) || (a.ScrollX == 0 && a.ScrollY == 0) {
			return false
		}
		return wheelSteps(a.ScrollX) <= uia.MaxScrollSteps && wheelSteps(a.ScrollY) <= uia.MaxScrollSteps
	}
	return false
}

// pointRun is one Action at a point.
type pointRun struct {
	x, y   int           // the point, in the latest screenshot's pixels
	need   uia.PointNeed // what the Element must offer
	action string        // NeedAction: the canonical Secondary Action
	done   string        // the output's past-tense verb: "clicked", "set the value of"
	run    headlessRun   // the Headless route; its target is set to the resolved Element
	retry  func(id string) string
}

// runAtPoint runs an Action at a point Headless: the Window resolves and
// its refusals run (nothing sent), the point maps through the latest
// screenshot, the Window's own Elements resolve it, and the Element's
// Headless route runs through runHeadless. Every failure before the route
// says nothing was sent; a point that names no usable Element gives the
// --headed retry.
func (s Services) runAtPoint(ctx context.Context, ref string, actor HeadlessActor, pr PointResolver, p pointRun) (string, *Error) {
	w, rerr := resolveWindow(s, ref)
	if rerr != nil {
		return "", rerr
	}
	if rerr := s.headlessRefusals(w, adminPatternHint); rerr != nil {
		return "", rerr
	}
	pt, serr := shotPoint(ctx, s, w, p.x, p.y, p.run.cmd)
	if serr != nil {
		return "", nothingSent(serr)
	}
	hit, err := pr.ElementAtPoint(ctx, w.ID, int32(pt.X), int32(pt.Y), p.need, p.action)
	if err != nil {
		return "", pointFailure(err, w, p)
	}
	index := hit.Index
	p.run.target = elementTarget{index: &index}
	out, rerr := s.runHeadless(ctx, strconv.FormatUint(w.ID, 10), actor, p.run)
	if rerr != nil {
		return "", rerr
	}
	return withNote(out, pointNote(p.done, hit, p.x, p.y)), nil
}

// pointFailure maps the hit-test's failure: a point that names no usable
// Element gives the --headed retry; a missing or stale Window State asks
// for a fresh read. Nothing was sent either way.
func pointFailure(err error, w win.WindowInfo, p pointRun) *Error {
	var pe *uia.PointError
	if errors.As(err, &pe) {
		at := pointToken(p.x, p.y)
		why := fmt.Sprintf("%s at %s has no background route: %s", p.run.cmd, at, pe.Error())
		alt := ""
		if pe.Kind == uia.PointAmbiguous {
			alt = fmt.Sprintf("name the Element by its Element Index (%d or %d), name: or id: from winuse get-window-state", pe.Index, pe.Other)
		}
		return headedRefusal(w, why, p.retry, alt)
	}
	if errors.Is(err, uia.ErrNoState) {
		return nothingSent(elementError(err, w, -1))
	}
	return nothingSent(Fail(err.Error(), ""))
}

// pointNote names the Element acted on at the point: `clicked element 5
// button "Save" at @412,88`.
func pointNote(done string, hit uia.PointHit, x, y int) string {
	what := "element " + strconv.Itoa(hit.Index)
	if hit.Role != "" {
		what += " " + hit.Role
	}
	if name := strings.Join(strings.Fields(hit.Name), " "); name != "" {
		what += " " + strconv.Quote(name)
	}
	return done + " " + what + " at " + pointToken(x, y)
}

// withNote puts a note into an `ok` output: `ok (note)`, or the note first
// inside an existing `ok (...)`; any further lines stay as they are.
func withNote(out, note string) string {
	first, rest, _ := strings.Cut(out, "\n")
	if rest != "" {
		rest = "\n" + rest
	}
	switch {
	case first == "ok":
		return "ok (" + note + ")\n" + strings.TrimPrefix(rest, "\n")
	case strings.HasPrefix(first, "ok (") && strings.HasSuffix(first, ")"):
		return "ok (" + note + "; " + strings.TrimPrefix(first, "ok (") + "\n" + strings.TrimPrefix(rest, "\n")
	}
	return out
}

// wheelDirection is the Element scroll direction of a wheel amount in
// scroll's signs (positive dy down, positive dx right).
func wheelDirection(horizontal bool, units int) uia.ScrollDirection {
	switch {
	case horizontal && units < 0:
		return uia.ScrollLeft
	case horizontal:
		return uia.ScrollRight
	case units < 0:
		return uia.ScrollUp
	}
	return uia.ScrollDown
}

// pointScrollVerb is the Agent Cursor's label for a scroll at a point:
// the vertical direction when there is one.
func pointScrollVerb(dx, dy int) string {
	if dy != 0 {
		return "scroll " + wheelDirection(false, dy).String()
	}
	return "scroll " + wheelDirection(true, dx).String()
}

// wheelSteps converts a wheel amount (120 per notch) to Element scroll
// steps: three per notch, as Windows scrolls three lines per notch by
// default, rounded up so any amount scrolls.
func wheelSteps(units int) int {
	if units < 0 {
		units = -units
	}
	return (units*3 + wheelNotch - 1) / wheelNotch
}
