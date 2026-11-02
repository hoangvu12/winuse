# 06: Page-only Actions

**What to build:**

- `navigate`, `back`, `forward` and `reload`
- hover, drag, select and check on page Elements
- `upload` through file chooser interception
- `dialog accept|dismiss`, with dialog-blocked fast failures
- downloads on launched and cloned browsers
- `wait` for a selector, text, URL or load state
- `eval`

All are Headless.

See spec: "Routing and page Actions".

**Blocked by:** 05

**Status:** done

- [x] Offline tests per command against a fake CDP connection
- [x] A parity test: every documented command has a handler

## Comments

### 2026-11-02: probe decisions (accepted by the user)

From `../probe-results.md`:

- **`navigate` is the only Headless way to open a URL.** Posted keys into Chrome's address bar do nothing, with or without set-value first. The skill (ticket 08) should say: with a Browser Connection, use `navigate`, never type into the omnibox.
- **Background windows.** `Target.createTarget {newWindow:true, background:true}` opened a window below the user's foreground Window without changing the foreground, so a download or popup flow that needs a new window can stay Headless.

### 2026-11-02: landed

**Command surface.** All are Headless and have no Headed form (no `--headed`). Each takes a `<window>` that is a browser Window with a Browser Connection; the Tab is the one the Window shows now (`page.FindTab`), so no Window State is needed first. Budgets: `protocol.BrowserTimeout` (30 s), because the first command may dial. All are Batch steps, classified Headless.

| Command | Wire name, args | Output |
|---|---|---|
| `winuse navigate <window> <url> [--wait load\|domcontentloaded\|none]` | `navigate`, `protocol.NavigateArgs{Window, URL, Wait}` | `ok <final URL>` |
| `winuse back\|forward\|reload <window> [--wait ...]` | `back`, `forward`, `reload`, `NavigateArgs` | `ok <final URL>` |
| `winuse upload <window> [<element>] <file>...` | `upload`, `protocol.UploadArgs{Window, ElementIndex, Ref, Files}` (absolute paths, made so by the CLI) | `ok` |
| `winuse dialog <window> accept\|dismiss [text]` | `dialog`, `protocol.DialogArgs{Window, Action, Text *string}` | `ok` |
| `winuse wait <window> selector <css>\|text <text>\|url <url or *pattern*>\|load [domcontentloaded]\|download [--timeout <s>]` | `wait`, `protocol.WaitArgs{Window, For, Value, TimeoutMS}` | `ok`, or `ok <path>` for download |
| `winuse eval <window> <expression>` | `eval`, `protocol.EvalArgs{Window, Expression}` | the value, plus a `note:` line when cut |
| `winuse hover <window> <element>` | `hover`, `HoverArgs` + `ElementIndex`, `Ref` | `ok` |
| `winuse drag <window> <element> <element \| @x,y>` | `drag`, `DragArgs` + `FromIndex`, `FromRef`, `ToIndex`, `ToRef` (the point stays `ToX`, `ToY`) | `ok` |
| `winuse set-value <window> <select> <label or value>`, `set-value <window> <check box> on\|off` | unchanged | `ok` |

Args types are in the new `internal/protocol/page.go`. Usage errors exit 2 locally. `wait --timeout` takes seconds, above 0 and at most 25. The default wait is 10 s, and every wait is capped at the request deadline minus 1 s. In a Batch, what navigate, wait and eval print is dropped (a Batch prints only ok); an eval that throws stops the Batch.

**Per file.**
- `internal/page/watch.go` (new): `TabWatch`, one per (connection, session), in a package registry.
  - `Watch(ctx, c, session)` subscribes to every event of the session (one ordered subscription), then sends `Page.enable` and `Page.setInterceptFileChooserDialog {enabled:true}`.
  - It keeps the open `Dialog{Type, Message, DefaultPrompt, URL}` from `javascriptDialogOpening` and `Closed`, and the waiting `Chooser{BackendNodeID, FrameID, Multiple}` from `fileChooserOpened` (cleared by a main-frame `frameNavigated`), plus a count of choosers and a `Changed()` channel.
  - `WatchOf`, `Unwatch`, `OpenDialog(c, session)`, `DialogOpenError`, `Dialog.Describe()`.
  - `(*Reader).TabSession(ctx, conn, targetID)`: the reader's cached Tab session for window-level Actions, probed with `Page.getFrameTree` (no script) and attached again once if detached.
