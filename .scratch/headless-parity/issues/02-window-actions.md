# 02: Window Actions

**What to build:** five new commands, none of which activates the Window:

- `restore-window <window>`
- `minimize-window <window>`
- `move-window <window> <x> <y>`
- `resize-window <window> <width> <height>`
- `close-window <window>`

Each one verifies its result:

- move and resize read the placement back, with an 8 px tolerance;
- restore and minimize check the iconic state;
- close waits up to 2 s for the Window to go away. A Window that is still open is `ok` with a note.

Refusals and limits:

- Each command refuses a locked desktop and an administrator Window with "nothing was sent".
- move and resize refuse a maximized or minimized Window.
- They are not Batch steps and show no Agent Cursor.

See spec: "Window Actions".

**Blocked by:** 01 (the minimized refusal hint names restore-window)

**Status:** done

- [x] Win32 layer: SW_SHOWNOACTIVATE, SW_SHOWMINNOACTIVE, SetWindowPos(SWP_NOACTIVATE|SWP_NOZORDER|SWP_ASYNCWINDOWPOS), PostMessage(WM_CLOSE)
- [x] Helper commands behind Services fakes; offline tests cover the verification and the refusals
- [x] restore-window notes "restored at normal size" for a Window that was maximized before it was minimized (WPF_RESTORETOMAXIMIZED)
- [x] The CLI parses the commands and validates the numbers; the usage text lists them
- [x] The minimized-Window hint in readyError points at restore-window as well

## Comments

### 2026-11-02: landed

- `internal/win/placement.go`: `RestoreNoActivate` and `MinimizeNoActivate` (ShowWindowAsync with SW_SHOWNOACTIVATE / SW_SHOWMINNOACTIVE, async so a hung app cannot block the helper), `MoveNoActivate` and `ResizeNoActivate` (SetWindowPos with SWP_NOACTIVATE|SWP_NOZORDER|SWP_ASYNCWINDOWPOS plus NOSIZE or NOMOVE), `PostClose` (PostMessage WM_CLOSE), and the read-backs `WindowRect` (GetWindowRect), `IsMaximized` (IsZoomed), `RestoresToMaximized` (WPF_RESTORETOMAXIMIZED) and `WindowOpen` (IsWindow and visible). The mutating calls refuse in a test binary (LiveDesktopGuard, pinned in `liveguard_test.go`). Move and resize use GetWindowRect so the numbers round-trip; the comment notes it includes the invisible resize borders (about 7 px) that the screenshot line's extended frame bounds leave out.
- `internal/helper/windowactions.go`: the five commands over new Services hooks (`RestoreWindow`, `MinimizeWindow`, `MoveWindow`, `ResizeWindow`, `CloseWindow`, `WindowRect`, `Maximized`, `RestoresMaximized`, `WindowOpen`, `Sleep`; `Minimized` is shared with ticket 01). `runWindowAction`: resolve, locked desktop and administrator Window refused with "nothing was sent", then the step in a passive Esc scope. No Activate, no banner, no Agent Cursor. Verification polls every 20 ms (counted in intervals, so the faked Sleep makes tests instant): 1 s for restore, minimize, move and resize (8 px tolerance), 2 s for close. A failed Win32 call says nothing was sent; a failed read-back says delivered. restore and minimize leave an already-normal or already-minimized Window alone; restore also un-maximizes, and appends "restored at normal size" only when WPF_RESTORETOMAXIMIZED was set and the Window reads back not maximized. close-window that stays open is `ok (the close was requested and window N is still open; it may be asking to save: ...)`.
- Registered in `CommandsWith` and serialized on the action gate (not observation-only). Not in `protocol.BatchStepKinds`, no `headed` flag. Protocol: `WindowActionArgs`, `MoveWindowArgs`, `ResizeWindowArgs`.
- `readyError` (headless.go): the minimized hint names `winuse restore-window <id>` (headless) first, then activate-window and --headed.
- CLI: `internal/cli/window.go` parses the five commands; move takes integer x, y within +/-1000000 (negative allowed), resize integer width and height in 1..1000000; usage errors exit 2. The helper repeats the range checks. Usage text lists the commands and says Window Actions are always headless.
- Tests: `internal/helper/windowactions_test.go` (a faked Window whose state settles after a few polls: outputs, the normal-size note, tolerance in and out, maximized/minimized refusals, locked and admin refusals for all five, close still open, call failure, never Activate or cursor, serialized and not Batch steps, the readyError hint), `internal/cli/window_test.go` (parsing, usage exits, usage text). SKILL.md and e2e are left to tickets 08 and 09.

- 2026-11-02 review fix: move-window and resize-window now take and verify the visible frame (DWM extended frame bounds), the screenshot line's `window at X,Y WxH`, instead of GetWindowRect, so its numbers can be passed to them directly. `win.MoveNoActivate` and `ResizeNoActivate` read GetWindowRect and the extended frame bounds, keep the invisible border insets, and pass SetWindowPos the outer rectangle that gives the requested frame (pure `outerForFrame`; `placementFrame` falls back to GetWindowRect for both when DWM does not answer or returns an empty rectangle; tested offline in `internal/win/placement_test.go`). `win.WindowRect` became `win.WindowFrame` (Services `WindowFrame`), so the read-back compares visible frames within the same 8 px. `placementHint`, the CLI usage text, SKILL.md's Manage Windows section and the e2e test follow.
