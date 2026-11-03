# ADR 0012: Browser pages go through CDP inside winuse; use-browser is retired

## Status

Accepted (browser merge spec, `.scratch/browser-merge/spec.md`).
Reverses the v1 spec's out-of-scope "Integration with use-browser or any
browser-extension/CDP path" and its rule that the skill never mentions
use-browser.

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
