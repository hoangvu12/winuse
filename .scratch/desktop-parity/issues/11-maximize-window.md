# 11: maximize-window

**What to build:** `maximize-window <window>`. agent-desktop has `maximize`.

Windows has no non-activating maximize (ADR 0011 consequences): `ShowWindow(SW_MAXIMIZE)` activates. Options:

1. **Headed Window Action** (research recommendation): requires `--headed`, activates, `SW_MAXIMIZE`, verifies `IsZoomed`. Without `--headed`: "nothing was sent", hint to pass `--headed`, or to use `move-window` and `resize-window` to the monitor's work area instead.
2. **Headless "fill the work area"**: `SetWindowPos(SWP_NOACTIVATE)` to the monitor's work area. Looks maximized, is not (no maximized state, borders stay). Could be `resize-window <window> --fill`.


**Blocked by:** none

**Status:** done

- [x] The chosen form(s) in CLI, protocol, helper
- [x] Offline tests
- [x] Opt-in e2e on the fixture
- [x] CONTEXT.md, ADR 0011 amendment, SKILL.md

## Comments

### 2026-11-03: triage decision (user)

Do it like agent-desktop: a real maximize (option 1). It activates, so it is a Headed Window Action and needs `--headed`; without it, "nothing was sent" with the hint. Option 2 is not built.

### 2026-11-03: landed (option 1)

`winuse --headed maximize-window <window>`, the one Headed Window Action.

- Helper (`internal/helper/windowactions.go`, `maximizeWindow`): resolves the Window; one already maximized (and not minimized) prints `ok` with nothing sent, headed or not. Without `headed`: `window N cannot be maximized headless: Windows has no maximize that leaves the foreground alone; nothing was sent`, hint `retry with --headed ... winuse --headed maximize-window N; or keep the foreground and fill the monitor's work area with winuse move-window and winuse resize-window ...`. With `headed`: the Headed pipeline (`runAction`: locked and admin refusals, banner, swallowing Esc scope, activation, which restores a minimized Window), then `ShowWindowAsync(SW_MAXIMIZE)`, polled through `IsZoomed` for up to `placementVerifyWait`. A failed read-back says the maximize was delivered; a failed call says the Window was brought to the foreground (no "nothing was sent", the activation already happened). Not a Batch step; serialized on the action gate.
- Win32 (`internal/win/placement.go`): `Maximize` (SW_MAXIMIZE through the existing `showAsync`, LiveDesktopGuard included).
- Protocol: `MaximizeWindowArgs{Window, Headed}`. Services: `MaximizeWindow` (default `win.Maximize`). Registry: `maximize-window` in `CommandsWith`.
- CLI (`internal/cli/window.go`, `cli.go`): `maximizeWindowCmd` forwards the global `--headed`; usage line and the headless paragraph of the usage text.
- Docs: CONTEXT.md (Window Action includes maximize; "Every Window Action except maximize is always Headless"; maximizing is in the always-Headed list), ADR 0011 amendment (and its Status line), ADR 0013 "What stays impossible" names maximize-window, SKILL.md (Headless and Headed lists, the `--headed` paragraph, Manage Windows, command reference), README command table, `docs/codex-parity.md` extensions, `test/e2e/README.md`.

Tests:

- Offline, `internal/helper/windowactions_test.go`: `TestMaximizeWindowWithoutHeadedSendsNothing` (normal and minimized), `TestMaximizeWindowHeadedActivatesThenMaximizes` (activation before the maximize, banner, `arm`/`disarm` scope, polled; normal and minimized), `TestMaximizeWindowAlreadyMaximizedSendsNothing`, `TestMaximizeWindowUnverified` (delivered read-back failure, call failure), `TestMaximizeWindowRefusalsComeBeforeTheActivation` (locked, admin, missing Window), `TestMaximizeWindowIsSerializedAndNoBatchStep`; `registry_test.go` lists it. `internal/cli/window_test.go`: `TestMaximizeWindowCarriesHeaded` (flag before or after), usage errors, usage text.
- E2E, `test/e2e/window_actions_test.go`: `TestMaximizeWindowNeedsHeaded`. On a covered fixture: unheaded refused with nothing sent and the retry, the fixture not zoomed, the cover keeps the foreground; `--headed` prints `ok`, the fixture is zoomed and in the foreground; `restore-window` returns it to its earlier size.

Ran: `go build ./...`, `go vet ./...`, `go test ./...`, `go vet -tags e2e ./test/e2e` all pass. Live on the laptop (2026-11-03, under the `.live-lock`): `WINUSE_E2E=1 go test -tags e2e -run '^TestMaximizeWindowNeedsHeaded$' ./test/e2e` passed (9.2 s). The fixture has no WS_MAXIMIZEBOX; SW_MAXIMIZE maximized it anyway.
