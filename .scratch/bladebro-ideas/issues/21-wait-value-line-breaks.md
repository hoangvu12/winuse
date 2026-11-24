# 21: Line breaks in `wait value`, `wait text` and the rename fallback

Status: resolved
Type: task
From: ticket 20

## Problem

These are the gaps left after ticket 20 confirmed `set-value` itself is
fixed.

1. **`wait <window> <ref> value <v>` compares exactly.**
   `elementStateHolds` (`internal/helper/waits.go:509`) checks
   `st.Value == value`. Notepad (`RichEditD2DPT`) stores CR only, so
   `wait … value $'a\nb'` times out with `it reads "a\rb"`. Inside a Batch,
   that timeout comes out as `delivery_uncertain`, `retry: unsafe`.
2. **`wait text` with a line break can never match,** because the Window
   State prints line breaks as spaces.
3. **The rename fallback is narrow.** `renamedInPlace`
   (`internal/uia/renamed.go`) accepts only an exact `""` readback from the
   element that was written to. If that element instead reads back its old
   name, the false "delivered, do not repeat" comes back.

## Ask

- `case "value"`: `return linebreak.Same(st.Value, value)`. Check the other
  value comparisons in `waits.go` for the same issue.
- `wait text`: normalize line breaks in the wanted text to spaces, matching
  how the tree prints them. Document this in `batch-and-wait.md`.
- `renamedInPlace`: also accept a readback of the old name when a new
  sibling under the same parent carries the new name.
- Tests: unit tests for each. Add an e2e test for Notepad multi-line
  `wait value` if the suite has a Notepad fixture.

## Answer

Built on branch `agent/bladebro-ideas` (merge 60c62f9; review fixes in 081a150). `wait value` treats LF/CRLF/CR alike; native `wait text` and `wait notification --text` match whitespace-collapsed text; rename read-back accepts a stale old name when the new name appeared. On Pages, `wait text` matches line breaks as they are (documented).
