# 02: Wait briefly before refusing

Status: wontfix (superseded in part)
Type: task
Branch: learn-wait-before-refusing (worktree `.claude/worktrees/agent-aab35627364444bcf`), off main 03fc3a4
From: `.scratch/learn-from/README.md`, rank 2 (agent-desktop §2.2 bounded auto-wait, agent-browser §4.1 click point settle and re-centre)

## Ask

- Native and Page targets: before refusing, poll a short, bounded time for a disabled target to become enabled, or a `name:`/`id:` reference to resolve. Pick the bound against winuse's budgets and Lanes (ADR 0014's ~12 s lane wait) and justify it.
- Element Indexes stay index-based: an index never re-resolves to another Element.
- Refusals still say "nothing was sent" (Delivery). Esc stop interrupts the wait.
- Page click point: after scrolling an Element into view, wait for its box to stop moving (a couple of animation frames); if it is still off-screen or covered, re-centre once before refusing.
- An opt-out flag is optional; keep the default bounded and short.
- Help, the skill (agents no longer need a `wait` before acting on something about to appear), CONTEXT.md if a term changes.

## What was built

- **The wait** (`internal/helper/targetwait.go`): after a first try refused for a transient reason, the try runs again every 200 ms for up to **2 s** (`targetWait`), then the usual refusal with `, still after waiting 2s` before `; nothing was sent`:

  ```
  the Element is disabled, still after waiting 2s; nothing was sent
  no element named "Save" in window 101, in its cached Window State or a fresh read, still after waiting 2s; nothing was sent
  element 7 cannot be clicked: it is disabled, still after waiting 2s; nothing was sent
  ```

  Transient: a `name:`/`id:` that matches nothing (the fresh read runs again, `elementTarget.resolve`; only with a live resolver, never in the cache-only Page classification), a native pattern call UIA refused as disabled before delivery (`uia.ErrDisabled` and `refusedBeforeCall`: the same call on the same Element, in `runHeadless`), a Page Element refused as disabled by its check or aim (both run again, `runPage` and the headed Page click and scroll pre-checks). Everything else refuses at once: ambiguous references, stale indexes, hidden or covered Page Elements, missing patterns, writes that may have taken effect.
