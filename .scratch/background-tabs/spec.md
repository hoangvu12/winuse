# Background Tabs: agents work in their own Tabs while the user keeps theirs

Status: done offline (decided by the user; design details in ADR 0017);
live verification is issue 06, ready-for-human

## Problem

A Page is always the active Tab of a browser Window (CONTEXT.md "Page";
`page.MatchTab` skips inactive Tabs, `pageWindow` maps the Window to the
Tab it shows, `pageTabCheck` refuses anything else, `page.Merge` splices
the Page into the UIA document of the active Tab, and the Agent Cursor's
origin is that document). So an agent that wants to work in the user's own
browser must `tab switch` there, which changes what the user sees, and two
agents in one window take turns on its one Page lane.

The user wants to keep using the Tab they are looking at in their own Brave
or Chrome (through the winuse extension) while one or more agents work in
other Tabs of the same window, at the same time, never switching the user's
Tab and never taking the foreground.

## Prior art (research, verified where it touches our code)

Gemini in Chrome auto browse, chrome-devtools-mcp (`new_page
background:true`, pageId routing), Playwright MCP's extension mode,
Nanobrowser and Crawlio all use one recipe:

- `Emulation.setFocusEmulationEnabled(true)` on the Tabs an agent targets
  only (chrome-devtools-mcp PR #3230: on every Tab it made YouTube autoplay
  in untouched Tabs). winuse sends it in `AttachPage`, and attaches only to
  Tabs a command targets.
- `Page.setWebLifecycleState({state:"active"})` unfreezes a frozen page
  (Energy Saver freezes hidden silent Tabs after 5 minutes, Chrome 133+).
- `chrome.tabs.update(tabId, {autoDiscardable:false})` while an agent holds
  the Tab, restored on release (Nanobrowser).
- Crawlio 1.9.5: without the two CDP calls, input on a never-shown Tab did
  nothing; with them, input and screenshots worked and "the user's active
  tab never moves".
- `Page.captureScreenshot` on a hidden Tab has hung on Windows
  (anthropics/claude-code#78533): bound it and fall back to the text tree.

## Decisions (ADR 0017)

1. **Tab target.** Wherever a Page command takes `<window>`, it also takes
   `<window>:<tab>`: the Window (ID or App name) and a tab id from `tab
   list`/`tab new` (4 or more characters). The Tab may be in any window of
   that Window's browser (a popup a link opened, say). `tab new` prints the
   id as today. Commands without a Tab target behave exactly as before.
2. **Reading a Tab target**: `get-window-state <window>:<tab>` reads the
   Tab's Page through its Browser Connection alone (no UI Automation, which
   shows only the active Tab): a `document` Element 0 named after the Tab,
   the Page's Elements below it. Element Indexes are kept per Agent per
   Tab, apart from the Window's own Window State. `--screenshot` captures
   the Tab through CDP, bounded, and falls back to the tree with a note.
   `--root`, `--skeleton`, `find` and `zoom` are refused.
3. **Acting in a Tab target** is CDP only and always Headless: click,
   set-value, scroll (Element forms), hover, drag (Element to Element),
   select-text, type-text (with or without `--into`), press-key, upload,
   navigate, back, forward, reload, dialog, wait (the Page waits), eval,
   and a Batch of those. `--headed`, `--post`, `@x,y` points and every
   other command are refused with nothing sent. The Tab is never activated,
   nor its window.
4. **Keeping it alive.** Every command on a Tab target holds the Tab: focus
   emulation on, `setWebLifecycleState active`, and through the extension
   `autoDiscardable:false`. The hold is released 5 minutes after the last
   command on it (focus emulation off, `autoDiscardable` restored); the
   extension restores it itself when its debugger detaches. No other Tab is
   touched.
5. **Guarding the user's Tab** (extension). A Tab opened by a held Tab
   (`target=_blank`, `window.open`) never stays the active one: the
   extension selects the Tab the user had before, puts the new Tab in the
   opener's tab group, and minimizes a popup window it opened. The Action's
   `Note:` names the new Tab as today. JavaScript dialogs and file choosers
   already go through CDP; a click on a `<select>` or a date, time or colour
   input of a background Tab is refused (it would open a native popup on the
   user's screen) and set-value sets them through the DOM instead.
6. **Agent Cursor**: none for a background Tab (nothing on the screen shows
   it). A Tab target that is its window's active Tab gets the cursor as
   today's Page does. The Agent's tab group already names the Agent in its
   colour.
7. **Lanes**: an Action on a Tab target takes a lane of that Tab (ADR 0014
   Decision 1), so agents in different Tabs of one window run at once.
   Actions without a Tab target keep the Window's Page lane.
8. **Routes**: the extension first. The launched, cloned and `browser
   connect` routes read and act on Tab targets the same way, without
   `autoDiscardable` and the opener guard (CDP cannot select a Tab without
   `Target.activateTarget`); the Window State says so in a note.

## Out of scope

- `@x,y` points and `zoom` on a Tab target (no screen mapping).
- Drawing anything inside the Page or on the tab strip.
- Preventing a site from believing it is visible and focused (autoplay,
  read receipts): that is what focus emulation means; held Tabs only.

## Issues

`.scratch/background-tabs/issues/`, one per ticket.
