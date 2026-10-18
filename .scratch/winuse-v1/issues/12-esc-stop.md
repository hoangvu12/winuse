# 12 — Esc stop

**What to build:** Pressing the physical Esc key while winuse is acting immediately stops the current Action or Batch. The helper watches with a low-level keyboard hook on its own OS-locked thread with a message loop, ignoring injected keystrokes (including winuse's own). A stopped command exits with Codex's interrupt code (130) and says the user pressed Esc, so the agent stops and asks. See spec: Control.

**Blocked by:** 11 — `name:` / `id:` references + Batches

**Status:** done

- [x] Physical Esc aborts the in-progress Action or Batch; remaining Batch steps do not run
- [x] Injected Esc (e.g. `press-key Escape`) does not trigger the stop
- [x] Stopped commands exit 130 with a message saying the user stopped winuse
- [x] End-to-end: a long Batch against the fixture is interrupted by pressing Esc (verified by user run)

## Comments

- 2026-10-18 — live e2e run (Windows 11, 100% scaling, primary monitor, after the live-fixes merge into winuse-v1): TestInjectedEscDoesNotStopTheBatch passed. TestEscStopsTheLongBatch (the physical Esc press) has not been run yet; it needs the user at the keyboard.
- 2026-10-18 — TestEscStopsTheLongBatch passed with the user pressing the physical Esc key: the long Batch stopped (exit 130, stop-and-ask message), typing aborted as a prefix, the app never saw Escape, and the overlay was gone.
