# 06: Idle float, label pill, reduced motion, optional trail

Status: done

Spec decision 6 (ADR 0018 Decisions 7–10).

**Done.**

- Float: `floatOffset` (sway 1.64·sin, bob 1.97·cos − 1.64, logical px,
  4 s loop), position only, so the window is only moved. Its amplitude is
  exactly 0 while `busy` (travel, press, outline, caret), held at 0 for
  0.5 s after, eased in over 0.8 s; a click that comes while it floats
  eases it out in 0.2 s (`quiet`). A travel sets off from the floated spot.
  Resting wakes are 30 ms, at the default timer resolution.
- Label: still shown with every Action, whole at the arrival; restyled as
  cua's verbose badge (a diagonal gradient of #5E97B2 / #2B5C77 / #0D1B26
  mixed 66 / 52 / 26 % toward the Agent's colour at 95 %, a 1 px rim of
  white mixed 55 % toward it, a soft glow in it over a faint black shadow),
  26 px high. ADR 0018 Decision 8 says why it is not cua's hover-only name.
- Reduced motion: `reducedMotion()` reads `SPI_GETCLIENTAREAANIMATION` on
  every travel and click; off gives a straight 120 ms minimum-jerk move, no
  turn, no float, no press, no trail (the outline and label stay).
- Trail: `WINUSE_CURSOR_TRAIL=1` turns on `trailPoints` (the last 0.18 s of
  path in 26 pieces, cut 16 px behind the tip) and `drawTrail` (width
  2 → 12, opacity 0.38·k², the accent lifted 45 % toward white, the
  strongest piece wins) in the old ripple window.
- Tests: `TestFloatIsZeroForEveryAction`, `TestTrailFollowsBehindTheTip`,
  `TestDrawTrailWidensAndFadesToItsTail`, `TestDrawPillBodyAndShadow`,
  `TestIdleFadeDecision`, `TestTravelReturnsAtTheArrival` (reduced).

## Comments
