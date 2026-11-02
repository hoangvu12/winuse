# 07: Tabs

**What to build:** `tab list|new|close|switch <window>`.

- New tabs open in the background.
- winuse never calls bringToFront or activateTarget.
- switch is Headless through the extension. Otherwise it is Headed, per the probe's result.

See spec: "Tabs".

**Blocked by:** 04

**Status:** done

- [x] Offline tests for the commands and the Headless or Headed routing of switch

## Comments

### 2026-11-02: probe decisions (accepted by the user)

From `../probe-results.md`:

- **`tab new` is Headless.** `Target.createTarget {background:true}` and the extension's background tab (`chrome.tabs.create active:false`, grouped) both kept the foreground and the z-order at every scale. `{newWindow:true, background:true}` also kept them; the new window opened below the cover, not minimized.
- **`tab switch` through the extension is Headless.** `Winuse.activateTab` (`chrome.tabs.update active:true`) made the tab active with the foreground and z-order unchanged.
- **`tab switch` on a launched or cloned browser stays Headed.** It was not measured: the probe never calls `Target.activateTarget`, which is forbidden. Keep the spec's rule until a later probe shows a Headless route.

### 2026-11-02: landed

**Command surface.** One wire command per subcommand. All four use `protocol.TabArgs{Window, Tab, URL}` (new file `internal/protocol/tab.go`) and a 30 s budget (`BrowserTimeout`), because the first command may dial and wait for an approval prompt. There is no `--headed`: the route comes from the Browser Connection.

| Command | Wire name | Route | Batch step |
|---|---|---|---|
| `winuse tab list <window>` | `tab-list` | read only, observation-only (no gate, no cursor, no Esc scope) | no: it prints, and a Batch prints only ok |
| `winuse tab new <window> [url]` | `tab-new` | Headless | `tab new [url]` |
| `winuse tab close <window> <tab>` | `tab-close` | Headless | `tab close <tab>` |
| `winuse tab switch <window> <tab>` | `tab-switch` | Headless through the extension, Headed otherwise (always) | `tab switch <tab>` |

