# 05: Headless Return and Escape on classic dialogs

**What to build:** `press-key <window> Return|KP_Enter|Escape`, with no modifiers, on a `#32770` dialog runs Headless.

- **Return:** DM_GETDEFID, then a posted WM_COMMAND(id, BN_CLICKED).
- **Escape:** a posted WM_COMMAND(IDCANCEL).
- **Stays Headed** when there is no default button, or when the focus is in an ES_WANTRETURN multi-line edit.
- **New Window probe, `Services.KeyRoute`.** Both the routing predicate and the Batch scope use it.
- **Agent Cursor:** it travels to the button.

See spec: "Headless Return and Escape in classic dialogs".

**Blocked by:** none

**Status:** done

- [x] win: dialog key route detection (class, DM_GETDEFID, the focus control style), and posting
- [x] Routing: press-key Headless only for that route; any other key or Window stays Headed; `--headed` forces Headed
- [x] The Batch decides its scope with the Window probe after resolving its Window
- [x] Offline tests with a faked KeyRoute

## Comments

### 2026-11-02: landed

- `internal/win/keyroute.go`: `KeyTarget` (Dialog, HasDefault, DefaultID, Default, Cancel, Focus) and `Control` (HWND, ID, Class, Style, Enabled, Rect, Text) with the pure style reads `IsEdit`, `Writable`, `WantsReturn`, `Password`, `PushButton`. `ProbeKeyTarget` reads only: IsWindow, the focused control (`FocusedControl`: GetGUIThreadInfo hwndFocus, kept only when it is the Window or IsChild of it), the class, DM_GETDEFID through SendMessageTimeout (SMTO_ABORTIFHUNG, 500 ms), GetDlgItem for the default and IDCANCEL controls, their cached captions. `PostDialogCommand` posts WM_COMMAND(MAKEWPARAM(id, BN_CLICKED), control) and refuses in a test binary (pinned in `liveguard_test.go`).
- `internal/helper/keyroute.go`: the pure `dialogKeyRoute` (single key Return, KP_Enter or Escape on a #32770). Return: an ES_WANTRETURN multi-line edit focus stays Headed; a focused enabled push button is pressed itself (the dialog manager makes it the temporary default); else the DM_GETDEFID button, Headed when there is none or it is disabled. Escape: IDCANCEL with the Cancel control or 0, Headed when that button is disabled. `runKeyHeadless` is the rect-based pipeline beside runHeadless: the shared `headlessRefusals` (now extracted from runHeadless: locked, administrator, Ready, all "nothing was sent"), passive Esc scope, `cursorTravelRect` (an empty rect falls back to the Window bounds), act, ripple unless nothing was sent. `keyFailure` maps `notSentError` (nothing was sent), `uia.VerifyError` (delivered) and anything else (may have taken effect). A failed PostMessage is nothing sent.
- Routing: `headlessStep(kind, args, route *win.KeyTarget)`; nil route keeps press-key Headed. `Services.KeyRoute` and `Services.DialogCommand` (defaults `win.ProbeKeyTarget`, `win.PostDialogCommand`); `testServices` fakes KeyRoute with the zero KeyTarget. press-key resolves the Window, probes (never with `headed`), then routes; `takesHeaded` includes press-key. Cursor label `press Return "OK"` (mnemonics stripped by `mnemonicFree`), or `press Escape` at the Window when there is no Cancel control.
- Batch: resolves its Window once before the scope, `stepHeadless` probes once per keyboard step (not for headed steps), and the race is documented in batch.go (a step that changes route at run time is still safe in either scope).
- Protocol `PressKeyArgs.Headed`; CLI `pressKeyRequest` forwards the global flag; usage text updated.
- Tests: `internal/win/keyroute_test.go`, `internal/helper/keyroute_test.go` (decision table, command routing, refusals, Esc stop, predicate, Batch scope and probe counts), `internal/cli/keyboard_test.go`.
- Risks to measure live: whether GetGUIThreadInfo returns the focus of a background thread reliably; GetWindowText of another process's button caption; message boxes whose default is set only by focus.
