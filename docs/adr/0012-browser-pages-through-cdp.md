# ADR 0012: Browser pages go through CDP inside winuse; use-browser is retired

## Status

Accepted (browser merge spec, `.scratch/browser-merge/spec.md`).
Reverses the v1 spec's out-of-scope "Integration with use-browser or any
browser-extension/CDP path" and its rule that the skill never mentions
use-browser.

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
