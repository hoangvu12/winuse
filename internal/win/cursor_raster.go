package win

// The Agent Cursor's pixels (headless ticket 03): a small rasterizer over
// a premultiplied 32bpp BGRA buffer — the layout UpdateLayeredWindow
// takes from a top-down DIB section (each pixel a little-endian
// 0xAARRGGBB). Pure, so the premultiplication, the compositing, the text
// blend and the shapes are tested offline; the window side points a
// surface at its DIB section's bits.
//
// Ported from vercel-labs/agent-desktop at HEAD 9eb9b29 (Apache-2.0; see
// third_party/agent-desktop/LICENSE), modified:
//   - crates/windows/src/system/cursor_overlay/raster.rs: the rimmed dart
//     and the ripple (centre disc and spreading ring, both fading);
//   - crates/windows/src/system/cursor_overlay/rounded.rs: the highlight
//     and the label pill with its soft shadow (shadow_strength);
//   - crates/windows/src/system/cursor_overlay/fade.rs: dimming a
//     premultiplied pixel;
//   - crates/windows/src/system/cursor_overlay/text.rs: GDI draws only
//     the text, onto the opaque pill;
//   - crates/core/src/cursor_overlay/style.rs: the colours (accent
//     #4299FF, rim #111318, the fill defaulting to the accent);
//   - crates/macos/src/system/cursor_overlay/cursor_overlay_bridge.m: the
//     dart's drop shadow, the black pill with white text;
//   - crates/macos/src/system/cursor_overlay/cursor_overlay_chrome_bridge.m:
//     the highlight's tint and glow, and the two pops (ADShowBubble: the
//     label's bitmap scaled; ADHighlightShow: the highlight's layer
//     scaled about its centre).
// Our shapes are signed-distance functions antialiased by a one-pixel
// ramp at the edge (coverage = ½ − distance, clamped) rather than their
// supersampling.
//
// GDI leaves alpha zero wherever it draws, so the label's text is drawn
// white on black into the cleared text rectangle and turned into coverage
// afterwards, then composited over the pill pixels that were there: the
// pill's alpha survives everywhere, its rounded ends included.

import (
	"image/color"
	"math"
)

// cursorAccent is the Agent Cursor's accent — the dart's fill, the ripple
// and the highlight: agent-desktop's default blue. Set it to OverlayAccent
// for the banner's teal.
var cursorAccent = color.RGBA{R: 0x42, G: 0x99, B: 0xFF, A: 0xFF}

// AgentCursorAccent is the cursor's accent colour as drawn, for the e2e
// test that checks winuse's own screenshots never contain the cursor.
func AgentCursorAccent() color.RGBA { return cursorAccent }

var (
	// cursorRimColor is the dart's dark rim (style.rs DEFAULT_RIM).
	cursorRimColor = color.RGBA{R: 0x11, G: 0x13, B: 0x18, A: 0xFF}
	// cursorShadowColor is the dart's and the pill's shadow.
	cursorShadowColor = color.RGBA{A: 0xFF}
	// cursorPillColor and cursorTextColor: a black pill with white text
	// (rounded.rs draw_label_pill; the macOS bridge's blackColor bubble
	// and whiteColor text).
	cursorPillColor = color.RGBA{A: 0xFF}
	cursorTextColor = color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	// cursorPillEdgeColor at cursorPillEdgeAlpha is an optional 1 px
	// lighter edge inside the pill's outline (ours, not agent-desktop's),
	// for a black pill on a dark-themed app. Off, to match theirs exactly;
	// 0.22 turns it back on.
	cursorPillEdgeColor = color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
)

const cursorPillEdgeAlpha = 0.0

// surface is a premultiplied BGRA pixel buffer, top-down, w×h.
type surface struct {
	w, h int
	pix  []uint32
}

// clear makes every pixel fully transparent.
func (s *surface) clear() {
	clear(s.pix)
}

// premultiply returns c at opacity a (∈ [0, 1], multiplied with c.A) as a
// premultiplied BGRA pixel.
func premultiply(c color.RGBA, a float64) uint32 {
	alpha := clamp01(a) * float64(c.A) / 255
	ch := func(v uint8) uint32 { return uint32(math.Round(float64(v) * alpha)) }
	return uint32(math.Round(alpha*255))<<24 | ch(c.R)<<16 | ch(c.G)<<8 | ch(c.B)
}

