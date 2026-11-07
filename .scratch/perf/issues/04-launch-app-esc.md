# 04: launch-app stops only on Esc, with the right message

Status: done (branch perf-catch-up)

**What to build:** `launch-app` arms the Esc hook fully (`internal/helper/commands.go:182`). A full arm is the Headed scope (ADR 0007 as amended by ADR 0014): any physical key, mouse button or wheel latches the stop. launch-app takes no foreground and sends no input, so it should be Headless: a passive arm, where only a physical Esc stops it. It also reports any stop with `escStopError()` ("stopped by the user with the physical Escape key") instead of `stopErrorFor`, which names a yield correctly.

Seen twice on 2026-11-07: `launch-app Calculator` stopped with the Escape message while the user was away from the keyboard (some physical input reached the machine).

**Blocked by:** none

- [x] `ArmPassive`/`DisarmPassive` in launch-app
- [ ] Offline test: a yield-style input during launch does not stop it; Esc does

## Comments

### 2026-11-07: landed (branch `perf-catch-up`)

`launch-app` brackets itself with `armPassive(esc)` (`internal/helper/commands.go`), like close-app and the Headless Actions. Test: `TestLaunchAppArmsPassively` (the scopes are arm-passive/disarm-passive, never arm). Not run live.
