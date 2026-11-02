# 18: Longer settle for a cold Chromium UIA tree

**What to build:** when a Chromium Window has no Browser Connection (the user's own browser without the extension, Electron, WebView2), wait longer for its UIA tree to build, within the request budget, and say what happened.

ADR 0005 retries once after 500 ms. The Laptop probe measured that a cold Chromium can take 10-25 s, and a fully covered cold Chrome never built its web tree (36 nodes for 14 s), while one that was partly visible settled in about 0.1 s. Ticket 04 fixes browsers winuse launches; this ticket covers the rest.

- Poll the document subtree size every 250-500 ms until it grows and then stops growing, up to a settle budget (proposal 5 s, or the caller's `--timeout` from desktop-parity 14).
- If it never settles: return what is there with a note: "the page's accessibility tree is still thin; Chromium may not build it while the Window is fully covered. Connect the browser (winuse browser ...) or uncover the Window."
- Never activate or un-cover the Window to make it build.

**Blocked by:** none

**Status:** done (amends ADR 0005)

- [x] Settle loop with growth detection
- [x] The note
- [x] Offline tests with a faked growing tree
- [x] Opt-in e2e: a covered Electron App read cold
- [x] ADR 0005 amendment

## Comments

### 2026-11-02: landed

- `internal/uia/retry.go`: `readTree` now settles a thin Chromium tree before anything else. The trigger (`worthSettle`) is a Chromium Window (the `Chrome_` class or a known browser exe), read whole, whose Document was not cut for a Browser Connection's Page (`readResult.paged`, set by `CaptureFrame` when the walk cut a Document), and whose web content is thin (`documentSize` < `thinDocumentNodes` = 3 Elements under its Documents, nested iframe Documents counted once; no Document counts as 0). `settleTree` polls every `chromiumSettleEvery` (300 ms) until the size has grown and then stopped growing. A paged or partial poll ends it, and so does a failed sleep (the request ending). A poll that fails fails the read. A tree still thin at the end gets `thinPageNote` ("the page's accessibility tree is still thin; Chromium may not build it while the Window is fully covered. Connect the browser (winuse browser status says how) or uncover the Window; for an Electron or WebView2 App, request a screenshot"). The note replaces the walk's nearly-empty note when there is one, so the agent gets one note. A tree that grew past thin but was still growing at the end is returned without a note. Nothing activates or uncovers the Window.
- Budget: `readTree` takes the settle budget as a parameter, and `CaptureFrame` passes `DefaultChromiumSettle` (5 s). That is the seam where desktop-parity ticket 14's `--timeout` can pass a wider one. `settlePolls` counts polls, not time, so the faked sleep keeps the tests instant. It also bounds them by the request deadline: each poll needs its interval, with `settleReadReserve` (1 s) and the 500 ms response margin left over.
- The non-Chromium path is unchanged: one retry after 500 ms and the sparse-after-retry note. The old "complete Chromium tree with no Document" trigger is now part of the settle (no Document is thin). `hasDocument` and `worthRetry` are gone.
- Tests (`internal/uia/retry_test.go`): a faked Chrome tree (`chromeRead(n)`) that grows and then stops; one that never settles (thin note, once, total sleep within 5 s); a sparse Electron window (thin note replaces the nearly-empty note); still growing at the end (no note); a paged poll and a paged first read; a partial poll; a warm page; a poll error; an interrupted sleep; the request deadline bounding the polls; a wider caller settle; `documentSize` cases; the 250-500 ms, 5 s and note contracts. The existing one-retry tests now use a non-Chromium canvas class. The Chromium ones expect the settle (three reads: grew, then stopped).
- Opt-in e2e `test/e2e/electron_settle_test.go`, `TestCoveredElectronReadCold`. It starts the prebuilt Electron binary named by the new `WINUSE_E2E_ELECTRON` (and skips without it) with an isolated profile on `testdata/chromium-fixture.html`. It covers the Window with the fixture as soon as it exists and runs one cold `get-window-state`. The read must answer within the 10 s request budget and leave the cover in front. It must show either the page's button or the thin note (not both), and the note must come after about 5 s. Described in `test/e2e/README.md`. **It is written and passes `go vet -tags e2e ./test/e2e`, but it has not been run.** Nothing in this ticket was measured live.
- ADR 0005: an inline amendment marker on the second-trigger paragraph and a dated "Amendment" section (trigger, loop, note, budget seam, consequences).
- Docs: the get-window-state usage line, SKILL.md (after the nearly-empty paragraph), CONTEXT.md (Browser Connection), README (Browsers) and docs/codex-parity.md (get_window_state prose; no row status changed).
- Known cost, recorded in the ADR: a Chromium Window with no Browser Connection that really shows an almost empty page (`about:blank`) spends the full 5 s on every read and gets the thin note. A canvas-only Electron App spends 5 s where it spent 500 ms. Size is the only cue the walk has; a covered-Window signal could cut this, but it needs a live measurement. With `--screenshot`, the capture runs after a settle that may use up to about 5 s of the 10 s request.
