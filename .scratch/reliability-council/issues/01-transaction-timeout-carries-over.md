# 01: The UIA transaction timeout carries over from the last tree chunk

Status: ready-for-agent (user, 2026-11-07: implement all 13; ask the user at the decision points in 08, 09, 12 and 13)

**What's wrong:** each UIA worker sets its TransactionTimeout in one place only: before every tree chunk, to `min(remaining, 2 s)`, never below 1 ms (`wm:internal/uia/capture.go:151-159`). Nothing resets it. At worker start only ConnectionTimeout is set (`wm:internal/uia/thread.go:80-82`). The timeout belongs to the worker's IUIAutomation2, so it bounds every later call on that worker:

- the first root read on a fresh worker (`capture.go:136`) runs under UIA's default 20 s, longer than the 10 s request;
- after a read that ran into its time limit, the worker keeps a timeout of a few milliseconds, so the next root read, Invoke or SetValue on it fails with UIA_E_TIMEOUT. A root timeout is reported as "the Window may have closed" (`capture.go:137`, ticket 04); a timed-out write as "may have taken effect" (`wm:internal/uia/value.go:883-890`).

An agent reads a big Window, the read hits its limit, and its next click fails for no reason, or tells it the click may have landed.

**agent-desktop:** sets one fixed transaction timeout when it creates each client (`ad:crates/windows/src/tree/automation.rs:190-203`); its probe measured that these bounds do apply, once the client is IUIAutomation2 (`ad:probes/windows/FINDINGS.md:178`, A14-12).

**What to build:**

- Put a fixed TransactionTimeout on the worker at start (next to ConnectionTimeout, `thread.go:82`).
- Restore it after each chunk, or set it before every call that is not a chunk. Restoring in `Children` is the smaller change.
- Also, from Reviewer 1, lower confidence: make one real call (GetRootElement) inside `workerInit`. agent-desktop measured that UIA's core initialises on the first real call and fails with E_FAIL ("Re-Entrant CheckInit") when threads race (`ad:probes/windows/FINDINGS.md:179`, A14-13; its fix `ad:crates/windows/src/tree/automation.rs:160-166`). winuse serialises CoCreate and the setters (`thread.go:49-91`, multi-agent 07); whether the setters already initialise the core is not measured.

**Blocked by:** none

- [ ] Fixed TransactionTimeout at worker start, restored after each chunk
- [ ] Offline test: a chunk with a tiny remaining budget, then an Action on the same worker gets the fixed timeout (fake call recorder)
- [ ] Optional: warm-up call in `workerInit`, after measuring whether the race reproduces

**Verified by the orchestrator:** the only `uia2PutTransactionTimeout` call in `wm:internal` is `capture.go:159`; `thread.go:80-88` sets only ConnectionTimeout and AutoSetFocus. Inferred: how often a real read leaves a tiny timeout behind; the warm-up race.

**Conflicts:** none. Touches nothing in the pending perf change.
