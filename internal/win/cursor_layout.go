package win

// The Agent Cursor's layout (headless ticket 03): the dart's shape, the
// label pill's size and placement beside it, label ellipsizing, and the
// rectangles of the three layered windows. Pure functions of physical
// pixels and a DPI scale (dpi/96), tested offline.
//
// Ported from vercel-labs/agent-desktop at HEAD 9eb9b29 (Apache-2.0; see
// third_party/agent-desktop/LICENSE), modified:
//   - crates/windows/src/system/cursor_overlay/geometry.rs: the dart, the
//     glyph box, the rim, the ripple's size, core and ring inset, the
//     highlight's border, corner radius and padding, the label shadow's
//     extent (label_shadow_rect);
//   - crates/windows/src/system/cursor_overlay/render.rs: the text's
//     vertical inset (TEXT_VERTICAL_INSET);
//   - crates/core/src/cursor_overlay/label_pill.rs: the pill's font size,
//     height, padding, maximum width and shadow;
//   - crates/core/src/cursor_overlay/layout.rs (place_label): the pill
//     beside the tip, below and to the right, flipping left and up when it
//     would leave the monitor's work area, then clamped into it;
//   - crates/macos/src/system/cursor_overlay/cursor_overlay_bridge.m: the
//     dart's fill and rim stroke widths and its drop shadow, and the
//     label bubble's margin (ADBubbleMargin), which its pop grows from;
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

// cursorDart is agent-desktop's four-point dart in its 32×40 glyph box,
// top-down, the tip first (geometry.rs DART).
var cursorDart = [...]fpoint{{1.0, 5.0}, {29.6, 22.5}, {12.7, 23.7}, {4.2, 38.4}}

// The dart's look, logical pixels. macOS strokes the dart's path twice
// with round joins: the rim at 6.5 wide under the fill at 3 wide, so the
// fill reaches 1.5 and the rim 3.25 beyond the path, with a soft shadow
// below and to the right of both.
const (
	cursorDartFillGrow      = 1.5  // the fill layer's lineWidth 3, halved
	cursorDartRimGrow       = 3.25 // geometry.rs GLYPH_RIM_WIDTH 6.5, halved
	cursorDartShadowOpacity = 0.34
	cursorDartShadowX       = 2.5 // the shadow's offset: right …
	cursorDartShadowY       = 3.0 // … and down
	cursorDartShadowBlur    = 5.0
)

// The label pill, logical pixels (label_pill.rs): black, fully rounded,
// white text centred in it (colours in cursor_raster.go).
const (
	cursorPillHeight    = 24.0  // LABEL_PILL_HEIGHT; the radius is half of it
	cursorPillPadX      = 10.0  // LABEL_PILL_PADDING, each end
	cursorPillMaxWidth  = 360.0 // LABEL_PILL_MAX_WIDTH; longer labels are ellipsized
	cursorPillFontPx    = 12.5  // LABEL_FONT_POINTS, semibold as in text.rs (cursorLabelFaces)
	cursorPillTextInset = 2.0   // render.rs TEXT_VERTICAL_INSET
	// The pill's shadow: black at this opacity, offset down and blurred
	// (LABEL_SHADOW_OPACITY, _OFFSET, _BLUR).
	cursorPillShadowOpacity = 0.31
	cursorPillShadowOffset  = 2.0
	cursorPillShadowBlur    = 2.5
	// The pill sits this far right of and below the tip, and as far left
	// of and above it when flipped (layout.rs LABEL_OFFSET_X/Y).
	cursorPillOffsetX = 20.0
	cursorPillOffsetY = 30.0
	// cursorPillMargin is the macOS bubble window's margin around the pill
	// (ADBubbleMargin): its pop grows from that window's corner.
	cursorPillMargin = 8.0

	cursorLabelMaxRunes = 200 // labels are cut before measuring
)

