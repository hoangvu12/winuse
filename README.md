# winuse

winuse lets a coding agent use Windows apps. Any harness that can run shell
commands can list the open windows, read a window's accessibility tree as
text, take a screenshot when the text isn't enough, and click, type, scroll
or drag. It follows the command surface and output format of Codex Computer
Use, so agents trained on Codex's tools feel at home, but it works with any
agent (Claude Code, Pi, or anything with a shell).

It runs on Windows only, sends no telemetry, and is MIT licensed.

## Install

The binary installs with one PowerShell line and needs no Go toolchain:

```powershell
irm https://raw.githubusercontent.com/hoangvu12/winuse/main/install.ps1 | iex
```

The script downloads the latest release to `%LOCALAPPDATA%\winuse\winuse.exe`,
removes its Mark of the Web and adds it to your user PATH. Open a new
terminal afterwards so the PATH change is visible. It downloads to a
temporary file and stops a running winuse helper before swapping the file in,
so a failed download leaves a working install alone.

The installer needs a published GitHub Release. Until there is one, or if
you prefer to build it yourself, use Go 1.27 or newer:

```powershell
$env:CGO_ENABLED=0; go build -o winuse.exe ./cmd/winuse
```

Then install the skill, so your agent knows how to use winuse:

```
npx skills add hoangvu12/winuse
```

