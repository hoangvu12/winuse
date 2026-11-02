Status: ready-for-agent (the ticket 03 probe ran on 2026-11-02; results in `probe-results.md`)

# Browser merge: use-browser becomes part of winuse

## Problem statement

winuse reads Chromium pages only through UI Automation. That works for clicks and set-value, but:
- typing, keys and navigation take the foreground;
- dense pages are slow to read and thin when covered;
- unnamed web elements are hard to address;
- there are no page waits, uploads, JS dialogs or downloads.

use-browser does all of that over the Chrome DevTools Protocol (CDP), but it is a second tool with its own skill, its own refs and no Agent Cursor or Esc stop. Agents juggle two tools, and nothing ties a page to the Window the user sees.

Research in this folder:
- `agent-desktop-research.md`: agent-desktop never merged; it hands off to agent-browser.
- `agent-browser-vs-use-browser.md`: build from use-browser's Go code and port agent-browser's ideas.

## Solution

winuse gains a CDP backend, and use-browser is retired. A Chromium Window has one Window State:
- the browser frame (tab strip, address bar, menus, dialogs) is read through UI Automation;
- the active Tab's Page is read through CDP.

Every Element has one Element Index, and winuse routes each Action to the right backend by the Element's origin. The agent never chooses a backend.

Page Actions are Headless: CDP input goes to the page without the foreground, pointer or keyboard. They show the Agent Cursor and stop on Esc like any other Headless Action. Without a Browser Connection, a Chromium Window falls back to today's UI Automation read, so nothing breaks.

## User decisions (2026-11-02)

1. use-browser is retired. winuse replaces its CLI and skill.
2. Connection defaults follow use-browser:
   - prefer the extension when it is connected to that browser, because it drives the user's real browser with their logins;
   - otherwise use a browser winuse launched with its own profile;
   - `clone` and `connect` stay available as explicit routes.
3. Chromium only for now: Chrome, Edge, Brave, Chromium, Vivaldi, Opera. No Firefox, Electron or WebView2 yet.

## User stories

1. As an agent, I want one Window State for a browser Window, with the frame and the page numbered together, so that I never pick a tool.
2. As an agent, I want to click, fill, type, press keys and scroll in a page without taking the user's foreground.
3. As an agent, I want `navigate <window> <url>`, back, forward and reload, so that I never type into the address bar.
4. As an agent, I want hover, drag, select, check and upload on page Elements to work headless.
5. As an agent, I want JS dialogs, file choosers and downloads handled through winuse, never as surprise native windows.
6. As an agent, I want to wait for a selector, text, URL or load state.
7. As an agent, I want refs that survive until the page changes, and a clear "read the Window State again" when they go stale.
8. As an agent, I want cross-origin iframes in the Window State.
9. As a user, I want the agent to use my real browser and logins through the extension, without a permission prompt per command.
10. As a user, I want agent tabs to open in the background and the browser never to jump to the front.
11. As a user, I want the Agent Cursor to show page Actions in the right place, and Esc to stop them.
12. As a user, I want one install (winuse) and one skill, with use-browser retired cleanly.

## Implementation decisions

ADR 0012 records the merge. CONTEXT.md gains Page, Tab and Browser Connection.

### Browser Connection (tickets 01, 02)

- Port use-browser v0.9.0's Go code (https://github.com/hoangvu12/use-browser, commit `14aac7d`) into `internal/cdp` and `internal/browser`:
  - the zero-dependency WebSocket client and the `msgConn` abstraction;
  - discovery, the `DevToolsActivePort` reader, port-owner checks and the refusal to guess between two debuggable browsers;
  - the Windows browser and profile tables;
  - the clone flow;
  - the extension bridge and native messaging host.
