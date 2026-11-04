package win

// The Agent Cursor's motion and timeline (headless ticket 03): pure
// functions of time and screen points, so the path, the easing, the
// press/outline/float/fade timeline and the idle decision are unit-tested
// offline. The window side (cursor_view.go) only renders the frames this
// model produces; the controller (cursor.go) feeds it the clock.
//
// The motion follows trycua/cua's default cursor motion at ecaf10b (MIT,
// libs/cua-driver/rust/crates/cua-cursor-motion; cursor-look spec,
// ADR 0018): the "signature_arc" path, a cubic bezier bowed to its natural
// side (upward for a horizontal move) with its apex about 9 % of the
// distance off the chord (path.rs, spec.rs); eased by distance through an
// arc-length table with the "steady" timing's in-out sine (plan.rs) plus a
// small follow-through past the target (ease.rs bump_profile); a Fitts
// duration with a 600 ms floor (spec.rs, style.rs); arrival, when the
// Action is delivered, as the first moment the tip is within a pixel of
// the target (plan.rs arrival_t); and the tip turning along the path
// (cursor_pose.rs heading rotation). Reduced motion is their straight
// 120 ms minimum-jerk move (plan.rs). The minimum-jerk polynomial is
// vercel-labs/agent-desktop's (Apache-2.0,
// crates/core/src/cursor_overlay/hand_path.rs). The macOS bridge's
// highlight pop and opacity are ported too
// (crates/macos/src/system/cursor_overlay/cursor_overlay_chrome_bridge.m:
// ADHighlightShow's scale and opacity keyframes over the highlight hold,
// core/src/cursor_overlay/timing.rs CURSOR_HIGHLIGHT_HOLD_MS); all from
// HEAD 9eb9b29, modified. See third_party/agent-desktop/LICENSE.

import (
	"math"
	"time"
)

// Agent Cursor timings.
const (
	// cursorTravelFloor is the shortest travel: cua's "steady" timing gives
	// every move at least 600 ms (style.rs; "relaxed" is 450, "calm" 750),
	// so even a short hop reads as a glide. The one knob for the feel.
	cursorTravelFloor = 600 * time.Millisecond
	// cursorFittsMin and cursorFittsMax clamp the Fitts time before its
	// 1.1 stretch (spec.rs), so the longest travel takes 1.1 s.
	cursorFittsMin     = 300 * time.Millisecond
	cursorFittsMax     = 1000 * time.Millisecond
	cursorFittsStretch = 1.1
	// cursorTravelMax is the longest travel (the Fitts cap, stretched).
	cursorTravelMax = time.Duration(float64(cursorFittsMax) * cursorFittsStretch)
	// cursorReducedTravel is the whole move when Windows' animations are
	// off (SPI_GETCLIENTAREAANIMATION): straight, minimum-jerk, nothing
	// else moving (plan.rs reduced motion).
	cursorReducedTravel = 120 * time.Millisecond

	// cursorPressDuration is the click's press: the arrow squeezes to 93 %,
	// springs to 103 % and settles (cua's action_click theme frames, 20 at
	// 30 fps).
	cursorPressDuration = 667 * time.Millisecond
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

	// cursorFrameInterval is the animation timer's period, about 120
	// frames per second. SetTimer rounds up to the system tick, 15.6 ms by
	// default, so the view raises the timer resolution to 1 ms while
	// anything animates and gives it back when the cursors rest
	// (cursor_view.go, cua's overlay.rs); positions are computed from the
	// clock, never from frame counts.
	cursorFrameInterval = 8 * time.Millisecond
	// cursorFloatInterval is the timer's period while a resting cursor
	// floats: about 32 frames per second at the default tick, without the
	// raised resolution.
	cursorFloatInterval = 30 * time.Millisecond
)

