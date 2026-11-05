# ADR 0012: Browser pages go through CDP inside winuse; use-browser is retired

## Status

Accepted (browser merge spec, `.scratch/browser-merge/spec.md`).
Reverses the v1 spec's out-of-scope "Integration with use-browser or any
browser-extension/CDP path" and its rule that the skill never mentions
use-browser.

Amended by ADR 0017: besides the Window's one Window State, a Tab target
(`<window>:<tab>`) has a Window State of its own, its Page read through CDP
alone, whether or not the Window shows it.

Amended (speed issue 07, below): a Page is read in one burst of
commands, and the role-less clickables come from
`DOMSnapshot.captureSnapshot` instead of an injected script.

Amended by ADR 0013: Decision 6's "Chromium browsers only" is reversed
for Electron and WebView2 Apps that winuse launches with a debugging
port, and for an opt-in attach to a running Electron App (headless-max
ticket 15).

## Context

winuse reads Chromium pages through UI Automation. That path is slow on
dense pages, cannot type or press keys without the foreground, and has
no waits, uploads or JS dialogs. use-browser does all of that over the
Chrome DevTools Protocol, but it is a second tool, so the agent juggles
two ref schemes and two skills, and use-browser's actions get no Agent
Cursor or Esc stop.

agent-desktop, the closest prior art, keeps the two tools apart and lets
the agent choose (`.scratch/browser-merge/agent-desktop-research.md`).

## Decision

1. One Window State per browser Window. The frame is read through UI
   Automation, and the active Tab's Page through CDP. Elements are
   numbered together.
2. winuse routes every Action by the Element's origin. The agent never
   picks a backend.
3. The CDP backend is built from use-browser's Go code: transport,
   discovery, launch, clone and the extension bridge. agent-browser's
   ideas are ported on top: the accessibility-tree snapshot, refs by
   `backendNodeId`, iframes, actionability checks, dialogs, uploads and
   waits.
4. The helper holds one connection per running browser process, so the
   user's own Chrome and a Chrome winuse launched are served at once. A
   browser Window always uses the connection of the process that owns
   it, whatever the pin. Commands without a Window prefer the extension;
   a winuse-launched profile is the fallback.
5. Without a Browser Connection, a Chromium Window falls back to the
   UI Automation read.
6. use-browser is retired. Chromium browsers only.

## Consequences

- Page Actions, typing and keys included, become Headless. Navigation
  replaces typing into the address bar.
- winuse gains a WebSocket client, an extension and a native messaging
  host. That is its first non-Win32 surface, and it must stay
  dependency-free (ADR 0003).
- An open debugging port can be driven by any process of the same user.
  winuse only launches with loopback ports and verifies who owns each
  one.
- Two staleness rules (UIA and page) meet behind one message.
- Electron, WebView2 and Firefox stay on UI Automation until a later
  decision.

## Amendment: tab switch on a launched browser is Headless (2026-11-03)

Ticket 05 of `.scratch/headless-max/`, under ADR 0013 rule 1. The
browser-merge spec kept `tab switch` Headed on a browser winuse launched
or cloned, because only the extension's switch had been measured. Probe
round 4 (`.scratch/headless-gaps/probes-round4.md` section 4) measured a
covered launched Chrome 156: UI Automation SelectionItem.Select on the
tab strip's TabItem switched the Tab, the Window's title and the item's
IsSelected followed, and the foreground and cursor stayed put. The
TabItems offer no Invoke. Branded Chrome ignores `--load-extension`, so
the extension cannot be loaded into a launched profile instead.

- `tab switch` on a launched or cloned browser finds the Tab's item in
  the Window's own UIA tab strip by the Tab's title (Chrome may append a
  state such as " - Memory usage - 17.3 MB"), selects it, and reads
  IsSelected back. The walk never enters the web content, whose ARIA
  tabs are TabItems too.
- No item, several items with the title, or no tab strip refuse with
  "nothing was sent" and the `--headed` retry. winuse never switches to
  the keys on its own.
- `--headed` keeps the old switch: the Window comes to the foreground and
  Ctrl+<n> or Ctrl+Tab steps switch the Tab, checked over CDP.
- Through the extension the switch stays `chrome.tabs.update`, with or
  without `--headed`.
- `Target.activateTarget` and `Page.bringToFront` stay forbidden.

## Amendment: Electron and WebView2 Apps at launch (2026-11-03)

Ticket 15 of `.scratch/headless-max/`, under ADR 0013 ("Reversals",
Decision 6). Probe round 5 (`.scratch/headless-gaps/probes-round5.md`
section 3) measured both hosts with the foreground unchanged.

