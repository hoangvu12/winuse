# 22: ContextMenu on File Explorer list items

**What to build:** `perform-secondary-action <explorer> <item> ContextMenu` fails on Windows 11 File Explorer's list items with "ShowContextMenu was delivered, but no menu opened" (both Explorer tasks of the 2026-11-03 Task Suite run, `../../../tasksuite/results/2026-11-03-claude-opus-5-5.md`).

- Find out why: Explorer's items view (DirectUIHWND / the WinUI items view) may ignore IUIAutomationElement3::ShowContextMenu, show the menu late (see `../../multi-agent/issues/12-give-back-waits-for-a-late-menu.md`), or show it in another process the opened-Window diff does not watch.
- If a Headless route exists (a posted WM_CONTEXTMENU or Shift+F10 to the right window, the item's own ShowContextMenu after selecting it), add it with a read-back; otherwise refuse with nothing sent and the `--headed` retry, never "delivered, but no menu opened".

**Blocked by:** none

**Status:** done

- [x] Measure ShowContextMenu, a posted WM_CONTEXTMENU and Shift+F10 on a Win11 Explorer item, covered
- [x] A Headless route with read-back, or a clean refusal
- [x] Offline test of the routing
- [x] Live e2e on an Explorer window of the test's own (a temp folder), closed afterwards

## Comments

### 2026-11-03: from the Task Suite run (orchestrator 2)

Found by b6e5189e in the transcripts; approved by the user ("YES").

### 2026-11-03: done (worker, branch hm22-explorer-menu)

- Measured live (a probe, not committed): each route on a list item of a fresh, covered Win11 File Explorer Window of a temp folder, polling the top-level Windows every 10 ms for 6 s.
  - ShowContextMenu (today's route; the item's framework is DirectUI, no own window): the read-back gave up at 2 s ("delivered, but no menu opened"), and the `#32768` showed 5.8 s after the command started, untracked and topmost over the cover. On a warm Explorer (a menu shown a moment before) it showed within the 2 s.
  - WM_CONTEXTMENU posted to the items view (the DirectUIHWND under SHELLDLL_DefView), after a Headless Select of the item: a `#32768` at 1.9 s. It opens the menu of the selected items, so it needs the Select first.
  - Shift+F10 and the Menu key, posted (`press-key --post`) after the Select: nothing within 6 s.
  - The cover kept the foreground in every case.
- So ShowContextMenu works and was only read back too briefly. The route stays; the read-back on a Window of explorer.exe (`slowMenuApp`) waits up to `contextMenuSlowWait` (8 s), cut to the request's deadline less 1 s (`menuWait`), and returns as soon as the menu shows. Other Apps keep the 2 s. When even that passes, the refusal's hint says Explorer can still show it late and how to close it.
- Not done: the posted WM_CONTEXTMENU route (it needs a Select, which changes the user-visible selection, and ShowContextMenu already works).
- Tests: `TestContextMenuWaitsLongerOnFileExplorer` (Explorer finds a menu that shows 5.8 s later; another App gives up at 2 s; Explorer's no-menu refusal has the late-menu hint and waits the full 8 s), `TestMenuWaitKeepsAMarginBeforeTheDeadline`.
- Live e2e `TestContextMenuOnACoveredExplorerItem` (`test/e2e/explorer_menu_test.go`): an Explorer Window of its own on a temp folder, covered; ContextMenu on its file reads the menu back, the cover keeps the foreground; the menu and the Window are closed.
- SKILL.md: the ContextMenu bullet says the menu can come up to 8 s later on File Explorer.

