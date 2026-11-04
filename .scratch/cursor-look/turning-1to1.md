# The tip's turning, 1:1 with cua

Research for the `cursor-turn` branch: what trycua/cua at ecaf10b
(ecaf10b1973f304e2e6088adc063069031a5e6ab, MIT) does to the arrow's angle,
what winuse's port did, and what changed. Paths are under
`libs/cua-driver/rust/crates/`.

## What sets the arrow's angle in cua

The painted angle is **`travel_rotation` plus the float tilt**, nothing
else.

- `cursor-overlay/src/render_state.rs:473-475` `painted_rotation()` =
  `heading_rotation_at(motion_t)` + `float_offset().tilt`.
- `render_state.rs:479-487` `heading_rotation_at`: `travel_rotation(traj,
  motion_t, mode, pressed)` while a trajectory plays, **0 under reduced
  motion**.
- `render_state.rs:1422-1428`: the theme pivots on its hotspot; the painted
  heading is `REST_HEADING + rotation`, and `theme_artifact.rs:830-849`
  turns the art by `heading − π/4` (= the rotation) about the hotspot,
  `post_rotate` in tiny-skia's y-down frame: **positive is clockwise**, as
  ours.
- Default mode: `cursor_options.rs:93-100` `CursorOptions::DEFAULT` has
  `rotate: RotateMode::Heading` (and float and halo on).

### `travel_rotation` (`cursor-overlay/src/cursor_pose.rs:170-213`)

1. `0` when `mode == Off`, when not travelling (`t >= traj.arrival_t`,
   `:165-168`), or when the hop (start → end sample, straight) is under
   `ROTATE_MIN_HOP_PT = 40` points (`:122`, `:159-163`, `:174`).
2. `chord = atan2(end − start)`.
3. Heading mode stretches the ramps by `1 + |wrap(chord − tip_axis())| / π`
   (`:179-185`).
4. `ramps(t, arrival_t, 0.15·stretch, 0.25·stretch)` (`:123-124`,
   `:186-191`), where `ramps` (`:148-157`) **caps each ramp at half the
   arrival time** (`ramp.min(arrival·0.5).max(1e-6)`) and returns
   `smoothstep(t / in)`, `smoothstep((arrival − t) / out)`.
5. Travel direction: `traj.velocity_at(t)` (`:192-197`), a central
   difference over ±8 ms of the interpolated samples
   (`cua-cursor-motion/src/plan.rs:489-498`, `sample_at` `:467-487`, linear
   and clamped to the ends), in points per second; below 1 pt/s the chord.
6. `target = wrap(chord − tip_axis()) + wrap(travel − chord)`, unwrapped
   about the chord so a path near straight down never flips (`:198-200`).
7. `lean` only: `cap·tanh(target / cap)`, cap 30°, 15° while a button is
   held (`:117-120`, `:201-211`). Heading ignores `pressed`.
8. Result `target · p_in · p_out` (`:212`).

### The tip axis (`cursor_pose.rs:126-134`)

