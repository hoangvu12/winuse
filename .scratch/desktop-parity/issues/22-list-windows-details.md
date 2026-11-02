# 22: list-windows details and filter

**What to build:** `list-windows` gains optional columns and a filter, matching agent-desktop's `list-windows` (bounds, `is_focused`, `minimized`, pid, `--app`; research `../../headless-gaps/agent-desktop-parity.md` row 4).

Today a line is `<Window ID>\t<App>\t<title>` (`internal/cli/cli.go:308-320`). Owned dialogs are already listed (winuse leads there).

- `list-windows --details`: adds `x,y WxH` (the visible frame, the same numbers `move-window` takes), `focused`, `minimized`, `maximized`, `admin` (higher integrity), and the pid.
- `list-windows --app <name>`: only that App's Windows (same App matching as `launch-app` and Window references).
- The default line stays the Codex format (codex-parity `list_windows` row).
- All Headless: EnumWindows, IsIconic, IsZoomed, DWM frame bounds, the integrity check from `internal/win/integrity.go`.

**Blocked by:** none

**Status:** done

- [x] Flags in CLI and protocol; helper fills the fields
- [x] Offline tests of the line format and the filter
- [x] SKILL.md list-windows section

## Comments

### 2026-11-02: landed

- Protocol: `ListWindowsArgs{Details, App}` (both omitempty). The CLI sends no arguments when neither flag is given, so the plain request is unchanged.
- `internal/win/format.go`: `FormatWindowsDetailed` and `WindowDetails{Focused, Maximized, Admin}`. Line: `<Window ID>\t<App>\t<x>,<y> <W>x<H> pid <pid>[ focused][ minimized][ maximized][ admin]\t<title>`. The details column goes before the title so the title stays last (split on tab with limit 4; titles may contain tabs, as the existing format test pins). The frame is `WindowInfo.Bounds`, the DWM extended frame bounds from the same enumeration (the visible frame move-window and resize-window take); minimized comes from `WindowInfo.Minimized` (IsIconic at enumeration). Flags print only when true. `FormatWindows` (Codex's line) is untouched.
- `internal/win/resolve.go`: `WindowsOfApp`, the App matching of a Window reference (`appMatches`: display name or exe base name with or without extension, case-insensitive, exact; never a title or a prefix).
- `internal/helper/commands.go`: `listWindows` now takes `Services` (s.Windows, so it is fakeable) and decodes its arguments strictly (unknown fields refused). `--app` with no matching Window fails with `app "X" has no open Windows` and a hint, rather than printing nothing (empty output already means "no visible Window"). `--details` reads `s.Foreground()` once, `s.Maximized` per non-minimized Window, and `s.Integrity`; `admin` is set for a `*win.AdminWindowError` (higher integrity, or an unreadable token, the same rule that refuses Actions). Other integrity failures (pid 0) are not reported as admin. Still observation-only (not on the action gate), no COM.
- CLI: `list-windows [--details] [--app <name>|--app=<name>]`; a stray argument, an unknown flag, a missing or empty name, or `--app` twice exit 2 with the usage line. Usage text describes the column.
- Docs: SKILL.md "Find the Window" (commands, the column, admin, the minimized frame, the --app error) and the command reference; README commands table; codex-parity.md `list_windows` row reworded (still matched: no flags is Codex's line) with a note, and the "extensions beyond Codex" section mentions both flags. CONTEXT.md is unchanged: the ticket introduces no new term and CONTEXT.md does not describe the list-windows line.
- Tests: `internal/helper/listwindows_test.go` (default line for no args/null/{}, with no integrity reads; the detailed lines over faked focus, maximize, minimize and admin; unreadable integrity is admin, other failures are not; --app by display name, exe name, case and spaces; no partial or title match; no-Window failure; unknown arguments refused), `internal/win` (`TestFormatWindowsDetailed`, `TestWindowsOfApp`), `internal/cli/listwindows_test.go` (flag parsing, usage exits, usage text). No e2e test written; nothing was run against the live desktop.
