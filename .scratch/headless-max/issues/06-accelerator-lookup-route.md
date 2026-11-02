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

**Status:** ready-for-agent (low priority)

- [ ] AcceleratorKey search with normalization
- [ ] Routing: lookup before the existing press-key routes; Batch probe picks it up
- [ ] Offline tests: match, ambiguous, disabled, none
- [ ] Opt-in e2e: Ctrl+O on Notepad and WordPad behind a covering Window
- [ ] SKILL.md keys section

## Comments

### 2026-11-03: triage decision (user)

Do it like cua (`tools/hotkey.rs:262-300`): a matching AcceleratorKey is Invoked Headless, with no match or two matches the existing routing runs. Low priority: few apps fill AcceleratorKey. The ADR 0011 Decision 4 change goes into ADR 0013 (ticket 17).

### 2026-11-03: which Apps label shortcuts (`../headless-gaps/probes-round5.md` section 4)

- **AcceleratorKey is filled by:** WinForms MenuStrip, WinUI 3 MenuFlyoutItems (menu items only, not Buttons), and Paint's ribbon buttons.
- **Not filled by:** WPF (never), WinUI Buttons, or classic dialogs.
- Classic Win32 menus put the shortcut in the Name after a tab ("Save	Ctrl+S"). Chromium menus put it at the end of the Name ("Select all Ctrl+A") and use AcceleratorKey for the mnemonic letter.

Build it like cua, plus:

1. Match AcceleratorKey when it is a chord (has a "+"). A single letter is a mnemonic: skip it.
2. Also match the "	<chord>" suffix of a Name, and Chromium's trailing " <chord>" on MenuItems.
3. For classic menu bars, read the HMENU (GetMenu, GetSubMenu, GetMenuStringW) without opening it, and run the command through ticket 08's posted WM_COMMAND. Don't Expand (it fails on HMENU bars).
4. For WinUI and WinForms menus, the items exist only once the menu bar item is expanded. Expand (no foreground change, measured), search, Invoke, and close.

Value: useful for WinForms, WinUI menus, Paint-like ribbons and classic menus. Not for WPF. Priority stays low.
