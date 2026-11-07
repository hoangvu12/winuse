# 10: A plain get-window-state cannot print a diff

Status: ready-for-agent (user, 2026-11-07: implement all 13; ask the user at the decision points in 08, 09, 12 and 13)

**What's wrong:** `--observe=diff` works after an Action, but `checkObserve` refuses it on anything that is not an Action or Batch (`wm:internal/helper/observe.go:56-72`). Re-reading a Window after a `wait`, after the user did something, or in a loop that polls one Window, the agent pays for the whole tree again.

**cua:** `get_window_state` takes `since: <snapshot_id>` and returns only the changed rows (`cua-w:tools/get_window_state.rs:150-157`).

**What to build:** `get-window-state <window> --diff`, reusing `wstate.RenderChanges` and ADR 0021's rules: the same index-based diff, the same 60% fall-back to the whole State, the whole State when there is no earlier read by this Agent. Not cua's "reindexed" form (ADR 0021, Considered).

**Blocked by:** none

- [ ] `--diff` on get-window-state, sharing observe's renderer and budgets
- [ ] Offline tests: no earlier read, small change, large change
- [ ] SKILL.md and help; codex-parity.md extensions paragraph

**Verified by the orchestrator:** `observe.go:56-72`. One reviewer (1); the orchestrator agrees: small, and ADR 0021's machinery is already there.

**Conflicts:** extends ADR 0021 Decision 1 (diff after an Action only); needs a line there.
