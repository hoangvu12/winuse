package helper

// The pointer Action commands (ticket 10): scroll, drag and hover. All
// three aim at points of the target Window's latest screenshot (@x,y
// coordinate Element References), resolved exactly like the coordinate
// click (ticket 08's shotPoint): the screenshot pixel maps through the
// recorded Mapping to the Window's physical pixel, anchored at the
// extended frame bounds re-read at Action time — so input lands exactly
// where aimed at any display scaling, on any monitor, and survives the
// Window moving since the screenshot. Like every Headed Action they run
// through runAction (resolve, refusals, overlay bracket, activation, then
// act) and print `ok` or a one-line failure with a recovery hint. Only
// with `headed`: without it they are refused with nothing sent and the
// --headed retry (headless-max ticket 21), except a scroll at a point of a
// Page and the Page Element forms of drag and hover, which are Headless,
// and a scroll at a point elsewhere, which scrolls the target Window's own
// Element there through UI Automation (headless-max ticket 11).
//
// drag and hover also take a UIA Element (desktop-parity 13): the pointer
// goes to the Element's clickable point, resolved after the activation as
// a Headed click resolves it (ElementPoint), then the same drag or hover.
// A drag from an Element ends at another Element or at @x,y. They stay
// Headed, so they too need `headed`. --drop-delay holds a drag at its end
// before the drop, on the native and the Page routes.
//
// Signs and units follow Codex's scroll: positive scrollY scrolls down,
// positive scrollX scrolls right, amounts are in wheel units (one notch
// is 120); drag is left-button only (Codex supports no other). Hover is
// a winuse extension: move the pointer and leave it there.

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// The commands' usage lines (also the argument-failure hints).
const (
	scrollUsageHint = "usage: winuse scroll <window> <element index | name:<text> | id:<id>> <up|down|left|right> [steps 1-50, default 3] | " +
		"winuse scroll <window> <element> into-view | winuse scroll <window> @x,y <dx> <dy>   (positive dy scrolls down; one wheel notch is 120)"
	dragUsageHint = "usage: winuse drag <window> @x,y @x,y [--drop-delay <ms>]   (from, to; both in the latest screenshot's pixels) | " +
		"winuse drag <window> <element> <element | @x,y> [--drop-delay <ms>]"
	hoverUsageHint = "usage: winuse hover <window> @x,y   (the point, in the latest screenshot's pixels) | winuse hover <window> <element>"
)

// maxDropDelayMS bounds --drop-delay: the drag and its wait must fit the
// Action's request budget (protocol.RequestTimeout, 10 s) with the
// activation and the moves, and 5 s is far more hover time than any drop
// target asks for.
const maxDropDelayMS = 5000

// dropDelay checks --drop-delay and returns it as a duration.
func dropDelay(ms int) (time.Duration, *Error) {
	if ms < 0 || ms > maxDropDelayMS {
		return 0, Fail(fmt.Sprintf("drag: --drop-delay must be 0 to %d milliseconds, got %d", maxDropDelayMS, ms), dragUsageHint)
	}
	return time.Duration(ms) * time.Millisecond, nil
}

// dropDelayOption renders --drop-delay for a retry, left out when none.
func dropDelayOption(ms int) string {
	if ms == 0 {
		return ""
	}
	return " --drop-delay " + strconv.Itoa(ms)
}

// maxScrollUnits bounds a scroll amount: the wheel delta travels as a
// signed 16-bit value per event (the win layer chunks larger amounts),
// and a million units — over 8000 notches, far beyond any real scroll —
// keeps a runaway amount from injecting a huge batch while never
// rejecting a legitimate one.
const maxScrollUnits = 1000000

