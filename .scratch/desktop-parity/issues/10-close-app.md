# 10: close-app

**What to build:** `close-app <app>` posts WM_CLOSE to every top-level Window of the App and waits for the process to exit (up to a few seconds). agent-desktop has `close-app` with `--force` and a protected-process list (`ad:crates/windows/src/system/app_ops.rs:7-17`).

- Headless: posted WM_CLOSE, as `close-window` does.
- An App still running after the wait: `ok (the close was requested; <App> is still running, it may be asking to save)`, like `close-window`.
- Packaged apps: the same, per Window.
- Refuse administrator Apps and a locked desktop with "nothing was sent".
- `--force` (user decision, 2026-11-03): TerminateProcess the App's own processes after the posted close, as agent-desktop does. Refuse agent-desktop's protected list (csrss, wininit, winlogon, services, lsass, smss, lsaiso, dwm, explorer). Reword SKILL.md's "winuse never ends a process" (`skills/winuse/SKILL.md:326-327`): only `close-app --force` does.

**Blocked by:** none

**Status:** done

- [x] App-to-Windows resolution, posted closes, exit wait
- [x] CLI, protocol, helper
- [x] `--force` with the protected list
- [x] Offline tests: all closed, still running, admin refusal, `--force`, protected refusal
- [x] Opt-in e2e: two fixture Windows closed by one command
- [x] SKILL.md

## Comments

### 2026-11-03: triage decision (user)

Do it like agent-desktop, `--force` and the protected-process list included.

### 2026-11-03: built (laptop, branch laptop/dp-10-close-app)

What landed: `winuse close-app <app> [--force]`. The App is named as a
Window reference names one (display name or executable name, every
matching Window, all its instances). Refusals before anything is sent,
each with "nothing was sent": an App with no Windows, a locked desktop,
an administrator Window, and agent-desktop's protected list (csrss,
wininit, winlogon, services, lsass, smss, lsaiso, dwm, explorer). Then
each distinct process is opened (a handle, so a reused pid never answers
for it), WM_CLOSE is posted to every Window (`win.PostClose`, as
close-window), and winuse waits up to 5 s for every process to exit and
every Window to go. Exited: `ok`. Still running:
`ok (the close was requested; <App> is still running, it may be asking to
save: read winuse list-windows --app <app> or the Window State next)`.
With `--force`, the processes still running then get TerminateProcess,
and the output is `ok (<App> was still running after the close; --force
ended N process(es))`. An Esc during the wait stops it before anything
is ended. close-app is serialized on the action gate, Headless, has no
headed form and is no Batch step.

Choices the ticket did not spell out (flagged in the report):

- The protected list is refused with or without `--force` (agent-desktop's
  close-app refuses them, ad research row 2); the hint points at
  close-window for a single File Explorer Window.
- `--force` also refuses a Window whose executable winuse cannot read (it
  cannot check the list) and ApplicationFrameHost.exe (it hosts every
  packaged App's frame, so it is no one App's own process).

Files: `internal/helper/closeapp.go` (new), `internal/win/appprocess.go`
(new: OpenAppProcess, Exited, Terminate), `internal/cli/closeapp.go`
(new), `internal/cli/cli.go` (dispatch, usage), `internal/protocol/protocol.go`
(CloseAppArgs), `internal/helper/state.go` (Services.OpenProcess,
registration), `internal/helper/windowactions.go` (comment),
`internal/fixture/app.go` and `cmd/winusefixture/main.go` (`-ignore-close`),
`skills/winuse/SKILL.md` ("winuse never ends a process" reworded: only
`close-app --force` does), `README.md`, `docs/codex-parity.md`,
`CONTEXT.md` (App: closing is not an Action), `test/e2e/README.md`.

Tests: `internal/helper/closeapp_test.go` (all closed, still running,
`--force`, `--force` with a clean exit, terminate failure, Esc stop,
a process already gone, close failure, refusals: no Windows, locked,
admin, admin under `--force`, protected with and without `--force`,
unreadable image and ApplicationFrameHost under `--force`, no App; and
registration), `internal/cli/closeapp_test.go` (parsing, usage errors,
usage text), `internal/helper/registry_test.go`. Opt-in e2e
`test/e2e/close_app_test.go`: `TestCloseAppClosesTwoWindowsWithOneCommand`
and `TestCloseAppForceEndsAnAppThatStaysOpen`, on a uniquely named copy of
the fixture so close-app can only reach processes the test started. The
protected refusal is offline only (live, it would aim at the user's
explorer.exe).

Ran: `go build ./...`, `go vet ./...`, `go test ./...`,
`go vet -tags e2e ./test/e2e`, all passing. Live (WINUSE_E2E=1, under the
laptop's live lock): both close-app e2e tests passed (11.8 s and 11.5 s),
and again after merging origin/main (desktop-parity 08) into the branch.
