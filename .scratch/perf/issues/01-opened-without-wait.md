# 01: Report opened Windows without the 300 ms wait

Status: wontfix (superseded by ADR 0019 on main)

**What to build:** after a reported Action (`click`, `press-key`, `set-value`, `invoke-menu`, `batch`, Expand/Select), winuse waits up to `openedSettle` (300 ms, six 50 ms polls of `Surfaces`) for a Window the Action opened (`internal/helper/opened.go:51,225`). When nothing opens, the usual case, the whole 300 ms is spent: a Headless click is 375 ms instead of about 55 ms (`../spec.md`).

agent-desktop does not wait: it lists the process's surfaces once right after dispatch, for actions that may raise one (`ad:crates/core/src/ref_action/mod.rs:161,178`), and misses a dialog that shows later.

winuse keeps the report and drops the wait:

- one snapshot right after the Action; a change found there is reported under the Action as today;
- otherwise the settle goes on in the background for the rest of `openedSettle`, and what it finds (a new Window, the target closing, a new Tab from a probe, the App taking the foreground) is added as notes to the same Agent's next command, worded "after the last Action";
- the foreground note (`selfactivation.go`) follows the same rule;
- `in new` in a Batch keeps its wait (`batch.go:515`): the agent said a Window will open.

**Blocked by:** none

- [x] Immediate snapshot, background settle, per-Agent pending notes delivered with the next command
- [x] Foreground note through the same path
- [x] Offline tests: immediate change, late change delivered next, nothing pending, other Agent unaffected
- [ ] Benchmark: click back near the no-settle numbers
- [x] SKILL.md / CONTEXT.md wording of the note

## Comments

### 2026-11-07: landed (branch `perf-catch-up`)

- `reportOpened` takes one look right after the Action (`openedNow`, `internal/helper/opened.go`); a change found there is reported under the Action as before.
- Otherwise the rest of the settle runs in the background (`internal/helper/late.go`): one watch per Agent, keyed by `agent.FromContext(ctx).ID`. `deliverLate` wraps every command but status: a command that may act (not observation-only) stops the Agent's watch before it runs and gets what it found; a read gets what a finished watch found. Notes are reworded `Note: after the last Action, ...`. A failed command keeps them for the next one, except a Batch that ran on.
- The foreground note: `fgWatch` now carries pid, foreground and desktop-lane count from before the Action; the late watch ends with `lateForeground`, and `fgWatch.claim` keeps the immediate and late checks from both adding it.
- Without the store (`Services.late` nil) the settle runs inline as before; `defaultServices` always creates it.
- `in new` in a Batch keeps its own wait (`batch.go:515`).
- Tests: `TestOpenedLateWindowWithinTheSettle` (late note on the next command), `TestOpenedLateNotesStayWithTheirAgent`, `TestLateNotesStopAndWording`, `TestOpenedNothingNew` (whole settle in the background, nothing delivered). SKILL.md describes the late notes.
- Not exercised live: a dialog that opens after the first look (offline tests only).

### 2026-11-07: superseded

This branch was built on a local `main` 57 commits behind GitHub. GitHub `main` (release 0.3.0) already solved the same wait its own way: ADR 0019 watches the settle with a WinEvent hook and ends it when the App goes quiet (`speed-waits`, bca130b), and `--observe` prints what changed in the same call (ADR 0021). The late-notes version stays on the unmerged `perf-catch-up` branch only. Re-measure 0.3.0's clicks before reopening anything here.
