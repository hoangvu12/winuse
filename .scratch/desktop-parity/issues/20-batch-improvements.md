# 20: Batch improvements

**What to build:**

- Each step's output is printed (prefixed with the step number) instead of only `ok`, so `eval`, `tab list` and the notes from ticket 02 come back.
- Like agent-desktop (user decision, 2026-11-03): a Batch runs past a failed step by default, reports every failure at the end and exits 1 if any failed. `--stop-on-error` stops at the first failure (today's behaviour).
- Batch from stdin (`winuse batch -`), as agent-desktop PR #439 adds.

Today: "A Batch prints only ok" (`skills/winuse/SKILL.md:471-473`).

- Steps that failed with "may have taken effect" or "delivered; do not repeat" do not stop the Batch either (agent-desktop's behaviour); the final report marks them as uncertain.

**Blocked by:** none

**Status:** needs-info (everything but stdin batches landed; the stdin format is an open decision, see the 2026-11-03 implementation comment)

- [x] Per-step output
- [x] Continue by default, `--stop-on-error`, and the final report
- [ ] stdin batches (waits on the format decision below)
- [x] Offline tests
- [x] SKILL.md Batch section

## Comments

### 2026-11-03: triage decision (user)

Do it like agent-desktop: per-step output, continue past failures by default (uncertain ones too, marked in the report), `--stop-on-error`, and a Batch from stdin. This changes today's default (stop at the first failure); SKILL.md's Batch section is rewritten.

### 2026-11-03: implemented, except stdin batches (branch laptop/dp-20-batch)

What landed:

- Per-step output. Each step prints `Action N (<action>): <first line>`, and the rest of its output is indented two spaces, so a left-margin line always starts a step or is a note for the whole Batch. `eval` results, `tab new`'s id and `navigate`'s URL now come back. `tab list` is a Batch step now (`protocol.BatchStepKinds`, the CLI's `tab list` step). An empty step output reads `ok`.
- A failed step no longer stops the Batch. Its line is `Action N (<action>): failed: <message>` with `hint: <hint>` indented under it, and the next step runs. After the last step the Batch fails with exit 1: `batch: K of N Actions failed: Action 2 (type-text), Action 4 (set-value, uncertain)`. A step is uncertain when its message or hint says "may have taken effect" or "do not repeat". When any step is uncertain, the hint says to read the Window State (`winuse get-window-state <id>`) before repeating it.
- `--stop-on-error` goes before the first Action, before or after the Window, among `--timeout`. It is `stop_on_error` on the wire, and it restores today's `batch stopped at Action N (<action>): <message>` with the step's hint and exit code. The lines of the steps before it are still printed.
- An Esc stop (exit 130) and a spent request budget still stop the Batch at once, in either mode, and keep the lines of the steps that ran.
- New: a Window that does not resolve fails the Batch before any step, as `batch: <resolution message>`. Running on, every step would have failed the same way. This used to be `batch stopped at Action 1 (...)`.
- Transport. `helper.Error` gained `Out` (the step lines). The server copies it into `Response.Out` on a failure, an Esc stop and a timeout, and the CLI's `finish` prints `Out` before the failure. No other command sets `Out` on a failure.
- Ticket 02's notes. `reportOpened` and `watchForeground` also run after a Batch that failed with step lines (`ranOn`; not after an Esc stop). The opened and closed Windows and the foreground note still come after the steps. A step's own notes (a Page dialog) stay indented under that step. `stepNotes` is gone.
- dp-19's trace. `redactBatchOut` (internal/cli/trace.go) replaces the output of each step that `redactsOut` covers (an `eval` step) with `{"redacted":true,"len":N}` after its `Action N (eval): ` prefix. The step is found by its number in the request's steps, not by the kind printed. The other steps are kept. A Batch whose arguments cannot be read is redacted whole. `--trace-unredacted` keeps everything.
- hm-21's Batch notes. A step refused for having no Headless route ("nothing was sent") is an ordinary failed step: the Batch runs on past it, and it is not marked uncertain. The Batch scope rule (passive unless `activate-window` or a step's own `headed`) is unchanged.

Files: internal/helper/batch.go, server.go, opened.go, selfactivation.go; internal/protocol/protocol.go; internal/cli/batch.go, cli.go (usage text, `finish`), tab.go, trace.go; CONTEXT.md (Batch); skills/winuse/SKILL.md (Batches rewritten, the notes paragraph, the command reference); README.md; docs/codex-parity.md (extensions paragraph, the notes sentence); test/e2e/README.md.

Offline tests:
- New in helper: `TestBatchRunsPastAFailedStep`, `TestBatchReportMarksUncertainSteps` (the marking, the hint and multi-line step output), `TestBatchStopOnErrorStopsAtFirstFailureNamingTheStep` (renamed from the old stop test, now with `StopOnError`), `TestBatchWindowThatDoesNotResolveFailsBeforeAnyStep`, `TestOpenedBatchPastAFailedStepStillReports`, `TestTabListStepPrintsTheTabs`, `TestServeFailureCarriesItsOutput`.
- Extended in helper: `TestBatchExitCodeForwarding` (an Esc stop keeps step 1's line and nothing runs after it), `TestBatchStopsWhenTheRequestBudgetExpires` (`Out` kept), `TestHeadedActionReportsNothing` (the foreground note after a run-on Batch), and the `stepLines` cases in `TestOpenedNotesFormat`.
- New in cli: `TestTakeBatchStopOnError` and `TestTraceRedactsABatchsEvalSteps`.
- Extended in cli and protocol: `TestBatchTabSteps` (`tab list` step), `TestBatchArgsJSONShape` (`stop_on_error`), `TestBatchStepKindsAreBatchableActions`.
- Every helper test that expected `ok\n` from a Batch, or `batch stopped at Action N` for a refused step, now expects the step lines, or reads the failed step's line through `failedStep` (batch_test.go). These are in headless, keyroute, invokemenu, opened, overlay, pageactions, pageonly, pointer, scroll, selecttext, selfactivation, tabs and typeinto.

e2e (`go vet -tags e2e` clean). New helpers `batchSteps` and `assertBatchOK` (batch_test.go).
- Tests that relied on stopping at the first failure, updated:
  - `TestBatchStopsAtTheFirstFailure` became `TestBatchRunsPastAFailedStep` (step 3 is now a Headless `set-value` that must land) plus `TestBatchStopOnErrorStopsAtTheFirstFailure`.
  - `TestBatchUnknownWindowFailsStepOne` became `TestBatchUnknownWindowFailsBeforeAnyStep`.
  - `TestHeadedOnlyActionsAreRefusedWithoutTheFlag`: its Batch now runs the checkbox step, and a `--stop-on-error` rerun must not.
- Tests that expected a Batch's `ok`, moved to `assertBatchOK`: `TestBatchTypesThenClicksAndSucceeds`, `TestBatchAgainstTheDialog`, `TestHeadlessTypeTextIntoElement`, `TestHeadlessInvokeMenuOnClassicDialog` and `TestInjectedEscDoesNotStopTheBatch`.
- `assertEscStopped` (`TestEscStopsTheLongBatch`) now accepts the ok lines of the steps before the stop.

Live, on the laptop, under the live lock, one run at a time:
- Pass: `TestBatchRunsPastAFailedStep`, `TestBatchStopOnErrorStopsAtTheFirstFailure`, `TestBatchUnknownWindowFailsBeforeAnyStep`, `TestBatchUsageErrorsExit2WithoutDialing`, `TestBatchAgainstTheDialog` (its `Note: window ... closed` came after the step lines), `TestHeadlessTypeTextIntoElement`, `TestHeadlessInvokeMenuOnClassicDialog`, `TestHeadedOnlyActionsAreRefusedWithoutTheFlag` and `TestBatchTypesThenClicksAndSucceeds`.
- Fail, and not from this ticket: `TestInjectedEscDoesNotStopTheBatch`. Its concurrent `--headed press-key Escape` times out (`winuse request timed out: press-key`) queued behind the Batch's ~7 s of typing. It fails the same way on main a55959c.
- Not run: `TestEscStopsTheLongBatch`, which needs a human Esc press.

Open decision: the stdin format (`winuse batch -`). agent-desktop's PR #439 reads its batch JSON from stdin. winuse's Batch has no JSON input form, so the ticket leaves open what stdin carries. The options:
1. The same tokens as the command line (`<window> [--timeout s] [--stop-on-error] <action> ... then ...`), split on whitespace with double quotes grouping. This needs a quoting rule of winuse's own.
2. Recommended: one step per line in the CLI step syntax, with no `then` needed. The first line is the Window and the Batch flags, and double quotes group words. Long or quote-heavy text stops fighting the shell, which is what #439 is for.
3. The wire JSON (`protocol.BatchArgs`: `{"window":..,"steps":[{"kind":..,"args":{..}}]}`). This is closest to agent-desktop, but it exposes the wire argument names as a user format.
