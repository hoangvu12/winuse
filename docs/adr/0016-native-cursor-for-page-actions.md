# ADR 0016: Page Actions show the native Agent Cursor

## Status

Accepted (browser-ux issue 04, `.scratch/browser-ux/issues/04-page-cursor-transform.md`,
2026-11-04). It keeps ADR 0006 (the cursor never in winuse's own
captures) and ADR 0014 (one cursor per Agent, placed just above its
Window) for Actions that reach a Page through a Browser Connection.

## Context

The user asked for the Agent Cursor on browser Actions that go through
the winuse extension and CDP. Page Element Actions already glided the
native cursor, mapped from the node's CSS box with the page's
devicePixelRatio (zoom included) from the **UI Automation document
Element's** top-left. That origin needed a round trip into Chromium per
Action, and was missing when Chromium had not built its accessibility tree
(a covered or cold Window: the user's Brave sat maximized behind another
App the whole session). The cursor then glided to the Window's centre and
rippled the whole Window.

Two designs were on the table:

1. **An in-page cursor** drawn by the extension (as Claude in Chrome and
   Browser Use show): a DOM overlay injected into the Tab.
2. **The native cursor**, mapped from the Page to the screen.

## Decision

1. Page Actions keep the native Agent Cursor. No cursor is drawn inside a
   Page.
2. The page-to-screen origin is the browser Window's one visible
   `Chrome_RenderWidgetHostHWND` child (the Tab's content area, read with
   `GetWindowRect` in the helper's per-monitor-aware process, covered or
   not). With none, or several (DevTools docked, a side panel), it is the
   UI Automation document Element as before. With neither, the Action runs
   without the cursor rather than gliding to the Window's centre.
3. The ripple plays when the Page accepts the first event that acts (a
   press, a key, text, a wheel step) or the browser accepts a navigation,
   not after the Action's read-back or the new page's load.

## Consequences

- An in-page cursor would appear in `Page.captureScreenshot` and in the
  user's own captures (it is page content; there is no exclusion), is
  visible to and stylable by the page's scripts and CSS, needs injecting
  into every frame against each site's CSP, does not exist on a browser
  page (`chrome://`) or in a launched Electron App without the extension,
  and would duplicate the Agent's colour, name and label for each route.
  The native cursor already has all of these, through every route.
- Like every Headless Action's cursor, a Page Action's cursor sits just
  above its browser Window in the z-order: when another Window covers the
  browser, the cursor is covered too. That is ADR 0014's choice, kept.
- The cursor's position follows the content child window, so a zoomed
  Page, a scrolled Page (the Action scrolls the node into view first) and
  any DPI are covered by the existing transform; a docked DevTools falls
  back to UI Automation.
