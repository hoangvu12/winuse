# 06: Per-Window lanes and a desktop lane

**What to build:** replace the single `actionGate` (`internal/helper/state.go:195-205`) with lanes.

- **Thread lane** (correction from the research chat, 2026-11-03): Headless and Posted Actions are keyed by the target Window's **UI thread** (GetWindowThreadProcessId), not by the HWND. Actions on Windows of different UI threads run at the same time; Windows on the same UI thread take turns. The reason: several routes change per-thread state.
  - focus (classic typing routes by the thread's focused control, GetGUIThreadInfo);
  - attached key state for chords (per input queue);
  - headless-max 20's fake activation (Chromium's focused widget for the browser UI thread).

  All Windows of one Chrome, Explorer or Win11 Notepad share a thread, so two agents in two of its Windows take turns.
- **Page lane:** CDP Page Actions touch no thread state, so they are keyed per Tab and run in parallel even inside one browser.
- **Desktop lane:** any Headed Action, and anything that activates a Window, uses the real cursor or SendInput, holds the desktop lane and its target Window's lane.
- **Waiting in line, like cua and agent-desktop:** a request waits for its lanes up to a limit (default 12 s like cua's input_busy, clamped by the request deadline). Past it: "nothing was sent; agent <label> is using <the desktop | window N>", with the retry advice.
- **A Batch** takes the lanes of every Window it touches (desktop-parity 24) and the desktop lane if any step is Headed. A Batch cannot switch from Headless to Headed halfway (Desktop-Computer-Use's rule).
- Lane order: desktop lane, then thread lanes by thread id, then Page lanes, so two Batches cannot deadlock.
- Reads (`get-window-state`, `list-windows`) take no lane, as today.
- The lane covers dispatch and verification, never the Ready wait (agent-desktop: re-resolve inside the lane).

**Blocked by:** 02; ticket 07 removes the UIA thread as the remaining serial point

**Status:** done (the live two-agent e2e run is still to do: paused while the user games)

- [x] Lane manager with ordered acquisition and a timeout
- [x] Every command's lane class (Window, desktop) from the existing routing predicates
- [x] Batch lanes
- [x] Offline tests: parallel Headless on Windows of two threads, serial on one Window and on two Windows of one thread, parallel Page Actions on two Tabs, Headed vs Headless, timeout wording, the deadlock order, Esc while waiting (05)
- [x] docs/codex-parity.md "one request at a time" wording; SKILL.md

### 2026-11-03: done (worker, branch ma06-lanes)

- `internal/helper/lanes.go` replaces `actionGate` with a `laneManager`: one capacity-1 channel per lane key, made on first use, with holder records (agent label, Window).
  - Keys: `desktop`, `thread <tid>` (win.WindowThreadID, behind `Services.WindowThread`; a Window whose thread cannot be read gets a lane of its own), and `page <hwnd>`. The Page lane is keyed by the browser Window, whose one active Tab its Page Actions act on, so two browser Windows run at once, even in one browser. Keys sort desktop, then threads, then Pages: the one acquisition order.
- Lane classes come from the commands' own routing (`stepHeadless`, `pageStep`, unchanged).
  - Window Actions: thread lane. activate-window: desktop and thread.
  - press-key and type-text: from the `--headed` flag (since headless-max 21 nothing else goes Headed), so the Window's focus is not probed twice.
  - A command with no Window (clipboard, display screenshot) or an unresolvable Window: desktop.
  - A Batch: desktop if `headed` or any step is Headed; the Page lane if any step goes to the Page; the thread lane if any step does not. A Batch never switches class halfway: it holds what it computed.
- `onLanes` takes the lanes in order inside one `gateWait`, so Esc stops a waiter (ticket 05). The wait limit is 12 s (`Services.LaneWait`), cut to 100 ms before the request's deadline. Past it the request is refused: `agent <label> is using <the desktop | window N>; nothing was sent`. A cancelled request keeps the old queued-out message. `serialized` is now "the desktop lane alone", used by the screenshot path, and a Page wait takes its Page lane (`waitGated`).
- The banner names the agent holding the desktop lane: `win.Overlay.SetAgent` and `OverlayBannerText` ("winuse is in control for <label> — press Esc to stop"). The default agent is unnamed (old text); the bracket overlay forwards.
- Concurrency fix: the self-activation note (headless-max 03) is skipped when the desktop lane was taken during the Action (`desktopUses`), so another agent's Headed activation is never blamed on this App.
- Docs: codex-parity "one request at a time" (reworded; matched for one agent, extension for several), SKILL.md "Several agents", ADR 0014 status and Consequences (the Page-lane keying, and the shared identity-records limit the orchestrator asked for).
- Tests: `internal/helper/lanes_test.go`: parallel on two threads; serial on one thread and on one Window; parallel Page Actions in two browser Windows; Page beside the browser's thread; Headed vs Headless; two Headed; refusal wording; budget clamp; sorted order and 40 crossing Batches with no deadlock; lane classes of real command arguments incl. the Page rig and mixed Batches; the banner's agent; the self-activation skip. Esc while waiting: `escall_test.go`. Updated: the browser/status/escall tests that held the old gate.
- Opt-in e2e `TestTwoAgentsRunInTheirOwnLanes` (`test/e2e/lanes_test.go`), compiled, NOT run. Two fixtures, three agents: alpha's Batch holds A's lane for 2 s; beta's click on B finishes within 1.5 s; gamma's click on A waits (>0.5 s) and then runs; covers keep the foreground.


### 2026-11-03: live run (branch e2e-live-fixes-3)

- `TestTwoAgentsRunInTheirOwnLanes` ran nothing: alpha's `batch <A> click <checkbox> then wait 2000` was refused by the Batch parser, since a `wait` step takes a state. Alpha now types twenty characters into fixture A as Posted Keys (`type-text <A> --post --interval-ms 200 ...`), which holds A's UI-thread lane for about 4 s. The test waits for the first character, then checks that beta's click on fixture B ends before alpha's typing and gamma's click on A after it, and that every character arrived. It compares end times: a click alone takes 0.8 to 1.9 s, so the old 1.5 s bound on beta failed in a full run. Passes live, three runs in a row.
- Found with the live runs: two requests in different lanes deadlocked the helper. `win.enumAll` held its lock only while it set and cleared the callback's target slice, so a concurrent call's callback appended through a target another call had just cleared, panicked while holding the lock, and every later `Collect` (every Window resolution) blocked: any two Actions sent at once by two agents, standalone or in Batches, timed out, and so did everything after them until the helper was killed. `enumAll` now holds the lock for the whole EnumWindows call, as `descendants` and `ListDisplays` already did. `TestEnumAllIsSafeConcurrently` (8 goroutines, 50 enumerations each, read-only) deadlocked before the fix and passes after it.
