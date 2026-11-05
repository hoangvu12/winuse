# 07: The Page read in one burst, marks from DOMSnapshot

Status: ready-for-human (done on branch `speed-page`; the live check is to run)

**From:** research recommendation 7 (`.scratch/research-speed/report.md`,
`notes-oss-browser.md`: browser-use's parallel burst and `captureSnapshot`,
agent-browser's N+5 shape, Playwright's frames in parallel). ADR 0012 is
amended ("the Page read in one burst, marks from DOMSnapshot").

**What to build:** a warm read of a Page (`internal/page/reader.go`) made
about 10 CDP calls one after another, then the `clickables.js` pass:
`Runtime.evaluate`, `Runtime.getProperties`, and one `DOM.describeNode` per
mark, 16 at a time, then `Runtime.releaseObjectGroup`. Each iframe was then
read the same way, one iframe after another. `DOM.enable` and
`Accessibility.enable` were sent before every read.

## Step 1 (S): concurrency, and the domains once per session

Commit `4c5da92`, proven on its own by `TestReadOverlapsItsRoundTrips`,
`TestReadTurnsDomainsOnOnce` (`readbench_test.go`) and every earlier test.

- [x] `cdp.Client.Send` puts a command on the wire and returns at once, and
  `Pending.Wait` collects the answer. A session's commands sent one after
  another from one goroutine run in that order in the browser, while their
  round trips overlap. `CallSession` is `Send` plus `Wait`, so its
  behaviour is unchanged.
- [x] `readDoc` sends a document's commands together: the domains (when
  needed), `Page.getFrameTree`, `Accessibility.getFullAXTree`. The dialog
  probe (`Ready`) runs alongside. If it fails, the read's commands are
  given up and its error is returned, as before.
- [x] Each cross-origin iframe's read (its `DOM.getFrameOwner` plus its
  document) starts with the Tab's own. The same-origin iframes the Tab's
  tree left out are read together right after it (owner plus the
  accessibility tree by frameId). They no longer turn the domains on again
  or ask for the frame tree again.
- [x] `windowTab` asks for its bounds alongside the Tab list: on CDP, every
  title-matching Tab's `Browser.getWindowForTarget` at once; through the
  extension, `Winuse.windows` alongside `Target.getTargets`.
- [x] `DOM.enable` and `Accessibility.enable` go once per session, with its
  first read, and again after `enableAgain` (4 min) since the session's
  last read. That covers the extension's silent idle detach (5 min, then
  a transparent re-attach on the next command with every domain off). A
  failed read forgets the session, so the next read turns the domains on
  again.

## Step 2 (M): `DOMSnapshot.captureSnapshot` in place of the pass

- [x] `snapshot.go`: one `captureSnapshot` per session, in the same burst,
  with computed styles cursor, visibility, display and text-transform only
  (no rects, no paint order). `marksOf` applies `clickables.js`'s rules to
  it:
  - elements under body in document order, light tree only;
  - its caps: 5000 looked at, 200 found, 80 UTF-16 units of text;
  - native tags, `isContentEditable` from the contenteditable attributes,
    widget roles (trimmed, lower-cased);
  - the outermost pointer: cursor:pointer whose parent's is not;
  - onclick, and a tabindex other than -1;
  - visibility hidden; no layout object or a 0-width or 0-height box;
  - inside `a[href]`, a button, an input, a select, a textarea, a summary
    or a widget role;
  - the text: an innerText approximation, then aria-label, then title.
- [x] innerText is approximated the way Chromium computes it:
  - the rendered text nodes in document order;
  - none whose style is not visible;
  - a break around block-level boxes and at `<br>`;
  - a `<select>`'s options' text;
  - layout text, or upper- or lowercase from text-transform;
  - no generated content (::before, ::marker) and no shadow tree;
  - none for an SVG element (innerText is undefined there, so the pass
    falls back to aria-label or title).
- [x] A same-origin iframe left out of the Tab's tree takes its marks from
  its own document in the Tab's snapshot: no isolated world, no script.
- [x] Fallback: a browser or bridge that refuses the snapshot gets the
  `clickables.js` pass, and that connection is not asked again. A refusal
  is no such method (-32601), the bridge's refusal (-32010), or "wasn't
  found" or "not allowed" in the message. Any other failure, or a snapshot
  without the document, falls back for that read only. `Reader.JSMarks`
  forces the pass (tests and the live comparison). `Doc.Snapshot` says
  where a document's marks came from.

## Proof (offline)

- `internal/page/snapshot_test.go`:
  - every rule above, case by case, with the caps, the text rules, a
    shadow tree, an SVG `g` and a `<select>`;
  - a document without a body;
  - the `display: contents` difference, pinned down;
  - the snapshot read and the pass give the same marks and the same
    rendered Elements on the benchmark page (200 clickables, a password
    field, a cross-origin iframe);
  - a warm read is 10 commands in 3 round trips, with no enable, no
    describeNode and no getProperties;
  - the fallback on a refusal and its memory, and a passing failure that is
    not taken for a refusal;
  - `Doc.Snapshot` set only for marks from the snapshot;
  - the extension's bridge (`bridgetest`), with the snapshot and with it
    refused;
  - the left-out same-origin iframe.
- `internal/page/readbench_test.go`: the domains go once per session and
  again after `enableAgain`; the pass's read overlaps its documents.
