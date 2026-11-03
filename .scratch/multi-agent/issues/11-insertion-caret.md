# 11: Insertion caret after a Headless type-text or set-value

**What to build:** after a Headless type-text or set-value into a field with a UI Automation Text pattern, the Agent Cursor draws a short insertion caret at the field's caret for its dwell time. This is the caret from desktop-parity 21 (`../../desktop-parity/issues/21-agent-cursor-polish.md`), split out of ticket 04 so the cursor work could land first.

- **Caret rectangle (UIA, on the worker):** TextPattern.GetSelection, then GetBoundingRectangles of the selection's range. A collapsed caret range often returns no rectangles: expand it by one character (or move one end back by one) and take the leading edge of the first rectangle, or the trailing edge when the caret sits at the end of the text.
- **Unmeasured** on WPF, WinUI 3 and Chromium's frame: measure each before relying on it, as probe round 5 did for the caret recipe of headless-max 13. A field with no rectangle gets no caret, never a guessed one.
- **A fourth drawing path in the cursor view** (`internal/win/cursor_view.go`), beside the dart, ripple and outline: a thin bar in the Agent's colour, placed above the Window like the rest of the cursor (`placeAbove`), hidden from winuse's own captures (HideWhile) and faded with the cursor.
- **Hooks** in the Headless set-value and type-text routes (classic edit, `--into`, the Text-pattern splice of headless-max 13): after a delivered write, read the caret rectangle and show it through the Agent's cursor (`cursorFor`).

**Blocked by:** headless-max 21 (it is changing the set-value and type-text routes)

**Status:** ready-for-agent

- [ ] Caret rectangle from the Text pattern, with the expand-by-one edge; probe WPF, WinUI 3 and Chromium
- [ ] The caret drawing path in the cursor view
- [ ] Hooks after a delivered Headless set-value and type-text
- [ ] Offline tests: the edge choice from faked rectangles; no rectangle, no caret
- [ ] Opt-in e2e: the caret after a Headless type-text into the fixture's Text-pattern field (visual check by the user)
