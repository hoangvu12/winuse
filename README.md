# winuse

A Windows computer-use tool for coding agents: it lets any harness that can
run shell commands list Apps and Windows, read a Window State (accessibility
tree as text, screenshots on request) and perform Actions — at the quality of
Codex Computer Use, without being tied to Codex.

**Status: tickets 01–15 of the v1 plan** (`.scratch/winuse-v1/`; only the
Task Suite — 16 — remains): the background helper and the full command
surface — discovery (`list-windows`, `list-apps`, `launch-app`),
observation (`get-window-state`, Codex's Element-tree format with opt-in
screenshots and `zoom`) and every Action (`click`, `type-text`,
`press-key`, `set-value`, `perform-secondary-action`, `scroll`, `drag`,
`hover`, `activate-window`, `batch`), with Element References (index,
`name:`, `id:`, `@x,y`), the Chromium/Electron lazy-tree retry, the
in-control overlay, the Esc stop (exit 130), and the
Windows.Graphics.Capture-first screenshot chain — plus, with ticket 15, the
shipped agent skill, installer and release workflow. The product spec and
the domain language live in `.scratch/winuse-v1/spec.md` and `CONTEXT.md`.

Windows-only. MIT licensed (see `LICENSE`). No telemetry.

## Install

**The binary** — Windows only, one PowerShell line, no Go toolchain
needed:

```powershell
irm https://raw.githubusercontent.com/hoangvu12/winuse/main/install.ps1 | iex
```

That downloads the release binary from GitHub Releases to
`%LOCALAPPDATA%\winuse\winuse.exe`, removes its Mark of the Web, and adds
it to your user PATH — restart the terminal once so the PATH is visible.
To build from source instead: Go 1.27 or newer, then
`CGO_ENABLED=0 go build -o winuse.exe ./cmd/winuse`.

