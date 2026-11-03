# 02: Report Windows that an Action opened

**What to build:** after `click`, `perform-secondary-action Expand|Select`, `press-key`, `set-value` and a Batch, compare the App's Windows before and after, and append a note:

- `Note: a new Window opened: <id> <App> "<title>"` (one line per Window: dialogs, menus `#32768`, popups);
- `Note: window <id> closed` when the target closed.

This is agent-desktop's `data.surfaces` (`ad:docs/surfaces.mdx:53-66`). Today a modal is found only when the next Action is refused (`internal/win/activate.go:163-171`); a Page file chooser is already reported (`internal/helper/pageelements.go:442`).

- Snapshot the App's top-level and owned Windows (same process, visible) before the Action, and again after a short settle (reuse the verify wait, up to about 300 ms; stop early when a new Window appears).
- Menus and popups that are not in `list-windows` today (`#32768`, tooltips) are reported only if they are menus; tooltips are skipped.
- For Page Actions, JS dialogs and new Tabs are reported the same way (the Page already knows).

**Blocked by:** none

**Status:** done (the live e2e run is still to do: paused while the user games)

- [x] Window snapshot diff behind a Services fake
- [x] Notes on the listed Actions and on Batch
- [x] Offline tests: new dialog, menu, closed target, nothing new
- [x] Opt-in e2e: fixture button that opens a message box, covered
- [x] SKILL.md: read the new Window next

## Comments

### 2026-11-03: triage decision (user)

Do it like agent-desktop's `data.surfaces`: after the listed Actions, report menus, dialogs and alerts that opened. The codex-parity rewording is accepted.

### 2026-11-03: done (worker, branch dp02-opened-windows)

- `internal/helper/opened.go`: `reportOpened` wraps click, perform-secondary-action Expand/Select, press-key, set-value and batch in the registry (the Batch keeps the raw steps, so it is diffed once). Before the Action it snapshots every visible top-level window (new seam `Services.Surfaces`, default `win.Collect` refused inside a test binary); after a successful Action it polls every 50 ms for up to 300 ms and stops at the first change. Notes: `Note: a new Window opened: <id> <App> "<title>"` per new Window of the target's pid, `Note: window <id> closed` when the target is gone. A failed Action keeps its error alone; no target in the snapshot skips the diff.
- `win.OpenedSurface` (pure, `internal/win/filter.go`): what list-windows keeps plus untitled or tool-window popups (`#32768`, PopupHost, a Chromium menu popup). Dropped: tooltips (`tooltips_class32`), `SysShadow`, click-through (WS_EX_TRANSPARENT), hidden, cloaked, minimized, zero-area, shell, winuse's own. Toasts are another process (ShellExperienceHost), so never reported.
- `resolveWindow` also resolves a numeric Window ID that list-windows drops when it is an open surface, so a reported menu can be read next (`get-window-state <id>`).
- Page Actions (`pageactions.go`): a JavaScript dialog opened by Page input is now `ok` plus `Note: the Page opened a confirm dialog: "..."; ... run winuse dialog <id> accept or ...` (it was an exit-1 failure). A new Tab is found by a probe (`watchNewTabs`, `page.ListTabs` = Target.getTargets before and at each poll of the same settle): `Note: a new Tab opened: <tab> "<title>" <url>`. A Batch keeps its steps' note lines.
- Docs: SKILL.md ("Read the result": read the new Window next; the dialog and Tab bullets; Batch output; screenshots of popups), `docs/codex-parity.md` process-model row reworded (status stays **intentionally different**) and the extensions row; `test/e2e/README.md`.
- Tests: `internal/helper/opened_test.go` (dialog, menu with a tooltip skipped, late Window, closed target, nothing new, only the listed Actions, failed Action, no target, Batch diffs once, menu resolves by ID, note format, Page new Tab), `TestOpenedSurface`, the Page dialog test updated. Opt-in e2e `TestActionReportsOpenedAndClosedWindows` (`test/e2e/opened_windows_test.go`): the covered fixture's "Classic Dialog..." click reports the dialog, `get-window` finds it, Escape reports it closed, foreground unchanged. `assertActionOK` and the Page `actOK` now accept and log note lines after `ok`; the Page dialogs subtest expects `ok` plus the note.
- Not measured live: whether Chrome's status bubble or other Chromium popups are click-through (they would show as extra notes after a Page click), and whether WPF tooltips appear within the settle. Live e2e not run (the user is gaming); run: `go test -tags e2e -v -run "OpenedAndClosed|HeadlessReturnAndEscapeOnClassicDialog|BrowserPages" ./test/e2e`.
