# 25: Restart a helper from another build of the same version

**What to build:** make the CLI tell a helper apart by its build, not only by its version, so a rebuilt `winuse` never acts through a helper left running by an older build.

Today the CLI sends `version.Version` ("0.1.0") as `Request.Ver` with every request. The helper answers `Stale` and exits only when that string differs (`internal/helper/server.go`, `internal/version/version.go`). The constant changes only per release, so two builds of the same version look identical. dp-18's live run (2026-11-03, laptop) hit this: after a rebuild, the old helper kept serving the new e2e binary until its process was killed by hand. Its tests ran against stale code.

- A build id next to the version: for example a hash of the running executable, or its size and mod time, read once at start (`os.Executable`). Go only, no new dependencies, and no ldflags step, so a plain `go build` and `go test` binary still work.
- The CLI sends it with every request (a new optional field, appended to `protocol.Request`). The helper compares it with its own, and a mismatch is `Stale` exactly as a version mismatch is today: the helper answers, exits, and the CLI restarts one and retries (`TestCallRestartsStaleHelper`'s path).
- An empty build id on either side (an old CLI or helper) compares as a match, so mixed builds across this change behave as before.
- `status` keeps answering across builds and names both builds when they differ (ticket 16).
- The e2e binary and the CLI it runs share a build only when they are the same file. Check the e2e harness still starts one helper per test run and not one per command.
- Precedent: the browser extension already carries a stamped build (`internal/bridge` `StampedBuild`).

**Status:** ready-for-agent

- [ ] Build id read at start, CLI and helper
- [ ] Request field and the helper's Stale check
- [ ] Offline tests: same version with another build restarts the helper; empty build ids match; status names both
- [ ] CONTEXT.md / SKILL.md only if the user-facing behavior needs a line

## Comments

### 2026-11-03: filed (laptop orchestrator, at the PC orchestrator's request)

Found in dp-18's live run: a helper of the same version (0.1.0) from a previous build kept serving a rebuilt binary. The PC orchestrator asked for this ticket and will queue it on the PC.
