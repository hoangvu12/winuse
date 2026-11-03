# 26: e2e Notepad tests leave tabs in Notepad's restored session

**What to build:** in the Task Suite run at b370064 (`../../../tasksuite/results/2026-11-03-b370064-claude-opus-5-5.md`, finding 2), `launch-app notepad` restored a Windows 11 Notepad session holding two tabs the e2e tests had left (a `winuse-e2e-select-*.txt` and a modified `winuse-e2e-notepad-*.txt`), plus an OK dialog about a missing file. Windows 11 Notepad keeps its tabs across runs (`%LOCALAPPDATA%\Packages\Microsoft.WindowsNotepad_8wekyb3d8bbwe\LocalState\TabState`, listed by `WindowState`), and the tests end their Notepad by killing its process, so every tab they opened comes back: in the Task Suite, and in the user's own Notepad.

- The e2e tests that open a file in Notepad (`TestHeadlessTypeTextIntoNotepad`, `TestHeadlessSelectTextInNotepad`, `TestMultiLineSetValueIntoNotepadReadsBack`, `TestLaunchAppNotepadReportsTheForeground`, and the Explorer open tests whose `alpha.txt` opens in Notepad) close their own tab cleanly, without saving (discard), on every exit path (`t.Cleanup`), so no session entry is left. Measure what works before choosing.
- The user's own Notepad is never touched: the tests already skip while a Notepad is running; the cleanup closes only the tab the test opened.
- Remove the `winuse-e2e-*` tabs already in the session, and only those, without touching the user's other tabs; if that cannot be done safely, say so instead.

**Blocked by:** none

**Status:** ready-for-agent

- [ ] Measure how a Notepad tab is closed without a save prompt or a session entry
- [ ] Every e2e test that opens a Notepad tab closes it on every exit path
- [ ] The `winuse-e2e-*` tabs already in the session removed, the user's tabs untouched
- [ ] Live: the Notepad e2e tests, then a fresh `launch-app notepad` brings back no `winuse-e2e` tab

## Comments

### 2026-11-03: from the Task Suite run (orchestrator 3)

Finding 2 of the b370064 Task Suite run; approved by the user ("ok do then").
