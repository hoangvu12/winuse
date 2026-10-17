# winuse

A Windows computer-use tool for coding agents: it lets any harness that can
run shell commands list Apps and Windows, read a Window State (accessibility
tree as text, screenshots on request) and perform Actions — at the quality of
Codex Computer Use, without being tied to Codex.

**Status: ticket 03 of the v1 plan** (`.scratch/winuse-v1/`) — background
helper, `winuse list-windows`, the fixture app with its opt-in e2e runner,
and `winuse get-window-state`: the Window State as Codex's Element tree,
read through UI Automation on a single locked COM thread, plus the recorder
for seam-2 fixtures. Later tickets add actions, screenshots, the overlay
and the Esc stop. The product spec and the domain language live in
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
  serving and idle exit, the fixture app's Element contract, and the
  seam-2 Window State tests: recorded/hand-authored tree fixtures are
  rendered and compared with expected text, and the bounded tree walk
  (caps, time limit, truncation notes) is tested against a fake element
  source. No desktop access, no input injection, safe to run any time.
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
- Requests time out after **10 s** (15 s for launch-app once that command
  exists) and report `winuse request timed out: <command>`;
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

## Reading a Window State

```
winuse get-window-state <window>
```

`<window>` is a **Window ID** from `list-windows`, or an **App name** —
which resolves only when that App has exactly one Window; otherwise the
error lists the candidates (never a guess). A title is never a match.

The output is Codex Computer Use's tree format: a header line, then one
line per Element, depth-first, numbered from 0 at the root window, one TAB
per depth level:

```
Window: "Example", App: app.exe.
	0 window Example
		1 edit Search ID: searchBox
		2 button OK ID: okButton

The focused UI element is 1 edit Search ID: searchBox.
```

Element lines follow the grammar
`<index> <role>[ (<states>)][ <name>][ Description: …][ Value: …][ on|off|indeterminate][ Secondary Actions: …][ ID: <AutomationId>]`
(codex-surface.md §2; ADR 0004 records the deliberate divergences —
notably English role names from the numeric ControlType instead of the
locale-dependent LocalizedControlType). Later tickets fill in the rich
state and Secondary Actions columns; the format supports them from the
start.

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

### Recorder

```
winuse record-tree <window> <file.json>
```

Captures a real Window's tree and saves it as a **fixture file** — the
same JSON the renderer loads (`wstate.Tree`, format 1) — for the seam-2
offline tests (`internal/wstate/testdata/`). Hand-authored fixtures are
written in the same format and marked as such in their `source` field.

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
internal/win/        Win32 layer: identity, DPI awareness, window enumeration, process/App names, window resolution, helper spawn
internal/helper/     helper: serve loop, idle exit, stale-exit, command registry, window-state + record-tree commands
internal/uia/        UI Automation: locked COM worker thread, bounded tree walk, live-Element store (raw vtables, no cgo)
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
