# Ship as a skill + CLI, not an MCP server

winuse is delivered as an Agent Skill (SKILL.md) plus a command-line tool, with no MCP server. Computer use is needed occasionally, and a skill costs only its name and description in context until used, while an MCP server's tool definitions sit in every session; a CLI also works in any Harness that can run shell commands, which matches how our other agent tools (android-control, use-browser) are built.

## Considered Options

- **MCP server**: what nearly every other computer-use tool ships. Rejected because its tool schemas are always loaded, even in sessions that never touch the desktop.
- **MCP + CLI**: rejected as two front doors to maintain for no Harness we need; both target Harnesses (Claude Code, Pi) run shell commands and load skills.

## Consequences

- Screenshots are delivered as image files the agent opens with its Harness's own file-reading tool, not as inline tool results.
- Each CLI call is a separate process, so keeping Element Indexes valid between calls needs an explicit design (it cannot rely on a long-lived MCP connection).
