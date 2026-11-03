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
4. **Acting on a shell surface's Elements is Headed only.** A Headless
   click, set-value or Secondary Action on any Element of a shell
   surface, in any form (index, `name:`/`id:`, `@x,y`), is refused with
   nothing sent and the `--headed` retry. A running App's taskbar button
   activates its App, which takes the user's foreground: ADR 0013 rule 3
   exempts only `activate-window`, so the refusal names
   `winuse activate-window <window>` when the button's App has one
   listed Window (its `list-windows --app` when it has several). The
   chevron's Invoke opens the overflow and takes the foreground, the
   clock's opens nothing, and the rest (Start, Search, Task View,
   Widgets, the other notification-area buttons, matched by
   AutomationId `SystemTrayIcon` rather than by Name, the overflow's tray
   icons, the items of an open Start or Action Center) are unmeasured
   flyouts or App activations, refused on the cautious side. Element
   scroll and select-text stay Headless: they activate nothing.
   press-key's shortcut lookup (headless-max 06), which invokes an
   Element labelled with the chord, never runs on a shell surface.
5. **With `--headed`** they are clicked with the real pointer, Start
   opens with `--headed press-key <taskbar> Control_L+Escape`, and the
   output says where the foreground went.
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

Decided on 2026-11-03: a dedicated
`--headed open-shell start|notifications|quick-settings|overflow`, built
with ticket 18, which closes this point. press-key keeps refusing the
Windows key.

ShellExperienceHost's CoreWindow also hosts the toast banner ("New
notification", probes round 5 section 7), so a visible toast is listed as
the Action Center today, and an Action on a shell surface that coincides
with one reports it as the Action Center opening. Ticket 18
(notifications) must tell the toast banner apart from the panel.

## Consequences

- An agent reads the taskbar without the foreground, but every click on
  it needs `--headed`. To switch Apps it uses `activate-window`, which
  asks for the foreground by name.
- Opening the overflow, Start or the panel changes the user's screen and
  needs `--headed`. The overflow's tray icons are read only while it is
  open, so reading them is foreground-taking too.
- An Action on a shell surface reports Start or the Action Center
  opening, though each lives in its own host process.
- The shell's class names and host processes are Windows 11's (and the
  Windows 10 overflow class). A future shell that renames them drops out
  of `--shell` quietly rather than misreporting.
