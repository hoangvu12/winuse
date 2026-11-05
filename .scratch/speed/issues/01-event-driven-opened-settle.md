# 01: Watch the opened-Window settle instead of waiting 300 ms

Status: ready-for-human (done on branch `speed-waits`; the live check is to run)

**From:** research recommendation 2(a) (`.scratch/research-speed/report.md`,
`notes-local-perf.md` §2 and §9 #1).

**What to build:** after an Action that is reported (click, press-key,
set-value, invoke-menu, perform-secondary-action Expand/Select, batch),
`opened.go` took 6 snapshots over 300 ms and stopped early only when a
Window opened. That costs 300 ms on every Action that opens nothing.

- Look for the new Window as soon as it shows (WinEvent hook for the App's
  top-level windows).
- Stop early once the Action has settled.
- Report exactly the same notes.

**Out of scope:** the Agent Cursor's 600 ms floor (ADR 0018, the user's
call); act+observe; help, skill and output format.

- [x] `win.AppWatch` (`internal/win/appwatch.go`):
  - an out-of-context `SetWinEventHook(EVENT_OBJECT_CREATE..HIDE)` for the
    target's process, on its own pumping thread (not the Esc hook's UI
    thread);
  - a top-level show, or the target hiding or being destroyed, signals the
    settle;
  - a created top-level window that has not shown is pending;
  - `Sample()` reads the process's threads (state, wait reason, context
    switches) from one `NtQuerySystemInformation` snapshot, and
    `AppQuiet(a, b)` compares two.
- [x] `Services.WatchApp`, defaulting to `win.WatchApp`, which refuses in
  a test binary, so the offline tests keep the polled settle.
- [x] `settleWatched`:
  - looks at 50, 70, 100, 150, …, 300 ms, and at once on a signal;
  - stops after a look that finds nothing when the App was quiet since
    the previous sample and nothing is pending.
- [x] Kept polled:
  - Chromium/Electron (`Chrome_WidgetWin_*`) and shell surfaces;
  - Page Actions with a new-Tab probe;
  - an App whose watch fails.
- [x] ADR 0019.
- [x] Offline tests:
  - `internal/helper/opened_watch_test.go`: quiet stop, busy runs to
    300 ms, pending holds, a signal looks at once, the same notes as the
    polled settle in 7 cases, unwatched targets, closed after a failure;
  - `internal/win/appwatch_test.go`: event filter, pending and signals,
    quiet rules, snapshot parse, a real thread of the test process blocked
    in GetMessage reads as `WrUserRequest`.
- [ ] Live check (below).

## Measured (offline)

| What | Before | After |
|---|---|---|
| Settle after an Action that opens nothing, quiet App (`BenchmarkOpenedSettle`, wall clock, 3 ms sample cost included) | 302 ms | 74 ms |
| Same, App never quiet | 302 ms | 300 ms |
| Snapshots (`win.Collect`) in the settle, quiet App | 6 | 2 |
| One App sample (`BenchmarkSampleProcess`) | n/a | 3.0 ms |

So an Action on a quiet native App saves about 228 ms. A Window that
opens is reported when it shows instead of at the next 50 ms look, which
saves 0–50 ms.

## Live check (to run by a human)

`go test -tags e2e -v -run "OpenedAndClosed|HeadlessReturnAndEscapeOnClassicDialog" ./test/e2e`
covers the opened report on the fixture. Timings come from
`$env:WINUSE_TRACE` and the `duration_ms` of `click` on Notepad or
Calculator, before and after. See the report to the orchestrator for the
exact commands.

## Comments
