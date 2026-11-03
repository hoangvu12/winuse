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
