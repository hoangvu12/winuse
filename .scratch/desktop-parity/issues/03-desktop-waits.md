# 03: Desktop waits

**What to build:** extend `wait` beyond Pages (today: selector, text, URL, load state, download; `internal/cli/cli.go:187-193`):

- `wait window <app|title> [--gone]`: a Window of that App or title appears (or disappears). Prints its list-windows line.
- `wait <window> text <text>`: the text appears in the Window's UIA tree (Pages keep today's route).
- `wait <window> <element> enabled|disabled|gone|value <v>|checked|unchecked|expanded|collapsed`.
- `wait <ms>`: a plain pause, capped.
- `--timeout <s>` on all of them; raise the cap above 25 s with the request deadline (ties into ticket 14).

agent-desktop's predicates are in `ad:docs/commands/` (row 52 of the research) plus post-action `--wait-for`. Post-action waits are out of this ticket.

- Poll: EnumWindows for window waits (Laptop probe: found a new Window in about 0.4 s); a bounded UIA walk every 250-500 ms for text and element waits. UIA events were measured as a cheaper signal (WindowOpened 0.63 s after launch, value change in 55 ms), but winuse banned event subscriptions after a Codex freeze; stay with polling and note it.
- A Window that goes away mid-wait fails with "the Window closed".
- Element references resolve once at the start; `gone` succeeds when the Element is stale.

**Blocked by:** none

**Status:** done

- [x] Predicates in CLI and protocol, routed by origin (UIA Window vs Page)
- [x] Polling loops bounded by the request deadline
- [x] Offline tests per predicate with faked state over time
- [x] Opt-in e2e: wait for a fixture dialog, a value change, a closed Window
- [x] SKILL.md waits section

## Comments

### 2026-11-03: triage decision (user)

Kept as written.

### 2026-11-03: landed

