package win

// The Agent Cursor's pixels (headless ticket 03, cursor-look): a small
// rasterizer over a premultiplied 32bpp BGRA buffer — the layout
// UpdateLayeredWindow takes from a top-down DIB section (each pixel a
// little-endian 0xAARRGGBB). Pure, so the premultiplication, the
// compositing, the text blend and the shapes are tested offline; the
// window side points a surface at its DIB section's bits.
//
// The arrow and the name tag are drawn after Mural's presence cursor
// (cursor-look issue 11): flat in the Agent's colour, a white rim outside
// and a soft black shadow below. The trail is trycua/cua's at ecaf10b
// (MIT; see third_party/cua/LICENSE): effects.rs (the trail, the lifted
// colour). We draw the upright arrow exactly once per look, finer than
// the screen, and sample every turned or pressed sprite from it.
//
// Ported from vercel-labs/agent-desktop at HEAD 9eb9b29 (Apache-2.0; see
// third_party/agent-desktop/LICENSE), modified:
//   - crates/windows/src/system/cursor_overlay/rounded.rs: the highlight
//     and the soft shadows (shadow_strength);
//   - crates/windows/src/system/cursor_overlay/fade.rs: dimming a
//     premultiplied pixel;
//   - crates/windows/src/system/cursor_overlay/text.rs: GDI draws only
//     the text, onto the tag;
//   - crates/macos/src/system/cursor_overlay/cursor_overlay_chrome_bridge.m:
//     the highlight's tint and glow, and its pop (ADHighlightShow: the
//     highlight's layer scaled about its centre).
// Our shapes are signed-distance functions antialiased by a one-pixel
// ramp at the edge (coverage = ½ − distance, clamped) rather than their
// supersampling.
//
// GDI leaves alpha zero wherever it draws, so the tag's text is drawn
// white on black into the cleared text rectangle and turned into coverage
// afterwards, then composited over the tag pixels that were there: the
// tag's alpha survives everywhere, its rounded ends included.

import (
	"image/color"
	"math"
)

// cursorAccent is the Agent Cursor's accent — the arrow's fill, the
// outline and the caret: the default Agent's mid-tone blue, ahead of the
// palette's colours (cursor_colors.go).
var cursorAccent = color.RGBA{R: 0x2F, G: 0x6F, B: 0xD6, A: 0xFF}

// AgentCursorAccent is the cursor's accent colour as drawn, for the e2e
// test that checks winuse's own screenshots never contain the cursor.
func AgentCursorAccent() color.RGBA { return cursorAccent }

var (
	// cursorRimColor is the arrow's and the tag's white rim.
	cursorRimColor = color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	// cursorShadowColor is the arrow's and the tag's shadow.
	cursorShadowColor = color.RGBA{A: 0xFF}
	// cursorTextColor is light text: white.
	cursorTextColor = color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	// cursorInkColor is dark text: a near-black, for light Agent colours.
	cursorInkColor = color.RGBA{R: 0x16, G: 0x18, B: 0x1D, A: 0xFF}
)

// cursorEffectLift lifts the accent toward white for the effects (the
// trail), as cua's effects.rs does.
const cursorEffectLift = 0.45

// pillTextColor is the text on a tag filled with accent: white or
// near-black, whichever contrasts more with it (WCAG relative luminance),
// as Miro, Mural, Pitch and Canva choose it. The palette's mid-tones take
// white, its yellow the near-black.
func pillTextColor(accent color.RGBA) color.RGBA {
	if contrast(cursorTextColor, accent) >= contrast(cursorInkColor, accent) {
		return cursorTextColor
	}
	return cursorInkColor
}

// contrast is the WCAG contrast ratio of a and b, from 1 to 21.
func contrast(a, b color.RGBA) float64 {
	la, lb := luminance(a), luminance(b)
	return (math.Max(la, lb) + 0.05) / (math.Min(la, lb) + 0.05)
}

// luminance is c's WCAG relative luminance.
func luminance(c color.RGBA) float64 {
	ch := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(c.R) + 0.7152*ch(c.G) + 0.0722*ch(c.B)
}

// mixColor is a mixed f of the way toward b.
func mixColor(a, b color.RGBA, f float64) color.RGBA {
	f = clamp01(f)
	m := func(x, y uint8) uint8 { return uint8(math.Round(float64(x) + (float64(y)-float64(x))*f)) }
	return color.RGBA{R: m(a.R, b.R), G: m(a.G, b.G), B: m(a.B, b.B), A: m(a.A, b.A)}
}

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
	best := math.Inf(1) // squared: one square root per point, not per edge
	inside := false
	j := len(pts) - 1
	for i := range pts {
		a, b := pts[j], pts[i]
		if (b.Y > y) != (a.Y > y) && x < (a.X-b.X)*(y-b.Y)/(a.Y-b.Y)+b.X {
			inside = !inside
		}
		dx, dy := b.X-a.X, b.Y-a.Y
		px, py := x-a.X, y-a.Y
		t := 0.0
		if l2 := dx*dx + dy*dy; l2 > 0 {
			t = clamp01((px*dx + py*dy) / l2)
		}
		ex, ey := px-t*dx, py-t*dy
		best = math.Min(best, ex*ex+ey*ey)
		j = i
	}
	if inside {
		return -math.Sqrt(best)
	}
	return math.Sqrt(best)
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

