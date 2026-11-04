# 10: A flat label tag, styled from multiplayer apps' presence tags

Status: done

Asked by the user after issue 09 (they did not like the pill's gradient);
amends ADR 0018 Decision 8 again.

**Asked.** Find references with poppin (Mobbin), app by app, keep 6–10
crops in `.scratch/cursor-look/refs/` with a README; restyle the pill from
them: a flat solid fill in the Agent's colour, white or near-black text by
contrast, the references' corner radius and offset; keep two lines for
named Agents with a clear hierarchy; the default Agent in the same style;
the name about 12 px and slightly bolder, capped at about 24 characters;
revisit the text shadow; re-render the contact sheet with a reference
strip.

**References** (`refs/README.md`). Mobbin records with one test account,
so live multiplayer is rare; usable tags came from Figma's onboarding and
the FigJam file thumbnail (six crops). Figma: a flat, solid tag in the
cursor's colour, white medium-weight text, corners 1–2 px on a 21 px tag, a
soft drop shadow, its top-left about 14 px right of and 18 px below the tip
(just past the arrow's tail), name only. FigJam's cursor chat: what the
person says in a big bubble, the name in a small tag under it. Miro, Mural,
Pitch, Canva, Framer, Spline, Jitter, Rive, Webflow, FLORA, ClickUp,
Notion, Freeform and Figma iOS showed no other people's cursors; FigJam,
tldraw, Excalidraw, Liveblocks, Google Docs/Slides are not in the
catalogue; `poppin sections` and `poppin similar` failed server-side.

**Done.**

- `drawPill` (`internal/win/cursor_raster.go`): a soft black shadow, then the
  body flat and opaque in the Agent's colour; no gradient, glow or rim.
  `cursorPillRadius` 4 px (`pillRadius`).
- `pillTextColor`: white or `cursorInkColor` (#16181D), whichever has the
  higher WCAG contrast (`contrast`, `luminance`). On cua's pastels it is the
  near-black throughout, at 4.5:1 or better.
- Two lines: the name 12 px semibold over the action 12.5 px **regular** at
  82 % opacity (`cursorPillActionOpacity`), both left-aligned in the text
  colour; the default Agent's one line stays centred semibold. Regular faces
  `cursorLabelRegularFaces` (Segoe UI Variable Text, Segoe UI); `font`,
  `labelFace` and `measureLabel` take the weight, so the pill fits the face
  drawn.
- `capName`: names over 24 characters end in "…" at 24.
- Offset 14, 30 (`cursorPillOffsetX/Y`): just past the arrow's tail, as
  Figma's tag.
- The 1 px text shadow is gone (`putTextShadowed` replaced by `putTextAt`):
  with a flat fill and contrast chosen per colour it no longer helped.
- Contact sheet: label section with six Agents (the default; violet, amber
  with a long name and action, red, mint, blue) at 100 % and 200 % on dark
  and light, and a references strip from `refs/` (sheet-local only).
- Tests: `TestDrawPillBodyAndShadow`, `TestPillTextColorHasTheBetterContrast`,
  `TestNamedAgentsPillHasTwoLines` (24-character cap),
  `TestPlacePillBesideTheTipAndFlipsAtWorkAreaEdges` (14, 30).

## Comments
