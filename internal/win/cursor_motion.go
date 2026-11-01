package win

// The Agent Cursor's motion and timeline (headless ticket 03): pure
// functions of time and screen points, so the path, the easing, the
// ripple/outline/fade timeline and the idle decision are unit-tested
// offline. The window side (cursor_view.go) only renders the frames this
// model produces; the controller (cursor.go) feeds it the clock.
//
// The motion follows vercel-labs/agent-desktop's cursor overlay
// (Apache-2.0, crates/core/src/cursor_overlay/hand_path.rs): a
// minimum-jerk ease along the straight line plus a slight sideways bow,
// with a Fitts-like, distance-scaled duration. The minimum-jerk
// polynomial and the bow ratio/cap are ported from there; the tremor,
// overshoot and seeded variation are left out. The ripple's duration
// (core/src/cursor_overlay/motion.rs RIPPLE_MS) and the label's reveal —
// held back while the cursor travels, then eased in once it lands
// (windows/src/system/cursor_overlay/reveal.rs and schedule.rs
// reveal_progress, core/src/cursor_overlay/timing.rs
// CURSOR_LABEL_REVEAL_MS) — are ported too, and so are the macOS
// bridge's two pops and the highlight's opacity
// (crates/macos/src/system/cursor_overlay/cursor_overlay_chrome_bridge.m:
// ADShowBubble's 0.94 → 1 ease-out scale over the reveal;
// ADHighlightShow's scale and opacity keyframes over the highlight hold,
// core/src/cursor_overlay/timing.rs CURSOR_HIGHLIGHT_HOLD_MS); all from
// HEAD 9eb9b29, modified. See third_party/agent-desktop/LICENSE.

import (
	"math"
	"time"
)

// Agent Cursor timings.
const (
	// cursorTravelMin and cursorTravelMax clamp the travel's duration; a
	// typical on-screen hop takes about 300 ms.
	cursorTravelMin = 150 * time.Millisecond
	cursorTravelMax = 400 * time.Millisecond

	// cursorRippleDuration is the click ripple: a spreading, fading ring
	// around a fading disc (agent-desktop's RIPPLE_MS).
	cursorRippleDuration = 300 * time.Millisecond
	// cursorLabelReveal is how long a new label's pill takes to fade in
	// once the cursor has landed (CURSOR_LABEL_REVEAL_MS).
	cursorLabelReveal = 180 * time.Millisecond
	// cursorOutlineDuration is the highlight's whole life: agent-desktop's
	// CURSOR_HIGHLIGHT_HOLD_MS, over which macOS plays its opacity and
	// scale keyframes with an ease-in-ease-out curve (ADHighlightShow). It
	// fades in by about 125 ms, pops until about 280 ms, holds until about
	// 490 ms and fades out by 900 ms.
	cursorOutlineDuration = 900 * time.Millisecond

	// cursorIdleDelay: after this long without Travel or Click the
	// cursor fades out (cursorFadeDuration) and hides; the next Travel
	// starts again from its origin's centre. agent-desktop rests after
	// 6 s, but an agent usually thinks 5–20 s between Actions, so 6 s
	// would hide the cursor between nearly every step; 30 s keeps it on
	// screen while the agent is working.
	cursorIdleDelay    = 30 * time.Second
	cursorFadeDuration = 200 * time.Millisecond

	// cursorFrameInterval is the animation timer's period. SetTimer rounds
	// up to the system tick (15.6 ms by default), so asking for the
	// USER_TIMER_MINIMUM of 10 ms yields about 64 frames per second;
	// positions are computed from the clock, never from frame counts.
	cursorFrameInterval = 10 * time.Millisecond
)

// Path shape (agent-desktop's hand_path.rs: BOW_RATIO, MAX_BOW).
const (
	cursorBowRatio = 0.055 // sideways bow as a fraction of the distance
	cursorMaxBow   = 38.0  // the bow's cap, logical pixels
)

// fpoint is a screen point in physical pixels, with sub-pixel precision
// for the motion maths.
type fpoint struct{ X, Y float64 }

// rectCentre returns r's centre.
func rectCentre(r Rect) fpoint {
	return fpoint{X: float64(r.Left+r.Right) / 2, Y: float64(r.Top+r.Bottom) / 2}
}

func (p fpoint) dist(q fpoint) float64 { return math.Hypot(q.X-p.X, q.Y-p.Y) }

// minimumJerk is the minimum-jerk ease-in-out on [0, 1]:
// 10t³ − 15t⁴ + 6t⁵ (agent-desktop's minimum_jerk). Monotonic, with zero
// velocity and acceleration at both ends; clamped outside [0, 1].
func minimumJerk(t float64) float64 {
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	return t * t * t * (10 + t*(-15+6*t))
}

