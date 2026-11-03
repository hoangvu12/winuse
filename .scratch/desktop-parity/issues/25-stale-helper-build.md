# 25: Restart a helper from another build of the same version

**What to build:** make the CLI tell a helper apart by its build, not only by its version, so a rebuilt `winuse` never acts through a helper left running by an older build.

Today the CLI sends `version.Version` ("0.1.0") as `Request.Ver` with every request. The helper answers `Stale` and exits only when that string differs (`internal/helper/server.go`, `internal/version/version.go`). The constant changes only per release, so two builds of the same version look identical. dp-18's live run (2026-11-03, laptop) hit this: after a rebuild, the old helper kept serving the new e2e binary until its process was killed by hand. Its tests ran against stale code.

- A build id next to the version: for example a hash of the running executable, or its size and mod time, read once at start (`os.Executable`). Go only, no new dependencies, and no ldflags step, so a plain `go build` and `go test` binary still work.
- The CLI sends it with every request (a new optional field, appended to `protocol.Request`). The helper compares it with its own, and a mismatch is `Stale` exactly as a version mismatch is today: the helper answers, exits, and the CLI restarts one and retries (`TestCallRestartsStaleHelper`'s path).
- An empty build id on either side (an old CLI or helper) compares as a match, so mixed builds across this change behave as before.
- `status` keeps answering across builds and names both builds when they differ (ticket 16).
- The e2e binary and the CLI it runs share a build only when they are the same file. Check the e2e harness still starts one helper per test run and not one per command.
- Precedent: the browser extension already carries a stamped build (`internal/bridge` `StampedBuild`).

**Status:** done

- [x] Build id read at start, CLI and helper
- [x] Request field and the helper's Stale check
- [x] Offline tests: same version with another build restarts the helper; empty build ids match; status names both
- [x] CONTEXT.md / SKILL.md only if the user-facing behavior needs a line

## Comments

### 2026-11-03: filed (laptop orchestrator, at the PC orchestrator's request)

Found in dp-18's live run: a helper of the same version (0.1.0) from a previous build kept serving a rebuilt binary. The PC orchestrator asked for this ticket and will queue it on the PC.

### 2026-11-03: done (laptop, branch laptop/dp-25-stale-helper-build)

**Build id** (laptop orchestrator's call): `version.Build()` (`internal/version/build.go`), read once per process from `os.Executable`. It is the last part of the Go linker's build ID, found in the exe's first 32 KB as `cmd/internal/buildid` finds it (`\xff Go build ID: "..."`). That part is the hash of the linked content, so two builds of one version differ and a rebuild of identical code does not. A binary without the stamp gives `<size>-<mod time>`; an unreadable exe gives "", which matches any build. No ldflags, no new dependency; `go build` and `go test` binaries both carry the stamp (tested against the test binary itself and `go tool buildid`).

**Wire and check:** `protocol.Request.Build` (appended, omitempty). `Server.otherBuild`: another `Ver`, or the same `Ver` with both builds set and different, answers `Stale` exactly as before. The message names both builds (`helper 0.1.0 build X, CLI 0.1.0 build Y`) and keeps "different winuse build", so codes.go's INTERNAL rule still matches. `status` is still exempt; the helper's report carries `helper.build`, and the CLI prints both builds on the winuse and helper lines only when they differ.

**Design (PC orchestrator's call): one shared pipe, not one helper per build.** The pipe and mutex stay per user, session and integrity level. One helper per build would mean two helpers, each with its own desktop lane, Esc hook and Agent Cursors, and their real input could interleave, which breaks ADR 0014. The accepted cost: two different winuse binaries used at once (two worktrees, or an e2e binary beside the installed one) restart each other's helper on each switch. Two guards make that safe:

1. A helper that finds itself stale exits only after its requests in flight finish. `Shutdown` closes the listener and `Serve` drains, so no request is cut mid-Action, and each Headed Action's give-back (the ma-08 ledger release) runs. `Main` also calls `win.ReleaseHeld` after the drain, as a backstop on any exit. Test: `TestStaleExitFinishesTheRequestsInFlight`.
2. The first answer after a build restart ends with `Note: the helper was restarted for another winuse build, so Window States and Element Indexes from before are gone; read the Window again, or use name: or id:, which re-resolve live.` (`cli.Client.Call`, `restartNote`). Only a stale restart adds it; a first start, a dropped-connection retry or a second stale answer does not. A chat whose helper was taken by another build gets the Note on its own next command, which restarts the helper back to its build. If that helper had already idled out meanwhile, the chat sees an ordinary start, and the existing no-Window-State refusals apply.

**e2e harness:** `winuseBinary` builds `cmd/winuse` once per run, and the helper it spawns is the same file, so one helper serves the whole run. The multi-command e2e tests (for example the two Agents' Window States in `agents_test.go`) would fail if it restarted per command. The run's first command still makes a helper of another build exit, after its in-flight requests, as designed.

**Tests (offline):** `internal/version/build_test.go`; `internal/helper/stalebuild_test.go` (same version and another build exits, empty builds match, another version stays stale, status across builds, the status report's build, and guard 1); `internal/cli/client_test.go` (guard 2 on the restart, no Note otherwise, the request carries the build); `internal/cli/status_test.go` and `internal/status/status_test.go` (both builds named only when they differ).

**Docs:** SKILL.md (one paragraph on the Note), README's `status` row, codex-parity's process model, and the spec row. CONTEXT.md is unchanged: no new term.

### 2026-11-03: follow-up, the CLI waits for a draining helper (laptop, branch laptop/dp-25b-spawn-waits)

The open edge (PC orchestrator's call): a CLI of another build met a stale helper that was still finishing another request in flight (a Batch can run up to 300 s). The CLI failed in two ways. While the helper drained, its pipe stayed busy: no instance listens, so the dial timed out and the CLI returned a bare `context deadline exceeded`. Once the pipe was gone, the helper still held the single-instance mutex for a moment, so the helper the CLI spawned lost the mutex and exited, and the CLI failed after `spawnDialWait` (5 s) with `helper did not start`.

**Fix (`cli.Client.dialOrStart`, `waitExit`):** a helper on its way out is waited for, up to the request's own budget (`protocol.Budget`), timed from the first dial. Two signs mark one: a busy pipe (the dial times out on its own `dialTimeout` while the caller's ctx is live; a live helper listens again within milliseconds of each accept), or no pipe while the mutex is held (`pipe.SingletonHeld`, which only opens the mutex with `OpenMutex` and never takes it; the new `Client.Running` hook). The wait ends when the pipe is gone and the mutex is free (spawn as before), when a pipe answers (another CLI started a helper: use it, spawn nothing), or at the budget. This applies to every `dialOrStart`, so it covers the CLI that got the stale answer, another chat's command meanwhile, and the dropped-connection retry during an idle exit.

**How it says so:** like the restart Note and the `ok` + `Note:` outputs (origin/main 0693039), on the answer: `Note: waited for the old helper to finish its requests in flight and exit first`, before the restart Note. It is added only when the pipe was seen busy and then went away (review fix: a live helper under load whose pipe was busy for a dial and then answered exited nothing, and gets no Note), so the brief mutex tail of a helper with nothing in flight, a plain first start, or the dropped-connection retry adds no Note. At the budget the CLI fails without spawning: `the old winuse helper was still finishing its requests in flight when this request's budget expired; nothing was sent` (or `... did not exit before this request's budget expired; nothing was sent` when only the mutex was held), hint `retry once it has exited; winuse then starts a helper of its own build`. Under `--json` both are TIMEOUT, not_delivered, retry safe (the existing `budget (ended|expired)` and `nothing was sent` rules; no new rule needed).

**Tests (offline):** `internal/cli/client_test.go`: the stale helper drains, then holds the mutex, and the CLI spawns once it is free (drain Note then restart Note); it gives up at the budget with the message, TIMEOUT and not_delivered, and spawned nothing; a mutex never released gives up; a brief mutex tail waits without a Note; a helper that came up meanwhile is used; a live helper that answers after a busy dial gets no Note; `dialOrStart` waits up to the budget it is given. `internal/pipe/pipe_test.go`: `SingletonHeld` before, during and after `AcquireSingleton`. The existing restart, first start, dropped-connection and stale-twice tests pass unchanged.

**Not covered:** no live run (the orchestrator's rule). A wait that ends in a busy dial can overshoot the budget by up to one `dialTimeout` (1 s); the request's own exchange slack then covers it. SKILL.md is unchanged: both lines explain themselves.