// over composites the premultiplied src over dst (Porter-Duff over):
// out = src + dst·(1 − αsrc).
func over(dst, src uint32) uint32 {
	sa := src >> 24
	if sa == 0xFF {
		return src
	}
	if src == 0 {
		return dst
	}
	inv := 255 - sa
	var out uint32
	for shift := uint32(0); shift <= 24; shift += 8 {
		s := (src >> shift) & 0xFF
		d := (dst >> shift) & 0xFF
		v := s + (d*inv+127)/255
		out |= min(v, 255) << shift
	}
	return out
}

// dimPixel multiplies every channel of a premultiplied pixel by f ∈ [0, 1]
// (fade.rs dim_pixel): colour and alpha together, so the pixel recedes
// rather than darkening.
func dimPixel(p uint32, f float64) uint32 {
	if f >= 1 {
		return p
	}
	f = clamp01(f)
	var out uint32
	for shift := uint32(0); shift <= 24; shift += 8 {
		out |= uint32(math.Round(float64((p>>shift)&0xFF)*f)) << shift
	}
	return out
}

// fillSDF paints c at opacity a wherever sd (the signed distance to the
// shape: negative inside) covers a pixel, antialiased over one pixel,
// composited over what is there. Only pixels in box are visited.
func (s *surface) fillSDF(box Rect, c color.RGBA, a float64, sd func(x, y float64) float64) {
	x0, y0 := max(int(box.Left), 0), max(int(box.Top), 0)
	x1, y1 := min(int(box.Right), s.w), min(int(box.Bottom), s.h)
	for y := y0; y < y1; y++ {
		cy := float64(y) + 0.5
		for x := x0; x < x1; x++ {
			cov := clamp01(0.5 - sd(float64(x)+0.5, cy))
			if cov <= 0 {
				continue
			}
			i := y*s.w + x
			s.pix[i] = over(s.pix[i], premultiply(c, a*cov))
		}
	}
}

// fillSoft paints c at opacity a·strength(pixel centre) across box, for a
// soft edge such as a shadow, where coverage is a gradient rather than a
// shape (rounded.rs fill_graded).
func (s *surface) fillSoft(box Rect, c color.RGBA, a float64, strength func(x, y float64) float64) {
	x0, y0 := max(int(box.Left), 0), max(int(box.Top), 0)
	x1, y1 := min(int(box.Right), s.w), min(int(box.Bottom), s.h)
	for y := y0; y < y1; y++ {
		cy := float64(y) + 0.5
		for x := x0; x < x1; x++ {
			v := strength(float64(x)+0.5, cy)
			if v <= 0 {
				continue
			}
			i := y*s.w + x
			s.pix[i] = over(s.pix[i], premultiply(c, a*v))
		}
	}
}

// shadowStrength is how strongly a shadow covers a point distance outside
// its shape (negative inside): full a blur's width inside, none a blur's
// width outside, smoothstepped between (rounded.rs shadow_strength).
func shadowStrength(distance, blur float64) float64 {
	t := clamp01((distance + blur) / (blur * 2))
	return 1 - t*t*(3-2*t)
}

// polygonSD is the signed distance from (x, y) to the closed polygon pts
// (negative inside, even-odd rule).
func polygonSD(pts []fpoint, x, y float64) float64 {
	best := math.Inf(1)
	inside := false
	j := len(pts) - 1
	for i := range pts {
		a, b := pts[j], pts[i]
		if (b.Y > y) != (a.Y > y) && x < (a.X-b.X)*(y-b.Y)/(a.Y-b.Y)+b.X {
			inside = !inside
		}
		best = math.Min(best, segmentDist(a, b, x, y))
		j = i
	}
	if inside {
		return -best
	}
	return best
}

// segmentDist is the distance from (x, y) to the segment a–b.
func segmentDist(a, b fpoint, x, y float64) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	l2 := dx*dx + dy*dy
	t := 0.0
	if l2 > 0 {
		t = clamp01(((x-a.X)*dx + (y-a.Y)*dy) / l2)
	}
	return math.Hypot(x-(a.X+t*dx), y-(a.Y+t*dy))
}

// roundRectSD is the signed distance to the rounded rectangle r (float
// edges) with corner radius rad.
func roundRectSD(left, top, right, bottom, rad, x, y float64) float64 {
	cx, cy := (left+right)/2, (top+bottom)/2
	hw, hh := (right-left)/2, (bottom-top)/2
	rad = math.Max(math.Min(rad, math.Min(hw, hh)), 0)
	qx := math.Abs(x-cx) - (hw - rad)
	qy := math.Abs(y-cy) - (hh - rad)
	outside := math.Hypot(math.Max(qx, 0), math.Max(qy, 0))
	inside := math.Min(math.Max(qx, qy), 0)
	return outside + inside - rad
}