`tip_axis() = atan2(−BODY_OFFSET_PT.y, −BODY_OFFSET_PT.x)`, the direction
from the arrow body's centre to the hotspot, with `BODY_OFFSET_PT =
(2.0, 11.9)` points: **−99.54°** on cua's upright arrow. The test
`the_body_offset_is_the_centre_of_the_painted_arrow` (`:536-560`) defines
the body centre as the mean of the painted arrow's pixels with alpha > 128.

### The float tilt (`cursor_pose.rs:21-115`)

`float_at` adds a tilt of `2.5° · cos(phase)` on a 4 s loop (`:32`,
`:81-88`), scaled by the float amplitude, which is full at rest after a
0.5 s hold and 0.8 s ease-in (`:93-97`) and **also full mid-move** on hops
of 30 points or more (`travel_amplitude`, `:99-115`, ramps 0.25/0.35 s),
exactly 0 at the arrival.

### The planner's heading channel is not the painted angle

`cua-cursor-motion/src/plan.rs:680-718` gives every sample a `heading`:
`want = wrap(atan2(v) − TIP_ANGLE)·w` with `w = clamp((speed − 40)/260, 0,
1)` and `TIP_ANGLE = −0.75π` (`:654`), smoothed `rot += wrap(want −
rot)·(1 − e^(−22·dt))` (`:701-702`), then relaxed back to rest after the
last point (`:710-721`). It is used only to place the anchor that the
hotspot is drawn from (`render_state.rs:785`,
`geom.rs:61-74` anchor/pointer 16 pt along the heading) and the trail,
which flows from the anchor. `render_state.rs:1424-1426`: "The arrow is
upright (the rest heading) unless `cursor.rotate` turns it ... The hotspot
above still follows the planned heading". **research.md's gap 1 (speed
weighting and k = 22 smoothing in the rotation) is wrong for the painted
arrow**: that channel never turns it.

### Drag, reduced motion, arrival

- Drag (`pressed`): only caps `lean` (`cursor_pose.rs:203-207`); heading
  turns a drag like any move.
- Reduced motion: no rotation (`render_state.rs:482`); the move itself is
  the straight one (`plan.rs` `req.reduced`, `:591-593`).
- Arrival: the first 120 Hz sample within 1 point of the target
  (`plan.rs:27`, `:33`, `:738-743`); the rotation is 0 from then on.

## What winuse's port did (main, before this branch)

`internal/win/cursor_motion.go:385-437` matched the 40 pt rule, the
arrival cut, the stretch, the 0.15/0.25 s ramps and the unwrapping, but:

1. **No cap on the ramps.** A big turn stretches them to up to 0.3 s and
   0.5 s; with a 534 ms arrival they overlap, so the arrow never reaches
   its heading. cua caps each at arrival/2 (267 ms here).
2. Travel direction from the analytic tangent of the arc, not the velocity
   of the shown path (the same direction before the arrival, to within the
   sampling; the follow-through comes after the arrival).
3. Tip axis: the bisector of the dart's wings (−135.5°), not body centre →
   tip.

The float: winuse's is position only and exactly 0 while travelling
(ADR 0018 Decision 7); cua's floats in travel and tilts ±2.5°. **Not
changed here** (see below).

## What cua's default does, frame by frame

The hotspot paths below are winuse's port of cua's signature arc with the
steady timing (the same planner), so the times are cua's: a 400 px move with
no target lasts 712 ms and arrives at 534 ms. "tip − travel" is how far the
tip points from where it is going.

### (a) 400 px rightward, cua's arrow (axis −99.5°, turn +99.5°, clockwise)

```
  ms   rotation  tip points  travels  tip-travel
    0      0.0°     -99.5°    -20.8°    -78.7°
   66     16.0°     -83.6°    -19.3°    -64.3°
  133     51.3°     -48.2°    -15.1°    -33.1°
  199     85.2°     -14.4°     -9.5°     -4.9°
  266     96.5°      -3.0°     -3.0°      0.0°
  333     87.5°     -12.1°      3.9°    -16.0°
  399     55.6°     -43.9°     11.1°    -55.0°
  466     18.6°     -80.9°     17.8°    -98.7°
  533      0.0°     -99.5°     22.6°   -122.1°
```

### (b) 400 px down-right, cua's arrow (turn +144.5°, clockwise)

```
  ms   rotation  tip points  travels  tip-travel
    0      0.0°     -99.5°     24.2°   -123.7°
   66     19.5°     -80.0°     25.7°   -105.7°
  133     64.6°     -35.0°     29.9°    -64.8°
  199    113.8°      14.3°     35.5°    -21.2°
  266    141.5°      42.0°     42.0°      0.0°
  333    125.5°      26.0°     48.9°    -22.9°
  399     78.2°     -21.3°     56.1°    -77.4°
  466     25.8°     -73.8°     62.8°   -136.6°
  533      0.0°     -99.5°     67.6°   -167.1°
