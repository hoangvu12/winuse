# 23: A Headed double click on an Explorer folder does not open it

**What to build:** in the 2026-11-03 Task Suite run (explorer-organize, `../../../tasksuite/results/2026-11-03-claude-opus-5-5.md`), `--headed click <folder> --count 2`, by Element and at a point, printed ok twice and the "2024" folder did not open. The agent had to navigate by the address bar.

- Find out why: the two clicks may be too far apart in time or space for the system's double-click settings, the first click may start a rename (a slow double click on a selected item), the activation before the input may eat the first click, or polite input (multi-agent 08) may give the foreground back between the clicks.
- Fix the Headed double click so it opens the folder, and check it on the fixture's canvas (which counts double clicks) and on an Explorer folder.
- Also consider a Headless route: Invoke on an Explorer item opens it (the default action), so a plain `click` may already do it; if so, say so in SKILL.md.

**Blocked by:** none

**Status:** done (no winuse fix: the double click is sound; the run's failure came from a Notepad the Action before had started, see Comments)

- [x] Measure the Headed double click's timing and positions against GetDoubleClickTime and SM_CXDOUBLECLK
- [ ] Fix it, with an offline test of the timing (not built: the timing is not the cause, and the cause is outside winuse; see Comments)
- [x] Live: a `--headed` double click opens a folder in an Explorer window of the test's own
- [x] Note the Headless way to open an item, if Invoke does it

## Comments

### 2026-11-03: from the Task Suite run (orchestrator 2)

Found by b6e5189e in the transcripts; approved by the user ("YES").

### 2026-11-03: measured; the double click is sound (branch hm23-dblclick)

All live, in Explorer windows the tests opened on temp folders and closed afterwards (`test/e2e/explorer_open_test.go`), at d791b1b and at the run's build b8f0e58 alike:

- **Timing and position:** GetDoubleClickTime is 500 ms and SM_CXDOUBLECLK/SM_CYDOUBLECLK 4 px on this PC; winuse sends the two clicks in one SendInput batch at one point, so they are always within both. The activation does not eat the first click, and polite input (multi-agent 08) gives nothing back between the clicks.
- **A `--headed` double click opens the folder** by Element on a fresh window; with the folder selected first; after two files were selected and Cut through the command bar; and on a folder made the run's way (New > Folder, the rename box set, a `--headed` Return). Every replay opened it, at both builds.
- **The run's real cause:** the transcript shows the step before: a Headless `click` on invoice-2024-01.txt. On an Explorer file a click is Invoke, which opens the file in Notepad (the Notepad window that stayed open through the later tasks). Replaying that, a Headless click on a text file and at once the `--headed` double click on the folder, failed 5 times in 11 (three batches): Notepad, still starting, came up during or right after the clicks, took the foreground, and the folder did not open, while winuse printed ok. winuse cannot stop another App from activating itself. Two detections were tried and dropped: another App's Window holding the foreground right after the Action, and another App's Window that is new since the save; in 2 of 5 failing runs neither showed yet when winuse reported, so they would only sometimes say it.
- **The Headless way:** a plain `click` on an Explorer item is its default action (Invoke): it opens a folder in the same window with no foreground (`TestHeadlessClickOpensAnExplorerFolder`), so no double click is needed. SKILL.md, "Keep the user's foreground", now says a click opens the item (a folder in place, a file in its App) and to select files with `perform-secondary-action <ref> Select` (more with `click --modifiers ctrl`); with that, the run's stray Notepad never starts.

