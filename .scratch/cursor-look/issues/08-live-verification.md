# 08: Live verification on the desktop

Status: ready-for-human

The offline tests, the contact sheet and `TestAgentCursorFrameRate` cover
the drawing and the timing; these need a person watching the real desktop
with the helper built from this branch.

1. A Calculator click batch (the default Agent): curved glides, the tip
   turning and upright on landing, the label whole on landing, the press
   and the outline on each key, the float after, the idle fade after 30 s.
   With `WINUSE_CURSOR_TRACE=1` in the helper's environment the log shows
   about 120 frames per second per animation.
2. Two Agents at once (`WINUSE_AGENT`): two palette colours, each with its
   name in its pill, each just above its own Window; their tab groups in
   different colours.
3. Animation effects off: straight 120 ms moves, no turn, no float, no
   press; the outline and the label stay.
4. A 150 % or 200 % monitor, and a move across monitors: the arrow's size
   follows the DPI.
5. `WINUSE_CURSOR_TRAIL=1`: a trail behind the tip; none by default.
6. A screenshot during an Action never shows the cursor.

## Comments
