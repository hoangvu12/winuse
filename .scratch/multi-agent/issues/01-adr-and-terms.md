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

**Status:** ready-for-agent

- [ ] ADR 0014
- [ ] Pointers in the reversed spec lines and ADR 0009
- [ ] CONTEXT.md terms; codex-parity wording; `go test ./internal/skill/...`