- The CDP client routes events instead of dropping them. Dialogs, downloads, the file chooser and auto-attach need them.
- The helper holds one persistent connection per browser for its lifetime, so the Chrome 144+ approval prompt appears once per helper, not per command. Follow agent-browser's approval handling: hold the handshake up to 20 s, never open a second prompt while one is pending, and send `Runtime.runIfWaitingForDebugger` after attach.
- Verify every endpoint: a loopback-only WebSocket URL, and the listener port owned by the expected browser process (agent-desktop's checks, `agent-desktop-research.md` section 5).
- `Emulation.setFocusEmulationEnabled` on every attach, so a background page acts as if focused.
- Commands, replacing use-browser's: `winuse browser status|use|launch|clone|connect|stop|ext install|ext status`. Exact names are settled in ticket 01. `status` diagnoses every failure with a sentence the agent can relay.
- The extension never sees browser-level methods (`Browser.*`). Features that need them (downloads) work on launched and cloned browsers only, and say so on the extension.

### Mapping a Window to its Tab (ticket 03 probe, then 04)

- A Chromium Window (class `Chrome_WidgetWin_1`, owned by a connected browser's process) maps to the active Tab of that browser window.
- The match, settled by the probe (`probe-results.md`), needs both of these:
  - `Browser.getWindowForTarget` bounds times the Window's DPI scale against its GetWindowRect;
  - the active tab's title against the Window title, minus the " - <browser>" suffix.
- On the extension, the bridge supplies the bounds from `chrome.windows`, because `Browser.*` is refused there.
- An ambiguous match is an error that names the candidates, never a guess. The one exception is not a guess: a Window that earlier mapped to one browser window without doubt keeps it, when exactly one candidate is in that browser window (ticket 04's comments).

### One Window State (ticket 04)

- The frame comes from UI Automation as today. The UIA document subtree of the active Tab is replaced by a CDP snapshot:
  - `Accessibility.getFullAXTree`, plus a cursor-pointer pass for role-less clickables;
  - rendered in winuse's Window State grammar (ADR 0004), with the same roles, states and Secondary Actions vocabulary where they map.
- Element Indexes are numbered across both parts. Each index records its origin: a UIA Element, or a `backendNodeId` with its frame session.
- Staleness: a UIA Element follows today's rule. A page Element is stale after navigation or when its node is gone. Both fail with the same "read the Window State again" message.
- Cross-origin iframes: flattened auto-attach with a session per frame, one level deep at first. The extension bridge passes child sessions through.
- No Browser Connection, or a failed snapshot: today's UIA-only read, with a one-line note saying why.

### Routing and page Actions (tickets 05, 06)

Page Element Actions go over CDP, frame Element Actions over UIA. Both are Headless unless `--headed`.

- **click:**
  - `DOM.scrollIntoViewIfNeeded`, the box-model centre, `mouseMoved` before the press;
  - refuse with the blocker named when covered, disabled or outside the viewport, with "nothing was sent";
  - race each mouse event against `Page.javascriptDialogOpening`.
- **set-value:** focus through CDP (`DOM.focus`, no OS focus), select all, `Input.insertText`, then read the value back.
- **type-text and press-key** on a Window whose focused Element is in the Page: `Input.insertText` and `Input.dispatchKeyEvent`, with modifiers. Headless, and no `--post` needed. This resolves the address-bar case for pages: `navigate` replaces typing in the omnibox.
- **scroll:** the Element form uses `mouseWheel` at the Element, or `DOM.scrollIntoViewIfNeeded` for into-view. The `@x,y` form on a page point also goes through `mouseWheel`, Headless.
- **hover, drag, select, check:** trusted CDP input (agent-browser's sequences). For page Elements these are Headless, unlike native hover and drag.
- **upload:** `Page.setInterceptFileChooserDialog`, then `DOM.setFileInputFiles`. A click that would open a native file chooser on an intercepted page never shows one.
- **navigate, back, forward, reload:** `Page.navigate` and history.
- **dialog:** subscribe to `Page.javascriptDialogOpening`. While a dialog is open, every other page Action fails fast and names `winuse dialog <window> accept|dismiss [text]`. Alerts are not auto-accepted.
- **downloads:** `Browser.setDownloadBehavior` into a folder winuse reports, on launched and cloned browsers.
- **wait:** for a selector, text, URL or load state, with one default timeout under the request budget.
- **eval:** runs a JS expression in the page and prints the result (use-browser's `js`). This is a page Action, so it appears in the Window State history for the user, but it shows no cursor.
- **Agent Cursor:** a page rectangle is converted to physical screen pixels from the UIA document Element's bounds and the page's `devicePixelRatio`. The probe measured this within 1 px at every display scale and page zoom.
- **Esc stop:** page Actions run in the passive Esc scope.

### Tabs (ticket 07)

- `tab list|new|close|switch <window>`.
- New tabs are created in the background (`Target.createTarget` with `background: true`, or the extension's background tabs).
- Never `Page.bringToFront` or `Target.activateTarget`.
- `tab switch` is Headless through the extension (`chrome.tabs.update` active, without focusing the window). Otherwise it stays Headed: the probe confirmed that the extension's switch keeps the foreground, but it never measured a switch on a launched browser.

### Retiring use-browser (ticket 08)

- The winuse skill teaches browsers. use-browser's skill and binary are no longer installed by anything.
- `install.ps1` prints a one-line note when it finds use-browser installed: how to remove it, and that winuse replaces it.
- A deprecation notice for the use-browser repo README is drafted in this folder. The user publishes it; agents do not push to that repo.
- License: use-browser has no license file. The user adds one, or states in winuse's `third_party/` that the ported code is the same author's and is relicensed under winuse's MIT license.

## Testing decisions

- Offline (default `go test ./...`):
  - the CDP client against a fake WebSocket server;
  - discovery and endpoint verification over faked process tables;
  - snapshot rendering from recorded `getFullAXTree` JSON fixtures (like `internal/wstate/testdata`);
  - Window-to-Tab matching over faked bounds and titles;
  - routing by origin;
  - the click actionability decisions;
  - the CSS-to-screen transform;
  - dialog-blocked failures;
  - a parity test that every documented command has a handler.
- Opt-in e2e (`WINUSE_E2E=1`, user-run only), against real Chrome and Edge in isolated profiles with local probe pages:
  - drag, pointer capture, dialogs, iframes, upload, downloads;
  - every page Action with another Window covering the browser, and the foreground unchanged afterwards.
- Ticket 03 is a user-run probe, not a test suite. It measures the unknowns before 04 to 07 are built.

## Out of scope

- Firefox (WebDriver BiDi), Electron and WebView2 apps. They are recorded as later options in `.scratch/headless-coverage/research.md`.
- Headless Chrome (`--headless`): the user's browser is a real window.
- Network interception, cookies and storage editing, device emulation, recording.
- A machine-wide input lease for several agents.
