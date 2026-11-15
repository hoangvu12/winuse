# 09: Shorter Agent Cursor glide inside Batches

Status: ready-for-human
Type: task
Branch: learn-batch-glide (worktree `.claude/worktrees/agent-ac0f952110de410dd`), off main 03fc3a4
From: `.scratch/learn-from/agent-desktop.md` §2.3 (rank 8) and `.scratch/learn-from/README.md` "Needs a decision"; the user's decision: keep the glide for a single Action, shorten it for the steps of a Batch

## Ask

- Each Headless Action glides the Agent Cursor for at least 600 ms (ADR 0018 Decision 1) and is delivered at the arrival. A long Batch adds that up (agent-desktop measured about 355 ms per Action for its overlay).
- Keep the glide for a single Action. Shorten it for a Batch's steps (from the second step, or for all; decide and justify), so the user still sees where the agent acts.
- Pick the duration from the motion code and ADR 0018's reasoning: still a glide to the target, not a teleport.
- The Task Suite's unattended runs follow the same Batch rule unless the code already tells them apart.
- An amendment to ADR 0018. Unit tests with fake clocks.

## What was built

- **The rule** (`internal/helper/cursors.go`, `glideFor`): `batch` marks its context (`withBatchGlides`). The first glide in a Batch is the full one; every later glide in the same Batch is `win.GlideBatch`. A single Action, and the next Batch, start full again.
  - **Why the first glide stays full:** the cursor may be hidden (it starts from the Window's centre) or far away, and the user's eye has to find it. That was what the 600 ms floor was for. Once the user is watching, a shorter glide to the next Element is still easy to follow.
  - **Counts glides, not steps:** a first step with nothing to point at (a `get-window-state` or `find` step, a key with no target) leaves the full glide to the first step that glides.
  - All travels go through `cursorTravel` / `cursorTravelRect` (`internal/helper/headless.go`), so Headless, posted and Page steps all follow the rule. A plain `Cursor` without `Glide` (offline fakes) still travels in full.
- **The duration** (`internal/win/cursor_motion.go`, `cursorGlideDuration`): a `GlideBatch` takes `cursorBatchGlideScale` (0.5) of the full glide, at least `cursorBatchGlideFloor` (300 ms, `cursorFittsMin`): **300–550 ms** instead of 600–1100 ms. The path, the in-out sine, the follow-through and the lean are unchanged (`newCursorGlide` only changes `dur`), so it is the same motion played twice as fast. A hop under 2 px is still nothing, and reduced motion is still the straight 120 ms move.
  - **Why half, at least 300 ms:** 300 ms is cua's own shortest Fitts time and 2.5 times the 120 ms reduced-motion move (which reads as a jump). The 150–400 ms glide winuse had before ADR 0018 read as a jump in the GIF, but it was straight and minimum-jerk; this one keeps cua's arc, lean and follow-through. cua's `relaxed` floor (450 ms) was the other candidate: it would save only about 110 ms per short hop.
- **The API:** `win.Glide` (`GlideFull`, `GlideBatch`); `AgentCursor.GlideAs` and `AgentCursor.Glide` (`TravelAs` and `Travel` are the full glide); the controller's `cursorUI.glide`. `travelBound` takes the glide, so a stuck UI thread holds a Batch step back at most about twice the shorter glide.
- **Task Suite:** `internal/tasksuite` has no cursor setting of its own and runs the same helper, so unattended runs follow the same Batch rule. No switch was added.
- **Docs:** ADR 0018 Decision 1 amended, its Status and Consequences noted. CONTEXT.md is unchanged: the Agent Cursor still glides along a curve and the Action is delivered at its arrival. The skill and help are unchanged: agents control nothing new.

## Measurements

From `newCursorGlide` at scale 1, moves from (100, 500) to the right (computed offline with the real code; the Action is delivered at the arrival, about 75% of the duration):

| Move | Full: duration / arrival | Batch: duration / arrival | Saved per step |
|---|---|---|---|
| 60 px to a 24 px target | 600 / 437 ms | 300 / 219 ms | 218 ms |
| 150 px to 30 px | 600 / 446 ms | 300 / 223 ms | 223 ms |
| 300 px to 24 px | 661 / 495 ms | 330 / 248 ms | 247 ms |
| 400 px to 40 px | 622 / 466 ms | 311 / 233 ms | 233 ms |
| 800 px to 24 px | 838 / 644 ms | 419 / 322 ms | 322 ms |
| 1500 px to 24 px | 956 / 747 ms | 478 / 374 ms | 373 ms |

Steps of one Batch usually point within one Window, so short hops (about 230 ms saved) are the common case:

- **5-step Batch:** 4 shorter glides, about **0.9 s** saved (about 2.25 s of glides becomes about 1.35 s).
- **10-step Batch:** 9 shorter glides, about **2.1 s** saved (about 4.5 s becomes about 2.4 s).

agent-desktop.md's "a ten-step Batch spends about 6 s in glides" counts the whole 600 ms. The Action is delivered at the arrival, so the wait is closer to 4.5 s.

## Tests

- `internal/win/cursor_test.go`:
  - `TestABatchGlideIsTheFullOneInHalfTheTime`: `GlideFull` is the full glide; `GlideBatch` is `max(full/2, 300 ms)`, within 300–550 ms, over distances and target sizes; a hop under 2 px and reduced motion are unchanged.
  - `TestABatchGlideKeepsThePath`: the same point at the same fraction of the travel, the arrival at the same fraction, a visible lean mid-way, upright at the arrival.
  - `TestGlideAsReleasesAtTheShorterArrival`: the controller plays the shorter duration and releases the waiter at its arrival (fake clock); the wait's bound is shorter than the full glide's.
  - `travelBound` callers take the glide.
- `internal/helper/cursors_test.go`:
  - `TestABatchGlidesInFullOnceThenShorter`: a 3-step Headless Batch glides full, Batch, Batch; a single click after it is full; the next Batch starts full again.
  - `TestTheBatchRuleCountsGlidesNotSteps`: `glideFor` outside a Batch, in one, in a new one; a plain `Cursor` still travels.

`go vet ./...`, `go vet -tags e2e ./test/e2e` and `go test ./...` pass.

## Live check (to run by a human)

These start the build's own helper, which makes an installed helper of another build exit.

- **Watch a 5-step Batch on the fixture:** start the fixture app, then e.g. `winuse batch <fixture> click <i1> then click <i2> then set-value <i3> x then perform-secondary-action <i4> Toggle then click <i5>`. Expect: the first glide as before (about 0.6 s); the next four visibly glide along the same arc to each Element, about twice as fast, not jumping. Judge whether 300 ms still reads as a glide. If not, raise `cursorBatchGlideScale` or `cursorBatchGlideFloor` (one constant each).
- The same steps as five separate commands: each glides in full.
- A named Agent (`WINUSE_AGENT=reviewer`) doing the same Batch: its own cursor follows the same rule.
- A Batch whose first step is `get-window-state`: the first click still glides in full.

## Comments