**The skill** — so your agents load winuse's usage guidance. The skill is
the `SKILL.md` at the repo root, and it installs into both Claude Code's
and Pi's skill locations (and any other agent the [skills
CLI](https://skills.sh) supports):

```
npx skills add hoangvu12/winuse
```

It is project-scoped by default (`.claude/skills/`, `.agents/skills/`);
`-g` installs to the user-level directories (`~/.claude/skills/`,
`~/.agents/skills/`). Pi discovers `.agents/skills/` from the working
directory up through its ancestors, so a project install just works. The
skill is self-contained: even when only its one `SKILL.md` file is
installed, the guidance is complete.

Then ask your agent to use it — e.g. *“open Notepad and type hello world
using the winuse skill”*. The skill costs only its name and description in
context until it is used.

### Allowing the unsigned binary

winuse is **unsigned for v1** (code signing is out of scope), so Windows
may flag the freshly downloaded `winuse.exe`. All of these are false
positives — the binary is built by [this
workflow](.github/workflows/release.yml) from the tagged source in this
repo, and you can always build it yourself instead:

- **SmartScreen** — “Windows protected your PC” on the first run: choose
  **More info → Run anyway**. This dialog comes from the Mark of the Web
  that browsers put on downloaded files; the installer already strips it,
  so you should rarely see this.
- **Defender Antivirus** flags the file (rare): Windows Security →
  Virus & threat protection → **Protection history** → open the winuse
  detection → **Allow**. Allow that one file — don't disable real-time
  protection.
- A launch blocked with a zone error: run
  `Unblock-File "$env:LOCALAPPDATA\winuse\winuse.exe"` once.

## Quick start

The working loop is **observe → one Action (or one Batch) → observe
again**; the tree is the cheap read, screenshots are opt-in, and the
physical Esc key stops anything winuse is doing:

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

(Abridged; see the sections below for the real formats.) A `<window>` is
a Window ID from `list-windows`, or an App name when that App has exactly
one Window. Elements are named by index, `name:`, `id:` or — after a
screenshot — `@x,y` coordinates. Actions print `ok` or a one-line failure
with a recovery hint; the user pressing Esc exits 130 with a message that
says so.

The complete guidance your agents load — including the safety rules, the
recovery ladder and the Esc contract — is [`SKILL.md`](SKILL.md); this
README documents the same surface for humans and the internals for
developers.

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
each Window (a new read replaces them), so Element Indexes resolve to the
same Element across CLI calls — and indexes from an earlier Window State
go stale instead of hitting the wrong Element.

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
the command fail. Every method runs with the in-control overlay hidden, so
winuse's own screenshots never contain it even while an Action runs. Which
method captured each frame is written to the
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

`@x,y` coordinate references — clicks, and the pointer actions scroll,
drag and hover — and zoom regions live **in that screenshot's coordinate
space**, and the helper maps them back exactly: every
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
winuse click <window> <element index | name:<text> | id:<id> | @x,y> [--count 1|2|3] [--button left|middle|right]
winuse type-text <window> <text>
winuse press-key <window> <key>
winuse set-value <window> <element index | name:<text> | id:<id>> <value>
winuse perform-secondary-action <window> <element index | name:<text> | id:<id>> <action>
winuse scroll <window> @x,y <dx> <dy>
winuse drag <window> @x,y @x,y
winuse hover <window> @x,y
winuse activate-window <window>
winuse batch <window> <action> [arguments] [then <action> [arguments]] ...
```

Every Action (this is the pipeline all Action commands share) does the
same thing before its own step:

1. **resolve** the Window by Window ID or App name (exactly one match);
2. **refuse a locked desktop** (`Windows desktop is locked (input desktop
   is LockApp); unlock before using winuse`) — no input is sent;
3. **refuse an administrator Window** with an explanation (Windows would
   silently drop the input, UIPI): start winuse from an elevated terminal
   to control elevated Windows;
4. **raise the in-control overlay** over the target Window (best effort —
   see below); it comes down when the Action ends, whatever the outcome;
5. **activate** the Window with the standard foreground-lock workarounds
   (a no-op injected move so the caller "received the last input", plus
   `AttachThreadInput`) and **verify it is in front** — a Window blocked
   by its own modal dialog fails naming the dialog instead;
6. then act.

`click` resolves the Element Index against the Window's cached Window
State, scrolls the Element into view when it offers ScrollItem, takes its
clickable point (or the centre of its bounds), verifies the cursor
actually reached that point and that the point is over the target's
window group — and only then injects the click through **SendInput**
(pixel-exact under per-monitor-v2, no DPI offsets), one batch so double
and triple clicks register.

### Element References

Every Element-taking Action (click, set-value, perform-secondary-action)
accepts the same Element Reference forms: an **Element Index** (the
number the Window State prints), **`name:<text>`** (the Element's name,
the text before any `Description:`/`Value:`/`ID:` column), **`id:
<AutomationId>`** (the `ID:` column), and — for click — **`@x,y`**
(coordinates in the latest screenshot's space, see above):

```
winuse click 101 name:OK
winuse click 101 id:ParityButton
winuse set-value 101 id:ParityBox "hello"
winuse perform-secondary-action 101 name:Details expand
```

A reference resolves against the **Window's latest Window State**: the
name or ID is matched against exactly what that tree printed
(whitespace-collapsed, otherwise **exact and case-sensitive**), so a
reference copied from the tree resolves to the Element behind that line.
Exactly one match must resolve — a name or ID matching several Elements
fails listing the candidates as tree lines, never guessing:

```
name "Twin" matches 2 elements in the cached Window State for window 101
hint: candidates:
1 button Twin ID: twin-a
2 button Twin ID: twin-b
```

and no match fails with the instruction to read the Window State again.
Like an Element Index, a reference is only valid within the Window State
that produced it — the Element behind it can go stale, and a re-read of
the Window State is the recovery. The advantage over an Index is
stability across re-reads: the Element keeps its name and ID while its
Index shifts as the UI changes. A reference with spaces is one argument
(`winuse click 101 "name:Save As"`); an unquoted one is caught with a
hint that says to quote it.

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

`scroll`, `drag` and `hover` are the pointer Actions: they aim at
`@x,y` points of the Window's **latest screenshot** (mapped back exactly
like coordinate clicks — see above) rather than at Elements, because
where the pointer is matters to them. `scroll` moves the pointer to the
point and injects the wheel there — both axes at once, amounts in wheel
units (one notch is 120), Codex's signs: positive `dy` scrolls down,
positive `dx` scrolls right, negatives scroll up/left; a point over
another window is refused, not scrolled. `drag` places the pointer at
the from-point, presses the left button (Codex supports no other),
injects ~16 interpolated absolute moves ~10 ms apart to the to-point —
so apps register a drag (menus track, canvases draw, lists reorder)
instead of a teleport — pins the exact end point and releases; the
button always comes up, even if a move fails. `hover` moves the pointer
to the point — an injected absolute move plus the pixel-exact position
— and **leaves it there**, so hover-only toolbars and tooltips appear
and stay while the agent re-observes. All three go through the same
Action pipeline (activation, refusals, overlay) as every other Action.

The **output contract** for all Actions: `ok` on stdout, or a one-line
failure with a recovery hint on stderr and a non-zero exit. Timeouts are
the shared request budget (10 s). An Element Index whose Element no longer
exists (or a Window with no cached state, e.g. after a helper restart)
fails as **stale** and instructs a fresh `get-window-state` — Element
Indexes are only valid within the Window State that produced them.

### Batches

A **Batch** runs several Actions on one Window in a single call, in
order, stopping at the first failure and naming the failed step — so a
routine sequence (click a field, type into it, press Return, click OK)
costs one round-trip instead of five:

```
winuse batch 101 click id:field then type-text hello world then press-key Return then click name:OK
```

Steps are separated by `then` (the first needs no `then`), and each step
is an Action command **without its Window argument** — the same arguments
the standalone form takes: `click <ref> [options]`, `type-text <text>`,
`press-key <key>`, `set-value <ref> <value>`,
`perform-secondary-action <ref> <action>`, `activate-window`. Variadic
arguments (a step's text or value) join that step's remaining arguments
with single spaces, so an unquoted `then` inside them still separates
steps — quote the text to keep it. Every step runs through the same
command the standalone form uses, so the full pipeline (window
resolution, refusals, activate-and-verify) applies per step.

A successful Batch prints `ok`, Codex-style (Codex has no batch tool; its
model chains calls in one JS cell — winuse moves the chaining into one
command). A failed step stops the Batch:

```
batch stopped at step 2 (click): name "Twin" matches 2 elements in the cached Window State for window 101
```

with that step's own recovery hint (here the candidate list) forwarded.
Steps after the failure do not run, and the failure's exit code survives
(an Esc stop exits 130, like a standalone Action). Malformed steps —
an unknown action, a bad flag — are refused **before any input is sent**,
so a mistyped Batch never half-runs. A Batch's request budget is 60 s
(each step is fast, but a Batch of dozens of steps needs more than the
10 s default). Pressing the physical Esc key aborts a running Batch at
the next checkpoint — between a stopped step's input batches, between
steps, in a step's tail — and the remaining steps do not run. The
pointer Actions are Batch steps too (`scroll`, `drag`, `hover`, taking
the same arguments as their standalone forms minus the Window).

## The in-control overlay

While an Action runs, winuse shows what Codex shows: an on-screen
indicator that the agent is driving the PC. A border frames the Window
being controlled and a pill banner at its top centre reads
**“winuse is in control — press Esc to stop”** — and since ticket 12,
pressing that key really does stop winuse (see the Esc section below).
When the user presses it, the hook swallows the key, the running command
exits 130, and the overlay disappears with it.

The indicator is a topmost layered window with a magenta colour key:
everything except the border and the pill is fully transparent. It is
**click-through** (`WS_EX_TRANSPARENT` plus `HTTRANSPARENT`, so even the
pill's opaque pixels pass clicks to the Window beneath), **never takes
focus** (shown with `SWP_NOACTIVATE|WS_EX_SHOWWINDOW`), never appears in
Alt-Tab or the taskbar, and is excluded from winuse's own window
listings. It is raised over the target Window for exactly the duration of
the Action — after the locked-desktop and administrator-Window refusals
(winuse is not “in control” when it refuses), around the activation and
the injected input — and hidden again whatever the outcome; observation
commands (`get-window-state`, `zoom`) never show it.

It never appears in **winuse's own screenshots**: every capture method
runs with the indicator hidden (hidden, captured, restored) — the
deterministic path, since it is unverified whether
`WDA_EXCLUDEFROMCAPTURE` also hides a layered window from GDI BitBlt and
pre-2004 Windows turns it into a black box. The affinity is still applied
best-effort so other software's captures (screen recorders) exclude the
indicator where the OS supports it. If a screenshot is taken while an
Action runs, the indicator blinks off for the capture's duration and
comes back — and if the indicator cannot start or a show fails, the
Action still runs (the failure is logged to the helper log).

### Recorder

```
winuse record-tree <window> <file.json>
```

Captures a real Window's tree and saves it as a **fixture file** — the
same JSON the renderer loads (`wstate.Tree`, format 1) — for the seam-2
offline tests (`internal/wstate/testdata/`, `internal/uia/testdata/`).
Hand-authored fixtures are written in the same format and marked as such
in their `source` field. Development only — it exists to maintain the
offline fixtures, not for agents to use.

## The Esc stop

Pressing the **physical Esc key** while winuse is acting stops the
running command immediately (ADR 0007): a low-level keyboard hook
(`WH_KEYBOARD_LL`) on the helper's UI thread — the overlay's thread —
sees the press, **swallows the key** (the controlled app never receives
it; Codex does the same), and the stopped command exits with **code 130**
(Codex's interrupt code):

```
winuse was stopped by the user with the physical Escape key
hint: stop your current task and ask the user how to proceed before sending more winuse commands; ...
```

so the agent stops and asks instead of retrying. What counts:

- The press is latched while winuse is in control — an Action, a whole
  Batch (including the gaps between steps and a step's tail), or a
  launch-app wait — and never leaks into the next command.
- **Injected keys are ignored** (the hook checks `LLKHF_INJECTED`):
  `winuse press-key <window> Escape` types a plain Escape into the app
  and stops nothing.
- The swallow is down/up paired, so no app ever sees half an Esc press,
  and while nothing runs the user's Esc passes through untouched.
- A stop mid-typing aborts between the injection batches (what already
  landed, landed — the hint says so); a stop mid-Batch leaves the
  remaining steps unrun; a stop during a launch-app wait reports the
  stop, not the timeout, and the App that already started keeps running.

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
- Requests time out after **10 s** (15 s for `launch-app`, 60 s for a
  Batch) and report `winuse request timed out: <command>`;
  Esc stops exit with code 130.
- Helper logs go to `%LOCALAPPDATA%\winuse\helper.log` (its stderr is NUL).
- The process is **per-monitor DPI aware v2** (manifest + runtime call) so
  every coordinate is physical pixels from the start.
- All **UI Automation work runs on one OS-locked, COM-initialised (MTA)
  thread** in the helper (see `internal/uia`), reached through a job queue
  — hand-written COM vtable calls, no cgo. Nothing else touches COM; there
  is **no UIA event monitoring at all** (a known cause of Codex's
  system-wide UIA freezes), and a wedged worker thread is abandoned and
  replaced, never killed.
- The helper also owns a **UI thread**: one more OS-locked thread, with a
  Win32 message loop, that carries the **in-control overlay** window and
  the **Esc keyboard hook** (`WH_KEYBOARD_LL`; injected keystrokes are
  ignored, and the physical key is swallowed only while winuse is in
  control). Input injection and UI Automation never run on it (ADR 0006,
  ADR 0007).

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
  offline, the Chromium/Electron sparse-tree detection (the
  lazy-accessibility retry and its notes) is tested against recorded
  Chromium/Electron trees (`internal/uia/testdata/`), and the in-control
  overlay's brackets are tested with fakes: the show/hide bracket around
  every Action (never shown for the pre-input refusals, always hidden
  after, whatever the outcome) and the capture bracket every capture
  method runs inside, plus the overlay's layout functions and the UI
  thread's job mechanics (in order, on one locked thread, bounded waits).
  The Windows.Graphics.Capture method's pure parts are offline-tested too
  (`internal/wgc`: the GUID literals against the documented IIDs, the
  by-value struct packing, the frame-size cross-check, the BGRA→RGBA
  readback, the environment knobs); its WinRT/Direct3D calls are
  exercised only by the user-run e2e suite.
  The ticket-11
  tests: **Element Reference resolution** (unique, ambiguous with its
  candidate lines, missing, exact-match semantics) against tree fixtures
  (`internal/wstate`), the reference resolution the helper performs
  against the live-Element store's Window State (`internal/uia`), every
  Element-taking Action's reference wiring and failure messages, and
  **Batch parsing and execution** (step splitting at `then`, per-step
  argument parsing, in-order execution stopping at the first failure that
  names the step, malformed steps refused before any input, the Window
  injected into every step) with every Win32 and element side faked
  (`internal/helper`, `internal/cli`). The ticket-12 tests: **the Esc
  stop** — the hook's decision table (physical vs injected keys, the
  down/up swallow pairing, the not-armed pass-through) and the latch's
  nested scopes (`internal/win`), the typing abort between batches, the
  launch-app wait stop, every command's exit-130 mapping with the
  user-stopped-winuse message, Batch stops at every checkpoint (a stopped
  step, between steps, the last step's tail), and the Esc stop outranking
  the request timeout (`internal/apps`, `internal/helper`), plus the
  pointer Actions as Batch steps. A guard proves no offline test ever
  installs the real hook. The ticket-15 tests (`internal/skill`) guard the
  shipped skill: the frontmatter stays valid for `npx skills add`, the
  SKILL.md keeps covering the full command surface (cross-checked against
  the CLI's usage text), never mentions the tool it must not depend on,
  and the installer's asset names stay in sync with the release workflow.
  No desktop access, no input injection, safe to run any time.
- **End-to-end (seam 1):** `test/e2e` — the real binary and the fixture app
  against the live desktop. **Opt-in only, started by the user, never by
  an agent** (see `test/e2e/README.md` for the rules):

  ```
  set WINUSE_E2E=1
  go test -tags e2e -v ./test/e2e
  ```

  These tests take over the screen — they click, type, activate windows
  and take screenshots while they run — so an agent must never enable the
  opt-in, run them, or start the fixture app, without the user's explicit
  go-ahead. `go test ./...` (above) is the only test command an agent may
  run.

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

## Layout

```
SKILL.md                the agent skill (installed by `npx skills add`; self-contained)
install.ps1             one-line installer: release binary onto the user PATH
.github/workflows/release.yml   builds the release binaries on a pushed tag
cmd/winuse/          binary: CLI dispatch + hidden __helper entry (manifest .syso here)
cmd/winusefixture/   the e2e fixture app's entry point (see The fixture app)
internal/protocol/   CLI↔helper contract: JSON-lines request/response, timeouts, exit codes
internal/pipe/       named pipe: naming per user/session, user-only DACL, dial, anti-squat check, singleton
internal/win/        Win32 layer: identity, DPI awareness, window enumeration, process/App names, window resolution, helper spawn, and the Actions' Win32 side: SendInput clicks, the pointer Actions (wheel scroll, interpolated drag, hover), keyboard (Unicode text and key chords), foreground activation, integrity/locked-desktop refusals; the UI thread and the in-control overlay window (ADR 0006)
internal/apps/       App catalog and launching: list-apps (UserAssist, Start menu, store Apps), launch-app (CreateProcess/ShellExecuteExW + Window polling)
internal/keys/       Codex/X keysym key-name and chord parsing for press-key (pure Go, fully covered offline)
internal/helper/     helper: serve loop, idle exit, stale-exit, command registry, window-state + record-tree + list-apps + launch-app commands, the Action pipeline and its commands (click, type-text, press-key, set-value, perform-secondary-action, activate-window, batch) and name:/id: reference resolution
internal/uia/        UI Automation: locked COM worker thread, bounded tree walk, Chromium/Electron lazy-tree retry, live-Element store with its Window State (the name:/id: reference space) (raw vtables, no cgo)
internal/shot/       screenshots: frames, capture-method chain, box-filter scaling, the screenshot↔Window coordinate mapping and its per-Window store (pure, offline-tested)
internal/wgc/        Windows.Graphics.Capture: hand-written WinRT/Direct3D interop (raw vtables, no cgo), the dedicated capture thread, the capture flow and its fallbacks
internal/wstate/     Window State model: Element/Tree, Codex-format rendering, name:/id: Element Reference resolution, recorded-tree fixtures (seam 2)
internal/cli/        CLI: subcommands, dial-or-spawn client state machine, output/exit-code policy
internal/skill/      guards the shipped SKILL.md / installer / release surface (offline tests)
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

## Publishing a release

Nothing publishes by itself; the workflow triggers only on a tag you
push.

1. Bump the version — `internal/version/version.go` (the stale-helper
   check) and `winres/winres.json` — then regenerate the committed
   `.syso` resources:
   `go run github.com/tc-hib/go-winres@latest make --arch amd64,arm64 --out cmd/winuse/rsrc`.
2. Commit, tag, push the tag:
   `git tag v0.1.0 && git push origin v0.1.0`.
3. GitHub Actions (`.github/workflows/release.yml`) cross-compiles
   `winuse_windows_amd64.exe` and `winuse_windows_arm64.exe` and creates
   the GitHub Release with generated notes. `install.ps1` always installs
   from `releases/latest`.

The binaries are unsigned (see
[Allowing the unsigned binary](#allowing-the-unsigned-binary)).