// scroll runs `winuse scroll <window> @x,y <dx> <dy>`: the Window is
// activated and verified in the foreground, the point is mapped from the
// latest screenshot's pixels to the screen, the cursor is moved there,
// and the wheel is injected — vertical and horizontal, Codex's signs
// (positive dy scrolls down). The Element forms (an Element with a
// direction, or into-view) are scrollElement's (scroll.go).
func scroll(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.ScrollArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("scroll: "+err.Error(), scrollUsageHint)
		}
		if isElementScroll(a) {
			return scrollElement(ctx, s, args, a)
		}
		if a.Direction != "" || a.Steps != nil {
			return "", Fail("scroll: a direction or steps needs an element index, name: or id: reference", scrollUsageHint)
		}
		if a.X < 0 || a.Y < 0 {
			return "", Fail("scroll: the @x,y coordinates must be non-negative integers", scrollUsageHint)
		}
		if a.ScrollX == 0 && a.ScrollY == 0 {
			return "", Fail("scroll: at least one scroll amount must be non-zero", scrollUsageHint)
		}
		if abs(a.ScrollX) > maxScrollUnits || abs(a.ScrollY) > maxScrollUnits {
			return "", Fail(fmt.Sprintf("scroll amounts must be within ±%d wheel units (one notch is 120)", maxScrollUnits), scrollUsageHint)
		}
		x, y, dx, dy := a.X, a.Y, a.ScrollX, a.ScrollY
		// A point on a browser Window's Page wheels over the Browser
		// Connection, Headless (pageactions.go).
		if out, rerr, ok := s.pageScrollAt(ctx, a.Window, x, y, dx, dy); ok {
			return out, rerr
		}
		if ha, pr, ok := s.pointActor(); ok && !a.Headed &&
			wheelSteps(dx) <= uia.MaxScrollSteps && wheelSteps(dy) <= uia.MaxScrollSteps {
			// Elsewhere the point scrolls the target Window's own Element
			// there through UI Automation, Headless (headless-max ticket
			// 11): its nearest scrollable container, three steps per wheel
			// notch, vertical first.
			return s.runAtPoint(ctx, a.Window, ha, pr, pointRun{
				x: x, y: y, need: uia.NeedScroll, done: "scrolled",
				retry: func(id string) string { return fmt.Sprintf("scroll %s %s %d %d", id, pointToken(x, y), dx, dy) },
				run: headlessRun{
					cmd:  "scroll",
					verb: pointScrollVerb(dx, dy),
					act: func(ctx context.Context, w win.WindowInfo, index int) error {
						if dy != 0 {
							if err := ha.ScrollElement(ctx, w.ID, index, wheelDirection(false, dy), wheelSteps(dy)); err != nil {
								return err
							}
						}
						if dx != 0 {
							return ha.ScrollElement(ctx, w.ID, index, wheelDirection(true, dx), wheelSteps(dx))
						}
						return nil
					},
					refusal: func(err error, w win.WindowInfo, index int) *Error {
						return elementError(err, w, index)
					},
				},
			})
		}
		if !a.Headed {
			return "", s.headedOnly(a.Window,
				fmt.Sprintf("scroll at %s has no background route outside a Page: the wheel needs the real pointer", pointToken(x, y)),
				func(id string) string {
					return fmt.Sprintf("scroll %s %s %d %d", id, pointToken(x, y), dx, dy)
				},
				"to scroll without the foreground, name an Element inside the list or page: winuse scroll <window> <element> <up|down|left|right> [steps]")
		}
		return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
			pt, serr := shotPoint(ctx, s, w, x, y, "scroll")
			if serr != nil {
				return "", serr
			}
			if err := s.Scroll(w.ID, int32(pt.X), int32(pt.Y), int32(dx), int32(dy)); err != nil {
				return "", inputError(err)
			}
			return okOutput, nil
		})
	}
}

