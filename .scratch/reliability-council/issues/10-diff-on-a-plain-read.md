# 10: A plain get-window-state cannot print a diff

Status: done

**What's wrong:** `--observe=diff` works after an Action, but `checkObserve` refuses it on anything that is not an Action or Batch (`wm:internal/helper/observe.go:56-72`). Re-reading a Window after a `wait`, after the user did something, or in a loop that polls one Window, the agent pays for the whole tree again.

**cua:** `get_window_state` takes `since: <snapshot_id>` and returns only the changed rows (`cua-w:tools/get_window_state.rs:150-157`).

**What to build:** `get-window-state <window> --diff`, reusing `wstate.RenderChanges` and ADR 0021's rules: the same index-based diff, the same 60% fall-back to the whole State, the whole State when there is no earlier read by this Agent. Not cua's "reindexed" form (ADR 0021, Considered).

**Blocked by:** none

- [x] `--diff` on get-window-state, sharing observe's renderer and budgets
- [x] Offline tests: no earlier read, small change, large change
- [x] SKILL.md and help; codex-parity.md extensions paragraph

**Verified by the orchestrator:** `observe.go:56-72`. One reviewer (1); the orchestrator agrees: small, and ADR 0021's machinery is already there.

**Conflicts:** extends ADR 0021 Decision 1 (diff after an Action only); needs a line there.

## Comments

### 2026-11-07: landed

- Helper (`internal/helper/state.go`, `views.go`): `windowRef.Diff`. `renderState` takes the baseline and, under `--diff`, prints `wstate.RenderChanges` through the same `View` the plain read uses, so the budgets and defaults are observe's: shared IDs left out on both sides, the 28000-character cap, `--interactive`, `--depth`, `--compact`, `--max-chars`, `--full`, and `--out` (writes the diff uncut and prints `Window State diff: <file> ...`). The baseline is `storedState` (observe's: the Agent's latest Window State of the Window, find's and `--root`'s Elements included), taken by `baselineOf` before the read replaces it. Same index-based diff, same 60% fall-back, the whole State with a note when this Agent has no earlier read; not cua's reindexed form.
- Forms: the plain read, `--screenshot` (`screenshot.go`: baseline taken before the concurrent read, the Screenshot line after the diff), a Tab target (`tabtarget.go`, `tabWindowState`: the Agent's Tab State, named `tab <id> of window <n>`, as observe does), a browser Window (the merged Page read, as any full read), and a Batch step (`... then get-window-state --diff`, both Window and Tab). Refused: `--diff` with `--root` or `--skeleton` (they read part of the Window; usage error, nothing read); a Tab target already refuses those. `--json` wraps the text as for any read. `get-window-state --observe` now says to pass `--diff` (`internal/cli/observe.go`).
- CLI (`internal/cli/screenshot.go` `takeDiff`, `cli.go`, `batch.go`): `--diff` on get-window-state and its Batch step; `--diff=...` is a usage error. Usage lines (CLI and helper) and help (`help.go`: the flag's paragraph and an example) name it.
- Docs: `skills/winuse/SKILL.md` (the loop paragraph), `skills/winuse/references/batch-and-wait.md` (a bullet under --observe), `docs/codex-parity.md` (the observe paragraph, with cua's `since:`, and the extensions-beyond-Codex list), ADR 0021 Decision 1 extended to the plain read.
- Tests: `internal/helper/observe_test.go` `TestWindowStateDiff` (no earlier read prints the whole State with the note; a small change prints the exact diff; a second read finds no change; an insertion near the top falls back to the whole State under `--interactive`; `--out`; `--root`/`--skeleton` refused with no read). `internal/cli`: `find_test.go` (`--diff` parses, `--diff=1` refused), `batch_test.go` (a Batch step with `--diff`), `observe_test.go` (the `--observe` refusal names `--diff`). `go vet ./...`, `CGO_ENABLED=0 go test ./...` and `CGO_ENABLED=0 go build ./...` pass.
- Open: Live: not run (offline only). No offline test drives `--diff` on a Tab target or with `--screenshot`; both reuse the paths their plain reads test.
