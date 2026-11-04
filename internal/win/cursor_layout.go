package win

// The Agent Cursor's layout (headless ticket 03, cursor-look): the
// arrow's shape, the label pill's size and placement beside it, label
// ellipsizing, and the rectangles of the layered windows. Pure functions
// of physical pixels and a DPI scale (dpi/96), tested offline.
//
// The arrow is trycua/cua's default arrow at ecaf10b (MIT; see
// third_party/cua/LICENSE): its outline, rim, glow and halo
// (libs/cua-driver/rust/crates/cursor-overlay assets/build_default_theme.py,
// theme.rs, cursor_pose.rs), and the trail's widths (effects.rs).
//
// The rest is ported from vercel-labs/agent-desktop at HEAD 9eb9b29
// (Apache-2.0; see third_party/agent-desktop/LICENSE), modified:
//   - crates/windows/src/system/cursor_overlay/geometry.rs: the
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

// cursorArrowOutline is cua's default arrow (build_default_theme.py
// DART_OUTLINE, already turned upright by UPRIGHT_DEGREES about the
// hotspot): eight cubic beziers on its 128-unit canvas, each the three
// points after the one before, starting and ending at the hotspot. A
// classic pointer with rounded corners, its left edge vertical.
var cursorArrowOutline = [...]fpoint{
	{55.0, 30.0},
	{49.11, 25.73}, {41.76, 28.37}, {39.96, 36.23},
	{39.96, 36.23}, {40.20, 96.98}, {40.20, 96.98},
	{40.28, 105.52}, {45.92, 107.57}, {52.07, 102.36},
	{52.07, 102.36}, {67.37, 86.65}, {67.37, 86.65},
	{70.62, 83.58}, {74.47, 81.78}, {78.91, 81.27},
	{78.91, 81.27}, {93.52, 79.14}, {93.52, 79.14},
	{101.47, 77.78}, {102.57, 71.79}, {97.02, 66.58},
	{97.02, 66.58}, {55.00, 30.00}, {55.00, 30.00},
}

// The arrow's look (theme.rs, cursor_pose.rs, build_default_theme.py).
// The canvas is painted as a 42-logical-pixel footprint; everything else
// is in logical pixels.
const (
	cursorArrowUnit   = 42.0 / 128 // logical pixels per canvas unit (DISPLAY_SIZE / CANVAS_SIZE)
	cursorArrowFlat   = 12         // line pieces per bezier when flattened
	cursorArrowRim    = 2.5 * cursorArrowUnit
	cursorArrowHaloX  = 2.0  // the halo's centre from the tip …
	cursorArrowHaloY  = 11.9 // … on the body (HALO_CENTER)
	cursorArrowHaloR  = 14.0 // HALO_RADIUS_PT: 28 across
	cursorArrowHaloOp = 0.11 // its peak opacity, at the centre
	// cursorPressMax is the largest the click's press grows the arrow.
	cursorPressMax = 1.03
	// cursorArrowNarrow and cursorArrowTall reshape cua's outline about
	// its tip: narrower and taller, so its tip (about 43° instead of 56°;
	// the system arrow's is 45°) is its sharpest corner. cua's upright
	// dart is nearly equilateral: at rest it read as a "▶" pointing right,
	// past its own tip, and turned a little another corner read as the
	// point (ADR 0018). The left edge stays vertical; the area stays about
	// the same.
	cursorArrowNarrow = 0.82
	cursorArrowTall   = 1.18
)

// arrowCanvas maps a canvas point to logical pixels off the tip, reshaped.
func arrowCanvas(p fpoint) fpoint {
	return fpoint{
		X: (p.X - cursorArrowHotspot.X) * cursorArrowUnit * cursorArrowNarrow,
		Y: (p.Y - cursorArrowHotspot.Y) * cursorArrowUnit * cursorArrowTall,
	}
}

// arrowWings are the rounded corners of the arrow's two wings on the
// canvas: the bottom-left and the right one.
var arrowWings = [2]fpoint{{43.5, 103.5}, {99.5, 72}}

// arrowHalo is the halo's centre off the tip, logical pixels: cua's body
// centre (cursor_pose.rs), reshaped with the outline.
var arrowHalo = fpoint{X: cursorArrowHaloX * cursorArrowNarrow, Y: cursorArrowHaloY * cursorArrowTall}

// cursorArrowHotspot is the tip on the canvas: the point the arrow names.
var cursorArrowHotspot = fpoint{55, 30}

// cursorArrowGlow is the arrow's coloured glow: the outline stroked in the
// fill colour at these widths (canvas units) and opacities, widest and
// faintest first, under the body (build_default_theme.py). There is no
// dark drop shadow: the white rim and the glow read on light and dark.
var cursorArrowGlow = []struct{ width, opacity float64 }{
	{44, 0.020}, {36, 0.024}, {29, 0.030}, {23, 0.038},
	{18, 0.048}, {14, 0.060}, {10, 0.075}, {7, 0.095},
}

// arrowPoints returns the arrow's outline at scale, relative to the tip:
// each bezier flattened into cursorArrowFlat pieces, a closed polygon.
func arrowPoints(scale float64) []fpoint {
	o := cursorArrowOutline
	at := func(p fpoint) fpoint {
		q := arrowCanvas(p)
		return fpoint{q.X * scale, q.Y * scale}
	}
	pts := make([]fpoint, 0, (len(o)-1)/3*cursorArrowFlat)
	for i := 0; i+3 < len(o); i += 3 {
		a, b, c, d := at(o[i]), at(o[i+1]), at(o[i+2]), at(o[i+3])
		for j := range cursorArrowFlat {
			s := float64(j) / cursorArrowFlat
			v := 1 - s
			w0, w1, w2, w3 := v*v*v, 3*v*v*s, 3*v*s*s, s*s*s
			pts = append(pts, fpoint{
				X: w0*a.X + w1*b.X + w2*c.X + w3*d.X,
				Y: w0*a.Y + w1*b.Y + w2*c.Y + w3*d.Y,
			})
		}
	}
	return pts
}

