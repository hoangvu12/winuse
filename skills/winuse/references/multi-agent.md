# Several agents

Read when: more than one agent or sub-agent uses the desktop: `WINUSE_AGENT`, Agent Cursor colours, who waits for whom.

This is one topic of the winuse skill. `winuse skill multi-agent` prints it, and `winuse skill` prints the core, SKILL.md, which holds the safety rules, the Esc stop and recovery.

### Several agents

winuse names each caller after the program that ran it, and `winuse
status` lists the agents it served. Sub-agents of one session share that
program, so give each its own name in `WINUSE_AGENT`, set on every
command (a Harness often starts a fresh shell per command):
`$env:WINUSE_AGENT = "reviewer"; winuse click 101 3`. Add
`WINUSE_CURSOR_COLOR` (a hex colour such as `#FF8A3D`) to choose the
colour of that agent's Agent Cursor, whose tag shows its name, such as
`reviewer`; an agent with no name shows the arrow alone.

Each agent keeps its own Window State and screenshot of each Window, so
another agent's read never renumbers your indexes or moves your `@x,y`
points. Read a Window yourself before you act in it: until you do, winuse
refuses with nothing sent and says the Window has not been read by this
agent. If that comes on every command, your identity is changing; set one
`WINUSE_AGENT` for all of them.

Agents in Windows of different Apps run at the same time. Windows sharing
a UI thread (all of one Chrome's or Explorer's Windows) take turns, as do
two agents in one Window; Page Actions in two browser Windows run at once.
A Headed Action holds the whole desktop, and its banner names the agent. A
command that has waited about 12 seconds, or most of its budget, is
refused with nothing sent and names the holder, such as
`agent reviewer is using window 101`. Retry later or work in another
App's Window. Reads never wait.
