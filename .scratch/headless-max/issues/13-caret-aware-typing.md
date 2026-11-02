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

**Status:** ready-for-agent

- [ ] Focused-Element lookup inside a background Window
- [ ] Splice and read-back; caret placement after
- [ ] Routing: after the classic-edit route, before Headed; Batch probe follows
- [ ] `--into` leg: the splice on the named Element, after its classic-edit route and before its Headed/refusal (`internal/helper` type-into routing); Batch `headlessStep` follows
- [ ] Offline tests: splice maths in UTF-16 (surrogate pairs), each refusal
- [ ] Opt-in e2e: a WPF or WinUI text box (e.g. the fixture if it gains one, or Settings search), covered
- [ ] SKILL.md

## Comments

### 2026-11-03: absorbed desktop-parity 23's Text-pattern leg

The de-duplication pass found that desktop-parity 23's last `--into` route, "the caret splice of this ticket, aimed at this Element", was the same work as this ticket. That leg now lives here, in the `--into` paragraph and checkbox above. desktop-parity 23 keeps only `--interval-ms`. The `--into` CLI, protocol and the Page and classic-edit routes already exist (landed with headless-max 19). This ticket adds one route between the classic-edit route and the Headed fallback or refusal.

### 2026-11-03: triage decision (user)

Adopt, preferring Headless everywhere: the splice serves both `type-text --into <element>` and plain `type-text` when the Window's focused Element can be found inside its subtree. When it cannot be found, plain `type-text` keeps today's behaviour (the Headed fallback with ticket 19's note). It fires no key events and loses undo, and the output and the skill say so. Chromium-frame fields prefer ticket 20's real key events.