// arrowGlowReach is how far the glow reaches outside the outline at scale.
func arrowGlowReach(scale float64) float64 {
	return cursorArrowGlow[0].width / 2 * cursorArrowUnit * scale
}

// arrowReach is the radius about the tip that the arrow's pixels can
// touch at scale, turned any way and pressed to its largest: the farthest
// of the outline with its glow and of the halo, plus a pixel.
func arrowReach(scale float64) int32 {
	far := 0.0
	for _, p := range arrowPoints(scale) {
		far = math.Max(far, math.Hypot(p.X, p.Y))
	}
	far += arrowGlowReach(scale)
	halo := (math.Hypot(arrowHalo.X, arrowHalo.Y) + cursorArrowHaloR) * scale
	return int32(math.Ceil(math.Max(far, halo)*cursorPressMax)) + 1
}

// arrowBounds is the box (relative to the tip) the arrow's pixels can
// touch at scale: a square about the tip, so the arrow may turn freely.
func arrowBounds(scale float64) Rect {
	r := arrowReach(scale)
	return Rect{Left: -r, Top: -r, Right: r + 1, Bottom: r + 1}
}

// The label pill, logical pixels: fully rounded, white text centred in
// it, in the Agent's colour (cua's verbose badge; colours in
// cursor_raster.go). The sizes started from agent-desktop's
// label_pill.rs.
const (
	cursorPillHeight    = 26.0  // the radius is half of it
	cursorPillPadX      = 11.0  // each end
	cursorPillMaxWidth  = 360.0 // LABEL_PILL_MAX_WIDTH; longer labels are ellipsized
	cursorPillFontPx    = 12.5  // semibold (cursorLabelFaces)
	cursorPillTextInset = 2.0   // render.rs TEXT_VERTICAL_INSET
	// A named Agent's pill has two lines (ADR 0018 Decision 8): its name,
	// smaller and in its colour, over the action. The lines' boxes stack
	// between the vertical padding; the corners keep the one-line pill's
	// radius, half cursorPillHeight.
	cursorPillNameFontPx = 10.5
	cursorPillNameLine   = 14.0
	cursorPillLine       = 17.0
	cursorPillPadY       = 4.5
	// The pill's shadow: black at this opacity, offset down and blurred,
	// under a glow in the Agent's colour (session_badge.rs).
	cursorPillShadowOpacity = 0.22
	cursorPillShadowOffset  = 2.0
	cursorPillShadowBlur    = 3.0
	cursorPillGlowOpacity   = 0.30
	// The pill sits this far right of and below the tip, and as far left
	// of and above it when flipped (layout.rs LABEL_OFFSET_X/Y).
	cursorPillOffsetX = 20.0
	cursorPillOffsetY = 30.0
	// cursorPillMargin is the macOS bubble window's margin around the pill
	// (ADBubbleMargin): its pop grows from that window's corner.
	cursorPillMargin = 8.0

	cursorLabelMaxRunes = 200 // labels are cut before measuring
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

// pillSize2 is a named Agent's two-line pill's size for a name measuring
// nameW and an action measuring textW at scale: the wider line plus
// padding, capped like one line, and both lines' height.
func pillSize2(nameW, textW int32, scale float64) (w, h int32) {
	w, _ = pillSize(max(nameW, textW), scale)
	return w, pillHeight2(scale)
}

// pillHeight2 is the two-line pill's height at scale.
func pillHeight2(scale float64) int32 {
	return 2*scaled(cursorPillPadY, scale) + scaled(cursorPillNameLine, scale) + scaled(cursorPillLine, scale)
}

// pillRadius is the pill's corner radius at scale: fully rounded for one
// line, the same radius for two.
func pillRadius(pill Rect, scale float64) float64 {
	return math.Min(float64(pill.Height())/2, float64(scaled(cursorPillHeight, scale))/2)
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

// pillShadowBounds is the box a pill's shadow and glow can touch
// (geometry.rs label_shadow_rect): the pill moved down by the shadow's
// offset and grown by twice its blur, plus a pixel.
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
	Name  string // the Agent's name, the pill's first line; "" for one line
	Label string // already sanitized and ellipsized; "" draws no pill
	Pill  Rect   // relative to the (rounded) tip; empty without a label
}

// cursorWindowLayout is the cursor window's geometry for a look, in the
// window's own pixels: its size, where the tip sits in it, the pill and
// the rectangles the name and the label text are drawn into.
type cursorWindowLayout struct {
	W, H int32
	Tip  fpoint // the tip, window-relative
	Pill Rect   // window-relative; empty without a label
	Text Rect   // window-relative; inside the pill, padded
	Name Rect   // window-relative, above Text; empty without a name
	// Origin is the window's top-left relative to the rounded tip.
	OriginX, OriginY int32
}

// layoutCursorWindow computes the window geometry for look: the union of
// the arrow's box and the pill with its shadow, so the window is only as
// large as what it shows.
func layoutCursorWindow(look cursorLook) cursorWindowLayout {
	b := arrowBounds(look.Scale)
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
		if look.Name != "" {
			top := l.Pill.Top + scaled(cursorPillPadY, look.Scale)
			mid := top + scaled(cursorPillNameLine, look.Scale)
			l.Name = Rect{Left: l.Text.Left, Top: top, Right: l.Text.Right, Bottom: mid}
			l.Text.Top, l.Text.Bottom = mid, mid+scaled(cursorPillLine, look.Scale)
		}
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
