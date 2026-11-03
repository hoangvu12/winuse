# 22: ContextMenu on File Explorer list items

**What to build:** `perform-secondary-action <explorer> <item> ContextMenu` fails on Windows 11 File Explorer's list items with "ShowContextMenu was delivered, but no menu opened" (both Explorer tasks of the 2026-11-03 Task Suite run, `../../../tasksuite/results/2026-11-03-claude-opus-5-5.md`).

- Find out why: Explorer's items view (DirectUIHWND / the WinUI items view) may ignore IUIAutomationElement3::ShowContextMenu, show the menu late (see `../../multi-agent/issues/12-give-back-waits-for-a-late-menu.md`), or show it in another process the opened-Window diff does not watch.
- If a Headless route exists (a posted WM_CONTEXTMENU or Shift+F10 to the right window, the item's own ShowContextMenu after selecting it), add it with a read-back; otherwise refuse with nothing sent and the `--headed` retry, never "delivered, but no menu opened".

**Blocked by:** none

**Status:** ready-for-agent

- [ ] Measure ShowContextMenu, a posted WM_CONTEXTMENU and Shift+F10 on a Win11 Explorer item, covered
- [ ] A Headless route with read-back, or a clean refusal
- [ ] Offline test of the routing
- [ ] Live e2e on an Explorer window of the test's own (a temp folder), closed afterwards

## Comments

### 2026-11-03: from the Task Suite run (orchestrator 2)

Found by b6e5189e in the transcripts; approved by the user ("YES").
