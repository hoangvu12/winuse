# ADR 0018: The Agent Cursor looks and moves like cua's

## Status

Accepted (cursor-look spec, `.scratch/cursor-look/spec.md`, decided by the
user on 2026-11-04 from the research in `.scratch/cursor-look/research.md`).
It replaces the Agent Cursor's look and motion of headless ticket 03 (ported
from agent-desktop) and keeps ADR 0006 (never in winuse's own captures,
still excluded from other software's), ADR 0014 (a cursor per Agent, just
above its Window, its colour) and ADR 0016 (the native cursor on Pages).
The code is ported from trycua/cua at ecaf10b (MIT; `third_party/cua/LICENSE`).

## Context

winuse's cursor moved in a straight 150–400 ms glide that read as a jump,
and its dark-rimmed dart with a black shadow disappeared on a dark App.
cua's cursor arcs, follows through, turns along its path, floats at rest
and presses on a click, at about 120 frames per second; its pastel arrow
with a white rim and a glow in its own colour reads on light and dark
alike. winuse is ahead on per-monitor DPI, the element outline, the
insertion caret and showing what each Action does.

## Decision

1. **Motion.** cua's `signature_arc` with the `steady` timing: a cubic
   bezier bowed to its natural side (upward for a horizontal move), apex
   about 9 % of the distance off the chord; eased by distance (an arc-length
   table) with the in-out sine; a follow-through of 1.8 % of the distance,
   at most 8 logical px, at 0.82 of the move, settled by its end; the
   duration `max(600, 1.1·clamp(150 + 120·log2(D/W + 1), 300, 1000))` ms in
   logical pixels. The 600 ms floor is one constant (`cursorTravelFloor`);
   every Headless Action is about 300 ms slower than before, which the user
   chose for the feel. In cua the bump is added to the ease; added to a sine
   spanning the whole move it never reaches past the target (the sine still
   trails by 8 % at 0.82), so our sine covers the path by 0.82, where the
   bump peaks.
2. **Act at the arrival.** Travel returns, and so the Action is delivered,
   when the tip is first within 1 logical px of the target (cua's
   `arrival_t`); the label's reveal ends then (browser-ux 03), and the
   follow-through settles while the Action runs.
3. **The arrow.** cua's upright rounded arrow, 42 logical px for its
   128-unit canvas, with a white 1.6 px rim, eight glow strokes in its own
   colour, a 28 px halo at 11 % and no black shadow, filled #5EC0E8 for the
   default Agent and with cua's nine pastels for the others, drawn by our
   pure-Go rasterizer (ADR 0002/0003: no cgo, no new DLL; `winmm.dll`'s
   `timeBeginPeriod` is a system DLL, loaded like `dwmapi.dll`). **We
   reshape the outline about its tip**, 0.82 across and 1.18 down, so the
   tip is its sharpest corner (about 43°, like the system arrow's 45°; cua's
   is about 56°). On the contact sheet cua's nearly equilateral dart read as
   a "▶" pointing right at rest, past its own tip, and, turned, its other
   corners read as the point.
