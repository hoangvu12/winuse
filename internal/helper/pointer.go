package helper

// The pointer Action commands (ticket 10): scroll, drag and hover. All
// three aim at points of the target Window's latest screenshot (@x,y
// coordinate Element References), resolved exactly like the coordinate
// click (ticket 08's shotPoint): the screenshot pixel maps through the
// recorded Mapping to the Window's physical pixel, anchored at the
// extended frame bounds re-read at Action time — so input lands exactly
// where aimed at any display scaling, on any monitor, and survives the
// Window moving since the screenshot. Like every Action they run through
// runAction (resolve, refusals, overlay bracket, activation, then act)
// and print `ok` or a one-line failure with a recovery hint.
//
// Signs and units follow Codex's scroll: positive scrollY scrolls down,
// positive scrollX scrolls right, amounts are in wheel units (one notch
// is 120); drag is left-button only (Codex supports no other). Hover is
// a winuse extension: move the pointer and leave it there.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// The commands' usage lines (also the argument-failure hints).
const (
	scrollUsageHint = "usage: winuse scroll <window> @x,y <dx> <dy>   (positive dy scrolls down; one wheel notch is 120)"
	dragUsageHint   = "usage: winuse drag <window> @x,y @x,y   (from, to — both in the latest screenshot's pixels)"
	hoverUsageHint  = "usage: winuse hover <window> @x,y   (the point, in the latest screenshot's pixels)"
)

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
// (positive dy scrolls down).
func scroll(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.ScrollArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("scroll: "+err.Error(), scrollUsageHint)
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
		return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
			pt, serr := shotPoint(s, w, x, y, "scroll")
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
// apps register it as a drag.
func drag(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.DragArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("drag: "+err.Error(), dragUsageHint)
		}
		if a.FromX < 0 || a.FromY < 0 || a.ToX < 0 || a.ToY < 0 {
			return "", Fail("drag: the @x,y coordinates must be non-negative integers", dragUsageHint)
		}
		fx, fy, tx, ty := a.FromX, a.FromY, a.ToX, a.ToY
		return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
			from, serr := shotPoint(s, w, fx, fy, "drag")
			if serr != nil {
				return "", serr
			}
			to, serr := shotPoint(s, w, tx, ty, "drag")
			if serr != nil {
				return "", serr
			}
			if err := s.Drag(w.ID, int32(from.X), int32(from.Y), int32(to.X), int32(to.Y),
				func() bool { return s.Esc.Stopped() || ctx.Err() != nil }); err != nil {
				return "", inputAbortError(s, ctx, err, "during the drag")
			}
			return okOutput, nil
		})
	}
}

// hover runs `winuse hover <window> @x,y`: the Window is activated and
// verified in the foreground, the point is mapped from the latest
// screenshot's pixels to the screen, and the pointer is moved there and
// left there — an injected move plus the pixel-exact position — so
// hover-only UI (toolbars, tooltips) appears and stays.
func hover(s Services) Command {
	return func(ctx context.Context, args json.RawMessage) (string, *Error) {
		var a protocol.HoverArgs
		if err := parseArgs(args, &a); err != nil {
			return "", Fail("hover: "+err.Error(), hoverUsageHint)
		}
		if a.X < 0 || a.Y < 0 {
			return "", Fail("hover: the @x,y coordinates must be non-negative integers", hoverUsageHint)
		}
		x, y := a.X, a.Y
		return s.runAction(ctx, a.Window, func(ctx context.Context, w win.WindowInfo) (string, *Error) {
			pt, serr := shotPoint(s, w, x, y, "hover")
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

// abs returns |v| without pulling math in for two comparisons.
func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
