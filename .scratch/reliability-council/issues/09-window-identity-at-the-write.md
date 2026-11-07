# 09: Window identity: re-check at the write; the shared-record gap

Status: ready-for-agent (user, 2026-11-07: implement all 13; ask the user at the decision points in 08, 09, 12 and 13)

**What's wrong:**

1. The identity check runs once, when the command starts (`wm:internal/helper/identity.go:115-148`), before the lane wait and the read. A Window that closes and hands its handle to a new one while the command waits or reads is not caught (Reviewer 2).
2. The records are shared by all agents, and any agent's list or read replaces them (`identity.go:12-17,74-98`). An agent holding an old ID passes the check after another agent has listed the new Window. Keys, typing, point Actions and `close-window` can land on the wrong Window. The records are also lost when the helper exits after 5 idle minutes; the first Action after that records whatever is there and passes (`identity.go:142-143`).

**agent-desktop:** "Window Identity" is handle plus process creation time, checked against what that observation recorded, and re-checked around the write ("Identity Sandwich", `ad:CONCEPTS.md`). Its window inventory brackets the read with identity checks (`ad:crates/windows/src/system/window/window_ops.rs:130-176`, Reviewer 2). That every write is bracketed is documented, not checked across all its actions.

**What to build:**

- Carry the identity seen at the command's start through the lane, and check it again right before an HWND write (keys, typing, point, close). Cheap: one GetWindowThreadProcessId plus the process start time.
- Optional, needs the user: per-agent records next to each agent's Window State.

**Blocked by:** none

- [ ] Re-check before HWND writes, with an injected handle reuse test
- [ ] Decision: per-agent records or not (ADR 0014)

**Verified by the orchestrator:** `identity.go` as cited; ADR 0014's note.

**Conflicts:** **ADR 0014** (lines 129-134) accepts the shared-record gap on purpose: "the shared design was chosen". The second part reverses it and needs an ADR amendment. The first part (re-check at the write) does not conflict. Late opened notes from the pending perf change should carry the original identity, not a later lookup.
