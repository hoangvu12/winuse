# winuse

A Windows computer-use tool for coding agents: it lets any harness that can
run shell commands list Apps and Windows, read a Window State (accessibility
tree as text, screenshots on request) and perform Actions — at the quality of
Codex Computer Use, without being tied to Codex.

**Status: ticket 01 of the v1 plan** (`.scratch/winuse-v1/`) — background
helper and `winuse list-windows`. Later tickets add the UI Automation tree,
actions, screenshots, the overlay and the Esc stop. The product spec and the
domain language live in `.scratch/winuse-v1/spec.md` and `CONTEXT.md`.

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
  window filtering/formatting, client state machine, helper serving and
  idle exit. No desktop access, no input injection, safe to run any time.
- **End-to-end (seam 1):** `test/e2e` — the real binary against the live
  desktop. Opt-in only, **started by the user, never by an agent**:

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
- The helper exits by itself after **2 minutes idle** (override with
  `WINUSE_HELPER_IDLE`, a duration like `90s`); the next command restarts
  it transparently.
- Helper logs go to `%LOCALAPPDATA%\winuse\helper.log` (its stderr is NUL).
- The process is **per-monitor DPI aware v2** (manifest + runtime call) so
  every coordinate is physical pixels from the start.

`winuse list-windows` prints one line per visible Window:

```
<Window ID>\t<App>\t<title>
```

Hidden, DWM-cloaked and tool windows are excluded; owned dialogs ("Save
As") are listed as their own Window of the same App. UWP windows are
attributed to their real app process, and the App name is the executable's
FileDescription, falling back to the exe base name.

## Layout

```
cmd/winuse/          binary: CLI dispatch + hidden __helper entry (manifest .syso here)
internal/protocol/   CLI↔helper contract: JSON-lines request/response, timeouts, exit codes
internal/pipe/       named pipe: naming per user/session, user-only DACL, dial, anti-squat check, singleton
internal/win/        Win32 layer: identity, DPI awareness, window enumeration, process/App names, helper spawn
internal/helper/     helper: serve loop, idle exit, stale-exit, command registry
internal/cli/        CLI: subcommands, dial-or-spawn client state machine, output/exit-code policy
internal/version/    binary version (stale-helper detection)
test/e2e/            opt-in end-to-end tests (user-run only)
winres/              manifest + version-info source for the .syso files
```

## Dependencies

| Module | License | Use |
|---|---|---|
| golang.org/x/sys | BSD-3-Clause | Win32 API calls |
| github.com/Microsoft/go-winio | MIT | named-pipe transport |
| tc-hib/go-winres (tool, not imported) | 0BSD | manifest/version .syso generation |
