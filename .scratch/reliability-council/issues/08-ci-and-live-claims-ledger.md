# 08: No CI runs tests; live claims are tracked in free text

Status: ready-for-agent (user, 2026-11-07: implement all 13; ask the user at the decision points in 08, 09, 12 and 13)

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

- [ ] `ci.yml` with vet and test on windows-latest
- [ ] e2e lane on the runner (fixture only); amend `test/e2e/doc.go`'s rule to "the user, or CI on a hosted runner"
- [ ] Ledger file and the skip-fails check

**Verified by the orchestrator:** the single workflow, `doc.go`'s wording, agent-desktop's `windows-latest` lanes. Not opened by the orchestrator: cua's workflow.

**Conflicts:** `test/e2e/doc.go` says agents must never enable the opt-in. The runner is not the user's PC, but this is the user's call. ADR 0008 (task suite, user-started) stays.
