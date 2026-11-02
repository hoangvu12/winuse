# 20: Batch improvements

**What to build:**

- Each step's output is printed (prefixed with the step number) instead of only `ok`, so `eval`, `tab list` and the notes from ticket 02 come back.
- `--continue`: run past a failed step, report every failure at the end, exit 1 if any failed. Default stays stop-at-first-failure.
- Batch from stdin (`winuse batch -`), as agent-desktop PR #439 adds.

Today: "A Batch prints only ok" (`skills/winuse/SKILL.md:471-473`).

- Open question for triage: should `--continue` stop after a step that failed with "may have taken effect" or "delivered; do not repeat"? Proposal: it continues after any failure, and the final report marks those steps as uncertain.

**Blocked by:** none

**Status:** needs-triage (SKILL.md Batch rule; decide `--continue` semantics after uncertain steps)

- [ ] Per-step output
- [ ] `--continue` and the final report
- [ ] stdin batches
- [ ] Offline tests
- [ ] SKILL.md Batch section