- CLI (`internal/cli/wait.go`, `page.go`): `wait <milliseconds>` (one whole number and nothing else; above 0 and at most 300000; refuses `--timeout`), `wait window <app or title> [--gone] [--timeout <s>]`, and after a Window either today's Page predicates or `<element index | name: | id:> enabled|disabled|gone|value <v>|checked|unchecked|expanded|collapsed`. The element form lives in `parseWaitArgs`, so a Batch's `wait` step takes it too. A pause asks for its length plus `WaitMargin` as the request budget, the other forms for `--timeout` plus the margin, as ticket 14 set up (cap 300 s, already done there). Usage text, `protocol.PageUsage("wait")` and the usage errors name every form.
- Protocol (`internal/protocol/page.go`): still the one `wait` command. `WaitArgs` gains `gone`, `element_index`, `ref`, `state` and `ms`; kinds `window`, `element`, `ms` (`WaitWindow`, `WaitElement`, `WaitMS`) and `WaitStates`. No new registration and no new budget.
- Routing (`internal/helper/waits.go`, `desktopWait`): window, element and ms are desktop waits; text is one on a Window that names no browser (`PageReader.Browser`, get-window-state's own rule), so a browser Window keeps today's Page route, with or without a Browser Connection. selector, url, load and download stay Page waits, unchanged.
- Polling, no UIA event subscriptions (ADR 0004): a window wait reads the Window list every 100 ms; a text wait walks the tree with the bounded read every 250 ms through the new `uia.Client.PeekTree`, which releases the read's Elements instead of installing them, so the agent's Element Indexes stay valid; an element wait resolves its Element once (`uia.Client.HoldElement`, per Agent through the request's ctx), holds an AddRef'd reference, reads IsEnabled (stale HRESULT = gone), Value, IsPassword, Toggle, SelectionItem and ExpandCollapse every 250 ms, and releases it on the worker at the end. Each loop is bounded by the wait (10 s default, `--timeout` up to 300 s) and the request deadline less `WaitMargin` (`pageWaitContext`), and is a passive Esc scope (exit 130).
- Outcomes: a window wait prints the matching Windows' list-windows lines (App name or executable as a Window reference matches it, or any part of the title, both case-insensitive); `--gone` prints `ok`. `gone` succeeds when the held Element is stale, and prints `ok (the Window closed)` when its Window closed. Any other text or element wait whose Window closes fails with `waiting for ...: the Window closed`; an element that goes while its Window stays fails with the usual stale message. A timed-out element wait says what it read last (`it reads disabled`; a password field's value is never echoed). value, checked/unchecked and expanded/collapsed on an Element without that pattern fail on the first read instead of waiting out the timeout; checked/unchecked fall back to SelectionItem (radio buttons) when there is no Toggle. A Page Element is refused with the `wait <window> selector|text` hint. A pause whose number is an open Window's ID is refused (it is `wait <window>` with the predicate left out).
- Action gate: desktop waits only read, so they skip the gate like get-window-state (`waitGated` in `CommandsWith`; one line in `state.go`'s registry loop). A Page wait still takes it, and a Batch's wait step runs inside the Batch's gate as before. This lets an Action run while a wait polls, and the e2e test relies on it.
- Docs: SKILL.md "Wait for a change" section plus the command reference; README command table; codex-parity.md: the "one request at a time" row names the desktop waits among the concurrent reads (it keeps **matched**) and the extensions paragraph gains a sentence. CONTEXT.md needed no new term: a wait is not an Action and the window wait is no Desktop command (it skips the gate).
- Tests: `internal/helper/waits_test.go` (window appears/gone/times out, App or title matching, text in name/value/description without touching the stored Window State, Window closed mid-wait, browser Windows keep the Page route, every element state over faked polls, name: resolved once, gone and gone-with-its-Window, stale, timeout reports, missing-pattern refusals, Page Element refusal, pause and its Window ID guard, Esc on each kind, the gate skipped for a desktop wait and kept for a Page wait, an element wait as a Batch step), `internal/uia/wait_test.go` (HoldElement holds one reference until Release, misses, PeekTree on a fake worker), `internal/cli/wait_test.go` (every form, errors, request budgets, the Batch step) and two updated cases in `cli/page_test.go` (`wait 301` is now a pause).
- Live (laptop, 2026-11-03): `WINUSE_E2E=1 go test -tags e2e -run 'TestWaitForADialogAndItsClose|TestWaitForAValueChange' ./test/e2e` passed. It waits for the Classic Dialog to open (and checks its printed line), then for it to close (`--gone`), for its Edit to go (`id:1001 gone`) and for text in it (fails with the Window closed), then for the main window's text field to reach a set-value's value (element and UIA text waits), checks the timeout message, and runs `wait 400`.
- Live after merging origin/main 82cc761 (headless-max 03, desktop-parity 10): the first rerun failed and the second passed (7.8 s and 4.1 s). In the failed run the Headless click on Classic Dialog... printed `Note: window <id> closed` and no new Window, `wait window` did not answer within 20 s although its handler is bounded at 15 s, the next test's get-window-state timed out, and the helper never went idle, so a handler was stuck. Not diagnosed. The commands that hung all start with `win.Collect` (the window wait polls it every 100 ms), which reads titles with GetWindowTextW before the own-process filter, and that sends WM_GETTEXT to the helper's own overlay and cursor windows. A busy UI thread would block it.
- Open: element waits on Page Elements (CDP) are not built and are refused; post-action `--wait-for` is out of scope as the ticket says.

### 2026-11-03: the stuck helper, fixed

- Cause, confirmed: `win.Collect` read every visible window's title with GetWindowTextLengthW and GetWindowTextW before `VisibleWindows` drops the helper's own windows (`OwnProcess`). For a window of the calling process those send WM_GETTEXT and wait for its thread. The overlay and the Agent Cursor are visible while an Action runs and live on the helper's UI thread, so a Window list read from another request (the window wait polls one every 100 ms; every command resolving its Window reads one) blocked while that thread was busy. The offline test reproduces it: a message-only window on a busy UI thread of the test process blocks GetWindowTextW (logged), and no longer blocks the title read.
- Fix (`internal/win/enum.go`): `collectOne` works out the process first, and `windowTitle` reads a window of this process with InternalGetWindowText, which sends no message. Other processes' windows keep GetWindowTextW, which reads the cached caption for them. Listings are unchanged: the helper's own windows are filtered out as before. Test: `internal/win/windowtitle_test.go`.
- Wait loops (`internal/helper/waits.go`): `pollUntil` runs each poll on its own goroutine and returns when the wait's context ends, even if a poll never answers. The stuck poll is left to finish and its result dropped. Checks return their results instead of writing shared variables, so nothing races. Test: `TestAWaitEndsAtItsDeadlineWhenAPollBlocks` (window, text and element waits with a poll that never returns end at their 300 ms timeout). What can still block outside the loops: the Window resolution at a wait's start (one `Collect`, as in every command) and the window-ID guard of a pause (one `Collect`). Neither waits for the helper's own windows any more.
- Live after the fix and merging origin/main c7eb2ee: the two e2e tests passed 3 runs in a row under the lock (dialog test 11.7 s, 12.8 s, 10.2 s; value test 5.3 s, 6.4 s, 4.9 s), and the helper went idle and exited after each.

### 2026-11-03: review fixes (a poll that outlives its wait)

- Race fix (`internal/uia/wait.go`): a poll that `pollUntil` leaves running past the wait's end could call `held.State` after `Release` cleared the Element, and the field had no synchronisation. `heldElement` now guards the Element with a mutex and takes it inside the read's worker job (`use`/`done`). A read after Release gets `errHoldEnded` and reads nothing. A Release with a read in flight leaves the release to that read, which does it on its own worker when it ends. Test: `TestHeldElementOutlivesItsWaitSafely`. `-race` could not run on this laptop (no C compiler for cgo).
- Worker abandonment: it could happen. `uia.Client.Run` abandons the worker when a job's context ends while the job is queued or running. A wait's poll uses the wait's context, which ends at the wait's timeout, an ordinary outcome, so a poll queued behind another request's read (or still walking) closed a healthy worker. Every request queued on that worker then failed with "the UIA worker is winding down ... retry the command". Stored Window States were not lost, because the store is client-level and survives a worker replacement. Fix: `uia.WithWaitPoll` marks the wait's context (`runDesktopWait`), and `Run` gives such a job up without abandoning, whether queued (never delivered) or running (left to finish: a poll installs nothing). A worker that is really wedged is still abandoned by the next request that overruns on it. Test: `TestRunGivesAWaitPollUpWithoutAbandoning`.
