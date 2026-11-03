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

**Status:** done (the live e2e run is still to do: live runs are paused)

- [x] Worker pool, affinity map, per-worker replace
- [x] Hung pre-check
- [x] Offline tests: affinity, one wedged worker while others serve, the hung refusal
- [x] Opt-in e2e: a fixture mode that hangs its UI thread; another fixture's click still lands within the budget
- [x] ADR 0009 amendment

## Comments

### 2026-11-03: done (worker, branch ma07-uia-pool)

- `internal/uia/pool.go`: `Client` holds up to `PoolSize` (4) workers, started on first use, and an affinity map from App process id to slot. `RunFor(ctx, hwnd, job)` runs a job on the worker of hwnd's App (`win.AppPID`: the process, or a UWP App's content process); `Run` is RunFor with no Window. Every UIA entry point that names a Window now calls RunFor; the `@x,y` set-value and Secondary Action key by the top-level Window under the point (`win.RootWindowAt`). A held Element (waits) is read on its Window's App's worker and released on the no-Window worker.
  - Binding: a new App goes to the slot with the fewest Apps with a job in flight (a wedged job counts), then the fewest Apps, then the lowest index. Bindings idle 10 min with nothing in flight are dropped when a new App binds. A Client without `pidOf` (the offline tests that build one by hand) keeps one worker, as before.
  - Abandon and replace is per slot: `abandon(w)` clears only w's slot. `Close` releases the store on one worker, then winds every worker down.
  - `workerInit` serializes worker start-up (the first UIA call is not re-entrant across threads), so a worker takes work only after it initialised COM and created its IUIAutomation2. Each worker keeps its own IUIAutomation2 (abandoning one releases only its own).
- Hung pre-check: `win.WindowHung` (IsHungAppWindow, then WM_NULL with SMTO_ABORTIFHUNG and a 300 ms timeout; UIPI-refused WM_NULL falls back to IsHungAppWindow alone) behind `Services.WindowHung`. `internal/helper/hung.go` `refuseHung` runs inside the lanes after the identity check (`laned` hands the lane set on through the context). It checks commands whose lanes include the Window's UI-thread lane, so Page-only Actions, commands with no Window and reads are not checked. Refusal: `window <id>'s App is not responding; nothing was sent`, hint to wait and retry or work in another App's Window.
- Fixture: `-hang-on-click <ms>` (`fixture.Options.HangOnClick`): Click Me is recorded, then a posted message sleeps the UI thread, so the Invoke returns first. `startFixture` in the e2e package takes extra flags.
- Docs: ADR 0009 amendment section (pool, affinity, per-worker replace, serialized start-up, the pre-check, consequences) and its status note; ADR 0014 status; codex-parity "one request at a time"; SKILL.md "Refusals and recovery" (the not-responding refusal).
- Tests: `internal/uia/pool_test.go`: one worker per App, Windows of one App share it; more Apps than workers spread evenly; no `pidOf` keeps one worker; a wedged worker leaves the others serving (a healthy App's job runs at once, the hung App's second job waits behind its own, the wedged worker is replaced for its slot only); a new App avoids a busy worker; idle bindings are dropped; Close winds every worker down. `internal/helper/hung_test.go`: a hung App is refused for click, set-value, press-key, move-window and activate-window with nothing reaching the App, and acts once it responds; the identity refusal comes first; reads and Desktop commands are not checked; a Page click is not checked, a frame click in the same browser is.
- Opt-in e2e `TestHungAppStallsOnlyItsOwnWorker` (`test/e2e/hung_app_test.go`), compiled, NOT run (live runs paused). Fixture A runs with `-hang-on-click 15000`; after its Click Me, alpha reads the hung A in the background, beta's Headless click on fixture B must land within 2 s, gamma's click on A must be refused as not responding within 3 s, and A must not see the refused click after it wakes; the covers keep the foreground.

