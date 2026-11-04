# 02: The new arrow and the pastel palette

Status: done

Spec decision 2 (ADR 0018 Decisions 2 and 3).

**Done.**

- `internal/win/cursor_layout.go`: `cursorArrowOutline` is cua's upright
  DART_OUTLINE (eight cubics on the 128-unit canvas, hotspot (55, 30)),
  42/128 logical px per unit, reshaped about the tip by `cursorArrowNarrow`
  (0.82) and `cursorArrowTall` (1.18) so the tip is the sharpest corner
  (about 43°; cua's about 56°). The contact sheet showed cua's nearly
  equilateral dart reading as "▶" at rest and as its other corners when
  turned. `arrowPoints` flattens each cubic into 12 pieces; `arrowReach` /
  `arrowBounds` size a square that holds it turned any way, pressed to
  1.03, with its glow and halo. The dart, its dark rim and its shadow are
  gone.
- `internal/win/cursor_raster.go`: `newArrowMaster` draws the upright arrow
  once per Agent and DPI at twice the resolution: the halo (28 px, 11 %
  peak, stops 1 / 0.55 / 0), the eight glow strokes (44…7 units,
  2…9.5 %), the white rim (stroke 5 units, about 1.6 px), the fill.
  `drawArrowSprite` samples it 2×2 per pixel, turned and pressed about the
  tip. `polygonSD` takes one square root per point.
- Colours: `cursorAccent` #5EC0E8; `CursorPalette` is cua's nine pastels,
  ordered so the first six map to six different Chrome tab group colours
  (purple, pink, green, yellow, red, blue), none the default's cyan;
  orchid, lime and cyan (closest to the default) come last.
  `TabGroupColor` is unchanged (nearest hue); the default Agent's "winuse"
  group is now cyan.
- `third_party/cua/LICENSE` (MIT) lists what is ported.
- Tests: `TestArrowShapeAndBounds`, `TestArrowPixels`, `TestTabGroupColor`,
  helper `TestAgentTabGroup`, `TestTabNewOnTheExtension`.

## Comments
