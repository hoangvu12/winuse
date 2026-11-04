# Agent Cursor look and motion, like cua's

Status: done offline (decided by the user; design in ADR 0018); live
verification is issue 08, ready-for-human

## Problem

The Agent Cursor works, but it does not look or move as well as cua's
(`research.md`, next to this spec). Its 150–400 ms straight glide reads as
a jump; its sharp dark-rimmed dart with a black shadow disappears on a dark
App (the README's Calculator GIF); it never turns, never rests alive, and
its 300 ms disc ripple is heavier than cua's arrow press. Frames come at
about 64 per second (`SetTimer` on the 15.6 ms tick).

## Decisions (ADR 0018)

Every number is from `research.md` §1–§3 unless it says otherwise.

1. **Motion like cua's** (`signature_arc` + `steady`): a cubic bezier bowed
   to the natural side (upward for horizontal moves), its apex about 9 % of
   the distance off the chord; eased by distance through an arc-length
   table with the in-out sine; a follow-through of 1.8 % of the distance,
   at most 8 logical px, peaking at 0.82 of the move and settling back by
   its end; the duration `max(600, 1.1·clamp(150 + 120·log2(D/W + 1), 300,
   1000))` ms, D and W in logical pixels (W the target's smaller side, at
   least 4, 24 with no target). The 600 ms floor is one named constant,
   `cursorTravelFloor`. The Action is delivered at the **arrival**, the
   first moment the tip is within 1 logical px of the target: Travel returns
   then, and the label's reveal is timed to end then (browser-ux 03); the
   follow-through settles while the Action runs.
2. **New arrow**: cua's upright rounded arrow (MIT), 42 logical px for its
   128-unit canvas, reshaped about its tip (0.82 across, 1.18 down) so its
   tip is its sharpest corner; a pastel fill, #5EC0E8 for the default Agent
   and cua's nine pastels for the others (ADR 0014 colours); a white rim of
   1.6 px, eight glow strokes in its own colour, a 28 px halo at 11 %; no
   black shadow. Drawn by the existing Go rasterizer (ADR 0002/0003).
   `win.TabGroupColor` keeps mapping to the nearest Chrome colour; the
   palette's order keeps the first six Agents on six different tab group
   colours, none the default's.
3. **About 120 frames per second while animating**, with the timer
   resolution raised only then, as cua does; back to the slow float or idle
   wake when nothing moves, nothing at all when hidden.
4. **The tip turns along the path** (cua's heading rotation, eased in over
   0.15 s and out over 0.25 s, stretched for big turns, none under 40 px)
   and is upright again at the arrival. Sprites every 3°, made once from one
   master image per Agent and DPI and kept, so a frame is a copy.
5. **Click press** (1 → 0.93 → 1.03 → 1 over 667 ms) instead of the disc
   ripple. The element outline and the insertion caret stay.
6. **Lower-priority items**: a position-only idle float on a 4 s loop that
   is exactly zero for every travel, click, outline and caret (and 0.5 s
   after); the label pill restyled in the Agent's colour, still shown with
   every Action (ADR 0018 says why); Windows' "Animation effects" off
   (`SPI_GETCLIENTAREAANIMATION`) gives a straight 120 ms move, no float, no
   turn, no press; an optional comet trail, `WINUSE_CURSOR_TRAIL=1`, off by
   default.
7. **Kept**: per-monitor DPI v2, the element outline, the insertion caret,
   the cursor just above its target Window rather than topmost (ADR 0014),
   never in winuse's own screenshots (ADR 0006), one cursor per Agent.

## Not in scope

Whether the cursor shows in OBS, Teams or other software's captures. Today's
permanent `WDA_EXCLUDEFROMCAPTURE` (ADR 0006) stays; ADR 0018 notes what a
change would involve.

## Verifying the look

The cursor is excluded from capture, so `TestAgentCursorContactSheet`
(`WINUSE_CURSOR_SHEET=<dir>`) renders frames with the view's own code into
`cursor-sheet.png` (kept next to this spec), `cursor-path.png` and
`cursor-arrow-large.png`, and, given the README's original GIF, re-records
`winuse-calculator.gif`. `TestAgentCursorFrameRate`
(`WINUSE_CURSOR_MEASURE=1`) measures frames and CPU on the desktop.

## Issues

- 01 motion — done
- 02 arrow and colours — done
- 03 frames — done
- 04 turning — done
- 05 press — done
- 06 float, label, reduced motion, trail — done
- 07 docs, contact sheet, GIF — done
- 08 live verification — ready-for-human
- 09 two-line pill for a named Agent — done
- 10 flat label tag from multiplayer-app references — done
