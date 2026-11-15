# Handoff: cold Chromium settle, and where the reliability council left off

Written 2026-11-08 by the orchestrator of the reliability council (the Zeron chat "Implement reliability-council tickets 01-13", now archived). The chat "Agent Desktop Performance Comparison" that started the council is archived too. From here on, one orchestrator reports straight to the user. The user wants plain-language updates without jargon.

## State of the repo

- Local `main` = GitHub `main` at 9bf5abf, plus this handoff's commit, which is local only (not pushed).
- Both GitHub workflows pass on 9bf5abf: `ci` (vet, offline tests, `CGO_ENABLED=0` build) and `e2e` (the 154 fixture-only real-app tests on the hosted `windows-latest` runner, about 5 minutes).
- The reliability council (`.scratch/reliability-council/`) is closed. Every ticket has a Status and a `## Comments` note:
  - Done: 01, 02, 03, 04, 05, 06, 07, 08 (parts 1-3: CI, the e2e lane, `test/e2e/ledger.md`), 10, 11, 12 (fold before the cut).
  - 09: part 1 done (identity re-check right before a write). Part 2 (per-agent records) is wontfix (user): ADR 0014's shared records stay.
  - 13: wontfix (user: LibreOffice not installed).
  - Extra, user-asked: `set-value` refuses a value that ends with winuse flags (`--observe`, `--headed`, `--no-wait`, `--timeout` ...); a leading `--` keeps such text.
- The old `perf-catch-up` branch was deleted (local only; its tip was e43e358).

## The open question: should winuse wait longer for a cold Chromium/Electron App?

### What happened

On 2026-11-07 the user's Discord (maximized on the second monitor, on top, not covered, no Browser Connection) was read cold. The default read settled for about 5.8 s, came back with the frame only (7 nested unnamed panes), and printed ADR 0005's note: "the page's accessibility tree is still thin; Chromium may not build it while the Window is fully covered ...". The Window was not covered. A read a few minutes later had the whole tree (784 Elements). So the 5 s settle gave up before Discord had built its tree, and the note's "covered" guess misled.

### How winuse settles today (ADR 0005, amendment of 2026-11-02)

- A Chromium Window whose web content is thin (fewer than 3 Elements under its Documents, or no Document) is read again every 300 ms until the Documents grow and then stop growing, up to `DefaultChromiumSettle` (5 s), bounded by the request deadline.
- A Window whose settle ended thin is remembered (by HWND). Its later thin reads get one 500 ms retry, not the loop.
- `--timeout` from desktop-parity 14 can widen the settle budget, but nothing tells the agent so.
- The constants and the note are pinned by `internal/uia/retry_test.go`. ADR 0005 says none of them was measured live.

### How the others do it (from the earlier research notes, not re-checked)

- agent-desktop re-reads with backoff until the renderer's lazy accessibility build settles. Its probe measured a cold Chromium taking 10-25 s (`.scratch/browser-merge/agent-desktop-research.md`; ADR 0005's amendment cites the same 10-25 s from winuse's own laptop probe).
- cua: no settle found in our notes. It retries only a failed read (E_FAIL) and adds a screenshot to every read (`.scratch/perf/cua-research.md`).

### Options put to the user

