# What winuse can learn from vercel-labs/agent-desktop (November 2026)

Status: research note, read only. Written 2026-11-15. Nothing was built,
run, launched or driven. It builds on three earlier notes and does not
repeat them:

- `.scratch/headless-parity/agent-desktop-comparison.md` (headless mechanics)
- `.scratch/headless-gaps/agent-desktop-parity.md` (whole command surface, 22 tickets)
- `.scratch/browser-merge/agent-desktop-research.md` (browsers, CDP)

Those notes compared agent-desktop at 9eb9b29 (v1.0.2). Since then winuse
closed almost every gap they listed: clipboard, displays, notifications,
shell surfaces, `find`, `--skeleton`/`--root`, desktop `wait`, `--json`
with delivery fields, `--trace`, `status`, the embedded skill, blocked
combos, click modifiers, `close-app`, `maximize-window`, Batch per-step
output and `--stop-on-error`, live `name:`/`id:`, `--observe`, several
Agents. So this note looks for what is new upstream, and for deeper ideas
in the code that the parity tables did not reach.

## Sources and versions

- **agent-desktop**: shallow clone of `main` at
  **8ffecc1** ("fix: name the launching app in the permission guide under
  tmux (#681)", 2026-11-14), made with `gh repo clone` on 2026-11-15.
  Latest release **v1.3.1** (2026-11-13). Releases since the earlier notes:
  v1.1.0 (2026-11-02), v1.2.0 (2026-11-05), v1.3.0 (2026-11-11), v1.3.1.
- Citation prefix `ad/<path>:<lines>` is relative to that clone. Permalink
  form: `https://github.com/vercel-labs/agent-desktop/blob/8ffecc1d3d088727f8d84baf4dde0224aab2e8ad/<path>#L<a>-L<b>`.
  `ad/docs/...` abbreviates `ad/apps/docs/content/docs/...`.
- Open PRs are cited by number (`#643`) and read with `gh pr view`; their
  head commits are not on `main`. Where I cite a PR's file, I name its
  head commit: PR #667 at `ce6e388` (`ad@ce6e388/<path>`), PR #689 at
  `b05dc12`.
- **winuse**: paths are relative to this worktree, branch
  `zeron/browser-alternative-concept-discussion` at **03fc3a4** (release
  0.3.0). Citation form `winuse/<path>:<lines>`.
- "(inference)" marks a conclusion I drew and did not see stated or
  measured. agent-desktop's numbers are their measurements, on their
  machines.

## What changed upstream since 9eb9b29

- **v1.1.0**: refs re-identified by a "capture rule" on Windows and Linux
  (role plus stable fields, unique in the window at capture) (#442); the
  macOS command host removed, so every call is its own process on every OS
  (#414, `ad/CHANGELOG.md:75`); `type --interval-ms`; overlay caret.
- **v1.2.0**: `agent-desktop mcp`, one MCP tool per CLI command, generated
  from the CLI definitions (#519, `ad/CHANGELOG.md:39`); `--stdin` for
  `type`, `set-value`, `clipboard-set` (#373, `ad/CHANGELOG.md:40`);
  partial snapshots kept when the deadline expires (#372).
- **v1.3.0/1.3.1**: `sandbox` (macOS VMs only) (#505, #506); state-machine
  fixes (#602); a virtual clock for the settle-budget tests (#642).
- **Open, large, relevant to Windows**: #643 reads a node's children in one
  cached call (winuse's design already); #613 turns AutoSetFocus off (winuse
  did this in ADR 0010); #651 a UIA activation fallback; #660/#664/#666/#667
  a reliability stack (headless on all OSes, truthful errors, dispositions
  on every command, one-file skills that never suggest `--headed`); #683
  twin refs identified inside their nearest unique ancestor; #689 grid
  sizes and cell positions; #690 click on an expand-only button; #679 radio
  state through SelectionItem; #665 golden JSON fixtures.

## Ranked summary

Ranked by value to winuse (turns and wall-clock saved, then output, then
robustness). Effort: S under a day, M a few days, L a week or more.

| # | Idea | Area | Effort |
|---|---|---|---|
| 1 | `--observe` follows the Window the Action opened (menu, dialog) | Speed | S |
| 2 | Bounded auto-wait for an Element to be present and enabled before refusing | Speed | S–M |
| 3 | One huge child list must not end the whole tree read; count-only skeleton reads with a small budget | Robustness/perf | M |
| 4 | `--stdin` (and `--file`) for `type-text`, `set-value`, `clipboard-set` | Feature | S |
| 5 | Tables and grids: one node with its size in `--skeleton`; cells carry row and column | Output | S–M |
| 6 | A latency A/B harness: HEAD vs merge-base, fixture plus read-only real apps, p50/p95 | Tests/perf | M |
| 7 | Cache policy by provider: no CacheRequest for in-process Win32 proxies | Perf | S (measure first) |
| 8 | Agent Cursor cost in Batches and unattended runs (user's call) | Speed | S |
| 9 | Golden output fixtures and a virtual clock for timing tests | Tests | S |
| 10 | Skill lines: never copy through the app, Action and its wait in one Batch, Escape can open discard dialogs | Output | S |
| 11 | Intent resolver that keeps the tree out of the main model's context (jev-desktop) | Speed | L, later |
| 12 | MCP server generated from the CLI | Feature | M, conflicts ADR 0001 |

## 1. Features

### 1.1 `--stdin` and `--file` for text (rank 4)

- **agent-desktop.** `type`, `set-value` and `clipboard-set` take
  `--stdin`, which reads UTF-8 from a pipe or a redirected file; terminal
  input is rejected (`ad/CHANGELOG.md:40`;
  `ad/docs/commands/interaction.mdx:159-181`). The rewritten skill tells
  agents to send secrets this way, because Batch JSON puts text in process
  arguments, and says `--stdin` drops observed `value` fields from results
  (`ad@ce6e388/skills/agent-desktop/SKILL.md:105-107`).
- **winuse.** Only `batch -` and `eval --file -` read stdin
  (`winuse/skills/winuse/references/batch-and-wait.md:18`,
  `winuse/skills/winuse/references/browser.md:102`). `type-text`,
  `set-value` and `clipboard-set` take text only as arguments
  (`winuse/skills/winuse/references/commands.md:23-25,53`).
- **Why it matters for winuse.** Two reasons. Secrets typed into a login
  form sit in the command line, the Harness log and `--trace`. And the
  skill already warns that PowerShell 5.1 strips double quotes inside
  arguments (`winuse/skills/winuse/SKILL.md:203-205`), which breaks any
  text with quotes, not only JavaScript.
- **Mapping.** `type-text <window> [--into <ref>] --stdin` and
  `--file <path>`, the same for `set-value` and `clipboard-set`. Read in
  the CLI process and send in the request, so nothing new crosses the pipe.
  With `--stdin`, leave the value out of `--trace` and of the read-back
  note. Not a Batch step form (a Batch already has `batch -`).
- **Effort.** S.

### 1.2 Tables and grids (rank 5)

See 3.1; it is mostly an output change.

### 1.3 MCP server generated from the CLI (rank 12)

- **agent-desktop.** `agent-desktop mcp` serves each leaf command as a tool
  named `desktop_<command>`; the tool list comes from the CLI's own
  definitions; a tool returns the JSON envelope as `structuredContent`;
  global flags apply only from the server's command line; at most 8 calls
  run at once (`ad/docs/guides/mcp.mdx:7-68`). A read-only server mode is
  an open PR (#587).
- **winuse.** ADR 0001 chose a skill plus CLI, not MCP
  (`winuse/docs/adr/0001-skill-and-cli-not-mcp.md`). `--json` exists.
- **Mapping.** A thin generated wrapper over the command registry and
  `--json` would cost little and would reach MCP-only hosts (Claude
  Desktop, Cursor) (inference). It needs an ADR 0001 amendment. The skill
  would have to reach those hosts as a tool (`desktop_skills_get` is their
  answer). Not recommended now; revisit if a user asks for an MCP host.

### 1.4 Trace export with screenshots (low)

- **agent-desktop.** `trace export` writes a self-contained HTML viewer;
  `session start --screenshots` stores before and after PNGs per ref
  action and embeds them (`ad/docs/guides/traces.mdx:41-44,81`).
- **winuse.** `--trace <file>` and `trace show <file>` (summary)
  (`winuse/skills/winuse/references/commands.md:64`).
- **Mapping.** An HTML export of a winuse trace, with the Window State
  diff per step from `--observe`, would help debug Task Suite failures
  (inference). Effort M. Low value for agents themselves.

## 2. Speed

### 2.1 `--observe` follows the Window the Action opened (rank 1)

- **agent-desktop.** `click`, `expand`, `select`, `press` and the
  multi-clicks report the surfaces open after the action in
  `data.surfaces`; the skill says to target that overlay next
  (`ad/docs/surfaces.mdx:55-66`; `ad/skills/agent-desktop/SKILL.md:47`).
  It does not inline the overlay's tree. The rewritten skill warns that an
  attached `--wait-for` "usually observe[s] the parent window, not a
  separate popup" (`ad@ce6e388/skills/agent-desktop/SKILL.md:76-78`).
- **winuse.** The opened-Window note already exists
  (`winuse/internal/helper/opened.go:9,396`). But `--observe` reads only
  the Window the command named (`winuse/internal/helper/observe.go:124-145`,
  ADR 0021 Decision 2). So after `click name:File` the agent gets the
  diff of the main Window, plus `Note: a new Window opened: 5678 ...`, and
  then spends one more turn on `get-window-state 5678`. A Batch can do it
  (`then in new get-window-state`,
  `winuse/skills/winuse/references/batch-and-wait.md:37-40`), but only when
  the agent planned for it.
- **Mapping.** When `--observe` is set and the opened-Window settle reports
  exactly one new Window of the same App (a menu, a dialog, a popup), read
  that Window too, in full (there is no baseline), after the diff of the
  Action's own Window. With several new Windows, keep the note. Menus and
  dialogs are among the most common Action effects, so this saves a turn
  where agents spend the most (inference, consistent with ADR 0021's
  "LLM calls are 75–94% of task time").
- **Conflicts.** ADR 0021 Decision 2 says the observation is the Action's
  Window. Amend it. The new Window gets its own Window State per Agent,
  as `get-window-state` would give it.
- **Effort.** S.

### 2.2 Bounded auto-wait before refusing (rank 2)

- **agent-desktop.** Every ref action waits up to 5000 ms (`--timeout-ms`)
  for its target to resolve and become actionable. Core polls the
  transient checks (visible, stable, enabled, and for pointer delivery
  receives_events) and fails at once on terminal ones (supported action,
  policy, editable). `--timeout-ms 0` tries once. A timeout returns
  `actionability_timeout` with the failing check
  (`ad/docs/commands/interaction.mdx:61-66,85,99`). The guide says this is
  why an agent "rarely need[s] a `wait` before it"
  (`ad/docs/guides/observe-act-verify.mdx:39`). A retryable resolution
  failure (element not there yet) also keeps polling to the deadline
  (`ad/CHANGELOG.md`, v0.10.0 breaking-changes list).
- **winuse.** A disabled Element is refused at once with
  `the Element is disabled; nothing was sent`
  (`winuse/internal/uia/value.go:232`; Page: `page.NotActionableError`,
  `winuse/internal/helper/pageactions.go:457-475`). A missing `name:` gets
  one fresh read, then fails (`winuse/skills/winuse/references/reading.md:130-139`).
  The earlier parity note listed "no wait-until-enabled" as partial and
  made no ticket (`.scratch/headless-gaps/agent-desktop-parity.md:202`).
  The agent's recovery is `wait <window> <ref> enabled`, then a retry: two
  more turns.
- **Mapping.** Before refusing for a transient reason, poll for up to about
  2 s (inside the Action's 10 s budget): Element disabled, `name:`/`id:`
  not found yet, Page Element covered or not yet visible. Poll before the
  lane is taken, as agent-desktop holds its lease only during dispatch
  (`.scratch/browser-merge/agent-desktop-research.md:175`), so another
  Agent is not blocked. Keep the refusal text and "nothing was sent" when
  the wait runs out, and add how long it waited. A flag (`--no-wait`) keeps
  the old behaviour for tests.
- **Conflicts.** Codex parity rows on refusals may need "(extended)". The
  Esc stop must still end the poll.
- **Effort.** S–M.

### 2.3 Agent Cursor time per Action (rank 8, the user's call)

- **agent-desktop.** The overlay is off unless a session enables it
  (`ad/docs/commands/system.mdx:106`). They measured its cost: a headless
  click took 427 ms without it and 782 ms with it, "about 355 ms per
  action", "a hundred overlaid clicks add about 35 seconds"
  (`ad/skills/agent-desktop-windows/references/shell-and-overlay.md:104-106`).
  The cursor travels before the lease is taken, so parallel agents' cursors
  move at once (#347).
- **winuse.** The cursor is always on for Headless Actions. The glide has a
  600 ms floor (`winuse/internal/win/cursor_motion.go:36-39`), and ADR 0018
  records "every Headless and Page Action takes about 300 ms longer"
  (`winuse/docs/adr/0018-agent-cursor-look-and-motion.md:33-35,200-201`).
  The user chose this for the feel.
- **Mapping (options, not a recommendation to change the default).**
  (a) In a Batch, glide in full to the first step's Element and use the
  120 ms reduced-motion move (ADR 0018 Decision 9) for the following steps
  in the same Window. (b) A helper environment switch for unattended runs
  (the Task Suite) that uses the reduced-motion move. (c) Start the glide
  as soon as the Element's bounds are known, while the lane wait and
  preflight run (inference: check whether this already overlaps).
  A ten-step Batch spends about 6 s in glides today (inference from the
  600 ms floor).
- **Effort.** S. Needs the user's decision.

### 2.4 Keep the tree out of the main model's context (rank 11, later)

- **agent-desktop.** `jev-desktop` resolves one intent at a time with a
  separate decision model, so the 150-element tree never enters the
  agent's context. Each turn asks for an operation and its target in one
  request; the target is chosen only from Elements that advertise that
  operation; "looking is an operation" (skeleton first, `DRILL`, `WIDEN`);
  five questions (target, command, present, destructive, needs_text) are
  answered in parallel, "so the speculative ones cost tokens and no
  latency"; confidence bars rise with the risk (0.70, 0.90 for
  destructive) (`ad/skills/jev-desktop/SKILL.md:44-62,90-99,208-224`). It
  needs a third-party API key and sends screen text off the machine
  (`ad/skills/jev-desktop/SKILL.md:71-89`).
- **winuse.** `find`, `--skeleton`, `--interactive --compact`, the 28000
  character cap and `--observe` diffs keep reads small
  (`winuse/skills/winuse/references/reading.md:68-107`).
- **Mapping.** Not as a remote service: that breaks "runs on the user's
  machine" and sends private screen text away. The pattern could be a
  skill recipe: hand a dense Window to a cheap sub-agent of the same
  Harness with "return only the Element Reference for <intent>" (inference).
  The operation-gated target list is a good rule for `find` too: a
  `find --can click|set|toggle` filter. Low priority.

### 2.5 Already covered (speed)

- **Process model.** agent-desktop removed its only long-lived host and
  runs one process per call everywhere (#414). winuse's helper keeps live
  UIA elements and caches across calls (ADR 0004 Live-Element store). winuse
  is ahead; nothing to adopt.
- **Shared process inventory per listing** (#586: `list-windows` 59.1 ms to
  21.8 ms). winuse did this in speed issue 02
  (`.scratch/speed/issues/02-shared-app-name-cache.md`).
- **Children in one cached call** (#643: Chrome 1089 ms to 808 ms). winuse
  reads each node with one `TreeScope_Element|Children` cache chunk already
  (`winuse/internal/uia/capture.go:61-66,151-196`). See 4.1 for the case
  #643 refuses to batch.
- **Act and verify in one call.** agent-desktop's `--wait-for role:text`
  and batch-then-wait (`ad@ce6e388/skills/agent-desktop/SKILL.md:63-69,84-91`)
  match winuse's `--observe` and `batch --observe ... then wait ...`
  (ADR 0021).
- **Wait failures say what they saw** (agent-desktop `details.last_observed`;
  winuse `batch-and-wait.md:94-95`).

## 3. Output quality

### 3.1 Tables and grids in a skeleton, cells with positions (rank 5)

- **agent-desktop.** Open PR #689: "A table or grid that reports its size is
  one node. Below the root it lists no cells at any depth. It carries
  `row_count`, `column_count` and `children_count`. Drill at its ref to read
  its cells" (`ad@b05dc12/apps/docs/content/docs/skeleton-and-drill-down.mdx:73`;
  the cut is `crates/core/src/snapshot/grid_collapse.rs` there). Cells
  report their grid position, so an agent fills a spreadsheet "finding each
  cell by its grid address". At 8ffecc1 the Windows crate reads only
  GridItem/TableItem availability, for the role
  (`ad/crates/windows/src/tree/roles.rs:190-211`), so the Windows numbers
  come later in that stack (inference).
- **winuse.** No GridItem or Grid properties are read
  (`winuse/internal/uia/capture.go:201-225`). A `--skeleton` cuts by depth
  only, with `(+N children)` (`winuse/skills/winuse/references/reading.md:79`).
  A data grid (Excel, a WinForms DataGridView, Explorer's details view)
  prints every cell as an unaddressed line.
- **Mapping.** Add GridRowCount/GridColumnCount (30062/30063) and
  GridItemRow/GridItemColumn (30064/30065) to the cache request; they cost
  no extra round trip. Print a grid's size on its line (`table 120x8`) and
  a cell's position (`r3 c2`). In `--skeleton`, cut a sized grid to one
  line with its size, like the depth cut. Optionally `find --cell 3,2`.
  Measure the token cost with `observe_cost_test.go`.
- **Conflicts.** ADR 0004's line grammar and Codex parity: print positions
  only outside `--full`, as the speed 13 defaults do.
- **Effort.** S–M.

### 3.2 Skill lines worth adding (rank 10)

From the rewritten core skill in PR #667 (`ad@ce6e388/skills/agent-desktop/SKILL.md`):

- "Never read a value through an app's own Copy command: it replaces the
  user's clipboard" (line 194). winuse's clipboard topic suggests
  `clipboard-set` then Ctrl+V for long text
  (`winuse/skills/winuse/references/input.md:208-212`) and says nothing
  about the user's clipboard. Add one line: read values from the tree,
  and when you must use the clipboard, read it first and put it back.
- "Put an action immediately before its event `wait` in the same batch so
  the baseline is captured before the action" (lines 89-91). winuse's
  `wait window` waits for a Window to show, so the race is smaller; still
  worth one line in `batch-and-wait`.
- Issue #534 (open): an agent pressed Escape to close a dropdown and opened
  "Cancel Transaction?". winuse already notes a new Window after
  `press-key` (`winuse/skills/winuse/SKILL.md:192`), but a modal inside a
  Page is not a Window. One line in `input`: prefer clicking a neutral spot
  or the control again over Escape in web apps.
- "Fast path: one observation, then one call per step. These forms cover
  most tasks" (line 59). winuse has "The commands that matter"
  (`winuse/skills/winuse/SKILL.md:149-167`). Covered.
- **Not adopted:** "Never add `--headed` on your own; never switch to
  headed mode to recover" (lines 15-21). winuse's rule is "taking the
  foreground is always your decision ... tell the user first"
  (`winuse/skills/winuse/SKILL.md:169-178`), and its hints give the exact
  `--headed` retry. The two products differ on purpose; no change
  suggested.

### 3.3 Already covered (output)

- **Roles present on a role miss** (`ad/docs/commands/observation.mdx:105`):
  winuse "A role that matches nothing lists the roles the Window shows"
  (`reading.md:98-100`).
- **Drill-down cost.** agent-desktop off macOS re-reads the whole window to
  re-identify a drill root (`ad/docs/skeleton-and-drill-down.mdx:7`).
  winuse's `--root` reads only the subtree from the live Element
  (`winuse/internal/uia/capture.go:126-134`). winuse is ahead.
- **Partial reads kept, with a note** (#372): winuse's partial-tree note
  (`winuse/internal/uia/walk.go:128-130`).
- **Radio state through SelectionItem** (#679): winuse caches
  `SelectionItemIsSelected` for every Element (`capture.go:219`).
- **Disposition on every result** (#666): winuse's three delivery phrases
  and `--json` fields (CONTEXT.md "Delivery").
- **Text tree, not JSON.** agent-desktop's tree is JSON with `path` arrays
  per match (`ad/docs/commands/observation.mdx:85-102`). ADR 0004's grammar
  stays cheaper.

## 4. Performance, robustness and tests

### 4.1 One huge child list must not end the read (rank 3)

- **agent-desktop.** #643 batches a node's children with one
  `TreeScope::Children` cache request, "never for a collection (List,
  ListItem, ComboBox, DataGrid, DataItem, Table, Tree, TreeItem, Document)
  or a node of unknown type, since UI Automation cannot cap how many
  children a cached read returns". Those nodes are walked one child at a
  time under a sibling cap. Their numbers: a WinForms ListBox with 20,000
  items takes 3.5 s and 40 MB on the one-by-one walk; a WinForms panel with
  9,000 buttons (not a collection, so batched) takes 7.9 s batched against
  3.5 s one by one, "because ... the batch cannot be cut short" (PR #643
  body). On `main` the walk enumerates children with first-child and
  next-sibling and stops at `max_siblings` or the deadline
  (`ad/crates/windows/src/tree/walk/walker_enumerate.rs:206-240`). A node
  read only for its count gets its own 25 ms budget, "one such node could
  otherwise consume an entire snapshot budget on a count nobody needs"
  (`walker_enumerate.rs:6-18`).
- **winuse.** Every expanded node is one `BuildUpdatedCache` with
  `TreeScope_Element|Children` and the full property set
  (`winuse/internal/uia/capture.go:77-106,151-196`). The 1000-children cap
  applies after all children came back (`winuse/internal/uia/walk.go:304-308`).
  A chunk that hits the 2 s transaction timeout (`capture.go:11-15`) sets
  `stopped`, which ends the whole walk, not only that node
  (`walk.go:293-298`). The skeleton's count (`countHidden`) fetches every
  child with every property only to count them (`walk.go:344-359`).
- **Risk (inference).** A non-virtualized list with tens of thousands of
  rows (a WinForms ListBox, a big Explorer folder in some views, a log
  viewer) can return a partial tree that stops at the list, with nothing
  after it, and a `--skeleton` can spend seconds on one count. winuse has
  not measured this. agent-desktop's numbers say the batch on a huge
  sibling list is about 2x slower than a capped walk.
- **Mapping.**
  1. Count-only reads: for `countHidden`, build a second cache request with
     no properties and `AutomationElementMode_None`, and give it a short
     budget (agent-desktop: 25 ms); a starved count prints no number.
  2. On `ErrChunkTimedOut` for one node, mark that node truncated and go on
     with its siblings, instead of stopping the walk. Keep the time note.
  3. For collection control types, or a node whose chunk timed out, walk
     its children with a cached TreeWalker
     (`GetFirstChildElementBuildCache`, `GetNextSiblingElementBuildCache`)
     and stop at `MaxChildren` or the deadline.
  4. Measure first with `internal/fixture` (a 20,000-item list and a
     9,000-button panel) and `readbench`-style timing.
- **Effort.** M.

### 4.2 Cache policy by provider (rank 7)

- **agent-desktop.** The walk skips the CacheRequest when the root's
  `ProviderDescription` names only `uiautomationcore.dll` and no layer runs
  in the target's own process: those client-side proxies answer in the
  client, so "a cache request is pure setup"
  (`ad/crates/windows/src/tree/walk/cache.rs:1-58`). Their probes: on
  classic Notepad batching was a pessimization, 0.58x overall (A6-2,
  `ad/probes/windows/FINDINGS.md:109`); on Explorer the cache-building find
  pass was slower but property reads became about 298x faster (A6-1, line
  108); a WinForms MSAA-bridged tree named the same DLL but ran it in the
  target, 840 ms live against 310 ms batched, hence the pid check
  (`cache.rs:60-66`, A24-17).
- **winuse.** Always caches (`winuse/internal/uia/capture.go:77-106`).
- **Mapping.** Small trees only gain a few ms (inference: classic Win32
  windows have tens of Elements). Worth it only if the A/B harness (4.3)
  shows it. Effort S.

### 4.3 A latency A/B harness (rank 6)

- **agent-desktop.** `scripts/perf-baseline-compare.sh` builds HEAD and the
  merge-base, runs a fixture A/B (snapshots, reads, fixture-only actions),
  a deterministic synthetic locator benchmark, and optional read-only probes
  of real apps (snapshot and find only), then writes JSON and an HTML
  report (`ad/scripts/perf-baseline-compare.sh:1-14`). The benchmark
  reports p50 and p95 over 31 runs (`ad/benchmarks/locator-resolution/README.md:1-23`).
  Their perf PRs report interleaved runs with the first discarded and
  min/median/max (#643, #586, #690).
- **winuse.** The Task Suite measures turns and wall time per task
  (`.scratch/speed/issues/09-task-suite-speed-metrics.md`); a few Go
  benchmarks exist (`winuse/internal/page/readbench_test.go`,
  `winuse/internal/helper/opened_watch_test.go`). There is no one-command
  HEAD-vs-base comparison.
- **Mapping.** A script that builds two `winuse.exe` (HEAD, merge-base),
  starts each helper with its own pipe name, and times `get-window-state`,
  `find`, `--skeleton` and fixture Actions on `internal/fixture` windows,
  plus read-only reads of a few real Windows, interleaved, 10+ rounds,
  p50/p95. Speed tickets could then show numbers the same way. Effort M.

### 4.4 Golden fixtures and a virtual clock (rank 9)

- **agent-desktop.** #665 adds golden JSON fixtures for observation,
  inventory, action, lifecycle and wait output, so a contract change fails
  a test. #642 moved the post-action settle-budget tests to the crate's
  virtual clock after a loaded runner flaked on real 50 ms sleeps.
- **winuse.** `--json` tests check shapes in code
  (`winuse/internal/cli/json_test.go`); tree fixtures exist
  (`winuse/internal/wstate/testdata/*.txt`). Walk and waits take injectable
  clocks or sleeps (`winuse/internal/uia/walk.go:79`,
  `winuse/internal/helper/waits.go:153-196`).
- **Mapping.** Golden files for the `--json` line of each outcome and for
  `--observe` diffs. Check that every settle and lane test (ADR 0019, ADR
  0014's ~12 s lane wait) uses a fake clock. Effort S.

### 4.5 Already covered (robustness)

- **AutoSetFocus FALSE** (agent-desktop #613, open, closes #567): winuse
  since ADR 0010 (`winuse/internal/uia/thread.go:83-88`).
- **Expand-only button click** (#690: Chromium `popovertarget`,
  `<summary>`, WPF): winuse's click route tries ExpandCollapse before the
  Legacy default action (`winuse/internal/uia/headless.go:10-17`).
- **Launch behind the user's windows** (#615): winuse launches with
  `SW_SHOWNOACTIVATE` (`winuse/internal/apps/launch.go:24-25`).
- **Lease contention says nothing was sent** (#648 open): winuse's lane
  refusal says so (CONTEXT.md "Lane").
- **UIA activation fallback** (#651): a UIA client with AutoSetFocus on
  activates a window when `SetForegroundWindow` is refused (0/7 to 7/7 on
  their Notepad). winuse uses the input-event and AttachThreadInput route
  and verifies (`winuse/internal/win/activate.go`). Keep in mind as a third
  rung only if activation failures show up.
- **Transaction timeouts per chunk** and a 1 s connection timeout
  (`winuse/internal/uia/capture.go:151-159`, `thread.go:82`).

## Pitfalls from their issue tracker worth a check in winuse

Open agent-desktop issues on Windows that point at cases winuse may share
(inference: not tested in winuse):

- **#617** A ProseMirror composer in an Electron app is a UIA `Group` with
  TextPattern and only SetFocus/ScrollItem, no ValuePattern. Check how
  winuse prints it and whether `type-text --into` reaches it without a
  Browser Connection.
- **#611** (closed as documented) The common file dialog's "File name"
  combo accepts `SetValue` but the value never reaches its child Edit.
  Check `set-value` on that combo in the Save As Task Suite case.
- **#647** `wait --text` missed deep Chromium text that a snapshot showed,
  because the wait's walk had a smaller budget. Check that winuse's text
  wait reads with the same caps as `get-window-state`.
- **#569** An elevated window read from medium integrity came back empty
  and "complete". winuse refuses `admin` Windows by name
  (`winuse/skills/winuse/SKILL.md:69`); covered.
- **#479** Word's document body is not exposed. Worth a Task Suite note if
  Word tasks are added.

## Not recommended

- **Snapshot-qualified, persisted refs and the capture-rule identity**
  (#442, #683). PR #683 says "about one ref in three was refused as
  ambiguous on Windows and Qt" before it. winuse's Element
  Index plus live `name:`/`id:` avoids that (ADR 0004, ADR 0021).
- **A sandbox command.** Their provider is macOS VMs only
  (`ad/skills/agent-desktop/SKILL.md:75-77`); winuse runs on the user's
  desktop by design (CONTEXT.md first line).
- **Turning the Agent Cursor off by default.** ADR 0018 records the user's
  choice; 2.3 lists the cheaper options.
