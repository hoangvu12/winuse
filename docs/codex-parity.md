# Codex parity checklist

winuse's promise is Codex Computer Use's Windows quality from any
Harness. This checklist walks **every tool and parameter of Codex's
Windows surface** (recovered in `.notes/codex-surface.md` §1, sourced
from OpenAI's `api.md` and the reverse-engineered helper) and marks each
one **matched** by winuse, **intentionally different** (the spec or the
orchestrator decided otherwise — every known Codex Windows bug is fixed
on purpose), or an **extension** (something Codex cannot do).

The winuse side is what `winuse --help` ships; the guard test
(`internal/skill/parity_test.go`) fails when a Codex tool, a parameter
or a cross-cutting behaviour drops off this list, when a row loses its
status, or when the checklist names a command the CLI does not actually
have.

Statuses: **matched** · **intentionally different** · **extension**.

## Tools: Codex window2 → winuse CLI

### list_windows

| Parameter | Codex | winuse `list-windows` | Status |
|---|---|---|---|
| `—` (no parameters) | returns `Window[] {app, id, title?}` of targetable windows | prints `<Window ID>	<App>	<title>` per Window | matched |

Behaviour notes (see cross-cutting rows for the differences that live
here): the window filter is Codex's (visible, not cloaked, not a tool
window) **plus** owned dialogs — the fix for Codex's untargetable-dialog
bug. UWP Windows are attributed to the real app process; the App name is
the executable's FileDescription, falling back to the exe base name.

### get_window

| Parameter | Codex | winuse | Status |
|---|---|---|---|
| `id` | rehydrate a currently open window by id after losing a binding | every command's `<window>` argument resolves a Window ID (or an App name with exactly one Window); `get-window-state` re-reads it | intentionally different |
| `app` | disambiguate the window by app | the App-name form of `<window>`; several Windows of one App fail listing the candidates | intentionally different |

Codex needs a rehydrate call because its window bindings live in the
JS REPL; winuse's Window ID is stable across CLI calls, so the step
folds into every command. Ambiguity is an error in both, never a guess.

### list_apps

| Parameter | Codex | winuse `list-apps` | Status |
|---|---|---|---|
| `—` (no parameters) | `ListAppsApp[] {id, displayName?, isRunning?, lastUsedDate?, useCount?, windows}` | running Apps with their Windows, then recently used Apps (`not running`, UserAssist tracking, most recent first) | matched |

