# 09: The Task Suite measures speed

Status: ready-for-human
Type: task
Source: `.scratch/research-speed/report.md` recommendation 9; `notes-benchmarks.md` §3, §5

The number matches the research report's recommendation 9.

## Problem

The Task Suite says whether a task passed, plus Claude's `num_turns` and the
wall time from setup to cleanup. It cannot show whether a speed change
helps:

- The runner never sets `WINUSE_TRACE`, so winuse's own time per command is
  not recorded.
- `ParseClaudeResult` reads only `num_turns`, so tokens, cost and API time
  are thrown away.
- Each task runs once. Turn counts vary 2-3× between runs
  (explorer-organize 18 → 41 → 15), which is more than most changes would
  move them.
- Comparing two builds or two skills means two separate runs and a
  hand-made table.

## What was built

On branch `speed-metrics`, in `internal/tasksuite` and `cmd/winusetasks`.

- **The winuse trace, per run.** The Harness's environment gets
  `WINUSE_TRACE=<run dir>\trace.jsonl`. The setup and Check scripts never
  get it, and a user's own `WINUSE_TRACE` is removed. `ParseTrace` and
  `ReadTraceFiles` (which also read rolled-over `trace.N.jsonl` files) sum
  `duration_ms` by kind:
  - **observe**: list-windows, get-window, get-window-state, find,
    record-tree, zoom, list-apps, tab-list, list-displays, screenshot,
    clipboard-get, list-notifications
  - **wait**: wait
  - **other**: status, version, skill, browser-*
  - **act**: everything else (the Actions, batch, Window Actions,
    launch/close, navigate/eval, tab and clipboard changes, notification
    buttons)

  They also count commands, Actions (a Batch counts its steps), failed
  commands by `code`, and commands with a Headed route.
- **Model usage.** Claude now runs with `--output-format stream-json
  --verbose` (it was `json --verbose`). The result gives `num_turns`,
  `usage` (input with the cache-read and cache-write split, output),
  `total_cost_usd` and `duration_api_ms`. A run killed at its time limit
  has no result. Its turns and tokens are then summed from the assistant
  messages, each message id counted once, and marked `partial`; the cost
  is unknown. Pi: its assistant messages' `usage` blocks
  (input/output/cacheRead/cacheWrite/cost.total) are summed. This is
  untested against a real Pi, which is not installed here.
- **Phase times.** Setup, Harness (the agent's wall time) and Check are
  timed separately. `Seconds` still covers setup to cleanup.
- **Repeats.** `--repeats N` (1-20) runs each task N times. The summary
  shows medians, with `[min-max]` for wall time and turns.
- **A/B.**
  - `--winuse`, `--skill` and `--label` set arm A.
  - `--b-winuse`, `--b-skill` and `--b-label` set arm B, which takes arm
    A's value for anything it doesn't override.
  - A winuse value can be an exe, which is copied into the run directory,
    or a source tree holding `go.mod`, which is built there.
  - A skill value can be a SKILL.md, a directory holding one, or a tree.
  - The arms are interleaved ABBA across repeats.
  - The comparison table shows per task A → B medians and B's change.
    A `*` marks a B median outside A's min-max range. The totals are sums
    of the medians.
- **Results.**
  - `results.json` uses schema 2: every run's metrics, plus each arm's
    source, Go build id, skill source and SHA-256.
  - `summary.txt` holds the readable summary and, for A/B, the
    comparison.
  - `table.txt` now has a row per run.
  - `winusetasks report <results.json>` prints them again offline.
    Schema-1 files load too, with their steps and time standing in.

All of this is tested offline: `testdata/claude-stream.jsonl`,
`claude-stream-killed.jsonl` (torn last line) and `trace.jsonl` are
synthetic and follow the real formats. A tiny live `claude -p`
stream-json call, with no tools and no desktop access, confirmed the
field names. The tests also cover parsing, roll-over, the env, medians,
the summary and A/B tables, a results-file round trip, the CLI flags and
`report`.

## On this machine (2026-11-05, checked read-only)

- Edge 144 is installed (`App Paths\msedge.exe` exists), so edge-bookmark,
  edge-download and edge-form **run**. This differs from the earlier
  result files, which came from another machine.
- Paint is installed (Microsoft.Paint 11.2605; `mspaint.exe` app alias on
  PATH, and Go's `os.Stat` accepts it), so paint-draw and paint-rect
  **run**.
- VS Code: `code` is not on PATH, so vscode-edit is **skipped**.
- Pi is not installed, so `--harness pi` cannot run.
- explorer-organize, explorer-zip, notepad-note and notepad-replace run.

## Not done (next)

- No live run. Runs are user-started (ADR 0008).
- Per-phase timings inside the helper (cursor glide, settle) are still
  only in `WINUSE_CURSOR_TRACE` / helper.log.
- `reference-steps:` front-matter and the WES+ ratio.
- Image-token estimates from screenshot sizes.
- Statistical tests: the bootstrap CI or Wilcoxon from notes §5.3. The
  `*` marker is a stand-in.
- `--bare` for reproducibility.

## Comments
