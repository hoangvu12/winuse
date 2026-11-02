# 07: UIA worker pool with per-App affinity

**What to build:** several UIA MTA worker threads instead of one (ADR 0009), each App pinned to one worker, so a hung App stalls only the agents working in it.

Measured on the PC (`../headless-gaps/probes-round3.md`):
- with App A hung, ElementFromHandle on A returned UIA_E_TIMEOUT after about 1 s and the click was never delivered late;
- meanwhile other threads clicked a healthy App in 17 ms (sharing one IUIAutomation) and 210 ms (each with its own).

- Pool of N workers (proposal 4). Affinity by process id: every UIA call for an App's Windows goes to that App's worker.
- The existing abandon-and-replace of a wedged worker (`internal/uia/thread.go:186-290`) applies per worker.
- A hung pre-check before acting: `IsHungAppWindow`, plus SendMessageTimeout(WM_NULL, SMTO_ABORTIFHUNG) with a **short** timeout (it waits the full timeout even when Windows already marks the App hung). A hung App: "nothing was sent", the App is not responding.
- The first UIA call is not re-entrant across threads (research): initialize each worker before it takes work.
- Amend ADR 0009.

**Blocked by:** 01

**Status:** ready-for-agent

- [ ] Worker pool, affinity map, per-worker replace
- [ ] Hung pre-check
- [ ] Offline tests: affinity, one wedged worker while others serve, the hung refusal
- [ ] Opt-in e2e: a fixture mode that hangs its UI thread; another fixture's click still lands within the budget
- [ ] ADR 0009 amendment
