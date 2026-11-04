package win

// The tip's turning against cua's own formulas (cursor-turn): cua's
// cursor-overlay cursor_pose.rs travel_rotation with its ramps, and
// cua-cursor-motion plan.rs Trajectory::sample_at and velocity_at,
// re-implemented here from trycua/cua at ecaf10b (MIT; see
// third_party/cua/LICENSE) and run on the same moves as tipAngle.

import (
	"math"
	"testing"
	"time"
)

// cuaSample is one planned trajectory sample: seconds, points.
type cuaSample struct{ t, x, y float64 }

// cuaTrajectory is a trajectory as cua's planner hands it to the pose: the
// path sampled every DT_MS (1000/120 ms) until it ends, in points.
type cuaTrajectory struct {
	samples []cuaSample
	arrival float64
}

// cuaSampled samples p as cua's planner does, at 120 Hz in points. The
// arrival is p's own, so the comparison is of the turning, not of where
// the arrival falls between two 120 Hz samples.
func cuaSampled(p cursorPath) cuaTrajectory {
	var tr cuaTrajectory
	const dt = time.Second / 120
	for el := time.Duration(0); ; el += dt {
		if el > p.dur {
			el = p.dur
		}
		pt := p.at(p.progress(el))
		tr.samples = append(tr.samples, cuaSample{el.Seconds(), pt.X / p.scale, pt.Y / p.scale})
		if el == p.dur {
			break
		}
	}
	tr.arrival = p.arrive.Seconds()
	return tr
}

// sampleAt is Trajectory::sample_at: linear between samples, clamped.
func (tr cuaTrajectory) sampleAt(t float64) cuaSample {
	first, last := tr.samples[0], tr.samples[len(tr.samples)-1]
	if t <= first.t {
		return first
	}
	if t >= last.t {
		return last
	}
	i := 1
	for tr.samples[i].t <= t {
		i++
	}
	a, b := tr.samples[i-1], tr.samples[i]
	f := (t - a.t) / (b.t - a.t)
	return cuaSample{t, a.x + (b.x-a.x)*f, a.y + (b.y-a.y)*f}
}

// velocityAt is Trajectory::velocity_at, points per second.
func (tr cuaTrajectory) velocityAt(t float64) (float64, float64) {
	const h = 0.008
	if t > tr.samples[len(tr.samples)-1].t+h {
		return 0, 0
	}
	a, b := tr.sampleAt(t-h), tr.sampleAt(t+h)
	return (b.x - a.x) / (2 * h), (b.y - a.y) / (2 * h)
}

// cuaWrap is cua's wrap_angle.
func cuaWrap(a float64) float64 {
	r := math.Mod(a, 2*math.Pi)
	if r > math.Pi {
		r -= 2 * math.Pi
	}
	if r < -math.Pi {
		r += 2 * math.Pi
	}
	return r
}

func cuaSmoothstep(x float64) float64 {
	t := math.Max(0, math.Min(1, x))
	return t * t * (3 - 2*t)
}

// cuaTravelRotation is travel_rotation for RotateMode::Heading, not
// pressed, with tipAxis the arrow's tip_axis().
func cuaTravelRotation(tr cuaTrajectory, t, tipAxis float64) float64 {
	a, b := tr.samples[0], tr.samples[len(tr.samples)-1]
	if t >= tr.arrival || math.Hypot(b.x-a.x, b.y-a.y) < 40 {
		return 0
	}
	chord := math.Atan2(b.y-a.y, b.x-a.x)
	stretch := 1 + math.Abs(cuaWrap(chord-tipAxis))/math.Pi
	rin := math.Max(math.Min(0.15*stretch, tr.arrival*0.5), 1e-6)
	rout := math.Max(math.Min(0.25*stretch, tr.arrival*0.5), 1e-6)
	pin, pout := cuaSmoothstep(t/rin), cuaSmoothstep((tr.arrival-t)/rout)
	vx, vy := tr.velocityAt(t)
	travel := chord
	if math.Hypot(vx, vy) > 1 {
		travel = math.Atan2(vy, vx)
	}
	target := cuaWrap(chord-tipAxis) + cuaWrap(travel-chord)
	return target * pin * pout
}

