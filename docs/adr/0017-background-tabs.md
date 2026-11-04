# ADR 0017: Agents work in background Tabs, addressed as `<window>:<tab>`

## Status

Accepted (background-tabs spec, `.scratch/background-tabs/spec.md`,
decided by the user on 2026-11-04). It amends ADR 0012 Decision 1 (one
Window State per browser Window, its Page the active Tab's) with a second
kind of target, and ADR 0014's Consequence that a Page lane is keyed by the
browser Window, for that kind of target only. ADR 0016's native cursor is
kept for the Tab a Window shows.

## Context

The user keeps using the Tab they are looking at in their own browser,
while agents work in other Tabs of the same window, at once, never moving
the user's Tab or the foreground. winuse could only act on the Tab a Window
shows, so an agent had to `tab switch` there first.

Every product that does this (Gemini in Chrome, chrome-devtools-mcp,
Playwright MCP's extension, Nanobrowser, Crawlio) drives the hidden Tab
over CDP with focus emulation and `Page.setWebLifecycleState`, applied only
to the Tabs an agent targets.

## Decision

1. **A Tab target is `<window>:<tab>`.** The Window part is a Window ID or
   App name, as everywhere; the tab part is a tab id from `tab list` or
   `tab new` (4 to 32 letters or digits, after the last colon). It is one
   token, so every command keeps its positional grammar, a Batch on a Tab
   target works unchanged, and hints name the target the agent passed.
   Rejected: a global `--tab <id>` (one more flag to carry on every step
   and hint) and a bare tab id (the Window part picks the Browser
   Connection without a search across browsers). The Tab may live in any
   window of that browser: a popup a link opened is reached through the
   Window that opened it.
2. **A Tab target is read and acted on over CDP alone.** Its Window State
   is the Page under a `document` Element 0 named after the Tab, with no
   UI Automation, which only shows the active Tab. Its Element Indexes
   belong to the Agent and the Tab, apart from the Window's own Window
   State. Every Action is a Page Action and Headless; `--headed`, `--post`,
   `@x,y`, `find`, `zoom`, the partial reads and the commands with no Page
   route are refused with nothing sent. Nothing activates the Tab or its
   window. A Tab target that is its window's active Tab is still a Tab
   target: the same store, the same lane.
3. **A held Tab is kept alive, and only a held Tab.** Each command on a Tab
   target holds it: `Emulation.setFocusEmulationEnabled(true)` (already in
   `AttachPage`), `Page.setWebLifecycleState({state:"active"})`, and through
   the extension `chrome.tabs.update(autoDiscardable:false)`, remembering
   the old value. Five minutes after the Agent's last command on it, the
   helper releases it (focus emulation off, `autoDiscardable` restored);
   the extension restores `autoDiscardable` itself when its debugger
   detaches, so a helper that died leaves nothing behind.
4. **The user's Tab is guarded by the extension.** A Tab created with a
   held Tab as its opener is marked. When it becomes the active Tab, the
   extension selects the Tab the user had before (`chrome.tabs.update
   active` inside the window, never the window itself), groups the new
   Tab with its opener, and minimizes a popup window it lives in. The
   Action reports the new Tab in a `Note:` line, as before (opened.go).
   `<select>` lists and date, time and colour inputs open a native popup
   on a click, drawn on the user's screen, so a click on one in a
   background Tab is refused and set-value sets it through the DOM. JS
   dialogs and file choosers already go through CDP (page/watch.go).
5. **No Agent Cursor in a background Tab.** Nothing of it is on the
   screen. Considered: marking the Agent's tab group (a "● " prefix while
   it acts) — rejected, it renames a group in the user's tab strip on every
   Action and races the user's own edits; the group already carries the
   Agent's name and colour, and the browser shows its debugging bar. A Tab
   target that is the active Tab shows the cursor as ADR 0016 places it.
6. **A lane per Tab.** A command on a Tab target takes the lane
   `3 tab <target id>` instead of the Window's Page lane (ADR 0014's
   Decision 1 as written); lanes still sort desktop, threads, Pages, Tabs.
   Agents in different Tabs of one window run at once. Commands without a
   Tab target keep the Window's Page lane.
7. **Routes.** All Browser Connections read and act on Tab targets the same
   way. Only the extension has `autoDiscardable` and the opener guard: on a
   launched, cloned or `browser connect` browser a link that opens a Tab
   may select it in that window. The Tab target's Window State says so.

## Consequences

- An agent's loop in the user's browser is `tab new <window> <url>`, then
  `<window>:<tab>` on every command; `tab switch` is for showing the user
  something, not for working.
- A held Tab believes it is visible and focused: media may autoplay and a
  chat may send read receipts while an agent works there. Holds end five
  minutes after the last command.
- Several held Tabs render at once: memory and CPU grow with the number of
  agents.
- `Page.captureScreenshot` of a hidden Tab is bounded (3 s) and falls back
  to the tree with a note; `@x,y` points do not apply to Tab targets.
- Keys that open a native popup on a focused `<select>` (Alt+Down, Space)
  are not caught; the skill says to use set-value.
- A Window-level Page Action on the active Tab and a Tab-target Action on
  the same Tab take different lanes and may run together.
