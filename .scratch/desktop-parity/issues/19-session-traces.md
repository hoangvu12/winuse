# 19: Session traces with redaction

**What to build:** `--trace <file>` (or `WINUSE_TRACE`) appends one JSON line per command: time, command, Window (App and title), arguments, outcome, delivery, duration, the route taken (Headless, Posted, Headed, CDP), and references. agent-desktop: session JSONL traces, `trace show`/`export` to HTML, redaction (`ad:docs/guides/traces.mdx:7-109`; `ad:crates/core/src/trace/trace_sanitize.rs`).

- Redact by default: typed text, set-value values, clipboard content, eval expressions and results, Element names in password fields. Replace with `{"redacted":true,"len":N}`.
- `--trace-unredacted` for local debugging only.
- Screenshots are referenced by path, not embedded.
- Size cap per file (32 MiB like agent-desktop), then roll over.
- `winuse trace show <file>` prints a readable summary. HTML export is out of scope.

**Blocked by:** none (uses 07's codes and delivery fields when present)

**Status:** done

- [x] Trace writer in the CLI (one place for every command)
- [x] Redaction rules with tests
- [x] `trace show`
- [x] SKILL.md and README mention

## Comments

### 2026-11-03: triage decision (user)

Kept as written.

### 2026-11-03: decisions on what the ticket left open (user, through the laptop orchestrator)

- **Output:** a line keeps the output capped at 4 KiB. The redaction rules apply first, then the cap. The line carries `out_len` and `truncated`. A Window State is never kept: the line has its line count (`out_lines`) and the screenshot path, so the tree's size and its password-field names stay out of the trace.
- **Window and route:** the helper reports them. The response gets optional, appended fields.
- **07's code and delivery:** left out until 07 lands, with a marked spot in the line.

### 2026-11-03: built (branch laptop/dp-19-traces)

- **CLI (`internal/cli/trace.go`):**
  - `--trace <file>`, `--trace=<file>` or `WINUSE_TRACE=<file>` turns tracing on. The flags are taken only before the command (among `--headed`), so a literal `--trace` in typed text is never eaten. `--trace-unredacted` without a trace file is a usage error.
  - `Main` starts the trace and writes the line when the command exits. `runRequest` sets `Request.Trace` and hands the exchange to the trace (two lines). `trace` itself is never traced.
  - A line holds: `time` (local, ms), `command` (the wire name), `agent` and `agent_label` (multi-agent 02), `window` {id, app, title}, `args` (the wire arguments), `routes`, `ok`, `exit`, `error`, `hint`, `out`, `out_len`, `out_lines`, `truncated`, `refs` (the screenshot or image paths the output names), `unredacted` and `duration_ms`. A usage error that sent nothing still writes a line, with its command and exit code. A comment marks where 07's code and delivery go.
  - **Redaction**, to `{"redacted":true,"len":N}` with N in characters:
    - arguments: type-text `text`, set-value `value`, clipboard-set `text`, dialog `text` (a prompt's answer) and eval `expression`, including in Batch steps;
    - output: eval results, and clipboard-get's text and files. An image's path is kept.
  - Each line is one write to a file opened for appending, so several Agents can share one trace. Past 32 MiB the file is renamed `<name>.1<ext>` (the first free number) and a new one started. A failed write prints one `winuse: trace:` line and never changes the exit code.
  - `winuse trace show <file>` prints one line per command (time, command, Agent, Window, routes, outcome, duration), then its arguments, error, hint and files indented, and totals at the end. Lines that are not trace lines are counted and skipped.
- **Protocol (`internal/protocol/trace.go`):**
  - The route names (`Headless`, `Posted`, `Headed`, `CDP`) and the `Trace` and `TraceWindow` types.
  - `Request.Trace` and `Response.Trace` are appended, both omitempty.
- **Helper (`internal/helper/trace.go`):**
  - The server puts trace notes on a traced request's context and answers them in `Response.Trace`, whatever the outcome.
  - Each pipeline notes its Window and route in one line: runAction (Headed), runHeadless and runWindowAction (Headless), runKeyHeadless (Headless, or Posted for Posted Keys), and runPage, runPageInput and runPageWindow (CDP).
  - `tracedWindow`, appended to `CommandsWith`'s wrapping loop, resolves the Window a traced command named when no pipeline noted one: reads, Tab commands, and refusals before a pipeline. Untraced runs pass through untouched.
- **Docs:** usage text (global options and `trace show`), SKILL.md (a recovery bullet and the command reference), README (the commands table) and docs/codex-parity.md (extension paragraph). CONTEXT.md is unchanged: the ticket names no term.
- **Tests:**
  - `internal/cli/trace_test.go`: flag parsing, the full line with the helper's Window and routes, redaction per command and in Batch steps (and `--trace-unredacted`), the output rules (eval, clipboard text, files and image, the Window State, the cap at a character boundary), failure, timeout and usage-error lines, roll-over twice, `trace show`'s exact output, and the usage text.
  - `internal/helper/trace_test.go`: notes only when asked (none on the wire otherwise), first Window and each route once, the Headed click and Headless restore-window pipelines, a traced read's Window, and no extra enumeration when untraced.
  - `internal/helper/parity_test.go`: `trace` is CLI-only.
- **Live (laptop, `WINUSE_E2E=1`, only `TestTraceRecordsRedactedLines`): PASS.** In the test temp folder, against the fixture, it ran `--trace` get-window-state `--screenshot`, then set-value with a secret and a refused click under `WINUSE_TRACE`, then `trace show`. The first run failed on the test's own assumption that screenshots are `.png`: they are `.jpg`. With the check fixed, it passed. It passed again after merging origin/main 82cc761 (headless-max 03, desktop-parity 10).
- **Not done:**
  - 07's `code` and `delivery`: 07 has not landed. Copying them from `protocol.Response` takes one line in `traceRun.line`.
  - Routes for the Headless Tab commands (`tab new`, `tab close`, and `tab switch` without `--headed`): their lines name the Window but no route. `tab switch --headed` goes through runAction and is Headed. select-text and invoke-menu go through runHeadless, runPage or runKeyHeadless, so they are covered.
  - Element names in password fields only reach a trace through a Window State, and a trace never keeps one. A `name:` reference the agent typed is kept as typed.
