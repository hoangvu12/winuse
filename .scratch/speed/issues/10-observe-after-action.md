# 10: Act and observe in one call (--observe, reads as Batch steps)

Status: ready-for-human
Type: task
Branch: speed-observe (worktree `winuse-wt/speed-observe`), off main 34512dd
From: `.scratch/research-speed/report.md`, recommendation 1 (cua `run_steps`, UFO² multi-action, Anthropic's computer toolset, agent-browser `diff snapshot`, browser-use `*` marks)

## Ask

- An opt-in `--observe=diff|full` on Actions and on `batch`: `full` is the Window State after the Action, `diff` only what changed since the last read, new Elements marked, falling back to full past a threshold (cua: 60%).
- `get-window-state` and `find` as Batch steps.
- The round-1 output budgets (`--interactive`, `--compact`, `--max-chars`, `--out`) on the observation.
- Element Indexes stay stable: define the diff baseline per Agent and Window and what a stale index does. An ADR.
- Measure `diff` against `full` in tokens on recorded fixtures.
- Help, the skill (core line, `batch-and-wait`, `reading`) and the Codex-parity doc.

## What was built

```
winuse <action> ... --observe[=diff|full] [--interactive] [--depth N] [--compact] [--max-chars N | --out <file>] [--full]
winuse --observe set-value 101 id:qty 3          # before the command works too
winuse batch <window> --observe[=diff|full] [...] <action> ... [then ...]
winuse batch <window> click name:Open then in new get-window-state --interactive then find --role button
```

- **Where:** every Action aimed at one Window or Tab and `batch` (`protocol.ObservableCommands`). The flags ride on the request (`protocol.Request.Observe`), not in the Action's arguments, so no step or command parser sees them. A bare `--observe` is `diff`. The budgets go anywhere outside type-text's text and set-value's value (as `--headed`), or on a Batch before its first step, so a `get-window-state` step keeps its own `--interactive`. Any other command refuses `--observe` (CLI: usage error; helper: `nothing was sent`, code `USAGE`).
- **The read** (`internal/helper/observe.go`): a registry wrapper runs the Action under its own budget, then reads the Window (or Tab target) once the Action released its lanes. The request budget grows by `ObserveTimeout` (10 s). A Batch reads its own Window once after the last step, past failed steps too; a failed Action and an Esc stop read nothing. A read that fails is a `Note: --observe could not read ...` after the Action's output; the Action's result stands.
- **Defaults (speed 13):** the observation takes a read's defaults: shared IDs left out (both sides of a `(was: ...)` too), the 28000-character cap (`--max-chars 0` lifts it), and `--full` for Codex's text. `Observe.MaxChars` is a pointer (absent: the default), and the view comes from the read's own `windowRef.view`; `--full` with `--compact`, `--interactive`, `--depth` or `--max-chars` is refused as on a read.
- **The diff** (`internal/wstate/diff.go`): by Element Index. `~` an index whose line or depth changed (old line in `(was: ...)`, cut at 100 characters), `*` an index past the old read's end, `-` an index past the new read's end. The focused line only when it changed; the walk's notes always.

  ```
  ok
  Window: "winuse fixture app", App: winusefixture.exe.
  Changed since this agent's last read of window 101 (~ changed, * new, - gone); Element Indexes not listed are unchanged:
  		~13 check box Notify me on Secondary Actions: Toggle ID: checkbox-notify (was: check box Notify me off Secondary Actions: Toggle ID: checkbox-notify)
  		*26 text Saved
  ```

  `No change since this agent's last read of window 101.` when nothing changed. The whole State prints, with `Note: the whole Window State: ...`, when there is no baseline or when the diff is longer than 60% of the whole under the same view.
- **Baseline:** the Agent's Window State of that Window exactly as stored when the read starts: per Agent (ADR 0014), per Agent per Tab for a Tab target (ADR 0017), including what `find`, `--root` and live `name:`/`id:` resolution added. That is every index the Agent can still use.
- **Indexes:** the observation is a full read and renumbers like `get-window-state`. Because the diff is by index, an index it does not list names an Element whose line is exactly as before; a `-` index is past the new end, so an Action on it fails as stale with nothing sent. A middle insertion makes every later index `~`, which goes past 60% and prints the whole State, so the agent never has to remap numbers. Durable indexes (agent-browser style) were considered and left for later: the store, the Page splice (`DocIndex`, `holdsPage`, `storedSubtree`) and the views assume positional, depth-first indexes (ADR 0021, Considered).
- **Batch read steps:** `get-window-state` (its flags, `--screenshot` included) and `find` (its filters), with the Batch's Window, `in <window>` or `in new`. They take no lane; a `--screenshot` step makes the Batch take the desktop lane up front, and `serialized` no longer waits for a lane its own Batch holds (it would have deadlocked for 12 s). A Tab-target Batch runs `get-window-state` steps on the Tab.
- **Docs:** ADR 0021 (new), an ADR 0004 amendment, CONTEXT.md (Window State, Batch), `docs/codex-parity.md`, help (a paragraph in `help global`/`help all`, the batch block, examples, and an `--observe` line under every observable command's `help <command>`), the skill core (one line in the loop, the topic table) and the `batch-and-wait` and `reading` topics.

## Measurements

`internal/wstate/observe_cost_test.go` (opt-in, `WINUSE_OBSERVE_COST_OUT`) renders, for each Window State and each simulated Action effect, what `--observe=diff` and `--observe=full` print, with the real code. Window States: the two recorded fixtures plus 14 distinct whole Window States re-extracted read-only from local Claude Code transcripts (5 to 341 Elements, median 13: Calculator, Settings, Discord, Brave, Word...). The copies were deleted after the run. Tokens: tiktoken o200k_base as a proxy, as in speed 05.

Rebased onto speed 13 (main 572adb3), so both sides print under the read's defaults: shared IDs left out, the 28000-character cap (which none of these states reaches). The first run, before speed 13, gave 12.6% for local effects.

| Effect | Cases | Whole fallbacks | diff tokens | full tokens | diff / full |
|---|---|---|---|---|---|
| no change | 16 | 1 | 779 | 9,118 | 8.5% |
| a field's value | 11 | 3 | 1,482 | 7,440 | 19.9% |
| a check box toggles | 4 | 0 | 407 | 5,737 | 7.1% |
| a status text changes | 8 | 1 | 691 | 7,145 | 9.7% |
| 3 Elements appear at the end | 16 | 3 | 1,550 | 9,454 | 16.4% |
| a node expands in the middle | 16 | 16 | 9,749 | 9,397 | 103.7% |

- **Local effects** (value, toggle, status, tail) print **13.9%** of the whole State: 4,130 against 29,776 tokens. By size: under 30 Elements 46.9%, 30 to 149 12.5%, 150 or more **3.6%**.
- **A middle insertion** always falls back. It costs 3.7% more than `full`, the note.
- **The turn is the bigger win:** `--observe=full` already saves one model turn (3 to 10 s) per checked step.
- **Real read, act, read pairs** were not measurable: the transcripts held almost none. Agents piped big reads through `Select-String` or `Select-Object -First` to save tokens, which `--observe` and the budgets now do inside winuse.

## Tests

- `internal/wstate/diff_test.go`: diff by index (depth counts), value and tail changes with the exact text, no change, gone indexes, fallback to the whole State on an insertion at the top, no baseline (the caller's tree untouched), `--interactive` leaving a change out with its note, `--max-chars` within the cap, `--compact` dropping shared IDs on both sides.
- `internal/wstate/observe_cost_test.go`: a printed Window State parses back to the same lines; the opt-in measurement.
- `internal/helper/observe_test.go`: diff after an Action, which keeps its own budget and does not see `--observe`; full with `--interactive`; `--out`; no baseline; a read that fails is a note; a failed Action reads nothing; a failed Batch observes under its lines; refusals before anything runs; the server hands `--observe` to the registry with the longer budget; a Batch with a `get-window-state` step; read steps take no lane, a screenshot step the desktop lane, and `serialized` does not wait for a lane its Batch holds.
- `internal/cli/observe_test.go`: flag extraction (bare, `=full`, budgets anywhere, before the command, literal text kept, Batch region, `batch -`), refusals, and every observable command has help.
- `internal/cli/batch_test.go`: `get-window-state` and `find` steps.
- `test/e2e/observe_test.go` (user-run): set-value `--observe` on the fixture, no change from a read-only Batch, `--observe=full --interactive`, a Batch with a `find` step, the foreground staying with the cover.

`go vet ./...`, `go vet -tags e2e ./test/e2e` and `go test ./...` pass.

## Live check (to run by a human)

These start the build's own helper, which makes an installed helper of another build exit. See the report to the orchestrator for the exact commands.

- `go test -tags e2e -v -run TestObserveOnTheFixture ./test/e2e`
- By hand on Notepad and Settings: `--observe` after `type-text`, `set-value` and a toggle; `--observe=full`; `perform-secondary-action ... Expand` on a tree item (expect the whole State with the note); `close-window ... --observe` (expect the `Note: --observe could not read ...` line); a Batch with `in new get-window-state`; a Page click with `--observe` (a late page change can show as `No change`).

## Comments
