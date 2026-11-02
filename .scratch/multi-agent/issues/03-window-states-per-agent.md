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

**Status:** ready-for-agent

- [ ] Store keys in uia and shot; resolver takes the agent
- [ ] Eviction and cap
- [ ] Offline tests: two agents reading the same Window keep separate indexes; the A-reads, B-reads, A-clicks case acts on A's Element; an agent without a read is refused
- [ ] SKILL.md references section
