# 01: ADR 0014 and CONTEXT.md terms

**What to build:** the decision record and terms for several agents at once, before any code ticket lands.

- **ADR 0014 "Several agents at once"**, recording the spec's decisions 1-8:
  - per-Window lanes plus one desktop lane;
  - Element Indexes and screenshot mappings per agent;
  - automatic agent identity with the `WINUSE_AGENT` override;
  - a cursor per agent, placed like cua's;
  - Esc stops every agent;
  - the desktop lane waits in line with a limit;
  - no automatic fallback to real input.
- It reverses "not adopted: multi-agent cursors and agent ids" (`.scratch/desktop-parity/spec.md:88`), the headless spec's out of scope "Per-agent cursors and a machine-wide input lease" (`.scratch/headless/spec.md:56`), and ADR 0009's single UIA thread (ticket 07). Amend those lines with a pointer.
- Record the fallback: if per-agent stores cause problems, try agent-desktop's snapshot-qualified refs.
- **CONTEXT.md:** add **Agent** (one caller of winuse, with an identity), **Lane** (a Window lane, and the desktop lane for Headed Actions), and **Agent Cursor** per agent. Reword "Element Index" and "Window State" as per agent. docs/codex-parity.md "one request at a time": reword it, keeping its status.

**Blocked by:** none

**Status:** done

- [x] ADR 0014
- [x] Pointers in the reversed spec lines and ADR 0009
- [x] CONTEXT.md terms; codex-parity wording (deferred to 06); `go test ./internal/skill/...`

## Comments

### 2026-11-03: done

- `docs/adr/0014-several-agents-at-once.md` records decisions 1-8, with thread-keyed lanes (06's correction), the UIA worker pool and the Window identity check, and the fallback to agent-desktop's snapshot-qualified refs.
- Pointers: `../../desktop-parity/spec.md:88` (already struck) now names ADR 0014; `../../headless/spec.md:56` struck with a pointer; ADR 0009 has an amendment note.
- CONTEXT.md: Agent and Lane (new "Agents" section), Agent Cursor per agent, Element Index and Window State per agent, each marked planned, since no code exists yet.
- docs/codex-parity.md "one request at a time" is unchanged on purpose: it describes today's single gate, and its wording changes with ticket 06.
