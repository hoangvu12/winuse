# 15: Reproduce the placeholder-editor readback bug

Status: resolved
Type: prototype
Branch: `agent/facade-repro` (worktree `C:\Users\ADMIN\Desktop\nguyenvu\winuse-facade-repro`), repro only, not to be merged
From: `map.md`, ticket 09

## Question

Does `set-value` (and `type-text --into`) on bladebro's `editor.html` show
the bug described in ticket 09?
- **Section 2:** the placeholder mounts a contenteditable on focus, then
  hides itself, and a draft is prepended 600 ms later.
- **Section 3:** the editor replaces its own node on the first input.

For each case, record:
- where the text actually went;
- what `CheckValue` returned;
- what Delivery message the agent would see;
- what `document.activeElement` is right after `DOM.focus`.

The repro uses winuse's real `internal/page` code against a headless Chrome.

## Answer

**Method.** A temporary Go test, not committed, at
`C:\Users\ADMIN\Desktop\nguyenvu\winuse-facade-repro\internal\helper\facade_repro_test.go`,
with a copy in `../prototypes/facade_repro_test.go`.
It skips unless `WINUSE_FACADE_REPRO=1` is set, and reads the fixture path
from `WINUSE_FACADE_FIXTURE`. Run it with
`go test ./internal/helper -run TestFacadeRepro -v -count=1`.

It drives winuse's real code against headless Chrome, in the same order as
`pageSetValue`:
1. `ReadField`
2. `Refusal`
3. `Aim{Scroll:true}`
4. `NewInput`
5. `SetValue` or `TypeInto`

It then passes the error through `pageFailure` and reads `Delivery()`.
`pageSetChoice` passes on a textarea or contenteditable, so `SetValue` is the
real route. The fixture is a copy of bladebro's `editor.html`, served over
`httptest`.

**Facade (section 2): reproduces.**
- Right after `DOM.focus(facade)`, the deep active element is
  `div#editor [contenteditable]`, and the facade is already `display:none`.
- set-value "hello facade": the editor holds `"hello facade"` and the facade
  holds `""`.
- `CheckValue` gives `the value was delivered, but the field holds ""
  afterwards (it may limit or reformat its text)`. The hint says `the Action
  was delivered; do not repeat it`, and Delivery is `delivered_unverified`.
  **This is a false failure.**
- **Draft hydration:** the draft lands 600 ms after mount and goes *before*
  the typed text (`"DRAFT\n\nhello facade"` at 800 ms). It doesn't touch an
  immediate readback.
- **Simulated fix:** after focus, run `ReadField` and `SetValue` on the
  active element. The result is Kind=editable, no error, and an exact value.
  The fix works on this fixture.
- type-text `--into`: the text lands in the editor and it returns nil. There
  is no readback, so there is no false failure.

**Remount (section 3): no false failure on one line, but there are other
problems.**
- **Single line.** set-value "hello remount" passes, but only because
  `CheckValue` reads the old node, now detached (`isConnected=false`), which
  still holds the text. The live node also holds it.
- **A second set-value** through the stale reference is refused before
  anything is sent (`stale: the node left the document`, `not_delivered`).
  That's correct.
- **Multi-line set-value "a\nb": false failure.** The detached node reads
  `"ab"` while the live editor holds `"a\n\nb"`.

**Two new bugs, not about remounting:**
- **Multi-line `set-value` into any contenteditable fails readback.** The
  plain contenteditable also holds `"a\n\nb"`, because paragraph blocks add
  a newline, and `ValueMatches` doesn't normalize it.
- **`type-text --into "one\ntwo"` on the remount editor silently corrupts the
  text.** The swap after "one" moves the caret to the start, so the editor
  ends up as `"\n\n\ntwoone"`, and the command returns nil.

**Fix sketch** (all in `internal/page/input.go`; `pageSetValue` needs no
change):
1. After `DOM.focus`, resolve the deep active element with `focusJS`'s
   piercing loop. If it is a different, connected editable, return its
   `backendNodeId` (`Runtime.evaluate` + `DOM.describeNode`).
2. `ReplaceText` selects and writes into that node and returns it.
   `SetValue` re-reads `Field` on it and calls `CheckValue` on it.
3. `CheckValue`: when the target has `isConnected=false`, read through the
   active node, or re-find the node. Never read a detached copy.
4. `ValueMatches` for editable fields: collapse the extra newline that
   paragraphs add.
5. `TypeInto`: after each part, if the original node was detached, re-focus
   the active element and put the caret back at the end.
6. Keep readback immediate, so late hydration doesn't matter.