// drawDart draws the agent's dart with its tip at tip: a soft shadow
// below and to the right, the dark rim, then the fill — the path grown
// by each one's half stroke width, with round joins.
func (s *surface) drawDart(tip fpoint, scale float64, fill, rim color.RGBA) {
	pts := dartPoints(scale)
	for i := range pts {
		pts[i].X += tip.X
		pts[i].Y += tip.Y
	}
	b := dartBounds(scale)
	box := Rect{
		Left: int32(math.Floor(tip.X)) + b.Left, Top: int32(math.Floor(tip.Y)) + b.Top,
		Right: int32(math.Ceil(tip.X)) + b.Right, Bottom: int32(math.Ceil(tip.Y)) + b.Bottom,
	}
	rimGrow := cursorDartRimGrow * scale
	ox, oy := cursorDartShadowX*scale, cursorDartShadowY*scale
	blur := cursorDartShadowBlur * scale
	s.fillSoft(box, cursorShadowColor, cursorDartShadowOpacity, func(x, y float64) float64 {
		return shadowStrength(polygonSD(pts, x-ox, y-oy)-rimGrow, blur)
	})
	s.fillSDF(box, rim, 1, func(x, y float64) float64 { return polygonSD(pts, x, y) - rimGrow })
	fillGrow := cursorDartFillGrow * scale
	s.fillSDF(box, fill, 1, func(x, y float64) float64 { return polygonSD(pts, x, y) - fillGrow })
}

// drawPill draws the label pill into r (rounded.rs draw_label_pill): its
// soft shadow, then the opaque black, fully rounded body, then — when
// cursorPillEdgeAlpha is on — a faint lighter edge just inside it.
func (s *surface) drawPill(r Rect, scale float64) {
	if r.Width() <= 0 || r.Height() <= 0 {
		return
	}
	l, t, rt, b := float64(r.Left), float64(r.Top), float64(r.Right), float64(r.Bottom)
	rad := float64(r.Height()) / 2
	off := cursorPillShadowOffset * scale
	blur := math.Max(cursorPillShadowBlur*scale, 0.5)
	s.fillSoft(pillShadowBounds(r, scale), cursorShadowColor, cursorPillShadowOpacity, func(x, y float64) float64 {
		return shadowStrength(roundRectSD(l, t+off, rt, b+off, rad, x, y), blur)
	})
	sd := func(x, y float64) float64 { return roundRectSD(l, t, rt, b, rad, x, y) }
	s.fillSDF(r, cursorPillColor, 1, sd)
	if cursorPillEdgeAlpha > 0 {
		// A one-pixel band just inside the outline: distance ∈ [−1, 0].
		s.fillSDF(r, cursorPillEdgeColor, cursorPillEdgeAlpha, func(x, y float64) float64 {
			return math.Abs(sd(x, y)+0.5) - 0.5
		})
	}
}

// clipBox clips r to the surface.
func (s *surface) clipBox(r Rect) (x0, y0, x1, y1 int) {
	return max(int(r.Left), 0), max(int(r.Top), 0), min(int(r.Right), s.w), min(int(r.Bottom), s.h)
}

// takeText readies r for GDI's text: it returns the pixels there (the
// pill) and clears them to black, for GDI to draw white text onto.
func (s *surface) takeText(r Rect) []uint32 {
	x0, y0, x1, y1 := s.clipBox(r)
	if x1 <= x0 || y1 <= y0 {
		return nil
	}
	saved := make([]uint32, 0, (x1-x0)*(y1-y0))
	for y := y0; y < y1; y++ {
		row := s.pix[y*s.w+x0 : y*s.w+x1]
		saved = append(saved, row...)
		clear(row)
	}
	return saved
}

// textCoverage is the coverage of a pixel GDI drew white text onto black:
// the mean of its channels, which also folds ClearType's three subpixel
// samples into one grayscale value (and keeps ClearType's light-on-dark
// weighting).
func textCoverage(p uint32) float64 {
	return float64(p>>16&0xFF+p>>8&0xFF+p&0xFF) / (3 * 255)
}

