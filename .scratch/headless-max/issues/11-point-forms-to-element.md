# 11: Point forms resolve to an Element in the target Window

**What to build:** `click @x,y` (left, single), `set-value @x,y`, `perform-secondary-action @x,y` and `scroll @x,y` on a non-Page point become Headless by resolving the point against the target Window's own UIA subtree, never against the desktop.

Today they are always Headed (`internal/helper/actions.go:115-129`, `setvalue.go:108-121`, `:185-199`, `pointer.go:76-85`) because a screen point names whatever is topmost there (ADR 0010 Decision 1). Resolving inside the target Window removes that objection (`../headless-gaps/headless-100.md` G2; cua's "(x, y) mode" UIA hit-test).

- Map the point from screenshot pixels to screen coordinates as today, then walk the target Window's subtree for the smallest Element whose bounds contain it.
- click: run the existing Headless click route on that Element.
- set-value and secondary action: the existing Headless Element routes.
- scroll: ScrollPattern on the smallest scrollable Element containing the point (reuse `ScrollElement`); scroll by the wheel amount converted to steps.
- Refuse with "nothing was sent" and the `--headed` hint when the smallest Element is the Window itself, a canvas/pane with no pattern, or two siblings overlap at the point with different patterns.
- The output names the Element it acted on: `ok (clicked "Save" at @x,y)`, so the agent can see a mismatch.
- Double, right and middle clicks at a point stay Headed, or go through ticket 12 with `--post`.

**Blocked by:** none

**Status:** done (live e2e not run yet: see the 2026-11-03 implementation comment)

- [x] Point hit-test inside a Window's subtree, bounded by the request budget
- [x] Routing: point forms Headless when the hit-test finds a routable Element; Batch predicate follows
- [x] Offline tests: nested bounds, overlap refusal, Window-only refusal, scroll conversion
- [x] Opt-in e2e: fixture button and list by point, covered, foreground unchanged (written and vetted, not run)
- [x] ADR amendment (ticket 17), SKILL.md

## Comments

### 2026-11-03: triage decision (user)

Adopt. It reverses ADR 0010 Decision 1, recorded in ADR 0013 (ticket 17). agent-desktop's open PR #438 does the same on macOS (hit-test the named App, press the Element, refuse when nothing can be pressed).

### 2026-11-03: implemented (branch hm11-point-to-element)

- Hit-test (`internal/uia/point.go`, `Client.ElementAtPoint`):
  - Input: the Elements behind the Window's latest Window State (the Agent's own store), read on the UIA worker. Each one's live `CurrentBoundingRectangle` is read, checking the request's deadline every 32 Elements; Page Elements, which have no live UIA Element, are skipped.
  - The pure `hitTest` takes the smallest Element containing the point, never the root (index 0); ties go to the deeper, then the later one.
  - Refusals: a point only the root contains is `PointWindowOnly`. An overlapping Element that is neither an ancestor nor a descendant and differs in role, value, toggle, expand state or Secondary Actions is `PointAmbiguous`. If neither that Element nor any ancestor below the root can take the Action, it is `PointNoRoute`: a canvas or pane.
  - Climbing: from the smallest Element up to the nearest one whose route holds, so a button's text label resolves to the button.
  - Routes:
    - click: probed live along that chain only (Invoke, Toggle, SelectionItem, non-leaf ExpandCollapse, a LegacyIAccessible default action: the Headless click's own order, without calling it).
    - set-value and Secondary Actions: read from the Window State line (settable, toggle, combo box or list; the named Secondary Action).
    - scroll: any Element, since `ScrollElement` climbs to the nearest scroller itself.
- Helper (`internal/helper/pointroute.go`): `PointResolver` is type-asserted from `Services.Actions`, like the other optional element sides. A helper without it keeps the point forms Headed-only (ticket 21's refusal).
  - `runAtPoint` order: resolve the Window, then the Headless refusals (locked, administrator, modal, minimized), then `shotPoint`, then the hit-test, then `runHeadless` on the resolved Element Index. The Agent Cursor glides to that Element. Everything before the pattern call says nothing was sent.
  - A `PointError` gives the `@x,y` `--headed` retry, plus the two Element Indexes for an overlap.
  - Output: `ok (clicked element 5 button "Save" at @412,88)`, `ok (set the value of ...)`, `ok (performed Expand on ...)`, `ok (scrolled ...)`. `withNote` merges the note into an `ok (...)` or into the lines a wrapper adds, such as dp-02's opened-window note.
- Routed: `click @x,y` left single, `set-value @x,y`, `perform-secondary-action @x,y` except Raise, and `scroll @x,y` outside a Page. Without `headed` they go Headless, never Headed.
  - A wheel amount converts to three Element scroll steps per notch (Windows' default of three lines per notch), rounded up, vertical first and then horizontal.
  - Over 50 steps (16 notches) on an axis there is no Element route; it is refused, as before.
  - Double, right and middle clicks, Raise and `--headed` keep ticket 21's routes.
  - The Batch predicate (`stepHeadless` → `pointStep`) counts these steps as Headless.
- Offline tests:
  - `internal/uia/point_test.go`: nested bounds and the climb from a label to its button, the right edge excluded, Window-only, a canvas with no route, overlap (different signatures refused, alike accepted, a child sticking out of its parent is not an overlap), unreadable bounds, parents from depths, `metaRoute`.
  - `internal/helper/pointroute_test.go`: click, set-value, Secondary Action and scroll at a point through a fake resolver, with the mapped screen point and the named output. The scroll conversion (120 → 3, -240 → 6 up, 40 → 1, both axes, too large refused). The refusals (Window only, canvas, overlap, no Window State, no screenshot) send nothing. Double and right clicks and Raise at a point stay refused, and with `--headed` the v1 pipeline runs. A Batch of point steps keeps the passive scope. Also `withNote` and `pointStep`.
- e2e (compiled with `go vet -tags e2e`, not run; live runs are paused):
  - New: `TestPointFormsActHeadlessOnTheCoveredWindow`.
  - `TestHeadedOnlyActionsAreRefusedWithoutTheFlag` keeps its click at a point on the canvas. That is now refused because the canvas has no click route, not because a point form needs the flag. The expected output is unchanged.
- Not measured, for the live run:
  - whether the fixture's canvas exposes a LegacyIAccessible default action. If it does, the canvas point becomes a click and both e2e tests need another target;
  - the hit-test's time on a large tree (one cross-process bounds read per Element).
- Docs: SKILL.md (the `@x,y` reference form, the Headless list, the Headed list, the scroll section and the command reference), CONTEXT.md (Headless and Headed Action), the usage text, codex-parity (the click, scroll, set-value and Secondary Action rows), the ADR 0013 table (done; its Reversals entry for ADR 0010 Decision 1 already described this), test/e2e/README.
