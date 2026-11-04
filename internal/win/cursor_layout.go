package win

// The Agent Cursor's layout (headless ticket 03, cursor-look): the
// arrow's shape, the name tag's size and placement beside it, name
// ellipsizing, and the rectangles of the layered windows. Pure functions
// of physical pixels and a DPI scale (dpi/96), tested offline.
//
// The arrow and the tag are drawn after Mural's presence cursor
// (cursor-look issue 11). The trail's widths are trycua/cua's at ecaf10b
// (MIT; see third_party/cua/LICENSE; effects.rs).
//
// The rest is ported from vercel-labs/agent-desktop at HEAD 9eb9b29
// (Apache-2.0; see third_party/agent-desktop/LICENSE), modified:
//   - crates/windows/src/system/cursor_overlay/geometry.rs: the
//     highlight's border, corner radius and padding, the label shadow's
//     extent (label_shadow_rect);
//   - crates/core/src/cursor_overlay/label_pill.rs: the pill's maximum
//     width;
//   - crates/core/src/cursor_overlay/layout.rs (place_label): the pill
//     beside the tip, below and to the right, flipping left and up when it
//     would leave the monitor's work area, then clamped into it;
//   - crates/macos/src/system/cursor_overlay/cursor_overlay_chrome_bridge.m:
//     the highlight's tint and glow, and the extent of its pop
//     (ADHighlightShow, 1.08 at most).
// Every length is in logical pixels, scaled by our dpi/96 (their style
// size, 1 by default, is not carried over).

import (
	"math"
	"strings"
	"unicode"
)

// The arrow is Mural's presence cursor, measured off its board (screen
// pixels ÷ 1.33; cursor-look issue 11, .scratch/cursor-look/refs/): a dart
// with its tip up-left at the hotspot, two wings, and a back curving in
// toward the tip, every corner rounded. Logical pixels off the tip.
var (
	cursorArrowWingTop    = fpoint{X: 19, Y: 6.5}
	cursorArrowWingBottom = fpoint{X: 6.5, Y: 18}
)

// The arrow's look, logical pixels: a white rim outside the shape, a soft
// black shadow below it, the body flat in the Agent's colour.
const (
	// cursorArrowNotch is how far the back curves in toward the tip at its
	// middle (0 is a plain rounded triangle).
	cursorArrowNotch       = 3.5
	cursorArrowTipRadius   = 1.3
	cursorArrowWingRadius  = 2.4
	cursorArrowRim         = 1.0
	cursorArrowShadowOp    = 0.16
	cursorArrowShadowOff   = 1.0 // down
	cursorArrowShadowBlur  = 1.6
	cursorArrowCornerSteps = 6 // line pieces per rounded corner; the back takes twice
	// cursorPressMax is the largest the click's press grows the arrow.
	cursorPressMax = 1.03
)

// arrowPoints returns the arrow's outline at scale, relative to the tip,
// a closed polygon starting beside the tip: each corner a quadratic curve
// through it (its control point), the back one whose middle lies
// cursorArrowNotch in from the line between the wings.
func arrowPoints(scale float64) []fpoint {
	toward := func(v, p fpoint, r float64) fpoint {
		l := math.Hypot(p.X-v.X, p.Y-v.Y)
		return fpoint{X: v.X + (p.X-v.X)/l*r, Y: v.Y + (p.Y-v.Y)/l*r}
	}
	var pts []fpoint
	quad := func(p0, c, p2 fpoint, n int) {
		for j := 0; j <= n; j++ {
			s := float64(j) / float64(n)
			u := 1 - s
			pts = append(pts, fpoint{
				X: (u*u*p0.X + 2*u*s*c.X + s*s*p2.X) * scale,
				Y: (u*u*p0.Y + 2*u*s*c.Y + s*s*p2.Y) * scale,
			})
		}
	}
	t, a, b := fpoint{}, cursorArrowWingTop, cursorArrowWingBottom
	mid := fpoint{X: (a.X + b.X) / 2, Y: (a.Y + b.Y) / 2}
	c := toward(mid, t, 2*cursorArrowNotch) // a quadratic's middle is halfway to its control
	const rt, rw, n = cursorArrowTipRadius, cursorArrowWingRadius, cursorArrowCornerSteps
	quad(toward(t, b, rt), t, toward(t, a, rt), n)
	quad(toward(a, t, rw), a, toward(a, c, rw), n)
	quad(toward(a, c, rw), c, toward(b, c, rw), 2*n)
	quad(toward(b, c, rw), b, toward(b, t, rw), n)
	return pts
}

