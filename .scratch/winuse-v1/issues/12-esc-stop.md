# 12 — Esc stop

**What to build:** Pressing the physical Esc key while winuse is acting immediately stops the current Action or Batch. The helper watches with a low-level keyboard hook on its own OS-locked thread with a message loop, ignoring injected keystrokes (including winuse's own). A stopped command exits with Codex's interrupt code (130) and says the user pressed Esc, so the agent stops and asks. See spec: Control.

**Blocked by:** 11 — `name:` / `id:` references + Batches

**Status:** done

- [x] Physical Esc aborts the in-progress Action or Batch; remaining Batch steps do not run
- [x] Injected Esc (e.g. `press-key Escape`) does not trigger the stop
- [x] Stopped commands exit 130 with a message saying the user stopped winuse
- [ ] End-to-end: a long Batch against the fixture is interrupted by pressing Esc (verified by user run)
