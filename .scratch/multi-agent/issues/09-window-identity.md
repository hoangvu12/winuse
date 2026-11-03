# 09: Window identity check before writes

**What to build:** a Window ID cannot silently point at a new Window that reused the old handle.

- A Window ID stays the HWND, as the agent sees it. The helper also records the owning process id and its start time when the Window is first listed or read.
- Before any Action, check the HWND's process id and start time against the record. A mismatch: "window N closed; a new window reused its ID", with nothing sent and `list-windows` advice.
- The records are shared by all agents and cleared when a Window closes.
- agent-desktop does the same before writes.

**Blocked by:** none

**Status:** done

- [x] Identity record and check in the Action prelude
- [x] Offline tests: same Window, a reused HWND, an unknown Window

### 2026-11-03: done (worker, branch ma09-window-identity)

- `win.WindowIdentity(hwnd)` returns a `WindowOwner`: the owning pid (GetWindowThreadProcessId), that process's creation time (GetProcessTimes) and the window class. The class also tells two Windows of one long-lived host process apart (ApplicationFrameHost, Explorer). Read-only; it answers false inside an offline test binary, so nothing is checked there unless a test fakes it.
- `internal/helper/identity.go`: one shared `windowRecords` per helper (all agents), built in `defaultServices` with the `Services.WindowIdentity` seam.
  - Recording: `list-windows` records every listed Window and drops the records of Windows that no longer exist. After a successful run, the reads that name a Window by ID (get-window-state, find, record-tree, tab-list), and the Window line get-window and launch-app print, record theirs without enumerating again (`recordReads`), replacing a record whose handle now names another Window.
  - Check: `checkWindow` runs inside the action gate before every serialized command that names a Window by its numeric ID; App names resolve afresh and are not checked.
    - No record (a helper restart): it is recorded and passes. A match passes.
    - A mismatch drops the record and refuses: `window N closed; a new window reused its ID; nothing was sent`, hint `run winuse list-windows ...`.
    - A Window whose identity cannot be read is not refused here (it fails on its own), and its record is dropped.
- Limit of the shared design the ticket chose: if another agent lists the new Window first, this agent's stale ID passes the check.
- Docs: SKILL.md (Window naming), codex-parity stale-elements section (extension), ADR 0014 status.
- Tests (`internal/helper/identity_test.go`): same Window acts; a reused ID is refused with nothing clicked, then acts after a new list; an unknown Window is recorded at its first Action; an App name is not checked; a read records; a list replaces a reused record and prunes a gone one; a gone Window is not reported as reused. No e2e in the ticket: reusing an HWND cannot be forced live.

