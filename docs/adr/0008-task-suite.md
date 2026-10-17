# ADR 0008: The Task Suite — checkers not judges, one runner, tasks as data

## Status

Accepted (ticket 16 — Task Suite + Codex parity checklist).

## Context

The spec's Testing Decisions define the Task Suite as the quality
measurement: about 10 real tasks across ordinary Windows apps,
Chromium/Electron apps, a dialog-heavy flow and a canvas app, each
working only in a scratch folder, each judged by a Check that inspects
end results — never a model. A runner executes the suite unattended
through Claude Code (`claude -p`) with Opus 5.5 under fixed time and
turn limits, plus a text-only subset through Pi with glm-5.3, and
prints a pass/fail table with steps and time. Runs take over the screen
and are started only by the user.

Method constraints: same Harness, model and prompt across runs
(Cua Driver Bench's method — its code is FSL and was never copied);
task ideas may be adapted from WindowsAgentArena (MIT). The runner must
be user-started only, like the opt-in e2e suite, and its pure logic
must be offline-testable per the repo's testing decisions.

## Decision

1. **The runner is Go, in `cmd/winusetasks`, not shipped with
   winuse.exe.** The release workflow builds `cmd/winuse` only; the Task
   Suite is maintainer tooling. Its pure logic lives in
   `internal/tasksuite` (task-file parsing, prompt rendering, the
   opt-in refusal, harness argv and output parsing, the result table,
   the task server, the cleanup diff) and is covered by the default
   offline test command; `run.go` wires those to real processes and is
   exercised only by user-started runs. No cgo, no new dependencies.

2. **Tasks are data, not code.** Each task is a directory under
   `tasksuite/tasks/` with `task.md` (a tiny `key: value` front-matter —
   no YAML dependency — plus the prompt body with `{{.ScratchDir}}`,
   `{{.BaseURL}}`, `{{.EdgePath}}` templated per run), `setup.ps1`
   (prepares the scratch folder) and `check.ps1` (the Check: exit 0 =
   pass, first stdout line = failure reason). Checks inspect end state
   only: files, an isolated browser profile's settings, a submitted
   web form's log, image pixels. No model ever grades.

3. **The same Harness, model and prompt, every run.** HarnessSpec pins
   the model ids (`claude-opus-5-5`, `iroha/dashscope/glm-5.3`) and the
   fixed limits (claude: 15 minutes and `--max-turns 40`; pi: 12
   minutes). The prompt is a fixed preamble (rules of engagement —
   including "the apps must do it; never touch the checked files from
   the shell") plus the task body; the preamble is byte-identical for
   every task and both Harnesses, and a test pins that. The SKILL.md
   the agent loads is copied from the source tree into the scratch
   folder (both `.claude/skills/` and `.agents/skills/`), and the
   `winuse.exe` the run measures is built from the tree at run start —
   a run always measures the tree it started from, independent of
   user-wide installs.

4. **The prompt travels on stdin, never as an argument.** Harnesses are
   resolved through PATH with Windows' extension preference (.exe
   before .cmd, extensionless files never win — an sh shim named `pi`
   sits next to `pi.cmd`); an npm `.cmd` shim is unwrapped to its
   `node script.js` form so Go's argv quoting stays exact. Flags only
   are safe through a shim; a multi-line prompt is not, so `claude -p`
   reads it from stdin and `pi --mode json` reads the piped stdin as
   its initial message. Steps come from what each Harness reports:
   Claude Code's result JSON `num_turns`, Pi's `tool_execution_start`
   events.

5. **The user start is an explicit flag.** `winusetasks run` refuses
   without `--i-am-the-user`, with a refusal that says why and who may
   pass it. This is a refusal of convenience, not a security boundary —
   an agent could type the flag too, which is why every doc repeats
   that agents never may. It mirrors the e2e suite's WINUSE_E2E pattern
   (the default invocation must refuse), and it makes "the run
   started" always mean a human chose it.

6. **Scratch-only via the runner, not via trust.** Each task gets
   `%TEMP%\winuse-tasks-<stamp>\<task>\scratch`; setup, the Harness
   (cwd = scratch) and the Check all work inside it; the task prompts
   are statically checked (offline test) to reference no path outside
   the scratch folder or the one app-starting command they are given.
   Browser tasks use an isolated `--user-data-dir` profile inside the
   scratch folder, so even "settings" checks (the download prompt
   toggle, bookmarks) read profile files the run owns. Apps still keep
   their own MRUs and caches — unavoidable, as for any interactive
   use; no real file or setting is a task's to change.

7. **A local task server gives browser tasks a file-checkable end
   state.** Tasks marked `http: yes` get a 127.0.0.1 server rooted at
   their scratch folder (files served, `POST /log` appended to
   `submissions.txt`): a web form's submitted values become the end
   state a Check reads, and a download link serves deterministic bytes.
   This is the only way a browser page can leave state a script can
   inspect without touching anything outside the scratch folder.

8. **Cleanup is a process diff, never the shell.** The runner snapshots
   processes before each task and kills (taskkill /T /F, after
   re-verifying the pid still holds the same exe name) everything that
   appeared — apps the agent started, winuse's helper. `explorer.exe`
   is never killed (all Explorer windows live in the one pre-existing
   shell process); task prompts ask the agent to close what it opened,
   which is part of the measured task. A time-limit kill takes down the
   Harness's whole tree the same way.

## Consequences

- The suite measures end results under fixed conditions; comparing two
  runs is comparing two winuse builds (the runner records `results.json`
  with the binary version for exactly that).
- No agent may start a run, enable its flag, or run any task's setup or
  Check script — the offline tests parse task files but never execute
  them; the runner executes them only inside a user-started run.
- Claude runs pass `--dangerously-skip-permissions` because unattended
  runs cannot answer prompts; the scratch folder is the blast radius,
  and the choice is the runner's, not the skill's.
- Tasks whose apps are missing skip visibly (SKIP in the table) rather
  than failing: only VS Code is conditional (Edge ships with Windows
  and cannot be removed; Notepad, Paint and Explorer are always there),
  so at most one task of ten can skip.
- The parity checklist (`docs/codex-parity.md`) is guarded by an offline
  test: every Codex tool and parameter of the recovered surface must
  appear with a status, and every winuse command it names must exist on
  the CLI — the checklist cannot silently rot.
- Unverified as of this ADR: no full run has happened yet (runs are
  user-started by design). The suite's first real run is the ticket's
  remaining acceptance step.
