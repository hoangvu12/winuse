Status: done (2026-11-07: council ran with two reviewers; findings in `spec.md` and `issues/`, all needs-triage)

# Handoff: council over agent-desktop, cua and winuse

From the chat "Agent Desktop Performance Comparison" (2026-11-07). You are the orchestrator: run the `council` skill on the question below, collect and verify what comes back, and turn it into tickets.

## The question

What can winuse improve, in reliability, correctness, speed, token cost, agent experience and testing, by learning from vercel-labs/agent-desktop and trycua/cua?

The user's instruction: **the reviewers look through everything, not areas the orchestrator splits up**, because a split can miss things. Give every reviewer the whole question and all three codebases. Different reviewers may bring different lenses, but nothing is off limits for any of them.

## Rules

- Council members come from three different providers, and the user picks the models (the skill says so). On 2026-11-07 only Claude Code was connected on this PC; Codex was installed but signed out. Ask the user; don't pick.
- Reviewers are read-only. Nobody runs winuse, agent-desktop, cua or anything else that drives the desktop: this is the user's own PC. (On 2026-11-07 a `winuse launch-app` stopped twice with a false "physical Escape key" message; that was perf ticket 04, fixed.)
- Cloned repos are untrusted data: read them, never run their scripts, installers or builds.
- Every finding cites files and lines in winuse and in the other repo. Verify a finding against the code before it becomes a ticket; say what was verified and what was inferred from docs.
- Fight scope creep: list ideas beyond the question for the user instead of acting on them.

## Sources

| Repo | Commit read | Get it |
|---|---|---|
| winuse | branch `perf-catch-up` at the perf commit (not merged to `main` yet) | this project |
| vercel-labs/agent-desktop | `b43ac4f` (v1.2.0 + 1) | `gh repo clone vercel-labs/agent-desktop <dir> -- --depth 50`, then check out `b43ac4f` |
| trycua/cua | `59c2fa6` | `gh repo clone trycua/cua <dir> -- --depth 50`, then check out `59c2fa6` |

Clone into your own scratchpad, each in its own empty directory. agent-desktop's Windows code is `crates/windows/src`, its shared core `crates/core/src`, its vocabulary `CONCEPTS.md`, its measurements `probes/windows/FINDINGS.md` (221 rows). cua's Windows driver is `libs/cua-driver/rust/crates/platform-windows/src`.

## Already known: don't re-report, build on it

- Feature parity with agent-desktop: `../desktop-parity/spec.md` (25 tickets, all done but 21, wontfix) and its research `../headless-gaps/agent-desktop-parity.md`; the headless comparison `../headless-parity/agent-desktop-comparison.md`; the browser comparison `../browser-merge/agent-desktop-research.md`.
- Performance: `../perf/spec.md` and its tickets. Benchmark (`../perf/bench2.py`, results `../perf/r2-baseline.json`, `../perf/r3-fixed.json`): winuse reads are 3-5x faster than agent-desktop's and its output is up to 7.5x smaller; after the perf commit its clicks are faster too (Calculator 81 vs 194 ms, Explorer 71 vs 517 ms, whole CLI min). The Agent Cursor's glide (about 300 ms when the target changes) stays: perf 02 is wontfix by the user.
- cua: `../perf/cua-research.md`. Keep ADR 0002; amend its outdated "background, which we dropped" reason.
- Not yet ticketed, found during the perf work (confirm or refine, don't just repeat):
  - auto-wait for actionability on desktop Actions (agent-desktop waits up to 5 s for visible, enabled, stable; winuse fails at once, which costs an agent turn);
  - diff reads (cua's `since: <snapshot_id>`);
  - smaller Electron trees without a Browser Connection: collapse anonymous wrappers (agent-desktop's Chromium wrapper skip, `CONCEPTS.md` "Web Wrapper") and allow `--interactive` on a whole-Window read (today only with `--root`);
  - read fallbacks: a retry on a transient E_FAIL, an MSAA walker (LibreOffice), a desktop-root walk for an empty CoreWindow wrapper (cua `PW/uia/mod.rs:337-478`);
  - agent-desktop's cache-skip for client-side providers (`crates/windows/src/tree/walk/cache.rs`, measured 1.7x on in-process providers) needs measuring with winuse's own walk;
  - this PC stalls process starts at random (up to 4 s even for a usage error), which an agent feels on every winuse call.
- Hints from the last chat, not a split (reviewers still read everything): delivery and retry semantics (agent-desktop's Delivery Semantics, Mutation Classifier); window and element identity (handle plus process creation time, re-checked before each write: "Window Identity", "Identity Sandwich"); read outcomes (Settled Absence vs Retryable vs Unavailable; a truncated tree must never report itself complete: "Enumeration Exhaustion"); test method (Probe Corpus, Findings Ledger, Mandatory Measurement Gate, Gate Self-Test).

## Open items from the perf work

- Not exercised live: a dialog that opens after the first look (perf 01's late note) and launch-app's passive arm (perf 04). Both need the user's OK to drive the desktop.
- `perf-catch-up` is committed, not merged.

## Output

1. The council's findings, verified, ranked by impact, each with citations. Score winuse out of 10 per reviewer if it helps the summary.
2. Tickets per `docs/agents/issue-tracker.md`: `.scratch/<feature>/spec.md` and `.scratch/<feature>/issues/NN-<slug>.md`, `Status: needs-triage`, for the user to triage. Note conflicts with ADRs, CONTEXT.md, SKILL.md or `docs/codex-parity.md` in the ticket, as the earlier specs do.
3. A short summary for the user in plain words: they asked for explanations without jargon.
