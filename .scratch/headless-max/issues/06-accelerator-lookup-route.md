# 06: Accelerator lookup route for press-key

**What to build:** `press-key <window> <chord>` first looks in the Window's UIA tree for an Element whose AcceleratorKey matches the chord, and Invokes it. Headless, with no flag, for every UIA framework. Otherwise the existing routing runs (classic dialog Return/Escape, then Headed).

cua does this (`tools/hotkey.rs:262-300`). It is verifiable through the Element and needs no keyboard state.

- Match AcceleratorKey case-insensitively after normalizing ("Ctrl+S", "Ctrl+Shift+S"; key names through `internal/keys`).
- Search menus that are already open, the menu bar, ribbons and toolbars. Bounded walk under the request budget.
- Zero matches: fall through to today's routing. Two or more enabled matches: fall through as well, and never guess.
- Disabled Element: refuse with "nothing was sent", naming the Element. Do not fall through to real keys, which would also do nothing.
- Classic menu bars do not expose their items until opened. Ticket 08 resolves these by HMENU; this ticket can call it once 08 lands.
- Output: `ok (invoked "Save" through its shortcut)`.

**Blocked by:** none (uses 08 for classic menus once it exists)

**Status:** done (the live e2e run is still to do)

- [x] AcceleratorKey search with normalization
- [x] Routing: lookup before the existing press-key routes; Batch probe picks it up
- [x] Offline tests: match, ambiguous, disabled, none
- [ ] Opt-in e2e: Ctrl+O on Notepad and WordPad behind a covering Window: written and compiled against the fixture's Classic Dialog menu instead (Ctrl+A labels its Edit > Select All); not run live yet
- [x] SKILL.md keys section

## Comments

### 2026-11-03: triage decision (user)

Do it like cua (`tools/hotkey.rs:262-300`): a matching AcceleratorKey is Invoked Headless, with no match or two matches the existing routing runs. Low priority: few apps fill AcceleratorKey. The ADR 0011 Decision 4 change goes into ADR 0013 (ticket 17).

### 2026-11-03: which Apps label shortcuts (`../headless-gaps/probes-round5.md` section 4)

- **AcceleratorKey is filled by:** WinForms MenuStrip, WinUI 3 MenuFlyoutItems (menu items only, not Buttons), and Paint's ribbon buttons.
- **Not filled by:** WPF (never), WinUI Buttons, or classic dialogs.
- Classic Win32 menus put the shortcut in the Name after a tab ("Save\tCtrl+S"). Chromium menus put it at the end of the Name ("Select all Ctrl+A") and use AcceleratorKey for the mnemonic letter.

Build it like cua, plus:

1. Match AcceleratorKey when it is a chord (has a "+"). A single letter is a mnemonic: skip it.
2. Also match the "\t<chord>" suffix of a Name, and Chromium's trailing " <chord>" on MenuItems.
3. For classic menu bars, read the HMENU (GetMenu, GetSubMenu, GetMenuStringW) without opening it, and run the command through ticket 08's posted WM_COMMAND. Don't Expand (it fails on HMENU bars).
4. For WinUI and WinForms menus, the items exist only once the menu bar item is expanded. Expand (no foreground change, measured), search, Invoke, and close.

Value: useful for WinForms, WinUI menus, Paint-like ribbons and classic menus. Not for WPF. Priority stays low.

### 2026-11-03: built (branch hm06-accelerators)

- `internal/keys/shortcut.go` (pure):
  - `ShortcutOf(chord)` reduces a chord to its modifiers and its one key. It is false for a lone key, so only chords with Control, Shift or Alt are looked up.
  - `ParseShortcutLabel` reads "Ctrl+S" and "Ctrl+Shift+O" (with the aliases Ctrl, PgUp, PgDn, Ins and Bksp). A label without "+" is a mnemonic and is skipped.
  - `LabelShortcuts`/`ShownBy` read AcceleratorKey, the text after a name's last tab (classic menus), and, on menu items only, a name's last word (Chromium's "Select all Ctrl+A").
- `internal/uia/accelerators.go`: `InvokeAccelerator` does a bounded walk (2000 nodes, depth 25, never inside a Document) over menu items, buttons and split buttons, matches them with the caller's matcher, and picks through the pure `pickAccelerator`. One enabled match is invoked through the Headless click's route. Matches that are all disabled are reported. Several enabled matches, or none, are left to the caller.
  - With `expandMenus` (the helper asks only for WinForms `WindowsForms10.` and WinUI 3 `WinUIDesktopWin32WindowClass`), a search that finds nothing expands the menu bar items one at a time (at most 12, a 2 s budget, 150 ms settle), searches again and collapses.
- Helper (`shortcuts.go`, called from press-key's unheaded, unposted path before the dialog route):
  - First the classic HMENU, scanned like invoke-menu (WM_INITMENUPOPUP before each submenu, depth 4, 512 items). One enabled item showing "\t<chord>" has its WM_COMMAND posted inside runKeyHeadless, with the Agent Cursor on its menu bar item.
  - Then the UIA search, with runHeadless's refusals first, a passive Esc scope during the search and invoke, and the Agent Cursor's click effect afterwards.
  - Output: `ok (invoked "File > Save" through its shortcut)`. A disabled match is refused with nothing sent. No match, or two enabled ones, fall through to the dialog route and then hm-21's refusal.
  - The Batch scope needed no change: since hm-21, an unheaded step never makes a Batch Headed.
- Tests:
  - `internal/keys/shortcut_test.go`: label parsing, chord reduction, every label place.
  - `internal/uia/accelerators_test.go`: the picker.
  - `internal/helper/shortcuts_test.go`: classic menu hits (New, Save As, Select All), the disabled refusal, no match and two matches falling through, the UIA outcomes (invoked, disabled, none, two, a failed invoke, a failed search), lone keys never searching, and the expand classes.
- E2e: `TestShortcutRunsAClassicMenuCommand` (`test/e2e/shortcut_lookup_test.go`). With the fixture's Classic Dialog covered, Control_L+a runs its "Select &All\tCtrl+A" menu item, and a type-text then replaces the selected text; Control_L+q keeps the refusal. Notepad and WordPad were not used: Windows 11 Notepad has no HMENU and WordPad is gone. Written and compiled, not run.
- Unmeasured live: the UIA search's speed on large trees (bounded by node count), and the WinForms/WinUI expand fallback outside round 5's probe windows.

