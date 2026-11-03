# 09: Shell surfaces

**What to build:** let the agent read and act on the taskbar, the notification area (tray and its overflow), Start and the Action Center. agent-desktop exposes all of them (`ad:docs/platforms/windows.mdx:53-55`). winuse excludes them on purpose (`internal/win/enum.go:64-71`).

- `list-windows --shell` lists `Shell_TrayWnd`, `Shell_SecondaryTrayWnd`, the overflow window (`TopLevelWindowForOverflowXamlIsland` on Windows 11), and Start / Action Center when open.
- Their Window States read through UIA like any Window; Headless Element Actions apply (taskbar buttons and tray icons expose Invoke).
- Opening Start or the Action Center needs keys: Start through `Control_L+Escape` (Headed). The Action Center is Win+A or Win+N, which winuse refuses; use the UIA Invoke on the taskbar's notification button instead (Headless if it works; probe).
- Clicking a taskbar button activates its App: that is the user-visible effect of the Action; say so in the skill.

**Blocked by:** none

**Status:** done (the Win+N route, `open-shell`, is built with ticket 18; the overflow e2e waits for the user's OK)

- [x] Shell Windows in the listing behind `--shell`
- [x] Probe: Invoke on taskbar, tray and notification buttons, foreground before and after (round 5: the chevron and the clock; no running-App button was touched)
- [x] Offline tests of the listing
- [ ] Opt-in e2e: read the taskbar (ran live, passes), open the tray overflow (written, gated by WINUSE_E2E_SHELL=1, not run yet)
- [x] ADR, SKILL.md

## Comments

### 2026-11-03: triage decision (user)

Do it like agent-desktop (Start, taskbar, tray, tray overflow, Action Center), Headless first: read and Invoke through UIA where the probe shows it works. agent-desktop's shell-accelerator route (Win+A and so on) is the fallback and runs only with `--headed`; the general Windows-key refusal stays for press-key.

### 2026-11-03: shell probe, partial (`../../headless-gaps/probes-round4.md` section 7)

Reading works: Shell_TrayWnd's UIA tree has 24 Elements, with Start, Search, the tray buttons ("Show Hidden Icons", Clock and so on) and the running-App buttons, read Headless with the foreground unchanged. The tray overflow window exists but has no Elements while it is hidden. **Not measured:** Invoke on the chevron and the Clock (it opens a flyout on the user's shell, and the user has not approved that yet). Build the listing and the reads now. The Invoke routes wait for that probe.

### 2026-11-03: Invoke routes measured (`../../headless-gaps/probes-round5.md` section 6)

- **Taskbar reads:** Headless (round 4).
- **Tray overflow:** Invoke on the chevron opens it and its 5 icons can be read, but **it takes the foreground** (the overflow window becomes the foreground, and Shell_TrayWnd keeps it after closing).
  - So opening the overflow is a Headed Action: it needs `--headed` (spec rule).
  - Close it with Escape posted to the island's InputSite child.
  - Match the chevron by AutomationId, not by Name: the Name changes to "Show Hidden Icons Hide" while open.
- **Clock and notification panel:** Invoke and DoDefaultAction return S_OK but open nothing. It cannot be opened through UIA. With `--headed`, use agent-desktop's shell accelerator route (Win+N) as the fallback.
- **Start:** unchanged (Headed through Control_L+Escape).

### 2026-11-03: built (laptop/dp-09-shell)

What landed:

- `list-windows --shell` adds the shell surfaces in z-order: the taskbars always (the untitled one is listed as `Taskbar`), the tray overflow, Start and the Action Center while open (visible, not cloaked). Recognised by window class, and the CoreWindow flyouts by host process (StartMenuExperienceHost, ShellExperienceHost, ShellHost), never by title. The plain listing and the desktop (`Progman`, `WorkerW`) are unchanged. `--app` and `--details` combine with it.
- A shell surface resolves by Window ID for every command (popupByID), never by App name. get-window-state reads it Headless.
- A Headless click on a running App's taskbar button is UIA Invoke; an Action on a shell surface adds `Note: the foreground moved to <id> <App> "<title>"` when the foreground moved.
- Headless click and Secondary Actions on the taskbar's flyout buttons (AutomationId `SystemTrayIcon`, `StartButton`, `SearchButton`, `TaskViewButton`, `WidgetsButton`) are refused with nothing sent and the `--headed` retry (Start also gets `--headed press-key <taskbar> Control_L+Escape`). Every other Headed fallback on the shell is refused by headless-max 21's general rule (merged mid-ticket; the shell-only guard I had written was cut back to the foreground note).
- `press-key <overflow> Escape` is Headless: Escape posted to the XAML island's bridge and InputSite windows, read back by the overflow hiding (2 s budget).
- An Action on a shell surface reports Start or the Action Center opening (`Note: a new Window opened`), though each has its own host process. The overflow is reported by the existing same-process rule.
- press-key still refuses the Windows key; nothing here sends it.
- ADR 0015 (`docs/adr/0015-shell-surfaces.md`) reverses the enum exclusion for `--shell`. CONTEXT.md gains **Shell surface** and the two route notes; SKILL.md gains "The taskbar and the shell" (clicking a taskbar button activates its App) and rewords the "cannot do headless" bullet; README and codex-parity have the new flag.

Files: `internal/win/shell.go` (new), `internal/win/enum.go`, `internal/win/format.go`, `internal/helper/shell.go` (new), `internal/helper/commands.go`, `internal/helper/opened.go`, `internal/helper/headless.go`, `internal/helper/keyboard.go`, `internal/helper/state.go` (Services `PostOverflowEscape` and `ShellKind`, `noteShell`), `internal/protocol/protocol.go`, `internal/cli/cli.go`.

Tests: `internal/win/shell_test.go` (kinds, the surface filter, z-order, the Taskbar title, the posted Escape, the test-binary guard), `internal/helper/shell_test.go` (the listing, resolution by ID only, keys need --headed, the flyout refusals, the App button's foreground note, the overflow Escape and its failures, Start reported opening, a Batch step refused), `internal/cli/listwindows_test.go` (`--shell` parsing), `test/e2e/shell_test.go`.

Live: `TestShellReadsTheTaskbar` (read-only: lists the taskbar, reads its tree, foreground unchanged) passed on this laptop. `TestShellRefusesWithoutHeaded` and `TestShellOpensAndClosesTheTrayOverflow` are gated by `WINUSE_E2E_SHELL=1` and were not run: the second opens the user's tray overflow, which takes the foreground; the user has not agreed yet.

Open:

- The Win+N fallback for the notification panel: which command sends it (a press-key exception for a shell target, or an `open-shell`-style command like agent-desktop's `open-system-surface`) is undecided. Until then the panel opens with a `--headed` click on the clock (a real click, not measured).
- Unmeasured: Invoke on a running App's taskbar button, and on the overflow's icons; the Start, Action Center and ShellHost CoreWindow names on this build.
- ShellExperienceHost's CoreWindow also hosts the toast banner ("New notification", probes round 5 section 7): a visible toast is listed as the Action Center, and an Action on a shell surface that coincides with one reports it as the Action Center opening. Ticket 18 must tell the toast banner apart from the panel.

### 2026-11-03: decision (PC orchestrator)

The Win+N fallback gets a dedicated command, `--headed open-shell start|notifications|quick-settings|overflow` (option b). press-key keeps refusing the Windows key, and CONTEXT.md is unchanged. It is built with ticket 18, not here: 09 ships as is, and ADR 0015 keeps the point open until 18 closes it.