// putText composites c at the coverage GDI left in r over the pixels
// takeText saved from there, so every pixel keeps the pill's alpha.
func (s *surface) putText(r Rect, saved []uint32, c color.RGBA) {
	x0, y0, x1, y1 := s.clipBox(r)
	if x1 <= x0 || y1 <= y0 || len(saved) != (x1-x0)*(y1-y0) {
		return
	}
	k := 0
	for y := y0; y < y1; y++ {
		row := s.pix[y*s.w+x0 : y*s.w+x1]
		for x, p := range row {
			row[x] = over(saved[k], premultiply(c, textCoverage(p)))
			k++
		}
	}
}

// drawRipple draws the click ripple centred at c at phase ∈ (0, 1): a
// solid centre disc and a spreading ring, both fading as it advances.
func (s *surface) drawRipple(c fpoint, phase, scale float64, accent color.RGBA) {
	if phase <= 0 || phase >= 1 {
		return
	}
	op := rippleOpacity(phase)
	ext := cursorRippleSize/2*scale + 1
	box := Rect{
		Left: int32(math.Floor(c.X - ext)), Top: int32(math.Floor(c.Y - ext)),
		Right: int32(math.Ceil(c.X + ext)), Bottom: int32(math.Ceil(c.Y + ext)),
	}
	core := rippleCoreRadius(phase, scale)
	s.fillSDF(box, accent, cursorRippleCoreAlpha*op, func(x, y float64) float64 {
		return math.Hypot(x-c.X, y-c.Y) - core
	})
	// The ring is the band [ring − width, ring]; a ring still smaller than
	// its width is a disc.
	ring := rippleRingRadius(phase, scale)
	hw := math.Min(cursorRippleRingWidth*scale, ring) / 2
	mid := ring - hw
	s.fillSDF(box, accent, cursorRippleRingAlpha*op, func(x, y float64) float64 {
		return math.Abs(math.Hypot(x-c.X, y-c.Y)-mid) - hw
	})
}

// drawOutline draws the highlight around target (surface coordinates),
// scaled by pop about the element's centre, into the outline window's
// own, cleared surface: a soft accent glow, a faint accent tint over the
// element and a rounded accent border padded just outside it. The
// element's interior — where only the tint lands — is filled with the
// tint directly, so a large element costs its area once and its
// perimeter in maths.
func (s *surface) drawOutline(target Rect, scale, pop float64, accent color.RGBA) {
	box, skip, px := outlineShape(target, scale, pop, accent)
	tint := premultiply(accent, cursorHighlightTint)
	x0, y0, x1, y1 := s.clipBox(box)
	for y := y0; y < y1; y++ {
		row := s.pix[y*s.w : y*s.w+s.w]
		cy := float64(y) + 0.5
		for x := x0; x < x1; x++ {
			if inRect(skip, x, y) {
				row[x] = tint
				continue
			}
			row[x] = px(float64(x)+0.5, cy)
		}
	}
}

// popOutline moves a highlight drawn at pop from to pop to. Only the
// pixels that can differ are recomputed: those either box reaches,
// except where both scales' interiors are the tint. Each frame of the pop
// thus costs a band around the outline as wide as its movement plus the
// glow, not the element's area.
func (s *surface) popOutline(target Rect, scale, from, to float64, accent color.RGBA) {
	if from == to {
		return
	}
	boxA, skipA, _ := outlineShape(target, scale, from, accent)
	boxB, skipB, px := outlineShape(target, scale, to, accent)
	x0, y0, x1, y1 := s.clipBox(unionRect(boxA, boxB))
	for y := y0; y < y1; y++ {
		row := s.pix[y*s.w : y*s.w+s.w]
		cy := float64(y) + 0.5
		for x := x0; x < x1; x++ {
			if inRect(skipA, x, y) && inRect(skipB, x, y) {
				continue
			}
			row[x] = px(float64(x)+0.5, cy)
		}
	}
}

// inRect reports whether pixel (x, y) lies in r.
func inRect(r Rect, x, y int) bool {
	return x >= int(r.Left) && x < int(r.Right) && y >= int(r.Top) && y < int(r.Bottom)
}

