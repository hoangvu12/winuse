# 02: Agent identity on every request

**What to build:** every CLI request carries an agent identity, which the helper uses for stores (03), cursors (04), lanes (06) and messages.

- `protocol.Request` gains `Agent` (`agent`, a short string) and `AgentLabel` (`agent_label`, for the cursor).
- **Automatic:** the CLI walks up its parent processes to the first one that is not a shell or a winuse binary (cmd, powershell, pwsh, bash, sh, conhost, node wrappers as measured), and uses that process's id plus its start time. Each Claude Code, Codex or other harness session is a different process, so it is a different agent.
- **Override:** `WINUSE_AGENT=<name>` replaces the automatic identity and becomes the label. The skill tells agents to set it when several run under one harness process, for example sub-agents of one session.
- No identity resolved: one shared "default" agent (today's behaviour).
- `status` lists the agents the helper has seen recently: identity, label, last command and when it ran.
- agent-desktop for comparison: `--session` and `--agent-id`, or `AGENT_DESKTOP_SESSION` and `AGENT_DESKTOP_AGENT_ID`, required in multi-agent mode. cua: the live connection is the identity.

**Blocked by:** 01

**Status:** done

- [x] Parent-process walk with start time; the skip list
- [x] `WINUSE_AGENT` override; protocol fields
- [x] `status` agent list
- [x] Offline tests: the walk over a faked process tree, the override, the default
- [x] SKILL.md: when to set `WINUSE_AGENT`

## Comments

### 2026-11-03: built (branch ma02-agent-identity)

- `internal/agent`:
  - `Resolve` reads `WINUSE_AGENT` first. The name is trimmed, cut to 64 runes, and dropped when it holds control characters. It becomes ID `name:<name>` and is also the label.
  - Otherwise it walks up from the CLI's own process (CreateToolhelp32Snapshot) to the first parent that is not skipped. The ID is `proc:<pid>:<start time>` (GetProcessTimes, 100 ns since 1601), and the label is `<exe> <pid>`, such as `claude 17048`.
  - The walk gives no identity at a missing parent, at a parent that started after its child (a reused pid), and after 32 steps.
- Skip list: cmd, powershell, pwsh, bash, sh, zsh, dash, conhost, openconsole, winuse. node is not skipped: under an npm-installed Harness it is the Harness itself.
  - **Measured on the PC (read-only process listing):** Claude Code's Bash tool runs commands under powershell, bash, bash, bash, then claude.exe, and its PowerShell tool under powershell, cmd, then claude.exe. Both resolve to claude.exe.
  - Codex and other Harnesses are not measured.
- The CLI resolves the identity once per process (`callingAgent`). `protocol.Request` gains `agent` and `agent_label` (omitted when there is none).
- The helper's server puts the Agent on the command's context. `helper.AgentOf(ctx)` is the read point for tickets 03, 04 and 06, and returns the default Agent (`default`) when the request named none.
- Every command except `status` records its Agent, keeping up to 16 and forgetting the oldest. `winuse status` gains an `agents:` line, most recent first, such as `claude 17048 (click, 12s ago); reviewer (get-window-state, 3m0s ago)`.
- Docs: SKILL.md ("Several agents" under "The loop", and the status line), CONTEXT.md's Agent term (the identity has landed; the per-Agent stores and cursor are still planned), and ADR 0014's Status (Decision 3 landed).
- Not done here (later tickets): per-Agent Window States (03), cursors (04), Esc (05) and lanes (06). SKILL.md says that the agents still share Element Indexes.
