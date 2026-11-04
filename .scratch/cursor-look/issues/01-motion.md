# 01: Motion like cua's: arc, sine ease, follow-through, Fitts with a 600 ms floor, act at arrival

Status: done

Spec decision 1 (ADR 0018 Decision 1).

**Done.**

- `internal/win/cursor_motion.go`: `cursorTravelDuration(from, to, side,
  scale, reduced)` is cua's Fitts time, `max(cursorTravelFloor,
  1.1·clamp(150 + 120·log2(D/W + 1), 300, 1000))` ms in logical pixels;
  `targetSide` is W. `cursorTravelFloor` (600 ms) is the one knob.
- `newCursorPath(from, to, target, scale, reduced)`: cua's Cua bezier
  (handles 0.3/0.3, arc 0.16, flow 0.575, the natural side upward for
  horizontal moves), a 128-step arc-length table (`param`); `along` is the
  in-out sine over the first 0.82 of the move plus `overshootBump`
  (bump_profile verbatim) × min(1.8 %, 8 px·scale / length), past the end
  along the end tangent (`dir`). Adding the bump to a sine spanning the
  whole move never got past the target (the ease still trails by 8 % at
  0.82), so the sine ends at the bump's peak.
- `arrive`: the first millisecond the tip is within 1 logical px of the
  target. The frame says `Travelling` until then, so the controller releases
  Travel's waiter at the arrival; `m.travel` keeps the follow-through
  animating. `labelRevealStart` keys off `arrive`, so the label is whole
  when the Action is delivered (browser-ux 03's timing holds).
- `AgentCursor.travelBound` passes the target's side.
- Tests: `TestCursorPathArcsLikeCua`, `TestCursorTravelDurationIsCuasFitts`,
  `TestTravelReturnsAtTheArrival`, `TestLabelRevealIsWholeOnArrival`
  (re-timed), `TestCursorPathEndpointsAreExact`.

## Comments
