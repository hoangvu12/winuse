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

### 2026-11-03: why charmap stayed behind in the e2e runs, and a reliable test

The e2e test skipped in both full runs of 2026-11-03 ("charmap did not bring itself to the foreground"). Throwaway diagnostics (deleted) measured why, on this PC (ForegroundLockTimeout is 2147483647 ms here, so the lock never times out):

- **The self-activation is Windows', not charmap's, and happens once.** An App takes the foreground on its first restore only if it has not been the foreground since it started:
  - charmap started behind an active cover, then minimized and restored by winuse: activated itself 3 of 3 (and 15 of 15 in earlier runs where it started in the background). With one `activate-window` on charmap (then the cover again) before the minimize: 0 of 3.
  - The plain fixture, which has no activation code at all, did the same: in one batch it started behind the cover, and `TestWindowActionsKeepTheForeground` failed 3 of 3 with `ok (the App brought itself to the foreground)`. Activated once before the minimize: 0 of 3.
- **Why the e2e runs skipped:** run after `TestWindowActionsKeepTheForeground` (as in the full suite), charmap took the foreground **at launch** (3 of 3, and 4 of 4 in a second batch), so it had nothing left to take on restore. Whether a process the test starts may take the foreground at launch depends on the desktop's input state. When `activate-window` had just injected its input (GetLastInputInfo idle ≈ 30 ms), charmap opened behind the cover. When the cover had taken the foreground at its own launch, so `activate-window` injected nothing (idle in minutes), every process the test started took the foreground at launch: the cover, then charmap. The state flipped between batches on its own, run alone included (pass 3 of 3, then fail 4 of 4).
- Hypotheses ruled out: the foreground lock timeout (never expires here); `ShowWindowAsync` vs the probe's `ShowWindow` (both activate charmap in the same state); restoring from another process (the test process restoring directly behaves like winuse); timing (charmap was in front 3-7 ms after `restore-window` returned, and still with 5 s and 20 s waits before the restore).
- Neither launch mode kept charmap behind: STARTUPINFO `SW_SHOWNOACTIVATE` (3 of 3 still in front at launch) or `SW_SHOWMINNOACTIVE` (5 of 5 iconic but in front). Activating the cover before starting charmap worked in one input state (6 of 6) and not in the other (8 of 8 in front at launch).
- A fixture that calls SetForegroundWindow on itself when restored is refused in the other state: opened with `SW_SHOWNOACTIVATE` after two injected activations, it stayed behind 3 of 3; opened normally, it took the foreground at launch, and once the cover had taken it back its restore was refused 3 of 3.
- **The reliable setup:** Windows lets a background process set the foreground when the foreground process grants it (AllowSetForegroundWindow). The fixture gained two flags (`internal/fixture.Options`): `-activate-on-restore` (SetForegroundWindow on itself in WM_SIZE when restored from minimized) and `-allow-foreground <pid>` (AllowSetForegroundWindow(pid) on each WM_ACTIVATEAPP activation). `TestRestoreWindowReportsSelfActivation` now restores an `-activate-on-restore` fixture under a cover that grants its PID: 3 of 3 alone, 4 of 4 after the Keep test, 3 of 3 with two injected activations first (the state that refused the plain call). The App still brings itself forward; the cover is the user's stand-in. winuse's restore is unchanged and grants nothing. charmap is no longer the target. `coverWindow` takes extra fixture flags.
- `TestWindowActionsKeepTheForeground` activates its fixture once before covering it, so its "no note" leg cannot meet the first-restore activation: 16 of 16 passes since.
- The note is still right for real Apps: the most likely case is an App started in the background (`launch-app`, or by the user) and restored before it was ever in front. ADR 0011's consequence and SKILL.md "Manage Windows" now say so.
