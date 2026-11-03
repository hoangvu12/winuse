# ADR 0015: Shell surfaces behind `list-windows --shell`

## Status

Accepted (desktop-parity ticket 09, `.scratch/desktop-parity/issues/09-shell-surfaces.md`;
triaged by the user on 2026-11-03: "do it like agent-desktop, Headless
first"). It reverses the shell exclusion of `internal/win/enum.go`
(`shellClasses`: the taskbars are no Windows an agent sees) for
`list-windows --shell`. Decisions 8-10 were added by ticket 18, which
closed the open point.

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
7. **press-key keeps refusing the Windows key.** Only `--headed
   open-shell` sends it (decision 8, ticket 18).

Every other Action on a shell surface follows the general rule (ADR 0013
rule 3, headless-max ticket 21): without a Headless route it is refused
unless the agent passed `--headed`.

## Closed by ticket 18 (2026-11-03)

The open point was which command sends the shell accelerators the user's
triage allows as the `--headed` fallback (Win+N, Win+A): an exception in
press-key for a shell target, or a dedicated command like agent-desktop's
`open-system-surface`. The PC orchestrator chose the dedicated command,
and desktop-parity ticket 18 (`.scratch/desktop-parity/issues/18-notifications.md`)
built it with the notifications:

8. **`winuse --headed open-shell start|notifications|quick-settings|overflow`**
   is a Headed Desktop command (`internal/helper/openshell.go`). Without
   `--headed` it is refused with nothing sent (ADR 0013 rule 3). With it,
   under the in-control indicator over the taskbar and an armed Esc scope,
   start presses Control_L+Escape, notifications Win+N and quick-settings
   Win+A (`keys.ShellAccelerator`: no key name press-key accepts reaches
   it), and overflow clicks the hidden-icons chevron with the real
   pointer, decision 5's measured route. The surface showing within 2 s is
   the read-back; its `list-windows` line is printed, with the foreground
   note. A surface that is open already is printed with nothing sent: the
   accelerators toggle. press-key keeps refusing the Windows key
   (decision 7 stands for press-key), and so does CONTEXT.md's rule.
9. **The toast banner is a surface of its own** (`win.ShellNotification`,
   "notification banner"): ShellExperienceHost's reused CoreWindow whose
   caption is "New notification" (probes round 5, section 7). This is the
   one surface told apart by its caption, against decision 1: the panel
   and the banner share their class and host process, and nothing else
   measured tells them apart. Under another display language the banner is
   taken for the Action Center, as before. An Action on a shell surface no
   longer reports a banner that shows meanwhile as a Window it opened.
10. **A toast's own buttons are the measured exception to decision 4.**
    Invoke on a toast's action button reached the raising App and Invoke
    on its dismiss button closed the banner, the foreground unchanged
    while the App was listening (round 5). So `notification-action <n>
    <button>` and `notification-dismiss <n>` press them Headless, after
    agent-desktop's identity guard (the banner read again, the
    notification acted on only while it shows with the App, title and
    text it was listed with). Their read-back is the notification leaving
    the banner. A press whose App is not listening can hand the foreground
    to the shell (round 5): the `ok` says where it went (headless-max
    03's detect-and-report). A click on the banner's Elements stays
    refused without `--headed`, and the refusal points at the
    notification commands.

The Notification Center panel still does not open from UI Automation:
the notification commands see the showing banners only, and older
notifications are read in the panel after `--headed open-shell
notifications`. Acting on the panel's Elements stays Headed (decision 4).

Unmeasured when this was written: Show and the press on a live toast
from winuse itself (toasts are off on the laptop, the e2e tests wait for
the user's approval), the panel's tree, and the quick settings host,
recognised as ShellHost.

## Consequences

- An agent reads the taskbar without the foreground, but every click on
  it needs `--headed`. To switch Apps it uses `activate-window`, which
  asks for the foreground by name.
- Opening the overflow, Start or the panel changes the user's screen and
  needs `--headed`. The overflow's tray icons are read only while it is
  open, so reading them is foreground-taking too.
- An Action on a shell surface reports Start or the Action Center
  opening, though each lives in its own host process; never a toast
  banner.
- Opening a surface is one command, `--headed open-shell`, the only
  sender of the Windows key.
- The shell's class names and host processes are Windows 11's (and the
  Windows 10 overflow class). A future shell that renames them drops out
  of `--shell` quietly rather than misreporting.