- **Why 2 s:** agent-desktop's 5 s is half of winuse's 10 s Action budget, and a reference's fresh read already takes up to 3 s of it. 2 s leaves the first and a last fresh read, the Agent Cursor glide and the Action inside the budget, and adds at most 2 s to another Agent's ~12 s Lane wait. The wait also stops 3 s before the request's deadline (`targetWaitMargin`), so a late Batch step refuses instead of timing out. Longer waits stay explicit (`wait ... enabled`, up to 300 s).
- **Lanes:** the wait runs inside the Action's Lanes (they are taken by the registry before the command resolves anything); the 2 s bound keeps the cost to other Agents small.
- **Esc:** the wait checks the Esc latch after every pause: exit 130 with the nothing-was-sent hint (`nothingSent` now leaves an Esc stop's message alone). Tries run on the command's goroutine and are never abandoned, because a try can act.
- **Indexes:** an Element Index is never re-resolved; a disabled one is tried again on the same Element.
- **Page click point** (`internal/page/input.go`, any Aim with `Scroll`): after `DOM.scrollIntoViewIfNeeded`, `settleJS` (awaited) compares the box across two animation frames (100 ms when frames do not come), up to five rounds, bounded at 700 ms, answering at once in a hidden document. Still outside or covered: `scrollIntoView({block:'center', inline:'center', behavior:'instant'})` once (a cross-origin iframe's element too), settle, measure again, then refuse. Covered is not polled for 2 s.
- **No opt-out flag:** the wait only runs when the Action would otherwise refuse, so it never slows an Action that succeeds. `Services.TargetWait` (0: 2 s, below 0: at once) is the seam for tests.
- **Docs:** ADR 0022 (new), CONTEXT.md (Element Reference), `docs/codex-parity.md` (stale elements), help (`help global`: one paragraph), the skill core (a paragraph under Element References) and the `batch-and-wait`, `reading` and `browser` topics.

## Tests

- `internal/helper/targetwait_test.go` (faked Sleep and Now): a disabled native Element tried until enabled (one cursor glide, one ripple); the refusal after 2 s with the waited note and Delivery `not_delivered`; `TargetWait` below zero and a shorter `TargetWait`; Esc during a disabled wait and during a reference's wait (exit 130, no try after the press); a pending reference found on the third fresh read with its note; never found with the waited note and no act; no wait for a stale index, no click route, a write error, an ambiguous reference or a cache-only resolver; the deadline margin and the wall clock bound; `withWaited`; a disabled Page Element aimed at again until enabled, a disabled Page field's check read again, covered not waited on.
- `internal/page/input_test.go`: `TestAimSettlesAndRecentres` (one awaited settle when in view; outside and covered re-centred once and aimed at the new point; still covered refused after one re-centre; disabled and hidden not re-centred; no scroll, no settle; a cross-origin iframe's element re-centred first) and `TestSettleIsBounded` (a settle that never answers costs at most 700 ms).
- Updated for the new wording, on a faked clock: `TestHeadlessRefusalsBeforeAnyPatternCall`, `TestLiveRefRefusals`, `TestPageClickRefusals`, `TestChooserInterceptionOffAfterAFailedClick`, `TestPageTypeTextInto`.

`go vet ./...`, `go vet -tags e2e ./test/e2e` and `go test ./...` pass (`internal/page`'s timing test `TestReadOverlapsItsRoundTrips` failed once under the full parallel run and passes alone, five runs; it does not aim).

## Live check (to run by a human)

These start the build's own helper, which makes an installed helper of another build exit.

- Native disabled: in a dialog whose OK enables on input (Notepad's Replace, or Run with an empty box), `winuse batch <window> set-value <field> x then click name:OK` and, separately, `click name:OK` sent while the field is empty, then typing in the field within 2 s by hand: expect `ok`. With the field left empty: expect `..., still after waiting 2s; nothing was sent` after about 2 s.
- Native pending reference: `winuse batch <window> click name:Save` on Notepad with unsaved text, then right away `click "name:Don't Save"` on the confirmation (or `click name:Cancel` on a Save As dialog as it opens): expect `ok` with the fresh-read note, no `wait` needed. A name that never appears: expect the refusal after about 2 s.
- Esc: send `click name:Nope` on any Window and press Escape within 2 s: expect exit 130 with nothing sent.
- Lanes: while one Agent waits on a missing name, a second Agent's Action on the same Window runs right after it, not refused.
- Page disabled: a page whose submit button enables on input (a form with `disabled` until a field is filled); click it and fill the field in the browser by hand within 2 s: expect `ok`.
- Page click point: a button straddling the bottom edge of the viewport, a button under a sticky header after scrolling, and a long list whose content above the target loads lazily: expect each click to land (check with `--observe` or a screenshot), and a cookie banner still to refuse as covered. Compare a plain Page click's latency with main (settle cost, expected a few tens of ms).

## Comments

### Superseded by main's brief wait, 2026-11-16

main shipped the same idea first and better while this branch was open: reliability-council 06's brief wait (`internal/helper/briefwait.go`, ADR on main) waits up to 1.8 s for a disabled Element, a closing modal dialog or a `name:`/`id:` that has not appeared, **outside the lanes** (this item waited while holding them), says how long in a `Note:`, and has `--no-wait`. So this item's helper side is reverted before merging main: `targetwait.go`, its refusal wording, ADR 0022 (the number went to main's update ADR) and its doc and skill lines. **Kept:** the Page click point's settle and one re-centre after scrolling (`internal/page/input.go`), which main does not have; it passed the e2e browser suite on Chrome and Edge (2026-11-15).