package win

// The Agent Cursor's offline tests (headless ticket 03): the motion
// (easing, path, duration), the layout (pill placement and flips,
// ellipsizing, DPI scaling), the pixels (premultiply, compositing, the
// text blend, shapes), the timeline (ripple, outline, label reveal, idle
// fade) and the
// controller's waits with a fake view and fake UI threads. No window is
// created: NewAgentCursor itself is refused inside the test binary.

import (
	"errors"
	"image/color"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- Motion ---

func TestMinimumJerkEndpointsAndMonotonic(t *testing.T) {
	if minimumJerk(0) != 0 || minimumJerk(1) != 1 {
		t.Fatalf("minimumJerk endpoints = %v, %v; want exactly 0, 1", minimumJerk(0), minimumJerk(1))
	}
	if minimumJerk(-1) != 0 || minimumJerk(2) != 1 {
		t.Error("minimumJerk must clamp outside [0, 1]")
	}
	if got := minimumJerk(0.5); math.Abs(got-0.5) > 1e-12 {
		t.Errorf("minimumJerk(0.5) = %v, want 0.5 (symmetric ease-in-out)", got)
	}
	prev := 0.0
	for i := 1; i <= 1000; i++ {
		v := minimumJerk(float64(i) / 1000)
		if v < prev {
			t.Fatalf("minimumJerk decreases at t=%v", float64(i)/1000)
		}
		prev = v
	}
	// Ease in and out: slow at both ends compared with the middle.
	if minimumJerk(0.1) > 0.05 || 1-minimumJerk(0.9) > 0.05 {
		t.Error("minimumJerk should start and end slowly")
	}
}

func TestCursorPathEndpointsAreExact(t *testing.T) {
	from, to := fpoint{X: 113.5, Y: 907.25}, fpoint{X: 1801, Y: 64}
	for _, reduced := range []bool{false, true} {
		p := newCursorPath(from, to, Rect{}, 1, reduced)
		for _, tc := range []struct {
			t    float64
			want fpoint
		}{{0, from}, {-0.5, from}, {1, to}, {1.5, to}} {
			if got := p.at(tc.t); got != tc.want {
				t.Errorf("reduced %v: at(%v) = %+v, want exactly %+v", reduced, tc.t, got, tc.want)
			}
		}
		if p.progress(0) != 0 || p.progress(p.dur) != 1 || p.progress(2*p.dur) != 1 || p.progress(-time.Second) != 0 {
			t.Error("progress must map [0, dur] onto [0, 1] and clamp outside")
		}
	}
}

// TestCursorPathArcsLikeCua (cursor-look 01): cua's signature arc bows to
// the natural side — upward for a horizontal move either way — about 9 %
// of the distance at its apex, moves only forward until the target, then
// follows through past it by 1.8 % of the distance (at most 8 px) along
// the end tangent and settles back.
func TestCursorPathArcsLikeCua(t *testing.T) {
	for _, tc := range []struct {
		from, to fpoint
		up       bool // bows upward on the screen
	}{
		{fpoint{0, 500}, fpoint{1000, 500}, true},
		{fpoint{1000, 500}, fpoint{0, 500}, true},
		{fpoint{500, 900}, fpoint{40, 20}, false},
		{fpoint{10, 10}, fpoint{14, 300}, false},
		{fpoint{100, 100}, fpoint{300, 100}, true},
	} {
		p := newCursorPath(tc.from, tc.to, Rect{}, 1, false)
		dx, dy := tc.to.X-tc.from.X, tc.to.Y-tc.from.Y
		d := math.Hypot(dx, dy)
		prev, maxSide, apexY, maxPast := -1.0, 0.0, tc.from.Y, 0.0
		arrivedAt := -1.0
		for i := 0; i <= 2000; i++ {
			pt := p.at(float64(i) / 2000)
			along := ((pt.X-tc.from.X)*dx + (pt.Y-tc.from.Y)*dy) / d
			if arrivedAt < 0 && along < prev-1e-9 {
				t.Fatalf("%+v→%+v: the cursor moves backwards at t=%v before arriving", tc.from, tc.to, float64(i)/2000)
			}
			if arrivedAt < 0 && pt.dist(tc.to) <= cursorArrivalSlack {
				arrivedAt = float64(i) / 2000
			}
			prev = along
			maxPast = math.Max(maxPast, along-d)
			side := math.Abs((pt.X-tc.from.X)*dy-(pt.Y-tc.from.Y)*dx) / d
			maxSide = math.Max(maxSide, side)
			apexY = math.Min(apexY, pt.Y)
		}
		if math.Abs(maxSide/d-0.09) > 0.006 {
			t.Errorf("%+v→%+v: the apex is %.1f px off the chord, want about 9 %% of %.0f", tc.from, tc.to, maxSide, d)
		}
		if tc.up && tc.from.Y-apexY < 0.08*d {
			t.Errorf("%+v→%+v: a horizontal move bows upward, its highest point is y=%.1f", tc.from, tc.to, apexY)
		}
		want := math.Min(cursorOvershoot*p.length, cursorOvershootMax)
		if maxPast < 0.8*want || maxPast > want+0.5 {
			t.Errorf("%+v→%+v: it follows through %.2f px past the target, want about %.2f", tc.from, tc.to, maxPast, want)
		}
		// The arrival (the tip within a pixel) is what Travel waits for.
		if got := p.progress(p.arrive); math.Abs(got-arrivedAt) > 0.002 || p.arrive >= p.dur {
			t.Errorf("%+v→%+v: arrive at %v (t=%.3f), sampled %.3f; dur %v", tc.from, tc.to, p.arrive, got, arrivedAt, p.dur)
		}
	}
	// The follow-through is capped at 8 px on a long move, scaled with the DPI.
	p := newCursorPath(fpoint{0, 0}, fpoint{3000, 0}, Rect{}, 2, false)
	if got := p.over * p.length; math.Abs(got-cursorOvershootMax*2) > 1e-9 {
		t.Errorf("a 3000 px move at 200 %% follows through %v px, want the 16 px cap", got)
	}
	// Reduced motion: straight, minimum-jerk, no follow-through, 120 ms.
	r := newCursorPath(fpoint{0, 0}, fpoint{1000, 500}, Rect{}, 1, true)
	if r.dur != cursorReducedTravel || r.turn != 0 {
		t.Errorf("a reduced travel takes %v and turns %v, want 120 ms and no turn", r.dur, r.turn)
	}
	for i := 0; i <= 100; i++ {
		pt := r.at(float64(i) / 100)
		if s := minimumJerk(float64(i) / 100); math.Abs(pt.X-1000*s) > 1e-9 || math.Abs(pt.Y-500*s) > 1e-9 {
			t.Fatalf("a reduced travel is straight and minimum-jerk: %+v at %d %%", pt, i)
		}
	}
	// The ease is the in-out sine, with zero speed at both ends.
	if sineInOut(0) != 0 || sineInOut(1) != 1 || math.Abs(sineInOut(0.5)-0.5) > 1e-12 || sineInOut(0.05) > 0.01 {
		t.Error("sineInOut must be 0.5 − 0.5·cos(πt)")
	}
	if overshootBump(0) != 0 || overshootBump(1) != 0 || math.Abs(overshootBump(cursorOvershootAt)-1) > 1e-9 {
		t.Errorf("the bump is 0 at both ends and 1 at its peak, got %v", overshootBump(cursorOvershootAt))
	}
}

// TestCursorTravelDurationIsCuasFitts (cursor-look 01): max(600,
// 1.1·clamp(150 + 120·log2(D/W + 1), 300, 1000)) ms, in logical pixels.
func TestCursorTravelDurationIsCuasFitts(t *testing.T) {
	o := fpoint{100, 100}
	at := func(d, side float64) time.Duration {
		return cursorTravelDuration(o, fpoint{100 + d, 100}, side, 1, false)
	}
	ms := func(v float64) time.Duration { return time.Duration(v * float64(time.Millisecond)) }
	near := func(got, want time.Duration) bool { return (got - want).Abs() <= time.Millisecond }

	if at(0, 40) != 0 || at(1, 40) != 0 {
		t.Error("a hop under two pixels has nothing to animate")
	}
	if got := at(100, 40); got != cursorTravelFloor {
		t.Errorf("100 px to a 40 px button takes %v, want the %v floor", got, cursorTravelFloor)
	}
	if got, want := at(400, 40), ms(1.1*(150+120*math.Log2(11))); !near(got, want) || got < 615*time.Millisecond || got > 625*time.Millisecond {
		t.Errorf("400 px to a 40 px button takes %v, want %v (about 620 ms)", got, want)
	}
	if got, want := at(1500, 24), ms(1.1*(150+120*math.Log2(1500.0/24+1))); !near(got, want) {
		t.Errorf("1500 px to a 24 px button takes %v, want %v (about 955 ms)", got, want)
	}
	if got := at(1500, 0); !near(got, at(1500, cursorTargetDefault)) {
		t.Errorf("no target size uses a 24 px box: %v", got)
	}
	if got := at(1500, 1); !near(got, at(1500, cursorTargetMin)) {
		t.Errorf("a 1 px target counts as 4 px: %v", got)
	}
	if cursorTravelMax != 1100*time.Millisecond {
		t.Errorf("the longest travel is %v, want 1.1 s", cursorTravelMax)
	}
	if got := at(100000, 4); got != cursorTravelMax {
		t.Errorf("a huge hop takes %v, want the 1.1 s cap", got)
	}
	prev := time.Duration(0)
	for d := 2.0; d < 50000; d *= 1.3 {
		got := at(d, 24)
		if got < prev || got < cursorTravelFloor || got > cursorTravelMax {
			t.Fatalf("duration %v at %v px: must be monotonic and within [%v, %v]", got, d, cursorTravelFloor, cursorTravelMax)
		}
		prev = got
	}
	// Logical pixels: on a 200 % monitor the same hop to the same button
	// is the same distance in widths, so it takes as long.
	if hi, lo := cursorTravelDuration(o, fpoint{2100, 100}, 48, 2, false), at(1000, 24); !near(hi, lo) {
		t.Errorf("at scale 2 the hop takes %v, at scale 1 %v", hi, lo)
	}
	if got := cursorTravelDuration(o, fpoint{2100, 100}, 48, 1, true); got != cursorReducedTravel {
		t.Errorf("with animations off a travel takes %v, want %v", got, cursorReducedTravel)
	}
}

// TestTipTurnsAlongThePathAndLandsUpright (cursor-look 04): the arrow
// turns toward where it is going, eases in and out, and is exactly upright
// at the arrival and after it; short hops and reduced motion never turn.
func TestTipTurnsAlongThePathAndLandsUpright(t *testing.T) {
	right := newCursorPath(fpoint{0, 500}, fpoint{900, 500}, Rect{}, 1, false)
	// Rightward, the tip (whose own axis, between its wings, points up and
	// a little left) turns some 105–110° clockwise.
	if want := -cursorTipAxis; math.Abs(right.turn-want) > 1e-9 || want < 100*math.Pi/180 || want > 112*math.Pi/180 {
		t.Errorf("a rightward travel turns %.1f°, want %.1f°", right.turn*180/math.Pi, want*180/math.Pi)
	}
	if right.tipAngle(0) != 0 || right.tipAngle(right.arrive) != 0 || right.tipAngle(right.dur) != 0 {
		t.Error("the arrow is upright at the start, at the arrival and after it")
	}
	peak := 0.0
	prev := 0.0
	for el := time.Duration(0); el <= right.arrive; el += time.Millisecond {
		a := right.tipAngle(el)
		if math.Abs(a-prev) > 0.03 {
			t.Fatalf("the angle jumps by %.3f rad at %v", a-prev, el)
		}
		peak = math.Max(peak, a)
		prev = a
	}
	if peak < 0.8*right.turn || peak > right.turn+0.3 {
		t.Errorf("a long rightward travel turns at most %.1f°, want near %.1f°", peak*180/math.Pi, right.turn*180/math.Pi)
	}
	// Straight down, the turn is near half a turn: never wraps mid-travel.
	down := newCursorPath(fpoint{500, 0}, fpoint{500, 900}, Rect{}, 1, false)
	prev = 0
	for el := time.Duration(0); el <= down.arrive; el += time.Millisecond {
		a := down.tipAngle(el)
		if math.Abs(a-prev) > 0.05 {
			t.Fatalf("going down the angle jumps from %.2f to %.2f at %v", prev, a, el)
		}
		prev = a
	}
	if short := newCursorPath(fpoint{0, 0}, fpoint{30, 0}, Rect{}, 1, false); short.turn != 0 || short.tipAngle(short.dur/2) != 0 {
		t.Error("a hop under 40 px never turns")
	}
	if hi := newCursorPath(fpoint{0, 0}, fpoint{70, 0}, Rect{}, 2, false); hi.turn != 0 {
		t.Error("the 40 px is logical: 70 physical px at 200 % is a short hop")
	}
	if wrapAngle(3*math.Pi/2) != -math.Pi/2 || wrapAngle(-3*math.Pi/2) != math.Pi/2 || wrapAngle(0.5) != 0.5 {
		t.Error("wrapAngle wraps into (−π, π]")
	}
}

// TestPressSqueezesAndSettles (cursor-look 05): 1 → 0.93 → 1.03 → 1
// over 667 ms, at 35 % and 60 % of it.
func TestPressSqueezesAndSettles(t *testing.T) {
	at := func(f float64) float64 {
		return pressScale(time.Duration(f * float64(cursorPressDuration)))
	}
	if pressScale(0) != 1 || pressScale(cursorPressDuration) != 1 || pressScale(-time.Second) != 1 {
		t.Error("the press starts and ends at 1")
	}
	if math.Abs(at(0.35)-0.93) > 1e-6 || math.Abs(at(0.6)-1.03) > 1e-6 {
		t.Errorf("the press is %v at 35 %% and %v at 60 %%, want 0.93 and 1.03", at(0.35), at(0.6))
	}
	lo, hi := 2.0, 0.0
	for el := time.Duration(0); el <= cursorPressDuration; el += time.Millisecond {
		v := pressScale(el)
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	if math.Abs(lo-0.93) > 1e-6 || math.Abs(hi-cursorPressMax) > 1e-6 {
		t.Errorf("the press ranges %v–%v, want 0.93–1.03", lo, hi)
	}
}

// TestFloatIsZeroForEveryAction (cursor-look 06): the resting cursor
// sways and bobs on a 4 s loop, but never while travelling or acting, for
// half a second after, and not at all with reduced motion.
func TestFloatIsZeroForEveryAction(t *testing.T) {
	if floatOffset(time.Second, 0, 1) != (fpoint{}) {
		t.Error("at amplitude 0 the float is exactly zero")
	}
	o := floatOffset(0, 1, 2) // phase 0: sin 0, cos 1
	if math.Abs(o.X) > 1e-9 || math.Abs(o.Y-(cursorFloatBob-cursorFloatSway)*2) > 1e-9 {
		t.Errorf("the float at phase 0 = %+v", o)
	}
	o = floatOffset(cursorFloatPeriod/4, 1, 1)
	if math.Abs(o.X-cursorFloatSway) > 1e-9 || math.Abs(o.Y+cursorFloatSway) > 1e-9 {
		t.Errorf("a quarter loop in the float = %+v, want the full sway", o)
	}

	var m cursorModel
	target := Rect{Left: 400, Top: 300, Right: 440, Bottom: 330}
	m.startTravel(t0, Rect{Right: 1000, Bottom: 800}, target, 1, false)
	centre := rectCentre(target)
	for el := time.Duration(0); el < m.path.dur; el += 4 * time.Millisecond {
		m.advance(t0.Add(el))
	}
	end := t0.Add(m.path.dur)
	if f := m.advance(end); f.Tip != centre {
		t.Errorf("the travel lands exactly on the target: %+v", f.Tip)
	}
	if f := m.advance(end.Add(cursorFloatHold - 10*time.Millisecond)); f.Tip != centre {
		t.Errorf("the float holds zero for %v after the travel: %+v", cursorFloatHold, f.Tip)
	}
	settled := end.Add(cursorFloatHold + cursorFloatIn + time.Second)
	if f := m.advance(settled); f.Tip == centre {
		t.Error("a resting cursor floats")
	}
	if d, ok := m.nextWake(settled); !ok || d != cursorFloatInterval {
		t.Errorf("a floating cursor wakes every %v, got %v", cursorFloatInterval, d)
	}
	// A click while floating: it eases out, then rests exactly on the target.
	m.startClick(settled, target, 1, false)
	if f := m.advance(settled.Add(cursorFloatOut)); f.Tip != centre {
		t.Errorf("after the float's ease-out the click rests exactly on the target: %+v", f.Tip)
	}
	for el := cursorFloatOut; el < cursorOutlineDuration; el += 4 * time.Millisecond {
		if f := m.advance(settled.Add(el)); f.Tip != centre {
			t.Fatalf("%v into the click the tip moved: %+v", el, f.Tip)
		}
	}
	// A travel from a floating cursor sets off from where it shows.
	var n cursorModel
	n.startTravel(t0, Rect{Right: 1000, Bottom: 800}, target, 1, false)
	n.advance(t0.Add(n.path.dur))
	later := t0.Add(n.path.dur + 3*time.Second)
	shown := n.advance(later).Tip
	n.startTravel(later, Rect{}, Rect{Left: 10, Top: 10, Right: 20, Bottom: 20}, 1, false)
	if f := n.advance(later); f.Tip != shown {
		t.Errorf("the travel sets off from the floated spot %+v, got %+v", shown, f.Tip)
	}
	// Reduced motion: no float, no press, and the rest of the wake is idle.
	var r cursorModel
	r.startClick(t0, target, 1, true)
	if f := r.advance(t0.Add(100 * time.Millisecond)); f.Press != 1 {
		t.Errorf("with animations off the click does not press: %v", f.Press)
	}
	r.advance(t0.Add(cursorOutlineDuration))
	if f := r.advance(t0.Add(5 * time.Second)); f.Tip != centre {
		t.Errorf("with animations off the cursor does not float: %+v", f.Tip)
	}
	if d, _ := r.nextWake(t0.Add(5 * time.Second)); d != cursorIdleDelay-5*time.Second {
		t.Errorf("with animations off the resting cursor sleeps until the idle fade, got %v", d)
	}
}

// TestTrailFollowsBehindTheTip (cursor-look 06): the last 0.18 s of path,
// cut 16 px short of the tip.
func TestTrailFollowsBehindTheTip(t *testing.T) {
	p := newCursorPath(fpoint{0, 500}, fpoint{1200, 500}, Rect{}, 1, false)
	el := p.dur / 2
	pts := trailPoints(p, el, 1)
	if len(pts) < 2 {
		t.Fatal("mid-travel the trail has points")
	}
	tip := p.at(p.progress(el))
	if d := pts[len(pts)-1].dist(tip); math.Abs(d-cursorTrailBehind) > 0.5 {
		t.Errorf("the trail's head is %.2f px from the tip, want %v", d, cursorTrailBehind)
	}
	if tail := p.at(p.progress(el - cursorTrailSpan)); pts[0] != tail {
		t.Errorf("the trail's tail = %+v, want the tip of %v ago %+v", pts[0], cursorTrailSpan, tail)
	}
	if trailPoints(p, 0, 1) != nil || trailPoints(p, p.dur+time.Second, 1) != nil {
		t.Error("a still cursor has no trail")
	}
	var m cursorModel
	m.trail = true
	m.startTravel(t0, Rect{}, Rect{Left: 1190, Top: 490, Right: 1210, Bottom: 510}, 1, false)
	if f := m.advance(t0.Add(m.path.dur / 2)); len(f.Trail) < 2 {
		t.Error("with the trail on the frame carries it")
	}
	var off cursorModel
	off.startTravel(t0, Rect{}, Rect{Left: 1190, Top: 490, Right: 1210, Bottom: 510}, 1, false)
	if f := off.advance(t0.Add(off.path.dur / 2)); f.Trail != nil {
		t.Error("the trail is off by default")
	}
}

// --- Layout ---

func TestPlacePillBesideTheTipAndFlipsAtWorkAreaEdges(t *testing.T) {
	work := Rect{Left: 0, Top: 0, Right: 1920, Bottom: 1040}
	const w, h = 200, 24
	ox, oy := scaled(cursorPillOffsetX, 1), scaled(cursorPillOffsetY, 1)
	if ox != 14 || oy != 30 {
		t.Fatalf("pill offsets = %d, %d; want 14, 30, just past the arrow's tail", ox, oy)
	}

	got := placePill(fpoint{500, 400}, w, h, work, 1)
	if want := (Rect{Left: 500 + ox, Top: 400 + oy, Right: 500 + ox + w, Bottom: 400 + oy + h}); got != want {
		t.Errorf("default placement = %+v, want below-right %+v", got, want)
	}

	right := placePill(fpoint{1850, 400}, w, h, work, 1)
	if right.Right != 1850-ox || right.Top != 400+oy {
		t.Errorf("near the right edge = %+v, want flipped left of the tip, still below", right)
	}

	bottom := placePill(fpoint{500, 1030}, w, h, work, 1)
	if bottom.Bottom != 1030-oy || bottom.Left != 500+ox {
		t.Errorf("near the bottom = %+v, want flipped above the tip, still right", bottom)
	}

	corner := placePill(fpoint{1900, 1035}, w, h, work, 1)
	if corner.Right != 1900-ox || corner.Bottom != 1035-oy {
		t.Errorf("in the corner = %+v, want flipped left and up", corner)
	}

	// Every placement stays inside the work area, on any monitor (a
	// secondary monitor at negative coordinates included).
	second := Rect{Left: -1280, Top: -200, Right: 0, Bottom: 824}
	for _, tip := range []fpoint{{-1280, -200}, {-1, 823}, {-640, 300}, {-5, -199}} {
		r := placePill(tip, w, h, second, 1)
		if !rectWithin(second, r) || r.Width() != w || r.Height() != h {
			t.Errorf("tip %+v: pill %+v leaves the work area %+v", tip, r, second)
		}
	}
	// A work area narrower than the pill keeps the pill's left edge on it.
	narrow := placePill(fpoint{50, 50}, w, h, Rect{Right: 120, Bottom: 600}, 1)
	if narrow.Left != 0 {
		t.Errorf("narrow work area: pill = %+v, want its left edge on the area's", narrow)
	}
}

func TestPlacePillScalesWithDPI(t *testing.T) {
	work := Rect{Right: 3840, Bottom: 2100}
	one := placePill(fpoint{1000, 800}, 100, 24, work, 1)
	two := placePill(fpoint{1000, 800}, 100, 24, work, 2)
	if two.Left-1000 != 2*(one.Left-1000) || two.Top-800 != 2*(one.Top-800) {
		t.Errorf("at 192 DPI the offsets must double: %+v vs %+v", two, one)
	}
	if cursorScale(96) != 1 || cursorScale(144) != 1.5 || cursorScale(0) != 1 {
		t.Error("cursorScale must be dpi/96 (96 when unknown)")
	}
}

func TestPillSizeCapsAndScales(t *testing.T) {
	w1, h1 := pillSize(100, 1)
	w2, h2 := pillSize(200, 2)
	if w1 != 100+2*11 || h1 != 26 {
		t.Errorf("pill at scale 1 = %dx%d, want text plus 11 each end × 26", w1, h1)
	}
	if w2 != 2*w1 || h2 != 2*h1 {
		t.Errorf("pill at scale 2 = %dx%d, want double %dx%d", w2, h2, w1, h1)
	}
	if w, _ := pillSize(100000, 1); w != 360 {
		t.Errorf("a huge label's pill is %d wide, want the 360 cap", w)
	}
	b1, b2 := arrowBounds(1), arrowBounds(2)
	if b2.Height() < 2*b1.Height()-6 || b2.Height() > 2*b1.Height()+2 {
		t.Errorf("the arrow should scale with the DPI: %+v at 1, %+v at 2", b1, b2)
	}
}

// TestArrowShapeAndBounds (cursor-look 02): cua's upright arrow, 42 px
// for its 128-unit canvas: the tip first, the body about 20×25 px below
// and right of it, the left edge vertical; the bounds hold it turned any
// way, pressed, with its glow and halo.
func TestArrowShapeAndBounds(t *testing.T) {
	for _, scale := range []float64{1, 1.5, 2} {
		pts := arrowPoints(scale)
		if pts[0] != (fpoint{}) {
			t.Errorf("scale %v: the arrow's first point is its tip, got %+v", scale, pts[0])
		}
		if len(pts) != 8*cursorArrowFlat {
			t.Errorf("scale %v: %d points, want 8 beziers of %d", scale, len(pts), cursorArrowFlat)
		}
		minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
		for _, p := range pts {
			minX, minY = math.Min(minX, p.X), math.Min(minY, p.Y)
			maxX, maxY = math.Max(maxX, p.X), math.Max(maxY, p.Y)
		}
		// cua's 19.9×25.3, narrowed and made taller about the tip.
		if w, h := (maxX-minX)/scale, (maxY-minY)/scale; math.Abs(w-19.9*cursorArrowNarrow) > 0.2 || math.Abs(h-25.3*cursorArrowTall) > 0.2 {
			t.Errorf("scale %v: the body spans %.1f×%.1f logical px, want about 16.3×29.9", scale, w, h)
		}
		// The tip is the sharpest corner: about 43° between its wings
		// (cua's is about 56°), like the system arrow's 45°.
		wl, wr := arrowCanvas(arrowWings[0]), arrowCanvas(arrowWings[1])
		if a := math.Abs(math.Atan2(wl.Y, wl.X)-math.Atan2(wr.Y, wr.X)) * 180 / math.Pi; a < 41 || a > 45 {
			t.Errorf("the tip's angle is %.1f°, want about 43°", a)
		}
		// Upright: the left edge (the second bezier) is vertical.
		a, b := pts[cursorArrowFlat], pts[2*cursorArrowFlat-1]
		if math.Abs(a.X-b.X) > 0.2*scale || b.Y-a.Y < 15*scale {
			t.Errorf("scale %v: the left edge runs %+v→%+v, want vertical", scale, a, b)
		}
		r := float64(arrowReach(scale))
		bd := arrowBounds(scale)
		if float64(-bd.Left) != r || float64(bd.Right) != r+1 || bd.Width() != bd.Height() {
			t.Errorf("scale %v: bounds %+v, want a square of reach %v about the tip", scale, bd, r)
		}
		for _, p := range pts {
			if math.Hypot(p.X, p.Y)*cursorPressMax+arrowGlowReach(scale) > r {
				t.Fatalf("scale %v: %+v with its glow leaves the reach %v", scale, p, r)
			}
		}
		if halo := (math.Hypot(cursorArrowHaloX, cursorArrowHaloY) + cursorArrowHaloR) * scale * cursorPressMax; halo > r {
			t.Errorf("scale %v: the halo reaches %v, past %v", scale, halo, r)
		}
	}
}

func TestEllipsizeFitsTheWidth(t *testing.T) {
	measure := func(s string) int32 { return int32(7 * len([]rune(s))) } // 7 px per rune
	if got := ellipsize(`click "Save"`, 200, measure); got != `click "Save"` {
		t.Errorf("a label that fits changed: %q", got)
	}
	long := `set-value "A very long field name that will not fit in the card"`
	got := ellipsize(long, 140, measure) // 20 runes fit, the ellipsis included
	if want := `set-value "A very l…`; got != want {
		t.Errorf("ellipsize = %q, want the longest fitting prefix %q", got, want)
	}
	if measure(got) > 140 {
		t.Errorf("ellipsize = %q measures %d px, over the 140 px limit", got, measure(got))
	}
	// No trailing space before the ellipsis.
	if got := ellipsize("ab cdefgh", 7*4, measure); got != "ab…" {
		t.Errorf("ellipsize trims trailing spaces: got %q, want %q", got, "ab…")
	}
	if got := ellipsize("abc", 3, measure); got != "" {
		t.Errorf("when not even … fits the result is empty, got %q", got)
	}
}

func TestSanitizeLabelMakesOneLine(t *testing.T) {
	if got := sanitizeLabel("  click\n\t\"Save\r\nAs\"  "); got != `click "Save As"` {
		t.Errorf("sanitizeLabel = %q", got)
	}
	if got := sanitizeLabel("a\x00b"); got != "a b" {
		t.Errorf("control characters become spaces: %q", got)
	}
	if got := sanitizeLabel(strings.Repeat("x", 1000)); len([]rune(got)) != cursorLabelMaxRunes {
		t.Errorf("a huge label is cut to %d runes, got %d", cursorLabelMaxRunes, len([]rune(got)))
	}
}

// TestNamedAgentsPillHasTwoLines (cursor-look 09, ADR 0018 Decision 8):
// a named Agent's pill shows its name, smaller, over the action; each line
// is cut to the pill's width on its own; the place beside the tip and the
// width cap are the one-line pill's; the default Agent's pill is one line
// as before, and so is a name with no action.
func TestNamedAgentsPillHasTwoLines(t *testing.T) {
	u := &cursorUI{view: &fakeCursorView{}}
	work := Rect{Right: 1920, Bottom: 1040}
	tip := fpoint{X: 500, Y: 400}
	for _, scale := range []float64{1, 1.5, 2} {
		one := u.lookFor(tip, "", `click "Save"`, work, scale)
		two := u.lookFor(tip, "reviewer", `click "Save"`, work, scale)
		if one.Name != "" || one.Pill.Height() != scaled(cursorPillHeight, scale) {
			t.Errorf("scale %v: the default Agent's pill = %+v, want one line", scale, one)
		}
		if two.Name != "reviewer" || two.Label != `click "Save"` || two.Pill.Height() != pillHeight2(scale) {
			t.Errorf("scale %v: a named Agent's look = %+v, want its name over the action", scale, two)
		}
		if two.Pill.Left != one.Pill.Left || two.Pill.Top != one.Pill.Top {
			t.Errorf("scale %v: the two-line pill moved: %+v vs %+v", scale, two.Pill, one.Pill)
		}
		l := layoutCursorWindow(two)
		if l.Name.Height() <= 0 || l.Name.Bottom != l.Text.Top || !rectWithin(l.Pill, l.Name) || !rectWithin(l.Pill, l.Text) {
			t.Errorf("scale %v: name %+v over text %+v inside pill %+v", scale, l.Name, l.Text, l.Pill)
		}
		if pillRadius(two.Pill, scale) != pillRadius(one.Pill, scale) {
			t.Errorf("scale %v: the two-line pill's corners differ from one line's", scale)
		}
	}
	// Each line is cut on its own: a long name to 24 characters, a long
	// action to the pill's width.
	long := strings.Repeat("n", 300)
	l := u.lookFor(tip, long, `click "Save"`, work, 1)
	if !strings.HasSuffix(l.Name, "…") || l.Label != `click "Save"` || l.Pill.Width() > scaled(cursorPillMaxWidth, 1) {
		t.Errorf("a long name is cut, the action kept: %q / %q, width %d", l.Name, l.Label, l.Pill.Width())
	}
	l = u.lookFor(tip, "reviewer", long, work, 1)
	if l.Name != "reviewer" || !strings.HasSuffix(l.Label, "…") {
		t.Errorf("a long action is cut, the name kept: %q / %q", l.Name, l.Label)
	}
	// A name with no action is the one line.
	if l := u.lookFor(tip, "painter", "", work, 1); l.Name != "" || l.Label != "painter" {
		t.Errorf("a name alone = %+v, want it as the one line", l)
	}
	// Names are capped at 24 characters, the last an ellipsis, before they
	// are measured.
	l = u.lookFor(tip, "release-notes-writer-for-the-quarterly-report", `click "Save"`, work, 1)
	if r := []rune(l.Name); len(r) != cursorNameMaxRunes || r[len(r)-1] != '…' {
		t.Errorf("a long name = %q, want 24 characters ending in …", l.Name)
	}
	if capName("reviewer") != "reviewer" || capName(strings.Repeat("a", 24)) != strings.Repeat("a", 24) {
		t.Error("a name of 24 characters or fewer is kept whole")
	}
}

// TestPillTextColorHasTheBetterContrast (cursor-look 10): the text on the
// flat pill is white or near-black, whichever contrasts more with the
// Agent's colour, at least 4.5:1 (WCAG AA) on the default and every
// palette colour but the saturated reds, where white still leads.
func TestPillTextColorHasTheBetterContrast(t *testing.T) {
	for _, c := range append([]color.RGBA{cursorAccent}, CursorPalette...) {
		ink := pillTextColor(c)
		other := cursorTextColor
		if ink == cursorTextColor {
			other = cursorInkColor
		}
		if contrast(ink, c) < contrast(other, c) {
			t.Errorf("%v: text %v has the lower contrast", c, ink)
		}
		if contrast(ink, c) < 4.5 {
			t.Logf("%v: text %v at %.1f:1", c, ink, contrast(ink, c))
		}
	}
	if pillTextColor(cursorAccent) != cursorInkColor {
		t.Error("the default pastel blue takes near-black text")
	}
	if pillTextColor(color.RGBA{R: 0x1A, G: 0x40, B: 0x9A, A: 0xFF}) != cursorTextColor {
		t.Error("a dark blue takes white text")
	}
	if c := contrast(cursorTextColor, color.RGBA{A: 0xFF}); math.Abs(c-21) > 0.01 {
		t.Errorf("white on black is %v:1, want 21:1", c)
	}
}

func TestLayoutCursorWindowHoldsDartAndPill(t *testing.T) {
	for _, scale := range []float64{1, 1.5, 2} {
		w, h := pillSize(150, scale)
		pill := placePill(fpoint{800, 500}, w, h, Rect{Right: 1920, Bottom: 1080}, scale)
		look := cursorLook{Scale: scale, Label: "click", Pill: Rect{
			Left: pill.Left - 800, Top: pill.Top - 500, Right: pill.Right - 800, Bottom: pill.Bottom - 500,
		}}
		l := layoutCursorWindow(look)
		win := Rect{Right: l.W, Bottom: l.H}
		// The window holds the arrow around the tip, the pill and its shadow.
		ab := arrowBounds(scale)
		dart := Rect{
			Left: int32(l.Tip.X) + ab.Left, Top: int32(l.Tip.Y) + ab.Top,
			Right: int32(l.Tip.X) + ab.Right, Bottom: int32(l.Tip.Y) + ab.Bottom,
		}
		if !rectWithin(win, dart) || !rectWithin(win, pillShadowBounds(l.Pill, scale)) {
			t.Errorf("scale %v: window %+v must hold the dart %+v and the pill %+v with its shadow", scale, win, dart, l.Pill)
		}
		if l.Pill.Width() != w || l.Pill.Height() != h {
			t.Errorf("scale %v: pill %+v, want %dx%d", scale, l.Pill, w, h)
		}
		// Window origin + window-relative tip = the tip.
		if float64(-l.OriginX) != l.Tip.X || float64(-l.OriginY) != l.Tip.Y {
			t.Errorf("scale %v: origin (%d,%d) and tip %+v disagree", scale, l.OriginX, l.OriginY, l.Tip)
		}
		// The text is padded inside the pill: its ends by the padding, top
		// and bottom by the inset.
		pad, inset := scaled(cursorPillPadX, scale), scaled(cursorPillTextInset, scale)
		if want := (Rect{Left: l.Pill.Left + pad, Top: l.Pill.Top + inset, Right: l.Pill.Right - pad, Bottom: l.Pill.Bottom - inset}); l.Text != want {
			t.Errorf("scale %v: text %+v, want %+v inside pill %+v", scale, l.Text, want, l.Pill)
		}
	}
	// No label: the window is just the arrow.
	bare := layoutCursorWindow(cursorLook{Scale: 1})
	if ab := arrowBounds(1); bare.W != ab.Width() || bare.H != ab.Height() || bare.Pill != (Rect{}) {
		t.Errorf("a cursor without a label = %+v, want only the arrow %+v", bare, ab)
	}
}

func TestOutlineWindowRectClipsToTheScreen(t *testing.T) {
	screen := Rect{Left: -1920, Top: 0, Right: 1920, Bottom: 1080}
	target := Rect{Left: 100, Top: 100, Right: 300, Bottom: 140}
	// The window holds the highlight at its largest pop: the padded box
	// grown 8 % about the centre, and the grown glow.
	grow := func(half float64) int32 {
		return int32(math.Ceil((half+cursorHighlightPad)*1.08-half+outlineGlowReach(1)*1.08)) + 2
	}
	gx, gy := grow(100), grow(20)
	got := outlineWindowRect(target, screen, 1)
	if want := (Rect{Left: 100 - gx, Top: 100 - gy, Right: 300 + gx, Bottom: 140 + gy}); got != want {
		t.Errorf("outline window = %+v, want %+v", got, want)
	}
	box, _, _ := outlineShape(Rect{Left: target.Left - got.Left, Top: target.Top - got.Top,
		Right: target.Right - got.Left, Bottom: target.Bottom - got.Top}, 1, cursorHighlightPopMax, cursorAccent)
	if !rectWithin(Rect{Right: got.Width(), Bottom: got.Height()}, box) {
		t.Errorf("the largest pop's box %+v leaves the window %+v", box, got)
	}
	if got := outlineWindowRect(Rect{Left: -5000, Top: -100, Right: 5000, Bottom: 5000}, screen, 1); got != screen {
		t.Errorf("a huge element's outline = %+v, want clipped to the screen %+v", got, screen)
	}
	if got := outlineWindowRect(Rect{Left: 5000, Top: 0, Right: 5100, Bottom: 10}, screen, 1); got != (Rect{}) {
		t.Errorf("an off-screen element's outline = %+v, want none", got)
	}
}

// --- Pixels ---

func TestPremultiply(t *testing.T) {
	c := color.RGBA{R: 0x10, G: 0xA3, B: 0x7F, A: 0xFF}
	if got := premultiply(c, 1); got != 0xFF10A37F {
		t.Errorf("opaque = %#08x, want 0xFF10A37F (BGRA as 0xAARRGGBB)", got)
	}
	if got := premultiply(c, 0); got != 0 {
		t.Errorf("transparent = %#08x, want 0", got)
	}
	half := premultiply(color.RGBA{R: 200, G: 100, B: 50, A: 255}, 0.5)
	if a, r, g, b := half>>24, half>>16&0xFF, half>>8&0xFF, half&0xFF; a != 128 || r != 100 || g != 50 || b != 25 {
		t.Errorf("half = a%d r%d g%d b%d, want 128/100/50/25", a, r, g, b)
	}
	// The colour's own alpha multiplies in.
	if got := premultiply(color.RGBA{R: 255, A: 128}, 1); got>>24 != 128 || got>>16&0xFF != 128 {
		t.Errorf("premultiply honours c.A: %#08x", got)
	}
}

func TestOverComposite(t *testing.T) {
	dst := uint32(0xFF0000FF) // opaque blue
	if over(dst, 0xFF00FF00) != 0xFF00FF00 {
		t.Error("an opaque source replaces")
	}
	if over(dst, 0) != dst {
		t.Error("a transparent source keeps the destination")
	}
	got := over(dst, premultiply(color.RGBA{R: 255, A: 255}, 0.5))
	if a, r, b := got>>24, got>>16&0xFF, got&0xFF; a != 255 || r < 126 || r > 129 || b < 126 || b > 129 {
		t.Errorf("half red over blue = %#08x, want opaque, half red, half blue", got)
	}
	if over(0, 0x80400000) != 0x80400000 {
		t.Error("over transparent keeps the source")
	}
}

func TestDimPixelAndShadowStrength(t *testing.T) {
	p := premultiply(color.RGBA{R: 200, G: 100, B: 50, A: 255}, 1)
	if dimPixel(p, 1) != p || dimPixel(p, 0) != 0 {
		t.Error("dimming by 1 keeps the pixel, by 0 clears it")
	}
	half := dimPixel(p, 0.5)
	if a, r, g, b := half>>24, half>>16&0xFF, half>>8&0xFF, half&0xFF; a != 128 || r != 100 || g != 50 || b != 25 {
		t.Errorf("half = a%d r%d g%d b%d, want 128/100/50/25 (premultiplied, all channels)", a, r, g, b)
	}
	if !validPremultiplied(dimPixel(premultiply(cursorAccent, 0.7), 0.33)) {
		t.Error("a dimmed pixel stays premultiplied")
	}
	// The shadow is full a blur inside, half at the edge, gone a blur out.
	if shadowStrength(-2.5, 2.5) != 1 || shadowStrength(0, 2.5) != 0.5 || shadowStrength(2.5, 2.5) != 0 {
		t.Errorf("shadowStrength = %v, %v, %v; want 1, 0.5, 0",
			shadowStrength(-2.5, 2.5), shadowStrength(0, 2.5), shadowStrength(2.5, 2.5))
	}
	prev := 2.0
	for d := -5.0; d <= 5; d += 0.1 {
		v := shadowStrength(d, 2.5)
		if v > prev || v < 0 || v > 1 {
			t.Fatalf("shadowStrength(%v) = %v: must fall monotonically within [0, 1]", d, v)
		}
		prev = v
	}
}

func TestDrawPillBodyAndShadow(t *testing.T) {
	const w, h = 140, 50
	s := &surface{w: w, h: h, pix: make([]uint32, w*h)}
	pill := Rect{Left: 10, Top: 10, Right: 130, Bottom: 36} // 26 tall, like scale 1
	s.drawPill(pill, 1, cursorAccent)
	at := func(x, y int) uint32 { return s.pix[y*w+x] }
	// The body is flat and opaque in the Agent's colour, edge to edge: no
	// gradient, no rim (cursor-look 10).
	for _, xy := range [][2]int{{10, 23}, {70, 10}, {70, 23}, {70, 35}, {129, 23}} {
		if p := at(xy[0], xy[1]); p != premultiply(cursorAccent, 1) {
			t.Errorf("the pill at %v = %#08x, want the flat accent", xy, p)
		}
	}
	// Below the pill a soft black shadow falls, above it almost none.
	below, above := at(70, 38), at(70, 7)
	if below>>24 == 0 || below&0x00FFFFFF != 0 || above>>24 >= below>>24 {
		t.Errorf("the shadow is black and offset down: above %#08x, below %#08x", above, below)
	}
	if float64(below>>24) > cursorPillShadowOpacity*255+1 {
		t.Errorf("below the pill = %#08x, stronger than its shadow", below)
	}
	// The corners are rounded, not fully: the corner pixel is cut off, but
	// six pixels down the edge is already the body (a fully rounded 26 px
	// pill would still be curving away there).
	if p := at(10, 10); p>>24 >= 0x40 {
		t.Errorf("the pill's corner = %#08x, want rounded off", p)
	}
	if p := at(10, 16); p>>24 < 0xE0 {
		t.Errorf("three pixels down the edge = %#08x, want the body: the radius is small", p)
	}
	if r := pillRadius(pill, 1); r != cursorPillRadius {
		t.Errorf("the corner radius is %v, want %v", r, cursorPillRadius)
	}
	for i, p := range s.pix {
		if !validPremultiplied(p) {
			t.Fatalf("pixel %d = %#08x is not premultiplied", i, p)
		}
	}
}

func TestTextIsBlendedOverThePill(t *testing.T) {
	const w, h = 60, 30
	s := &surface{w: w, h: h, pix: make([]uint32, w*h)}
	pill := Rect{Left: 0, Top: 3, Right: 60, Bottom: 27}
	s.drawPill(pill, 1, cursorAccent)
	before := append([]uint32(nil), s.pix...)
	// A text rectangle reaching into the rounded ends, as the padding (10)
	// is less than the radius (12).
	text := Rect{Left: 2, Top: 5, Right: 58, Bottom: 25}
	saved := s.takeText(text)
	for y := text.Top; y < text.Bottom; y++ {
		for x := text.Left; x < text.Right; x++ {
			if p := s.pix[int(y)*w+int(x)]; p != 0 {
				t.Fatalf("takeText left %#08x at (%d,%d), want black for GDI", p, x, y)
			}
		}
	}
	// GDI draws white text onto black: a full glyph pixel, a ClearType
	// fringe (a subpixel colour) and nothing at all; alpha stays zero.
	full, fringe, none := 12*w+20, 12*w+21, 12*w+30
	s.pix[full] = 0x00FFFFFF
	s.pix[fringe] = 0x00FF6000
	s.putText(text, saved, cursorTextColor)
	if p := s.pix[full]; p != 0xFFFFFFFF {
		t.Errorf("a fully covered pixel = %#08x, want opaque white", p)
	}
	cov := textCoverage(0x00FF6000) // (255 + 96 + 0) / 765
	if math.Abs(cov-351.0/765) > 1e-12 {
		t.Errorf("textCoverage = %v, want %v", cov, 351.0/765)
	}
	want := over(before[fringe], premultiply(cursorTextColor, cov))
	if p := s.pix[fringe]; p != want {
		t.Errorf("a ClearType fringe = %#08x, want %#08x: white at its coverage over the pill", p, want)
	}
	if s.pix[none] != before[none] {
		t.Errorf("a pixel GDI left alone = %#08x, want the pill's %#08x back", s.pix[none], before[none])
	}
	// No pixel loses the pill's alpha — the rounded ends included; text
	// over the nearly opaque body only adds to it.
	for i := range s.pix {
		if s.pix[i]>>24 < before[i]>>24 || !validPremultiplied(s.pix[i]) {
			t.Fatalf("pixel %d = %#08x: alpha below the pill's %d", i, s.pix[i], before[i]>>24)
		}
	}
	// Clipped to the surface: no panic.
	s.putText(Rect{Left: -10, Top: -10, Right: 100, Bottom: 100}, s.takeText(Rect{Left: -10, Top: -10, Right: 100, Bottom: 100}), cursorTextColor)
}

// validPremultiplied reports whether no colour channel exceeds alpha.
func validPremultiplied(p uint32) bool {
	a := p >> 24
	return p>>16&0xFF <= a && p>>8&0xFF <= a && p&0xFF <= a
}

// TestArrowPixels (cursor-look 02): the arrow is filled with the
// accent inside a white rim, wrapped in a faint glow of its own colour —
// never a dark shadow — and every sprite drawn from the master, turned or
// pressed, keeps its tip where it was.
func TestArrowPixels(t *testing.T) {
	for _, scale := range []float64{1, 1.5, 2} {
		m := newArrowMaster(scale, cursorAccent)
		r := arrowReach(scale)
		n := int(2*r) + 1
		draw := func(angle, press float64) *surface {
			s := &surface{w: n, h: n, pix: make([]uint32, n*n)}
			s.drawArrowSprite(&m, fpoint{X: float64(r), Y: float64(r)}, angle, press, Rect{Right: int32(n), Bottom: int32(n)})
			return s
		}
		s := draw(0, 1)
		pts := arrowPoints(scale)
		c := float64(r)
		for i := range pts {
			pts[i].X += c
			pts[i].Y += c
		}
		rim := cursorArrowRim * scale
		var fills, rims, glows int
		for y := 0; y < n; y++ {
			for x := 0; x < n; x++ {
				p := s.pix[y*n+x]
				if !validPremultiplied(p) {
					t.Fatalf("scale %v: pixel (%d,%d) = %#08x is not premultiplied", scale, x, y, p)
				}
				sd := polygonSD(pts, float64(x)+0.5, float64(y)+0.5)
				switch {
				case sd < -rim-1: // the fill
					fills++
					if d := colourDist(p, cursorAccent); p>>24 != 0xFF || d > 3 {
						t.Fatalf("scale %v: fill pixel (%d,%d) = %#08x, want the accent", scale, x, y, p)
					}
				case math.Abs(sd) < rim-0.6: // the rim, white
					rims++
					if p>>24 != 0xFF || p&0xFF < 0xF0 || p>>8&0xFF < 0xF0 || p>>16&0xFF < 0xF0 {
						t.Fatalf("scale %v: rim pixel (%d,%d) = %#08x, want white", scale, x, y, p)
					}
				case sd > rim+1.5 && p != 0: // the glow and halo: the accent, faint
					glows++
					// Eight strokes of 2–9.5 % stack to about 34 % at the rim;
					// at a few levels of alpha the colour is all rounding.
					if p>>24 > 0x5A || (p>>24 >= 40 && colourDist(p, cursorAccent) > 16) {
						t.Fatalf("scale %v: glow pixel (%d,%d) = %#08x, want a faint accent", scale, x, y, p)
					}
				}
			}
		}
		if fills == 0 || rims == 0 || glows == 0 {
			t.Errorf("scale %v: %d fill, %d rim, %d glow pixels; want some of each", scale, fills, rims, glows)
		}
		if p := s.pix[0]; p != 0 {
			t.Errorf("scale %v: the box's corner = %#08x, want transparent", scale, p)
		}
		// Turned a quarter clockwise about the tip, each pixel is the
		// upright one at (x, y) moved to (−y, x): only the edges' sampling
		// differs.
		turned := draw(math.Pi/2, 1)
		body := func(s *surface, x, y int) uint32 { return s.pix[(int(r)+y)*n+int(r)+x] >> 24 }
		var off, all float64
		for y := -int(r) + 1; y < int(r); y++ {
			for x := -int(r) + 1; x < int(r); x++ {
				a, b := float64(body(s, x, y)), float64(body(turned, -y-1, x))
				off += math.Abs(a - b)
				all += a
			}
		}
		if off > 0.03*all {
			t.Errorf("scale %v: turned a quarter, %.1f %% of the arrow's weight moved elsewhere", scale, 100*off/all)
		}
		dy := int(15 * scale)
		if body(s, 0, dy) != 0xFF || body(draw(math.Pi/2, 1), -dy, 0) != 0xFF {
			t.Errorf("scale %v: turned 90° clockwise the body below the tip swings to its left", scale)
		}
		if body(draw(0, 0.93), 0, int(27*scale)) >= body(s, 0, int(27*scale)) && body(s, 0, int(27*scale)) > 0 {
			t.Errorf("scale %v: pressed to 93 %% the arrow shrinks toward its tip", scale)
		}
	}
}

// colourDist is how far premultiplied p, unpremultiplied, is from c, per
// channel at most.
func colourDist(p uint32, c color.RGBA) float64 {
	a := float64(p >> 24)
	if a == 0 {
		return 255
	}
	ch := func(v uint32, want uint8) float64 { return math.Abs(float64(v)/a*255 - float64(want)) }
	return max(ch(p>>16&0xFF, c.R), ch(p>>8&0xFF, c.G), ch(p&0xFF, c.B))
}

func TestDrawOutlineSkipIsExactAndItsLook(t *testing.T) {
	const w, h = 260, 160
	target := Rect{Left: 30, Top: 30, Right: 230, Bottom: 130}
	fast := &surface{w: w, h: h, pix: make([]uint32, w*h)}
	fast.drawOutline(target, 1, 1, cursorAccent)
	slow := &surface{w: w, h: h, pix: make([]uint32, w*h)}
	box, skip, px := outlineShape(target, 1, 1, cursorAccent)
	if skip.Width() <= 0 || skip.Height() <= 0 {
		t.Fatal("a 200×100 element has an interior to skip")
	}
	for y := max(int(box.Top), 0); y < min(int(box.Bottom), h); y++ {
		for x := max(int(box.Left), 0); x < min(int(box.Right), w); x++ {
			slow.pix[y*w+x] = px(float64(x)+0.5, float64(y)+0.5)
		}
	}
	for i := range fast.pix {
		if fast.pix[i] != slow.pix[i] {
			t.Fatalf("pixel (%d,%d): filling the interior directly changed it (%#08x vs %#08x)",
				i%w, i/w, fast.pix[i], slow.pix[i])
		}
	}
	at := func(x, y int) uint32 { return fast.pix[y*w+x] }
	// The element is washed with a faint tint of the accent.
	tint := premultiply(cursorAccent, cursorHighlightTint)
	if p := at(130, 80); p != tint {
		t.Errorf("the element's interior = %#08x, want the %v tint %#08x", p, cursorHighlightTint, tint)
	}
	// The border is padded outside the element, opaque at its centre line:
	// x = 30 − 5 + 1.25 at the middle of the left edge.
	if p := at(26, 80); p>>24 != 0xFF {
		t.Errorf("the left border = %#08x, want the opaque accent", p)
	}
	if p := at(130, 26); p>>24 != 0xFF {
		t.Errorf("the top border = %#08x, want the opaque accent", p)
	}
	// Inside the element's edge only the tint and a little glow land.
	if p := at(32, 80); p>>24 == 0 || p>>24 > 0x60 {
		t.Errorf("inside the element's edge = %#08x, want faint", p)
	}
	// Outside the border a faint glow fades out.
	near, far := at(22, 80)>>24, at(14, 80)>>24
	if near == 0 || near > 0x40 || far >= near {
		t.Errorf("the glow outside the border: alpha %d near, %d farther; want faint and fading", near, far)
	}
	if p := at(2, 80); p != 0 {
		t.Errorf("beyond the glow = %#08x, want transparent", p)
	}
	// The corners are rounded: the outer corner of the padded box is
	// outside the border.
	if p := at(25, 25); p>>24 == 0xFF {
		t.Errorf("the padded box's corner = %#08x, want the border rounded off it", p)
	}
	for i, p := range fast.pix {
		if !validPremultiplied(p) {
			t.Fatalf("pixel %d = %#08x is not premultiplied", i, p)
		}
	}
}

func TestOutlinePopRedrawsOnlyWhatMoves(t *testing.T) {
	const w, h = 320, 220
	target := Rect{Left: 60, Top: 60, Right: 260, Bottom: 160}
	draw := func(pop float64) *surface {
		s := &surface{w: w, h: h, pix: make([]uint32, w*h)}
		s.drawOutline(target, 1, pop, cursorAccent)
		return s
	}
	// Stepping through the pop frame by frame lands on exactly the pixels
	// a fresh draw at each scale makes.
	s := draw(1.08)
	prev := 1.08
	for _, pop := range []float64{1.07, 1.05, 1.02, 0.995, 0.99, 0.993, 1} {
		s.popOutline(target, 1, prev, pop, cursorAccent)
		want := draw(pop)
		for i := range s.pix {
			if s.pix[i] != want.pix[i] {
				t.Fatalf("pop %v→%v: pixel (%d,%d) = %#08x, a fresh draw gives %#08x",
					prev, pop, i%w, i/w, s.pix[i], want.pix[i])
			}
		}
		prev = pop
	}
	// At 1.08 the border sits 8 % farther from the centre: the left
	// border's centre line moves from x = 56.25 to 160 − 105·1.08 + 1.35.
	big := draw(1.08)
	if p := big.pix[110*w+47]; p>>24 != 0xFF { // 160 − 105·1.08 + 1.35 = 47.95
		t.Errorf("the popped left border = %#08x, want the opaque accent", p)
	}
	if p := big.pix[110*w+56]; p>>24 == 0xFF {
		t.Errorf("the unpopped border's place = %#08x, want it moved away", p)
	}
}

func TestScalePixelsAboutAnAnchor(t *testing.T) {
	const w, h = 40, 20
	src := make([]uint32, w*h)
	for y := 5; y < 15; y++ {
		for x := 10; x < 30; x++ {
			src[y*w+x] = premultiply(cursorTextColor, 1)
		}
	}
	all := Rect{Right: w, Bottom: h}
	dst := make([]uint32, w*h)
	scalePixels(dst, src, w, h, fpoint{X: 10, Y: 5}, 1, all)
	for i := range src {
		if dst[i] != src[i] {
			t.Fatalf("scaling by 1 changed pixel %d: %#08x vs %#08x", i, dst[i], src[i])
		}
	}
	// Scaled by 0.5 about the block's top-left corner, it shrinks toward
	// that corner: the corner stays, the far half empties.
	clear(dst)
	scalePixels(dst, src, w, h, fpoint{X: 10, Y: 5}, 0.5, all)
	if dst[6*w+11] != src[6*w+11] {
		t.Errorf("near the anchor = %#08x, want the block", dst[6*w+11])
	}
	if dst[12*w+25] != 0 {
		t.Errorf("the far part = %#08x, want emptied by the shrink", dst[12*w+25])
	}
	if dst[9*w+19]>>24 != 0xFF || dst[9*w+21]>>24 > 0x20 {
		t.Errorf("the shrunk block ends at x = 20: %#08x, %#08x", dst[9*w+19], dst[9*w+21])
	}
	for i, p := range dst {
		if !validPremultiplied(p) {
			t.Fatalf("pixel %d = %#08x is not premultiplied", i, p)
		}
	}
}

// TestDrawTrailWidensAndFadesToItsTail (cursor-look 06): thin and faint
// at its tail, wide and stronger at its head, in the lifted accent, with
// no beads where its pieces meet.
func TestDrawTrailWidensAndFadesToItsTail(t *testing.T) {
	var pts []fpoint
	for i := 0; i <= cursorTrailSegments; i++ {
		pts = append(pts, fpoint{X: 20 + 10*float64(i), Y: 30})
	}
	r := trailWindowRect(pts, 1)
	if r.Left > 20-6 || r.Right < 280+6 || r.Top > 30-6 || r.Bottom < 30+6 {
		t.Errorf("the trail's window %+v must hold its widest stroke", r)
	}
	const w, h = 300, 60
	s := &surface{w: w, h: h, pix: make([]uint32, w*h)}
	s.drawTrail(pts, 1, cursorAccent, nil)
	at := func(x, y int) uint32 { return s.pix[y*w+x] >> 24 }
	if at(270, 30) <= at(60, 30) || at(270, 30) > uint32(math.Ceil(cursorTrailAlpha*255)) {
		t.Errorf("the head (%d) is stronger than the tail (%d), at most %v", at(270, 30), at(60, 30), cursorTrailAlpha)
	}
	if at(270, 34) == 0 || at(60, 34) != 0 {
		t.Error("the head is wide, the tail thin")
	}
	// No beads: along the centre line the strength never dips and rises.
	prev := uint32(0)
	for x := 30; x < 280; x++ {
		if a := at(x, 30); a+1 < prev {
			t.Fatalf("the trail dims at x=%d (%d after %d)", x, a, prev)
		} else {
			prev = a
		}
	}
	if c := mixColor(cursorAccent, cursorRimColor, cursorEffectLift); colourDist(s.pix[30*w+270], c) > 3 {
		t.Errorf("the trail = %#08x, want the accent lifted toward white %v", s.pix[30*w+270], c)
	}
	if trailWindowRect(nil, 1) != (Rect{}) {
		t.Error("no trail, no window")
	}
}

// --- Timeline ---

var t0 = time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)

func TestClickTimelinePressAndOutline(t *testing.T) {
	var m cursorModel
	target := Rect{Left: 100, Top: 100, Right: 200, Bottom: 140}
	m.startTravel(t0, Rect{Right: 1000, Bottom: 800}, target, 1, false)
	end := t0.Add(m.path.dur)
	m.advance(end)
	m.startClick(end, target, 1, false)

	f := m.advance(end.Add(100 * time.Millisecond))
	if f.Press != pressScale(100*time.Millisecond) || f.Press >= 1 || f.Tip != rectCentre(target) {
		t.Fatalf("100 ms after the click the arrow is squeezing at the target: %+v", f)
	}
	if !f.Outline || f.OutlineOp <= 0.5 || f.OutlineOp >= 1 || f.OutlineRect != target {
		t.Errorf("100 ms in, the outline is almost done fading in: %+v", f)
	}
	if f.OutlinePop <= 1 || f.OutlinePop >= 1.08 {
		t.Errorf("100 ms in, the outline is shrinking from its pop: %+v", f)
	}
	f = m.advance(end.Add(320 * time.Millisecond))
	if !f.Outline || f.OutlineOp != 1 || f.OutlinePop != 1 {
		t.Errorf("320 ms in, the outline is held, its pop over: %+v", f)
	}
	f = m.advance(end.Add(700 * time.Millisecond))
	if !f.Outline || f.OutlineOp <= 0 || f.OutlineOp >= 1 {
		t.Errorf("700 ms in, between the hold and the end, the outline is fading: %+v", f)
	}
	if f.Press != 1 {
		t.Errorf("the press ends after %v: %v", cursorPressDuration, f.Press)
	}
	f = m.advance(end.Add(cursorOutlineDuration))
	if f.Outline {
		t.Errorf("the outline is gone after %v", cursorOutlineDuration)
	}
	if !f.Cursor || f.Opacity != 1 {
		t.Error("the cursor stays after the click")
	}
	// The opacity rises, then holds, then falls; it never jumps.
	prev, peaked := 0.0, false
	for el := time.Duration(0); el <= cursorOutlineDuration; el += 5 * time.Millisecond {
		op := outlineOpacity(el)
		if op < 0 || op > 1 || math.Abs(op-prev) > 0.25 {
			t.Fatalf("outline opacity %v at %v (after %v): must stay within [0, 1] without jumps", op, el, prev)
		}
		if peaked && op > prev {
			t.Fatalf("outline opacity rises again at %v", el)
		}
		if op == 1 {
			peaked = true
		}
		prev = op
	}
	if outlineOpacity(0) != 0 || !peaked {
		t.Error("the outline fades in from nothing to full strength")
	}
}

func TestPopCurvesMatchAgentDesktop(t *testing.T) {
	// CoreAnimation's curves: ease-out starts fast, ease-in-out is
	// symmetric about its middle.
	if caEaseOut(0) != 0 || caEaseOut(1) != 1 || caEaseOut(0.25) < 0.35 {
		t.Errorf("caEaseOut = %v at 0.25, want a fast start", caEaseOut(0.25))
	}
	if math.Abs(caEaseInOut(0.5)-0.5) > 1e-9 || math.Abs(caEaseInOut(0.3)+caEaseInOut(0.7)-1) > 1e-9 {
		t.Errorf("caEaseInOut is not symmetric: %v, %v, %v", caEaseInOut(0.3), caEaseInOut(0.5), caEaseInOut(0.7))
	}
	// keyframe interpolates linearly between its keys.
	times, values := []float64{0, 0.12, 0.22, 1}, []float64{1.08, 0.99, 1, 1}
	if math.Abs(keyframe(times, values, 0.06)-(1.08+0.99)/2) > 1e-12 || keyframe(times, values, 0.5) != 1 || keyframe(times, values, -1) != 1.08 {
		t.Error("keyframe must interpolate linearly through the keys")
	}
	// The highlight pops from 1.08, dips to 0.99 at eased 0.12 and settles
	// at 1 by eased 0.22 — about 280 ms on the 900 ms clock.
	if outlinePop(0) != 1.08 {
		t.Errorf("the highlight starts at %v, want 1.08", outlinePop(0))
	}
	minPop, minAt := 2.0, time.Duration(0)
	for el := time.Duration(0); el <= cursorOutlineDuration; el += time.Millisecond {
		if p := outlinePop(el); p < minPop {
			minPop, minAt = p, el
		}
	}
	if math.Abs(minPop-0.99) > 1e-3 || minAt < 180*time.Millisecond || minAt > 240*time.Millisecond {
		t.Errorf("the dip = %v at %v, want 0.99 about 210 ms in", minPop, minAt)
	}
	if outlinePop(260*time.Millisecond) == 1 || outlinePop(300*time.Millisecond) != 1 {
		t.Errorf("the pop settles about 280 ms in: %v at 260 ms, %v at 300 ms",
			outlinePop(260*time.Millisecond), outlinePop(300*time.Millisecond))
	}
	// Its opacity keyframes: full by eased 0.04 (about 125 ms in), held
	// to eased 0.55 (about 490 ms), gone at 900 ms.
	if cursorOutlineDuration != 900*time.Millisecond {
		t.Errorf("the highlight lives %v, want agent-desktop's 900 ms hold", cursorOutlineDuration)
	}
	for _, tc := range []struct {
		at     time.Duration
		lo, hi float64
	}{
		{0, 0, 0}, {100 * time.Millisecond, 0.01, 0.99}, {140 * time.Millisecond, 1, 1},
		{450 * time.Millisecond, 1, 1}, {520 * time.Millisecond, 0.01, 0.99},
		{900 * time.Millisecond, 0, 0},
	} {
		if op := outlineOpacity(tc.at); op < tc.lo || op > tc.hi {
			t.Errorf("highlight opacity at %v = %v, want within [%v, %v]", tc.at, op, tc.lo, tc.hi)
		}
	}
	// The label grows from 0.94 to 1 over the reveal, eased out.
	if labelPop(0) != 0.94 || labelPop(cursorLabelReveal) != 1 || labelPop(time.Second) != 1 {
		t.Errorf("labelPop = %v, %v; want 0.94 → 1", labelPop(0), labelPop(cursorLabelReveal))
	}
	if half := labelPop(cursorLabelReveal / 2); half <= 0.97 || half >= 1 {
		t.Errorf("half-way the label is %v, past half its growth (eased out)", half)
	}
}

func TestPopAnchorIsTheBubbleWindowsBottomLeft(t *testing.T) {
	// The macOS bubble window is the pill grown by its 8 pt margin; its
	// content layer scales about the window's bottom-left corner.
	pill := Rect{Left: 20, Top: 30, Right: 220, Bottom: 54}
	if got := popAnchor(pill, 1); got != (fpoint{X: 12, Y: 62}) {
		t.Errorf("anchor = %+v, want 8 left of and below the pill's bottom-left (12, 62)", got)
	}
	if got := popAnchor(pill, 2); got != (fpoint{X: 4, Y: 70}) {
		t.Errorf("anchor at scale 2 = %+v, want the margin doubled (4, 70)", got)
	}
	// Scaling about the anchor keeps it and pulls the rest toward it.
	if scaleAbout(12, 12, 0.94) != 12 || math.Abs(scaleAbout(220, 12, 0.94)-(12+208*0.94)) > 1e-9 {
		t.Error("scaleAbout must keep the anchor and scale distances from it")
	}
}

func TestChooseLabelFaceTakesTheFirstInstalled(t *testing.T) {
	installed := map[string]bool{"Segoe UI Semibold": true}
	resolve := func(face string) string {
		if installed[face] {
			return face
		}
		return "Arial" // GDI substitutes silently
	}
	if got := chooseLabelFace(cursorLabelFaces, resolve); got != "Segoe UI Semibold" {
		t.Errorf("on Windows 10 the face is %q, want Segoe UI Semibold", got)
	}
	installed["Segoe UI Variable Text Semibold"] = true
	if got := chooseLabelFace(cursorLabelFaces, resolve); got != "Segoe UI Variable Text Semibold" {
		t.Errorf("on Windows 11 the face is %q, want Segoe UI Variable Text Semibold", got)
	}
	if got := chooseLabelFace(cursorLabelFaces, func(string) string { return "" }); got != "Segoe UI Semibold" {
		t.Errorf("when nothing resolves the face is %q, want the last fallback", got)
	}
}

// TestLabelRevealIsWholeOnArrival (browser-ux 03): the Action runs when
// the cursor lands, so a new label reveals over the travel's last
// cursorLabelReveal and is whole at the arrival, never after the click.
func TestLabelRevealIsWholeOnArrival(t *testing.T) {
	var m cursorModel
	window := Rect{Right: 1000, Bottom: 800}
	target := Rect{Left: 100, Top: 100, Right: 120, Bottom: 120}
	m.startTravel(t0, window, target, 1, false)
	m.revealLabel()
	arrival := t0.Add(m.path.arrive)
	from := arrival.Add(-cursorLabelReveal)
	if m.path.arrive <= cursorLabelReveal || m.path.arrive >= m.path.dur {
		t.Fatalf("the test travel arrives at %v of %v: must be longer than the reveal", m.path.arrive, m.path.dur)
	}
	if f := m.advance(from.Add(-time.Millisecond)); !f.Cursor || f.LabelOp != 0 {
		t.Errorf("early in the travel a new label is held back: %+v", f)
	}
	// Frames may come late: the reveal runs from its start, not the frame.
	half := m.advance(from.Add(cursorLabelReveal / 2)).LabelOp
	if want := revealOpacity(cursorLabelReveal / 2); half != want || half <= 0.5 || half >= 1 {
		t.Errorf("half-way through the reveal LabelOp = %v, want %v (eased out)", half, want)
	}
	if f := m.advance(arrival); f.LabelOp != 1 || f.LabelPop != 1 || f.Travelling {
		t.Errorf("at the arrival the label is whole and Travel's waiter released: %+v", f)
	}
	if !m.animating() {
		t.Error("the follow-through still settles after the arrival")
	}
	m.advance(t0.Add(m.path.dur))
	if m.animating() {
		t.Error("a finished travel and reveal need no frames")
	}
	// The same label on the next travel is not held back.
	m.startTravel(arrival.Add(time.Second), window, Rect{Left: 600, Top: 500, Right: 620, Bottom: 520}, 1, false)
	if f := m.advance(arrival.Add(time.Second + time.Millisecond)); !f.Travelling || f.LabelOp != 1 || f.LabelPop != 1 {
		t.Errorf("a label already shown travels with the cursor, whole: %+v", f)
	}
	// The pop runs with the reveal: small at its start, whole at the arrival.
	var p cursorModel
	p.startTravel(t0, window, target, 1, false)
	p.revealLabel()
	at := p.labelRevealStart()
	if f := p.advance(at.Add(40 * time.Millisecond)); f.LabelPop != labelPop(40*time.Millisecond) || f.LabelPop <= 0.94 || f.LabelPop >= 1 {
		t.Errorf("40 ms into the reveal the label is mid-pop: %+v", f)
	}
	if f := p.advance(at.Add(cursorLabelReveal)); f.LabelPop != 1 || f.LabelOp != 1 {
		t.Errorf("after the reveal the label is whole: %+v", f)
	}
	// A travel arriving sooner than the reveal (reduced motion) reveals
	// from its start.
	var s cursorModel
	s.startTravel(t0, Rect{Left: 100, Top: 100, Right: 104, Bottom: 104}, Rect{Left: 103, Top: 103, Right: 107, Bottom: 107}, 1, true)
	s.revealLabel()
	if s.path.dur <= 0 || s.path.arrive >= cursorLabelReveal {
		t.Fatalf("short travel %v", s.path.dur)
	}
	if !s.labelRevealStart().Equal(t0) {
		t.Errorf("a short travel's reveal starts at %v, want its start", s.labelRevealStart())
	}
	// A travel with nowhere to go reveals at once.
	var n cursorModel
	n.startTravel(t0, target, target, 1, false)
	n.revealLabel()
	if f := n.advance(t0.Add(cursorLabelReveal)); f.LabelOp != 1 {
		t.Errorf("a zero-length travel's label is revealed from its start: %+v", f)
	}
	prev := -1.0
	for el := time.Duration(0); el <= cursorLabelReveal+10*time.Millisecond; el += 5 * time.Millisecond {
		v := revealOpacity(el)
		if v < prev || v < 0 || v > 1 {
			t.Fatalf("revealOpacity(%v) = %v: must rise monotonically within [0, 1]", el, v)
		}
		prev = v
	}
	if revealOpacity(0) != 0 {
		t.Error("the reveal starts transparent")
	}
}

func TestClickWithoutTravelAppearsAtTarget(t *testing.T) {
	var m cursorModel
	target := Rect{Left: 10, Top: 10, Right: 30, Bottom: 50}
	m.startClick(t0, target, 1, false)
	f := m.advance(t0.Add(time.Millisecond))
	if !f.Cursor || f.Tip != rectCentre(target) || f.Press >= 1 || !f.Outline {
		t.Errorf("a click on a hidden cursor shows it at the target, pressing: %+v", f)
	}
}

func TestTravelStartsAtOriginWhenHiddenElseFromTheCursor(t *testing.T) {
	var m cursorModel
	window := Rect{Left: 0, Top: 0, Right: 1000, Bottom: 800}
	a := Rect{Left: 100, Top: 100, Right: 120, Bottom: 120}
	b := Rect{Left: 700, Top: 600, Right: 720, Bottom: 620}

	m.startTravel(t0, window, a, 1, false)
	if f := m.advance(t0); f.Tip != rectCentre(window) || !f.Travelling {
		t.Fatalf("a hidden cursor starts at the Window's centre: %+v", f)
	}
	mid := t0.Add(m.path.dur / 2)
	midTip := m.advance(mid).Tip

	// A visible cursor sets off from where it is (mid-travel included).
	m.startTravel(mid, window, b, 1, false)
	if f := m.advance(mid); f.Tip != midTip {
		t.Errorf("a visible cursor sets off from its spot %+v, got %+v", midTip, f.Tip)
	}
	end := mid.Add(m.path.dur)
	if f := m.advance(end); f.Tip != rectCentre(b) || f.Travelling {
		t.Errorf("the travel ends exactly at the target's centre: %+v", f)
	}

	// After the idle fade, the next travel starts at the origin again.
	m.advance(end.Add(cursorIdleDelay + cursorFadeDuration + time.Millisecond))
	later := end.Add(time.Minute)
	m.startTravel(later, window, a, 1, false)
	if f := m.advance(later); f.Tip != rectCentre(window) {
		t.Errorf("after fading out the cursor starts at the origin's centre again: %+v", f)
	}
}

func TestIdleFadeDecision(t *testing.T) {
	var m cursorModel
	target := Rect{Left: 100, Top: 100, Right: 120, Bottom: 120}
	m.startTravel(t0, Rect{Right: 1000, Bottom: 800}, target, 1, false)
	m.advance(t0.Add(m.path.dur))
	if m.animating() {
		t.Fatal("the travel has finished")
	}
	// Resting: the timer waits for the float, then wakes at its slower
	// rate — never at the frame rate.
	if d, ok := m.nextWake(t0.Add(m.path.dur)); !ok || d != cursorFloatHold {
		t.Errorf("just landed, nextWake = %v, %v; want the float's hold %v", d, ok, cursorFloatHold)
	}
	if d, ok := m.nextWake(t0.Add(5 * time.Second)); !ok || d != cursorFloatInterval {
		t.Errorf("resting nextWake = %v, %v; want the float's %v", d, ok, cursorFloatInterval)
	}
	if d, ok := m.nextWake(t0.Add(cursorIdleDelay - 10*time.Millisecond)); !ok || d != 10*time.Millisecond {
		t.Errorf("just before the idle fade nextWake = %v, %v; want its deadline", d, ok)
	}
	if f := m.advance(t0.Add(cursorIdleDelay - time.Millisecond)); !f.Cursor || f.Opacity != 1 {
		t.Errorf("just before 6 s idle the cursor is fully shown: %+v", f)
	}
	if m.idleDue(t0.Add(cursorIdleDelay - time.Millisecond)) {
		t.Error("idle is not due before 6 s")
	}
	fadeAt := t0.Add(cursorIdleDelay)
	if !m.idleDue(fadeAt) {
		t.Fatal("idle is due after 6 s without Travel or Click")
	}
	if f := m.advance(fadeAt.Add(cursorFadeDuration / 2)); !f.Cursor || math.Abs(f.Opacity-0.5) > 1e-9 {
		t.Errorf("halfway through the fade the cursor is half transparent: %+v", f)
	}
	if d, ok := m.nextWake(fadeAt.Add(cursorFadeDuration / 2)); !ok || d != cursorFrameInterval {
		t.Error("the fade animates at the frame rate")
	}
	if f := m.advance(fadeAt.Add(cursorFadeDuration)); f.Cursor {
		t.Errorf("after the fade the cursor is hidden: %+v", f)
	}
	if _, ok := m.nextWake(fadeAt.Add(time.Second)); ok {
		t.Error("a hidden cursor needs no timer")
	}

	// A click resets the idle clock; an animation in flight postpones it.
	var n cursorModel
	n.startTravel(t0, Rect{Right: 1000, Bottom: 800}, target, 1, false)
	n.advance(t0.Add(time.Second))
	n.startClick(t0.Add(5*time.Second), target, 1, false)
	n.advance(t0.Add(5*time.Second + cursorOutlineDuration)) // ripple and outline done
	if n.idleDue(t0.Add(5*time.Second + cursorIdleDelay - time.Second)) {
		t.Error("a Click restarts the idle wait")
	}
	if !n.idleDue(t0.Add(5*time.Second + cursorIdleDelay)) {
		t.Errorf("%v after the Click the idle fade is due", cursorIdleDelay)
	}
}

// --- Controller ---

// fakeCursorView records what the controller shows.
type fakeCursorView struct {
	mu sync.Mutex
	s  fakeViewState
}

// fakeViewState is a snapshot of the fake view.
type fakeViewState struct {
	cursor     bool
	tip        fpoint
	opacity    float64
	labelOp    float64
	labelPop   float64
	look       cursorLook
	angle      float64
	press      float64
	trail      bool
	reduced    bool // what reducedMotion answers
	outline    bool
	outlinePop float64
	caret      bool
	caretBar   Rect
	caretOp    float64
	composes   int
	armed      bool
	period     time.Duration
	destroyed  bool
	anchors    []uint64 // every raise's anchor, in order
}

func (v *fakeCursorView) with(f func(s *fakeViewState)) {
	v.mu.Lock()
	defer v.mu.Unlock()
	f(&v.s)
}

func (v *fakeCursorView) monitorAt(fpoint) (Rect, uint32) {
	return Rect{Right: 1920, Bottom: 1040}, 96
}
func (v *fakeCursorView) measureLabel(s string, px, scale float64, regular bool) int32 {
	return int32(7 * px / cursorPillFontPx * scale * float64(len([]rune(s))))
}
func (v *fakeCursorView) showCursor(tip fpoint, op, labelOp, labelPop, angle, press float64, look cursorLook) {
	v.with(func(s *fakeViewState) {
		s.cursor, s.tip, s.opacity, s.labelOp, s.labelPop, s.look = true, tip, op, labelOp, labelPop, look
		s.angle, s.press = angle, press
	})
}
func (v *fakeCursorView) hideCursor() { v.with(func(s *fakeViewState) { s.cursor = false }) }
func (v *fakeCursorView) showTrail([]fpoint, float64, float64) {
	v.with(func(s *fakeViewState) { s.trail = true })
}
func (v *fakeCursorView) hideTrail() { v.with(func(s *fakeViewState) { s.trail = false }) }
func (v *fakeCursorView) reducedMotion() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.s.reduced
}
func (v *fakeCursorView) showOutline(_ Rect, _, pop, _ float64) {
	v.with(func(s *fakeViewState) { s.outline, s.outlinePop = true, pop })
}
func (v *fakeCursorView) hideOutline() { v.with(func(s *fakeViewState) { s.outline = false }) }
func (v *fakeCursorView) showCaret(bar Rect, opacity float64) {
	v.with(func(s *fakeViewState) { s.caret, s.caretBar, s.caretOp = true, bar, opacity })
}
func (v *fakeCursorView) hideCaret() { v.with(func(s *fakeViewState) { s.caret = false }) }
func (v *fakeCursorView) composed()  { v.with(func(s *fakeViewState) { s.composes++ }) }
func (v *fakeCursorView) raise(anchor uint64) {
	v.with(func(s *fakeViewState) { s.anchors = append(s.anchors, anchor) })
}
func (v *fakeCursorView) schedule(d time.Duration) {
	v.with(func(s *fakeViewState) { s.armed, s.period = true, d })
}
func (v *fakeCursorView) cancel()  { v.with(func(s *fakeViewState) { s.armed = false }) }
func (v *fakeCursorView) destroy() { v.with(func(s *fakeViewState) { s.destroyed = true }) }

