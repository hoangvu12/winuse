# 16: Spike: MXC session container

**What to build:** a written evaluation, no product code. Can winuse plus Apps it launches run inside a Microsoft Execution Containers (MXC) isolated session, so an agent works on its own invisible desktop while the user keeps theirs?

Known (`../headless-gaps/headless-100.md` section 3):

- MXC went GA on 2026-10-07 (https://blogs.windows.com/windowsdeveloper/2026/10/07/microsoft-execution-containers-policy-driven-containment-for-ai-agents/). It runs a separate account and session.
- SendInput returns ERROR_ACCESS_DENIED inside it (microsoft/mxc `docs/development/architecture/backends/isolation-session/state-aware-rust.md`), so hover and drag stay impossible there too.
- Sources conflict on whether the OS API is public or behind a flag.
- It cannot reach the user's own running Apps.
- Agent Workspace (Copilot Actions) is a private preview for Microsoft-signed agents only.

Questions:

1. Is the API callable by a third-party unsigned Go binary on Windows 11 Pro 26200? Which SDK (Rust crate, NuGet)? Does it break ADR 0002/0003 (own native layer, Go, no dependencies)?
2. Inside a session: do UIA reads, pattern calls, PostMessage, WGC capture and CDP work?
3. Can the user watch the session (a viewer window) and stop it?
4. Cost: start-up time, App install visibility, file sharing.

Answer: recommend adopt, defer or reject, with measurements.

**Blocked by:** none

**Status:** needs-triage (spike; may conflict with ADR 0002 and 0003)

- [ ] Availability and licensing checked against primary sources
- [ ] A throwaway probe if the API is reachable
- [ ] Findings appended under `## Answer`
