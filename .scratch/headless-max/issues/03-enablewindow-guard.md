# 03: EnableWindow guard against self-activation

**What to build:** around a Headless pattern call (Invoke, Select, Expand/Collapse, Toggle) on a host that is known to activate itself, disable the top-level Window, make the call, and enable it again.

cua's Windows driver measured that UWP and Chromium hosts take the foreground on UIA Invoke about 91% of the time, and 0 of 507 times with `EnableWindow(FALSE)` before and `EnableWindow(TRUE)` after (`libs/cua-driver/rust/crates/platform-windows/src/uia/fg_bypass.rs:1-30`, MIT). winuse's `AutoSetFocus = FALSE` fixes the UIA side; this guards against the App itself.

Questions to settle first, with a probe against Windows the probe launches:

- Does any winuse target still self-activate on a Headless click today? Candidates: Calculator (`ApplicationFrameWindow`), Settings, Chrome's tab strip, an Electron app.
- Does the guard break the call? A disabled Window may refuse input-driven handlers, and a modal dialog opened by the click is owned by a disabled Window until it is re-enabled.

Rules if it is adopted:

- Only for an allow-list of host classes (`ApplicationFrameWindow`, `Windows.UI.Core.CoreWindow`, `Chrome_WidgetWin_1`), unless the probe shows it is harmless everywhere.
- Re-enable on every exit path, including a panic and an abandoned UIA worker. On helper start, re-enable any Window winuse disabled (record the HWNDs).
- Never on a Window that was already disabled (a modal owner).

**Blocked by:** none

**Status:** done (detect and report; the live e2e run is still to do: paused while the user games)

- [ ] Probe: self-activation per host, with and without the guard
- [ ] Guard with guaranteed re-enable and start-up recovery
- [ ] Offline tests: allow-list, already-disabled Window skipped, re-enable after an error
- [ ] Opt-in e2e on the probe's measured host

## Comments

### 2026-11-02: a measured self-activation through a dialog

- In the safe probe batch (`../../headless-gaps/probes-safe-batch.md`), a UIA Invoke on the fixture's "Classic Dialog..." button made the fixture show a dialog (`ShowWindow(SW_SHOW)`). The dialog took the foreground 19 ms later, from the user's League client [M].
- This is the same exposure winuse's own Headless click route has today: an App may activate a dialog that the click opens. AutoSetFocus FALSE does not cover it.
- Likely cause [I]: the user's last input went to a process that had since exited (the game), so the foreground lock no longer protected the client.
- The guard here (disable the owner around the call) may not help: the dialog is shown after the call returns. Also test refusing nothing but reporting it ("the App brought a dialog to the foreground"), as ticket 02 does for restore.
- Status stays `needs-triage`; the self-activation question is now answered "yes, it happens", which is what this ticket needed before adoption.

### 2026-11-03: round 3 probes (`../headless-gaps/probes-round3.md`)

- LockSetForegroundWindow(LOCK) from a process without foreground rights returns access denied, so it cannot be a guard.
- The lapse could not be reproduced on the laptop. It needs the last-input receiver to exit, as the League game did on the PC.
- **Proposed new scope: detect and report.** Watch EVENT_SYSTEM_FOREGROUND during and just after a Headless Action, and report `the App took the foreground`. Optionally give the foreground back only when the thief is the target App. The EnableWindow guard is kept only as an unmeasured option for XAML and Chromium Invoke.

### 2026-11-03: triage decision (user)

New scope: **detect and report**, no guard. Watch the foreground (EVENT_SYSTEM_FOREGROUND, or a before/after read as in ticket 02) during and just after a Headless Action. If the target App, or a Window it opened, took the foreground, the output says `ok (the App took the foreground)`. winuse does not take the foreground back: it has no foreground right, as ticket 02 found. LockSetForegroundWindow cannot work without rights (round 3), so the guard is dropped. The EnableWindow variant stays an unmeasured idea and is not built.

### 2026-11-03: done as detect and report (worker, branch hm03-self-activation)

The checklist above was the guard's; the decided scope replaced it. What was built:

- `internal/helper/selfactivation.go`: `watchForeground` wraps every Element, keyboard, Window, tab and page Action, plus `batch` and `invoke-menu`. It does not wrap restore-window (it keeps its own note), activate-window, drag, hover, zoom or the clipboard commands. The wrapper sits outside dp-02's `reportOpened` (`reportAfter` composes them in the registry). Before the Action it reads the foreground and the target Window's process; after the Action and its opened-Window settle, it checks whether the foreground moved from another process's Window to one of the target's process. If an Action opened a Window, it looks once more `openedPoll` (50 ms) later, because the probe's dialog took the foreground 19 ms after showing.
- Note: `Note: the App took the foreground; winuse does not take it back.`, the same line `launch-app` prints, appended after the ok and any other notes. The ticket comment wrote `ok (the App took the foreground)`; I used the Note line for consistency with launch-app and dp-02's notes.
- `runAction` marks the request Headed (`markHeaded`, one line in actions.go), so a Headed Action, or a Batch with any Headed step, never reports its own activation. A failed Action keeps its error alone. A Window whose process cannot be read is not watched, which covers the offline tests' fake Windows.
- No guard, no foreground taken back. ADR 0011 amendment; ADR 0013 row 03 done; SKILL.md ("Read the result": tell the user, do not repeat); codex-parity process-model row (status unchanged).
- Tests: `internal/helper/selfactivation_test.go` (dialog/target took it, nothing moved, another App took it, the App already had it, Headed click, mixed and all-Headless Batch, failed Action, a dialog that activates after it opened, the command list). Opt-in e2e `TestHeadlessClickReportsSelfActivation` (`test/e2e/self_activation_test.go`): the covered fixture's Classic Dialog click must say so exactly when the fixture holds the foreground afterwards; it logs which case ran, because the lapse cannot be set up on demand. Compiled, NOT run.

