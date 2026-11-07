# 09: Window identity: re-check at the write; the shared-record gap

Status: ready-for-human (part 1 done 2026-11-07; part 2, per-agent records, waits on the user's decision: it reverses ADR 0014)

**What's wrong:**

1. The identity check runs once, when the command starts (`wm:internal/helper/identity.go:115-148`), before the lane wait and the read. A Window that closes and hands its handle to a new one while the command waits or reads is not caught (Reviewer 2).
2. The records are shared by all agents, and any agent's list or read replaces them (`identity.go:12-17,74-98`). An agent holding an old ID passes the check after another agent has listed the new Window. Keys, typing, point Actions and `close-window` can land on the wrong Window. The records are also lost when the helper exits after 5 idle minutes; the first Action after that records whatever is there and passes (`identity.go:142-143`).

**agent-desktop:** "Window Identity" is handle plus process creation time, checked against what that observation recorded, and re-checked around the write ("Identity Sandwich", `ad:CONCEPTS.md`). Its window inventory brackets the read with identity checks (`ad:crates/windows/src/system/window/window_ops.rs:130-176`, Reviewer 2). That every write is bracketed is documented, not checked across all its actions.

**What to build:**

- Carry the identity seen at the command's start through the lane, and check it again right before an HWND write (keys, typing, point, close). Cheap: one GetWindowThreadProcessId plus the process start time.
- Optional, needs the user: per-agent records next to each agent's Window State.

**Blocked by:** none

- [x] Re-check before HWND writes, with an injected handle reuse test
- [ ] Decision: per-agent records or not (ADR 0014)

**Verified by the orchestrator:** `identity.go` as cited; ADR 0014's note.

**Conflicts:** **ADR 0014** (lines 129-134) accepts the shared-record gap on purpose: "the shared design was chosen". The second part reverses it and needs an ADR amendment. The first part (re-check at the write) does not conflict. Late opened notes from the pending perf change should carry the original identity, not a later lookup.

## Comments

### 2026-11-07: part 1 landed

- Design (`internal/helper/identity.go`): `checkWindow` (inside `laned`, so it runs once the lanes are held, and again on every try ticket 06's brief wait makes) now keeps what the Window ID names at the start (`reusedID` returns it) and puts it on the context (`withStartIdentity`; a map, so a Batch's step Windows join it). `sameWindow(ctx, id)` reads the identity again (`Services.WindowIdentity`: GetWindowThreadProcessId, the process's start time and the class, the same read as the start check, injectable) and compares it with the start's, not with the shared record, which another agent's list may have replaced since. On a mismatch it drops the record if it still holds the old Window and refuses with `nothingSent`. A command that names no Window by its ID (an App name, a `<window>:<tab>` target, an `in new` step) or whose identity cannot be read at the start or at the write is not checked, as before (a gone Window fails its write anyway).
- Where it runs, right before each pipeline's first write to the handle: `runAction` (Headed; before the activation, after the polite quiet-input wait; the activation then verifies this handle holds the foreground, where SendInput lands: click, type-text, press-key, scroll, drag, hover, maximize-window, the Headed tab switch, Page Actions with `--headed`, the right click of a ContextMenu); `runHeadless` (before `run.act`: the pattern calls, and the acts that post to the Window's controls, select-text's EM_SETSEL and the ContextMenu Secondary Action's WM_CONTEXTMENU); `runPosted` (posted click and wheel); `runKeyHeadless` (before the prepare step, since invoke-menu's sends WM_INITMENUPOPUP, and again before the act: the dialog's WM_COMMAND, the edit's EM_REPLACESEL, Posted Keys and text, posted chords, the menu shortcut, the classic menu click, Escape to a menu or the tray overflow); `runChromium` (Chromium text, key, chord and click messages); `runWindowAction` (restore, minimize, move, resize, close-window).
- Headless pattern calls do not need it: they act on a UIA Element of the Window State, which a reused handle does not inherit (the call fails stale). The check sits in `runHeadless` anyway because some acts there post to the Window's controls, and the refusal names the reuse rather than a stale index.
- Not checked: close-app (by App name, enumerates afresh), open-shell and the notification actions (no Window ID), the menu close of `closeLeftMenu` (the menu another Action left open, not this command's Window), the CDP Page Actions (they go through the Browser Connection to a target, not to the handle), zoom's and get-window-state's `--headed` activation (a capture, no input).
- Refusal: `window 101 closed during this command; a new window reused its ID; nothing was sent`, hint as the start check's (`run winuse list-windows and use the Window ID it prints now; read the new Window's State before acting on it`), delivery not delivered. The start check's text is unchanged.
- Opened-Window notes (`opened.go`, the settle of ADR 0019): not affected. They diff the window list before and after and never read or record an identity, so there is no later lookup to replace.
- Tests (`internal/helper/identity_write_test.go`, `handleReuse`: the first identity read of a command, the start check's, sees the old Window, every later one the new): Headed element click, type-text, press-key and a coordinate drag (no activation, no input); Headless pattern click, posted click, posted press-key, the dialog's WM_COMMAND, the edit's EM_REPLACESEL, invoke-menu (no WM_INITMENUPOPUP, no WM_COMMAND), a Chromium click; close-window and move-window; a Batch step in its own Window (Action 1 acts, Action 2 refused at its write); the record dropped so the agent acts after listing again; the same Window acts on the Headless, Headed (same step order as before) and close-window routes. With `sameWindow` stubbed out, the 14 refusal assertions fail.
- Docs: `skills/winuse/SKILL.md` (the Recover row names both texts), `references/windows.md`; ADR 0014's note on the shared records gains a sentence: the re-check narrows the gap, it does not close it.
- Green: `go vet ./...`, `CGO_ENABLED=0 go build ./...`, `CGO_ENABLED=0 go test ./...`.
- Open: part 2 (per-agent records next to each Agent's Window State) waits on the user: it reverses ADR 0014. The records still vanish when the helper exits idle, and the first Action then records what is there. Between the re-check and the write there is still a window of microseconds (a Headed Action's input after the verified activation, a pattern call's own round trip). Live: not run (offline only).
