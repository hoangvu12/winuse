# 12: The give-back waits for a menu that opens late

**What to build:** after a Headed Action, polite real input (ticket 08) keeps the foreground when the Action left a menu or a new Window of the target's App open. It checks right after the act. A Windows 11 File Explorer context menu appears a moment later, so the give-back runs first and the menu closes.

- Seen in the 2026-11-03 Task Suite run (`../../../tasksuite/results/2026-11-03-claude-opus-5-5.md`, explorer-zip): `--headed click <file> --button right` printed `ok` with no "a new Window opened" note, and no menu was open afterwards.
- Before the give-back, watch for a new Window of the target's App (a `#32768`, a PopupHost or another popup) for a short settle, as `opened.go` does (dp-02's 300 ms), and keep the foreground if one appears.
- Measure the Explorer menu's delay live first; the settle must not slow every Headed Action by much.

**Blocked by:** none

**Status:** ready-for-agent

- [ ] Measure the delay of the Win11 Explorer context menu after a Headed right click
- [ ] Settle before the give-back, keeping the foreground when a popup of the App appears
- [ ] Offline test: a popup that appears after the act keeps the foreground
- [ ] Live: a `--headed` right click on an Explorer item leaves its menu open, and the ok has the "new Window opened" note

## Comments

### 2026-11-03: from the Task Suite run (orchestrator 2)

Found by b6e5189e in the transcripts; approved by the user ("YES").
