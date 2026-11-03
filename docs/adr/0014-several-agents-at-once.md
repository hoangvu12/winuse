# ADR 0014: Several agents at once

## Status

Accepted (multi-agent spec, `.scratch/multi-agent/spec.md`, decided by
the user on 2026-11-03). Decision 3, the Agent identity on every
request, has landed (multi-agent ticket 02), and so have Decision 2, the
Window States and screenshot mappings per Agent (ticket 03), and Decision
4, a cursor per Agent (ticket 04). The rest is planned, and winuse still
serves one Action at a time. It reverses the desktop-parity
spec's "not adopted: multi-agent cursors and agent ids"
(`.scratch/desktop-parity/spec.md:88`), the headless spec's out-of-scope
"Per-agent cursors and a machine-wide input lease"
(`.scratch/headless/spec.md:56`), and ADR 0009's single UIA thread
(multi-agent ticket 07).

## Context

winuse serves one agent at a time:

- One `actionGate` serializes every Action, Headless and Page Actions
  included (`internal/helper/state.go`).
- One Window State per Window (`byHwnd` in `internal/uia/thread.go`) and
  one screenshot mapping per Window (`internal/shot/shot.go`).
- One Agent Cursor, always topmost.
- One UIA thread (ADR 0009). A hung App stalls every request until the
  thread is replaced.
- No agent identity on a request.

Two agents on one machine already collide silently. Agent A reads
Notepad (12 = Save), agent B reads it again (12 = Open), and A's
`click 12` clicks Open. No check catches it.

agent-desktop and cua both serialize every input-producing Action
machine-wide. agent-desktop's PR #436 (Actions on different Apps run
together) is open, not merged.

Since ADR 0010, 0011 and 0013, most Actions touch no shared input
state, so they need not wait for each other. A probe of a hung App
(`.scratch/headless-gaps/probes-round3.md`) showed that other UIA
threads keep working on healthy Apps while one App hangs.

## Decision

1. **Lanes keyed by UI thread, Tab and desktop.**
   - Headless and Posted Actions take the lane of their target Window's
     UI thread. Actions on Windows of different UI threads run at the
     same time. Windows on one UI thread take turns, because focus,
     attached key state and fake activation are per thread; all Windows
     of one Chrome, Explorer or Windows 11 Notepad share a thread.
   - Page Actions through CDP take a lane per Tab and run in parallel,
     even inside one browser.
   - Two agents in the same Window take turns.
   - Anything that uses the real mouse or keyboard, or activates a
     Window, takes the one desktop lane as well as its Window's lane.
   - Lanes are taken in a fixed order (desktop, then threads by id, then
     Tabs), so two Batches cannot deadlock. Reads take no lane.
2. **Element Indexes and screenshot mappings per agent.** Each agent
   keeps its own Window State per Window, so another agent's read never
   changes what its indexes or `@x,y` points mean. An agent that has not
   read a Window is told to read it, even when another agent has.
3. **Agent identity is automatic, with an override.** The CLI walks up
   its parent processes to the first one that is not a shell or winuse,
   and uses its process id and start time. `WINUSE_AGENT=<name>`
   overrides it and labels the cursor. With no identity, requests share
   one default agent, as today.
4. **A cursor per agent, placed like cua's.** Each agent has its own
   Agent Cursor, colour and label. It sits just above the Window it acts
   on rather than topmost, fades when idle and hides when the agent is
   gone.
5. **Esc stops every agent**, running and waiting alike. After the stop,
   new requests run again.
6. **The desktop lane waits in line, with a limit.** A second Headed
   Action waits for the first, up to a limit (cua uses 12 s), clamped by
   the request's budget. Past the limit it fails with "nothing was
   sent", naming the agent that holds the lane.
7. **No automatic fallback to real input** (ADR 0013 rule 3). An agent
   that needs Headed passes `--headed`, and then waits for the desktop
   lane.
8. **Build everything, in order:** identity, stores, cursors, Esc and the
   Window identity check first; then lanes and the UIA worker pool; then
   polite real input for Headed Actions.

With it come two safeguards from the tickets: a pool of UIA workers with
per-App affinity, so a hung App stalls only the agents working in it
(ticket 07, amending ADR 0009), and a check before every Action that a
Window ID still names the Window it named when it was listed or read
(ticket 09).

## Fallback

If per-agent stores cause problems, such as memory or agents confused
by another agent's changes to the same Window, try agent-desktop's
snapshot-qualified references (`@s8f3k2p9:e12`), which name the Window
State a reference came from.

## Consequences

- winuse can run several agents on Windows of different Apps without one
  waiting for the other. agent-desktop and cua cannot yet.
- An Element Index is valid only for the agent and the Window State that
  produced it. Another agent's Action can still change the Window, so the
  loop of observe, act, observe again stays.
- Two agents in Windows of one Chrome or Explorer take turns even though
  their Windows differ.
- The Headed banner stays one banner, naming the agent that holds the
  desktop lane.
- A request can now wait for a lane before it starts. Its budget covers
  the wait, and an Esc stops the wait.
- More memory per Window State: each agent's states are dropped when it
  is idle past the helper's idle period, and the total is capped.
- docs/codex-parity.md's "one request at a time" row changes when the
  lanes land (ticket 06), not before.
