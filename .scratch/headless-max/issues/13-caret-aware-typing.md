# 13: Caret-aware typing into Text-pattern fields

**What to build:** `type-text` into a focused WPF, WinUI/UWP or Chromium-frame field that has the Value and Text patterns becomes Headless by splicing the text at the caret. Today it is Headed (`internal/helper/keyboard.go:83-91`).

Route (`../headless-gaps/headless-100.md` G5):

1. Find the focused Element of the Window through UIA (not the desktop focus: the Window is in the background; use the Element with HasKeyboardFocus inside its subtree, or the last focused Element the provider reports).
2. Read the selection with TextPattern.GetSelection, measured against DocumentRange in UTF-16 units (agent-desktop reads these in `crates/windows/src/tree/text_selection.rs`).
3. `SetValue(prefix + text + suffix)`, then read the value back and move the caret after the inserted text with TextRange.Select.

The same splice serves `type-text <window> --into <element> <text>` (moved here from desktop-parity 23): when the named Element has no writable classic edit control behind it, splice at that Element's own selection instead. Step 1 is skipped, since the agent named the field.

Refusals ("nothing was sent", `--headed` hint):

- no Text pattern (no caret to splice at);
- a password field (IsPassword);
- a read-only Value;
- rich text (a Text pattern with formatting attributes, or a document control): a whole-value replace would drop formatting.

Limits stated in the output and the skill: no key events fire, the undo stack is lost, web validators may not run. This is not keystroke-faithful typing.

**Blocked by:** none (uses 10's TextRange.Select)

**Status:** done (the live e2e run is still to do)

- [ ] Focused-Element lookup inside a background Window: not built. Round 5 measured that no focused Element can be found in a background WPF or WinUI window, so plain `type-text` keeps hm-21's refusal, whose hint names `--into` (see Comments)
- [x] Splice and read-back; caret placement after
- [ ] Routing: after the classic-edit route, before Headed; Batch probe follows: plain type-text, not built (as above)
- [x] `--into` leg: the splice on the named Element, after its classic-edit route and before its Headed/refusal (`internal/helper` type-into routing); Batch `headlessStep` follows
- [x] Offline tests: splice maths in UTF-16 (surrogate pairs), each refusal
- [ ] Opt-in e2e: a WPF or WinUI text box (e.g. the fixture if it gains one, or Settings search), covered: written and compiled against a WPF box the test starts through Windows PowerShell; not run live yet
- [x] SKILL.md

## Comments

### 2026-11-03: absorbed desktop-parity 23's Text-pattern leg

The de-duplication pass found that desktop-parity 23's last `--into` route, "the caret splice of this ticket, aimed at this Element", was the same work as this ticket. That leg now lives here, in the `--into` paragraph and checkbox above. desktop-parity 23 keeps only `--interval-ms`. The `--into` CLI, protocol and the Page and classic-edit routes already exist (landed with headless-max 19). This ticket adds one route between the classic-edit route and the Headed fallback or refusal.

### 2026-11-03: triage decision (user)

Adopt, preferring Headless everywhere: the splice serves both `type-text --into <element>` and plain `type-text` when the Window's focused Element can be found inside its subtree. When it cannot be found, plain `type-text` keeps today's behaviour (the Headed fallback with ticket 19's note). It fires no key events and loses undo, and the output and the skill say so. Chromium-frame fields prefer ticket 20's real key events.

### 2026-11-03: measured on WPF and WinUI 3 (`../headless-gaps/probes-round5.md` section 1)

- **The splice works Headless:** GetSelection, then SetValue(prefix + text + suffix), read back exactly, TextChanged fired, foreground unchanged.
- **No focused Element can be found in a background WPF or WinUI window** (no HasKeyboardFocus, and the app's own focus is empty). So in practice the route serves `type-text --into <element>` only. Plain `type-text` into these Windows keeps today's Headed fallback, and its note should suggest `--into`, as ticket 19's does. The user's "prefer Headless everywhere" is met as far as Windows allows.

Implementation notes from the probe:

- Read offsets as DocumentRange.Clone + MoveEndpointByRange(End, selection, Start) + GetText(-1).Length (UTF-16).
- To place the caret: collapse, Move(Character, n), collapse again, Select. Then read it back with GetSelection and adjust, because the Character unit is a whole emoji in WPF but UTF-16 units in WinUI.
- SetValue resets the caret to 0, so place it again after the splice.
- Undo: WPF keeps one undo step; WinUI keeps none. Say so in the output.
- Refuse IsPassword and Value.IsReadOnly before sending (a read-only SetValue fails with 0x80131509).

### 2026-11-03: built (branch hm13-caret-typing)

- `internal/uia/textsplice.go`: `Client.SpliceText(ctx, hwnd, index, text)` on the worker.
  - Refusals before any write: no Text pattern, a password field (IsPassword), no Value pattern, a read-only Value, a Document control (rich text), a field whose Text-pattern text differs from its Value, and no selection.
  - Offsets come from round 5's recipe: DocumentRange.Clone, then MoveEndpointByRange(End, selection endpoint), then GetText, counted in UTF-16 units.
  - The write is `SetValue(prefix + text + suffix)` (pure `Splice16`), read back exactly; a failed read-back is a VerifyError, which means delivered.
  - The caret is then placed: collapse, Move(Character, n), collapse again, Select, read it back. n is tried as the UTF-16 count, then as the rune count, because WPF counts an emoji as one character and WinUI counts UTF-16 units.
  - A caret that cannot be placed is reported in the output, not failed.
- Helper (`typeinto.go`): on an unheaded `--into`, after the Page and classic-edit routes and before the refusal, an Element with no classic edit control behind it and text with no control characters goes to `spliceInto`. That runs inside runHeadless: its refusals, the passive Esc scope, the Agent Cursor labelled `type`, and minimized Windows allowed, because SetValue was measured on minimized WPF and WinUI 3. The success output is `ok (set as the field's value at the caret: no key events fired, and the field's undo may not hold it)`.
- The refusals go into `refusedBeforeCall`, so they say "nothing was sent", with the `--headed --into` retry (read-only points at another Element). Text with control characters keeps its own refusal. The Batch predicate needed no change: an unheaded `--into` was already Headless or nothing sent.
- **Not built:** the plain type-text leg and its focused-Element lookup. Round 5 found no focused Element in background WPF or WinUI windows (no HasKeyboardFocus, and the app's own focus is empty), and the user's triage said plain type-text keeps today's behaviour when none is found. After hm-21 that behaviour is a refusal whose hint names `--into`; SKILL.md now tells agents to name a WinUI or WPF box with `--into`. Chromium-frame fields are left to ticket 20's real key events.
- Rich text detection is the Document control type only; formatting attributes are not inspected.
- Tests:
  - `internal/uia/textsplice_test.go`: Splice16 with surrogate pairs, and the rune counts.
  - `internal/helper/splice_test.go`: the route, its output and cursor; each refusal says nothing was sent; a failed write or read-back does not; the other routes run first.
- E2e `TestTypeTextIntoAWPFBoxSplicesAtTheCaret` (`test/e2e/caret_typing_test.go`): a WPF TextBox the test starts through Windows PowerShell, shown without activation and covered. select-text puts the caret after "hello", then type-text --into adds "xy😀", which is read back from a file the box writes on every change. The next splice must follow the caret. Foreground and pointer unchanged, no banner. Written and compiled, not run: live runs are paused.

