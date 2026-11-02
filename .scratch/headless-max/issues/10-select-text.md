# 10: select-text

**What to build:** `select-text <window> <element> <start> <length>` and `select-text <window> <element> all` select text in a field without keys or the mouse. Today selecting needs Shift+arrows, Ctrl+A or a drag, all Headed.

Routes (`../headless-gaps/headless-100.md` G8):

- Classic Edit or RichEdit (the Element's native HWND): SendMessageTimeout(EM_SETSEL, start, end). Measured to work cross-thread.
- UIA Text pattern (WPF, WinUI, Chromium frame, RichEdit providers): DocumentRange, move the start and end by character units, `ITextRangeProvider::Select`. Unmeasured headless; AutoSetFocus is off.
- Page Element: through CDP (`Runtime.callFunctionOn` with `setSelectionRange`, or a `Selection` range for contenteditable).

Readback: EM_GETSEL, or TextPattern.GetSelection, or the CDP selection. A mismatch is a VerifyError.

Offsets are UTF-16 code units, as `type-text` verification already counts. `all` is 0 to the length.

Use with `type-text`: the classic-edit route replaces the selection, so select-text then type-text is a headless "replace this word".

**Blocked by:** none

**Status:** ready-for-agent

- [ ] Three routes behind Services fakes
- [ ] CLI parsing; Batch step kind; usage text
- [ ] Offline tests: offsets, `all`, out of range, no Text pattern ("nothing was sent")
- [ ] Opt-in e2e: fixture edit box, Notepad, a Page input; covered
- [ ] CONTEXT.md Action list; SKILL.md
