# 11: Mural's presence cursor: a dart and a name-only tag

Status: done

Asked by the user after issue 10: of the references in `refs/`, Mural's
presence cursor (`refs/07-mural-tags.png`, `08-mural-visitor.png`,
`11-mural-visitor-app-crop.png`). Prototyped with the package's own
rasterizer in `mural/cursor_mural_preview_test.go.txt` (previews
`mural/mural-preview.png`, `mural/mural-shapes.png`) and ported from it.
Supersedes ADR 0018 Decision 3 and amends Decisions 5, 8 and 11. The
motion is unchanged ("do like cua").

**Done.**

- Arrow (`arrowPoints`, `cursor_layout.go`): Mural's dart, tip at the
  hotspot, wings at (19, 6.5) and (6.5, 18) logical px, the back curving in
  toward the tip by `cursorArrowNotch` = 3.5 px (the user's choice after a
  first 2.5), the tip rounded by 1.3 px and the wings by 2.4. cua's
  outline, the 0.82 × 1.18 reshape, the glow and the halo are gone.
- `newArrowMaster`: a 1 px white rim outside the outline, a soft black
  shadow (16 %, 1 px down, 1.6 px blur) and the flat fill. The master and
  its cached sprites stay; `arrowReach` is the farthest outline point plus
  the rim and the shadow's offset and blur (`arrowOutset`), pressed.
- The arrow's axis (`cursorTipAxis`) is the bisector of the new wings,
  about −135.5°: a rightward travel turns the tip about 135° (before:
  about 108°). The turning code itself is unchanged.
- Tag (`lookFor`, `layoutCursorWindow`, `drawPill`): the Agent's name only,
  one line, capped at 24 characters; no name, no tag (the default Agent).
  22 px tall, 5.5 px corners, 8 px padding, top-left 12.5 px right of and
  12 px below the tip, 1 px white rim, the arrow's shadow, flat fill, text
  by `pillTextColor`; still flipped and clamped at the work area's edges,
  never turned. `cursorLook` lost `Label`; the two-line layout, the action
  opacity and the regular faces are gone.
- The tag shows whenever the cursor does and fades with it: the per-Action
  label reveal and its pop (`revealLabel`, `labelReveal`, `labelPop`,
  `revealOpacity`, `popAnchor`, `composeCard`, `scalePixels`) are removed,
  and `showCursor` no longer takes a label opacity or scale.
  `TravelAs` keeps its `label` parameter (the helper passes it) but does
  not draw it.
- Font: Figtree Bold 12.5 px, from erikdkennedy/figtree at 032dfa7
  (`fonts/ttf/Figtree-Bold.ttf`, checked against the upstream blob hash),
  in `third_party/figtree/` with its `OFL.txt`; the package `figtree`
  embeds it, `cursor_font.go` loads it once with `AddFontMemResourceEx`
  (process-private); `cursorLabelFaces` is Figtree, then Segoe UI, both
  bold.
- Palette: `cursorAccent` #2F6FD6; `CursorPalette` violet, orange, green,
  pink, teal, red, indigo, yellow (eight, Mural-like mid-tones). White text
  is 4.5:1 or better on all but the yellow, which takes the near-black.
  Tab groups: purple, orange, green, pink, cyan, red, then blue (the
  default's) and yellow; the default Agent's group is blue again.
  `WINUSE_CURSOR_COLOR` is unchanged.
- Attributions: cua is credited for the motion and the trail only
  (`third_party/cua/LICENSE`, file headers); Figtree's notice is
  `third_party/figtree/OFL.txt`.
- Docs: ADR 0018 (title and Decisions 3, 5, 8, 11, Consequences), this
  spec, CONTEXT.md "Agent Cursor", the README, the winuse skill, helper
  comments.
- Tests: `TestArrowShapeAndBounds`, `TestArrowPixels` (fill, rim, shadow
  below, turning, press), `TestTagShowsTheNameOnly`,
  `TestDrawPillBodyAndShadow` (rim), `TestPillTextColorHasTheBetterContrast`
  (4.5:1 everywhere, white but on yellow), `TestTabGroupColor`,
  `TestChooseLabelFaceTakesTheFirstInstalled`, the placement and controller
  tests; the label-reveal tests are gone with the reveal. Helper tab-group
  tests follow the palette.
- Contact sheet re-rendered: `cursor-sheet.png`, `labels.png`,
  `cursor-path.png`, `cursor-arrow-large.png`, and the README's
  `docs/images/winuse-calculator.gif`, clicked by an Agent named "claude"
  so its tag shows.

## Comments
