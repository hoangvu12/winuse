# 11: Insertion caret after a Headless type-text or set-value

**What to build:** after a Headless type-text or set-value into a field with a UI Automation Text pattern, the Agent Cursor draws a short insertion caret at the field's caret for its dwell time. This is the caret from desktop-parity 21 (`../../desktop-parity/issues/21-agent-cursor-polish.md`), split out of ticket 04 so the cursor work could land first.

- **Caret rectangle (UIA, on the worker):** TextPattern.GetSelection, then GetBoundingRectangles of the selection's range. A collapsed caret range often returns no rectangles: expand it by one character (or move one end back by one) and take the leading edge of the first rectangle, or the trailing edge when the caret sits at the end of the text.
- **Unmeasured** on WPF, WinUI 3 and Chromium's frame: measure each before relying on it, as probe round 5 did for the caret recipe of headless-max 13. A field with no rectangle gets no caret, never a guessed one.
- **A fourth drawing path in the cursor view** (`internal/win/cursor_view.go`), beside the dart, ripple and outline: a thin bar in the Agent's colour, placed above the Window like the rest of the cursor (`placeAbove`), hidden from winuse's own captures (HideWhile) and faded with the cursor.
- **Hooks** in the Headless set-value and type-text routes (classic edit, `--into`, the Text-pattern splice of headless-max 13): after a delivered write, read the caret rectangle and show it through the Agent's cursor (`cursorFor`).

**Blocked by:** headless-max 21 (it is changing the set-value and type-text routes)

**Status:** done (the live e2e run is still to do)

- [x] Caret rectangle from the Text pattern, with the expand-by-one edge; probe WPF, WinUI 3 and Chromium (the probes are not run: live runs are paused; the read is best effort, see Comments)
- [x] The caret drawing path in the cursor view
- [x] Hooks after a delivered Headless set-value and type-text
- [x] Offline tests: the edge choice from faked rectangles; no rectangle, no caret
- [ ] Opt-in e2e: the caret after a Headless type-text into the fixture's Text-pattern field (visual check by the user): written and compiled against the hm-13 WPF box; not run live yet

## Comments

### 2026-11-03: built (branch ma11-caret)

- `internal/uia/caret.go`: `CaretEdgeOf(ctx, hwnd, index)` reads the field's first selection through its Text pattern. It takes the bounding rectangles (GetBoundingRectangles, a SAFEARRAY of doubles) of the selection widened by one character: after the caret first, whose leading edge is the caret, else before it, whose trailing edge is the caret. The pure `caretEdgeOf` picks the edge; no rectangle, or one with no height, means no caret. Best effort: a failure only means no caret.
- `internal/win/cursor_caret.go`: the fourth drawing path.
  - The model gains a caret timeline: 1.2 s, with the last 300 ms fading. It keeps frames coming and the cursor active, but shows no dart.
  - The frame carries the bar: 2 logical pixels wide, centred on the edge, a line high.
  - `cursorView` gains showCaret/hideCaret, and the Win32 view gains a fourth layered window, drawn as a fully rounded bar in the Agent's accent. It is placed above the same Window as the rest (`placeAbove`, below the arrow) and hidden in HideWhile's pause, so it never appears in winuse's captures.
  - `AgentCursor.CaretAbove(anchor, x, top, bottom)`.
- Helper (`caret.go`): `showCaret(ctx, w, index)` reads the edge and draws it through the requesting Agent's cursor (`cursorFor`, `anchoredCursor.Caret`). It is called after a delivered Headless set-value (the Element route and the point route), the `--into` classic insert, and the caret splice. A failed write reads no caret.
  - Plain type-text into a focused classic edit names no Element and shows none.
  - Fake cursors that cannot draw a caret make the helper skip the read.
- Tests: `internal/uia/caret_test.go` (the edge choice), `internal/win/cursor_caret_test.go` (timing, bar, model frames, CaretAbove reaching the view, the capture pause hiding it), and `internal/helper/caret_test.go` (set-value and the splice drawing the Agent's caret above its Window, none without a readable caret, none after a failed write, none for a plain cursor).
- E2e `TestTheInsertionCaretShowsAfterASplice` (`test/e2e/insertion_caret_test.go`): it finds the bar among the `winuse.cursor` windows after a splice into the hm-13 WPF box, and logs where it is for the user's eye. Written and compiled, not run.
- **Unmeasured:** whether WPF, WinUI 3 and Chromium's frame report rectangles for the one-character range (the probe this ticket asked for needs a live run). Where they do not, no caret shows; nothing fails.