// arrowOutset is how far the arrow's pixels reach outside its outline at
// scale: the rim, and the shadow below it, offset and blurred.
func arrowOutset(scale float64) float64 {
	return (cursorArrowRim + cursorArrowShadowOff + cursorArrowShadowBlur) * scale
}

// arrowReach is the radius about the tip that the arrow's pixels can
// touch at scale, turned any way and pressed to its largest: the farthest
// point of the outline with its rim and shadow, plus a pixel.
func arrowReach(scale float64) int32 {
	far := 0.0
	for _, p := range arrowPoints(scale) {
		far = math.Max(far, math.Hypot(p.X, p.Y))
	}
	return int32(math.Ceil((far+arrowOutset(scale))*cursorPressMax)) + 1
}

// arrowBounds is the box (relative to the tip) the arrow's pixels can
// touch at scale: a square about the tip, so the arrow may turn freely.
func arrowBounds(scale float64) Rect {
	r := arrowReach(scale)
	return Rect{Left: -r, Top: -r, Right: r + 1, Bottom: r + 1}
}

// The name tag (the label pill), logical pixels: Mural's presence tag
// (cursor-look issue 11): the Agent's name alone, flat in its colour with
// white or near-black text (cursor_raster.go), a white rim and the
// arrow's soft shadow, its top-left nestled at the arrow's back. The
// layout's placement started from agent-desktop's label_pill.rs.
const (
	cursorPillHeight = 22.0
	cursorPillRadius = 5.5
	cursorPillPadX   = 8.0   // each end
	cursorPillFontPx = 12.5  // Figtree Bold (cursorLabelFaces)
	cursorPillRim    = 1.0   // white, outside the tag
	cursorPillMaxW   = 360.0 // the widest a tag gets; a capped name is far narrower
	// cursorNameMaxRunes caps a name before it is measured: longer ones
	// end in "…" within this many characters.
	cursorNameMaxRunes = 24
	// The tag's shadow is the arrow's: black at this opacity, offset down
	// and blurred, measured from the rim.
	cursorPillShadowOpacity = cursorArrowShadowOp
	cursorPillShadowOffset  = cursorArrowShadowOff
	cursorPillShadowBlur    = cursorArrowShadowBlur
	// The tag's top-left sits this far right of and below the tip, over
	// the arrow's back as Mural's does, and the tag as far left of and
	// above the tip when flipped (layout.rs LABEL_OFFSET_X/Y).
	cursorPillOffsetX = 12.5
	cursorPillOffsetY = 12.0

	cursorLabelMaxRunes = 200 // names are cut before measuring
)

// The target's highlight, logical pixels (geometry.rs; the tint and the
// glow from the macOS chrome bridge's ADHighlightShow).
const (
	cursorHighlightBorder   = 2.5  // HIGHLIGHT_BORDER_WIDTH
	cursorHighlightRadius   = 8.0  // HIGHLIGHT_CORNER_RADIUS
	cursorHighlightPad      = 5.0  // HIGHLIGHT_PADDING: the outline sits outside the element
	cursorHighlightTint     = 0.10 // the accent washed over the element
	cursorHighlightGlow     = 0.5  // the glow's opacity (shadowOpacity) …
	cursorHighlightGlowBlur = 10.0 // … and its blur (shadowRadius)
)

// cursorScale converts a monitor DPI to the drawing scale (96 → 1).
func cursorScale(dpi uint32) float64 {
	if dpi == 0 {
		return 1
	}
	return float64(dpi) / 96
}

// scaled rounds v·scale to whole pixels, never below 1 for a positive v.
func scaled(v, scale float64) int32 {
	r := int32(math.Round(v * scale))
	if r < 1 && v > 0 {
		r = 1
	}
	return r
}

// sanitizeLabel makes a label one line: control characters and runs of
// whitespace become single spaces, the ends are trimmed, and anything
// past cursorLabelMaxRunes is cut (the pill ellipsizes long labels
// anyway; this only bounds the measuring work).
func sanitizeLabel(s string) string {
	var b strings.Builder
	space := false
	n := 0
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			space = b.Len() > 0
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
		if n++; n >= cursorLabelMaxRunes {
			break
		}
	}
	return b.String()
}

