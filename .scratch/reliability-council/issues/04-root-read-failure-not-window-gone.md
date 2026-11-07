# 04: A failed root read is always "the Window may have closed", with no retry

Status: ready-for-agent (user, 2026-11-07: implement all 13; ask the user at the decision points in 08, 09, 12 and 13)

**What's wrong:** any failure of `ElementFromHandleBuildCache` becomes `ErrWindowGone` (`wm:internal/uia/capture.go:136-138`), whose hint is "the Window may have closed; run winuse list-windows" (`wm:internal/helper/state.go:1088-1089`). The first read is not retried (`wm:internal/uia/retry.go:141-144`; the retry there is for thin reads that succeeded). A chunk that fails with something other than a timeout is marked unreadable and not retried (`wm:internal/uia/walk.go:293-301`). A provider rebuilding its tree returns E_FAIL for a moment; the Window is still there, and the agent spends a turn relisting.

**cua:** retries the cache build up to 3 times, 40 ms apart (`cua-w:uia/mod.rs:381-409`; its comment names E_FAIL from a control rebuilding its subtree, issue #1881). It retries every HRESULT, which is broader than needed. **agent-desktop:** calls a window missing only on UIA_E_ELEMENTNOTAVAILABLE (`ad:crates/windows/src/tree/automation_classify.rs:213-232`: ElementNotAvailable or not-found is "Settled Absence", other failures keep their own disposition).

**What to build:**

- If `IsWindow(hwnd)` is still true after a failed root read, retry once or twice about 40 ms later, within the request's budget, for E_FAIL and the transport codes only. Not for access denied or a timeout.
- Say "the Window may have closed" only when it has. Otherwise "reading the Window's tree failed (HRESULT ...); retry" with its own code.
- The same single retry for a chunk that fails with E_FAIL.

**Blocked by:** 01 (a leftover tiny timeout is one cause of these failures)

- [ ] Root retry with a gone check; separate error for "still there but unreadable"
- [ ] Chunk retry once on E_FAIL
- [ ] Offline tests with a fake source: E_FAIL then success; E_FAIL three times on a live window; window gone
- [ ] ADR 0005 gets a line: this retry is for failed reads, its thin-read retry is for empty ones

**Verified by the orchestrator:** the winuse sites and cua's loop. Inferred: how often E_FAIL is transient on Windows apps winuse meets.

**Conflicts:** touches `internal/uia/retry.go`, which the pending perf change (skeleton skips the settle) also changes; keep that fast path.