func (v *fakeCursorView) state() fakeViewState {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.s
}

// fakeClock is a settable clock.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

// inlineDo runs every job at once, on the dispatcher: a UI thread that
// always answers.
func inlineDo(job func()) error { job(); return nil }

// waitFor polls cond for up to a second.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestTravelReturnsWhenTheTravelCompletes(t *testing.T) {
	view := &fakeCursorView{}
	clock := &fakeClock{t: t0}
	c := newAgentCursor(inlineDo, view, clock.now)
	defer c.Close()

	origin := Rect{Right: 1920, Bottom: 1040}
	target := Rect{Left: 1700, Top: 50, Right: 1800, Bottom: 90}
	bound := c.travelBound(origin, target)
	start := time.Now()
	returned := make(chan struct{})
	go func() { c.Travel(origin, target, `click "Save"`); close(returned) }()

	waitFor(t, "the travel to start", func() bool { return view.state().cursor })
	select {
	case <-returned:
		t.Fatal("Travel returned before the travel completed")
	default:
	}
	st := view.state()
	if st.tip != rectCentre(origin) || st.look.Label != `click "Save"` || st.look.Pill.Width() == 0 {
		t.Errorf("the travel starts at the Window's centre with the label pill: %+v", st)
	}
	if st.labelOp != 0 || st.opacity != 1 {
		t.Errorf("a new label is held back while the cursor travels: %+v", st)
	}
	if !st.armed || st.period != cursorFrameInterval {
		t.Errorf("a travel animates at the frame rate: armed %v period %v", st.armed, st.period)
	}
	// The UI thread's timer fires after the travel's end.
	clock.set(t0.Add(cursorTravelMax))
	c.post(func() { c.ui.tick() })
	select {
	case <-returned:
	case <-time.After(bound):
		t.Fatal("Travel did not return when the travel completed")
	}
	if el := time.Since(start); el >= bound {
		t.Errorf("Travel took %v, the bound %v: it should have returned on completion", el, bound)
	}
	if st := view.state(); st.tip != rectCentre(target) {
		t.Errorf("the cursor rests at the target's centre, got %+v", st.tip)
	}
	// The label has faded in by the arrival.
	clock.set(t0.Add(cursorTravelMax + cursorLabelReveal))
	c.postWait(func() { c.ui.tick() }, time.Second)
	if st := view.state(); st.labelOp != 1 {
		t.Errorf("after the reveal the label is whole: labelOp %v", st.labelOp)
	}
	// The same label again travels with the cursor; a new one waits.
	other := Rect{Left: 100, Top: 600, Right: 200, Bottom: 640}
	c.postWait(func() { c.ui.travel(0, origin, other, "", `click "Save"`, make(chan struct{})) }, time.Second)
	if st := view.state(); st.labelOp != 1 {
		t.Errorf("the same label travels shown: labelOp %v", st.labelOp)
	}
	c.postWait(func() { c.ui.travel(0, origin, target, "", `click "Open"`, make(chan struct{})) }, time.Second)
	if st := view.state(); st.labelOp != 0 || st.look.Label != `click "Open"` {
		t.Errorf("a new label is held back: labelOp %v, label %q", st.labelOp, st.look.Label)
	}
}

