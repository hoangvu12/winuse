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

**Status:** ready-for-agent

- [ ] Point hit-test inside a Window's subtree, bounded by the request budget
- [ ] Routing: point forms Headless when the hit-test finds a routable Element; Batch predicate follows
- [ ] Offline tests: nested bounds, overlap refusal, Window-only refusal, scroll conversion
- [ ] Opt-in e2e: fixture button and list by point, covered, foreground unchanged
- [ ] ADR amendment (ticket 17), SKILL.md

## Comments

### 2026-11-03: triage decision (user)

Adopt. It reverses ADR 0010 Decision 1, recorded in ADR 0013 (ticket 17). agent-desktop's open PR #438 does the same on macOS (hit-test the named App, press the Element, refuse when nothing can be pressed).