Codex caps the catalog at 40 entries (issue #47299); winuse caps the
recent tail at 30 and never times out building the catalog the same way
(Codex's `list_apps` is the usual timeout victim).

### launch_app

| Parameter | Codex | winuse `launch-app` | Status |
|---|---|---|---|
| `app` | an app id from `list_apps`, or an explicit `.exe` path/identifier | a name (matched case-insensitively against the catalog, Start menu, packaged Apps — several distinct matches are an error listing them), an explicit path, a Start-menu shortcut path, or an AUMID | matched |

Codex's identifier prefixes (`process:`, `path:`, `registry:`,
`app-user-model-id:`, `window-app:`) are not a winuse language: names
resolve through the catalog instead (see cross-cutting "app
identifiers"). The 15 s launch budget and Codex's failure message
`launched app did not expose a targetable window: <app>` are the same.

### get_window_state

| Parameter | Codex | winuse `get-window-state` | Status |
|---|---|---|---|
| `window` | the Window to observe | `<window>` (Window ID or unambiguous App name) | matched |
| `include_screenshot` | optional, **default true**; several `Screenshot` objects with `zIndex`, data URLs | `--screenshot`, **default off** (spec: the tree is the cheap read; image tokens are spent on request); exactly one frame, saved to a file whose path is printed, with size and scale | intentionally different |
| `include_text` | optional, **default false**; adds `selected_text`, `selected_elements`, `document_text` | the tree is always included; the selected/document-text blocks are not implemented (the tree's `Value:` and state columns carry per-Element content) | intentionally different |

Codex's default is screenshot-first (macOS inheritance); the spec flips
it deliberately — a text-first read is cheaper and enough for ordinary
tasks, and a note tells the agent when a Window's tree is too sparse to
trust (canvas apps). Popups and menus drawn over the Window appear in
the one frame (Codex composites several `zIndex` layers).

### click

| Parameter | Codex | winuse `click` | Status |
|---|---|---|---|
| `window` | the Window to click in | `<window>` | matched |
| `element_index` | the Element Index from the last state | the Element Index, or `name:`/`id:` references resolved against the latest Window State | matched (extended) |
| `x`, `y` | window-relative **logical** pixels (the 175%-scaling offset bug) | `@x,y` in the latest screenshot's pixel space, mapped back to **physical** pixels, re-anchored to the Window's live bounds at action time | intentionally different |
| `click_count` | 1 (single) or 2 (double); right double click is not supported | `--count 1|2|3`; non-left multi-clicks are refused with Codex's message | matched |
| `mouse_button` | `left`/`right`/`middle`, aliases `l`/`r`/`m`; unknown → `unsupported mouse_button: <v>` | `--button left|middle|right` with the same `l`/`r`/`m` aliases and the same error | matched |
| `screenshotId` | coordinates are validated against the screenshot they came from | no ids: coordinates bind to the Window's **latest** screenshot; a new screenshot or a helper restart fails with the instruction to take a fresh one instead of guessing | intentionally different |

The element path scrolls the Element into view and uses its clickable
point (or bounds centre) before injecting one SendInput batch, so
double and triple clicks register.

### press_key

| Parameter | Codex | winuse `press-key` | Status |
|---|---|---|---|
| `window` | the Window receiving the key | `<window>` (activated first, like every Action) | matched |
| `key` | X keysym names and `+` chords (`Return`, `Control_L+a`, `Numpad_0`), aliases, whitespace around `+` ignored; unknown → `unsupported key: <name>` | the same names, aliases and chord delivery (all key-downs in order, key-ups in reverse, scan codes filled, extended keys flagged); the same error | matched |

The Windows key differs in enforcement only: Codex's guidance forbids
it and winuse's code refuses it (see cross-cutting).

### type_text

| Parameter | Codex | winuse `type-text` | Status |
|---|---|---|---|
| `window` | the Window with the focused Element | `<window>` | matched |
| `text` | one string, typed into the current focus as Unicode | every argument after the Window joins into the text (CLI sugar); injected as Unicode key events, one UTF-16 unit per event, newline/tab as real Return/Tab keys | matched |

### scroll

| Parameter | Codex | winuse `scroll` | Status |
|---|---|---|---|
| `window` | the Window scrolled | `<window>` | matched |
| `x`, `y` | window-relative point to scroll at | `@x,y` in the latest screenshot's space, mapped like coordinate clicks | intentionally different |
| `scrollX`, `scrollY` | wheel amounts; positive down/right, one notch is 120; point-based, not element-based (Codex guidance: "do not pass element_index") | the same signs, the same notch size, the same point-based contract; a point over another window is refused, not scrolled | matched |
| `screenshotId` | validated against that screenshot | latest-screenshot binding (as click) | intentionally different |

### set_value

| Parameter | Codex | winuse `set-value` | Status |
|---|---|---|---|
| `window` | the Window holding the Element | `<window>` | matched |
| `element_index` | the Element to write | the Element Index or a `name:`/`id:` reference | matched (extended) |
| `value` | the string to set | the value; numeric Elements (RangeValue) take it parsed as a number | matched |

Non-settable Elements fail with Codex's `Cannot set a value for an
element that is not settable` plus the click-and-type-text hint. The
Element is focused first — Codex's `set_value` order.

### drag

| Parameter | Codex | winuse `drag` | Status |
|---|---|---|---|
| `window` | the Window dragged in | `<window>` | matched |
| `from_x`, `from_y` | window-relative start point | `@x,y` in the latest screenshot's space | intentionally different |
| `to_x`, `to_y` | window-relative end point | `@x,y` in the same space, pinned exactly at button-up | intentionally different |
| `screenshotId` | validated against that screenshot | latest-screenshot binding (as click) | intentionally different |

Left button only, as in Codex. winuse interpolates ~16 absolute moves
~10 ms apart so apps register a drag (menus track, canvases draw, lists
reorder) instead of a teleport — a fidelity extension on the same
contract.

### perform_secondary_action

| Parameter | Codex | winuse `perform-secondary-action` | Status |
|---|---|---|---|
| `window` | the Window holding the Element | `<window>` | matched |
| `element_index` | the Element to act on | the Element Index or a `name:`/`id:` reference | matched (extended) |
| `action` | case-insensitive; only `Raise`, `Scroll Up/Down/Left/Right`, `Expand`, `Collapse`; unknown → `unsupported secondary action: {action}; expected …` | the same set and the same error listing the expected names, **plus** the `Toggle` and `Select` extensions | matched (extended) |

An action the Element does not offer fails listing the ones it does,
probed live — so a changed Element says so.

### activate_window

| Parameter | Codex | winuse `activate-window` | Status |
|---|---|---|---|
| `window` | the Window to foreground; "an optional escape hatch" because input methods activate automatically | the same: every Action activates and verifies its target first; the command is the escape hatch | matched |

Activation uses the foreground-lock workarounds and refuses to
un-maximize (Codex's bug); a Window blocked by its own modal fails
naming the dialog.

## Cross-cutting behaviour

### process model

Codex ships a per-turn `codex-computer-use.exe` driven over JSON-RPC by
`@oai/sky` inside a JS REPL; winuse is a CLI plus a persistent
background helper over a named pipe restricted to the user's SID,
version-checked (a stale helper exits so upgrades never talk to an old
one), idle-exiting after 5 minutes. The observable contract — one
request at a time, `ok` or a one-line failure, no state echo — is
preserved. **intentionally different**.

### timeouts

10 s per request, 15 s for launching an App (a Batch gets 60 s, an
extension), message style `winuse request timed out: <command>` next to
Codex's `computer-use request timed out: <method>`. **matched**.

### esc stop

The physical Escape key stops the running Action or Batch: a
low-level keyboard hook ignores injected keys, swallows the physical
press (the app never sees it), and the command exits **130** with the
message that the user stopped winuse and a stop-and-ask hint. The latch
never leaks into the next command (winuse has no turn concept).
**matched**.

### overlay

A border framing the controlled Window and a pill banner
("winuse is in control — press Esc to stop"), click-through, never
focusable, hidden the moment the Action ends, and never in winuse's own
screenshots. **matched** (Codex's overlay lingering to turn end is a
bug winuse does not copy).

### error style

Lowercase, specific, one line, with a recovery hint: stale elements
("element N is not available in cached app state for <app>" plus the
read-again instruction), ambiguity with candidates, timeouts, window
gone, app not found. **matched**.

### admin windows

Codex reports limited accessibility but **silently drops** the input
and reports success (issues #45388, #52989); winuse refuses before
sending anything, with the explanation that controlling an elevated
Window needs an elevated winuse. **intentionally different** (fix).

### locked desktop

Detected and refused with the same message shape ("Windows desktop is
locked (input desktop is LockApp); unlock before using winuse"); the
agent is told to stop and ask. **matched**.

### owned dialogs

Codex drops owned top-level dialogs from `list_windows`, which is the
root of its untargetable-dialog bug; winuse lists them as their own
Window of the same App and an owner resolves to its enabled modal.
**intentionally different** (fix).

### dpi and coordinates

Codex validates in logical pixels but places input in physical pixels
(the 175% offset bug) and misses on multi-monitor setups; winuse is
per-monitor-DPI-aware v2, one coordinate space end to end, virtual
desktop absolute moves with cursor verification. **intentionally different** (fix).

### ui automation reads

Codex's background event monitoring and bulk reads freeze apps
system-wide (issues #36833, #43692); winuse does no event monitoring
ever, scopes cache requests to the target Window one node chunk at a
time, and caps the walk (3000 elements, depth 48, 1000 children, a time
budget) returning the partial tree with a note. **intentionally different** (fix).

### screenshots

Codex: Windows.Graphics.Capture only, "fails loudly", DPI-normalized
logical pixels, JPEG q80 data URLs, no size cap. winuse: WGC first
(same capture stack, hand-written interop), then the screen region
widened with overlapping popups, then PrintWindow — a method chain
that never depends on one method; JPEG q80, the long edge capped at
1568 px, the scale recorded so `@x,y` maps back exactly; the path is
printed instead of a data URL (a CLI carries text, not images).
`zoom` adds a full-resolution PNG crop. **intentionally different**.

### tree format

Codex's grammar, verified byte-for-byte against its helper: the header
(`Window: "<title>", App: <exe>.`), depth-first pre-order Element lines
numbered from 0, one TAB per depth, the states/Description/Value/
toggle/Secondary Actions/ID columns, the focused-Element sentence. Two
deliberate divergences: **English role names** derived from the numeric
ControlType (Codex prints locale-dependent LocalizedControlType — a
bug in every non-English locale), and **bounded, explicit truncation**
with Codex's `(truncated: <limit>, omitted <n> children)` suffix where
Codex's budgets are unknown. **matched (grammar) / intentionally
different (roles, bounds)**.

### secondary actions

Codex offers Raise, Scroll Up/Down/Left/Right, Expand, Collapse —
winuse offers exactly those plus the Toggle and Select extensions,
rendered in the same style. **matched (extended)**.

### key names

Codex's keysym names, the alias blob (`Ctrl`, `Alt`, `period`,
`greater`, `Numpad_Add`, …), chords delivered as all-downs-then-ups,
scan codes filled (Codex's zero-scancode Flutter bug), shifted names
mapping to the unshifted key. The Windows key is recognized only to be
**refused in code** — Codex relies on model guidance. **matched**.

### app identifiers

Codex identifies Apps with prefix strings (`process:`,
`app-user-model-id:`, …); winuse resolves names through the catalog
(Start menu, UserAssist, packaged Apps) and accepts explicit paths or
AUMIDs — `launch-app` never guesses between several distinct matches.
**intentionally different**.

### one request at a time

Codex's helper rejects concurrent requests ("already has an active
request"); winuse's helper serializes every command through one
UIA-locked thread. **matched**.

### stale elements

An Element Index whose Element went away fails as stale, with the
instruction to read the Window State again; references copied from an
older state go stale the same way. `name:`/`id:` references survive
re-reads better — an extension. **matched (extended)**.

### extensions beyond Codex

Codex's surface has no hover, no zoom, no Batch, no `name:`/`id:`
references, no `record-tree`; winuse has all of them, keeping Codex's
parameter names where a parameter exists. **extension**.
