# 02: Agent identity on every request

**What to build:** every CLI request carries an agent identity, which the helper uses for stores (03), cursors (04), lanes (06) and messages.

- `protocol.Request` gains `Agent` (`agent`, a short string) and `AgentLabel` (`agent_label`, for the cursor).
- **Automatic:** the CLI walks up its parent processes to the first one that is not a shell or a winuse binary (cmd, powershell, pwsh, bash, sh, conhost, node wrappers as measured), and uses that process's id plus its start time. Each Claude Code, Codex or other harness session is a different process, so it is a different agent.
- **Override:** `WINUSE_AGENT=<name>` replaces the automatic identity and becomes the label. The skill tells agents to set it when several run under one harness process, for example sub-agents of one session.
- No identity resolved: one shared "default" agent (today's behaviour).
- `status` lists the agents the helper has seen recently: identity, label, last command and when it ran.
- agent-desktop for comparison: `--session` and `--agent-id`, or `AGENT_DESKTOP_SESSION` and `AGENT_DESKTOP_AGENT_ID`, required in multi-agent mode. cua: the live connection is the identity.

**Blocked by:** 01

**Status:** ready-for-agent

- [ ] Parent-process walk with start time; the skip list
- [ ] `WINUSE_AGENT` override; protocol fields
- [ ] `status` agent list
- [ ] Offline tests: the walk over a faked process tree, the override, the default
- [ ] SKILL.md: when to set `WINUSE_AGENT`
