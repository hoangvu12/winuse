# winuse

A Windows computer-use tool for coding agents: it lets any harness that can
run shell commands list Apps and Windows, read a Window State (accessibility
tree as text, screenshots on request) and perform Actions — at the quality of
Codex Computer Use, without being tied to Codex.

**Status: tickets 01–09 and 14 of the v1 plan** (`.scratch/winuse-v1/`) —
background helper, `winuse list-windows`, the fixture app with its opt-in
e2e runner, `winuse list-apps` and `winuse launch-app`, `winuse
get-window-state`: the Window State as Codex's Element tree, read through UI
Automation on a single locked COM thread, plus the recorder for seam-2
fixtures; Chromium/Electron support (a near-empty first read is retried once
after a short delay, so web content appears in the tree); the first
Actions, `winuse click` (single, double, right click by Element Index) and
`winuse activate-window`, with the shared Action pipeline every later Action
reuses; keyboard input, `winuse type-text` (arbitrary Unicode text,
typed as Unicode key events) and `winuse press-key` (keys and chords by
Codex's X keysym names); and `winuse set-value` plus
`winuse perform-secondary-action`, with the full Codex line grammar in the
Window State: each Element's rich state (value, toggled, selected,
expanded, settable) and its Secondary Actions (Raise, Scroll Down, Expand,
Collapse, plus winuse's Toggle and Select); and screenshots (ticket 08):
`get-window-state --screenshot` captures the Window through a fallback-proof method chain and saves a
model-friendly image whose path is printed, `winuse zoom` returns a
full-resolution crop of a region, and `@x,y` coordinate clicks land exactly
where aimed at any display scaling and on any monitor. Windows.Graphics
Capture (ticket 09) is now the first capture method — Codex's own —
implemented with hand-written WinRT and Direct3D interop (no cgo) on a
dedicated capture thread, falling back to the GDI chain on any failure.
Later tickets add more Actions, the overlay and the Esc stop. The product
spec and the domain language live in
`.scratch/winuse-v1/spec.md` and `CONTEXT.md`.

Windows-only. MIT licensed (see `LICENSE`).

## Build

```
CGO_ENABLED=0 go build -o winuse.exe ./cmd/winuse
```

One static Go binary, no cgo: Win32 APIs are called directly through
`golang.org/x/sys/windows` and hand-written syscall vtables; the named-pipe
transport uses `github.com/Microsoft/go-winio`.

The binary embeds a per-monitor DPI-aware (v2) manifest (the fix for
Codex's 175%-scaling click-offset bug) and version info, generated from
`winres/winres.json` into committed `.syso` files:

```
go run github.com/tc-hib/go-winres@latest make --arch amd64,arm64 --out cmd/winuse/rsrc
```

## Tests

- **Default (offline, hermetic):** `go test ./...` — protocol framing,
  window filtering/formatting and resolution, client state machine, helper
  serving and idle exit, the fixture app's Element contract, **Codex's
  key-name parsing** (`internal/keys`: every accepted name, alias and
  chord; the `unsupported key:` and Windows-key refusals) and the
  seam-2 Window State tests: recorded/hand-authored tree fixtures are
  rendered and compared with expected text (including the rich state and
  Secondary Actions columns ticket 07 populates), the bounded tree walk
  (caps, time limit, truncation notes) is tested against a fake element
  source, the Secondary Actions policy (Codex's set plus Toggle/Select)
  and the set-value/perform-secondary-action argument handling are tested
  offline, and the Chromium/Electron sparse-tree detection (the
  lazy-accessibility retry and its notes) is tested against recorded
  Chromium/Electron trees (`internal/uia/testdata/`). The
  Windows.Graphics.Capture method's pure parts are offline-tested too
  (`internal/wgc`: the GUID literals against the documented IIDs, the
  by-value struct packing, the frame-size cross-check, the BGRA→RGBA
  readback, the environment knobs); its WinRT/Direct3D calls are
  exercised only by the user-run e2e suite. No desktop access,
  no input injection, safe to run any time.
- **End-to-end (seam 1):** `test/e2e` — the real binary and the fixture app
  against the live desktop. Opt-in only, **started by the user, never by
  an agent** (see `test/e2e/README.md` for the rules):

  ```
  set WINUSE_E2E=1
  go test -tags e2e -v ./test/e2e
  ```

## How it works

`winuse` is a thin CLI client plus a background **helper** process:

- The first command spawns the helper **detached** (no console window, no
  inherited pipes — safe inside harness job objects that kill children).
- The helper listens on a named pipe `\\.\pipe\winuse-<user-SID>-<session>[-admin]`
  with a DACL granting access **only to the current user's SID**; clients
  verify the serving process really runs as that user (anti-squatting).
  A `Local\` single-instance mutex deduplicates concurrent starts.
- The CLI sends **one JSON line request per connection** and prints the
  response text (see `internal/protocol`). Requests carry the binary
  version; a helper from a different build answers "stale" and exits so
  the CLI restarts a current one — upgrades never talk to an old helper.
- The helper exits by itself after **5 minutes without requests**
  (orchestrator decision; override with `WINUSE_HELPER_IDLE`, a duration
  like `90s`); the next command restarts it transparently.
- Requests time out after **10 s** (15 s for `launch-app`) and report
  `winuse request timed out: <command>`;
  Esc stops exit with code 130 (reserved in the protocol now).
- Helper logs go to `%LOCALAPPDATA%\winuse\helper.log` (its stderr is NUL).
- The process is **per-monitor DPI aware v2** (manifest + runtime call) so
  every coordinate is physical pixels from the start.
- All **UI Automation work runs on one OS-locked, COM-initialised (MTA)
  thread** in the helper (see `internal/uia`), reached through a job queue
  — hand-written COM vtable calls, no cgo. Nothing else touches COM; there
  is **no UIA event monitoring at all** (a known cause of Codex's
  system-wide UIA freezes), and a wedged worker thread is abandoned and
  replaced, never killed.

`winuse list-windows` prints one line per visible Window:

```
<Window ID>\t<App>\t<title>
```

Hidden, DWM-cloaked and tool windows are excluded; owned dialogs ("Save
As") are listed as their own Window of the same App. UWP windows are
attributed to their real app process, and the App name is the executable's
FileDescription, falling back to the exe base name.

`winuse list-apps` prints the App catalog — every running App with its
Windows, then recently used Apps (from Explorer's UserAssist tracking,
most recent first, at most 30) marked `not running`:

```
<App name>\t<state>\t<last used>\t<use count>\t<launch identifier>
\t<Window ID>\t<title>      (one line per Window of a running App)
```

Window lines start with a tab, so App and Window lines are easy to tell
apart. The launch identifier is exactly what `launch-app` accepts: an
executable path, a Start-menu shortcut path, or an App User Model ID for
packaged (Store) Apps (their running processes adopt it automatically).

`winuse launch-app <name|exe path>` starts an App — by name (matched
case-insensitively against the catalog, installed Start-menu shortcuts and
packaged Apps; several distinct matches are an error listing them, never a
guess), or by an explicit identifier — and prints the Window it opened as
one `list-windows`-style line, within the **15 s launch timeout**. Apps
that open no Window fail with `launched app did not expose a targetable
window: <app>` (Codex's message); a single-instance App that reuses an
existing Window is recognized by that Window coming to the foreground.
Name resolution reads the registry, the Start menu and (for packaged
Apps, once per helper lifetime, best-effort) PowerShell's `Get-StartApps`;
none of it sends input.

## Reading a Window State

```
winuse get-window-state <window> [--screenshot]
```

`<window>` is a **Window ID** from `list-windows`, or an **App name** —
which resolves only when that App has exactly one Window; otherwise the
error lists the candidates (never a guess). A title is never a match.

The output is Codex Computer Use's tree format: a header line, then one
line per Element, depth-first, numbered from 0 at the root window, one TAB
per depth level:

```
Window: "Example", App: app.exe.
	0 window Example Secondary Actions: Raise
		1 edit (settable, string) Search ID: searchBox
		2 check box Notify me off Secondary Actions: Toggle
		3 button OK ID: okButton

The focused UI element is 1 edit (settable, string) Search ID: searchBox.
```

Element lines follow the grammar
`<index> <role>[ (<states>)][ <name>][ Description: …][ Value: …][ on|off|indeterminate][ Secondary Actions: …][ ID: <AutomationId>]`
(codex-surface.md §2; ADR 0004 records the deliberate divergences —
notably English role names from the numeric ControlType instead of the
locale-dependent LocalizedControlType).

The rich columns are populated from UI Automation pattern availability
and state (ticket 07): the parenthesised states are `selectable`,
`selected`, `disabled`, `collapsed`/`expanded`/`partially expanded`, and
`settable, string` (a writable value) or `settable, float` (a numeric
range); `Value:` shows a value Element's content; the bare `on`/`off`/
`indeterminate` token is a toggle's state; and `Secondary Actions:`
lists what the Element offers besides a click — Codex's Raise (windows),
Scroll Up/Down/Left/Right (scrollable Elements) and Expand/Collapse
(per the current expand state), plus winuse's Toggle and Select
extensions — which `perform-secondary-action` accepts by name:

A read is **bounded**: it is scoped to the Window with UI Automation cache
requests (one chunk per expanded node — never a bulk Subtree read, the
class of Codex freeze), capped at 3000 elements / depth 48 / 1000 children
per node, and time-limited (8 s by default; `WINUSE_TREE_BUDGET` to
override). A read that exceeds a limit returns the **partial tree** with a
`Note:` line; structural cuts add the Codex truncation suffix
`(truncated: <limit>, omitted <n> children)` to the cut Element's line.

The helper keeps the **live Elements behind the latest Window State** for
each Window (a new read replaces them), so later Action tickets can
resolve Element Indexes — and so indexes from an earlier Window State go
stale instead of hitting the wrong Element.

### Chromium and Electron apps

Chrome, Edge and Electron apps (Discord, VS Code, …) expose their web
content through UI Automation too, but **lazily**: Chromium enables its
accessibility tree only when a UI Automation client first appears, so the
very first read of such a Window is often nearly empty — the read itself
is what turns the tree on. A nearly empty first read is therefore retried
once after a short delay (500 ms), which is long enough for Chromium to
build the tree, so the same `get-window-state` call returns the page's
links, buttons and inputs. A Chromium/Electron Window whose tree is
complete but carries no document Element (the web content's root) gets the
same one retry. A tree that stays nearly empty after the retry — canvas
apps, some Electron apps — carries a note saying so and pointing the agent
at a screenshot instead.

## Screenshots and coordinates

```
winuse get-window-state <window> --screenshot
winuse zoom <window> <x>,<y>,<w>,<h>
winuse click <window> @x,y [--count 1|2|3] [--button left|middle|right]
```

A screenshot is taken **only when `--screenshot` is passed** (the tree is
the cheap default; image tokens are spent on request). The Window is
brought to the foreground first — the region capture needs it — then
captured through a **method chain** that never depends on one method:

1. **Windows.Graphics.Capture** (ticket 09) — Windows's own capture
   stack, as Codex uses: the Window's composed content, from a
   hand-written WinRT/Direct3D interop layer with no cgo (a dedicated
   OS-locked capture thread, a free-threaded frame pool polled with a 2 s
   budget, no event handlers — Codex's deadlock class never gets a
   chance). While popups are open over the Window it defers to the next
   method so they appear in the screenshot (on Windows 11 24H2+ it draws
   popups within the Window itself);
2. the **screen region** of the Window's visible bounds, widened with its
   overlapping owned popups (menus, tooltips, dropdowns), copied from the
   screen with BitBlt|CAPTUREBLT — so **menus and popups drawn over the
   Window appear in the screenshot** (but not the mouse cursor);
3. `PrintWindow` (PW_RENDERFULLCONTENT), which reaches Windows that are
   partly off-screen but renders only the Window's own content.

A near-black frame (the classic "nothing was rendered" failure) or an
error falls through to the next method; only when every method fails does
the command fail. Which method captured each frame is written to the
helper log (`%LOCALAPPDATA%\winuse\helper.log`), and two environment
knobs control the first method for diagnosis: `WINUSE_WGC=off` disables
it outright, `WINUSE_WGC_BUDGET=<duration>` bounds its wait (default 2 s) —
both read by the helper process at start.

The image is scaled so the **long edge is at most 1568 px** (a box filter,
no aliasing), saved as **JPEG quality 80** into `%TEMP%\winuse`, and its
path printed after the tree, with the scale recorded:

```
Screenshot: C:\Users\me\AppData\Local\Temp\winuse\winuse-screenshot-101-....jpg (1568x784 px, scale 0.5)
```

`@x,y` coordinate clicks and zoom regions live **in that screenshot's
coordinate space**, and the helper maps them back exactly: every
coordinate is a physical pixel (the helper is per-monitor-v2 DPI aware),
and the mapping is anchored to the Window's visible bounds **re-read at
action time** — so a click lands on the intended pixel at 100%, 150%,
175% scaling, on any monitor (negative coordinates included), and still
aims right if the Window moved since the screenshot. Coordinates are only
valid for the latest screenshot of that Window (a new screenshot replaces
the mapping; a helper restart loses it — the click fails with the
instruction to take a fresh one instead of guessing).

`zoom` maps a region of the latest screenshot back to the Window,
re-captures at full resolution and saves a **PNG crop** (reading, not
recognition — no scaling, no JPEG), with the path printed:

```
Zoom: C:\Users\me\AppData\Local\Temp\winuse\winuse-zoom-101-....png (312x208 px)
```

## Acting on a Window

```
winuse click <window> <element index | @x,y> [--count 1|2|3] [--button left|middle|right]
winuse type-text <window> <text>
winuse press-key <window> <key>
winuse set-value <window> <element index> <value>
winuse perform-secondary-action <window> <element index> <action>
winuse activate-window <window>
```

Every Action (this is the pipeline all later Action tickets reuse) does
the same thing before its own step:

1. **resolve** the Window by Window ID or App name (exactly one match);
2. **refuse a locked desktop** (`Windows desktop is locked (input desktop
   is LockApp); unlock before using winuse`) — no input is sent;
3. **refuse an administrator Window** with an explanation (Windows would
   silently drop the input, UIPI): start winuse from an elevated terminal
   to control elevated Windows;
4. **activate** the Window with the standard foreground-lock workarounds
   (a no-op injected move so the caller "received the last input", plus
   `AttachThreadInput`) and **verify it is in front** — a Window blocked
   by its own modal dialog fails naming the dialog instead;
5. then act.

`click` resolves the Element Index against the Window's cached Window
State, scrolls the Element into view when it offers ScrollItem, takes its
clickable point (or the centre of its bounds), verifies the cursor
actually reached that point and that the point is over the target's
window group — and only then injects the click through **SendInput**
(pixel-exact under per-monitor-v2, no DPI offsets), one batch so double
and triple clicks register.

`type-text` types into the Window's focused Element (check the Window
State's focused-element line first, Codex's guidance). Every argument
after the Window is part of the text, joined with single spaces — quoting
is optional, so `winuse type-text 101 hello world` and `winuse type-text
101 "hello world"` type the same text. The text is injected as **Unicode
key events** (KEYEVENTF_UNICODE, one UTF-16 unit per event, in small
batches), so any script and emoji type correctly; newline and tab are
injected as real Return/Tab keys, because many controls ignore Unicode
CR.

`press-key` presses a key or a `+`-separated chord by **Codex's X keysym
names** (`a`, `space`, `Return`, `Tab`, `Control_L+a`,
`Control_L+Shift_L+period`, `Numpad_0`; case-insensitive, `-` as `_`,
whitespace around `+` ignored, aliases like `Ctrl`, `Alt`, `period`,
`greater`, `Numpad_Add` accepted). Chords are delivered as Codex delivers
them — every key down in order, then every key up in reverse — with the
scan code filled from MapVirtualKey (some apps read scancodes; Codex's
Ctrl-chords-in-Flutter bug was zero scancodes) and KEYEVENTF_EXTENDEDKEY
on the extended keys, so arrows arrive as arrows, not numpad keys.
Shifted punctuation names map to the *unshifted* key: add an explicit
Shift (`Control_L+Shift_L+period`) to type the shifted character. Unknown
names fail with `unsupported key: <name>` plus a hint; the Windows key
(`Win`, `Windows`, `Meta`, `Command`, `Cmd`, `Super`, `OS`) is recognized
only to be refused — winuse never sends it.

`set-value` writes an Element's value directly (one reliable Action for
filling a text field): the Element is focused first — Codex's `set_value`
order — then its value is written through the UI Automation Value
pattern, or through RangeValue with the value parsed as a number for
numeric Elements (sliders). An Element that is not settable fails with
Codex's `Cannot set a value for an element that is not settable` and a
hint to click the Element and use `type-text` instead.

`perform-secondary-action` performs one of the Element's Secondary
Actions (case-insensitive): Codex's set — `Raise`, `Scroll Up`,
`Scroll Down`, `Scroll Left`, `Scroll Right`, `Expand`, `Collapse` — plus
winuse's `Toggle` and `Select` extensions, going through the matching
UI Automation pattern. An unknown action fails listing the expected
names; an action the Element does not offer fails listing the ones it
does (probed live, so an Element that changed since the Window State was
read says so).

The **output contract** for all Actions: `ok` on stdout, or a one-line
failure with a recovery hint on stderr and a non-zero exit. Timeouts are
the shared request budget (10 s). An Element Index whose Element no longer
exists (or a Window with no cached state, e.g. after a helper restart)
fails as **stale** and instructs a fresh `get-window-state` — Element
Indexes are only valid within the Window State that produced them.

### Recorder

```
winuse record-tree <window> <file.json>
```

Captures a real Window's tree and saves it as a **fixture file** — the
same JSON the renderer loads (`wstate.Tree`, format 1) — for the seam-2
offline tests (`internal/wstate/testdata/`, `internal/uia/testdata/`).
Hand-authored fixtures are written in the same format and marked as such
in their `source` field.

## The fixture app

`cmd/winusefixture` is the e2e test target: a pure-Go, no-cgo Win32 app
whose Elements are known in advance, because it implements a hand-written
**server-side UI Automation provider** (raw COM vtables, no cgo) — the
only way a Win32 app gets stable, non-empty AutomationIds. It has buttons,
a text field, a checkbox, a list, menus, a hover-only toolbar, a canvas,
and a "Save As"-style dialog that opens as its own Window.

**Its Element contract — every Element's name, AutomationId, role,
patterns and behaviour — is documented in one place:
`internal/fixture/elements.go`.** Tests target those values and import the
package for the constants. The app reports its end state (text field
contents, checkbox state, last button pressed, canvas clicks, …) as JSON
to the file given by `-state`, rewritten atomically on every change, so
assertions never depend on pixels.

## Layout

```
cmd/winuse/          binary: CLI dispatch + hidden __helper entry (manifest .syso here)
cmd/winusefixture/   the e2e fixture app's entry point (see The fixture app)
internal/protocol/   CLI↔helper contract: JSON-lines request/response, timeouts, exit codes
internal/pipe/       named pipe: naming per user/session, user-only DACL, dial, anti-squat check, singleton
internal/win/        Win32 layer: identity, DPI awareness, window enumeration, process/App names, window resolution, helper spawn, and the Actions' Win32 side: SendInput clicks, keyboard (Unicode text and key chords), foreground activation, integrity/locked-desktop refusals
internal/apps/       App catalog and launching: list-apps (UserAssist, Start menu, store Apps), launch-app (CreateProcess/ShellExecuteExW + Window polling)
internal/keys/       Codex/X keysym key-name and chord parsing for press-key (pure Go, fully covered offline)
internal/helper/     helper: serve loop, idle exit, stale-exit, command registry, window-state + record-tree + list-apps + launch-app commands
internal/uia/        UI Automation: locked COM worker thread, bounded tree walk, Chromium/Electron lazy-tree retry, live-Element store (raw vtables, no cgo)
internal/shot/       screenshots: frames, capture-method chain, box-filter scaling, the screenshot↔Window coordinate mapping and its per-Window store (pure, offline-tested)
internal/wgc/        Windows.Graphics.Capture: hand-written WinRT/Direct3D interop (raw vtables, no cgo), the dedicated capture thread, the capture flow and its fallbacks
internal/wstate/     Window State model: Element/Tree, Codex-format rendering, recorded-tree fixtures (seam 2)
internal/cli/        CLI: subcommands, dial-or-spawn client state machine, output/exit-code policy
internal/version/    binary version (stale-helper detection)
internal/fixture/    the fixture app: Element contract (elements.go), state file, UIA provider, Win32 app
test/e2e/            opt-in end-to-end tests (user-run only; see test/e2e/README.md)
winres/              manifest + version-info source for the .syso files
```

## Dependencies

| Module | License | Use |
|---|---|---|
| golang.org/x/sys | BSD-3-Clause | Win32 API calls |
| github.com/Microsoft/go-winio | MIT | named-pipe transport |
| tc-hib/go-winres (tool, not imported) | 0BSD | manifest/version .syso generation |