func clamp01(v float64) float64 {
	if v < 0 || math.IsNaN(v) {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// cursorTravelDuration is how long a travel from→to takes: zero for a hop
// under two pixels (nothing to animate), otherwise logarithmic in the
// distance measured in logical pixels (so a 200 % monitor does not double
// the time), clamped to [cursorTravelMin, cursorTravelMax]. About 300 ms
// for a typical 400-logical-pixel hop.
func cursorTravelDuration(from, to fpoint, scale float64) time.Duration {
	d := from.dist(to)
	if d < 2 || math.IsNaN(d) {
		return 0
	}
	if scale <= 0 || math.IsNaN(scale) {
		scale = 1
	}
	ms := 120 + 60*math.Log2(1+d/scale/50)
	dur := time.Duration(ms * float64(time.Millisecond))
	return min(max(dur, cursorTravelMin), cursorTravelMax)
}

// cursorPath is one travel: from → to over dur, eased, bowed slightly to
// one side so the glide reads as a hand's motion rather than a slide.
type cursorPath struct {
	from, to fpoint
	dur      time.Duration
	bow      float64 // signed sideways offset at mid-travel, physical px
}

func newCursorPath(from, to fpoint, scale float64) cursorPath {
	if scale <= 0 || math.IsNaN(scale) {
		scale = 1
	}
	return cursorPath{
		from: from,
		to:   to,
		dur:  cursorTravelDuration(from, to, scale),
		bow:  math.Min(cursorBowRatio*from.dist(to), cursorMaxBow*scale),
	}
}

// progress maps the time since the travel started to [0, 1].
func (p cursorPath) progress(elapsed time.Duration) float64 {
	if p.dur <= 0 || elapsed >= p.dur {
		return 1
	}
	if elapsed <= 0 {
		return 0
	}
	return float64(elapsed) / float64(p.dur)
}

// at returns the cursor's tip at time progress t ∈ [0, 1]: the endpoints
// are exact, the motion along from→to is minimum-jerk eased (monotonic),
// and the bow is perpendicular to it, zero at both ends.
func (p cursorPath) at(t float64) fpoint {
	if t <= 0 {
		return p.from
	}
	if t >= 1 {
		return p.to
	}
	dx, dy := p.to.X-p.from.X, p.to.Y-p.from.Y
	d := math.Hypot(dx, dy)
	s := minimumJerk(t)
	pt := fpoint{X: p.from.X + dx*s, Y: p.from.Y + dy*s}
	if d > 0 {
		side := p.bow * math.Sin(math.Pi*s)
		pt.X += -dy / d * side
		pt.Y += dx / d * side
	}
	return pt
}

// rippleOpacity is the click ripple's opacity at phase ∈ (0, 1]: it fades
// linearly while it spreads (rippleSpread, cursor_layout.go).
func rippleOpacity(phase float64) float64 { return 1 - clamp01(phase) }

// outlineOpacity is the highlight's opacity at elapsed (ADHighlightShow's
// opacity keyframes): in from nothing, held, out to nothing at
// cursorOutlineDuration.
func outlineOpacity(elapsed time.Duration) float64 {
	if elapsed < 0 || elapsed >= cursorOutlineDuration {
		return 0
	}
	return keyframe(highlightOpacityTimes, highlightOpacityValues, outlineClock(elapsed))
}

// cubicBezier is the CoreAnimation/CSS timing curve through (0,0),
// (x1,y1), (x2,y2), (1,1): the progress y at time fraction x ∈ [0, 1]
// (x1, x2 ∈ [0, 1], so x is monotonic in the curve's parameter).
func cubicBezier(x1, y1, x2, y2, x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	at := func(p1, p2, u float64) float64 {
		v := 1 - u
		return 3*v*v*u*p1 + 3*v*u*u*p2 + u*u*u
	}
	lo, hi := 0.0, 1.0
	for range 40 { // bisection: 2⁻⁴⁰ is far below a pixel or a millisecond
		mid := (lo + hi) / 2
		if at(x1, x2, mid) < x {
			lo = mid
		} else {
			hi = mid
		}
	}
	return at(y1, y2, (lo+hi)/2)
}

// caEaseOut and caEaseInOut are CoreAnimation's
// kCAMediaTimingFunctionEaseOut and …EaseInEaseOut.
func caEaseOut(x float64) float64   { return cubicBezier(0, 0, 0.58, 1, x) }
func caEaseInOut(x float64) float64 { return cubicBezier(0.42, 0, 0.58, 1, x) }

// keyframe interpolates linearly through values at times (a
// CAKeyframeAnimation's linear calculation mode); times rise from 0 to 1.
func keyframe(times, values []float64, t float64) float64 {
	if t <= times[0] {
		return values[0]
	}
	for i := 1; i < len(times); i++ {
		if t <= times[i] {
			f := (t - times[i-1]) / (times[i] - times[i-1])
			return values[i-1] + (values[i]-values[i-1])*f
		}
	}
	return values[len(values)-1]
}

// The highlight's keyframes (ADHighlightShow): its scale and its opacity.
var (
	highlightPopTimes      = []float64{0, 0.12, 0.22, 1}
	highlightPopValues     = []float64{1.08, 0.99, 1, 1}
	highlightOpacityTimes  = []float64{0, 0.04, 0.55, 1}
	highlightOpacityValues = []float64{0, 1, 1, 0}
)

// outlineClock maps elapsed onto the highlight's eased progress.
func outlineClock(elapsed time.Duration) float64 {
	return caEaseInOut(float64(elapsed) / float64(cursorOutlineDuration))
}

// outlinePop is the highlight's scale at elapsed, about its centre:
// 1.08, dipping to 0.99, settling at 1.
func outlinePop(elapsed time.Duration) float64 {
	if elapsed < 0 {
		return highlightPopValues[0]
	}
	return keyframe(highlightPopTimes, highlightPopValues, outlineClock(elapsed))
}

// Label pop (ADShowBubble): the pill grows from 94 % to full size over
// the reveal, eased out.
const cursorLabelPopFrom = 0.94

// labelPop is the label pill's scale at elapsed into its reveal.
func labelPop(elapsed time.Duration) float64 {
	t := clamp01(float64(elapsed) / float64(cursorLabelReveal))
	return cursorLabelPopFrom + (1-cursorLabelPopFrom)*caEaseOut(t)
}

// fadeOpacity is the idle fade-out's opacity at elapsed into it.
func fadeOpacity(elapsed time.Duration) float64 {
	return 1 - clamp01(float64(elapsed)/float64(cursorFadeDuration))
}

// revealOpacity is a revealing label pill's opacity at elapsed into the
// reveal: an ease-out cubic, 1 − (1 − t)³ (reveal_progress), whole from
// cursorLabelReveal on.
func revealOpacity(elapsed time.Duration) float64 {
	if elapsed >= cursorLabelReveal {
		return 1
	}
	rest := 1 - clamp01(float64(elapsed)/float64(cursorLabelReveal))
	return 1 - rest*rest*rest
}

// cursorFrame is what one frame shows. The controller hands it to the
// view; every opacity is in [0, 1].
type cursorFrame struct {
	Cursor      bool // the dart (and its label pill) is on screen
	Tip         fpoint
	Opacity     float64
	LabelOp     float64 // the label pill's own opacity (under Opacity)
	LabelPop    float64 // the label pill's scale, about its corner nearest the tip
	Travelling  bool    // a travel is still in flight
	Ripple      bool
	RippleAt    fpoint
	RipplePhase float64 // (0, 1]
	Outline     bool
	OutlineRect Rect
	OutlineOp   float64
	OutlinePop  float64 // the outline's scale about the element's centre
}

// cursorModel is the Agent Cursor's state, advanced by the clock. It is
// pure: no Win32, no goroutines — the controller owns one on the UI
// thread and calls it with time.Now.
type cursorModel struct {
	shown  bool // on screen (fading included)
	tip    fpoint
	scale  float64
	travel bool
	path   cursorPath
	start  time.Time // the travel's start

	ripple      bool
	rippleAt    fpoint
	rippleStart time.Time

	outline      bool
	outlineRect  Rect
	outlineStart time.Time

	fading    bool
	fadeStart time.Time

	// The label pill's reveal (reveal.rs): pending while a travel with a
	// new label is in flight (the pill is not drawn), then revealing from
	// the arrival; settled (drawn in full) otherwise.
	labelPending   bool
	labelRevealing bool
	labelStart     time.Time

	lastActive time.Time // the last Travel or Click
}

// revealLabel holds the label pill back until the travel just started
// lands, then eases it in. The controller calls it after startTravel
// when the label is new.
func (m *cursorModel) revealLabel() {
	m.labelPending, m.labelRevealing = true, false
}

// labelReveal advances the label's reveal to now and returns the pill's
// opacity and scale (its pop).
func (m *cursorModel) labelReveal(now time.Time) (op, pop float64) {
	if m.labelPending && !m.travel {
		m.labelPending, m.labelRevealing = false, true
		m.labelStart = m.start.Add(m.path.dur) // the arrival
	}
	switch {
	case m.labelPending:
		return 0, cursorLabelPopFrom
	case m.labelRevealing:
		el := max(now.Sub(m.labelStart), 0)
		if el >= cursorLabelReveal {
			m.labelRevealing = false
		}
		return revealOpacity(el), labelPop(el)
	}
	return 1, 1
}

// startTravel begins a glide to target's centre. A hidden or fading
// cursor starts at origin's centre; a visible one sets off from where it
// is (mid-travel included). It returns the travel's duration.
func (m *cursorModel) startTravel(now time.Time, origin, target Rect, scale float64) time.Duration {
	from := m.tipAt(now)
	if !m.shown || m.fading {
		from = rectCentre(origin)
	}
	m.shown, m.fading = true, false
	m.scale = scale
	m.tip = from
	m.path = newCursorPath(from, rectCentre(target), scale)
	m.start = now
	m.travel = m.path.dur > 0
	if !m.travel {
		m.tip = m.path.to
	}
	m.lastActive = now
	return m.path.dur
}

// startClick ripples at the cursor (at a travel's destination when one
// is still in flight) and outlines target. A hidden cursor appears at
// target's centre first.
func (m *cursorModel) startClick(now time.Time, target Rect, scale float64) {
	if !m.shown || m.fading {
		m.shown, m.fading = true, false
		m.tip = rectCentre(target)
		m.travel = false
		m.scale = scale
		m.labelPending, m.labelRevealing = false, false
	}
	at := m.tip
	if m.travel {
		at = m.path.to
	}
	m.ripple, m.rippleAt, m.rippleStart = true, at, now
	if target.Width() > 0 && target.Height() > 0 {
		m.outline, m.outlineRect, m.outlineStart = true, target, now
	}
	m.lastActive = now
}

// tipAt is the tip's position at now, without advancing the model.
func (m *cursorModel) tipAt(now time.Time) fpoint {
	if m.travel {
		return m.path.at(m.path.progress(now.Sub(m.start)))
	}
	return m.tip
}

// advance moves the model to now — finishing a travel, a ripple, an
// outline or a fade that ran out, starting the idle fade when it is due —
// and returns the frame to show.
func (m *cursorModel) advance(now time.Time) cursorFrame {
	if m.travel {
		t := m.path.progress(now.Sub(m.start))
		m.tip = m.path.at(t)
		if t >= 1 {
			m.travel = false
		}
	}
	var f cursorFrame
	if m.ripple {
		phase := float64(now.Sub(m.rippleStart)) / float64(cursorRippleDuration)
		if phase >= 1 {
			m.ripple = false
		} else {
			f.Ripple, f.RippleAt, f.RipplePhase = true, m.rippleAt, math.Max(phase, 0)
		}
	}
	if m.outline {
		el := now.Sub(m.outlineStart)
		if el >= cursorOutlineDuration {
			m.outline = false
		} else {
			f.Outline, f.OutlineRect, f.OutlineOp = true, m.outlineRect, outlineOpacity(el)
			f.OutlinePop = outlinePop(el)
		}
	}
	if m.idleDue(now) {
		m.fading = true
		m.fadeStart = m.lastActive.Add(cursorIdleDelay)
	}
	op := 1.0
	if m.fading {
		el := now.Sub(m.fadeStart)
		if el >= cursorFadeDuration {
			m.shown, m.fading = false, false
		}
		op = fadeOpacity(el)
	}
	labelOp, labelPop := m.labelReveal(now)
	if m.shown {
		f.Cursor, f.Tip, f.Opacity, f.LabelOp, f.LabelPop = true, m.tip, op, labelOp, labelPop
	}
	f.Travelling = m.travel
	return f
}

// idleDue reports whether the idle fade should start: the cursor is
// shown, not already fading, nothing is animating, and cursorIdleDelay
// passed since the last Travel or Click.
func (m *cursorModel) idleDue(now time.Time) bool {
	return m.shown && !m.fading && !m.animating() && now.Sub(m.lastActive) >= cursorIdleDelay
}

// animating reports whether frames must keep coming at the frame rate.
func (m *cursorModel) animating() bool {
	return m.travel || m.ripple || m.outline || m.fading || m.labelPending || m.labelRevealing
}

// nextWake is when the controller's timer should fire next: a frame
// interval while anything animates, the idle deadline while the cursor
// rests on screen, never (false) once it is hidden.
func (m *cursorModel) nextWake(now time.Time) (time.Duration, bool) {
	if m.animating() {
		return cursorFrameInterval, true
	}
	if !m.shown {
		return 0, false
	}
	d := m.lastActive.Add(cursorIdleDelay).Sub(now)
	return max(d, cursorFrameInterval), true
}

// visibleForTravel reports whether a Travel starting now would set off
// from the current spot (true) or from its origin's centre (false).
func (m *cursorModel) visibleForTravel() bool { return m.shown && !m.fading }
