# 09: Window identity check before writes

**What to build:** a Window ID cannot silently point at a new Window that reused the old handle.

- A Window ID stays the HWND, as the agent sees it. The helper also records the owning process id and its start time when the Window is first listed or read.
- Before any Action, check the HWND's process id and start time against the record. A mismatch: "window N closed; a new window reused its ID", with nothing sent and `list-windows` advice.
- The records are shared by all agents and cleared when a Window closes.
- agent-desktop does the same before writes.

**Blocked by:** none

**Status:** ready-for-agent

- [ ] Identity record and check in the Action prelude
- [ ] Offline tests: same Window, a reused HWND, an unknown Window