// TestTravelReturnsAtTheArrival (cursor-look 01): Travel's waiter is
// released when the tip is within a pixel of the target — the Action is
// delivered then — while the follow-through still settles on screen; and
// with Windows' animations off the move is the short straight one.
func TestTravelReturnsAtTheArrival(t *testing.T) {
	for _, reduced := range []bool{false, true} {
		view := &fakeCursorView{}
		view.s.reduced = reduced
		clock := &fakeClock{t: t0}
		c := newAgentCursor(inlineDo, view, clock.now)
		origin := Rect{Right: 1920, Bottom: 1040}
		target := Rect{Left: 1700, Top: 50, Right: 1800, Bottom: 90}
		done := make(chan struct{})
		c.postWait(func() { c.ui.travel(0, origin, target, "", "click", done) }, time.Second)
		var p cursorPath
		c.postWait(func() { p = c.ui.model.path }, time.Second)
		if reduced != (p.dur == cursorReducedTravel) {
			t.Errorf("reduced %v: the travel takes %v", reduced, p.dur)
		}
		clock.set(t0.Add(p.arrive - time.Millisecond))
		c.postWait(func() { c.ui.tick() }, time.Second)
		select {
		case <-done:
			t.Errorf("reduced %v: released before the arrival", reduced)
		default:
		}
		clock.set(t0.Add(p.arrive))
		c.postWait(func() { c.ui.tick() }, time.Second)
		select {
		case <-done:
		default:
			t.Errorf("reduced %v: not released at the arrival %v", reduced, p.arrive)
		}
		st := view.state()
		if st.tip.dist(rectCentre(target)) > cursorArrivalSlack || st.angle != 0 {
			t.Errorf("reduced %v: at the arrival the tip is on the target and upright: %+v", reduced, st)
		}
		if !reduced && (!st.armed || st.period != cursorFrameInterval) {
			t.Error("the follow-through keeps animating after the arrival")
		}
		// Mid-travel a long move turns its tip (unless animations are off).
		c.postWait(func() {
			c.ui.travel(0, origin, Rect{Left: 10, Top: 500, Right: 30, Bottom: 520}, "", "click", make(chan struct{}))
		}, time.Second)
		clock.set(t0.Add(p.arrive + 250*time.Millisecond))
		c.postWait(func() { c.ui.tick() }, time.Second)
		if st := view.state(); reduced != (st.angle == 0) {
			t.Errorf("reduced %v: mid-travel the arrow is turned %v", reduced, st.angle)
		}
		c.Close()
	}
}

