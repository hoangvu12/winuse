# 24: Batch across several Windows

**What to build:** let a Batch step name its own Window, so one Batch can, for example, click a button in an App and then act in the dialog it opened. agent-desktop's `batch` takes a JSON list where each step has its own target (research row 60).

Today a Batch is `batch <window> <action> ... then ...`, one Window for all steps (`internal/cli/batch.go`; `skills/winuse/SKILL.md`).

- Step syntax: an optional `in <window>` prefix (`then in 3412 press-key Return`). Steps without it use the Batch's Window.
- A step's Window may be one that an earlier step opened: allow `in new` to mean "the Window the previous step opened" (uses `02-report-opened-windows.md`'s diff).
- Element Indexes refer to the latest Window State of that step's Window; a step on a Window with no read state accepts only `name:`/`id:` (resolved live through ticket 17) or Window-level Actions.
- Esc scope: Headed if any step is Headed, as today, computed over all steps.

**Blocked by:** 02 (for `in new`), 17 (for `name:` on unread Windows)

**Status:** done (the live e2e run is still to do)

- [x] Parser and protocol per-step Window
- [x] `in new`
- [x] Offline tests
- [x] Opt-in e2e: click a fixture button, then Return in the message box, in one Batch
- [x] SKILL.md Batch section

## Comments

### 2026-11-03: triage decision (user)

Do it like agent-desktop (each step may name its own Window) with the `in <window>` syntax, plus winuse's `in new` (the Window the previous step opened, from 02).

### 2026-11-03: done (worker, branch laptop/dp-24-multi-window-batch)

- Syntax: a step may start `in <window>` (its own Window) or `in new` (the Window the step before it opened), on the command line after `then` and on a `batch -` line. `in` and `new` are case-insensitive like `then`; `in` later in a step is the step's own argument. Wire: `protocol.Step` gained `window` and `in_new`. Usage errors exit 2: the first step `in new`, an `in` with no Action, two `in`s.
- Helper (`internal/helper/batch.go`):
  - A step's own Window resolves before any step, with the Batch's, and gets the Window identity check (`reusedID`, split out of `checkWindow`). A Window that does not resolve, or a reused ID, fails the Batch before any step: `batch: Action N (<action>): <message>`.
  - `in new` is refused before any step when it is the first step, names a Window too, or carries an Element Index or `@x,y` point. The new Window has no Window State or screenshot yet, so `name:`/`id:` (resolved live through 17), keys and Window Actions are what it takes.
  - `in new` uses 02's diff per step: before the step ahead of it, the Batch snapshots that step's Window's App (`snapshotWindow`). After the step it looks at once, then every 50 ms for up to 2 s (`inNewWait`, `openedWithin`), and stops at the first new Window. A longer wait than 02's 300 ms settle, because the agent says a Window will open. Exactly one new Window is used. None, several (each listed with its ID and title), or a failed step before it fails the `in new` step with "nothing was sent", and the Batch runs on as dp-20 does.
  - Output: a step in its own Window prints `Action N (<action> in <id>): ...`, or `(<action> in new)` when no new Window was found. The final report's uncertain-step hint names each such step's Window (`winuse get-window-state 101; winuse get-window-state 777`). The trace's step-line regex (`internal/cli/trace.go`) and the e2e one accept the `in` form.
  - Esc scope: still one scope for the whole Batch, Headed if any step is. A named step is probed on its own Window. An `in new` step cannot be probed before its Window exists, so only its own `headed` (or `activate-window`) makes the Batch Headed (`inNewHeaded`). A step that turns out Headed at its turn arms its own swallowing scope, as dp-20's race note says.
  - The opened/closed note and the foreground note still diff the Batch's Window's App once, around the whole Batch.
- ma-06 lanes (`internal/helper/lanes.go`, `batchLanes`): a Batch still takes all its lanes up front, in the fixed order.
  - Each step adds the lanes of its own Window: the thread lane, the Page lane for a Page step, and the desktop lane when Headed. A step's Window that does not resolve takes the desktop lane alone, and the Batch then fails before any step.
  - An `in new` step's Window does not exist yet, so it takes the lanes of the step before it (the opener's UI thread), plus the desktop lane when it carries `headed`.
  - At its turn, `onLanes` has put the held set in the context (`heldLanesKey`), and `lanesHeld` checks that the new Window needs no lane the Batch lacks. The desktop lane is left out of that check: a headed step took it up front, and an unheaded one with no Headless route is refused by its own command. A missing lane refuses the step with "nothing was sent" and the hint to send it alone or in a Batch on that Window. A lane is never taken mid-Batch, since that would break the deadlock-free order. A dialog or MessageBox runs on its owner's thread, so the common case passes.
  - The lanes of a Batch sent with `headed` now classify each step with `headed` injected, as the Batch runs it. A Page click in a headed Batch therefore takes the desktop and thread lanes, not the Page lane.
- Docs: SKILL.md Batches (one paragraph, plus the reference line; 54840 bytes), CONTEXT.md (Batch), README row, codex-parity extensions paragraph, the CLI usage text, test/e2e/README.md.
- Tests: `internal/helper/batchwindows_test.go` covers a step in its own Window, an unresolved step Window failing before any step, `in new` acting in the opened dialog (with the Batch's note after), the `in new` refusals (nothing opened, two opened, the step before failed), the refusals before any step, a headed `in new` step arming the swallowing scope, lanes across Windows, a new Window on another thread refused, and the report naming each Window. cli: `TestParseBatchStepsInTheirOwnWindow`, `TestParseBatchScriptStepsInTheirOwnWindow`, `TestTraceRedactsAnEvalStepInItsOwnWindow`, and two usage-error cases. Opt-in e2e `TestBatchInNewActsInTheOpenedMessageBox` (`test/e2e/multi_window_batch_test.go`): one Batch clicks "Message Box..." on the covered fixture, then presses Return `in new`. Compiled, not run yet.