// ellipsize returns text cut to fit maxW as measured by measure, ending
// in "…" when it was cut: the longest prefix that fits with the ellipsis
// (binary search; measure is monotonic in the prefix length). Text that
// fits is returned unchanged; when not even "…" fits, the result is "".
func ellipsize(text string, maxW int32, measure func(string) int32) string {
	if text == "" || measure(text) <= maxW {
		return text
	}
	const dots = "…"
	runes := []rune(text)
	lo, hi := 0, len(runes) // the answer is in [lo, hi)
	fits := func(n int) bool {
		return measure(strings.TrimRight(string(runes[:n]), " ")+dots) <= maxW
	}
	if !fits(0) {
		return ""
	}
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if fits(mid) {
			lo = mid
		} else {
			hi = mid
		}
	}
	return strings.TrimRight(string(runes[:lo]), " ") + dots
}

// pillTextMax is the widest the name may measure at scale.
func pillTextMax(scale float64) int32 {
	return scaled(cursorPillMaxW, scale) - 2*scaled(cursorPillPadX, scale)
}

// pillSize is the tag's size for a name measuring textW at scale: the
// text plus padding, capped at the maximum width (LabelPill::for_label).
func pillSize(textW int32, scale float64) (w, h int32) {
	textW = min(max(textW, 0), pillTextMax(scale))
	return textW + 2*scaled(cursorPillPadX, scale), scaled(cursorPillHeight, scale)
}

// pillRadius is the tag's corner radius at scale.
func pillRadius(pill Rect, scale float64) float64 {
	return math.Min(float64(pill.Height())/2, cursorPillRadius*scale)
}

// capName cuts a name longer than cursorNameMaxRunes to that many
// characters, the last an ellipsis.
func capName(name string) string {
	r := []rune(name)
	if len(r) <= cursorNameMaxRunes {
		return name
	}
	return strings.TrimRight(string(r[:cursorNameMaxRunes-1]), " ") + "…"
}

// placePill places a w×h pill beside the tip, in screen pixels
// (place_label): below and right of the arrow by default, flipped to the
// tip's left when it would cross the work area's right edge and above the
// tip when it would cross the bottom, then clamped into the work area (a
// pill wider than the work area keeps its left edge on the area's left).
func placePill(tip fpoint, w, h int32, work Rect, scale float64) Rect {
	tx, ty := roundPoint(tip)
	ox, oy := scaled(cursorPillOffsetX, scale), scaled(cursorPillOffsetY, scale)
	x := tx + ox
	if x+w > work.Right {
		x = tx - w - ox
	}
	y := ty + oy
	if y+h > work.Bottom {
		y = ty - h - oy
	}
	x = max(min(x, work.Right-w), work.Left)
	y = max(min(y, work.Bottom-h), work.Top)
	return Rect{Left: x, Top: y, Right: x + w, Bottom: y + h}
}

// pillShadowBounds is the box a tag's rim and shadow can touch
// (geometry.rs label_shadow_rect): the tag grown by its rim, moved down by
// the shadow's offset and grown by its blur, plus a pixel.
func pillShadowBounds(pill Rect, scale float64) Rect {
	g := int32(math.Ceil((cursorPillRim+cursorPillShadowBlur)*scale)) + 1
	off := int32(math.Ceil(cursorPillShadowOffset * scale))
	return Rect{Left: pill.Left - g, Top: pill.Top - g, Right: pill.Right + g, Bottom: pill.Bottom + g + off}
}

// cursorLook is everything the cursor window's pixels depend on; the
// window is re-rendered only when it changes, and merely moved while the
// cursor travels.
type cursorLook struct {
	Scale float64
	Name  string // the Agent's name, sanitized and ellipsized; "" draws no tag
	Pill  Rect   // the tag, relative to the (rounded) tip; empty without a name
}

// cursorWindowLayout is the cursor window's geometry for a look, in the
// window's own pixels: its size, where the tip sits in it, the tag and
// the rectangle its name is drawn into.
type cursorWindowLayout struct {
	W, H int32
	Tip  fpoint // the tip, window-relative
	Pill Rect   // window-relative; empty without a name
	Text Rect   // window-relative; inside the tag, padded at its ends
	// Origin is the window's top-left relative to the rounded tip.
	OriginX, OriginY int32
}

