# 04: The tip turns along the path, upright at the arrival

Status: done

Spec decision 4 (ADR 0018 Decision 5).

**Done.**

- `cursorPath.turn` / `tipAngle`: the path's heading less the arrow's own
  axis (`cursorTipAxis`, the bisector of its wings, about −105°; cua's line
  to its halo's centre left a rightward arrow reading as pointing down),
  unwrapped about the chord's turn so it never jumps across ±π; smoothstep
  in over 0.15 s and out over 0.25 s, both × (1 + |turn|/π); zero at the
  start, at the arrival and after; none under 40 logical px or with
  reduced motion.
- View: `spriteFor` quantizes to 3°; sprites are made from the master on
  first use and kept per Agent and DPI (dropped when the DPI changes); a
  frame copies the pill as shown and one sprite.
- Tests: `TestTipTurnsAlongThePathAndLandsUpright`, `TestArrowPixels` (a
  quarter turn moves under 3 % of the weight), `TestTravelReturnsAtTheArrival`.

## Comments
