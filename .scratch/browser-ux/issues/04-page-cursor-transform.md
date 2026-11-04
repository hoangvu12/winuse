# 04: The Agent Cursor for browser Page Actions

Status: done

**Evidence.** In the session Brave was maximized behind Zeron (the user's
foreground). Every UI Automation read said:

```
Note: the page's accessibility tree is still thin; Chromium may not build it while the Window is fully covered.
```

The user reports no cursor for the browser Actions that go through the
extension.

**What exists.** Page Element Actions (`runPage`) already glide the native
Agent Cursor: `pageScreenRect` maps the node's CSS box with `page.ToScreen`
from the **UIA document Element's** live top-left and the page's
devicePixelRatio (zoom included; `Aim` scrolls the node into view first).

**Root cause of the gap.** The transform's origin is a UI Automation read
(`ElementBounds(o.DocIndex)`): a round trip into Chromium before every
Action, and missing when the frame's tree is thin (covered or cold Window).
When it fails, `cursorTravelRect` falls back to the Window's bounds, so the
cursor glides to the middle of the browser and ripples the whole Window, which
looks like a bug.

**Decision (ADR 0016).** Keep the native cursor; do not inject an in-page one.

- An in-page cursor would show in the Page's own screenshots and the user's captures (`Page.captureScreenshot` has no exclusion), is visible to and stylable by the page, needs a CSP-proof injection in every frame, cannot show in a minimized or covered Window, and would duplicate the Agent identity, colour and label code per route (extension, launched browser, cloned browser, Electron).
- The native cursor already has per-Agent colour and name (ADR 0014), stays out of winuse's captures (ADR 0006), and is placed above the Window like every other Headless Action.

**Fix.** The origin comes from Win32 first: the browser Window's one visible
`Chrome_RenderWidgetHostHWND` child is the Tab's content area (checked on the
user's Brave: one visible child at the content area, covered or not). Its
`GetWindowRect` is physical pixels in the helper's per-monitor-aware process
and costs no round trip. Several visible ones (DevTools docked, a side panel)
or none fall back to the UIA document Element. When neither is known, the
cursor stays where it is instead of gliding to the Window's centre.

## Comments

Done on branch browser-ux: `win.ChromiumContentRect` (`internal/win/browserwindow.go`), `Services.PageContent`, `pageScreenRect`/`pageDocRect`, no cursor without an origin; ADR 0016. Tests: `TestPickContentRect`, `TestPageCursorOriginIsTheContentWindow`.
