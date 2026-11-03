# 13: Hover and drag by UIA Element

**What to build:** `hover <window> <element>` and `drag <window> <element> <element | @x,y> [--drop-delay <ms>]` on native Elements. Today both accept only `@x,y` or a Page Element (`internal/helper/pointer.go:100-101`, `:136-137`). agent-desktop takes refs for both.

- Resolve the Element's clickable point, as a Headed click does, then the existing Headed hover or drag.
- `--drop-delay`: wait between the last move and the button-up, for targets that need hover time before accepting a drop.
- Native hover and drag stay Headed: no headless route exists outside classic Win32 (`../../headless-gaps/headless-100.md` G3). Page Elements keep their Headless CDP route.

**Blocked by:** none

**Status:** done (CONTEXT.md still lists hover and drag as Headed; no change)

- [x] Element forms in CLI, protocol, Batch
- [x] `--drop-delay` on both drag routes (CDP and native)
- [x] Offline tests of parsing and point resolution
- [x] Opt-in e2e: fixture drag between two Elements
- [x] SKILL.md

## Comments

### 2026-11-03: triage decision (user)

Kept as written.

### 2026-11-03: implemented (branch laptop/dp-13-hover-drag)

What landed:

- `hover <window> <element>` and `drag <window> <element> <element | @x,y>` take a UIA Element. A Page Element keeps its Headless CDP route (`onPage`, before anything else). Any other Element is Headed: with `--headed`, `runAction` activates the Window, the Element resolves to its Element Index and then to its clickable point or bounds centre after scrolling it into view (`ElementPoint`, as a Headed click aims), and the existing `Hover` or `Drag` runs. A drag's `@x,y` end maps through the screenshot as the point form does. Without `--headed` both are refused with nothing sent through headless-max 21's gate (`headedOnly`), with the exact retry: `winuse --headed hover <id> <element>`, `winuse --headed drag <id> <element> <element | @x,y> [--drop-delay <ms>]`. A stale Element fails as click's does (`elementError`).
- `--drop-delay <ms>` (or `--drop-delay=<ms>`, anywhere in the command or a Batch step), on `DragArgs.DropDelayMS`: native, `win.MouseDrag` holds the button down at the end point before the release (`dropWait`, in 10 ms slices so Esc and the request deadline still stop it); CDP, `page.Input.Drag` waits before the release of a plain mouse drag, and between `dragOver` and `drop` of an HTML5 drag. The helper bounds it to 0-5000 ms (`maxDropDelayMS`): the drag and its wait must fit the Action's 10 s request budget. The point drag's retry carries it too.
- Batch: a `hover`/`drag` Element step is Headless only when it names a Page Element (`pageStep` now checks the Page route; `pageOnlyStep` used to count any Element form as Headless, when UIA Elements were refused). An unheaded Batch stops at a UIA Element step with the `--headed` refusal; a `--headed` Batch runs it (takesHeaded already injected `headed` into drag and hover).
- The old refusal "hover/drag takes an element only in a browser Window's Page" is gone for hover and drag (`notPageElement` stays for upload).

Files: `internal/helper/pointer.go` (`elementHover`, `elementDrag`, `elementScreenPoint`, `dragBetween`, `dropDelay`), `pageelements.go` (`pageHover`/`pageDrag` take the resolved Page Element and the hold), `pageactions.go`/`pagecommands.go` (Batch scope), `state.go` (`Services.Drag` takes the hold), `internal/win/input.go`, `internal/page/pointer.go`, `internal/protocol/protocol.go` (`DropDelayMS`, comments), `internal/cli/pointer.go` (`stripDropDelay`, usage), `cli/batch.go`, `cli/page.go` (messages), `cli/cli.go` (usage), SKILL.md, README, docs/codex-parity.md.

Tests:
- `internal/helper/pointer_test.go`: `TestHoverAndDragByUIAElement` (hover and drag aim at `ElementPoint`, an `@x,y` end maps with the fresh frame, the hold reaches `Drag`), `TestDragByUIAElementRefusals` (stale Elements, the drop-delay range on both forms, nothing activated), `TestPointDragHoldsTheDropDelay`, `TestBatchRunsElementDragAndHoverSteps` (headed Batch runs them; unheaded stops with the refusal).
- `headedgate_test.go`: three new rows (hover on an Element, drag between Elements with `--drop-delay`, drag from an `id:` to a point) with their exact retries.
- `pageonly_test.go`: `TestPageHoverAndDrag` now expects the `--headed` refusal for UIA element 2, pins the Batch scope (Page forms Headless, UIA forms not) and a Page drag's hold; `TestPageOnlyStepsAreHeadless` expects UIA Element forms not Headless.
- `internal/page/pageonly_test.go`: `TestDragHoldsForTheDropDelay` (plain: last move to release; HTML5: dragOver to drop).
- `internal/win/input_test.go`: `TestDropWaitHoldsAndStops`.
- `internal/cli/pointer_test.go`: `TestDragAndHoverTakeElementsAndADropDelay` (CLI and Batch parsing, bad values).
- e2e `test/e2e/pointer_test.go`: `TestDragBetweenElements`. Unheaded `drag <id> id:canvas id:status --drop-delay 200` is refused with the exact retry; with `--headed` the fixture records a canvas drag from the canvas's centre to the status line's centre (within 2 px), and the command takes at least the 200 ms hold.

Ran: `go build ./...`, `go vet ./...`, `go test ./...`, `go vet -tags e2e ./test/e2e` all pass. Live: `TestDragBetweenElements` passed on this laptop (7.4 s). No hover-by-Element e2e: the fixture has no Element whose hover state it records (its hover band is no Element), so hover by Element is covered offline only.
