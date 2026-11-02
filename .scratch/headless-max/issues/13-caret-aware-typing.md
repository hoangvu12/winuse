# 13: Caret-aware typing into Text-pattern fields

**What to build:** `type-text` into a focused WPF, WinUI/UWP or Chromium-frame field that has the Value and Text patterns becomes Headless by splicing the text at the caret. Today it is Headed (`internal/helper/keyboard.go:83-91`).

Route (`../headless-gaps/headless-100.md` G5):

1. Find the focused Element of the Window through UIA (not the desktop focus: the Window is in the background; use the Element with HasKeyboardFocus inside its subtree, or the last focused Element the provider reports).
2. Read the selection with TextPattern.GetSelection, measured against DocumentRange in UTF-16 units (agent-desktop reads these in `crates/windows/src/tree/text_selection.rs`).
3. `SetValue(prefix + text + suffix)`, then read the value back and move the caret after the inserted text with TextRange.Select.

Refusals ("nothing was sent", `--headed` hint):

- no Text pattern (no caret to splice at);
- a password field (IsPassword);
- a read-only Value;
- rich text (a Text pattern with formatting attributes, or a document control): a whole-value replace would drop formatting.

Limits stated in the output and the skill: no key events fire, the undo stack is lost, web validators may not run. This is not keystroke-faithful typing.

**Blocked by:** none (uses 10's TextRange.Select)

**Status:** needs-triage (headless-parity spec out-of-scope "Headless typing into WPF, WinUI or Chromium text fields")

- [ ] Focused-Element lookup inside a background Window
- [ ] Splice and read-back; caret placement after
- [ ] Routing: after the classic-edit route, before Headed; Batch probe follows
- [ ] Offline tests: splice maths in UTF-16 (surrogate pairs), each refusal
- [ ] Opt-in e2e: a WPF or WinUI text box (e.g. the fixture if it gains one, or Settings search), covered
- [ ] SKILL.md