func TestTravelIsBoundedWhenTheUIThreadNeverRuns(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	never := func(func()) error { <-block; return nil } // a wedged UI thread
	c := newAgentCursor(never, &fakeCursorView{}, time.Now)

	origin := Rect{Right: 1920, Bottom: 1040}
	target := Rect{Left: 10, Top: 10, Right: 20, Bottom: 20}
	bound := c.travelBound(origin, target)
	if want := 2*cursorTravelDuration(rectCentre(origin), rectCentre(target), 10, 1, false) + cursorTravelSlack; bound != want {
		t.Fatalf("bound = %v, want about twice the travel time (%v)", bound, want)
	}
	for i := 0; i < 2; i++ { // the second call queues behind the first
		start := time.Now()
		c.Travel(origin, target, "click")
		if el := time.Since(start); el < bound-5*time.Millisecond || el > bound+250*time.Millisecond {
			t.Errorf("call %d: Travel returned after %v, want the bound %v", i, el, bound)
		}
	}
	// Click never blocks either.
	start := time.Now()
	c.Click(target)
	if el := time.Since(start); el > 50*time.Millisecond {
		t.Errorf("Click blocked for %v", el)
	}
}

func TestTravelReturnsAtOnceWhenTheUIThreadRefuses(t *testing.T) {
	refuse := func(func()) error { return errUIThreadClosed }
	c := newAgentCursor(refuse, &fakeCursorView{}, time.Now)
	defer c.Close()
	start := time.Now()
	c.Travel(Rect{Right: 1920, Bottom: 1040}, Rect{Left: 10, Top: 10, Right: 20, Bottom: 20}, "x")
	if el := time.Since(start); el > 100*time.Millisecond {
		t.Errorf("Travel waited %v on a UI thread that refused the job", el)
	}
}

