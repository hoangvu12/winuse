# 05 — Opt-in e2e tests for Headless Actions

**What to build:** user-run e2e tests against the fixture app: Headless click of a button and a checkbox while another Window covers the fixture (foreground unchanged afterwards), Headless set-value without a focus change, a pattern-less Element failing with the `--headed` hint, and the Agent Cursor visible in a capture taken by the test.

**Blocked by:** 01, 02, 03

**Status:** ready-for-human

## Comments

- 2026-11-02 orchestrator: tests written and compiled (`go vet -tags e2e ./test/e2e/`), NOT run. `test/e2e/headless_test.go`: TestHeadlessClickUnderCoveringWindow, TestHeadlessSetValueKeepsFocusAndForeground, TestHeadlessClickWithoutPatternRouteFails (the canvas), TestHeadedClickBringsTheWindowForward, TestAgentCursorShownAndExcludedFromCaptures (finds a visible `winuse.cursor` window by enumeration, since WDA_EXCLUDEFROMCAPTURE keeps it out of every capture, then checks winuse's screenshot lacks it). The fixture gained `State.FocusRequests` (counts UIA SetFocus). Existing tests that click a field to focus it, or rely on click scrolling into view, now pass `--headed`: batch_test, esc_test (2 Batches), click_test (TestClickScrollsOffscreenElementIntoView). Unverified assumption: the fixture's canvas/edit report an empty LegacyIAccessible default action. User run: `set WINUSE_E2E=1` then `go test -tags e2e -v -run "Headless|Headed|AgentCursor" ./test/e2e`, then the whole suite `go test -tags e2e -v ./test/e2e`.
- Design question raised to the user: a Headless click never moves focus, so `batch … click id:field then type-text …` stops at step 1 unless `--headed`. Proposal: a Batch with any Headed step runs every step Headed.
- 2026-11-02 orchestrator: first full run with the user's go-ahead (skipping the interactive TestEscStopsTheLongBatch): 96 pass, 4 fail. Two real bugs found and fixed:
  1. **Headless pattern calls stole the foreground.** `IUIAutomation2::AutoSetFocus` defaults to TRUE, so UIA calls the provider's SetFocus (which moves the foreground) before every Invoke/Toggle/SetValue. Traced in the fixture: SetFocus arrived 2–3 ms before each pattern call while winuse never called it. Reproduced on real Chrome with the new `TestHeadlessChromiumKeepsTheForeground`. Fix: `put_AutoSetFocus(FALSE)` (slot 59) at client creation in `internal/uia/thread.go`.
  2. **Modal dialog undetected after a Headless click.** A dialog opened in the background is never activated, so `GetLastActivePopup` returns the owner itself; `TestClickWhileModalDialogOpenFails` and `TestScreenshotOfBlockedWindowNamesDialog` failed. Fix: `win.modalDialog` falls back to `GetWindow(GW_ENABLEDPOPUP)`; Activate and HeadlessReady share it.
  The Agent Cursor capture check also compared against the old teal; it now uses `win.AgentCursorAccent()`. Recorded both in ADR 0010's Consequences.