// The click ripple, logical pixels (geometry.rs, raster.rs draw_ripple).
const (
	cursorRippleSize       = 108.0 // RIPPLE_SIZE: the ripple's full extent
	cursorRippleCoreRadius = 19.0  // RIPPLE_CORE_RADIUS: the solid centre disc
	cursorRippleRingInset  = 4.0   // RIPPLE_RING_INSET: the ring's largest radius is 108/2 − 4
	cursorRippleRingWidth  = 3.0
	cursorRippleCoreShrink = 0.4 // the disc shrinks by this much as the ring spreads
	cursorRippleCoreAlpha  = 0.55
	cursorRippleRingAlpha  = 0.85
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

// dartPoints returns the dart's outline at scale, relative to the tip.
func dartPoints(scale float64) []fpoint {
	tip := cursorDart[0]
	pts := make([]fpoint, len(cursorDart))
	for i, p := range cursorDart {
		pts[i] = fpoint{(p.X - tip.X) * scale, (p.Y - tip.Y) * scale}
	}
	return pts
}

// dartBounds is the box (relative to the tip) the dart's pixels can touch
// at scale: the outline's bounds grown by the rim, the shadow's offset
// and blur, and a pixel of antialiasing.
func dartBounds(scale float64) Rect {
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, p := range dartPoints(scale) {
		minX, minY = math.Min(minX, p.X), math.Min(minY, p.Y)
		maxX, maxY = math.Max(maxX, p.X), math.Max(maxY, p.Y)
	}
	rim := cursorDartRimGrow * scale
	shadow := rim + (cursorDartShadowBlur+math.Max(cursorDartShadowX, cursorDartShadowY))*scale
	return Rect{
		Left:   int32(math.Floor(minX - rim - 1)),
		Top:    int32(math.Floor(minY - rim - 1)),
		Right:  int32(math.Ceil(maxX + shadow + 1)),
		Bottom: int32(math.Ceil(maxY + shadow + 1)),
	}
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

// pillTextMax is the widest the label's text may measure at scale.
func pillTextMax(scale float64) int32 {
	return scaled(cursorPillMaxWidth, scale) - 2*scaled(cursorPillPadX, scale)
}

// pillSize is the pill's size for text measuring textW at scale: the text
// plus padding, capped at the maximum width (LabelPill::for_label).
func pillSize(textW int32, scale float64) (w, h int32) {
	textW = min(max(textW, 0), pillTextMax(scale))
	return textW + 2*scaled(cursorPillPadX, scale), scaled(cursorPillHeight, scale)
}

// placePill places a w×h pill beside the tip, in screen pixels
// (place_label): below and right of the dart by default, flipped to the
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

// pillShadowBounds is the box a pill's shadow can touch (geometry.rs
// label_shadow_rect): the pill moved down by the shadow's offset and
// grown by twice its blur, plus a pixel.
func pillShadowBounds(pill Rect, scale float64) Rect {
	g := int32(math.Ceil(2*cursorPillShadowBlur*scale)) + 1
	off := int32(math.Ceil(cursorPillShadowOffset * scale))
	return Rect{Left: pill.Left - g, Top: pill.Top - g + off, Right: pill.Right + g, Bottom: pill.Bottom + g + off}
}

// cursorLook is everything the cursor window's pixels depend on; the
// window is re-rendered only when it changes, and merely moved while the
// cursor travels.
type cursorLook struct {
	Scale float64
	Label string // already sanitized and ellipsized; "" draws no pill
	Pill  Rect   // relative to the (rounded) tip; empty without a label
}

// cursorWindowLayout is the cursor window's geometry for a look, in the
// window's own pixels: its size, where the tip sits in it, the pill and
// the rectangle the label text is drawn into.
type cursorWindowLayout struct {
	W, H int32
	Tip  fpoint // the tip, window-relative
	Pill Rect   // window-relative; empty without a label
	Text Rect   // window-relative; inside the pill, padded
	// Origin is the window's top-left relative to the rounded tip.
	OriginX, OriginY int32
}

// layoutCursorWindow computes the window geometry for look: the union of
// the dart's box and the pill with its shadow, so the window is only as
// large as what it shows.
func layoutCursorWindow(look cursorLook) cursorWindowLayout {
	b := dartBounds(look.Scale)
	hasPill := look.Label != "" && look.Pill.Width() > 0 && look.Pill.Height() > 0
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
		inset := scaled(cursorPillTextInset, look.Scale)
		l.Text = Rect{Left: l.Pill.Left + pad, Top: l.Pill.Top + inset, Right: l.Pill.Right - pad, Bottom: l.Pill.Bottom - inset}
	}
	return l
}

// rippleSpread is how far the ripple has spread at phase ∈ [0, 1]:
// linearly, as their Windows port draws it (raster.rs draw_ripple; the
// phase is motion.rs's linear ripple progress).
func rippleSpread(phase float64) float64 { return clamp01(phase) }

// rippleRingRadius is the ring's outer radius at phase: from nothing to
// the ripple's half extent less the ring inset.
func rippleRingRadius(phase, scale float64) float64 {
	return (cursorRippleSize/2 - cursorRippleRingInset) * rippleSpread(phase) * scale
}

// rippleCoreRadius is the centre disc's radius at phase: shrinking a
// little as the ring spreads.
func rippleCoreRadius(phase, scale float64) float64 {
	return cursorRippleCoreRadius * (1 - cursorRippleCoreShrink*rippleSpread(phase)) * scale
}

// rippleWindowSize is the ripple window's side: the ripple's full extent
// plus antialiasing. The window is centred on the click.
func rippleWindowSize(scale float64) int32 {
	return 2*int32(math.Ceil(cursorRippleSize/2*scale+1)) + 1
}

// cursorLabelFaces are the label's fonts, best first, all semibold as
// their Windows port sets the label (text.rs FW_SEMIBOLD): the Windows 11
// system font, closest to agent-desktop's macOS SF, then Segoe UI's for
// Windows 10. GDI names each weight of these families as a family of its
// own, so a true semibold is asked for by name: their "Segoe UI" at
// FW_SEMIBOLD gets its Bold.
var cursorLabelFaces = []string{"Segoe UI Variable Text Semibold", "Segoe UI Semibold"}

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

// popAnchor is the point the label pill's pop grows from at scale: the
// bottom-left corner of the macOS bubble window, which sits
// cursorPillMargin outside the pill. ADShowBubble scales that window's
// content layer, a view's backing layer, whose anchor point AppKit keeps
// at (0, 0) — the bottom left in AppKit's upward y.
func popAnchor(pill Rect, scale float64) fpoint {
	m := cursorPillMargin * scale
	return fpoint{X: float64(pill.Left) - m, Y: float64(pill.Bottom) + m}
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