// drag runs `winuse drag <window> @x,y @x,y`: the Window is activated and
// verified in the foreground, both points are mapped from the latest
// screenshot's pixels to the screen, and a left-button drag is injected
// from the first to the second with interpolated moves in between, so
// apps register it as a drag. The Element forms are elementDrag's.
func drag(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.DragArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("drag: "+err.Error(), dragUsageHint)
		}
		drop, derr := dropDelay(a.DropDelayMS)
		if derr != nil {
			return "", derr
		}
		if a.FromIndex != nil || a.FromRef != "" {
			return s.elementDrag(ctx, a, drop)
		}
		if a.FromX < 0 || a.FromY < 0 || a.ToX < 0 || a.ToY < 0 {
			return "", Fail("drag: the @x,y coordinates must be non-negative integers", dragUsageHint)
		}
		fx, fy, tx, ty := a.FromX, a.FromY, a.ToX, a.ToY
		if !a.Headed {
			return "", s.headedOnly(a.Window,
				fmt.Sprintf("drag from %s to %s has no background route: a drag needs the real pointer", pointToken(fx, fy), pointToken(tx, ty)),
				func(id string) string {
					return "drag " + id + " " + pointToken(fx, fy) + " " + pointToken(tx, ty) + dropDelayOption(a.DropDelayMS)
				},
				"")
		}
		return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
			from, serr := shotPoint(ctx, s, w, fx, fy, "drag")
			if serr != nil {
				return "", serr
			}
			to, serr := shotPoint(ctx, s, w, tx, ty, "drag")
			if serr != nil {
				return "", serr
			}
			return s.dragBetween(ctx, w, int32(from.X), int32(from.Y), int32(to.X), int32(to.Y), drop)
		})
	}
}

// dragBetween is the Headed drag's input, inside runAction: screen point
// to screen point, held drop at the end.
func (s Services) dragBetween(ctx context.Context, w win.WindowInfo, fx, fy, tx, ty int32, drop time.Duration) (string, *Error) {
	if err := s.Drag(w.ID, fx, fy, tx, ty, drop,
		func() bool { return s.Esc.Stopped() || ctx.Err() != nil }); err != nil {
		return "", inputAbortError(s, ctx, err, "during the drag")
	}
	return okOutput, nil
}

// elementDrag runs `winuse drag <window> <element> <element | @x,y>`. From
// a Page Element it is the Page's Headless drag (pageelements.go). From a
// UIA Element it is Headed (desktop-parity 13): with `headed`, the Window
// is activated, the start is the Element's clickable point (ElementPoint,
// as a Headed click aims), the end is the target Element's or the
// screenshot point mapped like the point form, and the same drag runs.
// Without `headed` it is refused with nothing sent.
func (s Services) elementDrag(ctx context.Context, a protocol.DragArgs, drop time.Duration) (string, *Error) {
	from, rerr := parseElementTarget("drag", a.FromIndex, a.FromRef, nil, dragUsageHint)
	if rerr != nil {
		return "", rerr
	}
	var to *elementTarget
	if a.ToIndex != nil || a.ToRef != "" {
		t, rerr := parseElementTarget("drag", a.ToIndex, a.ToRef, nil, dragUsageHint)
		if rerr != nil {
			return "", rerr
		}
		to = &t
	} else if a.ToX < 0 || a.ToY < 0 {
		return "", Fail("drag: the @x,y coordinates must be non-negative integers", dragUsageHint)
	}
	if out, rerr, ok := s.onPage(ctx, a.Window, from, func(w win.WindowInfo, index int, o *page.Origin) (string, *Error) {
		return s.pageDrag(ctx, w, index, o, from, to, a.ToX, a.ToY, drop) // Headless (pageelements.go)
	}); ok {
		return out, rerr
	}
	if s.Actions == nil {
		return "", Fail("drag is unavailable: no UIA client", "")
	}
	end := pointToken(a.ToX, a.ToY)
	if to != nil {
		end = to.token()
	}
	if !a.Headed {
		return "", s.headedOnly(a.Window,
			"a drag from an Element has no background route outside a Page: a drag needs the real pointer",
			func(id string) string {
				return "drag " + id + " " + from.token() + " " + end + dropDelayOption(a.DropDelayMS)
			},
			"")
	}
	return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
		start, rerr := s.elementScreenPoint(ctx, w, from)
		if rerr != nil {
			return "", rerr
		}
		var tx, ty int32
		if to != nil {
			pt, rerr := s.elementScreenPoint(ctx, w, *to)
			if rerr != nil {
				return "", rerr
			}
			tx, ty = pt.X, pt.Y
		} else {
			pt, serr := shotPoint(ctx, s, w, a.ToX, a.ToY, "drag")
			if serr != nil {
				return "", serr
			}
			tx, ty = int32(pt.X), int32(pt.Y)
		}
		return s.dragBetween(ctx, w, start.X, start.Y, tx, ty, drop)
	})
}

