# 04: A failed root read is always "the Window may have closed", with no retry

Status: done

**What's wrong:** any failure of `ElementFromHandleBuildCache` becomes `ErrWindowGone` (`wm:internal/uia/capture.go:136-138`), whose hint is "the Window may have closed; run winuse list-windows" (`wm:internal/helper/state.go:1088-1089`). The first read is not retried (`wm:internal/uia/retry.go:141-144`; the retry there is for thin reads that succeeded). A chunk that fails with something other than a timeout is marked unreadable and not retried (`wm:internal/uia/walk.go:293-301`). A provider rebuilding its tree returns E_FAIL for a moment; the Window is still there, and the agent spends a turn relisting.

**cua:** retries the cache build up to 3 times, 40 ms apart (`cua-w:uia/mod.rs:381-409`; its comment names E_FAIL from a control rebuilding its subtree, issue #1881). It retries every HRESULT, which is broader than needed. **agent-desktop:** calls a window missing only on UIA_E_ELEMENTNOTAVAILABLE (`ad:crates/windows/src/tree/automation_classify.rs:213-232`: ElementNotAvailable or not-found is "Settled Absence", other failures keep their own disposition).

**What to build:**

- If `IsWindow(hwnd)` is still true after a failed root read, retry once or twice about 40 ms later, within the request's budget, for E_FAIL and the transport codes only. Not for access denied or a timeout.
- Say "the Window may have closed" only when it has. Otherwise "reading the Window's tree failed (HRESULT ...); retry" with its own code.
- The same single retry for a chunk that fails with E_FAIL.

**Blocked by:** 01 (a leftover tiny timeout is one cause of these failures)

- [x] Root retry with a gone check; separate error for "still there but unreadable"
- [x] Chunk retry once on E_FAIL
- [x] Offline tests with a fake source: E_FAIL then success; E_FAIL three times on a live window; window gone
- [x] ADR 0005 gets a line: this retry is for failed reads, its thin-read retry is for empty ones

**Verified by the orchestrator:** the winuse sites and cua's loop. Inferred: how often E_FAIL is transient on Windows apps winuse meets.

**Conflicts:** touches `internal/uia/retry.go`, which the pending perf change (skeleton skips the settle) also changes; keep that fast path.

## Comments

### 2026-11-07: landed

- Root read (`internal/uia/capture.go`, `comSource.Root` → `rootReadErr`): a failed `ElementFromHandleBuildCache` asks `IsWindow` (the package var `isWindow`, faked in tests). Window gone: `ErrWindowGone` as before ("window is gone: reading the Window's tree failed (HRESULT …)"). Window still there: the new `*uia.TreeReadError{HR}` (`internal/uia/thread.go`), "reading the Window's tree failed (HRESULT 0x80004005); retry", or for E_ACCESSDENIED "reading the Window's tree was refused (HRESULT 0x80070005)" (`Refused()`), since no retry fixes that. Every Window-root walk gets this classification (Capture, PeekTree, Find, the live `name:`/`id:` resolve).
- Root retry (`internal/uia/retry.go`, `retryRoot`): wraps the read `CaptureFrame` hands `readTree` (`internal/uia/thread.go`). A `TreeReadError` whose HRESULT is transient (`hrTransient` in `com.go`: E_FAIL, RPC_E_DISCONNECTED, CO_E_OBJNOTCONNECTED, RPC_E_SERVERFAULT, RPC_S_SERVER_UNAVAILABLE, RPC_S_CALL_FAILED, RPC_S_CALL_FAILED_DNE) is read again after 40 ms (`rootRetryDelay`), up to 2 times (`rootRetries`). The sleep is `sleepCtx` on the caller's goroutine, so the request deadline bounds it, and a retry with no time left is not made. Each retry re-checks IsWindow through `Root`, so a Window that closes meanwhile ends as `ErrWindowGone`. Access denied, UIA_E_TIMEOUT and a gone Window fail at once. Because it wraps the read, the settle polls and the thin retry get it too. `readTree` is unchanged, skeleton fast path included.
- Chunk retry (`comSource.Children`): a `BuildUpdatedCache` that fails with E_FAIL is tried once more after `chunkRetryDelay` (40 ms, on the worker, under a transaction timeout recomputed for the shorter remaining time), only when more than 80 ms remain. A second failure takes 05's path: the walk marks `reasonUnreadable`. A timeout is still `ErrChunkTimedOut`, not retried. The tx computation moved to `chunkTimeout`; 01's restore is unchanged.
- Helper (`internal/helper/state.go`, `treeReadError`): `ErrWindowGone` keeps "the Window may have closed; run winuse list-windows". A `TreeReadError` gets "the Window is still open but its app did not answer the read; run the command again, or request a screenshot if it keeps failing" (code `INTERNAL`, from "failed"; no closer protocol code fits). A refused one gets "the Window is open but refuses UI Automation reads; start winuse from an elevated terminal to read administrator Windows, or request a screenshot" (code `PERM_DENIED` through the existing `elevated terminal` hint rule). No change to `internal/protocol/codes.go`.
- ADR 0005: one bullet under Consequences: its retry is for empty reads; failed reads have this one.
- Tests: `internal/uia/rootretry_test.go`: a fake source whose root fails with given HRESULTs, classified through `rootReadErr` with a faked IsWindow, read through `retryRoot`: E_FAIL once, twice and RPC_E_DISCONNECTED once, then success (40 ms delays); E_FAIL three times on a live Window through `readTree` (the `TreeReadError` and its text, 3 reads); a gone Window (`ErrWindowGone`, one read, no retry); access denied and UIA_E_TIMEOUT not retried; a retry the deadline has no time for is not made; `hrTransient` and the new HRESULT constants pinned; the real `Children` over a fake COM node: E_FAIL then success, E_FAIL twice (error, 2 builds), E_FAIL with no time for a retry (1 build). `internal/helper/treeread_test.go`: the message, hint and code of each of the three.
- Not done (optional): `treeIsComplete` still reads the notes. Moving it to 03's `Partial` would need `readResult` to carry the flag and the retry tests' hand-built reads to set it. That is not a one-liner, so it was left out.
- `go vet ./...`, `CGO_ENABLED=0 go test ./...` and `CGO_ENABLED=0 go build ./...` pass.
- Open: the other root reads that still call any failure `ErrWindowGone` (`accelerators.go`, `tabstrip.go`) and a region read's root are unchanged. PeekTree, Find and the live resolve classify but do not retry. How often E_FAIL is transient on the apps winuse meets is not measured.
- Live: not run (offline only).
