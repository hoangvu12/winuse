# 08: invoke-menu for classic menu bars

**What to build:** `invoke-menu <window> "File > Save As..."` runs a classic (HMENU) menu command without opening the menu and without the foreground.

Measured (`../headless-gaps/headless-100.md` G7): a posted WM_COMMAND(id) was handled by a background window with the foreground unchanged. UIA Expand on a classic menu-bar item of a background window failed with HRESULT 0x80131509 and opened nothing, so today's Headless click cannot reach classic menus.

Route:

- `GetMenu(window)`, then walk the items by `GetMenuString`, matching each path segment case-insensitively, ignoring `&` and the tab-separated shortcut text.
- Optionally send WM_INITMENUPOPUP for each submenu first, so Apps that update item states lazily are current (measure whether it is needed).
- `GetMenuState`: a grey or disabled item refuses with "nothing was sent".
- PostMessage(WM_COMMAND, MAKEWPARAM(id, 0), 0) to the Window.
- Output `ok`. A command that opens a dialog: the agent reads the new Window (desktop-parity ticket 02 would report it).
- No HMENU (WinUI, WPF, Office ribbon, Chromium): refuse with "this Window has no classic menu bar; click the menu Elements instead".
- Batch step kind: yes, Headless.
- Agent Cursor: at the menu-bar item's rectangle (`GetMenuItemRect`), label `menu "File > Save As..."`.

**Blocked by:** none

**Status:** ready-for-agent

- [ ] Win32 layer: menu walk, state, rectangle, posted WM_COMMAND
- [ ] Helper command and Batch step; administrator Window and locked desktop refusals
- [ ] CLI parsing of the path; usage text
- [ ] Offline tests: path matching, grey item, no menu bar, ambiguous names
- [ ] Opt-in e2e: Notepad (classic) or the fixture with a menu bar, Edit > Select All, covered
- [ ] CONTEXT.md Action list; SKILL.md

## Comments

### 2026-11-03: triage decision (user)

Kept as written.
