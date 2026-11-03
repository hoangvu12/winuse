# 20: Batch improvements

**What to build:**

- Each step's output is printed (prefixed with the step number) instead of only `ok`, so `eval`, `tab list` and the notes from ticket 02 come back.
- Like agent-desktop (user decision, 2026-11-03): a Batch runs past a failed step by default, reports every failure at the end and exits 1 if any failed. `--stop-on-error` stops at the first failure (today's behaviour).
- Batch from stdin (`winuse batch -`), as agent-desktop PR #439 adds.

Today: "A Batch prints only ok" (`skills/winuse/SKILL.md:471-473`).

- Steps that failed with "may have taken effect" or "delivered; do not repeat" do not stop the Batch either (agent-desktop's behaviour); the final report marks them as uncertain.

**Blocked by:** none

**Status:** done

- [x] Per-step output
- [x] Continue by default, `--stop-on-error`, and the final report
- [x] stdin batches (one step per line, decided 2026-11-03)
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

### 2026-11-03: merged main (4fabe2a)

- One conflict, in test/e2e/README.md. Both sides are kept: this ticket's Batch wording for `TestHeadedOnlyActionsAreRefusedWithoutTheFlag`, and main's headless-max 11 sentence and its 11 and 12 test paragraphs.
- Three Batch tests from main expected the old `ok`. They now expect the step lines: `TestBatchPointStepsKeepTheHeadlessScope` (each step's `ok (clicked element 5 ...)` comes back), `TestBatchPostedStepsArePassive` and `TestDesktopWaitIsABatchStep` (dp-03).
- main's new e2e files run no Batch. The four checks pass after the merge.

### 2026-11-03: stdin format decided (PC orchestrator): option 2

One step per line in the normal step syntax, with no `then`. The first line holds the Window and the Batch flags (`--timeout`, `--stop-on-error`).

### 2026-11-03: stdin batches implemented; merged main (d3c3325)

- `winuse batch -` (internal/cli/batchstdin.go, hooked into `batchCmd`).
  - The first line is the Window, with `--timeout <s>` (or `--timeout=<s>`) and `--stop-on-error` in any order. `--headed` there is refused with the hint `winuse --headed batch -`: the global flag stays on the command line.
  - Each further line is one step, parsed by the same `parseBatchStep` as the command-line form. A `then` on a line is an ordinary word.
  - Words split at spaces and tabs. Double quotes group words and keep their spacing; inside them `\"` is a quote and `\\` a backslash. Outside quotes a backslash is itself, so Windows paths need no escaping, and `""` is an empty word.
  - Blank lines and lines whose first non-space character is `#` are skipped. CRLF line ends and a leading BOM are accepted.
  - Usage errors exit 2 without dialing the helper and name their line: `batch stdin line N: <the step's own error>`. So do an unclosed quote, a second word on the first line, a missing Window line, no Action, and anything after `-` on the command line.
- Docs: the usage text (a `batch -` entry, plus the second line of `batchUsage`), SKILL.md, the README row and the codex-parity extensions paragraph.
- SKILL.md size: main's file is at the 64 KB guard. The Batches section was rewritten shorter than main's version (stdin included), and the notes-paragraph addition was dropped. SKILL.md is 65362 bytes against main's 65509.
- Tests:
  - cli: `TestParseBatchScript` covers flags in any order, comments, a blank line, CRLF, a BOM, quoted text with `\"`, `then` and repeated spaces, a backslash path and an empty `""` value. `TestParseBatchScriptLongTextOnALine` puts about 26 KB of non-ASCII text on one line. `TestParseBatchScriptErrorsNameTheLine` has eight cases plus `batch - 101`. `TestBatchStdinUsageErrorExits2WithoutDialing` goes through `Main` with a file as stdin.
  - e2e: `TestBatchFromStdin` covers a comment, a blank line, CRLF and `--stop-on-error` on the first line, and a quoted `set-value` holding `then` and quotes.
- Merge of main (fa349de, then d3c3325):
  - Conflict in selfactivation.go: kept `ranOn` together with ma-06's `desktopUses` check.
  - Conflict in pageactions_test.go: kept hm-20's Chromium route, with the step-line output.
  - Batch tests from main moved to step lines: `TestBatchChromiumStepsArePassive` (hm-20), and `TestBatchRunsElementDragAndHoverSteps` (dp-13; its unheaded refusal is now a failed step read with `failedStep`).
  - ma-06 lanes: they decide a Batch's lanes up front from its steps, which continuation does not change. A Window that does not resolve takes the desktop lane, and the Batch then fails before any step as before.
- Live, under the lock, one run at a time:
  - Pass: `TestBatchFromStdin`, `TestBatchRunsPastAFailedStep`, `TestBatchStopOnErrorStopsAtTheFirstFailure`, `TestBatchUnknownWindowFailsBeforeAnyStep`, `TestBatchUsageErrorsExit2WithoutDialing`, `TestBatchAgainstTheDialog`, `TestHeadlessTypeTextIntoElement`, `TestHeadlessInvokeMenuOnClassicDialog`, `TestHeadedOnlyActionsAreRefusedWithoutTheFlag` and `TestBatchTypesThenClicksAndSucceeds`.
  - Fail, and not from this ticket: `TestInjectedEscDoesNotStopTheBatch`. With ma-06 its concurrent `press-key` is refused ("agent e2e.test ... is using the desktop") rather than timing out. It fails the same way on main d3c3325.
  - Not run: `TestEscStopsTheLongBatch`, which needs a human Esc press.
- Merged main again (1a727cc, skill-trim). The only conflict was SKILL.md's Batches section, rewritten again on top of the trim: 1261 bytes, against 1598 before the trim and main's trimmed 1160, which still described stopping at the first failure. SKILL.md is 54387 bytes. The four checks pass. The merge changed no code, so the live runs above stand.
