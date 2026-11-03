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

**Status:** done (the live e2e run is still to do)

- [x] Win32 layer: menu walk, state, rectangle, posted WM_COMMAND
- [x] Helper command and Batch step; administrator Window and locked desktop refusals
- [x] CLI parsing of the path; usage text
- [x] Offline tests: path matching, grey item, no menu bar, ambiguous names
- [ ] Opt-in e2e: Notepad (classic) or the fixture with a menu bar, Edit > Select All, covered (written and compiled against the fixture; not run live yet)
- [x] CONTEXT.md Action list; SKILL.md

## Comments

### 2026-11-03: triage decision (user)

Kept as written.

### 2026-11-03: built (branch hm08-invoke-menu)

- `invoke-menu <window> "File > Save As..."`: the CLI joins the arguments after the Window and splits them at each `>`. It is a Batch step and always Headless, with no headed form.
- Win32 layer (`internal/win/menu.go`): GetMenu, then each level read by position (GetMenuItemCount, GetMenuStringW, GetMenuState, GetMenuItemID, GetSubMenu). For a submenu, GetMenuState's high byte is its item count, not flags. GetMenuItemRect places the Agent Cursor on the bar item, and the command goes out as a posted WM_COMMAND(MAKEWPARAM(id, 0), 0).
- Matching: names match case-insensitively, without `&` and without the tab-separated shortcut. A trailing `...` is optional, but only when no exact name matches.
- Refusals, all with "nothing was sent": no classic menu bar (with the hint to click the menu Elements), no matching item (the refusal lists the items at that level, since a classic submenu never shows in the Window State), several matching items, a greyed or disabled item at any level, a command where the path goes on, a submenu where it ends, and an ID above 0xFFFF. The locked desktop, administrator Window, modal dialog and minimized refusals come first (runKeyHeadless, which gained a `prepare` step for the menu walk).
- **WM_INITMENUPOPUP (decided with the orchestrator):** sent to each submenu on the path before it is read, through SendMessageTimeout with SMTO_ABORTIFHUNG and 500 ms. A failed send is logged, and the submenu is then read as it is. Windows sends this message itself before it shows a menu, and Apps that grey or fill items lazily (MFC's UPDATE_COMMAND_UI, recent-file lists) update only there, so the grey check is honest only with it. The refusals still say "nothing was sent": the init message runs no command and is safe to repeat. **Measured on the fixture only:** no real classic App was probed under the rules (only what the tests launch). The Classic Dialog's Clear Name item is enabled only in WM_INITMENUPOPUP, so the e2e shows the mechanism, not how real Apps behave.
- Fixture: the Classic Dialog has a real HMENU (File > Fill Name; Edit > Select All, Clear Name (lazy), Locked (always greyed)), and the window grows by the menu bar's height. State gains `classicMenu` and `classicMenuInits`.
- Tests: offline tests through the Services fakes (`internal/helper/invokemenu_test.go`, plus the CLI, win, fixture and protocol tests). The e2e `TestHeadlessInvokeMenuOnClassicDialog` is written and passes `go vet -tags e2e`. **It has not run live:** the user was gaming, so the orchestrator paused live runs.