1. Keep 5 s and fix the note: drop the "covered" guess, say the App may still be building its page, and tell the agent to read again in a few seconds or pass `--timeout 20`. Cheap: one message and its test. (The orchestrator's suggestion.)
2. Raise `DefaultChromiumSettle` (to something like 15-20 s). Cost: a Window that is legitimately nearly empty (Chrome's popups, a blank tab, a canvas-only App) pays the full budget on its first read.

The user asked "is it reliable tho?" and wants to measure first, across the Electron Apps on this PC.

### The measurement the user said "ok" to (not run yet)

Confirm with the user before running it: when (they'd rather be away from the PC), and whether to restart Discord, Docker Desktop and Steam for a fair cold read. The user said "ok, lets handoff first" before giving either answer.

Plan:

- Build winuse from `main`. For each App below that is not running: start it with `winuse launch-app`. Read it every 0.5 s for up to 30 s, with `get-window-state <window> --timeout <n>` or a small script around the CLI. Record:
  - the time until its tree is built (Documents with more than a handful of Elements, and stable);
  - whether today's 5 s settle would have caught it;
  - what the note said.
- Then close the App (`close-window`, or `close-app`). No clicks or typing inside the Apps. A login screen is fine: it is still a page tree.
- Leave the user's running Apps alone unless they agree to restarts.
- Use only the user's PC, and only with their go-ahead. Never run the e2e suite here (`test/e2e/doc.go`).
- Report one table (App, engine, cold build time, caught by 5 s, note shown), then recommend 1 or 2 with the numbers.

Chromium-based Apps found on this PC (install folders only, 2026-11-08):

| Kind | App | Where | Running on 2026-11-08 |
|---|---|---|---|
| Electron | Discord | `%LOCALAPPDATA%\Discord\app-1.0.9267` | yes |
| Electron | Docker Desktop | `C:\Program Files\Docker\Docker\frontend` | yes |
| Electron | Termius | `%LOCALAPPDATA%\Programs\Termius` | no |
| Electron | Antigravity | `%LOCALAPPDATA%\Programs\antigravity` | no |
| Electron | Pen | `%LOCALAPPDATA%\Programs\Pen` | no |
| Chromium (likely Electron) | Ask | `%LOCALAPPDATA%\Programs\Ask` | no |
| Chromium (likely Electron) | RevoltG | `C:\Program Files\RevoltG` | no |
| CEF | Steam | `C:\Program Files (x86)\Steam` | yes (minimized) |
| CEF | NVIDIA App | `C:\Program Files\NVIDIA Corporation\NVIDIA App` | no |
| CEF | EA Desktop | `C:\Program Files\Electronic Arts\EA Desktop` | no |
| Browser | Brave | (winuse reads browsers through a Browser Connection) | yes |

VS Code is not installed. Zeron and Zed are not Chromium.

## Ideas noted and not acted on (for the user to pick from)

- A wait-text poll that treats an unreadable IsPassword like the tree read does (secret).
- Mark more refusals "nothing sent" at their source (missing Window, locked desktop, minimized, the start-of-command identity check, Headed name/disabled refusals); Headed Actions could then get 06's brief wait.
- Other root reads that still call any failure "Window gone": `accelerators.go`, `tabstrip.go`. PeekTree, find and the live `name:` lookup could retry like Capture.
- `find --state password`. `--out`'s "N interactive" count leaves out disabled Elements. The `--interactive` help line.
- In a batch step that starts with `in <window>`, `--headed` / `--no-wait` at the end of a set-value or type-text value is still taken as a flag. `set-value 101 --observe --headed 3 v` makes `--headed` the Element. `type-text` could get the trailing-flag refusal too, but that changes behaviour its tests expect.
- A test guard env var so a CLI unit test can never start a helper.
- CI polish: release.yml reads the Go version from go.mod, a concurrency group, a gofmt check (`internal/helper/pagecut_test.go` is not gofmt-clean), `-race` on the runner, a job timeout on `ci`.
- Live measurements still owed: the 2 s worker transaction timeout (01), 06's 1.8 s brief wait, how often a failed root read clears on its own (04), the warm-up call in `workerInit` (01, optional).
- From the council's spec ("Ideas beyond the question"): trace a process-start stall, RuntimeId on the cache request, persist identity records across the helper's idle exit, a completion journal for an uncertain delivery, completeness fields in `--json`, privacy canaries.

## Housekeeping left

- Local branches `rc-*` (14 of them) are all merged into `main`; delete them when convenient.
- Empty worktree folders under `..\winuse-rc\` (rc-03, rc-04, rc-06, rc-09, rc-setvalue-trailing-flags) could not be removed while their subagent sessions held them; delete them once those sessions are closed (`git worktree prune` already ran).
- The Discord reads from the measurement held the user's chat. They were only in the session scratchpad, never committed, and were deleted at handoff.
