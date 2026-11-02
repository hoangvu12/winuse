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

**Status:** done

- [x] Three routes behind Services fakes
- [x] CLI parsing; Batch step kind; usage text
- [x] Offline tests: offsets, `all`, out of range, no Text pattern ("nothing was sent")
- [x] Opt-in e2e: fixture edit box, Notepad, a Page input; covered
- [x] CONTEXT.md Action list; SKILL.md

## Comments

### 2026-11-02: landed

- `internal/win/keyroute.go`: `SetEditSelection` (SendMessageTimeout EM_SETSEL with SMTO_ABORTIFHUNG and the 2 s edit timeout; refuses in a test binary, pinned in `liveguard_test.go`), `ReadControl` (class, style, enabled state and rectangle of any window, never its text) and `Control.RichEdit`. The read-back reuses `ReadEditState` (WM_GETTEXTLENGTH and EM_GETSEL).
- `internal/uia/textselect.go`: `TextSpan` (Start, Length, All) with `CheckTextSpan` (0 or more, end at most 100,000,000) and `Bounds`; the refusals `ErrNoTextPattern`, `ErrNoTextSelection`, `ErrTextUnreadable`, `ErrTextRange`, `ErrTextSpan`. `Client.ElementWindow` reads UIA_NativeWindowHandlePropertyId. `Client.SelectText` is the Text pattern route: SupportedTextSelection None is refused; the DocumentRange is cloned, collapsed to its start, its end moved start+length characters and its start moved start characters (a short move is `ErrTextRange` before Select, nothing sent); `Select`, then `pollVerify` compares GetSelection's first range with it by CompareEndpoints. `all` selects the DocumentRange itself. The vtable slots and the TextPattern IID are from the SDK headers; the COM side has no offline test, its pure parts (`spanMoved`, `Bounds`, `CheckTextSpan`) do.
- `internal/page/selecttext.go`: `Origin.ReadTextField` (kind, length in UTF-16 units, whether an input type takes a selection) and `Origin.SelectText`: DOM.focus (so type-text then replaces the selection), then `setSelectionRange` on an input or text area, or a document Selection over the text nodes of editable text, read back in the same call (counts as one delivered event in `Input.Sent`, Esc checkpoint before it). A mismatch is a `page.VerifyError`.
- `internal/helper/selecttext.go`: the `select-text` command. A Page Element goes to `pageSelectText` (runPage: no text, an email/number input or a span past the end is refused with nothing sent). Anything else runs `runHeadless` (locked desktop, administrator Window, Ready, passive Esc scope, Agent Cursor labelled `select text "<name>"`) with this act: `ElementWindow`; when that window's class is Edit or RichEdit (`Services.EditControl`), `selectInEdit` reads the length (a failed read sends nothing), checks the span, sends EM_SETSEL (`all` sends 0 to -1) through `Services.SetEditSel` and reads EM_GETSEL back (`editSelectionOK`: exact, except that `all` on a RichEdit accepts 0 to at most the length, because RichEdit counts a paragraph break twice in WM_GETTEXTLENGTH; an end past 65,535 is not judged); otherwise the Text pattern route through the `TextSelector` seam. No route is "nothing was sent" with a hint naming the `--headed click` plus press-key way, since select-text itself has no headed form. The new uia refusals joined `refusedBeforeCall`; `headlessStep` and `pageStep` know select-text; it is registered, serialized on the action gate, and not in `takesHeaded`.
- Protocol: `SelectTextArgs`; `select-text` appended to `BatchStepKinds`. CLI: `internal/cli/selecttext.go` parses `<element> <start> <length>` or `<element> all` (an Element Index, name: or id:, not @x,y; integers 0 to 100,000,000), shared with the Batch step parser; `--headed` is accepted and ignored like the other always-Headless commands. The usage text lists the command and names it in the headless paragraph.
- Tests: `internal/helper/selecttext_test.go` (classic offsets, the caret, `all` with -1, the RichEdit `all` read-back and the plain Edit mismatch, `editSelectionOK` cases, past the end, a failed length read, a failed EM_SETSEL, a failed and a mismatched read-back, the Text pattern route for no native window and a non-edit window, no Text pattern / no selection / past the end / stale / VerifyError / WriteError wording, argument checks touching nothing, the locked desktop, no UIA client, the routing predicate and a Batch in one passive scope, registration; the Page route through the cdptest rig: focus and the span sent, `all`, the three refusals with nothing sent, a mismatched read-back), `internal/uia/textselect_test.go`, `internal/win` (`TestRichEdit`, the live guard), `internal/cli/selecttext_test.go` (parsing, the Batch step, usage exits, usage text), `internal/protocol` (`BatchStepKinds`).
- E2E, written and compiled with `go vet -tags e2e ./test/e2e`, **not run**: `test/e2e/select_text_test.go` (`TestHeadlessSelectTextInClassicEdit` on the covered fixture's Classic Dialog Edit by `id:1001`, select then type-text replaces, `all`, past the end refused; `TestHeadlessSelectTextInNotepad` on the Windows 11 Notepad `Text editor` Element) and new lines in `browser_pages_test.go`'s `pageActions` (a span and `all` on the Page text field, read back through `selectionStart`/`selectionEnd`, type-text replacing the span). Listed in `test/e2e/README.md`. The UIA Text pattern route is unmeasured headless and has no e2e target yet; a provider may still activate its Window on Select. The `id:1001` reference assumes UI Automation gives the dialog's Edit its control ID as AutomationId; the test logs the Window State when that line is missing.
- Docs: CONTEXT.md (select text in the Action list; always Headless), SKILL.md ("Select text" under fields, the Headless list and the command reference), README's command table, and a sentence in codex-parity.md's "extensions beyond Codex" (its status stays extension).
