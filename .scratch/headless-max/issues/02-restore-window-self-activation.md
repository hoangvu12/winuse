# 02: restore-window reports self-activation

**What to build:** `restore-window` compares the foreground before and after the restore, and says so when the App brought itself to the front.

Measured (`../headless-gaps/headless-100.md` section 0): charmap.exe, minimized, then restored with ShowWindow(SW_SHOWNOACTIVATE) from a background process, became the foreground in 6 of 6 runs. SetWindowPlacement, a temporary WS_EX_NOACTIVATE, EnableWindow(FALSE) around the call and a 10 s wait did not prevent it. The probe's own plain window stayed in the background. So the call is non-activating, but some Apps activate themselves on restore. `restoreWindow` (`internal/helper/windowactions.go:57`) only checks the iconic state.

- After the read-back, compare the foreground. If the restored Window (or another Window of its App) is now the foreground and was not before: `ok (the App brought itself to the foreground)`.
- Do not try to give the foreground back: that needs SetForegroundWindow, which winuse cannot do without the foreground right, and it would fight the user.
- Reword ADR 0011 Decision 2 and its consequences: the calls are non-activating; the outcome is not guaranteed.

**Blocked by:** none

**Status:** done

- [x] Foreground before and after in `restoreWindow`, behind a Services fake
- [x] Output note; offline test for both outcomes
- [x] Opt-in e2e: charmap (a `#32770` dialog App) minimized, restored, the note appears; the fixture restored, no note
- [x] ADR 0011 wording and SKILL.md "Manage Windows"

## Comments

### 2026-11-02: landed

- `internal/helper/state.go`: two new Services hooks, `Foreground` (`win.ForegroundWindow`, GetForegroundWindow) and `WindowPID` (`win.WindowPID`, GetWindowThreadProcessId), both read-only, filled by `defaultServices`.
- `internal/helper/windowactions.go`: `restoreWindow` reads the foreground just before `RestoreWindow` and again after the iconic read-back. `selfActivated` is true when the foreground changed, is now the restored Window or another Window of its process (`sameApp`, by raw PID), and was not one of them before. The output is then `ok (the App brought itself to the foreground)`; with the existing normal-size note the two are joined in one line, `ok (restored at normal size: ...; the App brought itself to the foreground)`. Nothing tries to give the foreground back. An already-normal Window still sends nothing and reads no foreground. The package comment now says the calls are non-activating but the outcome is not guaranteed.
- Tests (`internal/helper/windowactions_test.go`): the `windowSpy` fakes the foreground, a self-activating restore and per-Window PIDs. `TestRestoreWindowReportsSelfActivation` covers the restored Window taking the foreground, another Window of its App taking it, the foreground staying, another App taking it, the App already having it (no note) and no foreground before; each checks winuse never activates. `TestRestoreWindowJoinsTheNotes` covers both notes together.
- E2E (`test/e2e/window_actions_test.go`): `TestRestoreWindowReportsSelfActivation` starts its own charmap, covers it with a fixture, minimizes and restores it, and checks that the note matches the foreground read back afterwards; if charmap stays in the background on a run, the output must be plain `ok` and the test skips with that reason. The fixture leg ("no note") is the existing `TestWindowActionsKeepTheForeground`, whose `assertActionOK` pins exactly `ok`; its comment says so. Compiled with `go vet -tags e2e ./test/e2e`; **not run**. `test/e2e/README.md` lists it.
- Docs: ADR 0011 Decision 2 says the calls are non-activating and the outcome is not guaranteed, with a new consequence on self-activation and why winuse does not take the foreground back. SKILL.md "Manage Windows" gains the note and what to do with it, and the `is minimized` recovery no longer says restore-window "leaves the foreground alone". The CLI usage line for restore-window mentions the note. CONTEXT.md and docs/codex-parity.md needed no change (ticket 17 owns the CONTEXT.md wording).
- Limit: the after-read happens at the iconic read-back. An App that activates itself later than that is not reported; the e2e test would show that as a mismatch.
