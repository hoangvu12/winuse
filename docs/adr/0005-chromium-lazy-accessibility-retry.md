# Chromium/Electron lazy-accessibility retry

Ticket 14 makes the Window States of Chrome, Edge and Electron apps include
their web content. The spec already decides the core behaviour
("a near-empty first read is retried after a short delay before being
reported as sparse"); this ADR records the details the spec leaves open.
Sources: `.notes/uia-go.md` §5 ("if the first read is near-empty, sleep
~300–500 ms and re-read once") and the spec's user stories 22 and 24.

## Context

Chromium enables its accessibility tree lazily: it switches the tree on
only when a UI Automation client first appears, which for winuse is the
first cache build of a read itself. The first read of a Chrome, Edge or
Electron window is therefore often nearly empty, and the web content
(document, links, buttons, inputs) shows up only in a read that happens
after the tree has been built. Electron windows with the tree never
switched on — like canvas apps — stay nearly empty for good, and the agent
must fall back to a screenshot (user story 22).

## Decision

**One retry, after a fixed short delay.** A first read that comes back
nearly empty (the walk's ≤ 3-uncut-elements note) is retried exactly once
after 500 ms. The delay is spent on the request's goroutine, not the COM
worker thread, and is bounded by the request deadline: if no time is left,
the first read stands.

**A second trigger, scoped to Chromium.** A Window whose root element's
ClassName starts with `Chrome_` (Chrome, Edge, Electron and the other
Chromium browsers all use `Chrome_WidgetWin_1`), or whose App executable is
a known Chromium browser, gets the same one retry when its tree is complete
but carries no document Element — the read may have caught Chromium's tree
mid-build, with the browser chrome present and the web content not yet
attached. A partial (cut) tree never triggers this: the document may simply
be past the cut. This signal is a robustness refinement beyond the spec's
near-empty rule; Win32 apps are unaffected.

**Notes.** The walk's nearly-empty note stays as it is (ticket 03) for a
sparse read that was not retried. A tree that is still nearly empty after
the retry carries a stronger note that mentions the retry and tells the
agent to request a screenshot — the actionable part the ticket asks for.

**Where it lives.** The policy is `readTree` in `internal/uia/retry.go`,
generic over a read function, so it is offline-testable; `Client.Capture`
feeds it the two reads (each a job on the COM thread, each replacing the
Window's live Elements in the store) and both `get-window-state` and
`record-tree` get the retry automatically. Sparseness is detected by the
walk's exact note text, which recorded fixtures preserve — one definition
serving live reads, recorded fixtures and the offline tests.

## Consequences

- The retry applies to every Window, not just Chromium's: a canvas app's
  read costs one extra near-empty read plus 500 ms. That is the price of
  not maintaining a Chromium detector as the primary signal; the note the
  agent receives afterwards is better for it.
- `ClassName` (30012) joins the cached property set; the root element's
  class is the Chromium detection signal. If a provider reports no class
  name, only the near-empty trigger and the exe-name list apply.
- A retry read that fails (the Window closed mid-retry, COM error) fails
  the command: a stale tree is worse than an honest error.
- Offline tests drive `readTree` with hand-authored recorded Chromium/
  Electron trees (`internal/uia/testdata/`, marked as hand-authored in
  their `source`); the live two-phase behaviour is the user-run e2e test
  (`test/e2e/chromium_test.go`, Edge and Chrome with an isolated profile
  over a local HTML page).
- `a11yRetryDelay` (500 ms) and the note texts are contracts; changing them
  means updating the recorded fixtures' note text in step.
