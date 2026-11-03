# 03: Window States per agent

**What to build:** each agent's Element Indexes and screenshot mappings are its own. Agent B reading a Window never changes what agent A's indexes mean.

Today: `internal/uia/thread.go:487` (`byHwnd`) and `internal/shot/shot.go:149` (`byHwnd`) hold one state per Window.

- Key both stores by (agent, Window). References (`name:`, `id:`, Element Index, `@x,y`) resolve against the asking agent's latest state of that Window.
- An agent with no read of the Window gets today's "read the Window first" message, even when another agent has read it.
- Desktop-parity 04's option (c) (find and region reads add indexes) applies inside each agent's store.
- Memory: drop an agent's states when it has been idle past the helper's idle period, and cap the total (as `thinWindows` does).
- Page Elements: the per-Window Page mapping stays shared (it is browser state, not numbering). Element numbering is per agent.
- Batch steps use the Batch's agent.
- **Fallback recorded:** if per-agent stores cause problems (memory, or agents confused by a second agent's changes), try agent-desktop's snapshot-qualified refs (spec decision 2).

**Blocked by:** 02

**Status:** done

- [x] Store keys in uia and shot; resolver takes the agent
- [x] Eviction and cap
- [x] Offline tests: two agents reading the same Window keep separate indexes; the A-reads, B-reads, A-clicks case acts on A's Element; an agent without a read is refused
- [x] SKILL.md references section

## Comments

### 2026-11-03: built (branch ma03-states-per-agent)

- The Agent reaches the stores through the request context. `agent.NewContext` and `agent.FromContext` are set by the server and read through `helper.AgentOf`. `agent.Key(ctx)` is the store key: the Agent's ID, or `default` for a request that names none.
- `uia`: the store (now `internal/uia/store.go`) keys each live Window State by (Agent, Window). `Capture`, `CaptureFrame`, `SplicePage` and every Element lookup use the request's Agent. `ResolveElementRef`, `StoredElements` and `ElementOrigin` now take a context. In the helper this threads `ctx` into `elementTarget.resolve`, `resolveElementRef`, `shotPoint`, `pageDoc`, `pageResolve`, `pageRoute`, `onPage`, `pageStep`, `stepHeadless`, `pageOrigin`, `refusePageElement`, `notPageElement`, `pageElementName` and `tabStrip`. These are signature changes only. The page probes in `pageStep`, which started from `context.Background()`, now carry the Agent.
- `shot.Store` keys the screenshot mappings by (Agent, Window) too, so `@x,y` points and zoom regions are per Agent.
- **The default Agent is the one store of before:** a single-Agent user sees no change. Its states never idle out.
- **Memory (ADR 0014, Consequences):** an Agent other than the default that has not used the store for the helper's idle period (`SetAgentIdle` / `Store.Idle`, set from `WINUSE_HELPER_IDLE`, default 5 minutes) loses its states. At most 256 Window States and 256 screenshot mappings are kept across Agents, the least recently used dropped first. Eviction runs on install (on the COM thread, which releases the dropped Elements) and on screenshot record.
- **The refusal when another Agent read the Window:** `uia.NotReadByAgentError` (it matches `ErrNoState`, so it stays a "nothing was sent" refusal) maps to "window N has not been read by this agent; another agent read it, and each agent keeps its own Window State". The hint says to run get-window-state first, and, if this happens on every command, to set WINUSE_AGENT (winuse status lists the agents). With no Agent's read at all, the message is today's "no cached Window State".
- **The risk from ticket 02 (an identity that changes per command):** if a launcher the identity walk does not skip sits between the Harness and winuse and exits after each command, every command is a new Agent. That surfaces as exactly this refusal on every Action right after a read, because the read was filed under another identity. **How to spot it:** `winuse status` lists many short-lived agents with the same exe name and different pids, each with one command. **The fix:** set WINUSE_AGENT to one name (SKILL.md says so), or add the launcher to `agent.skipped` once it is measured.
- Tests:
  - `internal/uia/store_test.go`: two Agents keep separate indexes; A reads, B reads, A clicks and gets A's Element; the default Agent is the one store; the not-read refusal through lookup, ref, origin and the default Agent; idle drop (never the default); the cap.
  - `internal/shot/shot_test.go`: per-Agent mappings and idle drop.
  - `internal/helper/agents_test.go`: the refusal wording, and per-Agent `@x,y` points.
  - The existing uia, shot and helper tests now pass a context or the default Agent.
- The opt-in e2e `TestWindowStatesArePerAgent` (`test/e2e/agents_test.go`) uses two WINUSE_AGENT names against the fixture. beta is refused before its read with nothing sent; alpha's state survives beta's read; both clicks land; status lists both. It is written and compiled, not run: live runs are paused.
- Desktop-parity 04's option (c) numbering is not built yet. When it lands, it applies inside each Agent's store.