- `launch-app <exe> --cdp` starts the App with a loopback debugging port
  that winuse picks: an ephemeral port, never one of the 9222 to 9224
  ports browser discovery probes. An Electron App (an `.asar` in the
  `resources` folder next to the executable) gets
  `--remote-debugging-port=<port>` in front of its own arguments, and
  native occlusion tracking off as `browser launch` does. Any other
  executable gets `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS` in its own
  environment only, never the machine-wide policy.
- The port is accepted only when its listener is the launched process or
  one of its descendants (an Electron main process, or WebView2's
  `msedgewebview2.exe` child), and it is registered for the process that
  owns the App's Windows. Those Windows then reach it through
  `WindowConnection`, whatever their window class. The browser commands
  and the commands without a Window never pick it.
- An App's Window maps to its page by the App's one page, else by title
  (`page.MatchAppTab`): Electron's DevTools may not answer
  `Browser.getWindowForTarget`, and a WebView2 page's title is not the
  Window's.
- A port that never appears within the launch budget leaves the App on
  UI Automation, said in a note. An App that is already running is never
  restarted; attaching to a running Electron App is a separate, opt-in
  route.
- Electron, WebView2 and Firefox otherwise stay on UI Automation, as in
  the consequences above.

### Addendum: attaching to a running Electron App

- `browser attach <window>` gives a running Electron App a Browser
  Connection without a restart (probe round 3): winuse sends the App's
  main process the Node debug signal that `process._debugProcess` sends
  (a thread at the handler the process publishes in its
  `node-debug-handler-<pid>` mapping), finds the inspector among the
  loopback ports that process listens on, and installs a small shim with
  one `Runtime.evaluate`. The shim serves browser-level CDP over Electron's
  `webContents.debugger`, routed like the winuse extension: only
  `Target.getTargets`, `attachToTarget` and `detachFromTarget` at browser
  level, a page session's id is its target id, and `Page.bringToFront`
  and `Target.activateTarget` are refused.
- It runs winuse's code inside the user's App, so it is opt-in per App:
  the skill tells the agent to ask the user first, and nothing attaches by
  default. An App with the nodeCliInspect fuse off publishes no mapping
  and is refused with nothing sent (Discord 1.0.9266).
- Any process of the same user can reach the open inspector, so
  `browser detach`, a dropped connection and the helper's exit all detach
  the debuggers and close the inspector.
- WebView2 stays launch-time only.

## Amendment: the Page read in one burst, marks from DOMSnapshot (2026-11-05)

Speed issue 07 (`.scratch/speed/issues/07-page-read-burst.md`), from
research recommendation 7. A warm read of a Page used about 10 commands
one after another, then one `DOM.describeNode` per mark of the
`clickables.js` pass (16 at a time), and the same again for each iframe,
one iframe after another. On a page with 200 role-less clickables and one
cross-origin iframe that was 225 commands in 32 round trips, one after
another.

- Each document's commands go out together, in the order the browser then
  runs them: the frame tree, the accessibility tree and
  `DOMSnapshot.captureSnapshot`. The dialog probe, every cross-origin
  iframe's read (with its `DOM.getFrameOwner`) and the Tab's own read go
  at the same time. Same-origin iframes the Tab's tree left out are read
  together right after it. A Window's Tab is found with its bounds asked
  for together too. The same page now takes 10 commands in 3 round trips.
- `DOM.enable` and `Accessibility.enable` go once per session, with its
  first read. The extension detaches a Tab nobody drove for 5 minutes and
  attaches it again on the next command without telling winuse, which
  turns every domain off. So a read more than 4 minutes after the
  session's last one turns them on again. `DOMSnapshot.enable` is not
  sent. That `captureSnapshot` answers without it is unverified until the
  live check runs; a read whose snapshot fails falls back to the pass.
- The marks (role-less clickables, password fields) are computed from the
  snapshot by the rules of `clickables.js`, with its caps (5000 elements
  looked at, 200 found, 80 UTF-16 units of text). The snapshot gives
  backendNodeIds, attributes, boxes, layout text and four computed
  styles: cursor, visibility, display, text-transform. Nothing runs in the
  page and nothing is left in it.
- Differences from the pass, on purpose: password fields in shadow trees
  and in iframes the Tab's tree holds are marked too, so their values are
  never shown.
- Differences, accepted: innerText is approximated, not read. Text a
  shadow tree renders in place of an element's children is not seen, and
  a `white-space: pre` text of spaces only is taken for no text. An
  element under a `display: contents` parent that sets `cursor: pointer`
  itself is compared with the nearest boxed ancestor's cursor, because the
  snapshot has no style for a node without a box.
- `chrome.debugger` lists DOMSnapshot among its domains, so the snapshot
  also goes through the extension. A browser or bridge that refuses it
  (no such method, or the extension's refusal) gets the `clickables.js`
  pass, and that connection is not asked again. Any other failure of the
  snapshot falls back for that read only.
