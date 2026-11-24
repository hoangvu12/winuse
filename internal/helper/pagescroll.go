package helper

// Say when a Page scroll moved nothing (.scratch/bladebro-ideas ticket 03,
// after bladebro's scroll verdict, bb/src/action/verdict.rs:421-501). A
// Headless Page wheel (scroll <el> <dir> [n] and scroll @x,y) arms the
// effect watch in its scroll mode (page.ArmScroll) at the wheel point just
// before the wheel, and reads it once the scroll settled. The scroll stays
// `ok`, since the wheel was delivered, and gets at most one note:
//
//	Note: nothing scrolled; the Page is at its bottom | top | right edge | left edge
//	Note: nothing scrolled; the Page does not scroll [sideways]
//	Note: nothing scrolled; <scroller> is at its end | start[, and the Page is at its bottom]
//	Note: nothing scrolled where the wheel landed
//	Note: the Page is now at its bottom | <scroller> is now at its end
//
// A scroll that moved and did not reach an end prints nothing more, nor
// does a watch that could not read the scrollers. Nothing is sent again.
// scroll <el> into-view says when the Element's box did not move: it was
// already in view. Native scrolls go through UIA ScrollPattern instead.

import (
	"context"
	"math"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/page"
)

// The scroll check's tolerances, in CSS px: a move under scrollMoved is
// none, and an offset within scrollEdge of an end sits at that end
// (fractional offsets on a zoomed Page).
const (
	scrollMoved = 1.0
	scrollEdge  = 2.0
)

// pageScrollWatch is one Page wheel's scroll check.
type pageScrollWatch struct {
	dx, dy float64 // the wheel, in CSS px (positive scrolls down, right)
	rep    *page.ScrollReport
}

// wheel arms the watch at p on the Tab of o, sends the wheel, and reads
// the watch once the wheel was delivered; a wheel that failed or sent
// nothing closes it.
func (sw *pageScrollWatch) wheel(ctx context.Context, o *page.Origin, in *page.Input, p page.Point, send func() error) error {
	actx, cancel := context.WithTimeout(ctx, pageProbeBudget)
	w := page.ArmScroll(actx, o.Client, o.TopSession(), p)
	cancel()
	err := send()
	if in.Sent == 0 || err != nil {
		w.Close()
		return err
	}
	sw.rep = w.Read(ctx).Scroll
	if r, ok := w.Settled(); ok {
		recordSettled(ctx, o.Client, o.TopSession(), r)
	}
	return nil
}

// finish adds the scroll's note to a plain `ok`. An output that already
// holds a note (a dialog the wheel opened) keeps it alone.
func (sw *pageScrollWatch) finish(out string, rerr *Error) (string, *Error) {
	if rerr != nil || out != okOutput || sw.rep == nil {
		return out, rerr
	}
	if note := scrollNote(*sw.rep, sw.dx, sw.dy); note != "" {
		return okWith(note), nil
	}
	return out, nil
}

// scrollNote is the note for a wheel of dx, dy that left the scrollers as
// r says, "" for none.
func scrollNote(r page.ScrollReport, dx, dy float64) string {
	b, a := r.Before, r.After
	pageY, pageX := moved(b.Y, a.Y), moved(b.X, a.X)
	elY, elX := scrollerMoved(b.V, a.V), scrollerMoved(b.H, a.H)
	if pageY || pageX || elY || elX {
		// Where it now sits, when the wheel took it to the end.
		switch {
		case dy != 0 && pageY:
			if at := pageEnd(a.Y, a.MaxY, dy, "bottom", "top"); at != "" {
				return "the Page is now at its " + at
			}
		case dy != 0 && elY:
			if atEnd(a.V.At, a.V.Max, dy) {
				return a.V.El + " is now at its " + endWord(dy)
			}
		case dx != 0 && pageX:
			if at := pageEnd(a.X, a.MaxX, dx, "right edge", "left edge"); at != "" {
				return "the Page is now at its " + at
			}
		case dx != 0 && elX:
			if atEnd(a.H.At, a.H.Max, dx) {
				return a.H.El + " is now at its " + endWord(dx)
			}
		}
		return ""
	}
	var parts []string
	if dy != 0 {
		parts = append(parts, stuck(a.V, a.Y, a.MaxY, dy, "bottom", "top", "")...)
	}
	if dx != 0 {
		parts = append(parts, stuck(a.H, a.X, a.MaxX, dx, "right edge", "left edge", " sideways")...)
	}
	if len(parts) == 0 {
		return "nothing scrolled where the wheel landed"
	}
	return "nothing scrolled; " + strings.Join(parts, ", and ")
}

// stuck says why one axis did not move: the scroller under the point at its
// end, and the Page at its end (or not scrolling on that axis at all).
func stuck(el *page.ScrollerPos, at, max, delta float64, fwd, back, axis string) []string {
	var parts []string
	if el != nil && atEnd(el.At, el.Max, delta) {
		parts = append(parts, el.El+" is at its "+endWord(delta))
	}
	switch {
	case max <= scrollMoved:
		parts = append(parts, "the Page does not scroll"+axis)
	default:
		if end := pageEnd(at, max, delta, fwd, back); end != "" {
			parts = append(parts, "the Page is at its "+end)
		}
	}
	return parts
}

// pageEnd names the end of the Page's axis an offset sits at in the
// wheel's direction, "" when it is not at that end.
func pageEnd(at, max, delta float64, fwd, back string) string {
	if !atEnd(at, max, delta) {
		return ""
	}
	if delta > 0 {
		return fwd
	}
	return back
}

// atEnd reports whether an offset sits at the end the wheel goes toward.
func atEnd(at, max, delta float64) bool {
	if delta > 0 {
		return at >= max-scrollEdge
	}
	return at <= scrollEdge
}

func endWord(delta float64) string {
	if delta > 0 {
		return "end"
	}
	return "start"
}

func moved(before, after float64) bool { return math.Abs(after-before) >= scrollMoved }

func scrollerMoved(b, a *page.ScrollerPos) bool {
	return b != nil && a != nil && moved(b.At, a.At)
}

// intoViewNote is scroll into-view's note: the Element's box read before
// and after did not move, so it was already in view. A box that could not
// be read, or has no size, says nothing.
func intoViewNote(index int, before, after page.Rect, ok bool) string {
	if !ok || before.Width <= 0 || before.Height <= 0 ||
		moved(before.Left, after.Left) || moved(before.Top, after.Top) {
		return ""
	}
	return "element " + strconv.Itoa(index) + " was already in view; nothing scrolled"
}
