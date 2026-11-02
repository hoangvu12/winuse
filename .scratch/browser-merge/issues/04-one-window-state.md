# 04: One Window State for browser Windows

**What to build:**

- **Window to Tab mapping** using the signals the probe settled. An ambiguous match is an error.
- **A CDP accessibility-tree snapshot of the active Tab**, rendered in the Window State grammar, replacing the UIA document subtree. It includes a cursor-pointer pass and cross-origin iframes through flattened auto-attach.
- **Element Indexes numbered across both parts**, each with its origin, and one staleness message.
- **Fallback to the UIA-only read**, with a note, when there is no Browser Connection.

See spec: "One Window State".

**Blocked by:** 03

**Status:** done

- [x] Offline: rendering from recorded `getFullAXTree` fixtures, matching over faked bounds and titles, origin-tagged indexes, staleness
- [x] The fallback path is unchanged from today's output when there is no connection

## Comments

### 2026-11-02: probe decisions (accepted by the user)

From `../probe-results.md` (Chrome 155 at 100%, 150% and 175%; Edge not measured):

- **Mapping rule.** Take the browser's `Chrome_WidgetWin_1` Windows owned by the connected browser's process. For each, candidates are the page targets whose `Browser.getWindowForTarget` bounds, times the Window's DPI scale (DPI / 96), equal the Window's GetWindowRect within a few px, intersected with the targets whose title equals the Window title minus the " - <browser>" suffix. Exactly one candidate is the active Tab; zero or several is an error naming the candidates. Neither signal alone is enough: windows opened with `newWindow:true` share the same default bounds, and a background tab can repeat a title. Both together matched 3 of 3 Windows at every scale.
- **Extension.** `Browser.getWindowForTarget` is refused there, so add `chrome.windows.get` bounds to the bridge (it reports `windowId` today but no bounds) and apply the same rule. windowIds were stable; only one user window was open, so test several windows offline with fakes.
- **Covered and minimized.** `Accessibility.getFullAXTree` worked uncovered, fully covered and minimized, with focus emulation on and no extra launch flag. Do not rely on a fixed node count between reads (13 then 16 on the same page).
- **DOM ids reach UIA as AutomationId**, so a UIA document Element and its CDP node can be cross-checked if needed.

### 2026-11-02: landed

**What a browser Window's State is now.** `get-window-state` (with or without `--screenshot`) on a Window of class `Chrome_WidgetWin_1` whose process image is a known browser reads two things at once:
- the frame through UI Automation, exactly as before;
- the active Tab's Page through that browser's Browser Connection, with its own 4 s sub-budget (`pageBudget`, capped at the request deadline minus 1 s).

The Page replaces the UIA descendants of the frame's document Element. The document Element's own line stays: it is a UIA Element, and ticket 05's cursor transform starts from its bounds. Indexes run depth-first across both parts.

Every other Window, Electron apps with `Chrome_WidgetWin_1` included, is read exactly as before, with no note. Screenshots are unchanged. `record-tree` stays UIA only.

**Fallback.** On any of these, the output is today's UIA read, byte for byte, plus one `Note:` line. Render adds the tail's blank line first when the tree had no tail.

| Cause | Note |
|---|---|
| no connection | `Note: the Page is read through UI Automation because no Browser Connection is open to Chrome; run winuse browser status chrome.` |
| the connection is to another process of that browser | "this Window belongs to a Chrome process that has no Browser Connection (winuse is connected to Chrome process N); run winuse browser status chrome" |
| no Tab matches | "no Tab of the Browser Connection matches this Window's title and bounds" |
| several Tabs match | "several Tabs match this Window's title and bounds: "A" (target 1234abcd), "B" (target ...)" |
| the read failed | "reading it through the Browser Connection failed: <error>" |
| the sub-budget ran out | "reading it through the Browser Connection took longer than 4s" |
| no document Element in the frame | "UI Automation shows no document for the active Tab" |
| several document Elements, none named after the Tab | "UI Automation shows several documents and none is named after the active Tab" |
| the Window was read again before the splice | "the Window was read again while its Page was read" |

**Decision to confirm.** The ticket says an ambiguous match is "an error naming the candidates". The mapping returns exactly that error (`*page.AmbiguousTabError`), but `get-window-state` turns it into the fallback note above instead of failing the whole read. The agent still sees the Window, and nothing is guessed. Overrule this if a hard failure is wanted; it is one `case` in `readWindowTree`.