// outlineShape is the highlight around target at pop: the box its pixels
// can touch, the interior where it is exactly the tint (skip), and the
// pixel it puts at any point. The pop scales the whole highlight — its
// padded box about the element's centre, its border, corners and glow —
// as macOS scales the highlight's layer.
func outlineShape(target Rect, scale, pop float64, accent color.RGBA) (box, skip Rect, px func(x, y float64) uint32) {
	if pop <= 0 || math.IsNaN(pop) {
		pop = 1
	}
	pad := cursorHighlightPad * scale
	rad := cursorHighlightRadius * scale * pop
	bw := cursorHighlightBorder * scale * pop
	hw := bw / 2
	sigma := cursorHighlightGlowBlur / 2 * scale * pop
	reach := outlineGlowReach(scale) * pop
	cx, cy := float64(target.Left+target.Right)/2, float64(target.Top+target.Bottom)/2
	l, t := scaleAbout(float64(target.Left)-pad, cx, pop), scaleAbout(float64(target.Top)-pad, cy, pop)
	r, b := scaleAbout(float64(target.Right)+pad, cx, pop), scaleAbout(float64(target.Bottom)+pad, cy, pop)
	ext := reach + 1
	box = Rect{
		Left: int32(math.Floor(l - ext)), Top: int32(math.Floor(t - ext)),
		Right: int32(math.Ceil(r + ext)), Bottom: int32(math.Ceil(b + ext)),
	}
	// Farther than max(rad, border + reach) inside the outer edges, the
	// nearest boundary is a straight edge and the glow has run out: only
	// the tint remains (one pixel of slack each way).
	inset := math.Max(rad, bw+reach) + 1
	skip = Rect{
		Left: int32(math.Ceil(l + inset)), Top: int32(math.Ceil(t + inset)),
		Right: int32(math.Floor(r - inset)), Bottom: int32(math.Floor(b - inset)),
	}
	if skip.Width() <= 0 || skip.Height() <= 0 {
		skip = Rect{}
	}
	// The border's centre line: the outer edge inset by half the border.
	cl, ct, cr, cb, crad := l+hw, t+hw, r-hw, b-hw, math.Max(rad-hw, 0)
	px = func(x, y float64) uint32 {
		var p uint32
		d := math.Abs(roundRectSD(cl, ct, cr, cb, crad, x, y)) // to the centre line
		if d < hw+reach {
			// The border's shadow: a band of width bw blurred by σ.
			g := cursorHighlightGlow * (gaussCDF((d+hw)/sigma) - gaussCDF((d-hw)/sigma))
			p = premultiply(accent, g)
		}
		if cov := clamp01(0.5 - roundRectSD(l, t, r, b, rad, x, y)); cov > 0 {
			p = over(p, premultiply(accent, cursorHighlightTint*cov))
		}
		if cov := clamp01(0.5 - (d - hw)); cov > 0 {
			p = over(p, premultiply(accent, cov))
		}
		return p
	}
	return box, skip, px
}

// gaussCDF is the standard normal distribution function Φ.
func gaussCDF(x float64) float64 { return 0.5 * (1 + math.Erf(x/math.Sqrt2)) }

// scalePixels writes into dst the w×h premultiplied image src scaled by
// s about a, bilinearly (premultiplied channels interpolate correctly),
// within box only; pixels mapping outside src are transparent. It is the
// label pill's pop: CoreAnimation scales the layer's bitmap the same way.
func scalePixels(dst, src []uint32, w, h int, a fpoint, s float64, box Rect) {
	x0, y0 := max(int(box.Left), 0), max(int(box.Top), 0)
	x1, y1 := min(int(box.Right), w), min(int(box.Bottom), h)
	at := func(x, y int) uint32 {
		if x < 0 || y < 0 || x >= w || y >= h {
			return 0
		}
		return src[y*w+x]
	}
	for y := y0; y < y1; y++ {
		sy := a.Y + (float64(y)+0.5-a.Y)/s - 0.5
		fy := math.Floor(sy)
		ty := sy - fy
		iy := int(fy)
		for x := x0; x < x1; x++ {
			sx := a.X + (float64(x)+0.5-a.X)/s - 0.5
			fx := math.Floor(sx)
			tx := sx - fx
			ix := int(fx)
			p00, p10, p01, p11 := at(ix, iy), at(ix+1, iy), at(ix, iy+1), at(ix+1, iy+1)
			var out uint32
			for shift := uint32(0); shift <= 24; shift += 8 {
				c := func(p uint32) float64 { return float64((p >> shift) & 0xFF) }
				top := c(p00) + (c(p10)-c(p00))*tx
				bot := c(p01) + (c(p11)-c(p01))*tx
				out |= uint32(math.Round(top+(bot-top)*ty)) << shift
			}
			dst[y*w+x] = out
		}
	}
}