- `internal/page/pointer.go` (new): `HoverEvents`, `Input.Hover`, `DragEvents(from, to, steps)`, `Input.Drag`. Drag turns on `Input.setInterceptDrags`; when the browser answers `Input.dragIntercepted`, the drop finishes with `Input.dispatchDragEvent` dragEnter, dragOver and drop at the target, then the release. Otherwise it is plain mouse events: a move, the press, 10 moves 15 ms apart with the button held, the release.
- `internal/page/choice.go` (new): `ReadChoice` (select, check, radio, ARIA checkbox, switch and radio roles), `PickOption`, `SelectOption`, `ParseOnOff`, `ReadFileInput`, `SetFiles` (`DOM.setFileInputFiles`), and the context menu guard (`GuardContextMenu`, `ContextMenuResult`).
- `internal/page/nav.go` (new): `Navigate` (errorText becomes `*NavigateError`, isDownload becomes `ErrDownloadStarted`), `History`, `GoToEntry`, `Reload`, `MainFrame`, `WaitLoad`, `WaitExpr`, `Poll`, `Eval`, `FormatResult`, `CurrentURL`. `raceDialog` races a command against the watch, so a beforeunload on navigate or an alert in eval is reported rather than waited for.
- `internal/page/downloads.go` (new): `EnableDownloads(ctx, c, dir)` (once per connection), `DownloadsOf`, `Downloads.Wait`, `Seq`, `Since`, `Changed`, `SafeFileName`.
- `internal/helper/pagecommands.go` (new):
  - `pageCommands`, `pageOnlyStep`, `pageWindow`, and `runPageWindow` (locked desktop, Tab, dialog check, passive Esc scope, cursor, act, ripple);
  - `pagePrelude` (the dialog fast failure and download tracking, run before every Page Action);
  - `pageDownloadDir` and `pageDownloads`;
  - the handlers for navigate, back, forward and reload (`navigate`, `navFailure`), dialog, wait (`waitDownload`) and eval;
  - the cursor targets `addressBarRect` and `pageDocRect`.
