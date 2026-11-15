# 08: One huge list must not end the tree read

Status: ready-for-human
Type: task
Branch: learn-latency-and-lists (worktree `.claude/worktrees/agent-a6c96d894f7174ed3`), off main 03fc3a4
From: `.scratch/learn-from/agent-desktop.md` §4.1 (rank 5 in `README.md`; agent-desktop #643)

## Ask

- Check §4.1's two claims against the code: one chunk that times out (2 s) stops the whole walk, and `--skeleton` fetches every child with every cached property just to count them.
- If real: a slow or huge child list (20,000 list items, a panel of 9,000 buttons) is cut with a clear note ("N more items not read") and the walk goes on with the siblings after it, within `WINUSE_TREE_BUDGET`.
- Skeleton counts take the cheapest route (a minimal cache request, or a Grid/Table row count), not a full property fetch.
- Keep ADR 0004's format; amend it if the notes change. Unit tests with fakes; a micro-benchmark if it fits.

## What was built

**Both claims were real.** `walk.go` `expand`: `errors.Is(err, ErrChunkTimedOut)` set `stopped`, which cut every remaining sibling at every level. `countHidden` called `src.Children`, the same `BuildUpdatedCache` with `TreeScope_Element|Children` and all 22 tree properties, and a timed-out count also set `stopped`. The 1000-children cap applied only after every child came back.

- **A timed-out chunk cuts that node only** (`internal/uia/walk.go`, `slowChildren`). When the walk's own budget is not spent, the node's children are read one at a time through a new optional `ChildStepper` interface and the walk then goes on with the node's siblings. Each step is bounded like a chunk (min(remaining, 2 s)), and stepping stops at `MaxChildren` (1000), at the element cap or at the walk's budget; the last two still stop the walk, as before. A list whose children all fit under the cap is read whole with no note.
- **A provider that does not answer even the first step stops the walk** with the time-limit note, as a timed-out chunk always did. That tells a hung App apart from a long list, so a hung App's read still ends at about 4 s (2 s chunk, 2 s step) instead of spending the 8 s budget sibling by sibling.
- **The COM side** (`internal/uia/capture.go`): `comSource` implements `FirstChild`/`NextSibling` with the control-view tree walker (`get_ControlViewWalker`, `GetFirstChildElementBuildCache`, `GetNextSiblingElementBuildCache`) and a second cache request holding the tree properties for the child alone (`TreeScope_Element`, mode Full, so the stepped Elements are live like any other). `ItemsAfter` reads the last child's live SizeOfSet minus PositionInSet for the note's count. Slot numbers are in `com.go` with the SDK order.
- **Notes** (no grammar change, ADR 0004 amended). A cut slow list joins the partial-tree note, after the other reasons:
  ```
  Note: this Window State is partial: the children of Element 1 were too slow to read at once, so the first 1000 were read one at a time and 19000 more items were not read
  ```
  "and the rest were not read" when the provider gives no set size; "the children of Element 1 did not answer in time and were not read" when nothing could be stepped (a source without a stepper, or a step that failed on the first child for a reason other than a timeout). The line gets no `(truncated: ...)` suffix: ADR 0004 keeps that for exact child counts, and SizeOfSet counts items, not children. `find` and `--root` rebuild the note through `WalkResult.NotesWith`, naming the list by the Element Index they added it as, or by role and name (`the list "Items"`) when they did not print it, since walk positions are not the Agent's indexes there.
- **Skeleton counts read nothing per child** (`ChildCounter`, `comSource.CountChildren`): a third cache request with no properties and AutomationElementMode None, bounded to `countBudget` (250 ms) per count. A count that runs out of time prints no number, the walk goes on, and a separate note (not the partial-tree note, so the Chromium settle logic does not treat the skeleton as cut) says `the children of Element 12 were not counted in time`. A source without a counter (the test fakes, `helper`'s `frameSource`) still counts by reading and releasing.
- **Grid/Table row counts were not used**: RowCount is not the child count (header rows, groups, a scroll bar) and would add a cached property to every Element of every read. Collection types are not stepped up front either (agent-desktop's #643 does): stepping costs one round trip per child, which every ordinary list would pay. Both are worth a second look only if the live numbers ask for them.
- **A Document cut** (ADR 0012) also forgets the slow lists and uncounted nodes found under the cut Document, like its other notes.
- **The fixture** (`internal/fixture/hugelist.go`, `-huge-list N`, `State.HugeListWindowID`): a top-level Window with a classic LISTBOX of N items and an "After the list" button after it, shown without activation beside the main window. It is the system's own list box, read through UI Automation's proxy. The 9,000-button panel was not built: 9,000 child HWNDs come close to the 10,000 USER handles a process may hold.
- **Docs:** ADR 0004 amendment, one sentence in `docs/codex-parity.md` and in the skill's `reading` topic, the e2e README section.

## Measurements

No live measurement: reads run only by a human (below). Offline, the walk's own Go cost on a 20,000-item list (`go test ./internal/uia -run XXX -bench HugeList -benchmem`, i9-12900HK):

| Route | ns/op | B/op | allocs |
|---|---|---|---|
| one chunk, cut at 1000 (`BenchmarkWalkHugeListBatched`) | 2,050,879 | 3,042,240 | 45 |
| timed-out chunk, 1000 steps (`BenchmarkWalkHugeListStepped`) | 690,797 | 625,015 | 38 |

Both are noise next to the cross-process time (agent-desktop measured 3.5 s one by one against 7.9 s batched for 9,000 buttons; their ListBox of 20,000 items took 3.5 s). Whether UI Automation's transaction timeout fires at all for the list box proxy, which runs in the client, is unknown: if it does not, the chunk runs until the request's 10 s budget abandons the worker, as before this change. The live check answers it.

## Tests

- `internal/uia/slowlist_test.go` (fakes, no COM): a timed-out list read one at a time and cut at the cap with the exact note, the proof child released and no suffix; no set size ("the rest"); a slow list under the cap read whole with no note; a first step that times out stops the walk; a step failure mid-list cuts the rest and goes on; stepping keeps to the time budget and the element cap; two slow lists in one note and `NotesWith` relabelling; a late Document cut forgets a slow list under it; skeleton counts through `ChildCounter` read no child, are bounded by `countBudget`, and a timed-out count leaves no number, a note and the walk going on (or stops it past the budget); `addedLabel` for find and region reads.
- `internal/uia/walk_test.go`: `TestWalkChunkTimeoutStopsWalk` became `TestWalkChunkTimeoutCutsOnlyThatNode` (the siblings are read, the note names Element 1); `TestWalkChunkTimeoutPastTheBudgetStops` keeps the old stop when the budget is spent.
- `internal/uia/region_test.go`: the skeleton test without a counter still counts by reading and releasing.
- `internal/fixture/elements_test.go`: `hugeListWindowId` in the state JSON.
- `test/e2e/huge_list_test.go` (user-run): `TestHugeListDoesNotEndTheRead`.
- Benchmarks: `BenchmarkWalkHugeListBatched`, `BenchmarkWalkHugeListStepped`.

`go vet ./...`, `go vet -tags e2e ./test/e2e` and `go test ./...` pass.

## Live check (to run by a human)

These start the build's own helper, which makes an installed helper of another build exit (its next command starts its own again).

1. The e2e test, on the fixture's 20,000-item list box. Read the `MEASURED:` lines: which route ran (the `(truncated: 1000, omitted 19000 children)` suffix means the list came back as one chunk; the slow-list note means it timed out and was stepped), and how long each read took. Both reads must reach "After the list".
   ```
   set WINUSE_E2E=1
   go test -tags e2e -v -run "TestHugeListDoesNotEndTheRead$" ./test/e2e
   ```
   If the read fails with a request timeout instead, the transaction timeout does not reach the list box proxy, and stepping collection types up front is the next step.
2. The latency A/B (issue 07) on the fixture, HEAD against main's merge-base, with this change applied, read-only. The huge-list operations are the ones this item changes; the others should not move.
   ```
   go run ./cmd/winusetasks latency --i-am-the-user --huge-list 20000
   ```
3. By hand, read-only, on a real long list: an Explorer folder with a few thousand files in Details view, and `get-window-state <window> --skeleton` on Settings. Check that the read reaches the Elements after the list and that the skeleton prints its counts.

## Comments

### Live run, 2026-11-15

`go run ./cmd/winusetasks latency --i-am-the-user --huge-list 20000`, A = merge-base 03fc3a4, B = HEAD c3d93c6, 30 timed calls per operation and build (ABBA):

```
Latency A/B
  A: merge-base of HEAD and main (03fc3a44b5) (build 1f2euSr3YXHq)
  B: HEAD (c3d93c6b2a) (build nkHgtvBnoCmt)
30 timed calls per operation and build (6 rounds of 5, ABBA, after 1 untimed pass(es) per block).
Wall time of each winuse call, process start included. * = the p25-p75 ranges do not overlap.

operation                   A p50    A p95    B p50    B p95    p50 change            p95 change          failed A/B  
list-windows                37 ms    52 ms    35 ms    37 ms    -3 ms (-6.7%) *       -15 ms (-28.2%)     0/0         
fixture get-window-state    905 ms   1128 ms  65 ms    92 ms    -840 ms (-92.8%) *    -1036 ms (-91.8%)   1/0         
fixture --skeleton          66 ms    77 ms    65 ms    83 ms    -2 ms (-2.5%)         +7 ms (+8.5%)       0/0         
fixture find --role button  66 ms    80 ms    65 ms    82 ms    -1 ms (-1.3%)         +2 ms (+2.3%)       0/0         
huge list get-window-state  2242 ms  2255 ms  5593 ms  5844 ms  +3351 ms (+149.4%) *  +3589 ms (+159.2%)  0/0         
huge list --skeleton        -        -        5598 ms  5885 ms  -                     -                   30/0
```

- **The fix works.** On A a full read of the 20,000-item list box stops at the 2 s chunk timeout and prints nothing after the list (2.2 s); on B it steps the first 1000 items and reaches the button after it (5.6 s). `--skeleton` of that Window failed on A in 30 of 30 calls (`window is gone: ... HRESULT 0x80131505`, UIA_E_TIMEOUT: the count fetched every child with every property); on B all 30 worked (5.6 s).
- **The App's other Window reads 14 times faster on B** (fixture main Window 905 to 65 ms p50; one A call failed with the same timeout). On A the abandoned 20,000-item request keeps the fixture's UI thread busy, so its next read waits; B abandons nothing.
- **Cost:** B's huge-list read is 3.4 s slower than A's cut-short one: 2 s of chunk timeout plus about 3.5 ms per stepped item. A lower cap for a stepped list (for example 200 items) would bring it near 2.7 s; agents read a long list through `find` or `--root` anyway. Not changed: a decision for the issue's owner.
- Ordinary reads did not move: list-windows, the fixture's `--skeleton` and `find` are within noise.
