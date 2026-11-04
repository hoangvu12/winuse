# Finding, launching, placing and closing Windows

Read when: you need `list-windows` or `launch-app` flags (`--details`, `--app`, `--arg`, `--cwd`, `--env`, `--cdp`), dialogs, displays, or to restore, minimize, maximize, move, resize or close Windows and Apps.

This is one topic of the winuse skill. `winuse skill windows` prints it, and `winuse skill` prints the core, SKILL.md, which holds the safety rules, the Esc stop and recovery.

## Find the Window

```
winuse list-windows                 # one line per Window: <Window ID>  <App>  <title>
winuse list-windows --details       # adds <x,y WxH pid N flags> before the title
winuse list-windows --app <name>    # only that App's Windows (with or without --details)
winuse list-windows --shell         # adds the taskbar and the open shell flyouts (winuse skill shell)
winuse get-window <window>          # re-resolve one Window and print its line
winuse list-apps                    # running and recent Apps, with their Windows
winuse launch-app <name|exe path>   # start an App and print the Window it opened
```

Name a Window by the Window ID that `list-windows`, `list-apps` or
`launch-app` printed. An App name works only when that App has exactly one
Window; otherwise the error lists the candidates. `get-window` is a cheap
check that a Window still exists, for example after a helper restart.

Windows hands a closed Window's ID to a new Window. An Action on an ID a
new Window took is refused with
`window <id> closed; a new window reused its ID; nothing was sent`. Run
`list-windows` again, and read the new Window before you act on it.

`--details` adds the visible frame (the numbers `move-window` and
`resize-window` take), the pid and the flags `focused`, `minimized`,
`maximized` and `admin`, shown only when true, such as
`120,80 1024x768 pid 4312 focused`. winuse cannot act in an `admin`
Window. A minimized Window's frame is off screen, not a place to act.
`--app` takes an App or executable name; an App with no open Window is an
error, not an empty list.

If the App has no Window yet, `launch-app` it by name, by the launch
identifier `list-apps` prints, or by exe path. If it reports no Window, run
`list-windows` again before you decide the launch failed. A launch waits 15
seconds; pass `--timeout <seconds>` (up to 120) for a slow App.

```
winuse launch-app <app> --minimized                  # open its Window minimized
winuse launch-app <exe path> --arg <a> --arg <b>     # one --arg per argument
winuse launch-app <exe path> --cwd <dir> --env KEY=VALUE
winuse launch-app <exe path> --cdp                   # an Electron or WebView2 App, read like a browser
```

The App starts without taking the user's foreground, so its Window may
open behind their work. Some Apps, and packaged Store Apps such as
Calculator, take it anyway; a second line then says
`Note: the App took the foreground; winuse does not take it back.` Tell
the user if they were working elsewhere, and do not launch it again.

`--arg` passes one argument each, in order, quoted for you, so a path with
spaces is one `--arg`. `--cwd` sets the working folder, and `--env` adds or
replaces one environment variable. These three need an executable: a
shortcut or packaged App is refused with nothing started, and the hint
names the executable when winuse knows it. An App can ignore
`--minimized`.

`--cdp` starts an Electron App (VS Code, Slack, Discord, Teams) or a
WebView2 App, from its executable, with a loopback debugging port. A second
line then says `Note: a Browser Connection reaches the App's pages`, and
its Windows work like a browser Window with a Browser Connection (see
"Browser Windows" in `winuse skill browser`). When it says no debugging port appeared, the App stays
on UI Automation. `--cdp` works only at launch, and winuse never restarts a
running App; ask the user before you close one to relaunch it.

A dialog such as Save As, a file picker or a message box is its own Window
of the same App; target it directly. An Action that opens one names it in a
note after its `ok` (see "Read the result" in the core); otherwise look in
`list-windows`, which leaves out hidden, cloaked and tool windows and the
shell.

### Displays

```
winuse list-displays                # one line per display: <n>  <x,y WxH>  scale <s>  [primary]
winuse screenshot --display <n>     # a screenshot of the whole display
```

`list-displays` numbers the displays from 0, the primary, with each one's
rectangle in physical pixels (the coordinates `move-window` takes) and
scale (1.5 is 150%). `screenshot --display <n>` (or `--screen <n>`)
captures that display with every Window on it, without the foreground, and
prints `Screenshot: <path> (WxH px, scale S, display N at X,Y WxH)`. It is
for looking only, with no `@x,y` points and no `zoom`; to act at a point,
use `get-window-state --screenshot`.

## Manage Windows

```
winuse restore-window <window>                   # un-minimize or un-maximize, at normal size
winuse minimize-window <window>
winuse move-window <window> <x> <y>              # visible frame top-left, screen physical pixels
winuse resize-window <window> <width> <height>   # visible frame size, physical pixels
winuse close-window <window>                     # the same request as the close button
winuse --headed maximize-window <window>         # a real maximize; takes the foreground
winuse close-app <app> [--force]                 # every Window of the App, then waits for it to exit
```

Window Actions change the Window without bringing it to the front, read it
back, and print `ok`. They are not Batch steps.

- `maximize-window` is the exception: Windows cannot maximize without
  activating, so it needs `--headed` (refused with nothing sent without
  it), brings the Window to the foreground under the banner and reads the
  maximized state back. Tell the user first if they are working elsewhere;
  to keep their foreground, `move-window` and `resize-window` it to fill
  the monitor instead. An already maximized Window prints `ok`.
- A Window maximized before it was minimized is restored at its normal
  size, and the output says so; use `winuse --headed maximize-window` if
  the user wants it maximized again.
- Some Apps bring themselves to the front when restored, most often one
  not in front since it started. `restore-window` then adds
  `Note: the App brought itself to the foreground`; winuse does not take it
  back. Tell the user if they were working elsewhere, and do not repeat
  the restore.
- `move-window` and `resize-window` refuse a minimized or maximized Window
  (`restore-window` it first) and take the screenshot line's
  `window at X,Y WxH` numbers directly. An app can limit its placement; a
  placement that does not read back fails as delivered, so read the
  rectangle again rather than repeating it.
- `close-window` asks the Window to close and waits up to two seconds. If
  it stays open, the output says so: the app may be asking to save, so
  handle that prompt.
- `close-app` names the App as `list-windows` prints it, or by executable
  name, asks every Window to close and waits up to five seconds for it to
  exit. One still running gets `ok` and `Note: the close was requested;
  <App> is still running, it may be asking to save: ...`.
  It refuses administrator Apps, a locked desktop and Windows' own
  processes (explorer, dwm, csrss, winlogon and the like) with nothing
  sent.
- Only `close-app --force` ends a process: after the same request and
  wait, it ends the App's processes still running, losing unsaved work.
  Use it only when the user asked for the App to be ended, never to get
  past a save prompt you could answer.
