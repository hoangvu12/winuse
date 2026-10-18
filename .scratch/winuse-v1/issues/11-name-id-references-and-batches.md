# 11 — `name:` / `id:` references + Batches

**What to build:** Actions accept `name:<text>` and `id:<AutomationId>` Element References in addition to Element Index and `@x,y`; a name or ID matching more than one Element fails listing the candidates, never guessing. A Batch runs several Actions on one Window in a single call, in order, stopping at the first failure and reporting which step failed. Output stays Codex-style (`ok` or the failure). See spec: Command surface (Batch, Element Reference forms).

**Blocked by:** 06 — Type text and press keys; 07 — Set value and Secondary Actions

**Status:** done

- [x] `name:` and `id:` resolve against the latest Window State's Elements
- [x] Ambiguous matches fail with the candidate list; no match fails with a hint
- [x] A Batch runs Actions in order and stops at the first failure, naming the failed step
- [x] A successful Batch prints `ok`, Codex-style
- [x] Offline tests cover reference resolution (unique, ambiguous, missing) and Batch parsing
- [x] End-to-end: a Batch that types into the fixture field then clicks a button by `name:` succeeds; a Batch with a bad step stops there (verified by user run)

## Comments

- 2026-10-18 — live e2e run (Windows 11, 100% scaling, primary monitor, after the live-fixes merge into winuse-v1): TestBatchTypesThenClicksAndSucceeds and TestBatchStopsAtTheFirstFailure passed, with the rest of batch_test.go.
