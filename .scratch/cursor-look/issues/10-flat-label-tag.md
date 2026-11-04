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

**Second pass (references widened).** The first pass looked only at each
app's newest Mobbin version, screen by screen. The second downloaded every
web screen of every version (Figma, Miro, Mural, Pitch, Framer, Webflow,
Spline, Canva, Notion: about 10 000) and ran a scratch detector for a small
saturated arrow beside a same-hue tag with text in it, then checked the
hits by eye. Collaborator tags turned up in Miro, Mural, Pitch, Canva,
Framer and Figma's current UI; `refs/` now holds ten crops from seven apps
(the Figma zoom, the "Figma basics" illustration and the tagless Figma iOS
crop were dropped), and `refs/README.md` is rewritten. What changed:

- All seven are flat in the cursor's colour, confirming the style. Miro,
  Mural, Pitch and Canva pick the text by contrast (near-black on yellow,
  bright green, light blue, cyan, orange; white otherwise), confirming
  `pillTextColor`.
- Corners: only classic Figma is nearly square. Mural, Canva, Framer and
  Figma's current tags round about 20–30 % of their height; Miro and Pitch
  fully. `cursorPillRadius` is now 6 px (about a quarter of a 26 px line);
  a two-line tag keeps it rather than becoming a blob.
- The 14, 30 offset holds: every app puts the tag's top-left at or just
  past the arrow's tail.
- Contact sheet: lime (the palette's yellow-green, near-black text) and a
  dark green an Agent could choose with `WINUSE_CURSOR_COLOR` (white text)
  added to the label section, four Agents to a row at 100 %; the test also
  writes the label section alone to `.scratch/cursor-look/labels.png`.
- A stale "and glow" in `pillShadowBounds`' comment removed.

## Comments
