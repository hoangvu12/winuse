# The winuse Task Suite

The Task Suite is the proof that winuse is as good as Codex Computer
Use: a fixed set of about ten real desktop tasks — spread across
ordinary Windows apps (Notepad, File Explorer), Chromium apps (Edge,
isolated profile), an Electron app (VS Code) and a canvas app (Paint) —
each judged by a **Check script that inspects the end result** (files,
settings, app state in the task's scratch folder). No model grades
anything: a Check reads state and exits 0 or non-zero.

A run drives a real agent through a real Harness, unattended:

- **Claude Code headless (`claude -p`) with Opus 5.5** (`claude-opus-5-5`)
  for the full suite, under fixed limits (15 minutes and 40 turns per
  task);
- **Pi headless with glm-5.3** (`iroha/dashscope/glm-5.3`) for the
  text-only subset — the tasks a text-only model can do from the
  accessibility tree alone (12 minutes per task).

The method is borrowed from Cua Driver Bench without copying its FSL
code: **checkers, not judges; the same Harness, model and prompt for
every run of a Harness** — only the winuse build changes between runs,
which is exactly what the suite exists to compare. Task ideas are
adapted from WindowsAgentArena (MIT): Notepad typing and saving, file
management, Paint drawing, browser form filling.

## Read this first: runs are user-started only

A run **takes over the screen**: for every task, the driven agent will
launch apps, click, type, activate windows and take screenshots on the
live desktop, one task after another, for the whole run. Nobody should
be using the machine while it runs. That is why the runner refuses to
start without `--i-am-the-user` — you, the human at the keyboard, type
that flag from your own terminal. **Never pass it on an agent's
behalf**, and never let an agent start a run: the Task Suite, like the
opt-in e2e tests, is the user's to start (the spec's Testing Decisions
say so).

## What you need

- **Claude Code** on PATH (`claude`), logged in, with access to the
  Opus 5.5 model — for the full runs.
- **Pi** on PATH (`pi`), configured for the `iroha/dashscope/glm-5.3`
  model — for the text-only runs.
- **Go 1.27 or newer**: each run builds the `winuse.exe` it exercises
  from this source tree, so a run always measures the tree you are in.
- The apps the tasks use. Notepad, Paint, File Explorer and Edge are on
  every Windows install. VS Code (`code` on PATH) is optional — without
  it the `vscode-edit` task is skipped, and the table says so.
- A terminal you can leave alone, and an unlocked desktop.

## Starting a run

From the repository root (any subdirectory also works):

```
go run ./cmd/winusetasks run --i-am-the-user
```

or build once and run the binary:

```
go build -o winusetasks.exe ./cmd/winusetasks
.\winusetasks.exe run --i-am-the-user
```

The text-only subset through Pi:

```
.\winusetasks.exe run --i-am-the-user --harness pi
```

One task only (debugging a failure):

```
.\winusetasks.exe run --i-am-the-user paint-draw
```

Other commands, safe to run any time (no desktop access):

```
.\winusetasks.exe list          # the tasks, their categories, text-only flags
.\winusetasks.exe validate      # parse every task, render every prompt
```

Options: `--time-limit 20m` (per task), `--max-turns 30` (claude),
`--harness pi`, and task ids to select tasks. Defaults are the fixed
method: 15m/40 turns (claude), 12m (pi).

## What a run does

For each task, in a fixed order:

1. Creates a fresh scratch folder under
   `%TEMP%\winuse-tasks-<timestamp>\<task>\scratch` — the only place
   the task's files, the browser's isolated profile and the Check's
   expectations live. Nothing outside it is touched (apps keep their
   own MRUs and caches, as any interactive use does; no real file or
   setting is the task's to change).
2. Copies this repo's `SKILL.md` into the scratch folder (`.claude/skills/`
   and `.agents/skills/`), so every run starts from the same guidance.
3. Runs the task's `setup.ps1` in the scratch folder.
4. Starts the local task server (127.0.0.1, rooted at the scratch
   folder) for tasks marked `http:` — browser pages are served from it,
   and a web form's submitted values are logged to it, so browser tasks
   leave a Check-readable end state.
5. Runs the Harness (`claude -p` or `pi --mode json`) with the task's
   prompt on stdin, the scratch folder as its working directory, and
   the freshly built `winuse.exe` first on its PATH — under the time
   limit, killing the process tree if it runs out.
