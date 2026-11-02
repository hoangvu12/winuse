# 16: `status`

**What to build:** `winuse status` prints, one line each:

- winuse and helper versions, whether the helper is running, its uptime;
- input desktop state (locked or not, `internal/win/desktop.go`);
- the helper's integrity level (`internal/win/integrity.go`);
- DPI awareness;
- Browser Connections (a one-line summary from `browser status`);
- the Windows build.

agent-desktop has `status` and `permissions` (research section 1, System rows). Starts the helper if it is not running? No: `status` reports "not running" and starts nothing.

**Blocked by:** none

**Status:** done

- [x] Command in CLI; helper part when running
- [x] Offline tests of the output with fakes
- [x] SKILL.md troubleshooting line

## Comments

### 2026-11-02: landed

- `internal/status`: the `Report` both sides share (the helper answers the `status` wire command with it as JSON in `Out`) and `Format`, seven lines: `winuse:`, `helper:` (running with version, pid and uptime; "not running; the next command starts it"; a stale older build; or "running but not answering: <why>"), `desktop:` (unlocked, or locked with the input desktop's name), `integrity:` and `dpi awareness:` (each says whether it is the helper's or this CLI's), `browser connections:`, `windows:`. A helper of another build is named "a different build; the next command restarts it".
- `internal/win`: `IntegrityName` (RID to "medium", "high (administrator)", ...), `DPIAwareness` (GetDpiAwarenessContextForProcess, or the thread's before 1803, compared with AreDpiAwarenessContextsEqual), `WindowsBuild` and the pure `FormatWindowsBuild` (RtlGetVersion plus UBR and DisplayVersion from the CurrentVersion key, e.g. "Windows 11 25H2, build 26200.6899"). All read-only.
- `internal/browser`: `Manager.Held()` lists the open Browser Connections ("chrome process 1234, your running browser through the winuse extension") without probing, discovering or dialing. The ticket said "a one-line summary from browser status", but `browser status` connects (and can raise Chrome's approval prompt), so status reads only the connections the helper already holds and says "none open; winuse browser status connects one" otherwise.
- `internal/helper/status.go`: the `status` command over new Services probes (`Started`, `Now`, `OwnIntegrity`, `DPIAwareness`, `WindowsBuild`; `Locked` is shared), the Browser service's `Held` by optional interface (a service without it reports "unknown"). It is observation-only, so it never waits on the action gate. `server.handle` answers `status` for a CLI of any build without the stale exit, so checking the status never stops a running helper (an older helper from before this change still answers stale and exits; the CLI names that).
- CLI (`internal/cli/status.go`): `Client.CallExisting` dials once and never spawns or retries. With no helper, status says "not running" and fills the machine lines from its own probes. It skips the input-desktop pre-check other commands do, so it reports a locked desktop instead of refusing. It exits 0 whenever it printed, and 2 for arguments. The usage text lists it.
- Tests: `internal/helper/status_test.go` (the report over fakes, no Browser service call, locked desktop, unknown probes, argument refusal, runs while the action gate is held, the server answers status from another build and keeps serving), `internal/cli/status_test.go` (not running with Spawn failing the test, locked, running report and request line, other build, stale fallback, dropped/dial error/refused/bad JSON, usage exit 2, `CallExisting` never spawns), `internal/status/status_test.go`, `internal/win` (`IntegrityName`, `FormatWindowsBuild`), `internal/browser` (`Held` before and after a launch, starting nothing). No e2e test (optional; `status` was not run live).
- Docs: SKILL.md gets a Refusals-and-recovery line (run `winuse status` when failures make no sense; it only reads) and a command-reference row. README's command table gets a row. docs/codex-parity.md's process-model row now says only the read-only `status` crosses builds (still **intentionally different**), and the extensions row mentions `status` (still **extension**). CONTEXT.md is unchanged: status adds no domain term.
