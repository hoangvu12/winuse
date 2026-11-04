# ADR 0018: The Agent Cursor moves like cua's and looks like Mural's

## Status

Accepted (cursor-look spec, `.scratch/cursor-look/spec.md`, decided by the
user on 2026-11-04 from the research in `.scratch/cursor-look/research.md`).
It replaces the Agent Cursor's look and motion of headless ticket 03 (ported
from agent-desktop) and keeps ADR 0006 (never in winuse's own captures,
still excluded from other software's), ADR 0014 (a cursor per Agent, just
above its Window, its colour) and ADR 0016 (the native cursor on Pages).
The motion is ported from trycua/cua at ecaf10b (MIT; `third_party/cua/LICENSE`).
*Amended (cursor-look issue 11):* the arrow, the name tag and the palette
are no longer cua's but drawn after Mural's presence cursor (Decisions 3,
8 and 11); the motion is unchanged.

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
   *Superseded (cursor-look issue 11, the user's pick of Mural's presence
   cursor from `.scratch/cursor-look/refs/`):* the arrow is Mural's dart,
   measured off its board: the tip at the hotspot, wings at (19, 6.5) and
   (6.5, 18) logical px, the back curving in toward the tip by 3.5 px
   (`cursorArrowNotch`, the user's choice among 2–3.5), the tip's corner
   rounded by 1.3 px and the wings' by 2.4. A 1 px white rim outside the
   shape, a soft black shadow (16 %, 1 px down, 1.6 px blur) and a flat
   fill in the Agent's colour: no glow, no halo and no reshape. The master
   image and its kept sprites stay.
   *Amended (cursor-turn, the user's "do it like cua"):* the dart rests as
   cua's arrow rests. cua turns its arrow's art about the hotspot
   (`UPRIGHT_DEGREES`) so the line from its body's centre to its tip points
   up and a little left, −99.5°; the dart's outline is turned the same way
   about its tip, 36.6° clockwise from Mural's up-left pose, so its body
   points to its tip at −99.5° too. Its shape, notch, rim, shadow and
   colours are Mural's; only the resting pose is cua's.
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
   in Decision 3. For Mural's dart (issue 11) that axis is nearly the
   diagonal, about −135°, so a rightward move turns the tip about 135°
   (cua's reshaped arrow turned about 108°).
   *Amended (cursor-turn, the user's "do 1:1 to cua";
   `.scratch/cursor-look/turning-1to1.md`):* the turn is cua's
   `travel_rotation` exactly. Each ramp is at most half the time to the
   arrival, which the port had missed: a big turn's stretched ramps
   overlapped and the arrow never reached its heading (a 400 px down-right
   move peaked at 106° of 180°). The travel direction is the shown path's
   velocity over ±8 ms, the chord below 1 px/s. The axis is cua's rule,
   the body's centre to the tip, and since the dart rests as cua's arrow
   (Decision 3) it is cua's −99.5°: the dart turns by the very angles cua
   turns its arrow (a rightward move 99.5° clockwise, a down-right one
   144.5° clockwise), frame for frame (`TestTipTurningIsCuas`). cua's float tilt
   (±2.5°, and its float while travelling) is not ported: Decision 7
   stands.
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
   (Figma, FigJam, Miro, Mural, Pitch, Canva, Framer:
   `.scratch/cursor-look/refs/`), not like cua's badge. All seven draw the
   tag flat in the cursor's colour. So it is a flat, opaque tag in the
   Agent's colour, with no gradient, glow or rim, only a soft drop shadow
   (Figma, Pitch, Canva). Its corners have a 6 px radius, about a quarter
   of a line's height, as Mural, Canva, Framer and Figma's current tags
   round theirs (Miro and Pitch round fully, classic Figma barely). Its
   top-left sits 14 px right of and 30 px below the tip, just past the
   arrow's tail, as all of them place it. The text is white or near-black
   (#16181D), whichever has the higher WCAG contrast with the Agent's
   colour, as Miro, Mural, Pitch and Canva choose it (dark on yellow,
   bright green, light blue, cyan); on cua's pastels that is the
   near-black everywhere, at 4.5:1 or better, and a dark colour an Agent
   picks gets white. A named Agent's name is 12 px semibold, cut to 24 characters,
   over the action in the regular face at 82 % opacity, the way FigJam
   ranks a person's name and what they are doing. The default Agent keeps
   one centred semibold line in the same flat style. With a flat fill and
   contrast chosen per colour the text shadow no longer helps and is gone.
   *Superseded (cursor-look issue 11, the user's pick):* the tag is
   Mural's and shows the Agent's **name only**; the action text is no
   longer drawn (the API still takes it). An Agent with no name (no
   `WINUSE_AGENT`, the default Agent) shows no tag, the arrow alone. Since
   the name does not change from one Action to the next, the tag shows
   whenever the cursor does and fades with it, as Mural's and Figma's do;
   the per-Action reveal and its pop are gone. It stays upright while the
   arrow turns. The tag is 22 px tall, its corners rounded by 5.5 px,
   padded 8 px at each end, its top-left 12.5 px right of and 12 px below
   the tip, over the arrow's back (18 px below since cursor-turn: the
   upright dart's right wing came within 0.2 px of the tag, and 18 keeps
   Mural's 3.4 px between their rims); a 1 px white rim and the arrow's soft
   shadow; flat fill; the text white or near-black by contrast, as before.
   The name is capped at 24 characters with an ellipsis and set in
   **Figtree Bold** at 12.5 px, the closest free match to Mural's font,
   embedded in winuse.exe (`third_party/figtree`, SIL OFL 1.1) and loaded
   for the process alone with `AddFontMemResourceEx`, with Segoe UI Bold
   should that fail. The tag still flips left and up at the work area's
   edges and is clamped into it.
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
    *Amended (cursor-look issue 11):* the palette is mid-tones like
    Mural's, so white text reads at 4.5:1 or better on all but the yellow,
    which takes the near-black: the default Agent's blue #2F6FD6, then
    violet, orange, green, pink, teal, red, indigo and yellow. The first
    six take purple, orange, green, pink, cyan and red tab groups; the
    indigo shares the default's blue and the yellow comes last. The
    default Agent's tab group is blue again.

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
- The default Agent's tab group was cyan from issue 02 to issue 10, and
  is blue again since issue 11.
- The Agent Cursor no longer says what each Action does: only the
  Agent's name, and nothing for the default Agent. What an Action did is
  still in its output and the helper's trace.
