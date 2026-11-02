# 06: Headless type-text into classic edit controls

**What to build:** `type-text` runs Headless when both of these hold:

- the Window's focused control (GetGUIThreadInfo hwndFocus, inside the Window) is an enabled, writable Edit, RichEdit or RichEditD2DPT;
- the text has no control characters.

The route is SendMessageTimeout(EM_REPLACESEL, TRUE). It is verified through WM_GETTEXTLENGTH: the new length must equal the old length, minus the selection, plus the text length.

Anything else stays Headed. This ticket uses the KeyRoute probe from ticket 05.

See spec: "Headless typing into classic edit controls".

**Blocked by:** 05 (the KeyRoute probe)

**Status:** done

- [x] win: focused-control probe and the EM_REPLACESEL insert with length verification
- [x] Routing and Batch scope through KeyRoute; `--headed` forces Headed
- [x] The cursor label is `type` and never shows the text
- [x] Offline tests: control characters route Headed, the verification arithmetic, password edits never echoed

## Comments

### 2026-11-02: landed

- `internal/win/keyroute.go`: `EditState` (Length, SelStart, SelEnd, SelKnown) and `ReadEditState` (WM_GETTEXTLENGTH and EM_GETSEL through SendMessageTimeout, SMTO_ABORTIFHUNG, 2 s; the text itself is never read). EM_GETSEL is sent without pointers and decoded from the return value (`editSelectionOf`); -1 (either end past 65,535) leaves the selection unknown. `ReplaceSelection` sends EM_REPLACESEL(TRUE, text) with the same timeout; the comment notes all three messages are below WM_USER, so Windows marshals the string across processes. It refuses in a test binary (pinned in `liveguard_test.go`). The focused-control probe is ticket 05's `FocusedControl` (GetGUIThreadInfo hwndFocus, the Window or a descendant), with `Control.Writable`.
- `internal/helper/keyroute.go`: the pure `editTypeRoute` (non-empty text, no C0 character, a writable Edit/RichEdit20A/RichEdit20W/RICHEDIT50W/RichEditD2DPT focus) and `insertLengthOK` (after == before - |selection| + UTF-16 units; with an unknown selection, after is between the units and before + units). `typeIntoEdit` runs on `runKeyHeadless`: cursor `type` at the control's rectangle, state before (failure: nothing was sent), EM_REPLACESEL (failure: may have taken effect), state after (failure or mismatch: a `uia.VerifyError` with its own Msg, delivered, do not repeat). No message, hint or label carries the text, so a password edit is never echoed.
- Routing: `headlessStep` handles type-text with the probe; `Services.EditState` and `Services.ReplaceSel` (defaults `win.ReadEditState`, `win.ReplaceSelection`); type-text resolves, probes (not with `headed`), routes; `takesHeaded` includes type-text, so a headed Batch injects it. The Batch's per-step probe from ticket 05 covers type-text.
- Protocol `TypeTextArgs.Headed`; CLI `typeTextRequest` forwards the global flag (still literal inside the text); usage text updated.
- Tests: `TestEditSelectionOf` (win), `TestInsertLengthOK`, `TestEditTypeRoute`, `TestTypeTextIntoAnEditIsHeadless`, `TestTypeTextStaysHeadedOffTheEditRoute`, `TestTypeTextIntoAnEditFailureWording` (a password edit's text never appears), `TestHeadlessStepTypeText`, `TestBatchTypeTextIntoAnEditKeepsTheHeadlessScope`, and `TestHeadedReachesTypeTextRequests` (cli).
- Risks to measure live: RichEdit counts a paragraph break as 2 in WM_GETTEXTLENGTH but 1 in EM_GETSEL, so replacing a selection that spans a break reports a false VerifyError; an ANSI RichEdit20A may count bytes for double-byte text; Windows 11 Notepad's RichEditD2DPT must answer EM_REPLACESEL across processes and keep its hwndFocus while in the background.
