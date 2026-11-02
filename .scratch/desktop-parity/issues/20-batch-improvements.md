# 20: Batch improvements

**What to build:**

- Each step's output is printed (prefixed with the step number) instead of only `ok`, so `eval`, `tab list` and the notes from ticket 02 come back.
- Like agent-desktop (user decision, 2026-11-03): a Batch runs past a failed step by default, reports every failure at the end and exits 1 if any failed. `--stop-on-error` stops at the first failure (today's behaviour).
- Batch from stdin (`winuse batch -`), as agent-desktop PR #439 adds.

Today: "A Batch prints only ok" (`skills/winuse/SKILL.md:471-473`).

- Steps that failed with "may have taken effect" or "delivered; do not repeat" do not stop the Batch either (agent-desktop's behaviour); the final report marks them as uncertain.

**Blocked by:** none

**Status:** ready-for-agent (rewrite SKILL.md's Batch rules)

- [ ] Per-step output
- [ ] Continue by default, `--stop-on-error`, and the final report
- [ ] stdin batches
- [ ] Offline tests
- [ ] SKILL.md Batch section

## Comments

### 2026-11-03: triage decision (user)

Do it like agent-desktop: per-step output, continue past failures by default (uncertain ones too, marked in the report), `--stop-on-error`, and a Batch from stdin. This changes today's default (stop at the first failure); SKILL.md's Batch section is rewritten.