**Per file.**
- `internal/page` (new package):
  - `page.go`: the types.
    - `Raw` is one read, and also the fixture format: `Main Doc`, `Frames []Frame{Owner; Doc}`, `Client`.
    - `Doc` holds `SessionID`, `FrameID`, `Loaders` (frame id to loaderId), `Nodes []AXNode` and `Marks *Marks`.
    - `AXNode`, `Unavailable{Reason}`, `FallbackNote(reason)`, `ErrStale`.
  - `match.go`: the pure mapping.
    - `MatchTab(Geometry, browser, []Tab) (Tab, error)` returns `*NoTabError` or `*AmbiguousTabError`.
    - Bounds: DIP x DPI/96 against GetWindowRect within 8 px. A minimized Window matches `windowState: minimized`. A maximized one matches a maximized or fullscreen browser window whose centre lies in its rect, because the invisible borders make GetWindowRect overhang.
    - Title: `TitleMatches` accepts the tab title followed by " - <brand>", with at most one more part (a profile) before or after the brand, and Edge's " and N more pages". Zero-width characters are ignored. The brands are Google Chrome, Chromium, Microsoft Edge, Brave, Vivaldi and Opera.
    - When a Tab's `active` is known (the extension), a background Tab is never a candidate.
  - `roles.go`: CDP role to the UIA role words. Wrappers (generic, none, paragraph, row, LabelText, ...) never get a line and their children take their place. Landmarks and groups get a line only when named. InlineTextBox, LineBreak, ListMarker and separators are dropped. Three role words UIA has no equivalent for: `heading`, `dialog` and `clickable`.
  - `build.go`: `Build(raw, Limits) Result`, pure.
    - Ignored nodes are hoisted. Text that repeats its nearest line's name is dropped. A LabelText whose text is a field's name is dropped.
    - States: disabled; checked or pressed as on, off or indeterminate; expanded or collapsed; selectable and selected for options, tabs, tree items and radios; settable (string for edit and editable combo boxes, float for slider and spinner, none when readonly).
    - `Value:` for edit, combo box, spinner and slider. Never for a password field (`Marks.Passwords`), never for a value made of mask characters, and never for any text field when the cursor-pointer pass failed.
    - Secondary Actions: Expand or Collapse, Toggle, Select, in the canonical order.
    - Iframes in `Frames` are inlined under their iframe line (`pane`) as a `document` line, one level deep.
    - The caps are ADR 0004's, shared with the frame: element cap 3000 in total, depth 48 counted from the window, 1000 children per Element. The truncation suffix and the reasons are worded exactly like the walk's.
  - `merge.go`: `Merge(frame, raw) (*Merged, error)`.
    - It finds the document Element: the one named after the Tab's title, else the only one.
    - It builds with the caps the frame left, splices, and merges the partial note.
    - Focus: the Page's focused Element when the frame's focus was in the document or nowhere; the frame's focus otherwise (the address bar, the infobar), shifted past the Page.
    - `Merged{Tree, Doc, End, Origins}`.
  - `reader.go`: the live glue.
    - `Reader.Read(ctx, *browser.Connection, Geometry) (*Raw, error)`.
    - `FindTab` lists `Target.getTargets`, skips internal pages, and reads `Browser.getWindowForTarget` only for title candidates. On the extension it reads `Winuse.windows` instead.
    - One attached session per (connection, Tab) is cached, through ticket 01's `AttachPage`, so focus emulation is on. On first attach, `Target.setAutoAttach {autoAttach:true, waitForDebuggerOnStart:false, flatten:true}` runs on the Tab session. A goroutine keeps the iframe target to session table from `attachedToTarget` and `detachedFromTarget`, and sends `runIfWaitingForDebugger` to a waiting child. The first read waits 100 ms for the existing iframes' events.
    - A detached session is attached again once.
    - Per document: `DOM.enable`, `Accessibility.enable`, `Page.getFrameTree` (the loaders), `Accessibility.getFullAXTree`, then the cursor-pointer pass.
    - Iframes, one level deep: a child frame with an auto-attached session is read over it. A same-origin child frame missing from the main tree is read with `getFullAXTree {frameId}`, with the pass in an isolated world. Frames already in the main tree are left alone. `DOM.getFrameOwner` gives the iframe element.
    - `Reason(err)` words the note.
  - `clickables.js` (embedded): the cursor-pointer pass. It is our own code; nothing was copied from agent-browser, so there is no `third_party/agent-browser`. It returns `[el, kind, text, ...]`:
    - every `input[type=password]` as "p";
    - up to 200 elements as "c": outermost cursor:pointer, onclick, or tabindex >= 0 elements with no native or widget role, visible and non-empty, not inside a widget, with innerText up to 80 characters.

    The Go side reads the array with `Runtime.getProperties`, turns the elements into backendNodeIds with parallel `DOM.describeNode` calls, and releases the object group.
  - `origin.go`: `Origin` and the stale checks (below).
- `internal/uia/splice.go` (new):
  - `(*Client).SplicePage(ctx, hwnd, base, doc, end, merged, origins map[int]any) error` replaces the live store's UIA handles for the document's descendants with nil handles plus origins. The removed handles are released on the worker. It refuses with `ErrSuperseded` unless `base` is the latest stored read (the same backing array).
  - `(*Client).ElementOrigin(hwnd, index) (any, error)`: nil for UIA, the origin for a Page Element, `ErrNoState` or `ErrStale` otherwise.
  - `ErrPageElement`.
- `internal/helper/pagestate.go` (new):
  - `PageReader` (Services.Pages) and `PageSplicer`.
  - `readWindowTree`, used by get-window-state and its `--screenshot` path.
  - `livePages`, over `*browser.Manager`'s `Connection` and `Identify`, with `win.WindowRect`, `win.WindowDPI` and `Services.Maximized`.
  - `pageOrigin`, `checkPageOrigin`, `refusePageElement` and `pageElementError`.
- `internal/win/browserwindow.go` (new): `WindowRect` and `WindowDPI`. Both are reads only, so no guard.
- `internal/browser/window.go` (new): `(*Manager).Identify(exePath)`.

**The resolver ticket 05 routes on.** Everything is in package `helper`; the origin type is `page.Origin`.
- `pageOrigin(s Services, w win.WindowInfo, index int) (*page.Origin, *Error)`:
  - nil for a UIA Element;
  - the origin for a Page Element;
  - the usual `elementError` for no stored state or an index out of range.

  It reads `Services.Actions.(PageSplicer).ElementOrigin`.