- `tab list` prints one line per Tab of that browser window: `* bbbb0000 "Docs" https://docs.test/`. The `*` marks the active Tab (else a space). The title is collapsed and cut at 80 characters with `...`. Through the extension the order is the tab strip's; on a launched browser it is `Target.getTargets`' order.
- A `<tab>` is a tab id from `tab list`: the first 8 characters of the target id, lowercased (use-browser's `shortID`). Lookup is a case-insensitive prefix of at least 4 characters, among that Window's Tabs only. A prefix that is too short, matches nothing or matches several Tabs fails with the candidates and `winuse tab list <w>`. Nothing is guessed.
- `tab new` prints `ok <id>`. A URL without a scheme gets `https://`; no URL opens about:blank.
- Batch steps also accept the wire names (`tab-new ...`).

**Decisions.**
- **The last Tab is refused, with no `--force`.** Closing a Window's last Tab closes the Window. `tab close` refuses it: "tab X is the last tab of window W, and closing it would close the Window; nothing was sent", hint `to close the Window, run winuse close-window W`. That keeps one explicit way to close a Window, and close-window already handles an app that asks to stay open.
- **Where `tab new` opens on a launched or cloned browser.** `Target.createTarget` has no window parameter. Its parameters are url, width, height, browserContextId, enableBeginFrameControl, newWindow, background and forTab (checked against the protocol definitions). With `newWindow:false`, Chrome opens the Tab in the profile's last active window. winuse sends `{url, background:true, newWindow:false}`, then reads the new Tab's window back with `Browser.getWindowForTarget`. If the Tab landed in another window, winuse closes it again and fails with "Chrome opened the new tab in another of its windows, so winuse closed it again". The hint is `winuse activate-window W` (which takes the foreground), then retry.
- **`tab new` through the extension.** `Target.createTarget` now carries `windowId`. background.js passes it to `chrome.tabs.create({url, active:false, windowId})`, and the Tab is still grouped under "winuse". The window read-back runs there too.
- **Headed switch, before anything is sent.** winuse resolves the Window, lists its Tabs, looks the Tab up and plans the keys from the latest Window State's tab strip:
  - the strip is the UIA `tab` items that are not Page Elements;
  - it is trusted only when it has exactly as many items as the window has Tabs;
  - an item matches a title exactly, or as the title followed by a space or a comma and a suffix such as "- Audio playing", and the match must be unique.
- **Headed switch, the keys.** `runAction` shows the banner, arms the swallowing Esc scope and brings the Window to the foreground. Then:
  - `Control_L+<n>` for positions 1 to 8, or `Control_L+9` for the last Tab;
  - otherwise, or if that did not show the Tab, `Control_L+Tab` steps (`Control_L+Shift_L+Tab` when the Tab is nearer on the left), at most one full cycle, each step waiting up to 300 ms.
- **Headed switch, the check.** The check is the target's `document.visibilityState`, read over its own attached session (`page.Visibility`, detached afterwards). A failure says "tab X is not the active tab of window W after the switch", hint "keys were sent to the Window; read winuse tab list W before repeating it". winuse does not click the strip: keys are enough and need no point.
- **Already active.** `tab switch` to the active Tab prints ok and sends nothing, not even an activation.
- **Agent Cursor.**
  - `tab new` glides to the Window, labelled `open tab`.
  - `tab close` and the extension's `tab switch` glide to the Tab's strip item when the latest Window State has it, else to the Window. The labels are `close tab "News"` and `switch to "Inbox"`.
  - The click effect plays once the act is delivered.
  - The Headed switch shows the banner instead, and `tab list` shows nothing.
- **Esc.** Headless tab Actions run in a passive scope. A stop during the cursor's travel sends nothing (exit 130 with the "nothing was sent" hint). A stop after the act outranks the outcome. The Headed switch checks the latch between key steps.
- **Batch scope.** `stepHeadless` asks `tabStepHeadless`. tab-new and tab-close are Headless. tab-switch is Headless only when the Window's connection is `ModeExtension`, looked up with a 2 s bound. Unknown counts as Headed, the safe side.
- **Failures.**
  - Every refusal before the act says "; nothing was sent".
  - A CDP error answer to createTarget, closeTarget or activateTab also says nothing was sent: the browser did not act.
  - A dropped connection or a timeout says the browser may have done it.
  - An extension Problem (paused, cannot) keeps its sentence and next command.
- **Locked desktop and administrator Windows.** The Headless tab commands do not refuse them, because they go over CDP, not into the Window. The Headed switch gets runAction's refusals.

**Per file.**
- `internal/page/tabs.go` (new):
  - `TabID`, `MinTabRef`, and `WindowTabs` with `IsActive`;
  - `ListWindowTabs`: FindTab, then the Tabs of the same window (the extension's windowId and active flag, or `Browser.getWindowForTarget` per Tab);
  - `ResolveTab` and `TabRefError`, `NewTab` and `OtherWindowError`;
  - `TabWindow`, `TabOpen`, `CloseTab`, `ActivateTabThroughExtension` and `ExtensionTabActive`;
  - `Visibility`: one session per Tab, `Runtime.evaluate document.visibilityState`, and `Close` detaches;
  - `tabURL`.
- `internal/page/pagetest/tabs.go` (new package): a fake launched Chrome on `cdptest`, with windows, bounds and Tabs in strip order. It answers:
  - getTargets, newest first, plus a service worker;
  - getWindowForTarget;
  - createTarget, into the `LastActive` window;
  - closeTarget (`KeepOpen` plays a page that asks to stay);
  - visibility of each window's selected Tab (`SlowSwitch` delays it);
  - `Press(chord)`, which plays Chrome's Ctrl+1..9, Ctrl+Tab and Ctrl+Shift+Tab in the `Foreground` window.
- `internal/helper/tabs.go` (new):
  - the four handlers;
  - `tabWindow`: resolve, the browser through `livePages`, the connection, the other-process check, `ListWindowTabs`;
  - the error mappings;
  - `tabHeadless`, `tabCursor`, `tabStrip` and `stripPosition`, `planTabKeys`, `tabSwitchHeaded` and `tabStepHeadless`.
- `internal/cli/tab.go` (new): `parseTabArgs`, `tabCmd`, `parseBatchTabStep` and `checkTabRef`.
- `internal/protocol/tab.go` (new): `TabArgs`.
- `internal/uia/stored.go` (new): `(*Client).StoredElements(hwnd)`, a copy of the latest Window State's Elements without COM.
- `internal/bridge/extension/background.js`: `createTab(url, windowId)`. `manifest.json` is bumped to 0.1.2. The build hash changes anyway, so ext status self-updates.
- `internal/bridge/bridgetest/extension.go`:
  - createTarget takes `windowId` (default window 1) and gives the tab a tabId;
  - closeTarget removes the tab;
  - activateTab makes the tab its window's only active one.

**Tests.**
- page:
  - TabID;
  - ResolveTab: full id, prefix, case, spaces, ambiguous (exact text), too short, none;
  - tabURL;
  - ListWindowTabs on a launched browser: only window 1, in getTargets order; the other window's Tab with the same title and the service worker are left out;
  - ListWindowTabs on the extension, with no `Browser.*` reaching it;
  - NewTab params (background, newWindow false, no windowId), and a Tab landing in another window;
  - NewTab on the extension, naming the window;
  - Visibility: the selected Tab visible, a background Tab hidden, one attach per Tab, all detached.
- helper, tab list:
  - launched: exact output for two windows, with no cursor, no scope and no input;
  - extension: exact output, including the 80 character cut.
- helper, failures:
  - no connection, for all four commands: the exact sentence, `run winuse browser status chrome`, and "nothing was sent" on the three that act;
  - Notepad, and another Chrome process;
  - argument failures.
- helper, tab new:
  - launched: the output, a background Tab, the cursor's label and target, a passive scope;
  - landing in another window: closed again, the exact message, the activate-window hint;
  - on the extension, in window 2.
- helper, tab close:
  - launched: another window's id refused, the cursor to the strip item, the last Tab refused, a Tab that stays open;
  - on the extension: a short id, the last Tab.
- helper, tab switch:
  - through the extension it is Headless: activateTab, no activation, no keys, a passive scope; a switch to the active Tab does nothing;
  - stopped during the cursor's travel: nothing sent;
  - on a launched browser it is Headed: activate, two Ctrl+Tab, a swallowing scope, no cursor;
  - with the strip: `Control_L+3`;
  - a Tab that never shows: one cycle, the exact failure;
  - `planTabKeys`: 7 cases.
- helper, Batch scope: all-Headless through the extension, a Headed switch on a launched browser, and a nil Window counted as Headed.
- helper, registry: registration, the gate, the budget, no `headed`, and the Batch kinds.
- helper, `TestNothingSendsActivateTargetOrBringToFront`: it scans every non-test Go file for a `.Call` or `.CallSession` naming either method, and allows the names only in bridge.go's refusal table. Each test backed by a fake also checks the methods the fake received.
- cli: parsing (14 cases), usage errors exit 2, batch tab steps (both spellings, with `tab list` refused), the usage text and the budgets.
- protocol: `TestBatchStepKindsAreBatchableActions` now expects the three tab kinds.
- Checks run:
  - `go build ./...`, `go vet ./...`, `go vet -tags e2e ./test/e2e` and `go test ./...` are all green;
  - the tab tests pass 5 times in a row;
  - `node --check` passes on background.js;
  - go.mod is unchanged, and all touched files have LF endings.

**Shared and headless-parity-modified files I edited (minimal):**
- `internal/protocol/protocol.go`: four budget entries and three `BatchStepKinds`.
- `internal/protocol/protocol_test.go`: the expected kinds.
- `internal/helper/state.go`: the `tabCommands` loop, and `tab-list` in `observationOnlyCommand`.
- `internal/helper/headless.go`: one `case` in `stepHeadless`.
- `internal/helper/registry_test.go`: four names.
- `internal/cli/cli.go`: two lines in the headless paragraph, the four usage entries, and `case "tab"`.
- `internal/cli/batch.go`: one `case` in `parseBatchStep`.

No CONTEXT.md or ADR change: Tab and Window are already terms, and the routing follows ADRs 0010 to 0012.

**Live risks:**
- **New tab placement.** Where Chrome really puts a `Target.createTarget {newWindow:false}` Tab when several windows are open (assumed: the profile's last active window), and whether `Browser.getWindowForTarget` reports the new Tab's window at once.
- **Extension tab create with a windowId.** Whether `chrome.tabs.create({windowId, active:false})` leaves the foreground and z-order alone for a window that is not the last focused one. The probe measured creation without a windowId.
- **Tab shortcuts.** Whether Ctrl+1..8 reach Chrome when the page has focus and handles those keys itself. Ctrl+Tab is reserved by Chrome, so the fallback steps cover a page that takes Ctrl+<n>. Edge, Brave, Vivaldi and Opera are assumed to use Chrome's shortcuts.
- **Visibility check.** After the activation, an occluded or partly covered Window may report the selected Tab as hidden, which would read as a failed switch.
- **Strip item names.** Chrome may name tab items with suffixes beyond the title followed by a space or a comma (audio, memory, crashed). Pinned Tabs whose names carry no title break the position plan, and the switch falls back to steps.
- **Mixed connections.** A connection to the extension while the Window is a winuse-launched Chrome (or the reverse) gives the "another process" failure, as in ticket 04.
