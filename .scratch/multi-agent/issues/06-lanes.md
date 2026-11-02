# 06: Per-Window lanes and a desktop lane

**What to build:** replace the single `actionGate` (`internal/helper/state.go:195-205`) with lanes.

- **Window lane:** Headless, Posted and Page Actions on one Window take turns. Actions on different Windows run at the same time.
- **Desktop lane:** any Headed Action, and anything that activates a Window, uses the real cursor or SendInput, holds the desktop lane and its target Window's lane.
- **Waiting in line, like cua and agent-desktop:** a request waits for its lanes up to a limit (default 12 s like cua's input_busy, clamped by the request deadline). Past it: "nothing was sent; agent <label> is using <the desktop | window N>", with the retry advice.
- **A Batch** takes the lanes of every Window it touches (desktop-parity 24) and the desktop lane if any step is Headed. A Batch cannot switch from Headless to Headed halfway (Desktop-Computer-Use's rule).
- Lane order: desktop lane, then Window lanes in HWND order, so two Batches cannot deadlock.
- Reads (`get-window-state`, `list-windows`) take no lane, as today.
- The lane covers dispatch and verification, never the Ready wait (agent-desktop: re-resolve inside the lane).

**Blocked by:** 02; ticket 07 removes the UIA thread as the remaining serial point

**Status:** ready-for-agent

- [ ] Lane manager with ordered acquisition and a timeout
- [ ] Every command's lane class (Window, desktop) from the existing routing predicates
- [ ] Batch lanes
- [ ] Offline tests: parallel Headless on two Windows, serial on one, Headed vs Headless, timeout wording, the deadlock order, Esc while waiting (05)
- [ ] docs/codex-parity.md "one request at a time" wording; SKILL.md
