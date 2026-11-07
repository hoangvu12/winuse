# 12: Chromium wrappers spend the depth and the character cap (measure first)

Status: ready-for-human (2026-11-07: waits on the user: the measurement needs a live read)

**What's wrong (likely):** on a Chromium or Electron Window without a Browser Connection, depth counts every level, anonymous wrappers included: the walk (`wm:internal/uia/walk.go:279-286,327`), the skeleton's 3 levels (`wm:internal/uia/region.go:19-21`) and `--depth`. In the perf benchmark Discord's skeleton was 0.3 KB in winuse against 1.8 KB in agent-desktop, so the skeleton is probably all shell, and the full read hit the 28,000-character default cap (`.scratch/perf/r3.log`, `wm:internal/helper/views.go:30`). `--compact` exists but is opt-in.

**agent-desktop:** under Chromium, an unnamed Group, Custom or Pane with no value, no AutomationId and no action costs no logical depth (`ad:crates/windows/src/tree/walk/wrapper.rs:32-52`; "Web Wrapper", `ad:CONCEPTS.md`). It counted 54 of 81 such nodes on settled Chromium; skipping them recovers about a third of the depth (`ad:probes/windows/FINDINGS.md:231`, A16-8).

**What to build:**

1. Measure first, offline from stored reads or on the user's OK: how deep Discord's or VS Code's content is in raw versus logical levels, and how much of the cap wrappers use.
2. If it pays: count logical depth under a `Chrome_` root class for the skeleton and `--depth`, and fold bare containers before `--max-chars` cuts lines, as `--compact` does.
3. Keep the raw safety caps (nodes, time, depth 48).

**Blocked by:** none

- [ ] Measurement (two Electron apps, raw vs logical depth, chars spent on wrappers)
- [ ] If worth it: logical depth for skeleton and `--depth`; fold-before-cut

**Verified by the orchestrator:** the depth counting and agent-desktop's predicate. Inferred: the gain. Reviewer 2 rates it a limited win (it saves logical depth, not traversal), and winuse noted the research already (`wm:.scratch/browser-merge/agent-desktop-research.md:92`).

**Conflicts:** the skeleton definition (desktop-parity 04) and ADR 0004's default-budgets addendum.

## Comments

### 2026-11-07: not started (orchestrator)

- Nothing built: step 1 is a measurement, and no stored read has the trees it needs (`.scratch/perf/` keeps sizes only: `r2-baseline.json`, `r3-fixed.json`, `r3.log`). It needs live reads on the user's desktop, which agents don't start on their own.
- This PC has Discord but no VS Code (checked 2026-11-07), so only one of the ticket's two Electron apps can be measured here.
- Asked the user: run the Discord measurement (read-only), and when, or skip the ticket.