6. Runs the task's `check.ps1`. Exit 0 = the task passed; anything else
   = failed, and the script's first output line is the reason.
7. Cleans up: every process the task started (apps, winuse's helper) is
   killed — never the shell, never anything that was already running.
   The task prompts also ask the agent to close what it opened.

Then it prints the table — pass/fail with **steps and time** per task:

```
Task              Category   Result  Steps  Time
edge-bookmark     chromium   PASS       23  2m04s
edge-download     chromium   FAIL       19  6m41s
        report.bin was not saved in the task folder
notepad-note      ordinary   PASS       14  1m32s
...

7 passed, 2 failed, 0 errors, 1 skipped — 10 tasks, 28m17s total, 182 steps
```

Statuses: **PASS** (the Check found the required end state), **FAIL**
(the end state is wrong, or the time limit ran out), **ERROR** (the run
could not be measured — setup failed, the Harness crashed), **SKIP**
(a requirement was missing, e.g. no VS Code).

Steps are what each Harness reports: Claude Code's `num_turns`, Pi's
tool executions. Time is wall clock per task.

Everything a run produces is kept in the run directory (printed at the
start, e.g. `C:\Users\you\AppData\Local\Temp\winuse-tasks-20260118-093015`):
per task, the prompt, the Harness's stdout/stderr (the full JSONL event
stream for Pi), the setup and Check output, and the scratch folder with
the task's end state; at the root, `table.txt` and `results.json`
(machine-readable, for comparing runs across winuse versions).

## The tasks

| Task | Category | Text-only | What it exercises |
|---|---|---|---|
| `edge-bookmark` | chromium | – | tabs, address-bar typing, the bookmark dialog, the profile's Bookmarks file |
| `edge-download` | chromium | – | browser settings (a real toggle whose state the Check reads from the isolated profile), a native save dialog, a download |
| `edge-form` | chromium | yes | a web form through the Chromium accessibility tree: text, select, radio, checkbox, textarea, submit |
| `explorer-organize` | ordinary | yes | new folder, multi-file move, rename in File Explorer |
| `explorer-zip` | ordinary | – | multi-select and a context-menu-driven ZIP, checked by reading the archive |
| `notepad-note` | ordinary | yes | multi-line typing and the Save As dialog |
| `notepad-replace` | ordinary | yes | the Open dialog and find-and-replace |
| `paint-draw` | canvas | – | a near-empty tree forcing screenshot + coordinate work: a drag across the canvas, saved as PNG |
| `paint-rect` | canvas | – | shape tools on the canvas, saved as PNG |
| `vscode-edit` | electron | – | an Electron app: workspace trust, new file, save dialog (skipped without VS Code) |

Each task is a directory under `tasks/`:

- `task.md` — a front-matter block (title, category, tags, required
  apps, `text: yes` for the text-only subset, `http: yes` for the task
  server, an optional `time-limit`) and the prompt body, with
  `{{.ScratchDir}}`, `{{.BaseURL}}` and `{{.EdgePath}}` templated in
  per run.
- `setup.ps1` — prepares the scratch folder (creates the files the task
  works on and the expectations the Check compares against).
- `check.ps1` — the Check: inspects end state only, prints a one-line
  reason on failure, exits 0 on success.

Adding a task is adding a directory; `winusetasks validate` and the
offline tests (`internal/tasksuite`) parse it, check that its scripts
exist, that its prompt renders, and that it never references a path
outside its scratch folder. Keep checks inspecting **end results** —
files, settings, app state — never anything a model said.

## Notes on the method

- The prompts are fixed per task, and every run of a Harness sends the
  same bytes: a fixed preamble (the rules of engagement) plus the
  task's body with the run's paths. The preamble forbids the one cheat
  that would hollow out the measurement: creating, editing, moving or
  deleting the checked files from the shell instead of through the
  apps.
- The skill the agent loads is copied from this source tree into the
  scratch folder, so runs do not depend on what is installed
  user-wide, and a run always tests the SKILL.md of the tree it is in.
- Claude runs use `--dangerously-skip-permissions` because an
  unattended run cannot answer permission prompts; the scratch folder
  is the blast radius. That flag is the Task Suite's choice, not the
  skill's (the skill leaves safety to the Harness).
- App state that apps keep in the user profile (MRUs, caches) is
  unavoidable, as it is for any interactive use; the checked end state
  is always confined to the scratch folder.