// arrowSuper is how much finer than the screen the arrow's master image
// is drawn: each sprite averages arrowSuper² samples of it per pixel.
const arrowSuper = 2

// arrowMaster is the upright arrow drawn exactly once per look, finer
// than the screen; every sprite — turned, pressed or neither — is sampled
// from it, so a frame never re-rasterizes the shape.
type arrowMaster struct {
	n   int     // the image's side
	c   float64 // the tip's coordinate on both axes (a pixel corner)
	pix []uint32
}

// newArrowMaster draws the upright arrow at scale·arrowSuper in fill:
// the soft shadow, the white rim outside the outline, then the body.
func newArrowMaster(scale float64, fill color.RGBA) arrowMaster {
	hs := scale * arrowSuper
	r := arrowReach(hs)
	m := arrowMaster{n: int(2*r) + 1, c: float64(r)}
	m.pix = make([]uint32, m.n*m.n)
	pts := arrowPoints(hs)
	for i := range pts {
		pts[i].X += m.c
		pts[i].Y += m.c
	}
	rim, off, blur := cursorArrowRim*hs, cursorArrowShadowOff*hs, cursorArrowShadowBlur*hs
	far := rim + blur + 1
	for y := 0; y < m.n; y++ {
		cy := float64(y) + 0.5
		for x := 0; x < m.n; x++ {
			cx := float64(x) + 0.5
			// The shadow is the rimmed shape moved down and blurred: nothing
			// shows farther off it, so most of the square costs one distance.
			sdo := polygonSD(pts, cx, cy-off)
			if sdo > far {
				continue
			}
			var p uint32
			if v := shadowStrength(sdo-rim, blur); v > 0 {
				p = premultiply(cursorShadowColor, cursorArrowShadowOp*v)
			}
			sd := polygonSD(pts, cx, cy)
			if cov := clamp01(0.5 - (sd - rim)); cov > 0 {
				p = over(p, premultiply(cursorRimColor, cov))
			}
			if cov := clamp01(0.5 - sd); cov > 0 {
				p = over(p, premultiply(fill, cov))
			}
			m.pix[y*m.n+x] = p
		}
	}
	return m
}

// sample is the master's premultiplied pixel at (x, y), bilinearly, as
// four float channels (b, g, r, a); transparent outside it.
func (m *arrowMaster) sample(x, y float64) (b, g, r, a float64) {
	x -= 0.5
	y -= 0.5
	fx, fy := math.Floor(x), math.Floor(y)
	tx, ty := x-fx, y-fy
	ix, iy := int(fx), int(fy)
	at := func(i, j int) uint32 {
		if i < 0 || j < 0 || i >= m.n || j >= m.n {
			return 0
		}
		return m.pix[j*m.n+i]
	}
	w := [4]float64{(1 - tx) * (1 - ty), tx * (1 - ty), (1 - tx) * ty, tx * ty}
	for k, p := range [4]uint32{at(ix, iy), at(ix+1, iy), at(ix, iy+1), at(ix+1, iy+1)} {
		if p == 0 {
			continue
		}
		b += w[k] * float64(p&0xFF)
		g += w[k] * float64(p>>8&0xFF)
		r += w[k] * float64(p>>16&0xFF)
		a += w[k] * float64(p>>24)
	}
	return b, g, r, a
}

// drawArrowSprite draws the arrow from m into s with its tip at tip,
// turned by angle (radians, clockwise) and scaled by press about the tip,
// over what is there, inside box only: each pixel averages arrowSuper²
// samples of the master.
func (s *surface) drawArrowSprite(m *arrowMaster, tip fpoint, angle, press float64, box Rect) {
	if press <= 0 || math.IsNaN(press) {
		press = 1
	}
	cos, sin := math.Cos(angle), math.Sin(angle)
	k := arrowSuper / press
	const n = arrowSuper
	x0, y0, x1, y1 := s.clipBox(box)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			var sb, sg, sr, sa float64
			for j := range n {
				for i := range n {
					// The sub-sample, relative to the tip, turned back by angle.
					vx := float64(x) + (float64(i)+0.5)/n - tip.X
					vy := float64(y) + (float64(j)+0.5)/n - tip.Y
					ux, uy := cos*vx+sin*vy, -sin*vx+cos*vy
					b, g, r, a := m.sample(m.c+ux*k, m.c+uy*k)
					sb, sg, sr, sa = sb+b, sg+g, sr+r, sa+a
				}
			}
			if sa <= 0 {
				continue
			}
			q := 1.0 / (n * n)
			ch := func(v float64) uint32 { return uint32(min(math.Round(v*q), 255)) }
			p := ch(sa)<<24 | ch(sr)<<16 | ch(sg)<<8 | ch(sb)
			i := y*s.w + x
			s.pix[i] = over(s.pix[i], p)
		}
	}
}