func TestTravelSupersededReleasesTheEarlierWait(t *testing.T) {
	view := &fakeCursorView{}
	c := newAgentCursor(inlineDo, view, (&fakeClock{t: t0}).now)
	defer c.Close()
	first := make(chan struct{})
	c.post(func() {
		c.ui.travel(0, Rect{Right: 1000, Bottom: 800}, Rect{Left: 10, Top: 10, Right: 20, Bottom: 20}, "", "a", first)
	})
	second := make(chan struct{})
	c.post(func() {
		c.ui.travel(0, Rect{Right: 1000, Bottom: 800}, Rect{Left: 900, Top: 700, Right: 920, Bottom: 720}, "", "b", second)
	})
	select {
	case <-first:
	case <-time.After(time.Second):
		t.Fatal("a superseded travel must release its waiter")
	}
	select {
	case <-second:
		t.Error("the new travel is still in flight")
	default:
	}
}

func TestHideWhileHidesFirstAndRestoresAfterPanic(t *testing.T) {
	view := &fakeCursorView{}
	clock := &fakeClock{t: t0}
	c := newAgentCursor(inlineDo, view, clock.now)
	defer c.Close()

	target := Rect{Left: 100, Top: 100, Right: 140, Bottom: 120}
	c.Click(target) // shows the cursor, pressing, and the outline
	waitFor(t, "the click to show", func() bool { s := view.state(); return s.cursor && s.outline })

	func() {
		defer func() { _ = recover() }()
		c.HideWhile(func() {
			s := view.state()
			if s.cursor || s.trail || s.outline {
				t.Errorf("f ran before the cursor was hidden: %+v", s)
			}
			if s.composes != 1 {
				t.Errorf("the hide must be composed off the screen before f runs (composes = %d)", s.composes)
			}
			panic("capture failed")
		})
	}()
	waitFor(t, "the cursor to be restored after a panicking f", func() bool { return view.state().cursor })

	// Nested: the inner restore does not show it while the outer runs.
	c.HideWhile(func() {
		c.HideWhile(func() {})
		time.Sleep(20 * time.Millisecond) // let the inner restore run
		if view.state().cursor {
			t.Error("an inner HideWhile's restore showed the cursor inside the outer one")
		}
	})
	waitFor(t, "the outer restore", func() bool { return view.state().cursor })

	// Frames while hidden are not shown.
	c.HideWhile(func() {
		clock.set(t0.Add(50 * time.Millisecond))
		c.post(func() { c.ui.tick() })
		time.Sleep(20 * time.Millisecond)
		if view.state().cursor {
			t.Error("a frame during HideWhile showed the cursor")
		}
	})
}

