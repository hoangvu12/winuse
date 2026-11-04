# 07: Docs, contact sheet, README GIF

Status: done

**Done.**

- `TestAgentCursorContactSheet` (`internal/win/cursor_sheet_test.go`,
  `WINUSE_CURSOR_SHEET=<dir>`): renders through the view's own code on a
  memory DC (no window): arrows at 100/200 % on light, dark and the accent,
  all ten colours, turns every 30°, the press with the outline, three paths
  sampled at 120 fps, pills, the trail. It writes `cursor-sheet.png`,
  `cursor-path.png` and `cursor-arrow-large.png` (kept in this folder).
  With `WINUSE_CURSOR_SHEET_CALC` set to
  `.scratch/cursor-look/winuse-calculator-before.gif` it re-records the
  README's Calculator GIF: that recording's Calculator, its old cursor
  removed by a per-pixel median of three frames, its display after each
  click, and the new cursor driven by the model at 33 fps.
- `BenchmarkCursorFrame` (same file): per-frame drawing costs.
- `docs/images/winuse-calculator.gif` replaced; the old one is kept here as
  `winuse-calculator-before.gif`.
- ADR 0018; CONTEXT.md "Agent Cursor"; README, SKILL.md and codex-parity.md
  ("ripples" is now "presses"; the trail switch).

## Comments