// drawPill draws the name tag into r as Mural draws a collaborator's tag
// (refs/README.md): a soft black shadow below, a white rim of
// cursorPillRim outside, and the body flat and opaque in the Agent's
// colour, its corners rounded by cursorPillRadius.
func (s *surface) drawPill(r Rect, scale float64, accent color.RGBA) {
	if r.Width() <= 0 || r.Height() <= 0 {
		return
	}
	l, t, rt, b := float64(r.Left), float64(r.Top), float64(r.Right), float64(r.Bottom)
	rad := pillRadius(r, scale)
	rim := cursorPillRim * scale
	off := cursorPillShadowOffset * scale
	blur := math.Max(cursorPillShadowBlur*scale, 0.5)
	box := pillShadowBounds(r, scale)
	s.fillSoft(box, cursorShadowColor, cursorPillShadowOpacity, func(x, y float64) float64 {
		return shadowStrength(roundRectSD(l, t+off, rt, b+off, rad, x, y)-rim, blur)
	})
	s.fillSDF(box, cursorRimColor, 1, func(x, y float64) float64 { return roundRectSD(l, t, rt, b, rad, x, y) - rim })
	s.fillSDF(r, accent, 1, func(x, y float64) float64 { return roundRectSD(l, t, rt, b, rad, x, y) })
}

// clipBox clips r to the surface.
func (s *surface) clipBox(r Rect) (x0, y0, x1, y1 int) {
	return max(int(r.Left), 0), max(int(r.Top), 0), min(int(r.Right), s.w), min(int(r.Bottom), s.h)
}

// takeText readies r for GDI's text: it returns the pixels there (the
// tag) and clears them to black, for GDI to draw white text onto.
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
// takeText saved from there, so every pixel keeps the tag's alpha.
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

// drawTrail draws the comet trail pts (surface coordinates, tail first)
// in the accent lifted toward white (effects.rs): each piece a round-capped
// stroke widening from cursorTrailWidthMin to cursorTrailWidthMax toward
// the head, at opacity cursorTrailAlpha·k². Where pieces overlap the
// strongest wins, so the joints show no beads; each piece costs only its
// own box. alpha is the scratch buffer (resized as needed), returned.
func (s *surface) drawTrail(pts []fpoint, scale float64, accent color.RGBA, alpha []float64) []float64 {
	if len(pts) < 2 {
		return alpha
	}
	if len(alpha) < len(s.pix) {
		alpha = make([]float64, len(s.pix))
	}
	alpha = alpha[:len(s.pix)]
	clear(alpha)
	n := len(pts) - 1
	for i := range n {
		a, b := pts[i], pts[i+1]
		k := float64(i+1) / float64(n)
		hw := (cursorTrailWidthMin + (cursorTrailWidthMax-cursorTrailWidthMin)*k) * scale / 2
		op := cursorTrailAlpha * k * k
		box := Rect{
			Left: int32(math.Floor(math.Min(a.X, b.X) - hw - 1)), Top: int32(math.Floor(math.Min(a.Y, b.Y) - hw - 1)),
			Right: int32(math.Ceil(math.Max(a.X, b.X) + hw + 1)), Bottom: int32(math.Ceil(math.Max(a.Y, b.Y) + hw + 1)),
		}
		x0, y0, x1, y1 := s.clipBox(box)
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				cov := clamp01(0.5 - (segmentDist(a, b, float64(x)+0.5, float64(y)+0.5) - hw))
				if v := op * cov; v > alpha[y*s.w+x] {
					alpha[y*s.w+x] = v
				}
			}
		}
	}
	c := mixColor(accent, cursorRimColor, cursorEffectLift)
	for i, v := range alpha {
		if v > 0 {
			s.pix[i] = over(s.pix[i], premultiply(c, v))
		}
	}
	return alpha
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
	// The border's shadow, a band of width bw blurred by σ, as a function
	// of the distance to the centre line: tabled every 1/32 px and read
	// linearly, so a pixel costs no error function (the pop redraws the
	// band at the frame rate).
	const perPx = 32
	glow := make([]float64, int((hw+reach)*perPx)+2)
	for i := range glow {
		d := float64(i) / perPx
		glow[i] = cursorHighlightGlow * (gaussCDF((d+hw)/sigma) - gaussCDF((d-hw)/sigma))
	}
	px = func(x, y float64) uint32 {
		var p uint32
		d := math.Abs(roundRectSD(cl, ct, cr, cb, crad, x, y)) // to the centre line
		if d < hw+reach {
			f := d * perPx
			i := int(f)
			g := glow[i] + (glow[i+1]-glow[i])*(f-float64(i))
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
