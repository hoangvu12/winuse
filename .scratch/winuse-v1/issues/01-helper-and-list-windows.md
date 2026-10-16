# 01 — Background helper + list windows

**What to build:** The thinnest end-to-end path through winuse. Running `winuse list-windows` prints every visible Window with its Window ID, App and title. The first CLI call starts the background helper automatically; later calls reuse it over a named pipe that only the current user can open; the helper exits by itself after an idle period. The process is per-monitor DPI aware (v2) from the start so all later coordinates are physical pixels. Also establishes the Go module (no cgo), the CLI-to-helper request/response contract, and the default (offline) test command. See spec: Process model, Command surface, ADR 0001, ADR 0003.

**Blocked by:** None — can start immediately.

**Status:** ready-for-agent

- [ ] `winuse list-windows` prints one line per visible Window: Window ID, App, title
- [ ] Hidden, cloaked (DWM) and tool windows are excluded
- [ ] First invocation starts a detached helper with no console window; subsequent invocations reuse it
- [ ] The helper's pipe is restricted to the current user's SID
- [ ] The helper exits after the idle period and is restarted transparently by the next command
- [ ] Process is per-monitor DPI aware v2 before any window call
- [ ] Builds as a single static Go binary without cgo
- [ ] Default test command runs only offline tests and passes