- `internal/helper/pageelements.go` (new): `pageHover`, `pageDrag` and `dragEnd`, `pageUpload` and `uploadToChooser`, `pageSetChoice`, `pageClickRun` (the click's notes and the right click guard), and `actRefusal`.
- `internal/cli/page.go` (new): parsing for the eight commands, `pageCmd`, `parseBatchPageStep`, `parseHoverTokens` and `parseDragTokens`.

**Decisions, with reasons.**
- **select and check are forms of set-value, not new commands.** set-value already means "make the Element hold this value", and on UIA it already takes on|off for a check box. So `set-value W N <label or value>` on a `<select>` and `set-value W N on|off` on a check box, switch or radio is the smallest surface and the same words. 05's Secondary Action refusal now names these forms. `perform-secondary-action Toggle` stays refused on Page Elements: Toggle has no target state, and set-value does.
- **select uses the page's JS** (it sets `selected`, then fires bubbling input and change events), as agent-browser does. A trusted click opens Chrome's popup list, a window of its own on the user's screen. Keys need the focus and a typed prefix that may match another option first. An option matches by its exact label, then its exact value, then its label ignoring case and spaces. Several matches, or none, is a refusal that lists the options. The choice is read back.
- **check is a trusted click only when the state differs**, then the state is read back. An unchanged state is a VerifyError ("delivered, do not repeat"). There is no JS `.click()` fallback, because a page that ignores the trusted click would be toggled behind its back. A radio cannot be set off (refused: set another one on). A hidden styled check box is refused as hidden: click its label.
- **Right click stays Headless, and Chrome's own context menu is kept from opening.** Before the click, winuse adds one bubbling `contextmenu` listener on the node's window (the last to run). If the page did not cancel the event, the listener cancels it, so Chrome's menu, a window of its own on the user's screen, never opens. A page with a menu of its own (it cancels the event itself) shows its menu as usual. The listener is removed right after the click. The output says when the browser's menu was held back. This keeps the user's screen untouched without refusing a right click, which web apps with custom menus need.
- **Dialogs.** The watch starts when the reader attaches a Tab (`Reader.session`), so it covers 05's Actions too.
  - `NewInput` fails at once with `DialogOpenError` (nothing sent), which replaces 05's 5 s wait.
  - `pageFresh` and `runPageInput` call `pagePrelude` first. Without that, the staleness check's `Runtime.callFunctionOn` would hang behind the dialog.
  - The keyboard's focus probe routes to the Page while a dialog is open, so typing fails fast instead of falling back to Headed keys.
  - Message: `window W shows a confirm dialog: "Leave this page?", and its Page takes no input until it is answered; nothing was sent`. Hint: `run winuse dialog W accept or winuse dialog W dismiss (a prompt takes its text: ...)`.
  - Alerts are not auto-accepted.
  - The Window State shows it in place of the Page read: `Note: the Page is read through UI Automation because it shows a confirm dialog: "..."; answer it with winuse dialog W accept or winuse dialog W dismiss.` The cursor-pointer pass would hang otherwise, and UI Automation still shows Chrome's dialog in the frame.
  - `dialog` with no dialog open fails with "shows no JavaScript dialog; nothing was sent" (from the browser's own answer, so it also covers a dialog opened before the watch existed). Text with a non-prompt dialog is refused.
  - After answering, `dialog` waits up to 500 ms for the closed event, so the next Action does not still see the dialog.
- **File chooser interception is on for every watched Tab.** A click that opens a chooser prints `ok (the click opened a file chooser, which the browser held back so nothing opened on the screen: answer it with winuse upload W <file>...)`. `upload` without an element answers the waiting chooser. With an element, it uses `DOM.setFileInputFiles` on a file input, or answers the chooser that element's click opened. Paths are checked (absolute, present, not a folder) before anything is sent. A single file input or chooser refuses several files.
- **Downloads go to `%USERPROFILE%\Downloads\winuse`** (the known Downloads folder through `FOLDERID_Downloads`, so a redirected folder is respected).
  - Why there: the user finds what the agent downloaded where they look for downloads, the agent's files stay apart from their own, and the renames never touch the user's files. `%LOCALAPPDATA%` is hidden.
  - Behaviour is `allowAndName`: files land under their GUID, and winuse renames each one to its suggested name on completion (` (2)` when taken), so the reported path is exact. With `allow`, the events carry no final path.
  - It is on for launch and clone connections only. It turns on at the first Page Action on a connection (`pagePrelude`, `pageWindow`), so it is on before the click that downloads.
  - A click that starts a download says `the click started a download of "r.pdf": winuse wait W download prints its path when it is done`. `wait W download` follows the oldest download not yet reported.
  - On the extension it is refused with 02's ExtCannot sentence, prefixed with "downloads through the winuse extension go to the browser's own download folder, and winuse cannot track them". A toggle or port browser is the user's own: winuse says it does not track it there and names launch or clone.
  - The folder function refuses inside a test binary (`LiveDesktopGuard`), and its log line is written once.
- **Navigation.** `Page.navigate` with `transitionType: typed`; a URL without a scheme gets `https://` (tab new's rule).
  - Commit: the new loaderId (navigate), the history index (back and forward) or a changed loaderId (reload). Then `document.readyState` is polled every 100 ms.
  - A `#fragment` navigation does not wait.
  - A load still running at the deadline is `ok <url> (the page was still loading after Ns; winuse wait W load waits longer)` (exit 0): the navigation was delivered.
  - errorText is a failure without "nothing was sent" (the Tab shows Chrome's error page).
  - A dialog during the wait or the navigate call (beforeunload) is a failure naming the dialog command.
  - The ends of the history are refused ("has no earlier page in its history; nothing was sent").
  - The cursor goes to the address bar (a frame `edit` named like "Address"), else the document.
- **wait** polls one JS predicate through `Runtime.evaluate`, with each poll bounded to 2 s, and checks Esc and the dialog watch between polls. The agent's text is always a JSON string literal.
  - selector means it exists and is visible (has a size, is not hidden).
  - text is `body.innerText.includes`.
  - url matches when it contains the value; with `*`, the value is a glob over the whole URL.
  - load is readyState.
  - An invalid selector fails on the first poll with the browser's message.
  - A poll that meets a navigation counts as false.
  - No cursor (no target). An Esc stop says nothing was sent.
- **eval** runs `Runtime.evaluate` with `{returnByValue, awaitPromise, replMode}`.
  - A string prints as is. undefined, NaN, Infinity and bigints print in their JS form. Anything else prints as compact JSON.
  - The output is cut at 4000 characters, with a `note:` line.
  - A thrown exception is a failure, `the expression threw: TypeError: ...` (first line only).
  - DOM nodes, windows and functions are refused with "return a plain value".
  - An alert raised by the expression is reported as a dialog.
  - Esc stops it with `Runtime.terminateExecution`.
  - No cursor. winuse keeps no Window State history, so eval is not listed anywhere.
- **hover and drag** take Page Elements only in their Element forms. A UIA Element is refused with the @x,y form named (UIA has no hover or drag pattern, and adding a Headed element-point form was not asked for). A drag ends at another Element of the same Page (checked, not scrolled, because scrolling would move the start) or at @x,y inside that Page (05's inverse transform).
- **Click settle.** A Headless Page click waits up to 25 ms after its last event for a chooser or download event, so its note is not lost to event timing.
- **Parity test** (`internal/helper/parity_test.go`): every command in `cli.Usage`'s command list has a handler, every handler is documented there, and every `winuse <command>` line in SKILL.md's code blocks has a handler. The other SKILL.md direction is `internal/skill`'s existing TestSkillCoversFullCommandSurface.
  - **Note for 08:** SKILL.md got a short "Browser pages" stub listing the eight commands, because that existing test failed without them. Ticket 08 should rewrite it into the real guidance: never type into the omnibox; dialogs; `wait download`; set-value for lists and check boxes.
- No `third_party/agent-browser`: nothing was copied. The ideas (the move before the press, interpolated moves, JS select with events, re-reading a check, waits) were re-implemented.

**Tests (offline).**
- `internal/page/pageonly_test.go`:
  - the watch (dialog open and close, another session unaffected, chooser, navigation clears it, interception sent);
  - NewInput failing at once while a dialog shows, with nothing sent;
  - `DragEvents`;
  - plain and HTML5 drags (dragIntercepted after the third move gives dragEnter, dragOver, drop and the release);
  - `PickOption` (label, value, case, ambiguous, none) and `ParseOnOff`;
  - Navigate (success, errorText, isDownload) and the beforeunload race;
  - WaitLoad (commit then complete, timeout with the last state, domcontentloaded, Esc);
  - WaitExpr quoting and refusals;
  - Poll (success, invalid selector, timeout, Esc);
  - FormatResult (string, JSON, undefined, NaN, number, null, the cut);
  - Eval (value, params, exception, node refused, alert raced);
  - Esc terminating the script;
  - downloads (the behavior call, enabled once, the rename with ` (2)`, canceled, nothing begun) and `SafeFileName`.
- `internal/helper/pageonly_test.go`, over pagetest, the fake extension and 05's pageRig:
  - navigate (the output, the address bar cursor and label, errorText, a slow load as ok with a note, `--wait none`, a bad wait);
  - history ends, back, forward and reload;
  - no connection and Notepad;
  - the dialog watch: navigate, reload, wait and eval failing at once with nothing sent; the Window State note; text refused on a confirm; dismiss; the action working again afterwards; "no dialog"; a prompt's text;
  - wait (success after 3 polls, no cursor, timeout, Esc, bad kind);
  - eval (JSON, exception, the cut note);
  - upload (missing file, no chooser, two files on a single chooser, the chooser answered on the Tab session, answered only once);
  - wait download (the path) and its refusal on the extension (no `Browser.*` reached it);
  - Element Actions failing at once on a dialog (click, set-value, hover, type-text);
  - hover (the event and label), a UIA hover refused;
  - drag Element to Element (the sequence, interception), a drag to a UIA Element refused;
  - upload to a file input (one file only, the backendNodeId, the label), and an Element that is not a file input;
  - set-value on a select (by value, unknown option), on a check box (clicked and read back, unchanged read-back, already off sends nothing, radio off refused);
  - the right click guard (the order guard, press, release, result; the note only when the page has no menu of its own);
  - a click that opens a chooser;
  - the Batch's scope for all eight kinds plus the hover and drag forms, and a navigate, wait and eval Batch in one passive scope.
  - Every runner checks for passive scopes only and that nothing sent `Target.activateTarget` or `Page.bringToFront`.
- `internal/helper/parity_test.go`, `registry_test.go` (8 names).
- `internal/cli/page_test.go`: parsing (18 cases), refusals (19), the hover and drag forms, Batch steps, usage exit 2 without dialing, the budgets and the usage lines.
- `internal/protocol/protocol_test.go`: the 8 new Batch kinds.
- Checks run: `go build ./...`, `go vet ./...`, `go vet -tags e2e ./test/e2e` and `go test ./...` are all green. The new page and helper tests pass 5 times in a row. go.mod is unchanged. Every touched file has LF endings, and no added line has an em or en dash.

**Shared and headless-parity-modified files I edited (minimal, each re-read first):**
- `internal/page/reader.go`: `Watch` on attach (1 line), `Unwatch` in `drop` (1 line), and the dialog check at the top of `readTab` (5 lines).
- `internal/page/input.go`: 3 lines in `NewInput`.
- `internal/protocol/protocol.go`: 8 budget entries, 8 `BatchStepKinds`, and the Page Element fields on `DragArgs` and `HoverArgs`.
- `internal/protocol/protocol_test.go`: the expected kinds.
- `internal/helper/state.go`: the `pageCommands` loop (4 lines).
- `internal/helper/pageactions.go`:
  - `pagePrelude` in `pageFresh` and `runPageInput` (3 lines each);
  - two `pageFailure` cases;
  - `pageClick` now returns through `pageClickRun` (1 line);
  - the choice route in `pageSetValue` (3 lines);
  - the dialog route in `pageKeyTarget` (3 lines);
  - `pageOnlyStep` in `pageStep` (3 lines);
  - the Secondary Action refusal's hint.
- `internal/helper/pointer.go`: 3 lines each in drag and hover, and their usage hints.
- `internal/helper/pagestate.go`: `livePages.ReadPage` maps a `DialogOpenError` to the note (5 lines).
- `internal/helper/registry_test.go`: 8 names.
- `internal/cli/cli.go`: the headless paragraph (3 lines), the hover and drag lines, the 8 usage entries, and one `case`.
- `internal/cli/pointer.go`: hoverCmd and dragCmd go through the new parsers, plus the usage lines.
- `internal/cli/batch.go`: the Element branches of the drag and hover steps (the point forms keep their messages), and one `case`.
- `CONTEXT.md`: one sentence in Headless Action.
- `skills/winuse/SKILL.md`: the "Browser pages" stub (for ticket 08 to rewrite).

**Live risks for ticket 09's e2e.**
- **Interception affects the user.** While winuse holds a Tab's session, a file input the user clicks in that Tab opens nothing. Through the extension that is the user's real browser, until the extension's 5 minute idle detach or the helper's exit. Consider turning interception on only around winuse's own Actions if this bites.
- **Interception through the extension.** Whether `chrome.debugger` accepts `Page.setInterceptFileChooserDialog` and `Input.setInterceptDrags` (failures are ignored, and `Intercepting()` records it).
- **Dialogs opened before the watch existed.** Whether `Page.enable` replays `javascriptDialogOpening` for a dialog already open (assumed not). Until it is answered through `dialog`, such a dialog makes the Window State read time out (the 4 s note) and Actions wait on the staleness check.
- **Dialogs from cross-origin iframes.** On which session they arrive. Only the Tab session is watched; Input still races its child sessions.
- **The right click.** Whether a CDP right click opens Chrome's native context menu at all, whether cancelling the event in a last bubbling listener keeps it closed, and pages that stop the event in the capture phase (the menu would then open).
- **HTML5 drag-and-drop** through `setInterceptDrags` and `dispatchDragEvent` in headful Chrome on Windows. Plain mouse drags (sliders, sortable lists) on covered or minimized windows. Pointer capture.
- **Navigation.** When `Page.navigate` answers (start or commit). bfcache back and forward and same-document history entries, where the loaderId and readyState signals may differ. A download URL reported as `isDownload`.
- **Downloads.**
  - `allowAndName` file naming, and the rename right after `completed` (the file must be closed).
  - Whether the behaviour survives a reconnect (it is set again per connection).
  - A download that began before tracking turned on is never seen.
  - Edge's and Brave's handling of `Browser.setDownloadBehavior`.
- **select's synthetic events.** Frameworks that check `isTrusted` on change.
- **Check boxes styled with a hidden input** are refused as hidden: click the label instead.
- **eval's `replMode` and `terminateExecution`** through the extension.
- **wait reads only the main document**, not iframes.
- **The 25 ms click settle** adds latency to every Headless Page click.

### 2026-11-02: chooser interception scoped to Actions

Approved by the user: through the extension the Tab is the user's real browser, and interception left on meant a file input the user clicked there opened nothing while winuse held the Tab (up to 5 minutes). Interception is now on only while winuse acts.

**What changed.**
- `page.Watch` no longer sends `Page.setInterceptFileChooserDialog`; it sends `Page.enable` only.
- `(*TabWatch).Intercept(ctx) (release func() error)` (`internal/page/watch.go`): a hold count under its own mutex. The first hold sends `{enabled:true}`, the last release sends `{enabled:false}` (with `context.WithoutCancel` and a 2 s bound, so Esc and the deadline still turn it off), nested holds send nothing, and a release called twice is a no-op. A browser that answers the enable with a protocol error is marked unsupported and not asked again (the Action runs without it, as before). A failed disable keeps the state on, returns the error, and the next release tries again. A nil watch holds nothing. `InterceptingNow()` reports the live state; `Intercepting()` now means "the last enable was accepted".
- `holdChoosers(ctx, o)` (`internal/helper/pageelements.go`) takes the hold on the Tab session's watch and logs a failed release (`log.Printf`), never failing the Action.
- `runPage` and `runPageInput` (`pageactions.go`, one line each) hold it from right after `NewInput` (so after the dialog check: the enable never queues behind a dialog) until the Action returns, by `defer`, on success, failure, Esc and timeout. That covers every Headless Page input: click in all its forms, set-value's check box click, hover, drag, element upload, scroll by wheel, and type-text and press-key routed to the Page (Enter or Space on a file input opens a chooser too).
- `pageClickRun`: the 25 ms settle moved into the wrapped act, so it runs inside the hold, and a chooser the click opens a moment late is still caught. Notes are read after, as before.
- **upload after a separate click.** Per the CDP docs `Page.fileChooserOpened` is emitted only while interception is on, and an intercepted chooser leaves nothing pending in the browser (Chromium returns before opening it); `DOM.setFileInputFiles` on the input's `backendNodeId` sets the files and fires input and change with or without interception (this is how Puppeteer's `FileChooser.accept` works since `Page.handleFileChooser` was removed). So the watch keeps the caught chooser after the release, and `upload` (with or without an element) answers it later with interception off. `uploadToChooser` takes no hold. A chooser the user opens outside winuse's Actions sends no event, so it is never mistaken for winuse's.
- **The context menu guard** (`internal/page/choice.go`): it was already removed after every guarded click (`ContextMenuResult` runs even when the click fails). It could stay if that call failed, for example when the click removed the node from the document. Now the listener also removes itself after the first `contextmenu` event it sees, and after 10 s at the latest, so a user's own right click is never held back.

**Checked, no change needed.**
- The dialog watch only listens (`Page.enable`): a dialog still shows in the user's browser and the user can answer it. Winuse's Actions fail fast while it shows, which is about winuse, not the user.
- `Browser.setDownloadBehavior` runs only on launched and cloned connections (`pageDownloads` checks the mode, and the extension refuses `Browser.*`), which are winuse's own browsers. A toggle or port browser is never changed.
- `Input.setInterceptDrags` was already turned off by `defer` at the end of each drag.

**Residual risk for 09.** A page that opens its chooser later than the click's 25 ms settle (after a fetch, for one) now opens the real Windows file dialog, since interception is off by then; before, it was held back. The click note does not mention it.

**Tests (offline).**
- `internal/page/pageonly_test.go`: the watch sends only `Page.enable`; holds nest (on, off once), a second release is a no-op, a chooser caught while on stays after; a refused enable is not retried; a failed disable is returned and retried by the next release; a cancelled context still turns it off.
- `internal/helper/chooserscope_test.go` (new): off after the Tab is watched; on for every click event and off after (`intercept true`, events all seeing it on, `intercept false`); the caught chooser reported, then answered by a later `upload` on backendNodeId 70 in the Tab's session with interception off after; off after a failed click and after an Esc stop; a refusal before input never turns it on; type-text and press-key routed to the Page hold it; through the fake extension the watch sends no interception and a click holds it for its events only.
- `internal/helper/pageonly_test.go`: `TestUploadToAnInterceptedChooser` now checks that attaching sends no interception and that answering a chooser turns none on.
- Checks run: `go build ./...`, `go vet ./...`, `go vet -tags e2e ./test/e2e`, `go test ./...` all green. Every touched file has LF endings and no em or en dash.

### 2026-11-02: dialog hang fixed

**Symptom (e2e, Chrome 155, launched).** After a click opened an `alert`, `eval`, `navigate` and `dialog accept` each hung for the full 30 s request budget ("winuse request timed out"). `click`, `set-value` and `get-window-state` were fine: they read the watch first, and the watch had seen the dialog.

**Root cause (confirmed offline).** The three window-level commands go through `pageWindow`, which calls `Reader.TabSession`. Before returning, that sent `Page.getFrameTree` to the Tab session to check that the session was still attached, using the request context. The comment there said it "runs no script, so it answers even while a dialog is open". That is wrong: Chromium's renderer handles `Page.getFrameTree`, and the renderer's main thread is blocked while a JavaScript dialog shows. So the call waited until the request deadline, before `runPageWindow` ever read the watch. `dialog accept` then sent `Page.handleJavaScriptDialog` on a context that had already expired. A new fake reproduced it exactly (10 s per command against a 10 s deadline, with the dialog message only after the wait): a cdptest page session that stops answering page-thread calls while a dialog is open.

**Fix.**
- `Reader.TabSession` checks the session with `Target.getTargetInfo` instead. The browser answers that itself for a session, so a dialog does not block it, and it fails for a detached session. The call is bounded at 1 s. A refusal or no answer counts as alive, and only an `isGone` error triggers a re-attach.
- `page.Ready(ctx, c, session)` (`internal/page/watch.go`) is the first check every page Action makes. A dialog the watch saw returns `*DialogOpenError` with no call at all. Otherwise it sends a harmless page-thread probe (`Runtime.evaluate` of `/* winuse:ready */ 0`) bounded by `AnswerTimeout` (3 s, a var so tests can shrink it). No answer in time returns `*DialogOpenError` if the watch learned of the dialog meanwhile, else `*NotAnsweringError`, which says the Page most likely shows a JavaScript dialog winuse did not see open (it opened before winuse attached) or a script keeps it busy. Other probe failures pass, and the Action's own calls report them. The probe is needed because `Page.enable` is not known to replay `Page.javascriptDialogOpening` for a dialog that is already open.
- Callers: `runPageWindow` (every window-level command but `dialog` and `wait download`), `pagePrelude` (every Page Element Action and Page input), and `readTab` (the Window State). `pageNotReady` maps both errors to the Action's failure, with "nothing was sent" and the `winuse dialog <w> accept|dismiss` hint. `livePages.ReadPage` maps `NotAnsweringError` to the Window State's note as well.
- Setup calls no longer wait for a blocked Page. `Watch`'s `Page.enable` is bounded at 1 s, and `AttachPage`'s `Emulation.setFocusEmulationEnabled` and `Runtime.runIfWaitingForDebugger` share a 1 s bound. The browser keeps them queued in order on the session, so they still apply before any later input.
- `dialog accept|dismiss` does not need the watch. It sends `Page.handleJavaScriptDialog` on the mapped session, and the browser answers that itself. "No dialog is showing" maps to "shows no JavaScript dialog", as before.

**Tests (offline).**
- `cdptest.Server.Stall(f)` is new: a request `f` returns true for is recorded and never answered.
- `TestDialogBlockedPageFailsFast` (`internal/helper/pageonly_test.go`) has three variants: the watch saw the dialog, the watch missed it, and the dialog opened before winuse attached. The fake answers only `Target.*` and `Page.handleJavaScriptDialog` on the session while the dialog is open. In each variant, eval, navigate, reload and wait fail under 5 s with the dialog message, "nothing was sent" and the dialog hint, and nothing reaches the Page. The Window State's reason names the dialog. `dialog accept` answers it with `{"accept":true}`, navigate works again, and a second `dialog` reports no dialog.
- `TestPageElementActionsOnABlockedPageFailFast` covers click and set-value, seen and missed, using the real 3 s bound.
- Before the fix both tests went red: every command took the full 10 s deadline, and `dialog accept` failed with "context deadline exceeded".
- Checks: `go build ./...`, `go vet ./...`, `go vet -tags e2e ./test/e2e` and `go test ./...` are green. `TestManagerRedialsAfterTheBrowserRestarts` failed once and then passed 5 of 5 times. It looks like an existing race in cdptest (`DropAll` can run before the new connection is in `conns`), not related to this change.

**For the e2e rerun.** The dialogs subtest should now pass its timing checks. A first Action on a Page whose dialog opened before winuse attached takes up to about 3 s (the probe) plus 2 s (bounded setup) before it fails.

### 2026-11-02: internal pages are Tabs

**Problem.** `browser.IsInternal` dropped `chrome://` and the other browser pages from FindTab and the tab listing, so a Window showing the New Tab page mapped to no Tab. `navigate` could not leave a fresh window's New Tab page, `tab list` failed in the user's Chrome when only a New Tab page was open, and the Window State had the vague "no Tab ... matches" note.

**What changed, per file.**
- `internal/page/tabs.go`:
  - `IsTabTarget(type, url, hasTabID) (tab, internal bool)` is the one rule. A page target that is no browser page is a Tab. Through the extension, any target with a `tabId` is a Tab (background.js lists only chrome.tabs' tabs). Otherwise a browser page is a Tab only under the browser's own scheme (`chrome://`, `edge://`, `brave://`, `vivaldi://`, `opera://`) and not on a `*.top-chrome` host. `devtools://`, `chrome-extension://` and `chrome-untrusted://` stay out on CDP.
  - `pageTabs` decodes `tabId`, applies the rule and sets `Tab.Internal`. FindTab, ListWindowTabs, TabOpen, TabWindow and ExtensionTabActive all go through it.
  - New: `NavigateTabThroughExtension` (sends `Winuse.navigateTab`) and `TabNow` (a Tab as listed now, or `ErrTabGone`).
- `internal/page/match.go`: `Tab.Internal`.
- `internal/page/reader.go`: FindTab now calls `pageTabs` (its own copy of the decoding is gone). `Read` returns `*InternalPageError` for an internal Tab before any attach. `Reason` knows that error.
- `internal/page/page.go`: `InternalPageError{URL}`.
- `internal/page/pagetest/tabs.go`: `Others []Other`, page targets that are no Tabs (DevTools, tab search, extension popups). They are listed by getTargets and mapped to a window by getWindowForTarget.
- `internal/bridge/bridge.go`: `MethodNavigateTab = "Winuse.navigateTab"` `{targetId, url} -> {}`, documented in the method comment.
- `internal/bridge/extension/background.js`: `navigateTab(p)` does `chrome.tabs.update(tab.id, { url: p.url })` and nothing else (no `active`, no `chrome.windows`). The header comment names it. `node --check` passes. The manifest version is not bumped; the build hash changes, so ext status self-updates.
- `internal/bridge/bridgetest/extension.go`:
  - `navigateTab` mirrors background.js: the tab's URL and title become the URL, and `Navigated()` lists the calls.
  - `refuseBrowserPage` is chrome.debugger's refusal ("Cannot access a chrome:// URL") on attach and on session calls to a tab that shows a browser page or another extension's page.
- `internal/helper/pagecommands.go`:
  - `pageWindow` does not attach an internal Tab. `pageWin.attach` and `pageWin.reader` are new.
  - `pageWindowRun.browserPage` decides about an internal Tab before the cursor moves. Unset, the command is refused with `browserPageError`: "the active Tab of window W shows a browser page (URL), which winuse does not read or act in; nothing was sent", hint "run winuse navigate W <url> to open a site". The Ready probe is skipped for an internal Tab.
  - `navigate` attaches an internal Tab on CDP and runs as before. Through the extension it calls `navigateFromBrowserPage`. back, forward and reload through the extension are refused before the cursor moves ("... and through the winuse extension only navigate can leave it").
  - `navWait` is split out of `navigate`.
  - `navigateFromBrowserPage` steps:
    1. Send `Winuse.navigateTab`.
    2. Poll the target list every 100 ms until the Tab's URL changes.
    3. If the new URL is a browser page, or with `--wait none`, print `ok <url>` without attaching.
    4. Otherwise attach, retrying every 100 ms while the browser refuses, then `navWait`.
  - `wait download` is allowed on an internal Tab (`waitBrowserPage`), since a navigate to a file leaves the Tab there.
- `internal/helper/pagestate.go`: `livePages.ReadPage` maps `InternalPageError` to the note `the Page is read through UI Automation because the active Tab shows a browser page (chrome://newtab/); run winuse navigate W <url> to open a site.`
- `internal/helper/pageactions.go`: `PageActiveTab` returns `InternalPageError` for an internal Tab. `pageTabCheck` treats that as stale, with the same message as a switched Tab. So a Page Element whose Tab went to a browser page needs a fresh Window State, and keyboard routing falls back to today's route.
- `test/e2e/browser_pages_test.go`: `launch` now runs `winuse navigate <first Window> <url>` (3 tries, 1 s apart, while the title settles). On failure it reports `t.Errorf` and falls back to the old raw CDP `navigateFirstTab`, so later subtests still run. `test/e2e/README.md` says so. Not run (user-run only); `go vet -tags e2e` passes.

**Decisions, with reasons.**
- **Which browser pages are Tabs.** Over CDP a Tab cannot be told from other page targets, and Chrome lists frame WebUI (`tab-search.top-chrome`, side panels, the omnibox popup), DevTools windows, extension popups and Vivaldi's own `chrome-extension://` UI as type page. Mapping needs the title and the bounds, but `tab list` lists every target in the window, so those would pollute it. Through the extension a `tabId` is proof of a tab, so an extension's options page opened in a tab is a Tab there.
- **Never read or act inside.** The extension cannot attach to them, the content is the browser's, and keeping one rule on both routes is simpler. Frame (UIA) Elements still work, because the Window State is the UIA read with the note.
- **navigate on CDP attaches to the browser page.** It is the e2e's proven path (attach, then `Page.navigate`). back, forward and reload come with it at no cost there.
- **Through the extension, only navigate.** It needs a new bridge method. back and forward would need `chrome.tabs.goBack` and `goForward` plus another commit check, and reloading a New Tab page has no use. The refusal names navigate.
- **`chrome.tabs.update(tabId, {url})`** does not change the selected tab or the window's focus. The background.js foreground test (no `chrome.windows.update`, no `focused: true`) still passes, and a new test pins the exact call.
- **`tab list` output is unchanged.** The URL already shows the scheme, and adding a marker would change a format agents read.
- **`browser status`'s tab count** (`Connection.Pages`) still leaves browser pages out. It is a count, not a mapping, and is unchanged.

**Tests (offline).**
- `internal/page/tabs_test.go`:
  - `TestIsTabTarget`: 15 cases, including the localized title.
  - `TestListWindowTabsWithABrowserPageOnALaunchedBrowser`: the active New Tab page is Internal, and DevTools, tab search and the popup are left out. Nothing is attached.
  - `TestBrowserPageTabsOnTheExtension`: maps the localized "Thẻ mới" Tab, includes a `chrome-extension://` tab with a tabId, and checks that the fake refuses attach. `Winuse.navigateTab` changes the URL, after which attach works. `TabNow` returns `ErrTabGone` for a missing Tab, and neither `Browser.*` nor `Page.navigate` reaches the extension.
- `internal/page/reader_test.go`: `TestReadNoMatchingTab` now uses an unknown title. `TestReadBrowserPageTab` maps the New Tab page as Internal, and `Read` returns `InternalPageError` with the reason and no attach.
- `internal/bridge/navigatetab_test.go`: Refusal lets `Winuse.navigateTab` through without a session and refuses it with one. background.js has the exact handler and update call. I did not edit `frame_test.go`, because another worker was in it.
- `internal/helper/browserpage_test.go`:
  - tab list output on both routes;
  - tab new, switch and close from a New Tab page window, and closing the New Tab page itself;
  - navigate on a launched browser: an eval refusal first with no attach, then `Page.navigate` on the New Tab page's session and `ok https://a.test`;
  - navigate through the extension: back refused before the cursor moves; `Winuse.navigateTab`; no `Page.navigate` reaches the extension; the attach comes after the navigateTab; no activateTab;
  - navigate through the extension to `chrome://settings/`, and with `--wait none`: unread, no attach;
  - eval, wait selector, dialog and upload refused with the exact text and nothing attached; wait download reaches its own extension refusal;
  - the Window State note on both routes;
  - pageTabCheck stale on an internal Tab, the same as a switched Tab.
- `TestNothingSendsActivateTargetOrBringToFront` passes. The new tests also check the methods each fake received.
- Checks run: `go build ./...`, `go vet ./...`, `go vet -tags e2e ./test/e2e` and `go test ./...` all green. The new tests and the tab and navigate tests pass with `-count=5`. Touched files have LF endings and no added em or en dash.

**Shared and headless-parity-era files I edited:**
- `internal/bridge/bridge.go`: the const block and its comment.
- `internal/bridge/extension/background.js`: the header comment, one line in `handle`, and `navigateTab`.
- `internal/bridge/bridgetest/extension.go`: the `strings` import, the `navigated` field, one branch in `handle`, one check in `Target.attachToTarget` and `sessionCall`, and the new functions at the end.
- `internal/page/reader.go`, `internal/helper/pagestate.go`, `internal/helper/pageactions.go`, `test/e2e/browser_pages_test.go` and `test/e2e/README.md`.
- No SKILL.md or CONTEXT.md change: no sentence there became wrong.

**Live risks for the next e2e run.**
- **Which `chrome://` targets Chrome 155 lists as type page on a launched browser.** For example, whether a New Tab page footer or other WebUI not on `.top-chrome` appears. If it does, it shows in `tab list` as an extra Tab with that window's id.
- **When `chrome.debugger.getTargets()` reports the new URL after `chrome.tabs.update`** (pending or committed). The attach retry covers an early report. A late one only adds polling.
- **A navigate through the extension to a download URL** leaves the Tab on the New Tab page. winuse cannot tell it is a download, so it waits out the budget and fails with "still showed chrome://newtab/".
- **A navigate through the extension to an address that fails** (DNS). Whether chrome.debugger may attach to Chrome's error page. If not, the attach retries until the budget and fails with "reading the new page failed".
- **A third-party New Tab page** (`https://www.google.com/_/chrome/newtab`, `chrome-search://local-ntp/`) is not a browser page by this rule. winuse will try to attach to it, as before.
- **The Headed `tab switch` to a New Tab page on a launched browser** reads `document.visibilityState` over a session attached to it, which Chrome allows over CDP.
- **The e2e launch step.** The first Chrome Window found may not be titled after the New Tab page yet. The 3 tries cover that, and a failure is reported and then falls back.