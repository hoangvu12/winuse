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

Keep your hands off the keyboard and mouse once it starts. Since
multi-agent ticket 08, a real key press, click or wheel turn stops the
running Headed Action like Esc (exit 130), the agent is told the user
took over, and the task fails; a Headed Action also waits while input
is coming in.

## What to expect since ADR 0013 and 0014

The prompts and Checks did not change; winuse's routing did, and the
agent learns it from SKILL.md.

- Without `--headed`, an Action with no background route is refused
  with "nothing was sent" and the exact `--headed` retry (headless-max
  ticket 21). Expect the agent to retry with `--headed` where the tasks
  need the real pointer or keys: the canvas drags of `paint-draw` and
  `paint-rect`, and typing that has no background route, such as
  `notepad-note`'s multi-line note. A refusal costs a step or two.
- A Headed Action gives the foreground and pointer back when it is done
  (multi-agent 08): to the terminal running the Harness, unless the
  Action left a menu or dialog open, which a `Note:` says. The next
  Headed Action brings the app forward again.

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
.\winusetasks.exe report <run directory>\results.json   # a run's summary again
```

Options: `--time-limit 20m` (per task), `--max-turns 30` (claude),
`--harness pi`, and task ids to select tasks. Defaults are the fixed
method: 15m/40 turns (claude), 12m (pi). The speed options are below.

## Measuring speed

Every task run records how fast it was, not only whether it passed:

- **the agent's wall time** (the Harness process, start to exit), and
  the setup and Check times beside it;
- **the model's turns, input and output tokens, cost and time in model
  calls**, from Claude Code's result (`num_turns`, `usage`,
  `total_cost_usd`, `duration_api_ms`). The runner reads Claude's
  `--output-format stream-json`, so a run killed at its time limit
  still has the turns and tokens it used (marked partial; the cost is
  then unknown). Pi's per-message usage is summed when Pi sends it;
- **winuse's own time**: the runner sets `WINUSE_TRACE` for the Harness
  (never for the setup or Check scripts), so every winuse command the
  agent runs appends a line to `trace.jsonl` in the run's directory.
  The durations are summed by kind: **observe** (`get-window-state`,
  `find`, `list-windows`, `screenshot`, ...), **act** (every Action,
  Batch, Window Action, launch, navigation, ...), **wait** (`wait`),
  and other (`status`, `skill`, the browser plumbing). Also counted:
  commands, Actions (a Batch counts its steps), failed commands and
  their codes (`STALE_REF`, ...), and commands that went Headed. The
  trace is redacted by default (typed text and values are kept as
  lengths only), so it is safe to keep with the results.

Turn counts vary 2–3× between runs of the same task, so one run says
little. **`--repeats N`** runs every task N times; the summary gives
the median of each metric, with the min–max range for wall time and
turns.

**A/B runs** compare two winuse builds, two skills, or both:

- `--winuse <exe|tree>` and `--skill <file|dir|tree>` set arm A (by
  default this tree's build and skill). A winuse.exe is copied into
  the run directory, and a source tree (a directory holding `go.mod`,
  such as a branch's worktree) is built there; nothing else is written.
  A skill is the folder that holds the SKILL.md named (a file, a folder
  holding it, or a tree's `skills\winuse\`): that SKILL.md and the
  topics in `references\` beside it, read once when the run starts and
  hashed together.
- `--b-winuse`, `--b-skill` and `--b-label` set arm B, which takes arm
  A's value for whatever it does not override. Any `--b-*` option makes
  the run A/B. `--label` names arm A (default `A`).
- Each task runs in both arms, interleaved ABBA across the repeats, so
  drift on the machine hits both arms. After the summary comes the
  comparison: per task, A's and B's medians and B's change, with a `*`
  where B's median falls outside A's min–max range (anything inside is
  as likely noise), then the totals over the tasks both arms ran.
- An A/B run with nothing different in arm B (`--b-label again`) is an
  A/A run: it shows how much a task moves by chance.

```
.\winusetasks.exe run --i-am-the-user --repeats 3
.\winusetasks.exe run --i-am-the-user --repeats 3 --label main --b-winuse ..\winuse-wt\speed-waits --b-label waits
.\winusetasks.exe run --i-am-the-user --repeats 3 --b-skill C:\path\to\lean\SKILL.md --b-label lean-skill
```

The winuse helper is one per user session, whichever winuse.exe
started it: a run's first command restarts a helper of another build,
and an arm whose exe is the installed winuse.exe's build uses that
helper if it is running. Quit other winuse users before a run, as for
any run.

## What a run does

For each task, in a fixed order:

1. Creates a fresh scratch folder under
   `%TEMP%\winuse-tasks-<timestamp>\<task>\scratch` — the only place
   the task's files, the browser's isolated profile and the Check's
   expectations live. Nothing outside it is touched (apps keep their
   own MRUs and caches, as any interactive use does; no real file or
   setting is the task's to change).
2. Copies this repo's skill, `skills/winuse/SKILL.md` and its topics in
   `references/` (or the arm's `--skill` folder), into the scratch folder
   (`.claude/skills/` and `.agents/skills/`), so every run starts from
   the same guidance.
3. Runs the task's `setup.ps1` in the scratch folder.
4. Starts the local task server (127.0.0.1, rooted at the scratch
   folder) for tasks marked `http:` — browser pages are served from it,
   and a web form's submitted values are logged to it, so browser tasks
   leave a Check-readable end state.
5. Runs the Harness (`claude -p` or `pi --mode json`) with the task's
   prompt on stdin, the scratch folder as its working directory, the
   run's `winuse.exe` first on its PATH and `WINUSE_TRACE` pointing at
   the run's `trace.jsonl` — under the time limit, killing the process
   tree if it runs out.
6. Runs the task's `check.ps1`. Exit 0 = the task passed; anything else
   = failed, and the script's first output line is the reason.
7. Cleans up: the processes the task started are killed one by one —
   the Harness's descendants (recorded while it runs), the run's own
   winuse helper, and new processes of the task's declared apps (not
   children of the user's already-running copy). Processes the machine
   starts meanwhile (browser renderers, sync clients, your tools) are
   not the task's and are left alone; so is the shell and anything that
   was already running. The task prompts also ask the agent to close
   what it opened.

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
tool executions. Time is wall clock per task. With repeats or arms the
table has a row per run, named by its arm and repeat (`A r2`).

After the table comes the speed summary (see Measuring speed), one row
per task: pass count, wall time and turns as `median [min-max]`, then
the medians of input and output tokens, cost, API time, winuse's time
and its observe/act/wait split, commands/Actions, failed commands and
Headed commands. An A/B run adds the comparison table.

Everything a run produces is kept in the run directory (printed at the
start, e.g. `C:\Users\you\AppData\Local\Temp\winuse-tasks-20260118-093015`):
per task (per `<task>\<arm>-r<n>` with repeats or arms), the prompt,
the Harness's stdout/stderr (the JSONL event stream), the winuse trace
(`trace.jsonl`), the setup and Check output, and the scratch folder
with the task's end state; at the root, `table.txt`, `summary.txt` and
`results.json` (machine-readable, schema 2: every run's metrics, and
each arm's winuse source, Go build id, skill source and SHA-256).
`winusetasks report <results.json>` prints the summary again, older
schema-1 files included.

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
  user-wide, and a run always tests the skill of the tree it is in.
- Claude runs use `--dangerously-skip-permissions` because an
  unattended run cannot answer permission prompts; the scratch folder
  is the blast radius. That flag is the Task Suite's choice, not the
  skill's (the skill leaves safety to the Harness).
- App state that apps keep in the user profile (MRUs, caches) is
  unavoidable, as it is for any interactive use; the checked end state
  is always confined to the scratch folder.
