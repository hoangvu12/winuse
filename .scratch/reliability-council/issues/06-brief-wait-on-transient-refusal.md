# 06: Wait briefly on a transient refusal instead of failing at once

Status: ready-for-agent (user, 2026-11-07: implement all 13; ask the user at the decision points in 08, 09, 12 and 13)

**What's wrong:** a Headless Action fails at once when the Element is disabled (`wm:internal/uia/value.go:887-888`, `wm:internal/helper/headless.go:630-633`), when the Window is blocked by a modal dialog that may be closing (`wm:internal/win/activate.go:84-97`), or when a `name:` reference has not appeared yet. A "Next" button that enables 300 ms later costs the agent a full turn and a re-read. `wait <window> <element> enabled` exists (`wm:internal/helper/waits.go`, desktop-parity 03), but the agent has to know to use it.

**agent-desktop:** polls actionability up to 5 s by default and fails fast on permanent errors (`ad:crates/core/src/ref_action/ref_action_poll.rs:28-101`). It polls outside its lease and re-checks once it holds it, and retries only when the failure proves nothing was delivered (`ad:crates/core/src/ref_action/ref_action_wait.rs:80-143`, Reviewer 2).

**What to build:**

- Only for refusals that sent nothing: disabled, modal dialog closing, name not found yet.
- Poll for 1.5-2 s (not 5 s) **before** taking the Window lane, then act under the lane after one more check. Never wait while holding a lane (ADR 0014).
- Never replay a write whose outcome is uncertain (ticket 02).
- Don't copy agent-desktop's visible or unoccluded checks for Headless Actions: acting on covered Windows is the point of Headless.
- The result says so: "waited 0.4 s for 14 to enable".
- An opt-out (`--no-wait`, or `--wait 0`) for agents that want the old behaviour.

**Blocked by:** 02 (the poll must know which failures are safe to retry)

- [ ] Pre-dispatch poll for the three refusals, outside the lane
- [ ] Note in the result; opt-out flag
- [ ] Offline tests: enables within the wait; stays disabled (old error, after the wait); dialog closes; name appears; the lane is not held while polling
- [ ] SKILL.md: the "disabled" advice and the waits section

**Verified by the orchestrator:** the immediate refusals at the cited lines. Inferred: the turn saved per case; the right timeout.

**Conflicts:** SKILL.md's current advice for a disabled Element; ADR 0014 lanes (respected by polling outside). It may touch `internal/helper/state.go` and command wrapping, which the pending perf change also touches. Must not bring back a fixed post-Action wait.
