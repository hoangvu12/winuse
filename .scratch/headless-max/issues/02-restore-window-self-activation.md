# 02: restore-window reports self-activation

**What to build:** `restore-window` compares the foreground before and after the restore, and says so when the App brought itself to the front.

Measured (`../headless-gaps/headless-100.md` section 0): charmap.exe, minimized, then restored with ShowWindow(SW_SHOWNOACTIVATE) from a background process, became the foreground in 6 of 6 runs. SetWindowPlacement, a temporary WS_EX_NOACTIVATE, EnableWindow(FALSE) around the call and a 10 s wait did not prevent it. The probe's own plain window stayed in the background. So the call is non-activating, but some Apps activate themselves on restore. `restoreWindow` (`internal/helper/windowactions.go:57`) only checks the iconic state.

- After the read-back, compare the foreground. If the restored Window (or another Window of its App) is now the foreground and was not before: `ok (the App brought itself to the foreground)`.
- Do not try to give the foreground back: that needs SetForegroundWindow, which winuse cannot do without the foreground right, and it would fight the user.
- Reword ADR 0011 Decision 2 and its consequences: the calls are non-activating; the outcome is not guaranteed.

**Blocked by:** none

**Status:** ready-for-agent

- [ ] Foreground before and after in `restoreWindow`, behind a Services fake
- [ ] Output note; offline test for both outcomes
- [ ] Opt-in e2e: charmap (a `#32770` dialog App) minimized, restored, the note appears; the fixture restored, no note
- [ ] ADR 0011 wording and SKILL.md "Manage Windows"
