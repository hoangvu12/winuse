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

**Status:** needs-triage (probe first; adopt only if a self-activation is measured)

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