```

So cua's arrow points along its path only in the middle of a move: it turns
up through the first half and back to upright through the second. In (b)
it is within 30° of the travel 29 % of the frames before the arrival, the
mean miss 73°. It never points backwards on the way out, because a
clockwise turn from "up" toward down-right passes through up and right;
on the way back it does (−137° at 466 ms) as it rights itself before the
arrival. That is cua's own look.

## Our dart under cua's rule

The dart's body centre (its area centroid, 7.1, 6.8 px right of and below the tip; the
painted alpha > 128 pixels agree to under 0.5 px) gives a tip axis of
**−136.2°**, 36.7° further round than cua's arrow. Every turn is 36.7°
bigger or smaller than cua's: rightward 136.2° (cua 99.5°), down-right
**178.8°** (cua 144.5°), straight down 133.8° counterclockwise (cua 170.5°,
also counterclockwise).

A down-right move is almost exactly opposite the dart's axis, so the turn is
a half turn and its sign follows the wrap: `wrap(45° + 136.2°) = −178.8°`,
**counterclockwise**, through left and down-left.

### Down-right 400 px, before (main) and after (this branch)

```
            before (no ramp cap)           after (1:1)
  ms    rotation  tip  tip-travel    rotation  tip  tip-travel
    0       0.0  -135.5   -159.7         0.0  -136.2   -160.4
   66     -24.9  -160.3    173.9       -30.9  -167.1    167.2
  133     -73.1   151.4    121.6       -96.8   127.0     97.2
  199    -104.3   120.2     84.7      -158.7    65.1     29.6
  266     -97.6   127.0     85.0      -181.8    42.0      0.0
  333     -62.3   162.2    113.3      -147.9    75.9     27.0
  399     -29.9  -165.4    138.6       -84.4   139.5     83.4
  466      -8.0  -143.5    153.7       -25.6  -161.7    135.5
  533       0.0  -135.5    156.9         0.0  -136.2    156.2
```

| 400 px move | peak turn / needed | tip within 30° of travel | mean miss |
|---|---|---|---|
| rightward, before | 95.7° / 135.5° | 0 % | 89.2° |
| rightward, after | 133.6° / 136.2° | 31 % | 68.2° |
| rightward, cua's arrow | 97.1° / 99.5° | 42 % | 47.3° |
| down-right, before | 106.4° / 179.5° | **0 %** | **128.8°** |
| down-right, after | 182.1° / 178.8° | 26 % | 88.4° |
| down-right, cua's arrow | 142.0° / 144.5° | 29 % | 72.9° |

(120 Hz frames from the start to the arrival.)

Before, the down-right arrow never got within 45° of its path: it turned a
little over half way, pointing left and down-left the whole time, and was
upright only after it stopped (the README video's look). After, it turns
the whole way and points along the path for the middle third, as cua's.

**The 1:1 port still turns the dart more than cua turns its arrow, and on a
down-right move the dart spins a half turn counterclockwise, through
left, instead of cua's 144.5° clockwise through up and right.** For about
the first 100 ms of the move (to 30 % of its turn) and the last 70 ms
the tip points 135–170° from where it goes. That is the shape, not the
rule: cua's arrow at rest points up (−99.5°), ours up-left (−136.2°). This
branch does not invent a fix; options for the user (not done): keep it;
turn the dart's art so its rest axis is cua's −99.5° (it would no longer
be Mural's dart at rest); use `lean` (30° cap) instead of `heading`; or
pick the turn's direction by something other than the shortest wrap.

## Not ported: the float's tilt and its float in travel

cua's painted angle also has the float's ±2.5° tilt, at rest and in
travel. winuse's float is position only and exactly 0 while travelling
(ADR 0018 Decision 7, the user's call in issue 06), and the sprites are
kept every 3°, so a ±2.5° tilt would show as 0° or ±3° steps. Porting it
means new sprites (or a 1° step), a tilt at rest and a float in travel: a
separate decision for the user.

## The branch

- `internal/win/cursor_motion.go`: `tipAngle` is `travel_rotation`
  (ramps capped by `turnRamps`, the velocity over ±8 ms of the shown path,
  the chord below 1 logical px/s); `cursorTipAxis` is body centre → tip
  (`arrowCentroid`).
- `internal/win/cursor_turn_test.go`: `TestTipTurningIsCuas` re-implements
  `travel_rotation`, `ramps`, `sample_at` and `velocity_at` from cua and
  checks `tipAngle` against them every 1/120 s on nine moves (under 0.25°;
  without the ramp cap it differs by up to 90°);
  `TestTipAxisIsTheBodyCentreToTheTip` measures the painted dart as cua's
  test does.
- Contact sheet: a new row, the 400 px rightward and down-right moves, a
  frame every 33 ms to the arrival.