// layoutCursorWindow computes the window geometry for look: the union of
// the arrow's box and the tag with its shadow, so the window is only as
// large as what it shows.
func layoutCursorWindow(look cursorLook) cursorWindowLayout {
	b := arrowBounds(look.Scale)
	hasPill := look.Name != "" && look.Pill.Width() > 0 && look.Pill.Height() > 0
	if hasPill {
		b = unionRect(b, pillShadowBounds(look.Pill, look.Scale))
	}
	l := cursorWindowLayout{
		W: b.Width(), H: b.Height(),
		Tip:     fpoint{X: float64(-b.Left), Y: float64(-b.Top)},
		OriginX: b.Left, OriginY: b.Top,
	}
	if hasPill {
		l.Pill = Rect{
			Left: look.Pill.Left - b.Left, Top: look.Pill.Top - b.Top,
			Right: look.Pill.Right - b.Left, Bottom: look.Pill.Bottom - b.Top,
		}
		pad := scaled(cursorPillPadX, look.Scale)
		l.Text = Rect{Left: l.Pill.Left + pad, Top: l.Pill.Top, Right: l.Pill.Right - pad, Bottom: l.Pill.Bottom}
	}
	return l
}

// trailWindowRect is the trail window's screen rectangle for the trail
// pts at scale: their bounds grown by half the widest stroke and a pixel.
// Empty without a trail.
func trailWindowRect(pts []fpoint, scale float64) Rect {
	if len(pts) < 2 {
		return Rect{}
	}
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, p := range pts {
		minX, minY = math.Min(minX, p.X), math.Min(minY, p.Y)
		maxX, maxY = math.Max(maxX, p.X), math.Max(maxY, p.Y)
	}
	g := cursorTrailWidthMax/2*scale + 1
	return Rect{
		Left: int32(math.Floor(minX - g)), Top: int32(math.Floor(minY - g)),
		Right: int32(math.Ceil(maxX + g)), Bottom: int32(math.Ceil(maxY + g)),
	}
}

// cursorLabelFaces are the tag's fonts, best first, all bold: Figtree,
// the closest free match to Mural's, embedded in winuse.exe and loaded
// into the process only (cursor_font.go; third_party/figtree), then
// Windows' own Segoe UI should that fail.
var cursorLabelFaces = []string{"Figtree", "Segoe UI"}

// chooseLabelFace returns the first face resolve (which creates the font
// and reports the face GDI actually selected, "" when it could not)
// really provides, and the last face when none does — GDI then
// substitutes something close.
func chooseLabelFace(faces []string, resolve func(face string) string) string {
	for _, f := range faces {
		if strings.EqualFold(resolve(f), f) {
			return f
		}
	}
	return faces[len(faces)-1]
}

// scaleAbout scales the coordinate v by s about a.
func scaleAbout(v, a, s float64) float64 { return a + (v-a)*s }

// outlineGlowReach is how far the highlight's glow reaches beyond the
// stroke's edge at scale: three standard deviations of its blur (a
// CoreAnimation shadowRadius r is a Gaussian of σ ≈ r/2).
func outlineGlowReach(scale float64) float64 {
	return 3 * cursorHighlightGlowBlur / 2 * scale
}

// cursorHighlightPopMax is the largest the highlight grows in its pop.
const cursorHighlightPopMax = 1.08

// outlineReach is how far the highlight's pixels reach beyond target on
// each side, horizontally and vertically, at its largest pop: the padded
// box scaled about the element's centre, plus the scaled glow.
func outlineReach(target Rect, scale float64) (gx, gy int32) {
	pad := cursorHighlightPad * scale
	grow := func(half float64) int32 {
		return int32(math.Ceil((half+pad)*cursorHighlightPopMax-half+outlineGlowReach(scale)*cursorHighlightPopMax)) + 2
	}
	return grow(float64(target.Width()) / 2), grow(float64(target.Height()) / 2)
}

// outlineWindowRect is the outline window's screen rectangle for target:
// the bounds grown by the padding, the glow and the pop, clipped to clip
// (the virtual screen) so an element partly off-screen never makes a
// window larger than the desktop. Empty when nothing of it is on screen.
func outlineWindowRect(target, clip Rect, scale float64) Rect {
	gx, gy := outlineReach(target, scale)
	r := Rect{Left: target.Left - gx, Top: target.Top - gy, Right: target.Right + gx, Bottom: target.Bottom + gy}
	r = Rect{
		Left: max(r.Left, clip.Left), Top: max(r.Top, clip.Top),
		Right: min(r.Right, clip.Right), Bottom: min(r.Bottom, clip.Bottom),
	}
	if r.Width() <= 0 || r.Height() <= 0 {
		return Rect{}
	}
	return r
}