func TestNilAgentCursorIsSafe(t *testing.T) {
	var c *AgentCursor
	c.Travel(Rect{}, Rect{Right: 10, Bottom: 10}, "x")
	c.Click(Rect{Right: 10, Bottom: 10})
	ran := false
	c.HideWhile(func() { ran = true })
	if !ran {
		t.Error("a nil cursor's HideWhile must still run f")
	}
	c.Close()
}

func TestCloseIsIdempotentAndStopsTheCursor(t *testing.T) {
	view := &fakeCursorView{}
	c := newAgentCursor(inlineDo, view, (&fakeClock{t: t0}).now)
	c.Close()
	c.Close()
	if !view.state().destroyed {
		t.Error("Close destroys the windows")
	}
	start := time.Now()
	c.Travel(Rect{Right: 1920, Bottom: 1040}, Rect{Left: 1, Top: 1, Right: 2, Bottom: 2}, "x")
	c.Click(Rect{Right: 1, Bottom: 1})
	ran := false
	c.HideWhile(func() { ran = true })
	if !ran || time.Since(start) > 100*time.Millisecond {
		t.Error("calls after Close return at once (HideWhile still runs f)")
	}
}

func TestNewAgentCursorIsRefusedOffline(t *testing.T) {
	if LiveDesktopGuard() == nil {
		t.Skip("the user's e2e opt-in allows windows; this checks the offline refusal")
	}
	ran := false
	_, err := NewAgentCursor(func(job func()) error { ran = true; job(); return nil })
	if !errors.Is(err, ErrLiveDesktopInTest) {
		t.Errorf("NewAgentCursor in a test binary = %v, want ErrLiveDesktopInTest", err)
	}
	if ran {
		t.Error("no window job may run in an offline test")
	}
	if _, err := NewAgentCursor(nil); err == nil {
		t.Error("a nil UI thread is an error")
	}
}
