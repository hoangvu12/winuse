# 22: list-windows details and filter

**What to build:** `list-windows` gains optional columns and a filter, matching agent-desktop's `list-windows` (bounds, `is_focused`, `minimized`, pid, `--app`; research `../../headless-gaps/agent-desktop-parity.md` row 4).

Today a line is `<Window ID>\t<App>\t<title>` (`internal/cli/cli.go:308-320`). Owned dialogs are already listed (winuse leads there).

- `list-windows --details`: adds `x,y WxH` (the visible frame, the same numbers `move-window` takes), `focused`, `minimized`, `maximized`, `admin` (higher integrity), and the pid.
- `list-windows --app <name>`: only that App's Windows (same App matching as `launch-app` and Window references).
- The default line stays the Codex format (codex-parity `list_windows` row).
- All Headless: EnumWindows, IsIconic, IsZoomed, DWM frame bounds, the integrity check from `internal/win/integrity.go`.

**Blocked by:** none

**Status:** ready-for-agent

- [ ] Flags in CLI and protocol; helper fills the fields
- [ ] Offline tests of the line format and the filter
- [ ] SKILL.md list-windows section