// elementScreenPoint resolves an Element form to the screen point a Headed
// pointer Action aims at, inside runAction: its Element Index, then its
// clickable point or bounds centre after scrolling it into view, exactly
// as a Headed click resolves it.
func (s Services) elementScreenPoint(ctx context.Context, w win.WindowInfo, t elementTarget) (uia.Point, *Error) {
	index, rerr := t.resolve(ctx, s, w)
	if rerr != nil {
		return uia.Point{}, rerr
	}
	pt, err := s.Actions.ElementPoint(ctx, w.ID, index)
	if err != nil {
		return uia.Point{}, elementError(err, w, index)
	}
	return pt, nil
}

// hover runs `winuse hover <window> @x,y`: the Window is activated and
// verified in the foreground, the point is mapped from the latest
// screenshot's pixels to the screen, and the pointer is moved there and
// left there — an injected move plus the pixel-exact position — so
// hover-only UI (toolbars, tooltips) appears and stays. The Element form
// is elementHover's.
func hover(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.HoverArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("hover: "+err.Error(), hoverUsageHint)
		}
		if a.ElementIndex != nil || a.Ref != "" {
			return s.elementHover(ctx, a)
		}
		if a.X < 0 || a.Y < 0 {
			return "", Fail("hover: the @x,y coordinates must be non-negative integers", hoverUsageHint)
		}
		x, y := a.X, a.Y
		if !a.Headed {
			return "", s.headedOnly(a.Window,
				fmt.Sprintf("hover at %s has no background route: hovering moves the real pointer and leaves it there", pointToken(x, y)),
				func(id string) string { return "hover " + id + " " + pointToken(x, y) },
				"")
		}
		return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
			pt, serr := shotPoint(ctx, s, w, x, y, "hover")
			if serr != nil {
				return "", serr
			}
			if err := s.Hover(w.ID, int32(pt.X), int32(pt.Y)); err != nil {
				return "", inputError(err)
			}
			return okOutput, nil
		})
	}
}

// elementHover runs `winuse hover <window> <element>`. A Page Element is
// the Page's Headless hover (pageelements.go). A UIA Element is Headed
// (desktop-parity 13): with `headed`, the Window is activated and the
// pointer goes to the Element's clickable point, as a Headed click aims,
// and stays there; without it the hover is refused with nothing sent.
func (s Services) elementHover(ctx context.Context, a protocol.HoverArgs) (string, *Error) {
	target, rerr := parseElementTarget("hover", a.ElementIndex, a.Ref, nil, hoverUsageHint)
	if rerr != nil {
		return "", rerr
	}
	if out, rerr, ok := s.onPage(ctx, a.Window, target, func(w win.WindowInfo, index int, o *page.Origin) (string, *Error) {
		return s.pageHover(ctx, w, index, o, target) // Headless (pageelements.go)
	}); ok {
		return out, rerr
	}
	if s.Actions == nil {
		return "", Fail("hover is unavailable: no UIA client", "")
	}
	if !a.Headed {
		return "", s.headedOnly(a.Window,
			"hover on an Element has no background route outside a Page: hovering moves the real pointer and leaves it there",
			func(id string) string { return "hover " + id + " " + target.token() },
			"")
	}
	return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
		pt, rerr := s.elementScreenPoint(ctx, w, target)
		if rerr != nil {
			return "", rerr
		}
		if err := s.Hover(w.ID, pt.X, pt.Y); err != nil {
			return "", inputError(err)
		}
		return okOutput, nil
	})
}

// abs returns |v| without pulling math in for two comparisons.
func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