The skill is the [`SKILL.md`](SKILL.md) at the repo root. It installs into
the project's `.claude/skills/` and `.agents/skills/` by default, or the
user-level directories with `-g`, which covers Claude Code, Pi and the other
agents the [skills CLI](https://skills.sh) supports. Until the agent uses
it, the skill costs only its name and description in context. After that,
ask for something like "open Notepad and type hello world using the winuse
skill".

### The unsigned binary

Version 1 isn't code signed, so Windows may warn about it. The release
binaries are built by [this workflow](.github/workflows/release.yml) from
the tagged source, and you can always build your own.

- SmartScreen says "Windows protected your PC": choose More info, then Run
  anyway. The installer already strips the Mark of the Web that triggers
  this, so it should be rare.
- Defender Antivirus flags the file: open Windows Security, Virus & threat
  protection, Protection history, find the winuse detection and choose
  Allow. Allow that one file rather than turning protection off.
- A launch is blocked with a zone error: run
  `Unblock-File "$env:LOCALAPPDATA\winuse\winuse.exe"` once.

## Quick start

An agent works in a loop: read the window, do one thing, read it again.
Reading the tree is cheap, screenshots are opt-in, and pressing the physical
Esc key stops whatever winuse is doing.

```
C:\> winuse list-windows
101     Example    Example App
142     Notepad    *untitled - Notepad
...

C:\> winuse get-window-state 101
Window: "Example", App: app.exe.
	0 window Example Secondary Actions: Raise
		1 edit (settable, string) Search ID: searchBox
		2 button OK ID: okButton

The focused UI element is 1 edit (settable, string) Search ID: searchBox.

C:\> winuse set-value 101 id:searchBox "hello"
ok
C:\> winuse click 101 name:OK
ok
C:\> winuse batch 101 click id:field then type-text hello then press-key Return then click name:OK
ok
```

A `<window>` is a Window ID from `list-windows`, or an app name when that
app has exactly one window. Elements are named by their index, `name:`,
`id:`, or `@x,y` coordinates in the latest screenshot. Actions print `ok`,
or a one-line error with a hint about how to recover.

[`SKILL.md`](SKILL.md) is the full guide agents load, with the safety rules
and the recovery steps. The rest of this README covers the same commands for
people, and how winuse works inside.

## Commands

```
winuse list-windows                  visible windows: Window ID, app, title
winuse get-window <window>           one window's line, re-resolved
winuse list-apps                     running and recently used apps, with their windows
winuse launch-app <name|exe path>    start an app and print the window it opened
winuse get-window-state <window> [--screenshot]
winuse zoom <window> <x>,<y>,<w>,<h>
winuse click <window> <ref> [--count 1|2|3] [--button left|middle|right]
winuse type-text <window> <text>
winuse press-key <window> <key>
winuse set-value <window> <ref> <value>
winuse perform-secondary-action <window> <ref> <action>
winuse scroll <window> @x,y <dx> <dy>
winuse drag <window> @x,y @x,y
winuse hover <window> @x,y
winuse activate-window <window>
winuse batch <window> <action> [args] [then <action> [args]] ...
winuse version
```

### Finding a window

`list-windows` prints one tab-separated line per window a person could see.
Hidden, cloaked and tool windows are left out. A dialog such as "Save As"
is listed as its own window of the same app, and the app name is the
executable's FileDescription (or its file name when there is none).

When an app name matches several windows, winuse lists the candidates and
stops. It never matches on a title and never guesses.

`list-apps` shows every running app with its windows, followed by up to 30
recently used apps from Explorer's UserAssist history. Each app line ends
with a launch identifier (an exe path, a Start menu shortcut, or a Store
app's AppUserModelID) that `launch-app` accepts. `launch-app` also takes a
plain name, matched against that catalog, the Start menu and installed Store
apps. It waits up to 15 seconds for a window and fails with `launched app
did not expose a targetable window` when none appears.

### Reading a window

`get-window-state` prints the window's element tree in Codex's format: a
header, then one line per element, depth first, numbered from 0, with one tab
per level.

```
Window: "Example", App: app.exe.
	0 window Example Secondary Actions: Raise
		1 edit (settable, string) Search ID: searchBox
		2 check box Notify me off Secondary Actions: Toggle
		3 button OK ID: okButton

The focused UI element is 1 edit (settable, string) Search ID: searchBox.
```

Each line follows
`<index> <role>[ (<states>)][ <name>][ Description: …][ Value: …][ on|off|indeterminate][ Secondary Actions: …][ ID: <AutomationId>]`.
States include `selectable`, `selected`, `disabled`, `collapsed` or
`expanded`, and `settable, string` or `settable, float`. Secondary Actions
are what an element offers besides a click: Codex's Raise, Scroll
Up/Down/Left/Right, Expand and Collapse, plus winuse's Toggle and Select.
Role names are English, taken from the numeric control type rather than the
localized name. [ADR 0004](docs/adr/) records where the format departs from
Codex on purpose.

A read is limited to the target window and bounded: at most 3000 elements,
depth 48, 1000 children per node, and 8 seconds by default
(`WINUSE_TREE_BUDGET` changes the time). If a read hits a limit, you get the
partial tree with a `Note:` line, and cut elements carry Codex's
`(truncated: …)` suffix.

The helper keeps the live elements behind each window's latest read, so an
index means the same element across separate commands. Indexes from an
older read go stale and fail with an instruction to read the window again,
instead of hitting the wrong element.

Chrome, Edge and Electron apps expose their web content too, but Chromium
only builds its accessibility tree once an accessibility client shows up,
so the first read is often almost empty. winuse retries such a read once
after 500 ms. A tree that is still nearly empty after that (canvas apps,
some Electron windows) comes with a note suggesting a screenshot.

### Screenshots and coordinates

`get-window-state --screenshot` also saves a screenshot and prints its path,
size and scale:

```
Screenshot: C:\Users\me\AppData\Local\Temp\winuse\winuse-screenshot-101-....jpg (1568x784 px, scale 0.5)
```

winuse brings the window to the front and tries three capture methods in
order:

1. Windows.Graphics.Capture, the method Codex uses. It defers to the next
   method while a popup is open over the window, so menus show up.
2. A copy of the window's screen region, widened to include its open menus,
   tooltips and dropdowns.
3. `PrintWindow`, which reaches windows that are partly off screen.

A failed or near-black frame falls through to the next method. The image is
scaled so its long edge is at most 1568 px and saved as a JPEG in
`%TEMP%\winuse`. The helper log (`%LOCALAPPDATA%\winuse\helper.log`) records
which method produced each frame. `WINUSE_WGC=off` and
`WINUSE_WGC_BUDGET=<duration>` disable or time-limit the first method for
troubleshooting.

`@x,y` references and `zoom` regions use the latest screenshot's pixels.
The helper is per-monitor DPI aware and maps each point against the
window's position at the moment of the action, so clicks land where aimed at
any display scaling, on any monitor, and after the window has moved. A new
screenshot replaces the mapping. After a helper restart, coordinate actions
ask for a fresh screenshot. `zoom` re-captures a region at full resolution
and saves it as a PNG.

### Acting on a window

Before every action, winuse resolves the window, refuses if the desktop is
locked or the window belongs to an administrator process (Windows would drop
the input silently), shows the in-control overlay, brings the window to the
front and checks that it got there. A window blocked by its own modal dialog
fails with the dialog's name.

`click` finds the element in the latest read, scrolls it into view when the
element supports that, moves the pointer to its clickable point, checks the
point is over the target window, and sends the click with SendInput.

Elements can be referenced four ways:

```
winuse click 101 7                          the index from the latest read
winuse click 101 name:OK                    the name exactly as the tree printed it
winuse click 101 id:okButton                the ID column
winuse click 101 @412,88                    a point in the latest screenshot
```

Names and IDs are matched exactly and case-sensitively against the latest
read. Several matches fail with the candidates listed as tree lines. They
survive re-reads better than indexes, since an element keeps its name while
its index shifts. Quote a reference with spaces: `"name:Save As"`. For
`set-value` and `perform-secondary-action`, `@x,y` acts on the element under
the point, which lets an agent work a window whose tree is sparse but whose
screenshot is readable.

`type-text` types into the focused element. Every argument after the window
is part of the text, so quotes are optional. Text is sent as Unicode key
events, so any script and emoji work; newlines and tabs are sent as real
Return and Tab keys.

`press-key` takes Codex's X keysym names: `Return`, `Tab`, `space`, `a`,
`Control_L+a`, `Control_L+Shift_L+period`, `Numpad_0`. Names are
case-insensitive, and aliases such as `Ctrl` and `Alt` work. Chords press
every key down in order and release them in reverse, with scan codes filled
in and extended keys flagged, so arrows arrive as arrows. An unknown name
fails with `unsupported key: <name>`. The Windows key is refused.

`set-value` focuses the element and writes its value through UI
Automation, which fills a text field in one step. Sliders take a number. A
non-settable element fails with Codex's message and a hint to click it and
use `type-text`.

`perform-secondary-action` runs one of the element's Secondary Actions. An
action the element doesn't offer fails with the ones it does.

`scroll`, `drag` and `hover` aim at screenshot points. `scroll` takes wheel
units (one notch is 120); positive `dy` scrolls down and positive `dx`
scrolls right. `drag` presses the left button, moves in small steps so the
app sees a real drag, and always releases the button. `hover` leaves the
pointer in place, so hover-only toolbars and tooltips stay visible while the
agent reads the window again.

Actions print `ok` on stdout, or a one-line error and hint on stderr with a
non-zero exit: 2 for usage errors, 1 for failures, 130 when the user pressed
Esc.

### Batches

`batch` runs several actions on one window in one call, stopping at the
first failure:

```
winuse batch 101 click id:field then type-text hello world then press-key Return then click name:OK
```

Each step is an action command without its window argument, and `then`
separates steps (quote text that contains the word "then"). A failure names
the step and forwards its hint:

```
batch stopped at Action 2 (click): name "Twin" matches 2 elements in the cached Window State for window 101
```

Malformed steps are rejected before any input is sent. A batch has 60
seconds instead of the usual 10, and Esc stops it between steps.

## The overlay and the Esc key

While an action or batch runs, a border frames the window and a banner at
the top reads "winuse is in control — press Esc to stop". The overlay
doesn't take focus, lets clicks through to the window under it, stays out of
Alt-Tab and the taskbar, and is hidden while winuse takes its own
screenshots. If the overlay fails to appear, the action still runs.

Pressing the physical Esc key during an action stops it. A low-level
keyboard hook sees the press and swallows it, so the app never receives it.
The command exits with code 130 and tells the agent the user stopped it and
that it should ask before continuing. Keys that winuse injects itself are
ignored, so `press-key <window> Escape` sends an ordinary Escape to the app.
When winuse isn't acting, Esc passes through untouched. A stop during typing
takes effect between batches of keystrokes; a stop during a batch skips the
remaining steps.

## How it works

`winuse` is a small CLI plus a background helper process.

- The first command starts the helper detached, with no console window. It
  exits after 5 minutes without requests (`WINUSE_HELPER_IDLE` changes this),
  and the next command starts it again.
- The CLI talks to the helper over a named pipe that only the current user
  can open, and checks that the process serving it runs as that user.
- Each command is one JSON request on its own connection. A helper from a
  different build answers "stale" and exits, so an upgrade never talks to an
  old helper.
- Requests time out after 10 seconds (15 for `launch-app`, 60 for a batch).
- All UI Automation calls run on one dedicated thread through hand-written
  COM calls. winuse subscribes to no UI Automation events, which avoids a
  known cause of Codex's system-wide freezes.
- A second dedicated thread runs a Win32 message loop for the overlay and
  the Esc hook.
- The binary is per-monitor DPI aware, so every coordinate is in physical
  pixels.

It is one static Go binary with no cgo. Win32 calls go through
`golang.org/x/sys/windows` and hand-written syscall tables, and the pipe uses
`github.com/Microsoft/go-winio`.

[`CONTEXT.md`](CONTEXT.md) defines the terms used throughout (Window,
Element, Action, Batch and so on), [`docs/adr/`](docs/adr/) records the
design decisions, and [`docs/codex-parity.md`](docs/codex-parity.md) checks
every Codex Windows tool and parameter against winuse.

## Tests

`go test ./...` runs the offline tests. They cover the protocol, window
filtering and resolution, the client and helper, key-name parsing, tree
rendering against recorded trees, the bounded tree walk, element
references, batches, the Esc hook's logic, the overlay's show and hide
rules, the shipped skill and the Task Suite runner. They need no desktop and
send no input.

The end-to-end tests in `test/e2e` drive the real binary against a purpose
built fixture app (`cmd/winusefixture`) whose elements are known in advance.
They take over the screen, so they only run when you opt in:

```powershell
$env:WINUSE_E2E=1; go test -tags e2e -v ./test/e2e
```

[`test/e2e/README.md`](test/e2e/README.md) explains the suite, including the
one test that asks you to press Esc and how to repeat the coordinate tests at
other display scalings or on a second monitor.

The Task Suite in [`tasksuite/`](tasksuite/) measures quality rather than
correctness. It hands about ten real tasks (Notepad, File Explorer, Edge,
Paint, VS Code) to an agent running unattended, and a script checks each
end result. It also takes over the screen and only starts when you pass
`--i-am-the-user`:

```
go run ./cmd/winusetasks run --i-am-the-user
```

Results are kept in [`tasksuite/results/`](tasksuite/results/).

## Building and releasing

```powershell
$env:CGO_ENABLED=0; go build -o winuse.exe ./cmd/winuse
```

The DPI manifest and version info live in `winres/winres.json` and are
compiled into committed `.syso` files:

```
go run github.com/tc-hib/go-winres@latest make --arch amd64,arm64 --out cmd/winuse/rsrc
```

To release, bump the version in `internal/version/version.go` and
`winres/winres.json`, regenerate the `.syso` files, then commit and push a
tag such as `v0.1.0`. The [release workflow](.github/workflows/release.yml)
builds `winuse_windows_amd64.exe` and `winuse_windows_arm64.exe` and creates
the GitHub Release, which `install.ps1` downloads from `releases/latest`.

## Dependencies

| Module | License | Use |
|---|---|---|
| golang.org/x/sys | BSD-3-Clause | Win32 API calls |
| github.com/Microsoft/go-winio | MIT | named-pipe transport |
| tc-hib/go-winres (build tool, not imported) | 0BSD | manifest and version resources |
