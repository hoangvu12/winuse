# 23: A Headed double click on an Explorer folder does not open it

**What to build:** in the 2026-11-03 Task Suite run (explorer-organize, `../../../tasksuite/results/2026-11-03-claude-opus-5-5.md`), `--headed click <folder> --count 2`, by Element and at a point, printed ok twice and the "2024" folder did not open. The agent had to navigate by the address bar.

- Find out why: the two clicks may be too far apart in time or space for the system's double-click settings, the first click may start a rename (a slow double click on a selected item), the activation before the input may eat the first click, or polite input (multi-agent 08) may give the foreground back between the clicks.
- Fix the Headed double click so it opens the folder, and check it on the fixture's canvas (which counts double clicks) and on an Explorer folder.
- Also consider a Headless route: Invoke on an Explorer item opens it (the default action), so a plain `click` may already do it; if so, say so in SKILL.md.

**Blocked by:** none

**Status:** ready-for-agent

- [ ] Measure the Headed double click's timing and positions against GetDoubleClickTime and SM_CXDOUBLECLK
- [ ] Fix it, with an offline test of the timing
- [ ] Live: a `--headed` double click opens a folder in an Explorer window of the test's own
- [ ] Note the Headless way to open an item, if Invoke does it

## Comments

### 2026-11-03: from the Task Suite run (orchestrator 2)

Found by b6e5189e in the transcripts; approved by the user ("YES").
