# 09: Placeholder and remounting editors in `set-value`

Status: resolved
Type: task
From: `map.md`; ticket 15 (reproduced); bladebro `bb/src/action/edit.rs` and the `tools/qol_probe/editor.html` fixture (sections 1–3)

## Problem (reproduced in ticket 15, against winuse's real code and a headless Chrome)

1. **Placeholder ("facade") editors give a false failure.** Many comment
   boxes (Reddit, GitHub, Lexical and Slate composers) start as a
   placeholder textarea that mounts the real contenteditable on focus, then
   hides itself.
   - `Origin.SetValue` (`internal/page/input.go:1104-1143`) focuses the
     textarea, and the text lands in the new editor.
   - It then reads back the hidden textarea (`""`) and reports `the Action
     was delivered; do not repeat it` (`delivered_unverified`), although
     the text went in correctly.
2. **Multi-line `set-value` into any contenteditable gives a false failure.**
   Paragraph blocks add a newline: `"a\nb"` is held as `"a\n\nb"`, and
   `ValueMatches` doesn't normalize that.
3. **Remounting editors make the readback read a detached node.** When the
   editor replaces its own node on the first input, `CheckValue` reads the
   old node (`isConnected=false`). It passes on one line only by luck, and
   fails on multi-line text.
4. **`type-text --into` on a remounting editor silently corrupts the text.**
   For `"one\ntwo"`, the swap after "one" moves the caret to the start, so
   the editor ends up as `"\n\n\ntwoone"`, and the command returns nil.

## Ask

All of this is in `internal/page/input.go`; `pageSetValue` in
`internal/helper/pageactions.go` needs no change.

- **Follow the focus.**
  - After `DOM.focus`, resolve the deep active element, piercing shadow
    roots and same-origin frames, with `focusJS`'s loop.
  - When it is a different, connected editable element, take its
    `backendNodeId` (`Runtime.evaluate`, then `DOM.describeNode`).
    `ReplaceText` selects and writes into it and returns the node it wrote
    to. `SetValue` re-reads `Field` on that node, because the kind changes
    from text to editable, then checks the value there.
  - Add `Note: the field handed its focus to <role> "<name>"; the text went
    there`.
- **Never read back a detached node.** In `CheckValue`, when the target has
  `isConnected=false`, read through the deep active element if it is
  editable, and say so. If there is none, report that the readback was not
  possible rather than reading the stale copy.
- **Editable newlines.** `ValueMatches` collapses the newline that paragraph
  blocks add when it compares a contenteditable. Check this against
  Lexical, ProseMirror and Slate behaviour, and against plain
  `<div contenteditable>`, before choosing the rule.
- **`TypeInto` across a remount.** After each part (text, Return, Tab), if
  the original node was detached, re-focus the deep active editable element
  and put the caret at the end. If that can't be done, stop and fail with
  `may have taken effect` rather than keep typing in the wrong place.
- **Late draft hydration.** Keep the readback immediate, which ticket 15
  showed is unaffected. Never claim an exact match that isn't one.
- **Don't use `execCommand`.** The fixture disables it, as Lexical does.
- **Tests.**
  - Copy `editor.html` sections 1–3 into winuse's e2e fixtures with
    attribution (Apache-2.0).
  - Cases: a plain contenteditable (single-line, multi-line, Unicode), the
    facade (set-value and type-text `--into`), and the remount (single-line,
    multi-line, type-text with newlines).
  - Ticket 15's repro test can become the regression test.

## Answer

Built on branch `agent/bladebro-ideas` (merge d8d4ea9; review fixes in 081a150). `set-value` and `type-text --into` follow a placeholder's focus hand-off and a remounted editor (with a note), never read a detached node, and read multi-line editors back by blocks. Regression test `TestPageEditors` replaces ticket 15's repro.
