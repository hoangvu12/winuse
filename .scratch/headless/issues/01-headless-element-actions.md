# 01 — Headless Element Actions and `--headed`

**What to build:** `click` (Element reference, left, single), `set-value` and `perform-secondary-action` (except Raise) run through UI Automation patterns without activating the Window, injecting input or showing the banner. Headless click route: Invoke → Toggle → Select → Expand/Collapse → LegacyIAccessible default action; none offered fails with nothing sent and a `--headed` hint. Headless set-value skips the focus-first step. Minimized Windows refuse Headless Actions. A global `--headed` flag (and the protocol's `headed`) restores v1 behaviour; Batches carry it for every step. Esc in Headless scopes latches without swallowing. See spec: Implementation Decisions.

**Blocked by:** none

**Status:** done

- [x] Headless click/set-value/secondary-action never call Activate, Click, TypeText or the banner (offline, fakes)
- [x] `@x,y`, count > 1, non-left buttons and Raise stay Headed
- [x] Click route order and the "nothing was sent … pass --headed" failure
- [x] `--headed` anywhere on the command line, and on batch, gives v1 behaviour
- [x] Minimized Window refusal with hint
- [x] Passive Esc latch: stops, not swallowed; a Batch with a Headed step swallows

## Comments

- 2026-11-02 orchestrator: uia layer (headless.go, value.go focus flag, WriteError/VerifyError), protocol `headed` fields and win.HeadlessReady landed in the previous chat (uncommitted). Helper routing, CLI `--headed`, Batch scopes, passive Esc and failure mapping delegated to Worker A (a subagent).
- 2026-11-02 orchestrator (reviewed, done): Worker A landed `internal/helper/headless.go` (HeadlessActor + `var _` assertion, the one routing predicate `headlessStep` shared by commands and Batch, `runHeadless`: resolve → locked → integrity → `Services.Ready` (default `win.HeadlessReady`) → passive Esc → resolve index → cursor travel → Esc checkpoint → act → cursor click → Esc checkpoint; no Activate, no banner). Batch picks its scope up front: all-Headless → passive Esc, no banner; any Headed step → v1 swallowing scope + banner; batch `headed` injects `headed:true` into click/set-value/perform-secondary-action steps. Passive Esc: `escLatch.passive` counter, latch clears only when both counts reach 0; `EscWatcher.ArmPassive/DisarmPassive` behind the optional `helper.PassiveEscWatch`. CLI: `--headed` stripped before the command and anywhere after it, except inside literal text (type-text's text, set-value's value, also in Batch steps), where it stays text (`internal/cli/headed.go`). `uia.PerformSecondaryActionHeadless` refuses Raise (`ErrRaiseHeadless`). No existing test changed. `go build/vet/test ./...` green.