- `page.Origin` has these fields: `Browser`, `Client *cdp.Client` (route on exactly this connection), `TargetID`, `SessionID` (the Tab's session, or the iframe's child session), `FrameID`, `LoaderID`, `BackendNodeID int64`, `Owner *page.Owner{SessionID, BackendNodeID, FrameID, LoaderID}` (set for cross-origin iframe nodes), and `DocIndex` (the UIA document Element's index).
- `checkPageOrigin(ctx, w, index, o) *Error` calls `o.Check(ctx)`. A `page.ErrStale` becomes exactly `elementError(uia.ErrStale)`: "element N is not available in the cached Window State for window W" with the "run winuse get-window-state W again" hint. `Check` is stale when:
  - the connection ended or there is none;
  - the frame's loaderId changed (navigation) or the frame is gone (`Page.getFrameTree`);
  - `DOM.resolveNode {backendNodeId}` fails;
  - `this.isConnected` is false.

  For an iframe node, the owner iframe element is checked first. Detached-session errors map to stale. Anything else passes through.
- `refusePageElement` is called from `elementTarget.resolve` (refs.go). Today it fails every Element Action given a Page Element, by index or by `name:`, with "element N is in the Page of window W, and winuse cannot act on Page Elements yet". The hint points to `--screenshot` plus `click @x,y`. Ticket 05 replaces this call with routing.
  - Headless paths refuse before anything is sent.
  - The Headed click path refuses after the activation, because resolve runs inside `runAction`'s step.
  - A UIA lookup of a Page index (`uia.lookup`) fails with `ErrPageElement` instead of using a nil handle.

**The CDP rect for an index.** `o.Rect(ctx) (page.Rect, error)`:
- `DOM.resolveNode`, then `Runtime.callFunctionOn` `getBoundingClientRect()` in the node's own session. The result is `{Left, Top, Width, Height}` in CSS px relative to the Tab's viewport, plus `DPR = window.devicePixelRatio`.
- For a cross-origin iframe node, it adds the iframe element's content-box offset (rect plus clientLeft or clientTop plus padding) read in the Tab session, and takes the DPR from there.
- It scrolls nothing. Ticket 05 does `DOM.scrollIntoViewIfNeeded` first, then: screen px = UIA document Element top-left (the Element at `o.DocIndex`) + (Left, Top) x DPR, as the probe measured.

**Bridge additions.**
- `bridge.MethodWindows = "Winuse.windows"` returns `{windows:[{windowId,left,top,width,height,state}]}` from `chrome.windows.getAll({windowTypes:["normal","popup","app"]})`. The extension only reads `chrome.windows`, and the header comment says so. The existing test that forbids `chrome.windows.update` and `focused: true` still passes.
- The extension manifest is bumped to 0.1.1. The content hash `bridge.Build()` changes by itself, so ext status self-updates.
- The fake extension has `Windows []bridgetest.Window` and answers `Winuse.windows`.
- `node --check` passes on background.js and clickables.js.

**Tests.**
- page:
  - golden Window States for 4 hand-authored `getFullAXTree` reads merged into a faked Chrome frame (`testdata/chrome-frame.json`): `login-form`, `clickable-div`, `cross-origin-iframe` and `password-field`. `go test ./internal/page -update` rewrites them.
  - password values never rendered (marked, masked, or with the pass failed);
  - origins (session, frame, loader, owner, DocIndex);
  - focus rules;
  - document choice;
  - the element, children and depth caps, with the walk's suffix and notes;
  - the frame around the document kept in place.
- page mapping:
  - the probe's shape (3 windows, two sharing default bounds, a background tab repeating a title) maps 3 of 3 at 96, 144 and 168 DPI;
  - unscaled bounds at 150% match nothing;
  - ambiguous with the exact error text, and settled by `active`;
  - none;
  - minimized and maximized;
  - 17 title cases (Chrome profile, Edge profile and "more pages", zero-width space, Brave, Chromium, Vivaldi, negatives).
- page live, over `cdptest` and `bridgetest`:
  - the read over a fake endpoint renders the same golden as the fixture, with the iframe through the auto-attached `CHILD1`;
  - focus emulation sent; no bringToFront or activateTarget;
  - the same-origin frame not re-read;
  - one attach for three reads, and a re-attach after "Session with given id not found";
  - no match, so nothing attached;
  - marks parsing, and an unnamed password field dropping all values;
  - the extension variant: 3 windows through `Winuse.windows`, with background tabs ruled out by `active` and no `Browser.*` reaching the extension;
  - `Origin.Check`: live, node gone, out of document, frame gone, navigated, iframe owner navigated, connection closed;
  - `Origin.Rect`, plain and with the iframe offset.
- uia: the splice numbers across both parts, releases only the replaced handles, keeps later handles at their new index, makes lookup and ElementPoint of a Page index fail with `ErrPageElement`, resolves `name:` across both parts, and lets a new read clear the origins. A superseded splice changes nothing.
- helper:
  - the merged get-window-state equals `page.Merge`'s render, with origins per index;
  - the fallback equals today's output plus exactly the note, with and without a tail, for each cause;
  - non-browser Windows and Electron are untouched, with no Page read;
  - the sub-budget timeout gives a note and leaves the request budget;
  - Page indexes refused by index, by `name:` and through the click command (no point and no click sent), while frame indexes still resolve;
  - the stale message is identical to UIA's;
  - livePages' reasons;
  - `newLivePages` needs the Manager.
- Checks run: `go build ./...`, `go vet ./...`, `go vet -tags e2e ./test/e2e` and `go test ./...` are all green. The page tests pass 5 times in a row. go.mod is unchanged. All touched files have LF endings.

**headless-parity-modified files I edited (minimal):**
- `internal/helper/state.go`: the `Services.Pages` field, its default after `Browser`, and get-window-state's Capture plus Render now calling `readWindowTree`.
- `internal/helper/screenshot.go`: the same 3 lines in `windowStateScreenshot`.
- `internal/uia/action.go`: `lookup` returns `ErrPageElement` for a nil (Page) handle.

Also edited, but not on the headless-parity list:
- `internal/helper/refs.go`: `elementTarget.resolve` calls `refusePageElement`.
- `internal/uia/thread.go`: one `origins` field on `windowState`.
- `internal/bridge/bridge.go`, `extension/background.js`, `extension/manifest.json` and `bridgetest/extension.go`.

No CONTEXT.md or ADR change: the grammar is unchanged, and `heading`, `dialog` and `clickable` are new role words, not new terms.

**Risks to measure live:**
- Mixed-DPI multi-monitor setups. DIP x the Window's DPI was measured on one monitor only. Chrome's DIP space across monitors with different scales may be offset, so the bounds may miss.
- Maximized and snapped windows. The maximized rule (state plus centre) and the 8 px tolerance for snapped windows are untested live.
- Edge titles: profile names, "and N more pages", and localized brand strings. Edge was never measured.
- Whether `Page.getFrameTree` on the Tab lists out-of-process iframes. Both paths are handled, but which one fires is unmeasured. Also whether `setAutoAttach` through the extension produces `attachedToTarget` with a `sessionId` (ticket 02's open risk), and whether the 100 ms settle catches existing iframes.
- Whether `getFullAXTree` includes same-origin iframe content in the main tree (assumed by the spec; a fallback per frame exists).
- Cost:
  - The UIA walk still reads the whole document subtree that is then thrown away, so dense pages are still slow on the UIA side. A follow-up could stop the walk at the document Element for browser Windows.
  - `getComputedStyle` over up to 5000 elements in the pointer pass.
  - `DOM.enable` and `Accessibility.enable` stay on for the attached Tab.
- The extension route attaches `chrome.debugger` to the active Tab on a mere read, which shows the "started debugging" bar. That bar is a frame Element, and it shifts the UIA tree.
- A connection to the extension (the user's Chrome) while the agent reads a winuse-launched Chrome Window, or the other way round, gives the "another process" note. One connection per browser name cannot serve both.
- Page Elements carry no `ID:` (the AX tree has no DOM id), so `id:` references do not reach Page Elements yet.
- The Headed click path activates the Window before refusing a Page index (until ticket 05).

### 2026-11-02: the UIA walk stops at the document

**What changed.** For a browser Window, the UI Automation walk no longer reads what the Page replaces. Once the Page read ends well, the walk keeps the document Element's own line and skips its descendants. The document is the one `page.Merge` picks by name: the first document, in index order, named after the Tab's title.

The walk never waits for the Page. Until the Page read ends, it reads on, into the document too. When the answer comes:
- If the walk has not reached the document yet, it skips it at entry. No chunk is read for it or under it.
- If the walk is inside the document, it stops there and climbs back up to the document. Everything it read under the document is dropped, and their handles are released. The partial-tree reasons and truncation markers go back to what they were when the walk reached the document. A focus read under the document moves to the document. The walk then goes on with the frame after it (the infobar).
- If the walk already read the document whole, nothing changes: the read and the merge are exactly today's.

A failed Page read never cuts anything, so each fallback row finds today's whole walk, byte for byte, with no extra walk.

**Per file.**
- `internal/uia/walk.go`:
  - `DocumentCut func(name string) (cut, known bool)`;
  - `WalkConfig.Cut` and `WalkResult.Cut` (the cut document's index, or -1);
  - the walker's cut state: `docMark`, `expand` (split out of `visit`), `cutPending`, `decideCut` and `cutAt`.

  The cut is asked before every chunk read while a Document is open, and again when a Document is reached. `decideCut` scans the Documents seen so far in index order. If the first one named is still open, it is cut. If that one is already closed, the cut is settled and nothing is cut, so a later Document with the same name stays whole (Merge would pick the earlier one). A cut tree never gets the nearly-empty note.
- `internal/uia/capture.go`: `captureResult.cut`.
- `internal/uia/thread.go`:
  - `(*Client).CaptureFrame(ctx, hwnd, title, app, budget, cut) (tree, at, err)`, which sets `cfg.Cut` on every read, the lazy-accessibility retry included. `at` comes from the read that is returned.
  - `Capture` is now `CaptureFrame` with a nil cut, so it is unchanged.
- `internal/page/merge.go`:
  - `NamedAfter(name, tabTitle)`, the document-name rule;
  - `findDocument` uses it, so the walk and Merge cannot drift apart.
- `internal/helper/pagestate.go`:
  - the header paragraph;
  - `readWindowTree` starts the Page read as before, then reads the frame through `readFrame` with a `pageCut`;
  - `FrameReader` (`*uia.Client`);
  - `readFrame`, which cuts only when the tree reader is both a `FrameReader` and a `PageSplicer` and reads whole otherwise;
  - `pageCut` (an `atomic.Pointer[string]` that `settle` stores when the Page read ends: the Tab's title, or "" on failure).

  Every fallback goes through one `fallback(reason)`. If the walk had cut (only possible after a good Page read, so only when Merge or `SplicePage` fails, e.g. superseded), `fallback` reads the Window whole again first, so the agent never gets a read missing the document's content. A defensive branch does the same if Merge ever picks another document than the cut one. If the sub-budget fires after the walk cut, the result is taken from `done` (it is guaranteed to be on its way) instead of reporting a timeout.

**Decisions.**
- **No waiting at the document.** I chose reading on, then dropping what was read, over pausing the walk at the document until the Page read ends. A pause would hold the one UIA worker idle, and other Windows' reads queue behind it. It would also eat the walk's own budget, so a late failure would no longer give today's output byte for byte. Reading on costs nothing on failure, and on success it costs only the UIA work done before the Page lands.
- **No second walk on the common fallbacks.** A failed Page read cuts nothing, so there is no cheap connection check and no re-walk for the no-connection, no-Tab, read-failed, timeout, no-document and several-documents rows. A second, whole walk happens only after a cut when Merge or the splice then fails. That is rare, and it stays inside the request budget.
- **Only the document Merge picks is cut.**
  - A side panel or other document named otherwise is read whole.
  - A document nested in the cut one goes with it.
  - If the only document is not named after the Tab (an untitled page, a title changed between the reads), it is read whole and merged as today. This is correct, but it saves nothing.
  - Several documents with none named after the Tab: nothing is cut, and the fallback is today's.
- **Concurrency.** The Page read already ran on its own goroutine alongside the UIA job. The only new crossing is `pageCut.decide`, called on the worker thread. It does an atomic load and a string compare, and it never blocks and never touches COM.
- **Caps.** The cut frame is smaller, so `page.Merge` gives the Page more of the 3000-Element cap (`outside` is the cut frame's length, the same number as before when no cap hit). A dense document no longer pushes the frame after it (the infobar) past the element cap or the time budget. Partial notes that came only from inside the document (unreadable children, a cap inside it) are gone, because that content is replaced.
- `record-tree` calls `Capture` (whole). `--screenshot` goes through `readWindowTree`, so it gets the cut. Non-browser Windows and Electron (no browser name) call `Capture` as before.

**Tests.**
- uia walk:
  - at entry, no chunk is read for the document or under it (counted on the fake);
  - a late answer drops what was read under the document (4 handles), restores the notes (an unreadable reason under it is gone) and moves the focus to the document;
  - only the first document named after the Tab is cut (a side panel stays whole, a nested document goes with it, a later document of the same name stays whole);
  - never known, named nowhere and an empty title each give today's walk;
  - an answer after the document closed cuts nothing;
  - the element cap and the time budget (fake clock) are hit by the whole walk, not by the cut one;
  - a cut tree gets no nearly-empty note.
- uia splice: `SplicePage` into a cut read (doc to doc+1). Nothing is released, origins are right, the infobar keeps its handle at its new index, `name:` resolves, and `StoredElements` shows the merged list.
- page: the 4 goldens merge identically from the cut frame, with equal Doc, origins and `NamedAfter` cases.
- helper (`pagecut_test.go`, new): `frameSource` serves `chrome-frame.json` to the real `uia.Walk` and counts chunks. Its whole walk renders the fixture exactly.
  - The 4 goldens, both at entry and late (the gated Page read ends during the document's chunk). Under the document, at most the document's own chunk is read, and the origins are right.
  - `name:Email`, `name:Cancel` after the Page, and an index past the end, over the cut read.
  - Each fallback cause (no connection, no Tab, ambiguous, read failed, out of time, no document, several documents) gives today's output plus the note, from one whole walk.
  - The only document not named after the Tab merges as today.
  - A superseded splice after a cut reads whole again and gives today's output plus the note. The stored state is the whole UIA read.
  - `--screenshot` gives the golden plus the Screenshot line from a cut walk. `record-tree` does one whole walk (14 Elements).
  - Electron and Notepad: no Page read and no `CaptureFrame`. The Electron Window's one walk is a whole `Capture`.
  - A dense document (3000 Elements): merged, it gives the golden with 10 chunks read. On a fallback, it gives today's capped read with its cap note.
- Checks run: `go build ./...`, `go vet ./...`, `go vet -tags e2e ./test/e2e` and `go test ./...` are green. Once, the whole run failed `TestTabSwitchOnALaunchedBrowserIsHeaded` (tabs_test.go, which never reads a Window State). It passes alone 3 times and in two more full helper runs, so it is a flake in the tab work, not this change. go.mod is unchanged. All touched files have LF endings.

**Files edited.**
- headless-parity-modified: `internal/uia/thread.go` (Capture now calls CaptureFrame, which is new).
- shared with the concurrent worker: `internal/helper/pagestate.go`. Only the header paragraph, the `sync/atomic` import, `readWindowTree`, and the new `FrameReader`, `readFrame` and `pageCut` placed after it. `livePages`, the connection lookup and `pageWindow`/`tabWindow` are untouched.
- others: `internal/uia/walk.go`, `capture.go`, `walk_test.go`, `splice_test.go`, `internal/page/merge.go`, `build_test.go`, and the new `internal/helper/pagecut_test.go`.
- No CONTEXT.md or ADR change: the grammar, caps and terms are unchanged.

**Live risks.**
- The saving depends on timing. Typically the Page read (Tab lookup, `getFullAXTree`, pointer pass) ends after the walk has entered the document, so UIA still reads the document until the Page lands. If the Page read is slower than the whole UIA walk, there is no saving (and no loss).
- One chunk can take up to 2 s (the transaction timeout), and the answer is noticed only at the next chunk.
- A cut at entry cannot tell where the UIA focus was. If it was under the document and the Page reports no focused node and no focused root, the document line no longer carries the focus. Before this change it did.
- If the Window title and document name differ from the Tab title Chrome reports (localized or zero-width characters beyond `Collapse`), the document is not cut. That is today's cost, not a loss.
- Between `CaptureFrame` and the splice, the stored state is the cut frame. A request that dies there (a deadline) leaves the document line without its children until the next read.

**To time live** (Chrome with a Browser Connection; also Edge):
- `get-window-state` on a dense page (a long Wikipedia article, a GitHub diff, a big table): wall time before and after, and how much of it is UIA. Log or count the chunks read under the document, at entry versus late.
- The same Window with no connection: it should be unchanged.
- A page where the Page read is slow (a heavy SPA): check that the output is still the merged golden shape and the infobar is present.
- A Window with the side panel open: the panel is read whole.
- A small page: no regression.

### 2026-11-02: a connection per browser process

**The problem.** The Manager held one Browser Connection per browser name, and discovery picked one endpoint per name (extension, then launch and clone profiles, then the toggle, then 9222 to 9224). The user's Chrome (through the extension) and a winuse-launched Chrome could not both be served: a Window of the other one got "this Window belongs to a Chrome process that has no Browser Connection", in get-window-state and in every page, tab and navigate command. Live, launch only seemed to work because it pins and re-discovers; going back to the user's Window then failed or flipped, and a helper restart lost the mapping.

**What a Window uses now.** Every command that names a Window (get-window-state's Page read, page Actions, navigate and the other window-level Page commands, the tab commands, the Batch's tab scope probe) asks `Manager.WindowConnection(ctx, name, w.PID)`. Chromium's top-level windows belong to its browser process, which is also the DevTools port's owner and the native host's `browser_pid`, so the Window's own pid is the key. It returns the held connection of that process, or discovers and dials the endpoint that belongs to it. The pin plays no part. A pid of 0 (unknown) falls back to today's `Connection(name)`.

**What landed, per file.**
- `internal/browser/discover.go`:
  - `discoverFor(ctx, want, pid)`: the routes in today's order, each kept only when its endpoint belongs to pid. That is the extension bridge whose `browser_pid` is pid, then the launch, clone and toggle directories whose verified port owner is pid, then 9222 to 9224 owned by pid (only when no directory named one). Every candidate still passes `verify` (loopback ws URL on the probed port, port owner a known browser).
  - One port found through two directories counts once. Two different endpoints of the process are `SeveralEndpoints`, naming both. No endpoint is `NoWindowEndpoint`, with the pids of the other processes of that browser winuse can reach. A paused or unanswering extension of that very process is returned as itself (`ExtPaused`, `ExtUnreachable`) when nothing else serves it.
  - `Endpoint.key()` is now the connection key: `pid N`, or browser plus URL when the owner is unknown.
- `internal/browser/extension.go`: `extensionEndpointFor(ctx, want, pid)` (two bridges of one process are `ExtSeveralProfiles`, naming both instances), and `readyBridge`, the self-update, probe and paused check shared with `extensionEndpoint`.
- `internal/browser/manager.go`:
  - `conns` and `dials` are keyed by `Endpoint.key()`: one connection and at most one dial in flight per browser process, so the approval prompt rule holds per connection.
  - `WindowConnection`, and `open(want)`, which lists the open connections.
  - `live(want)` returns the held connection only when exactly one is open for that name. With several, discovery picks as before and `dial` reuses the held one it names.
  - A new dial forgets every ended connection, so a browser that restarted as a new process leaves nothing behind.
  - `drop(name, match)` drops a browser's matching connections; `dropEndpoint(ep)` drops one process's connection; `held(name)` takes the lowest pid.
- `internal/browser/commands.go`:
  - `Status` reports the connection it resolves as before. When the helper holds others, the first line names its process and one line per other connection follows: `ok chrome process 500 connected: your running browser through the winuse extension, 1 tab` then `also connected: chrome process 100, winuse's own profile`. Exit 0 still means connected. With one connection the output is unchanged.
  - `Stop` drops only the stopped process's connection (the user's Chrome stays connected), and its already-stopped branch drops only launch and clone connections.
- `internal/browser/clone.go`: `--close-source` drops every connection of that browser (the close took them all down).
- `internal/browser/problem.go`: two kinds, each with one sentence and one next command, plus `Problem.Reachable()`:
  - `NoWindowEndpoint`: "This Chrome window belongs to Chrome process 777, which has no debugging connection winuse can use (winuse can reach Chrome processes 4242 and 5151)." Next: "if it is the user's own Chrome, run winuse browser ext install chrome to reach it through the winuse extension; otherwise work in a window of Chrome processes 4242 and 5151". With nothing reachable, the second half is "otherwise run winuse browser launch chrome and work in its windows".
  - `SeveralEndpoints`: "Chrome process 100 answers on port 9301 (winuse's own profile) and port 9302 (winuse's copy of your profile, with your logins), so winuse will not guess which one to use." Next: restart it with one port, or `browser stop` then `browser launch` if winuse launched it.
- `internal/helper/pagestate.go`:
  - `pageBrowsers` is now `WindowConnection` plus `Identify`.
  - `livePages.connection(ctx, w, name)`. A connection to another pid, which only a fake hands out, becomes `NoWindowEndpoint`.
  - `windowConnProblem` and `windowConnError` word a failed lookup. The Window State note reads "the Page is read through UI Automation because this Chrome window belongs to Chrome process 777, which has no debugging connection winuse can use (...); <next>". A command fails with "window 501 belongs to Chrome process 777, which has no Browser Connection (winuse can reach ...)" and the Problem's next command as the hint. `SeveralEndpoints` and `ExtSeveralProfiles` are worded the same way. Any other failure keeps today's "no Browser Connection is open to Chrome" and "window N is a Chrome Window with no Browser Connection".
  - I did not touch `readWindowTree`.
- `internal/helper/pageactions.go` (`PageActiveTab`), `pagecommands.go` (`pageWindow`, `pagePrelude`'s download lookup) and `tabs.go` (`tabWindow`, `tabStepHeadless`) go through `livePages.connection`. The duplicated "another process" checks are gone. `browser.go`: the `BrowserService` comment.

**Decisions.**
- **The extension wins over a debugging port of the same process.** Both reach the same process and the same Tabs, so this is a choice of route, not a guess about which browser. It follows the user's decision to prefer the extension. Two endpoints of the same kind for one pid (two ports, or two extension profiles) are refused, naming both. Overrule this if extension plus toggle on one process should also be an error. It is one `case` at the top of `discoverFor`.
- **The key is the pid, not the route.** Every route to one process shares the key, so `browser connect` on a Chrome that the extension already serves replaces that connection (an explicit request), and two dials to one process never run at once (one approval prompt). An extension host that restarts under the same browser replaces its old connection by the same key.
- **Commands without a Window keep today's rules.** The pin, then the one debuggable browser. Only `status` changed, and only when the helper holds more than one connection.
- **Downloads** are unchanged: `Browser.setDownloadBehavior` is sent per connection, launched and cloned only. `pagePrelude` now finds the Window's own connection.

**Docs.** CONTEXT.md, Browser Connection: two browsers (the user's Chrome and a launched one) have two Browser Connections, and a browser Window is always reached through the one of the browser it belongs to. ADR 0012, decision 4: one connection per running browser process; a Window uses its own process's connection whatever the pin; commands without a Window still prefer the extension. The skill needed no change: the note it quotes is still produced as written.

**Flaky tests fixed on the way.**
- `TestManagerRedialsAfterTheBrowserRestarts`: `cdptest` wrote the 101 before adding the Conn to `conns`, so a `DropAll` right after the dial could miss it ("the dropped connection never ended"). Widening that window reproduced the failure every time. The Conn is now listed (under its write lock) before the 101 goes out. 300 runs in a row pass.
- `TestTabSwitchOnALaunchedBrowserIsHeaded` failed 2 in 40 runs without this change. `pagetest` reported a switched Tab visible only when `time.Now().After(shownAt)`, and on Windows the clock can read the same instant as the switch. The check is now `!Before`, and the tab switch tests pass 60 runs in a row.

**Tests.**
- browser (`windowconn_test.go`):
  - The user's Chrome (pid 500, extension) and a launched Chrome (pid 100) are both served, in either order. Both are reused (1 handshake, 1 WebSocket dial), and a new Manager maps both again.
  - The pin does not steal a Window. Pinned to Chrome, `Connection("")` is the extension while pid 100's Window gets the launch connection. Pinned to Edge, a Chrome Window is still Chrome's. A named lookup with two held connections reuses the extension's.
  - A Window of pid 777 is `NoWindowEndpoint` with the exact sentence and pids 100 and 500. With nothing reachable, the next command names ext install and launch. A paused extension of that process is `ExtPaused`.
  - Two ports owned by one pid are `SeveralEndpoints` with the exact sentence and nothing dialed. One port in two directories is one endpoint. Two extension profiles in one process are `ExtSeveralProfiles` naming both instances.
  - The launched Chrome restarts in place (a re-dial), then as pid 101 on a new port. The old connection is forgotten, pid 100 no longer resolves, and the user's connection is the same object, alive, and still lists its Tab.
  - Approval: three concurrent lookups of a toggle Chrome (pid 300) with its handshake held give `AwaitingApproval` with 1 handshake. Meanwhile the launched Chrome connects at once, and after Allow there is still 1 handshake.
  - Status prints both lines. `stop` closes only the launched Chrome, the extension connection stays alive, no `Browser.*` reaches the extension, and status is back to one line.
  - `TestProblemSentences` has 3 new cases.
- helper (`connperprocess_test.go`): one real `browser.Manager` over a faked `System` (pagetest launched Chrome pid 4242, bridgetest extension with `browser_pid` 5151, the pin set to chrome), run in both orders:
  - For each Window: `PageActiveTab`, `tab list` (exact output), `navigate` (exact output) and a Page read.
  - No cross-talk: `Page.navigate` and `Accessibility.getFullAXTree` reached each browser only on its own Tab's session. Nothing of the other Tab reached either browser, no `Browser.*` reached the extension, and no `Winuse.*` reached the launched Chrome.
  - Both connections are held, with 1 handshake. A second helper (a new Manager) lists both Windows' Tabs again.
  - Window 501 (pid 777): the exact tab-list failure and hint, navigate with "nothing was sent", and the exact Window State note reason. The other two Windows still work.
- Updated: `TestLivePagesReasons` and `TestTabCommandsWithoutABrowserConnection` assert the new "another process" wording exactly. The `pageConns` fake implements `WindowConnection`.
- Checks run: `go build ./...`, `go vet ./...`, `go vet -tags e2e ./test/e2e` and `go test ./...` are all green. The browser tests pass 5 times in a row (`-count=5`). go.mod is unchanged. All touched files have LF endings and no non-ASCII bytes.
- One mid-run red came from `internal/helper/pagecut_test.go` (`TestBrowserWalkLeavesTheCapsToThePage`), the other worker's in-progress file. It passed on the final full run.

**Shared or headless-parity-era files I edited:**
- `CONTEXT.md`: the Browser Connection entry.
- `internal/helper/pagestate.go`: `pageBrowsers`, `livePages.connection`, `windowConnProblem`, `windowConnError` and `ReadPage`. Each was re-read before editing; `readWindowTree` was left alone.
- `internal/cdp/cdptest/server.go`: the 101 ordering.
- `internal/page/pagetest/tabs.go`: the visibility comparison.
- Nothing in `internal/uia`.

**Live risks.**
- That a Chromium top-level Window's pid is the browser main process for every browser (true for Chrome; Edge, Brave, Vivaldi and Opera are assumed), and equals the DevTools listener's pid and the bridge's `browser_pid` (`ParentBrowser` past `cmd.exe`). A bridge file with `browser_pid` 0 can never serve a Window. The Window then says no endpoint belongs to its process; nothing is guessed.
- The first Window lookup with no held connection probes every profile directory's port and, when none matches, 9222 to 9224. On a toggle-mode Chrome that may count as the probe that raises the approval prompt (the open question from ticket 01).
- `browser status` with several connections re-runs discovery (and the extension's `Winuse.status` probe) on each call instead of returning the held connection outright.
- `browser connect` on a Chrome the extension already serves replaces that connection, which drops the Page sessions cached on it. The next read attaches again.
- The user's Chrome is still reached only through the extension or the toggle. A second user Chrome profile running in its own process (`--user-data-dir`) gets `NoWindowEndpoint` until the extension is loaded there.

### 2026-11-02: the document after a tab switch, and a remembered mapping

**Fix A: the document after a Headless tab switch.** Measured live on Chrome 155: after `tab switch` through the extension, UI Automation keeps the previous Tab's document (named, `RootWebArea`, its URL as Value, moved to the window's top) and shows the active Tab's place as an empty unnamed document (`view_1033`, no children). Today that gave the "several documents" note, or with the same title picked the stale document, whose rect is 143 px off.

- **The rule (`page.DocRule`).** A document whose Value is the URL of another of the browser's Tabs, and not the active Tab's (fragments ignored), is a stale Tab's document. It is never picked, and Merge leaves it out with its descendants. The pick: the first document, in index order, named after the Tab and not stale; else the only candidate, the empty placeholder included. A document under a stale one is no candidate.
- **Decision: "another page" means another Tab's URL, not "any URL other than the Tab's".** The narrower reading never calls a document stale on a hunch:
  - a URL `history.pushState` changed may lag in UI Automation;
  - iframe documents and side panels have other URLs, and must stay (a side panel is still read whole and shown);
  - the frame fixture's document already differs from its Page's URL (`/` against `/login`), so the wider reading would have broken every golden.

  The measured shape is covered: the previous Tab is still open in the background, so its URL is listed.
- **Decision: no tie-break by URL.** I tried "the only candidate with the Tab's URL" for several unnamed candidates and dropped it. When two Tabs share a URL, the stale document has the Tab's URL and the placeholder has none, so that tie-break picks the stale document and turns a safe fallback into a wrong pick. Several candidates none named after the Tab stay today's "several documents" note. The rect would settle it (the stale one overlaps the toolbar), but the walk does not read bounds, and adding them to the cache request is unmeasured.
- **Kept fallbacks.** With no other Tab's URL known (`Raw.Others` empty) nothing is stale and every row is today's, word for word. Only stale documents gives today's "no document" note.

**Per file (A).**
- `internal/page/merge.go`:
  - `DocRule`, `RuleFor`, `Stale`, `Named`, `pageURL` and `subtreeEnd`;
  - `findDocument` returns the stale ranges too;
  - `Merge` drops them (one under the picked document goes with the Page) and gives the Page their room under the element cap;
  - `Merged.From` is new: the frame index behind each Element, -1 for a Page Element;
  - `Merged.Doc` and `DocIndex` are the document's index in the merged tree; `End` stays a frame index;
  - a focus on a stale document counts as none in the frame;
  - `NamedAfter` stays as the name half.
- `internal/page/page.go`: `Raw.Others`, the other Tabs' URLs.
- `internal/page/reader.go`: `Read` fills `Others` from the Tabs `FindTab` listed (`windowTab`, which also returns them).
- `internal/uia/walk.go`: `DocumentCut` is now `func(name, value string)`; the walk passes each Document's Value (when it has the Value pattern). The cut still skips only the first document `Named` picks, so a stale document named like the Tab is read whole and the Tab's own is cut.
- `internal/uia/splice.go`: `SplicePage(ctx, hwnd, base, merged, from, origins)`. `from` replaces `doc, end`: the store keeps the live Element of each mapped frame index and releases every other one (the replaced descendants and the stale documents). Indexes out of order or out of range are refused, with nothing changed.
- `internal/helper/pagestate.go`:
  - `pageCut` stores a `page.DocRule` and decides with name and Value;
  - the defensive re-read compares `merged.From[merged.Doc]` with the cut index;
  - the `SplicePage` call;
  - the header paragraph.

**Fix B: remembered Window to Tab mapping.**
- `internal/page/remember.go` (new): `WindowMemory`, one per helper (`newLivePages`). It is keyed by the Window's HWND and browser pid, and holds the connection's `*cdp.Client` and the browser window id (the extension's windowId, or `Browser.getWindowForTarget`'s).
  - **Use.** `FindTab` remembers every unique match. On an ambiguous one it takes the one candidate in the remembered browser window. A candidate already matched the Window's title and bounds, so one candidate there means the browser window still exists and still lines up. Several there (a launched browser has no active flag) stay the error.
  - **Forgetting.** An entry goes when:
    - its connection ended (swept on every write) or differs (a re-dial);
    - the extension no longer lists its windowId;
    - another Window maps to the same browser window (a recycled handle);
    - an ambiguous lookup finds no candidate in the remembered window. Then it is gone or no longer this Window's, since the Window's own active Tab is always a candidate.
  - **Never a guess.** A Window never mapped uniquely, a zero HWND, or no memory (every existing test's `Geometry`) keeps today's error.
- `internal/page/match.go`: `Geometry.HWND`, `PID` and `Memory`.
- `internal/helper/pagestate.go`: `newLivePages` builds the memory and sets the three fields.
- `internal/helper/tabs.go`: `tabWindow` keeps its Geometry, and `tab new` records the Window's browser window after its read-back. Today this only confirms an entry: the Tab lookup `tab new` starts with has already recorded it. winuse creates no window whose HWND it knows, so there is nothing else to record.

**Tests.**
- page (`docrule_test.go`):
  - Both measured shapes: another title, and the same title on example.org, with the stale document before and after the placeholder. The placeholder is picked, the stale document and its children are gone, `From`, origins and `DocIndex` are right, and the Page's focus wins. One case checks the exact render.
  - No `Others`, and another Tab with the active Tab's own URL: today's "several documents" note, word for word.
  - Only stale documents: today's "no document" note.
  - A stale document named like the Tab before the Tab's own: the golden.
  - A stale iframe under the document: the golden.
  - Several unnamed candidates: still the note, and a side panel stays.
  - A focus on the stale document, and a focus in the address bar after it.
  - `Named` cases and the zero rule.
- page, reader: `Read` fills `Others` (the background Tab and chrome://newtab/, not the iframe). The 4 goldens are unchanged.
- page (`remember_test.go`):
  - The extension: two windows at the default bounds, each Window mapped once, then both "Example Domain".
    - Without the memory, the exact live error.
    - With it, both Windows resolve, and `ListWindowTabs` too.
    - A never-mapped Window and another pid stay errors.
    - Window 1 closes, and its entry goes.
    - A new window 3 opens at the same place.
    - A re-dial forgets.
  - A launched browser, through `getWindowForTarget`: the Window resolves. Two candidates in the remembered window stay an error and keep the entry. A gone window with no candidate forgets it.
  - The entries: one Window per browser window, the same id on another connection, ended connections, no HWND, a nil memory.
- uia:
  - the walk asks the cut with each Document's Value, and cuts the Tab's document after a stale one of the same name;
  - the splice leaves a stale document out, releasing its handles and keeping the rest at their new indexes;
  - a bad mapping is refused with nothing released;
  - the 3 existing splice tests use `from`.
- helper:
  - `staledoc_test.go` runs the real walk through get-window-state.
    - The same-title shape: one walk, no cut, Merge's render with no note, no stale Element stored, `DocIndex` 4, and `name:Cancel` resolving.
    - A stale document named like the Tab before the Tab's own: the cut is at the Tab's own, at entry and late, with no re-read. The stale one is read whole and left out.
  - `TestTabCommandsRememberTheWindow`: `tab new`, then a third window at window 2's place. `tab list 302` lists window 2, and the same lookup without the memory is the ambiguity error.
- **Checks run.** `go build ./...`, `go vet ./...`, `go vet -tags e2e ./test/e2e` and `go test ./...` are green. page, uia and helper pass with `-count=3`.
  - One full run failed `TestBridgeHostGoingAwayEndsTheClient` (its 3 s wait, under load). It passed 13 times alone and on the next full run. My only bridge change is the new `bridgetest.Set`, which that test does not call.
  - go.mod is unchanged, and the touched files have LF endings.
  - The non-ASCII bytes in `walk.go` and `pageactions_test.go` were already there, not in my lines.

**Shared or headless-parity files I edited.**
- headless-parity-modified: `internal/uia/walk.go` (the `DocumentCut` signature and `docMark.value`).
- shared:
  - `internal/helper/pagestate.go`: `PageSplicer`, `readWindowTree`'s two splice lines, `pageCut` and `newLivePages`;
  - `internal/helper/tabs.go`: `tabWindow.g`, and one line in `tabNew`;
  - the `SplicePage` fakes in `pagestate_test.go`, `pageactions_test.go`, `tabs_test.go` and `pagecut_test.go`: the signature only, plus `cut("", "")`;
  - `internal/bridge/bridgetest/extension.go` and `internal/page/pagetest/tabs.go`: a `Set` each.
- `.scratch/browser-merge/spec.md`: the ambiguous-match bullet names the remembered exception.
- No CONTEXT.md or ADR change: no new term, and the memory is easy to reverse.

**Live risks.**
- **Two Tabs with one URL.** A stale document there is not recognised. With the same title it is picked by name (today's wrong pick, rect 143 px off); with different titles the note stays. Only the rect could tell. Measure whether reading the document's bounds is cheap enough.
- **A side panel.** Whether the placeholder stays when the side panel is open. If a side panel document sits next to it, the result is the "several documents" note.
- **How long the stale document stays.** Whether it ever goes away by itself or survives until the next focused switch, and what Chrome shows after a `tab switch` back (the e2e test does that).
- **A false stale.** A side panel or another non-Tab document whose URL equals another Tab's URL is left out as stale.
- **A helper restart.** The memory lives as long as the helper. After a restart, the two same-site windows are ambiguous again until each is seen once with a unique match.
- **No first unique match.** The first match of a Window must happen while it is unique. Windows that were never told apart (both opened on the same site) stay errors. The agent can make one unique by navigating it, or with a tab new plus a switch.
