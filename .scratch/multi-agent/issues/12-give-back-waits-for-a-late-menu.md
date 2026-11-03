# 12: The give-back waits for a menu that opens late

**What to build:** after a Headed Action, polite real input (ticket 08) keeps the foreground when the Action left a menu or a new Window of the target's App open. It checks right after the act. A Windows 11 File Explorer context menu appears a moment later, so the give-back runs first and the menu closes.

- Seen in the 2026-11-03 Task Suite run (`../../../tasksuite/results/2026-11-03-claude-opus-5-5.md`, explorer-zip): `--headed click <file> --button right` printed `ok` with no "a new Window opened" note, and no menu was open afterwards.
- Before the give-back, watch for a new Window of the target's App (a `#32768`, a PopupHost or another popup) for a short settle, as `opened.go` does (dp-02's 300 ms), and keep the foreground if one appears.
- Measure the Explorer menu's delay live first; the settle must not slow every Headed Action by much.

**Blocked by:** none

**Status:** done

- [x] Measure the delay of the Win11 Explorer context menu after a Headed right click
- [x] Settle before the give-back, keeping the foreground when a popup of the App appears
- [x] Offline test: a popup that appears after the act keeps the foreground
- [x] Live: a `--headed` right click on an Explorer item leaves its menu open, and the ok has the "new Window opened" note

## Comments

### 2026-11-03: from the Task Suite run (orchestrator 2)

Found by b6e5189e in the transcripts; approved by the user ("YES").

### 2026-11-03: done (worker, branch ma12-late-menu)

- Measured live (a probe, not committed): a `--headed` right click on a Win11 File Explorer list item, the Explorer Window of a temp folder, polling the top-level Windows every 10 ms.
  - The first context menu in a fresh Explorer Window (cold), with the Window already in front so nothing was given back: a `#32768` showed 2.21-2.42 s after the command started, 1.35-1.66 s after its `ok` (three Windows).
  - Later menus in the same Window: 364 and 566 ms after the start, inside the command; the old code kept those.
  - With the old give-back, the cold menu never showed (the probe's round 1: `ok`, no new Window within 1.5 s): the hand-back closed it as it opened.
- A 300 ms settle on every Headed Action would not catch it, and 2.5 s on every one would be too slow. So the wait is only for Headed Actions that ask for a context menu: `expectMenuHere` marks a right click (`click --button right`, by Element or at a point), the Headed ContextMenu (hm-09) and `press-key` Menu or Shift+F10 (`opensContextMenu`). For those, `giveBack` polls for a new Window of the App every 50 ms up to `politeMenuWait` (2.5 s), keeps the foreground when one shows (the usual note) and returns as soon as it does; when none shows it gives the foreground back, unless the user took it meanwhile. In a Batch only the last Headed step's ask counts. Cost: a right click whose App opens no menu waits 2.5 s.
- The `ok`'s "a new Window opened" note (dp-02) follows on its own: the menu is open by the time `reportOpened` looks.
- Tests: `TestPoliteWaitsForALateContextMenu` (a menu that shows 1.5 s after the click keeps the foreground), `TestPoliteMenuWaitOnlyForContextMenus` (a right click with no menu gives back after the wait, a left click does not wait), `TestOpensContextMenu`.
- Live e2e `TestHeadedRightClickKeepsALateExplorerMenu` (`test/e2e/explorer_menu_test.go`): an Explorer Window of its own on a temp folder, covered; the cold right click's `ok` notes the menu and that the foreground stays, and the menu is still open; it then closes the menu and the Window. Passed live twice.