- `internal/page/tdom_test.go` builds `captureSnapshot` answers from small
  DOM trees.
- Every earlier reader test passes unchanged; their fake answers `{}` to
  the snapshot, which exercises the per-read fallback.
- `go vet ./...`, `go vet -tags e2e ./test/e2e` and `go test ./...` pass.

## Measured (offline)

`BenchmarkPageRead` (`internal/page/readbench_test.go`) uses a
`cdp.Transport` that answers from a synthetic page after a modelled round
trip. Each command reaches the browser after half of it, waits its turn
in its session, and its answer comes back after the other half. The page
has 200 role-less clickables in 40 sections, a link and a paragraph per
row, a password field, and a cross-origin iframe with 5 clickables. The
read is warm: the Tab is attached, as in the helper after its first read.
Answers cost no browser time, so the times are the read's own shape plus
its Go-side decoding, not the renderer's work.

`go test ./internal/page -run '^$' -bench PageRead -benchtime 20x -count 5`,
medians, on this machine (i9-12900HK):

| Read | Commands | Round trips in a row | Go side only | at 1 ms per round trip | at 4 ms per round trip |
|---|---|---|---|---|---|
| Before (main 34512dd) | 225 | 32 | 11.6 ms | 74 ms | 170 ms |
| Step 1 only (the pass, concurrent) | 221 | 21–23 | 13.5 ms | 44 ms | 109 ms |
| Step 2 (snapshot) | 10 | 3 | 11.6 ms | 19.7 ms | 27.5 ms |

- The "before" row was measured with the same benchmark on the unchanged
  reader.
- "Step 1 only" is the benchmark's `js` mode, which is the reader's
  fallback path.
- 1 ms is about a loopback CDP round trip with renderer work. 4 ms models
  the extension, which adds two processes each way. Both are models; the
  live check measures the real ones.
- The 3 round trips are `Target.getTargets`, `Browser.getWindowForTarget`,
  then everything else. Through the extension the first two are one round
  trip.
- Not measured offline: the renderer's own cost of `captureSnapshot` (one
  style and layout pass, then serializing every node) against
  `clickables.js`'s 5000 `getComputedStyle` calls. The live check times
  both reads.

## Differences in the printed Window State

- **Password fields in shadow trees, and in iframes the Tab's tree holds**,
  are now marked, so their values are never printed. The pass saw only
  its own document's light tree. Chrome masks a password's value as
  bullets anyway, which `value` already drops, so this is a guard.
- **Accepted, from the innerText approximation:**
  - Text a shadow tree renders in place of an element's children is not
    seen. The clickable's name falls back to its aria-label or title, as
    if it had no text. This assumes Chromium's innerText walks the DOM
    children; the live page has a shadow host to check it.
  - A `white-space: pre` text of spaces only counts as no text.
  - A `::first-letter` split falls back to the node's own text, with
    upper- or lowercase applied, but not `capitalize`.
- **Accepted, `display: contents`:** an element under a `display: contents`
  parent that sets `cursor: pointer` itself is compared with the nearest
  boxed ancestor's cursor (the snapshot has no style for a node without a
  box), so it is marked where the pass would not mark it.
  `TestSnapshotMarksDisplayContents` pins this down; the live page leaves
  it out.
- **`isContentEditable`** is read from the contenteditable attributes, so a
  page in `designMode` is not seen as editable.

## The extension path

- `chrome.debugger`'s documented domains include DOMSnapshot, DOM,
  Accessibility, Page and Runtime
  (developer.chrome.com/docs/extensions/reference/api/debugger).
  `bridge.Refusal` and background.js do not block it, so the snapshot goes
  through unchanged. The extension is not changed.
- Brave: unverified. A Brave that refused the domain would get the pass,
  through the fallback.
- `DOMSnapshot.enable` is not sent. That `captureSnapshot` answers without
  it is unverified until the live check. A failure there would show as
  "did not come from DOMSnapshot" in the live test.
- Found, not changed: after the extension's silent idle re-attach, the
  Tab's `Target.setAutoAttach` and its dialog watch are gone too, and
  winuse's cached session does not know. This branch turns the domains on
  again after `enableAgain`, but cross-origin iframes stay unread until the
  session is dropped. That is an older gap, worth its own issue.

## Also

- `internal/helper/tabtarget_test.go`: `TestUsersTabComesBackWhenTheGuardMisses`
  failed about 1 run in 10 on main too. The helper selects the user's Tab,
  then sends `Winuse.adoptTab`, and the test checked the group as soon as
  it saw the selection. It now waits for both (50 of 50 pass).

## Live check (the user's to run)

- [ ] `TestPageReadSnapshot` (in-process, headless Chrome and Edge on
  throwaway profiles, no helper): no differences on
  `testdata/page-marks.html` and `browser-pages.html` with its
  cross-origin iframe; note both reads' median times.
- [ ] Optionally `WINUSE_E2E_PAGE_URLS` with a link-heavy news page, a
  modal-heavy app and a long list (research experiment 8).
- [ ] `TestPageReadSnapshotExtension` through each connected extension
  (Chrome, Edge, Brave), with `WINUSE_E2E_USER_BROWSER=1`.
- [ ] A `get-window-state` of a browser Window, before and after, timed
  (research experiment 8: a page with 200 or more `cursor:pointer`
  elements, direct CDP against the extension).

## Comments
