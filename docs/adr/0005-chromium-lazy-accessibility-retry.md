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
near-empty rule; Win32 apps are unaffected. *(Amended below: for a
Chromium Window this trigger and the near-empty one now start a settle
loop of up to 5 s instead of the one retry.)*

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
- This retry is for empty reads. A failed one has its own
  (reliability-council 04, `retryRoot`): a root read that fails with
  E_FAIL or a transport error while the Window still exists is read again
  up to twice, 40 ms apart, and a chunk that fails with E_FAIL once.
- Offline tests drive `readTree` with hand-authored recorded Chromium/
  Electron trees (`internal/uia/testdata/`, marked as hand-authored in
  their `source`); the live two-phase behaviour is the user-run e2e test
  (`test/e2e/chromium_test.go`, Edge and Chrome with an isolated profile
  over a local HTML page).
- `a11yRetryDelay` (500 ms) and the note texts are contracts; changing them
  means updating the recorded fixtures' note text in step.

## Amendment: a settle loop for a cold Chromium tree (2026-11-02)

Ticket 18 of `.scratch/headless-max/`. The Laptop probe measured that a
cold Chromium can take 10-25 s to build its UIA tree, and that a fully
covered cold Chrome never built its web tree (36 nodes for 14 s), while a
partly visible one settled in about 0.1 s. One retry after 500 ms is too
short for that, so for Chromium the one retry becomes a settle loop.

- **Trigger.** A Chromium Window (the `Chrome_` class or a known browser
  exe, as above), read whole (no partial-tree note or truncation), whose
  Document is not left to a Browser Connection (the walk did not cut it
  for a CDP Page), and whose web content is thin: fewer than 3 Elements
  under its Document Elements, or no Document at all. This replaces the
  "complete tree with no Document" trigger, and it also takes Chromium's
  nearly empty reads, so Electron and WebView2 go through it too. A
  Window with a built page is read once, as before.
- **Loop.** `readTree` reads again every 300 ms until the Documents'
  size has grown and then stopped growing, up to a settle budget
  (`DefaultChromiumSettle`, 5 s). The polls are counted, not timed, and
  bounded by the request deadline: each needs its interval, with 1 s for
  the last read and the 500 ms response margin left over. A poll whose
  read leaves the Document to the Page (the Browser Connection answered
  meanwhile) or comes back partial ends the loop. A poll that fails
  fails the command, as the retry does.
- **Note.** A tree still thin at the end carries "the page's
  accessibility tree is still thin; Chromium may not build it while the
  Window is fully covered. Connect the browser (winuse browser status
  says how) or uncover the Window; for an Electron or WebView2 App,
  request a screenshot". It replaces the walk's nearly-empty note, so the
  agent gets one note. A tree that grew but was still growing at the end
  is returned without a note.
- **Once per Window.** The Client remembers (by HWND) a Window whose
  settle ended thin, including one that grew a little and stopped under
  the threshold. Later thin reads of that Window get the one 500 ms retry
  instead of the loop, and still carry the thin-page note when they stay
  thin. A read of it that is not thin (built, or partial) clears the
  mark, so a Window that builds and later goes thin again settles again.
  A paged read leaves the mark alone. The memory holds at most 256
  Windows; when full, an arbitrary mark is dropped, which costs that
  Window one more settle. Closed Windows are not pruned one by one, and a
  reused HWND inherits a stale mark, which costs it one retry instead of a
  settle until a read finds it built.
- **Never activates or uncovers the Window** to make it build.
- **The non-Chromium path is unchanged**: a nearly empty read gets one
  retry after 500 ms and the sparse-after-retry note.

Consequences:

- A Chromium Window without a Browser Connection that never builds a
  Document, or legitimately shows an almost empty page (Chrome's own
  popups, menus and bubbles, `about:blank`, an empty new tab, a
  canvas-only Electron App), costs the full 5 s on its first read, and
  then 500 ms per read, as before this amendment. It gets the thin note,
  whose "may" covers that case, in place of the sparse-after-retry note.
  Without a covered-Window signal in the walk, size is the only cue.
- `readTree` takes the Window's mark (`thinMark`, from the Client's
  `thinWindows`; the zero mark remembers nothing) and the settle budget
  as parameters. `CaptureFrame`
  passes `DefaultChromiumSettle`; the caller's `--timeout` from
  desktop-parity ticket 14 can pass a wider one there, and the request
  deadline still bounds it.
- `chromiumSettleEvery` (300 ms), `DefaultChromiumSettle` (5 s),
  `thinDocumentNodes` (3) and the note text are pinned by
  `internal/uia/retry_test.go`. None of them has been measured live; the
  user-run e2e test `test/e2e/electron_settle_test.go` reads a covered
  Electron App cold.
