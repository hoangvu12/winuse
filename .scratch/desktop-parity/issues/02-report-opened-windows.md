# 02: Report Windows that an Action opened

**What to build:** after `click`, `perform-secondary-action Expand|Select`, `press-key`, `set-value` and a Batch, compare the App's Windows before and after, and append a note:

- `Note: a new Window opened: <id> <App> "<title>"` (one line per Window: dialogs, menus `#32768`, popups);
- `Note: window <id> closed` when the target closed.

This is agent-desktop's `data.surfaces` (`ad:docs/surfaces.mdx:53-66`). Today a modal is found only when the next Action is refused (`internal/win/activate.go:163-171`); a Page file chooser is already reported (`internal/helper/pageelements.go:442`).

- Snapshot the App's top-level and owned Windows (same process, visible) before the Action, and again after a short settle (reuse the verify wait, up to about 300 ms; stop early when a new Window appears).
- Menus and popups that are not in `list-windows` today (`#32768`, tooltips) are reported only if they are menus; tooltips are skipped.
- For Page Actions, JS dialogs and new Tabs are reported the same way (the Page already knows).

**Blocked by:** none

**Status:** ready-for-agent (reword the codex-parity "process model" row, `docs/codex-parity.md:186-194`; its status stays intentionally different)

- [ ] Window snapshot diff behind a Services fake
- [ ] Notes on the listed Actions and on Batch
- [ ] Offline tests: new dialog, menu, closed target, nothing new
- [ ] Opt-in e2e: fixture button that opens a message box, covered
- [ ] SKILL.md: read the new Window next

## Comments

### 2026-11-03: triage decision (user)

Do it like agent-desktop's `data.surfaces`: after the listed Actions, report menus, dialogs and alerts that opened. The codex-parity rewording is accepted.
