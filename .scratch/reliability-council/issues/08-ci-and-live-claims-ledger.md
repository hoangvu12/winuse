# 08: No CI runs tests; live claims are tracked in free text

Status: done (part 1 landed 2026-11-07; parts 2-3 landed 2026-11-07 after the lane's trial runs on GitHub; user, 2026-11-07: build parts 2 and 3)

**What's wrong:**

- The only workflow is `wm:.github/workflows/release.yml`: it cross-compiles and publishes. Nothing runs `go vet` or `go test`.
- The e2e tests are opt-in, and "Only the user starts them" (`wm:test/e2e/doc.go:8-14`). Live checks wait until the user's PC is free, and tickets ship with "Compiled, NOT run" (`wm:.scratch/headless-max/issues/03-enablewindow-guard.md`) or "Not exercised live" (perf 01).
- Which live claims have been run is free text in ticket comments.

**agent-desktop:** a Windows test lane on `windows-latest` (`ad:.github/workflows/ci.yml:341-343`) and a capability probe there (`ad:.github/workflows/windows-capability-probe.yml:39`). Its probe measured that the hosted runner has an active console session (A14-1) and that out-of-process windows the test creates are walkable (A14-2) (`ad:probes/windows/FINDINGS.md:167-168`). It keeps a Findings Ledger, and its Mandatory Measurement Gate fails a run that measured nothing (`ad:CONCEPTS.md`, "Findings Ledger", "Mandatory Measurement Gate", "Gate Self-Test"). **cua:** Windows e2e on `windows-latest` after a session check (`cua:.github/workflows/e2e-rust-windows.yml:237`, Reviewer 1).

**What to build:**

1. CI on `windows-latest`: `go vet ./...` and `go test ./...` on pushes and PRs (size S).
2. An e2e lane on the hosted runner against winuse's own fixture app (`wm:internal/fixture`), with `WINUSE_E2E=1` set only there (size M). Never on the user's desktop.
3. A small ledger of live claims (claim, test, last run, result), and an e2e lane that fails when a declared test skipped.

**Blocked by:** none

- [x] `ci.yml` with vet and test on windows-latest
- [x] e2e lane on the runner (fixture only); amend `test/e2e/doc.go`'s rule to "the user, or CI on a hosted runner"
- [x] Ledger file and the skip-fails check

**Verified by the orchestrator:** the single workflow, `doc.go`'s wording, agent-desktop's `windows-latest` lanes. Not opened by the orchestrator: cua's workflow.

**Conflicts:** `test/e2e/doc.go` says agents must never enable the opt-in. The runner is not the user's PC, but this is the user's call. ADR 0008 (task suite, user-started) stays.

## Comments

### 2026-11-07: part 1 landed

- Workflow (`.github/workflows/ci.yml`, name `ci`): runs on every push and pull request, one job on `windows-latest` with `CGO_ENABLED=0` for every step and `permissions: contents: read`. Steps: `actions/checkout@v4`, `actions/setup-go@v5` with `go-version-file: go.mod` (the same action pins as `release.yml`; the version comes from go.mod's `go 1.27.1`, while release.yml hard-codes `"1.27"`), then `go vet ./...`, `go test ./...` and `go build ./...`.
- No e2e: `WINUSE_E2E` is set nowhere, the `e2e` build tag is not passed, and `test/e2e/doc.go` is unchanged. Without the tag, `./test/e2e` compiles only `parsing_test.go` (`TestAppBlock`, offline). Unit tests that could reach the desktop sit behind `win.LiveDesktopGuard`, which refuses under `go test` unless `WINUSE_E2E=1`.
- Local check (this laptop, Go 1.27.1): with `CGO_ENABLED=0`, `go vet ./...`, `go test -count=1 ./...` and `go build ./...` all pass. Nothing needs a C compiler. The YAML parses.
- Not run on GitHub yet: nothing was pushed. Things that could fail on the hosted runner: `internal/win/windowtitle_test.go` creates a real message-only window on a busy thread (no visible UI, but it needs a window station); `internal/helper` takes about 32 s and has timing-based waits that a slower runner could make flaky; `internal/version/build_test.go` shells out to `go tool buildid` (it skips if that fails). `internal/cli/dispatch_test.go` skips one case in session 0, and `skilllink_test.go` skips when it cannot create junctions or symlinks.
- Open: parts 2 (the e2e lane on the runner, fixture only, and the change to doc.go's rule) and 3 (the ledger and the skip-fails check) wait on the user's decision about e2e in CI.

### 2026-11-07: parts 2-3 built (trial pending)

User's decisions (2026-11-07): an e2e lane on GitHub's hosted `windows-latest` runner against winuse's own fixture app only, `WINUSE_E2E=1` set only in that job; tests that drive other Apps, browsers, the shell, toasts or need anything else the runner lacks do not run there and the ledger says why; doc.go's rule becomes "the user, or CI on a hosted runner"; a small ledger of live claims; a check that fails the lane when a declared runner test skipped or did not run.

- Ledger (`test/e2e/ledger.md`): one Markdown table, one row per top-level e2e test (195): Test, Claim (plain words), Runs on (`runner` or `user`), Why not on the runner (required for `user` rows), Last run, Result. 154 rows are `runner`, 41 are `user`. Last run is `-` everywhere and Result is `not yet run on runner` for runner rows (`-` for user rows): nothing is marked passed yet. The lane never edits the file; the columns are filled by hand from a lane's job summary or a user's run.
- User-only rows and why: Edge or Chrome on an isolated profile (10, browsers are not the fixture), the user's own browser through the extension (2), Notepad (4), File Explorer (5), a WPF window hosted by Windows PowerShell (3), Electron / WebView2 / WinForms / WinUI measuring Apps behind their env vars (7), toasts and the notification banners (3), the shell and taskbar (5: four behind `WINUSE_E2E_SHELL`, plus `TestShellReadsTheTaskbar`, which reads the shell rather than the fixture), and the physical Esc key (2, `WINUSE_E2E_HUMAN`). Runner rows include `TestClipboard` (Windows PowerShell only reads the clipboard; no other App's UI is driven) and the two offline parsers that live in the package (`TestAppBlock`, `TestNotepadSessionParsing`).
- Runner set: chosen by the ledger, not by skip helpers. `cmd/e2eledger pattern` prints `^(TestA|TestB|...)$` from the runner rows (whole names only, so `TestClick` cannot pick `TestClickButton`); the lane passes it to `go test -run`. No test file changed for this. An env-var skip helper was the alternative, but it would need an edit in every non-fixture test, and a skip it missed would start Edge on the runner (Edge is installed there) instead of staying unselected.
- Check (`internal/e2eledger`, wired by `cmd/e2eledger check`; CI tooling, not shipped, like `cmd/winusetasks`): reads the `go test -json` stream and the ledger. The lane fails when a runner row's test skipped (the problem quotes its `t.Skip` reason), failed (first message), started but never ended, or has no event at all ("did not run"), and on any other failing test or subtest, a package failure (panic, `-timeout`) or a build failure. A user row that ran anyway, or a test with no row, is a note, not a failure. It prints a text report to the log and appends a Markdown table (problems first) to `$GITHUB_STEP_SUMMARY`. Offline tests: `ledger_test.go` (table parsing and its refusals, the pattern, and `TestLedgerCoversEveryE2ETest`, which fails `go test ./...` when an e2e test has no row or a row names no test) and `check_test.go` (pass, skip, did not run, cut short, failures, build failure, notes, non-JSON lines, the Markdown order).
- Workflow (`.github/workflows/e2e.yml`, name `e2e`, job `fixture`): push, pull_request and workflow_dispatch; `windows-latest`, `timeout-minutes: 75`, `CGO_ENABLED=0`, `permissions: contents: read`, the same checkout and setup-go pins as `ci.yml`. It builds `e2eledger`, then in one pwsh step with `WINUSE_E2E=1` and `TEMP`/`TMP` under the runner's temp folder runs `go test -tags e2e -count=1 -timeout 60m -json -run <pattern> ./test/e2e > e2e.json` (the tests build winuse and the fixture themselves, as `runner_test.go` and `fixture_test.go` do; no lock file or other setup is needed). The ledger check and an artifact upload (`e2e-results`: `e2e.json` and winuse's screenshots from `%TEMP%\winuse`) run `if: always()`. The go test step fails the job on any failure as go test does; the check fails it on skips and missing tests.
- Docs: `test/e2e/doc.go`, `README.md` (opt-in section, a new "The e2e lane and the ledger" section, step 5 of writing a test) and `runner_test.go`'s comment and skip message now say "the user, or CI on a hosted runner"; agents still never enable the opt-in on the user's PC. ADR 0008 (the Task Suite stays user-run) is unchanged.
- Local check (this PC, offline only; no e2e test was run here and `WINUSE_E2E` was never set): `go vet ./...`, `go vet -tags e2e ./test/e2e`, `go test -tags e2e -c` (compile only), `CGO_ENABLED=0 go test ./...` and `CGO_ENABLED=0 go build ./...` pass; both workflows parse as YAML; `e2eledger check` on a hand-written two-event file reports 1 passed, 1 skipped, 152 did not run and exits 1.
- Trial pending: nothing was pushed. Expected first-run trouble on the runner: the WGC tests may skip (WGC unsupported) and so fail the lane by design; `TestListWindowsOutput` and `TestGetWindowPrintsTheResovedLine` need at least one visible Window outside the fixture (the latter indexes the App field of an empty listing and would panic); Headed tests depend on the runner session granting the foreground and moving the real cursor; screenshot sizes depend on the runner's display and DPI; `TestClipboard` skips if the clipboard holds a non-text format; `internal/helper`-style timing waits may be flaky on a slower machine. A runner row that cannot work there moves to `user` with the reason, after the trial says so.

### 2026-11-07: the lane's trial runs on GitHub (orchestrator)

The branch was pushed to GitHub so the lane ran on the hosted `windows-latest` runner only; nothing ran on the user's PC. Every run selected the ledger's 154 runner rows and took about 4 minutes (the tests about 200 s).

- Run 42366078064: 127 passed, 27 failed, 0 skipped. Two stale tests, not the runner: 26 failures came from `expectedLines` (`get_window_state_test.go`), which still printed `ID: solo` on Solo One and Solo Two, an ID the two share, which a default read has dropped since speed-defaults. `TestObserveOnTheFixture` put `--observe` after set-value's value, where it is text (the same in release 0.3.0), so the field read "observed --observe".
- Run 42366605784, after fixing both: 153 passed, 1 failed. `TestObserveOnTheFixture`'s Batch failed at `press-key Tab`; its run helper printed only stderr, so it now prints stdout too.
- Run 42367002707: the same failure, now with its cause: an unheaded `press-key Tab` has no background route outside a classic dialog since headless-max 21 (`--post` or `--headed`). The test predates that; its Batch now posts the Tab.
- Run 42367421412 (push) and run 42367782401 (manual re-run of the same commit): 154 passed, 0 failed, 0 skipped, and the ledger check passed. The ledger's runner rows record both.
- None of the 27 failures came from the runner itself: WGC, screenshots, coordinate clicks, Headed Actions with the real cursor, the clipboard, the overlay and the multi-agent tests all passed there. The 41 user rows stay user-only (other Apps, browsers, the shell, toasts, a person's Esc).
- The lane runs on every push and pull request (about 5 minutes). The `ci` workflow passed on each of these pushes too.