4. **Frames.** About 120 per second while anything animates: `SetTimer`
   alone fires on the 15.6 ms tick on Windows 11 even with the resolution
   raised (64 measured), so a frame clock (a goroutine with an 8 ms ticker,
   Go's high-resolution waitable timers) posts the frames to the cursor's
   window, coalesced. As cua does, the timer resolution is raised
   (`timeBeginPeriod(1)`) only while some cursor animates and given back
   when they all rest. A resting cursor wakes at 30 ms for its float, a
   hidden one never. The arrow is drawn exactly once per Agent and DPI, at
   twice the resolution; every turned (3° steps) or pressed (0.005 steps)
   arrow is sampled from it once and kept, so a frame is copies.
5. **Turning.** The tip turns toward where it goes (cua's heading rotation:
   in over 0.15 s, out over 0.25 s, both stretched by 1 + |turn|/π, none
   under 40 px) and is upright again at the arrival. The arrow's axis is the
   bisector of its wings rather than cua's line to its halo, for the reason
   in Decision 3.
6. **The click's press** (1 → 0.93 → 1.03 → 1 over 667 ms) replaces the
   disc ripple. The element outline and the insertion caret stay.
7. **The float.** At rest the tip sways and bobs (position only, so the
   window is only moved) on a 4 s loop, exactly zero while the cursor
   travels, presses, outlines or shows a caret, and for 0.5 s after.
8. **The label stays with every Action, in the Agent's colour.** cua shows
   only the session's name, and only on hover or with two or more cursors
   on screen. winuse's label says what each Action does (`click "Save"`)
   and is whole when the Action is delivered (CONTEXT.md "Agent Cursor"):
   it is how the user reads headless work, the cursor's purpose (ADR 0010).
   Hover would also need the pointer sampled every 80 ms while a cursor is
   shown. So the label keeps its text and timing and takes cua's verbose
   badge's look: a diagonal gradient of dark stops mixed toward the Agent's
   colour, a light rim tinted with it, a soft glow in it. The colour alone
   does not say which Agent is which once the nine colours repeat or an
   Agent picks its own, so a named Agent's name is always shown too.
   *Amended (cursor-look issue 09, the user's request):* a named Agent's
   pill has two lines — its name, smaller (10.5 px), semibold, in its
   colour lightened to read on the pill, over the action at the usual size —
   instead of `reviewer: click "Save"` on one line. The name line shows
   whenever the Agent has a name; cua's "on hover or with two or more
   cursors" rule is not adopted. The default Agent's pill stays the one
   action line. The pill keeps its place, reveal, colours and maximum
   width; each line is cut on its own; a two-line pill's text has a faint
   dark shadow, since its name sits on the pill's light end.
   *Amended again (cursor-look issue 10, the user disliked the gradient):*
   the label is styled like the collaborator tags of multiplayer apps
   (Figma, FigJam: `.scratch/cursor-look/refs/`), not like cua's badge. It
   is a flat, opaque tag in the Agent's colour, with no gradient, glow or
   rim, only a soft drop shadow. Its corners have a 4 px radius: Figma's
   are 1–2 px on a 21 px tag, and ours is taller and sits under a rounded
   arrow. Its top-left sits 14 px right of and 30 px below the tip, just
   past the arrow's tail, as Figma's does. The text is white or near-black
   (#16181D), whichever has the higher WCAG contrast with the Agent's
   colour; on cua's pastels that is the near-black everywhere, at 4.5:1 or
   better. A named Agent's name is 12 px semibold, cut to 24 characters,
   over the action in the regular face at 82 % opacity, the way FigJam
   ranks a person's name and what they are doing. The default Agent keeps
   one centred semibold line in the same flat style. With a flat fill and
   contrast chosen per colour the text shadow no longer helps and is gone.
9. **Reduced motion.** When Windows' "Animation effects" are off
   (`SPI_GETCLIENTAREAANIMATION`, read on every travel and click), the move
   is a straight 120 ms minimum-jerk one with no turn, float, press or
   trail, as cua does.
10. **The trail** (cua's comet) is opt-in, `WINUSE_CURSOR_TRAIL=1` in the
    helper's environment, drawn in the window the ripple used.
11. **Tab groups.** `TabGroupColor` stays nearest-hue. Ten colours on
    Chrome's nine cannot all differ, so the palette is handed out in an
    order whose first six Agents take six different group colours, none
    the default Agent's (now cyan).

## Not decided: other software's captures

The cursor keeps `WDA_EXCLUDEFROMCAPTURE` permanently (ADR 0006), so OBS,
Teams and the Snipping Tool do not show it. cua sets it only around its own
capture. Changing that would mean: set the affinity in `HideWhile` (or not
at all) instead of at window creation, follow it with `DwmFlush` as the hide
already does, amend ADR 0006 Decision 4, and drop the demo-only
`cursorExcludeFromCapture` switch. winuse's own screenshots would be
unaffected, since `HideWhile` hides the windows anyway; other software's
recordings would show the cursor. It is the user's decision.

## Consequences

- Every Headless and Page Action takes about 300 ms longer before it is
  delivered (600 ms minimum glide, arrival a little before its end).
- About 13 % of one core of this machine while a cursor animates (124–126
  frames per second, 0.5 ms of drawing per frame), under 1 % while one
  floats, none when hidden (`TestAgentCursorFrameRate`).
- The look is checked offline by rendering it (`TestAgentCursorContactSheet`),
  since the cursor is excluded from capture.
- The default Agent's tab group is cyan instead of blue.