// Path shape: cua's signature_arc (spec.rs, params.rs).
const (
	cursorArcSize   = 0.16  // the bow's control offset as a fraction of the distance
	cursorArcFlow   = 0.575 // (arc_flow 0.15 + 1) / 2: the bow leans a little toward the end
	cursorArcHandle = 0.3   // each control point's way along the chord
	// cursorOvershoot is the follow-through past the target, a fraction of
	// the distance, at most cursorOvershootMax logical pixels, peaking at
	// cursorOvershootAt of the travel (spec.rs bump_profile).
	cursorOvershoot    = 0.018
	cursorOvershootMax = 8.0
	cursorOvershootAt  = 0.82
	// cursorArrivalSlack: the travel has arrived (Travel returns and the
	// Action is delivered) once the tip is this close to the target,
	// logical pixels (plan.rs arrival_t); the follow-through settles while
	// the Action runs.
	cursorArrivalSlack = 1.0
	// cursorTargetDefault is Fitts' target width when the target has no
	// size, and cursorTargetMin its least, logical pixels (spec.rs).
	cursorTargetDefault = 24.0
	cursorTargetMin     = 4.0
	// cursorArcSteps is the arc-length table's resolution.
	cursorArcSteps = 128
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

// sineInOut is cua's "steady" ease, 0.5 − 0.5·cos(πt): it replaces the
// path's native minimum-jerk "so a slow short hop does not linger at its
// ends" (plan.rs). Clamped outside [0, 1].
func sineInOut(t float64) float64 {
	t = clamp01(t)
	return 0.5 - 0.5*math.Cos(math.Pi*t)
}

// smoothstep is 3t² − 2t³ on [0, 1], clamped outside.
func smoothstep(t float64) float64 {
	t = clamp01(t)
	return t * t * (3 - 2*t)
}

// overshootBump is cua's bump_profile (ease.rs): zero at both ends, 1 at
// its peak at cursorOvershootAt, a normalised τᵃ(1 − τ)ᵇ.
func overshootBump(t float64) float64 {
	if t <= 0 || t >= 1 {
		return 0
	}
	a := math.Max(cursorOvershootAt*10, 1.5)
	b := math.Max((1-cursorOvershootAt)*10, 1.5)
	peak := math.Pow(a/(a+b), a) * math.Pow(b/(a+b), b)
	return math.Pow(t, a) * math.Pow(1-t, b) / peak
}

// targetSide is Fitts' width of a target in physical pixels: its smaller
// side, 0 for an empty one.
func targetSide(r Rect) float64 {
	if r.Width() <= 0 || r.Height() <= 0 {
		return 0
	}
	return float64(min(r.Width(), r.Height()))
}

// cursorTravelDuration is how long a travel from→to takes, toward a target
// whose smaller side is side physical pixels (0: none): zero for a hop
// under two pixels (nothing to animate), cursorReducedTravel when Windows'
// animations are off, otherwise cua's Fitts time — 150 + 120·log2(D/W + 1)
// ms with D and W in logical pixels (so a 200 % monitor does not double
// the time), clamped to [300, 1000] ms and stretched by 1.1 — and never
// under cursorTravelFloor. About 620 ms for 400 px to a 40 px button,
// 955 ms for 1500 px to a 24 px one.
func cursorTravelDuration(from, to fpoint, side, scale float64, reduced bool) time.Duration {
	d := from.dist(to)
	if d < 2 || math.IsNaN(d) {
		return 0
	}
	if reduced {
		return cursorReducedTravel
	}
	if scale <= 0 || math.IsNaN(scale) {
		scale = 1
	}
	w := side / scale
	if w <= 0 || math.IsNaN(w) {
		w = cursorTargetDefault
	}
	w = math.Max(w, cursorTargetMin)
	ms := 150 + 120*math.Log2(d/scale/w+1)
	fitts := min(max(time.Duration(ms*float64(time.Millisecond)), cursorFittsMin), cursorFittsMax)
	return max(time.Duration(float64(fitts)*cursorFittsStretch), cursorTravelFloor)
}

// cursorPath is one travel: from → to over dur along cua's signature arc,
// eased by distance, with a small follow-through past to that settles
// back by dur. It arrives (arrive) a little before dur.
type cursorPath struct {
	from, to fpoint
	dur      time.Duration
	arrive   time.Duration // the first moment the tip is within cursorArrivalSlack of to
	straight bool          // reduced motion: a straight minimum-jerk line, nothing else
	c1, c2   fpoint        // the bezier's inner control points
	arc      []float64     // arc length at bezier parameter i/cursorArcSteps
	length   float64       // the arc's whole length, physical pixels
	over     float64       // the follow-through, a fraction of length
	dir      fpoint        // the unit tangent at to: where the follow-through goes
	turn     float64       // the tip's rotation for the chord, radians; 0: no turning
	scale    float64       // the monitor's dpi/96: physical pixels per logical one
}

// newCursorPath plans a travel from → to toward target (its size sets
// the duration) at scale; reduced is Windows' animations being off.
func newCursorPath(from, to fpoint, target Rect, scale float64, reduced bool) cursorPath {
	if scale <= 0 || math.IsNaN(scale) {
		scale = 1
	}
	p := cursorPath{
		from:     from,
		to:       to,
		dur:      cursorTravelDuration(from, to, targetSide(target), scale, reduced),
		straight: reduced,
		scale:    scale,
	}
	d := from.dist(to)
	if p.dur <= 0 || d <= 0 {
		p.arrive = p.dur
		return p
	}
	ux, uy := (to.X-from.X)/d, (to.Y-from.Y)/d
	if !reduced {
		// cua's Cua bezier (path.rs): handles along the chord, both pushed to
		// the natural side — the chord turned a quarter left for a rightward
		// move, right for a leftward one, so a horizontal move bows upward.
		nx, ny := uy, -ux
		if ux < 0 {
			nx, ny = -uy, ux
		}
		off := cursorArcSize * d
		p.c1 = fpoint{X: from.X + cursorArcHandle*(to.X-from.X) + nx*off*(1-0.5*cursorArcFlow),
			Y: from.Y + cursorArcHandle*(to.Y-from.Y) + ny*off*(1-0.5*cursorArcFlow)}
		p.c2 = fpoint{X: to.X - cursorArcHandle*(to.X-from.X) + nx*off*(1-0.5*(1-cursorArcFlow)),
			Y: to.Y - cursorArcHandle*(to.Y-from.Y) + ny*off*(1-0.5*(1-cursorArcFlow))}
		p.arc = make([]float64, cursorArcSteps+1)
		prev := from
		for i := 1; i <= cursorArcSteps; i++ {
			pt := p.bezier(float64(i) / cursorArcSteps)
			p.arc[i] = p.arc[i-1] + prev.dist(pt)
			prev = pt
		}
		p.length = p.arc[cursorArcSteps]
		p.dir = unit(p.tangent(1), fpoint{X: ux, Y: uy})
		p.over = math.Min(cursorOvershoot, cursorOvershootMax*scale/p.length)
		if d/scale >= cursorTurnMinHop {
			p.turn = wrapAngle(math.Atan2(uy, ux) - cursorTipAxis)
		}
	} else {
		p.length = d
		p.dir = fpoint{X: ux, Y: uy}
	}
	// The arrival, sampled every millisecond: the follow-through passes
	// within the slack on its way out, so it is found well before dur.
	p.arrive = p.dur
	slack := cursorArrivalSlack * scale
	for el := time.Duration(0); el < p.dur; el += time.Millisecond {
		if p.at(p.progress(el)).dist(to) <= slack {
			p.arrive = el
			break
		}
	}
	return p
}

// unit is v scaled to length 1, or fallback for a zero v.
func unit(v, fallback fpoint) fpoint {
	l := math.Hypot(v.X, v.Y)
	if l == 0 || math.IsNaN(l) {
		return fallback
	}
	return fpoint{X: v.X / l, Y: v.Y / l}
}

// bezier is the path's cubic at parameter s ∈ [0, 1].
func (p cursorPath) bezier(s float64) fpoint {
	v := 1 - s
	a, b, c, d := v*v*v, 3*v*v*s, 3*v*s*s, s*s*s
	return fpoint{
		X: a*p.from.X + b*p.c1.X + c*p.c2.X + d*p.to.X,
		Y: a*p.from.Y + b*p.c1.Y + c*p.c2.Y + d*p.to.Y,
	}
}

// tangent is the cubic's derivative at s (not normalised).
func (p cursorPath) tangent(s float64) fpoint {
	v := 1 - s
	a, b, c := 3*v*v, 6*v*s, 3*s*s
	return fpoint{
		X: a*(p.c1.X-p.from.X) + b*(p.c2.X-p.c1.X) + c*(p.to.X-p.c2.X),
		Y: a*(p.c1.Y-p.from.Y) + b*(p.c2.Y-p.c1.Y) + c*(p.to.Y-p.c2.Y),
	}
}

// param is the bezier parameter a fraction u ∈ [0, 1] of the arc's length
// along: the ease acts on distance, not on the parameter (path.rs).
func (p cursorPath) param(u float64) float64 {
	s := u * p.length
	i := max(min(sortSearch(p.arc, s), cursorArcSteps), 1)
	seg := p.arc[i] - p.arc[i-1]
	f := 0.0
	if seg > 0 {
		f = clamp01((s - p.arc[i-1]) / seg)
	}
	return (float64(i-1) + f) / cursorArcSteps
}

// sortSearch is the first index of the ascending a whose value is ≥ v.
func sortSearch(a []float64, v float64) int {
	lo, hi := 0, len(a)
	for lo < hi {
		mid := (lo + hi) / 2
		if a[mid] < v {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
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

// along is how far along the path, as a fraction of its length, the tip
// is at time progress t: the sine ease covers the path by
// cursorOvershootAt, while the bump carries the tip past the end, to
// 1 + over at its peak there, and settles it back by t = 1. (Added to an
// ease spanning the whole move, a bump peaking at 0.82 would never get
// past the target: the ease still trails by 8 % then.)
func (p cursorPath) along(t float64) float64 {
	if p.straight {
		return minimumJerk(t)
	}
	return sineInOut(t/cursorOvershootAt) + p.over*overshootBump(t)
}

// at returns the cursor's tip at time progress t ∈ [0, 1]: the endpoints
// are exact; between them the tip moves along the arc, eased by distance,
// past to along its end tangent during the follow-through, and back.
func (p cursorPath) at(t float64) fpoint {
	if t <= 0 {
		return p.from
	}
	if t >= 1 {
		return p.to
	}
	u := p.along(t)
	switch {
	case u >= 1:
		k := (u - 1) * p.length
		return fpoint{X: p.to.X + p.dir.X*k, Y: p.to.Y + p.dir.Y*k}
	case p.straight || p.arc == nil:
		return fpoint{X: p.from.X + (p.to.X-p.from.X)*u, Y: p.from.Y + (p.to.Y-p.from.Y)*u}
	}
	return p.bezier(p.param(u))
}

// The tip's rotation along the path: cua's default cursor.rotate=heading,
// ported 1:1 from cursor-overlay's cursor_pose.rs travel_rotation (trycua/
// cua at ecaf10b, MIT; cursor-turn, .scratch/cursor-look/turning-1to1.md).
// The arrow turns to point where it is going, easing in over cursorTurnIn
// and out over cursorTurnOut before the arrival (both stretched by
// 1 + |turn|/π for a big turn, and each at most half the time to the
// arrival), and is upright at the arrival and after it.
const (
	cursorTurnIn  = 150 * time.Millisecond // ROTATE_RAMP_IN_SECS
	cursorTurnOut = 250 * time.Millisecond // ROTATE_RAMP_OUT_SECS
	// cursorTurnMinHop: hops shorter than this, logical pixels, never turn
	// (ROTATE_MIN_HOP_PT).
	cursorTurnMinHop = 40.0
	// cursorTurnVelocityStep is the half step of the central difference the
	// travel direction is read from (Trajectory::velocity_at's h).
	cursorTurnVelocityStep = 8 * time.Millisecond
	// cursorTurnMinSpeed: below this speed, logical pixels per second, the
	// chord stands in for the travel direction (velocity_at's 1 pt/s).
	cursorTurnMinSpeed = 1.0
)

// cursorTipAxis is the upright arrow's own heading by cua's rule
// (cursor_pose.rs tip_axis): the direction from the centre of the arrow's
// body to its tip. cua measures BODY_OFFSET_PT as the centre of the
// painted arrow's opaque pixels; here it is the dart's area centroid
// (arrowCentroid), which the painted pixels match
// (TestTipAxisIsTheBodyCentreToTheTip). About −136° for the dart; cua's
// own arrow's is about −99.5°.
var cursorTipAxis = func() float64 {
	c := arrowCentroid()
	return math.Atan2(-c.Y, -c.X)
}()

// arrowCentroid is the area centroid of the upright arrow's outline, in
// logical pixels off the tip.
func arrowCentroid() fpoint {
	pts := arrowPoints(1)
	var a, cx, cy float64
	for i := range pts {
		p, q := pts[i], pts[(i+1)%len(pts)]
		cr := p.X*q.Y - q.X*p.Y
		a += cr
		cx += (p.X + q.X) * cr
		cy += (p.Y + q.Y) * cr
	}
	return fpoint{X: cx / (3 * a), Y: cy / (3 * a)}
}

// wrapAngle wraps a into (−π, π].
func wrapAngle(a float64) float64 {
	a = math.Mod(a+math.Pi, 2*math.Pi)
	if a < 0 {
		a += 2 * math.Pi
	}
	return a - math.Pi
}

// tipAngle is the arrow's rotation, radians clockwise on the screen, at
// elapsed into the travel (travel_rotation): zero at the start, at the
// arrival and after it, and toward where the tip is going between.
func (p cursorPath) tipAngle(elapsed time.Duration) float64 {
	if p.turn == 0 || elapsed >= p.arrive {
		return 0
	}
	t, arrive := elapsed.Seconds(), p.arrive.Seconds()
	stretch := 1 + math.Abs(p.turn)/math.Pi
	in, out := turnRamps(t, arrive, cursorTurnIn.Seconds()*stretch, cursorTurnOut.Seconds()*stretch)
	chord := math.Atan2(p.to.Y-p.from.Y, p.to.X-p.from.X)
	// The direction the tip travels: its velocity by a central difference
	// over the shown path, or the chord when it barely moves.
	a := p.at(p.progress(elapsed - cursorTurnVelocityStep))
	b := p.at(p.progress(elapsed + cursorTurnVelocityStep))
	h := 2 * cursorTurnVelocityStep.Seconds()
	vx, vy := (b.X-a.X)/h, (b.Y-a.Y)/h
	travel := chord
	if math.Hypot(vx, vy) > cursorTurnMinSpeed*p.scale {
		travel = math.Atan2(vy, vx)
	}
	// Unwrapped about the chord, so a path that wobbles near straight down
	// never flips between +π and −π.
	target := p.turn + wrapAngle(travel-chord)
	return target * in * out
}

// turnRamps are the turn's ease-in after the start and ease-out before
// the arrival, each at most half the time to the arrival, so a short move
// still reaches exactly 0 at both ends (cursor_pose.rs ramps).
func turnRamps(t, arrive, rampIn, rampOut float64) (in, out float64) {
	rampIn = math.Max(math.Min(rampIn, arrive*0.5), 1e-6)
	rampOut = math.Max(math.Min(rampOut, arrive*0.5), 1e-6)
	return smoothstep(t / rampIn), smoothstep((arrive - t) / rampOut)
}

// The click's press (cua's action_click, build_default_theme.py): the
// arrow's scale about its tip at these times of cursorPressDuration, each
// segment eased with cubic-bezier(0.2, 0.75, 0.25, 1).
var (
	pressTimes  = []float64{0, 0.35, 0.6, 1}
	pressValues = []float64{1, 0.93, 1.03, 1}
)

// pressScale is the arrow's scale elapsed into a click's press: 1 before
// and after it.
func pressScale(elapsed time.Duration) float64 {
	if elapsed <= 0 || elapsed >= cursorPressDuration {
		return 1
	}
	t := float64(elapsed) / float64(cursorPressDuration)
	for i := 1; i < len(pressTimes); i++ {
		if t <= pressTimes[i] {
			f := (t - pressTimes[i-1]) / (pressTimes[i] - pressTimes[i-1])
			return pressValues[i-1] + (pressValues[i]-pressValues[i-1])*cubicBezier(0.2, 0.75, 0.25, 1, f)
		}
	}
	return 1
}

// The idle float (cua's cursor_pose.rs, position only — no tilt, so a
// floating cursor's window is only moved, never re-rendered): on a 4 s
// loop the tip sways sideways and bobs, logical pixels. It is exactly zero
// while the cursor travels, presses, outlines or shows a caret, holds zero
// for cursorFloatHold after, then eases back in over cursorFloatIn; a click
// that comes while it floats eases it out over cursorFloatOut.
const (
	cursorFloatPeriod = 4 * time.Second
	cursorFloatSway   = 1.64
	cursorFloatBob    = 1.97
	cursorFloatHold   = 500 * time.Millisecond
	cursorFloatIn     = 800 * time.Millisecond
	cursorFloatOut    = 200 * time.Millisecond
)

// floatOffset is the float's offset of the tip at phase (seconds into the
// loop, any value) and amplitude amp ∈ [0, 1], physical pixels: sway
// sin, bob cos, centred cursorFloatSway above the tip — exactly zero at
// amplitude zero.
func floatOffset(phase time.Duration, amp, scale float64) fpoint {
	if amp <= 0 {
		return fpoint{}
	}
	a := 2 * math.Pi * float64(phase%cursorFloatPeriod) / float64(cursorFloatPeriod)
	return fpoint{
		X: amp * cursorFloatSway * math.Sin(a) * scale,
		Y: amp * (cursorFloatBob*math.Cos(a) - cursorFloatSway) * scale,
	}
}

// The optional comet trail (cua's effects.rs, off by default;
// WINUSE_CURSOR_TRAIL=1): the last cursorTrailSpan of the path in
// cursorTrailSegments pieces, from cursorTrailBehind logical pixels behind
// the tip, so the tip stays clean.
const (
	cursorTrailSpan     = 180 * time.Millisecond
	cursorTrailSegments = 26
	cursorTrailBehind   = 16.0
	cursorTrailWidthMin = 2.0  // at its tail, logical pixels
	cursorTrailWidthMax = 12.0 // at its head
	cursorTrailAlpha    = 0.38 // at its head; 0.38·k² along it
)

// trailPoints is the trail at elapsed into p: cursorTrailSegments+1
// points of the path over the last cursorTrailSpan, tail first, with the
// last cursorTrailBehind·scale of it (nearest the tip) cut off. Nil when
// nothing of it is left.
func trailPoints(p cursorPath, elapsed time.Duration, scale float64) []fpoint {
	pts := make([]fpoint, 0, cursorTrailSegments+1)
	for k := 0; k <= cursorTrailSegments; k++ {
		at := elapsed - cursorTrailSpan*time.Duration(cursorTrailSegments-k)/cursorTrailSegments
		pts = append(pts, p.at(p.progress(max(at, 0))))
	}
	cut := cursorTrailBehind * scale
	for len(pts) >= 2 && cut > 0 {
		n := len(pts)
		seg := pts[n-2].dist(pts[n-1])
		if seg > cut {
			f := cut / seg
			pts[n-1] = fpoint{X: pts[n-1].X + (pts[n-2].X-pts[n-1].X)*f, Y: pts[n-1].Y + (pts[n-2].Y-pts[n-1].Y)*f}
			break
		}
		cut -= seg
		pts = pts[:n-1]
	}
	if len(pts) < 2 || pts[0].dist(pts[len(pts)-1]) < 1 {
		return nil
	}
	return pts
}

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

// caEaseInOut is CoreAnimation's kCAMediaTimingFunctionEaseInEaseOut.
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

// fadeOpacity is the idle fade-out's opacity at elapsed into it.
func fadeOpacity(elapsed time.Duration) float64 {
	return 1 - clamp01(float64(elapsed)/float64(cursorFadeDuration))
}

// cursorFrame is what one frame shows. The controller hands it to the
// view; every opacity is in [0, 1].
type cursorFrame struct {
	Cursor      bool // the arrow (and its name tag) is on screen
	Tip         fpoint
	Opacity     float64
	Angle       float64  // the arrow's rotation about its tip, radians clockwise
	Press       float64  // the arrow's scale about its tip (the click's press); 1 at rest
	Travelling  bool     // a travel has not yet arrived
	Trail       []fpoint // the optional comet trail, tail first; nil: none
	Outline     bool
	OutlineRect Rect
	OutlineOp   float64
	OutlinePop  float64 // the outline's scale about the element's centre
	Caret       bool    // the insertion caret (cursor_caret.go)
	CaretBar    Rect
	CaretOp     float64
}

// cursorModel is the Agent Cursor's state, advanced by the clock. It is
// pure: no Win32, no goroutines — the controller owns one on the UI
// thread and calls it with time.Now.
type cursorModel struct {
	shown   bool // on screen (fading included)
	tip     fpoint
	scale   float64
	travel  bool // a travel is in flight, its follow-through included
	arrived bool // and has arrived: Travel's waiter is released
	path    cursorPath
	start   time.Time // the travel's start

	// reduced is Windows' "animation effects" being off for the last
	// travel or click: no rotation, float, press or trail, and a short
	// straight move.
	reduced bool
	// trail draws the optional comet trail (WINUSE_CURSOR_TRAIL=1).
	trail bool

	press      bool // the click's press
	pressStart time.Time

	// The idle float: it may ease in from floatFrom; when something starts
	// while it floats it eases out from amplitude floatOutFrom at
	// floatOutStart.
	floatFrom     time.Time
	floatOutFrom  float64
	floatOutStart time.Time

	outline      bool
	outlineRect  Rect
	outlineStart time.Time

	caret      bool // the insertion caret (cursor_caret.go)
	caretBar   Rect
	caretStart time.Time

	fading    bool
	fadeStart time.Time

	lastActive time.Time // the last Travel or Click
}

// startTravel begins a glide to target's centre. A hidden or fading
// cursor starts at origin's centre; a visible one sets off from where it
// is (mid-travel and mid-float included, so it never jumps). reduced is
// Windows' animations being off. It returns the travel's duration.
func (m *cursorModel) startTravel(now time.Time, origin, target Rect, scale float64, reduced bool) time.Duration {
	from := m.tipAt(now)
	if !m.shown || m.fading {
		from = rectCentre(origin)
	}
	m.shown, m.fading = true, false
	m.scale = scale
	m.reduced = reduced
	m.tip = from
	m.path = newCursorPath(from, rectCentre(target), target, scale, reduced)
	m.start = now
	m.travel = m.path.dur > 0
	m.arrived = !m.travel
	if !m.travel {
		m.tip = m.path.to
	}
	m.floatOutFrom = 0 // the travel sets off from the floated spot
	m.lastActive = now
	return m.path.dur
}

// startClick presses the arrow and outlines target. A hidden cursor
// appears at target's centre first. A floating cursor eases its float out
// (cursorFloatOut) so the tip rests still on the target.
func (m *cursorModel) startClick(now time.Time, target Rect, scale float64, reduced bool) {
	if !m.shown || m.fading {
		m.shown, m.fading = true, false
		m.tip = rectCentre(target)
		m.travel, m.arrived = false, true
		m.scale = scale
		m.floatFrom = now
	}
	m.quiet(now)
	m.reduced = reduced
	if !reduced {
		m.press, m.pressStart = true, now
	}
	if target.Width() > 0 && target.Height() > 0 {
		m.outline, m.outlineRect, m.outlineStart = true, target, now
	}
	m.lastActive = now
}

// busy reports whether the tip must rest exactly on its spot: a travel,
// a press, an outline or a caret is playing.
func (m *cursorModel) busy() bool {
	return m.travel || m.press || m.outline || m.caret
}

// quiet starts the float's ease-out at now, from whatever it floats at,
// before something that wants the tip still begins.
func (m *cursorModel) quiet(now time.Time) {
	if !m.busy() {
		m.floatOutFrom = m.floatAmp(now)
		m.floatOutStart = now
	}
}

// floatAmp is the float's amplitude at now, in [0, 1].
func (m *cursorModel) floatAmp(now time.Time) float64 {
	if m.reduced || !m.shown {
		return 0
	}
	if m.busy() {
		if m.floatOutFrom <= 0 {
			return 0
		}
		return m.floatOutFrom * (1 - smoothstep(float64(now.Sub(m.floatOutStart))/float64(cursorFloatOut)))
	}
	return smoothstep(float64(now.Sub(m.floatFrom)) / float64(cursorFloatIn))
}

// floatAt is the float's offset of the tip at now.
func (m *cursorModel) floatAt(now time.Time) fpoint {
	return floatOffset(time.Duration(now.UnixNano()), m.floatAmp(now), m.scale)
}

// tipAt is the tip's position at now as shown (the float included),
// without advancing the model.
func (m *cursorModel) tipAt(now time.Time) fpoint {
	if m.travel {
		return m.path.at(m.path.progress(now.Sub(m.start)))
	}
	o := m.floatAt(now)
	return fpoint{X: m.tip.X + o.X, Y: m.tip.Y + o.Y}
}

// advance moves the model to now — finishing a travel, a press, an
// outline or a fade that ran out, starting the idle fade when it is due —
// and returns the frame to show.
func (m *cursorModel) advance(now time.Time) cursorFrame {
	var f cursorFrame
	f.Press = 1
	wasBusy := m.busy()
	if m.travel {
		el := now.Sub(m.start)
		t := m.path.progress(el)
		m.tip = m.path.at(t)
		if el >= m.path.arrive {
			m.arrived = true
		}
		if !m.reduced {
			f.Angle = m.path.tipAngle(el)
			if m.trail {
				f.Trail = trailPoints(m.path, el, m.scale)
			}
		}
		if t >= 1 {
			m.travel, m.arrived = false, true
		}
	}
	if m.press {
		el := now.Sub(m.pressStart)
		if el >= cursorPressDuration {
			m.press = false
		} else {
			f.Press = pressScale(el)
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
	if m.caret {
		el := now.Sub(m.caretStart)
		if el >= cursorCaretDuration {
			m.caret = false
		} else {
			f.Caret, f.CaretBar, f.CaretOp = true, m.caretBar, caretOpacity(el)
		}
	}
	if wasBusy || m.busy() {
		// The hold counts from the frame that saw the last of it.
		m.floatFrom = now.Add(cursorFloatHold)
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
	if m.shown {
		f.Cursor, f.Tip, f.Opacity = true, m.tipAt(now), op
	}
	f.Travelling = m.travel && !m.arrived
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
	return m.travel || m.press || m.outline || m.caret || m.fading
}

// floats reports whether a resting cursor floats: shown and not with
// Windows' animations off.
func (m *cursorModel) floats() bool { return m.shown && !m.reduced }

// nextWake is when the controller's timer should fire next: a frame
// interval while anything animates; while the cursor rests on screen, a
// float interval once its float may run (cursorFloatHold after the last
// Action), else the idle deadline; never (false) once it is hidden.
func (m *cursorModel) nextWake(now time.Time) (time.Duration, bool) {
	if m.animating() {
		return cursorFrameInterval, true
	}
	if !m.shown {
		return 0, false
	}
	d := m.lastActive.Add(cursorIdleDelay).Sub(now)
	if m.floats() {
		d = min(d, max(m.floatFrom.Sub(now), cursorFloatInterval))
	}
	return max(d, cursorFrameInterval), true
}

// visibleForTravel reports whether a Travel starting now would set off
// from the current spot (true) or from its origin's centre (false).
func (m *cursorModel) visibleForTravel() bool { return m.shown && !m.fading }
