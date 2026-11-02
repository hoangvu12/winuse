# 09: Opt-in e2e tests

**What to build:** e2e tests under `test/e2e` for the spec's Testing decisions.

- Use build tag `e2e` and `WINUSE_E2E=1`, and follow the existing headless_test.go style.
- Add fixture-app features if needed: a message box, a scrollable list, an edit box.
- Write and compile the tests only, with `go vet -tags e2e ./test/e2e`. Never run them. The user runs them.

**Blocked by:** 01-07

**Status:** done

- [x] Tests compile under the e2e tag
- [x] test/e2e/README.md lists the new tests and what each needs

## Comments

### 2026-11-02: landed (written and vetted, not run)

- Fixture: two main-window buttons, **Classic Dialog...** and **Message Box...** (`ElemButtonClassic`, `ElemButtonMessageBox`, in the checkbox row). Both post a WM_APP message, so a UI Automation Invoke returns before the window opens. `internal/fixture/classic.go` builds a modeless `#32770` dialog from an in-memory DLGTEMPLATE: a name Edit (ID 1001, focused on open), a password Edit (1002, ES_PASSWORD), OK (IDOK, BS_DEFPUSHBUTTON) and Cancel (IDCANCEL). It is owned by the main window, opens inside its frame, and the message loop runs IsDialogMessage for it. The MessageBox is MB_OKCANCEL, owned by the main window. New state fields: `classicOpen`, `classicWindowId`, `classicResult`, `classicText`, `classicPassword`, `classicFocus`, `messageBoxOpen`, `messageBoxResult`. The selected-item fill is exported as `SelectedItemCOLORREF`. Offline tests in `classic_test.go`: the template is parsed back (header, DWORD alignment, IDs, class ordinals), the tab order and control styles, the result and focus names, and the new buttons have no overlapping rectangle. The state JSON key test lists the new fields. The list already had Scroll and ScrollItem, so it needed no change.
- e2e: `headless_parity_test.go` holds the helpers and tests the screenshot, zoom and set-value on/off cases. The other files are `window_actions_test.go`, `element_scroll_test.go`, `classic_keys_test.go` (Return/Escape on the Classic Dialog and the MessageBox, type-text into the Edit, the password Edit and Windows 11 Notepad) and `posted_keys_test.go` (posted keys into the Edit, refusals on a blocked or minimized Window, the Chrome measurement). Every covered test checks the foreground after the Action. The capture test tells the target from the cover by the selected list item's colour.
- The password test reaches the password Edit with a posted Tab, which the fixture's IsDialogMessage turns into a focus move. So it also depends on posted keys working.
- The Notepad test skips while Notepad is running, because it kills the Notepad process at the end. It also skips unless the focused control is RichEditD2DPT. Notepad's session restore may keep the temp tab; the README says so.
- The Chrome test opens `about:blank` with an isolated profile, sets the address bar with set-value, posts Return and logs a `MEASURED:` line. It fails only on winuse's own output or a foreground change.
- The route-dependent tests log `win.ProbeKeyTarget` first (focus class and style, default button). That covers the live risks from tickets 05-07: a background thread's focus, the MessageBox default button, cross-process EM_REPLACESEL into RichEditD2DPT, and Chrome and posted keys.
- `go build ./...`, `go vet ./...`, `go test ./...` and `go vet -tags e2e ./test/e2e` pass. Nothing was run against the desktop.
