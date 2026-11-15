# 07: A latency A/B of two winuse builds

Status: ready-for-human
Type: task
Branch: learn-latency-and-lists (worktree `.claude/worktrees/agent-a6c96d894f7174ed3`), off main 03fc3a4
From: `.scratch/learn-from/agent-desktop.md` §4.3 (rank 6 in `README.md`; agent-desktop's `scripts/perf-baseline-compare.sh`)

## Ask

- One command that builds two revisions (HEAD and the merge-base with `main`, or two given refs) into separate temp folders and times the same read-only operations on each: reads of the fixture app, and optional read-only reads of real Apps the user names (Notepad, Settings, Explorer).
- p50 and p95 per operation, and the change.
- Run each revision's helper in turn and restore cleanly. Starting a build's helper makes an installed helper of another build exit: document it, and say so before it starts.
- Fit the repo's tooling (`internal/tasksuite`, speed issue 09, `test/e2e`); Go rather than a script.

## What was built

```
winusetasks latency --i-am-the-user [--a <ref|tree|exe>] [--b <ref|tree|exe>] [--a-label <name>] [--b-label <name>]
                    [--window <id|app>]... [--huge-list <n>] [--no-fixture]
                    [--rounds 6] [--per-block 5] [--warmup 1] [--out <dir>]
winusetasks latency-report <results.json>
```

- **Where:** a `latency` command of `winusetasks` (`cmd/winusetasks/latency.go`), beside the Task Suite's A/B, with the same `--i-am-the-user` gate and its own refusal text. The logic is a new package, `internal/latency`: `latency.go` (plan, ABBA schedule, statistics, report, results file; touches nothing), `build.go` (the arms), `live.go` (the processes), `run.go` (the run).
- **The arms.** `--a`/`--b` take a git ref, a source tree (`.` is the working tree with its uncommitted changes) or a winuse.exe, as the Task Suite's `--winuse` does. Defaults: A is `git merge-base HEAD main`, B is `HEAD` as committed. A ref is resolved with `rev-parse --verify <ref>^{commit}`, exported with `git archive --format=zip` into `<out>\build\<label>\src` and built there (`CGO_ENABLED=0 go build ./cmd/winuse`): the repository, its index and its worktrees are never touched. Each arm's build id (`version.ExeBuild`) is printed; two arms of one build get a warning (they would share one helper and measure noise).
- **The calls** (`latency.Plan`), all reads: `list-windows`; on the fixture's main Window `get-window-state`, `get-window-state --skeleton` and `find --role button --count`; with `--huge-list N` the first two on the fixture's huge-list Window (issue 08); and the same three on each `--window` (a Window ID, or an App name with one Window). No Action, key, screenshot or launch: the user opens the real Apps first. The fixture is built from this tree (it needs `-huge-list`) and started with a state file in the build folder.
- **The helper, in blocks.** One helper serves the session and a command of another build makes it exit (desktop-parity 25), so calls cannot alternate between builds one by one: every call would restart the helper. Each round runs one block per build, A then B, then B then A (ABBA). A block opens with an untimed `list-windows` (the other build's helper exits, this build's starts), then `--warmup` untimed passes, then `--per-block` timed passes over every call. The defaults (6 rounds of 5, 1 warm-up pass) time each call 30 times per build, as agent-desktop's 31. The calls run as `WINUSE_AGENT=latency-ab` with `WINUSE_HELPER_IDLE=60s`, and each is bounded at 60 s.
- **Said before it starts** (`latency.Notice`, printed before any build): the running helper (its pid and path, when one runs) exits at the A/B's first command, so agents using winuse lose their Window States; every switch restarts the helper; at the end the A/B stops its own helper and the installed winuse starts its own on its next command; the fixture's window appears and may take the foreground; which Windows it reads; leave the machine idle.
- **Restoring.** However the run ends (done, an error, Ctrl+C through `signal.NotifyContext`), it closes the fixture, stops the running helper only when its image path lies under the run's build folder (`StopHelperUnder`; both paths go through `GetLongPathName`, since a temp folder can come back as `C:\Users\HPMEDI~1\...` while an image path never does), and removes the builds. A helper of any other winuse is never ended. Results stay in `--out` (default: a new `%TEMP%\winuse-latency-*` folder).
- **The report** (`latency.Report`, also `summary.txt`): per call, p50 and p95 (nearest rank) on each build, B's change in ms and %, the failed calls per side (a failure is left out of the times and its first stderr line kept in `results.json`), and a `*` where the two builds' p25-p75 ranges do not overlap. The time is the wall time of each winuse.exe call, process start included (the same on both sides; it is what an agent waits for). `results.json` holds every sample with its round and exit code; `latency-report` prints the table again offline.
- **Docs:** a "latency A/B" section in `tasksuite/README.md`, the `winusetasks` usage and examples.

Not done: agent-desktop's deterministic synthetic locator benchmark and HTML report (the table and JSON are enough here), timing Actions on the fixture (the ask was read-only), and helper-side timings per phase (the A/B times what the agent waits for; `WINUSE_TRACE` stays the tool for a breakdown).

## Measurements

None live (the A/B is user-run). Offline, a throwaway build-only check (deleted after) ran `PrepareArm` on the merge-base 03fc3a4, `HEAD` and this tree, plus `BuildFixture`, into a temp folder without running anything: 32 s cold, 16 s warm; three distinct build ids (`iYaduU1oZ9t4`, `s4ATKCHQOENH`, `A7UBEIxMbR3u`), so the arms do restart each other's helper. That check is what showed the temp folder's 8.3 short name.

## Tests

- `internal/latency/latency_test.go` (no process, helper or fixture): the plan (names, arguments, every call a read, no screenshot); ABBA order; `Measure` with a fake caller (each block opens with `list-windows` on its own build, blocks A B B A, warm-up passes untimed, sample counts and times, progress lines); failures counted apart with their line; a caller error ends the A/B; a cancelled context stops between calls with the samples so far; nearest-rank percentiles; stats leave failures out; the report's lines (p50/p95, change, `*`, `-` with no samples, failures); results.json round trip and a Task Suite file refused; the notice's text with and without a running helper and the fixture; the source kinds; an exe arm copied; a bogus ref refused and this repository's merge-base read (read-only git); zip export refusing an entry outside its folder; `under` and `longPath` on an 8.3 temp folder.
- `cmd/winusetasks/main_test.go`: the refusal without `--i-am-the-user`; usage errors checked on the parser only (with the user's start among the arguments, so a case that parsed could still never start a run; a flag is never taken as a value); the parsed options and defaults; `latency-report`.

`go vet ./...`, `go vet -tags e2e ./test/e2e` and `go test ./...` pass.

## Live check (to run by a human)

The A/B replaces the running winuse helper: quit other winuse users first (their next command starts their helper again). Leave the mouse and keyboard alone while it runs (a few minutes; longer with `--huge-list`). From the repo root, on this branch:

1. The default A/B, fixture only (merge-base against HEAD, with issue 08 applied in HEAD):
   ```
   go run ./cmd/winusetasks latency --i-am-the-user
   ```
   Check: the notice prints before any build; both arms build; the table shows every call with 30 samples per side and no failures; at the end "Stopped the A/B's helper", the fixture window is gone, `<out>\build` is gone, and `winuse status` (the installed one) starts or reports its own helper. The fixture calls should not move much: issue 08 changes only slow lists and skeleton counts.
2. Issue 08's lists, and real Apps (open Notepad and one Explorer window first):
   ```
   go run ./cmd/winusetasks latency --i-am-the-user --huge-list 20000 --window notepad --window explorer
   ```
   Check the `huge list` rows: on the base, a read that hits the 2 s chunk timeout stops at the list; on HEAD it reaches "After the list". If `get-window-state` of the huge list fails on either side (exit 1, see `results.json`), note the error line. Also try `--window` with Settings' Window ID from `winuse list-windows`.
3. An A/A run to see the noise: `--a HEAD --b .` with a clean tree warns that both arms are one build; instead build the same commit twice with different paths, e.g. `--a HEAD --b <path to another worktree of the same commit>`, and note the spread of the changes.
4. Press Ctrl+C during a run: it must still stop its helper, close the fixture and remove the builds, and write the partial table.

## Comments
