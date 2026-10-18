# 01 — Background helper + list windows

**What to build:** The thinnest end-to-end path through winuse. Running `winuse list-windows` prints every visible Window with its Window ID, App and title. The first CLI call starts the background helper automatically; later calls reuse it over a named pipe that only the current user can open; the helper exits by itself after an idle period. The process is per-monitor DPI aware (v2) from the start so all later coordinates are physical pixels. Also establishes the Go module (no cgo), the CLI-to-helper request/response contract, and the default (offline) test command. See spec: Process model, Command surface, ADR 0001, ADR 0003.

**Blocked by:** None — can start immediately.

**Status:** done

- [x] `winuse list-windows` prints one line per visible Window: Window ID, App, title
- [x] Hidden, cloaked (DWM) and tool windows are excluded
- [x] First invocation starts a detached helper with no console window; subsequent invocations reuse it
- [x] The helper's pipe is restricted to the current user's SID
- [x] The helper exits after the idle period and is restarted transparently by the next command
- [x] Process is per-monitor DPI aware v2 before any window call
- [x] Builds as a single static Go binary without cgo
- [x] Default test command runs only offline tests and passes
