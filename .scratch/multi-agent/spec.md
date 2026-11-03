Status: ready-for-agent (decided by the user 2026-11-03)

# Multi-agent: several agents at once, each with its own cursor

## Problem statement

winuse serves one agent at a time:

- One `actionGate` serializes every Action, Headless and Page Actions included (`internal/helper/state.go:195-205`).
- One Window State per Window (`internal/uia/thread.go:487` `byHwnd`), and one screenshot mapping per Window (`internal/shot/shot.go:149`).
- One Agent Cursor, always topmost (`internal/win/cursor_view.go:250`).
- One UIA thread (ADR 0009). A hung App stalls every request until the thread is replaced.
- No agent identity on a request (`internal/protocol/protocol.go:164`).

**A silent wrong click today:** agent A reads Notepad (12 = Save), agent B reads it again (12 = Open), and A's `click 12` clicks Open. No check catches it.

agent-desktop and cua both serialize every input-producing action machine-wide. agent-desktop's PR #436 ("let actions on different apps run together") is open, not merged. Per-Window lanes would put winuse ahead of both.

## Decisions (user, 2026-11-03)

1. **Truly parallel.**
   - Background (Headless, Posted) Actions on Windows of different UI threads run at the same time. Windows on the same UI thread take turns, because focus, key state and fake activation are per thread. That includes two Windows of one Chrome or Explorer.
   - Page (CDP) Actions run in parallel per Tab.
   - Two agents in the same Window take turns.
   - Anything that uses the real mouse or keyboard (Headed) takes one desktop lane, so it always takes turns.
2. **Element Indexes per agent.** Each agent keeps its own Window State per Window, so another agent's read never changes its indexes. Desktop-parity 04's option (c) numbering applies inside each agent's store. **If this causes problems, try agent-desktop's approach** (snapshot-qualified refs such as `@s8f3k2p9:e12`).
3. **Agent identity: automatic, with an override.** winuse infers the agent from the calling harness process. `WINUSE_AGENT=<name>` overrides it and labels the cursor. agent-desktop needs `--session` and `--agent-id` on every call; cua gets identity from its live connection.
4. **A cursor per agent, like cua.** Each has its own colour and label. It sits just above the Window it acts on, not topmost, fades when idle and hides when the agent ends. Desktop-parity 21 (cursor polish) folds into ticket 04.
5. **Esc stops all agents.** A tray icon is out of scope for now. Neither agent-desktop nor cua has one.
6. **Build everything:** identity, stores, cursors and stop first; then lanes and the UIA worker pool; then polite real input.
7. **The desktop lane waits in line**, like cua and agent-desktop. A second Headed Action waits for the first, up to a limit (cua uses 12 s). Past the limit: "nothing was sent", naming the agent that holds the lane.
8. **No automatic fallback to real input** (ADR 0013): an agent that needs Headed passes `--headed`, and then waits for the desktop lane.

## Research

- The "winuse multi-agent research" chat (Zeron 690cfc8b): landscape, techniques and a gap list. Not in the repo; the essentials are in this spec and the tickets.
- `../headless-gaps/probes-round3.md`: the hung-App measurement behind ticket 07.

## Tickets

| # | Title | Effort | Status |
|---|---|---|---|
| 01 | ADR 0014 and CONTEXT.md terms | S | done |
| 02 | Agent identity on every request | S | done |
| 03 | Window States per agent | S-M | done |
| 04 | A cursor per agent, like cua | M | done (live e2e pending) |
| 05 | Esc stops every agent | S | done |
| 06 | Per-thread lanes and a desktop lane | M | done |
| 07 | UIA worker pool with per-App affinity | M-L | done (live e2e run still to do) |
| 08 | Polite real input | M | done (live e2e pending) |
| 09 | Window identity check before writes | S | done |
| 10 | Opt-in e2e for several agents | M | done (live e2e runs still to do) |
| 11 | Insertion caret after a Headless type-text or set-value (split from 04) | S-M | done (live e2e pending) |
| 12 | The give-back waits for a menu that opens late (from the 2026-11-03 Task Suite run) | S | ready-for-agent |

Order: 01, 02, then 03, 04, 05, 09 in any order, then 06, 07, 08, then 10.

## Out of scope

- A tray icon listing agents.
- Separate desktops or sessions (MXC, headless-max 16 wontfix).
- Agents on other machines.

## Testing decisions

- Offline (`go test ./...`): two fake agents against the Services fakes, covering store isolation, lane admission and ordering, the desktop-lane timeout and the Esc fan-out.
- Opt-in e2e (`WINUSE_E2E=1`): written and compiled; live runs follow the usual one-at-a-time rule.
