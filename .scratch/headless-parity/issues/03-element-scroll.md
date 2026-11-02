# 03: Element scroll

**What to build:** two new Headless forms of `scroll`, both through UI Automation:

- `scroll <window> <element index | name: | id:> <up|down|left|right> [steps]`. `steps` defaults to 3 and runs from 1 to 50.
  - It uses ScrollPattern SmallIncrement or SmallDecrement on the Element, or on its nearest scrollable ancestor.
  - It is verified by the scroll percent.
- `scroll <window> <element ref> into-view`. It uses ScrollItem.ScrollIntoView and is verified by IsOffscreen.

Other behaviour:

- `--headed` activates the Window, resolves the Element's point, then sends a wheel scroll of steps × 120.
- The `@x,y` form is unchanged.
- headlessStep routes the Element forms Headless, so Batch steps pick them up.

See spec: "Element scroll".

**Blocked by:** none

**Status:** done

- [x] uia: scroll by steps with an ancestor search and percent verification; into-view with IsOffscreen verification
- [x] Errors: no scrollable ancestor or no ScrollItem means "nothing was sent"; no movement means delivered, "may already be at the end"
- [x] Protocol ScrollArgs gains element_index, ref, direction, steps and headed; the CLI parses both forms
- [x] headlessStep includes the Element scroll; the Batch scope follows it
- [x] `--headed` Element scroll uses the wheel at the Element point after activation

## Comments

2026-11-02: Landed.
- `internal/uia/scroll.go`: pure decisions `ParseScrollDirection`, `CheckScrollSteps` (1 to 50, `DefaultScrollSteps` 3), `ScrollDirection.amounts` (SmallIncrement/SmallDecrement on one axis), `scrollVerdictOf` (a percent outside 0..100, including UIA_ScrollPatternNoScroll -1, is unreadable and not judged) and the driver `scrollBySteps` over injected steps, tested offline in `scroll_test.go`. COM side: `Client.ScrollElement` finds the Element's own ScrollPattern scrolling on the axis, else the nearest ancestor's through the raw view walker (IUIAutomation slot 16 get_RawViewWalker, IUIAutomationTreeWalker slot 3 GetParentElement, ScrollPattern percent getters slots 5 and 6, checked against the SDK 10.0.26100.0 header), at most 64 levels. `Client.ScrollElementIntoView` makes one ScrollItem call (`scrollItemIntoView`, now also used by the Headed click's best-effort `scrollIntoView`) and polls IsOffscreen (30022) for false.
- Errors: `ErrNotScrollable` (wrapped with the axis) and `ErrNoScrollItem` are refusals ("nothing was sent"). No percent change is a `VerifyError` whose new `Msg` field carries "the Element did not scroll; it may already be at the end" (delivered, "do not repeat"). A call that fails after an earlier step was delivered is always a `WriteError`.
- Protocol: `ScrollArgs` gains `element_index`, `ref`, `direction` (up, down, left, right, into-view), `steps` (*int, so an explicit 0 is refused rather than defaulted) and `headed`.
- Helper: `internal/helper/scroll.go` holds `parseElementScroll`, which both the command and `headlessStep` read. `takesHeaded` includes scroll, so a headed Batch injects the flag. The Headed direction form resolves `ElementPoint` (as a Headed click does) and wheels steps x 120 (`wheelDelta`); Headed into-view calls `ScrollElementIntoView` after activation. Cursor labels: `scroll down "<name>"`, `scroll into view "<name>"`.
- CLI: `parseScrollCommand` dispatches on an `@` target. Batch steps share `parseElementScrollTokens`. A name: reference the shell split is named, and a point missing its `@` keeps the old message. Usage text updated in cli.go and both scroll usage hints.
- Not done here: the e2e coverage on the fixture's list (ticket 09).