// TestTipTurningIsCuas (cursor-turn): frame by frame at 120 Hz, on long
// and short moves every way, at 100 % and 150 %, tipAngle is cua's
// heading rotation for our arrow's tip axis.
func TestTipTurningIsCuas(t *testing.T) {
	from := fpoint{X: 1200, Y: 700}
	for _, mv := range []struct {
		name  string
		d     fpoint
		scale float64
	}{
		{"rightward 400", fpoint{X: 400}, 1},
		{"down-right 400", fpoint{X: 400 / math.Sqrt2, Y: 400 / math.Sqrt2}, 1},
		{"straight down 600", fpoint{Y: 600}, 1},
		{"a little left of down", fpoint{X: -20, Y: 600}, 1},
		{"up-left 300", fpoint{X: -212, Y: -212}, 1},
		{"leftward 800 at 150 %", fpoint{X: -800}, 1.5},
		{"up 120 at 150 %", fpoint{Y: -120}, 1.5},
		{"a 30 px hop", fpoint{X: 30}, 1},
		{"45 px at 150 % (30 logical)", fpoint{X: 45}, 1.5},
	} {
		p := newCursorPath(from, fpoint{X: from.X + mv.d.X, Y: from.Y + mv.d.Y}, Rect{}, mv.scale, false)
		tr := cuaSampled(p)
		worst := 0.0
		for el := time.Duration(0); el <= p.dur+50*time.Millisecond; el += time.Second / 120 {
			ours, theirs := p.tipAngle(el), cuaTravelRotation(tr, el.Seconds(), cursorTipAxis)
			worst = math.Max(worst, math.Abs(ours-theirs))
		}
		// The two differ only in reading the path exactly or between its
		// 120 Hz samples: well under a sprite's 3°.
		if worst > 0.25*math.Pi/180 {
			t.Errorf("%s: tipAngle differs from cua's by up to %.2f°", mv.name, worst*180/math.Pi)
		}
	}
	// With Windows' animations off nothing turns, as in cua.
	if r := newCursorPath(from, fpoint{X: 1600, Y: 700}, Rect{}, 1, true); r.tipAngle(r.arrive/2) != 0 {
		t.Error("a reduced-motion travel turns")
	}
}

// TestTipAxisIsTheBodyCentreToTheTip (cursor-turn): the tip axis is cua's
// rule applied to our dart — from the centre of the painted arrow's
// opaque pixels (cursor_pose.rs the_body_offset_is_the_centre_of_the_
// painted_arrow) to the tip — and cua's own arrow's is about −99.5°.
func TestTipAxisIsTheBodyCentreToTheTip(t *testing.T) {
	const scale = 4
	m := newArrowMaster(scale, cursorAccent)
	var sx, sy, n float64
	for y := 0; y < m.n; y++ {
		for x := 0; x < m.n; x++ {
			if m.pix[y*m.n+x]>>24 > 128 {
				sx += float64(x) + 0.5
				sy += float64(y) + 0.5
				n++
			}
		}
	}
	hs := float64(scale * arrowSuper)
	painted := fpoint{X: (sx/n - m.c) / hs, Y: (sy/n - m.c) / hs}
	c := arrowCentroid()
	if math.Hypot(painted.X-c.X, painted.Y-c.Y) > 0.5 {
		t.Errorf("the painted body's centre is %+v, the outline's centroid %+v", painted, c)
	}
	if want := math.Atan2(-painted.Y, -painted.X); math.Abs(cursorTipAxis-want) > math.Pi/180 {
		t.Errorf("the tip axis is %.1f°, the painted body to the tip %.1f°", cursorTipAxis*180/math.Pi, want*180/math.Pi)
	}
	if a := math.Atan2(-11.9, -2.0) * 180 / math.Pi; a < -100 || a > -99 {
		t.Errorf("cua's arrow's axis is %.2f°", a)
	}
}
