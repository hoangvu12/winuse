# ADR 0015: Shell surfaces behind `list-windows --shell`

## Status

Accepted (desktop-parity ticket 09, `.scratch/desktop-parity/issues/09-shell-surfaces.md`;
triaged by the user on 2026-11-03: "do it like agent-desktop, Headless
first"). It reverses the shell exclusion of `internal/win/enum.go`
(`shellClasses`: the taskbars are no Windows an agent sees) for
`list-windows --shell`.

## Context

winuse dropped the desktop and the taskbars from every listing on
purpose: they are not Windows a person works in, and a Window ID nobody
listed could not be read or acted on. agent-desktop exposes the taskbar,
the tray and its overflow, Start and the Action Center
(`ad:docs/platforms/windows.mdx:53-55`), and opens them with shell
accelerators such as Win+A, which winuse refuses.

Probe rounds 4 and 5 (`.scratch/headless-gaps/probes-round4.md` section 7,
`probes-round5.md` section 6) measured the routes on Windows 11:

- The taskbar's UI Automation tree reads with the foreground unchanged:
  Start, Search, the notification area (every button with AutomationId
  `SystemTrayIcon`) and one button per running App.
- Invoke on the hidden-icons chevron opens the tray overflow
  (`TopLevelWindowForOverflowXamlIsland`) and the overflow takes the
  foreground. Its icons can then be read. Escape posted to the overflow
  window does nothing; posted to its XAML island's input windows it
  closes it. The chevron's Name changes while the overflow is open.
- Invoke and the default action on the clock return S_OK and open
  nothing: the calendar and notification panel does not open from UI
  Automation.

## Decision

1. **`list-windows --shell` adds the shell surfaces** to the listing, in
   z-order: the taskbars (`Shell_TrayWnd`, `Shell_SecondaryTrayWnd`)
   always, and the tray overflow, Start and the Action Center while they
   are open (visible and not cloaked). A surface is recognised by its
   window class, and the CoreWindow flyouts by their host process
   (StartMenuExperienceHost, ShellExperienceHost, ShellHost), never by a
   title, which follows the display language. The untitled taskbar is
   listed as `Taskbar`. The plain listing is unchanged, and the desktop
   (`Progman`, `WorkerW`) stays out.
2. **A shell surface is named by its Window ID only.** It resolves for
   every command, like a menu an Action reported opening. An App name
   (Windows Explorer) never names one.
3. **Reading is Headless**: the Window State, through UI Automation like
   any Window.
4. **A click on a running App's taskbar button is Headless** (UIA
   Invoke). winuse sends no input and takes no foreground itself; the
   App coming to the front is the Action's effect, which the agent asked
   for, as with `activate-window`. The output says where the foreground
   went.
5. **The taskbar's flyout buttons are Headed only**: Start, Search, Task
   View, Widgets and every notification-area button (`SystemTrayIcon`,
   matched by AutomationId, not by Name). A Headless click or Secondary
   Action on them is refused with nothing sent and the `--headed` retry:
   the chevron's Invoke takes the foreground, the clock's opens nothing,
   and the others are unmeasured flyouts. With `--headed` they are
   clicked with the real pointer, and Start opens with
   `--headed press-key <taskbar> Control_L+Escape`.
6. **Escape on the open tray overflow is Headless**: posted to the XAML
   island's input windows, read back by the overflow hiding. It does not
   give the foreground back: that stays on the taskbar.
7. **press-key keeps refusing the Windows key.** No route in this ADR
   sends it.

Every other Action on a shell surface follows the general rule (ADR 0013
rule 3, headless-max ticket 21): without a Headless route it is refused
unless the agent passed `--headed`.

## Open

The user's triage allows agent-desktop's shell accelerators (Win+N,
Win+A) as the `--headed` fallback for the notification panel. Which
command sends them is not decided yet: an exception in press-key for a
shell target, or a dedicated command like agent-desktop's
`open-system-surface`. Until then the panel opens only with a `--headed`
click on the clock.

## Consequences

- An agent can read the taskbar and switch Apps through it without
  taking the foreground itself. Switching Apps still changes what the
  user sees, so the skill says so.
- Opening the overflow, Start or the panel changes the user's screen and
  needs `--headed`. The overflow's tray icons are read only while it is
  open, so reading them is foreground-taking too.
- An Action on a shell surface reports Start or the Action Center
  opening, though each lives in its own host process.
- The shell's class names and host processes are Windows 11's (and the
  Windows 10 overflow class). A future shell that renames them drops out
  of `--shell` quietly rather than misreporting.
