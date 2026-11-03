# ADR 0013: Headless by readback, Posted by opt-in

## Status

Accepted by the user on 2026-11-03 (headless-max spec,
`.scratch/headless-max/spec.md`, ticket 17). Amends ADR 0010, 0011 and
0012. It reverses ADR 0010 Decision 1, the headless spec's minimized
refusal, ADR 0011 Decision 5's chord refusal and ADR 0012 Decision 6's
"Chromium browsers only"; "Reversals" below names the ticket behind
each. Most routes it rules on are planned: the table in "Routes" says
which have landed.

## Context

ADR 0010 and 0011 made Element Actions, screenshots, Window Actions,
Element scroll, dialog Return and Escape, classic-edit typing and Posted
Keys headless. ADR 0012 made browser Pages headless. 22 paths still take
the user's foreground, real cursor or keyboard, or let an App take the
foreground (`.scratch/headless-gaps/headless-100.md` section 1).

The user asked for winuse to be "100% headless". The research shows that
this cannot be reached on the user's own desktop (section 3 of the
research): hover, OLE drag-and-drop, raw-input Apps and administrator
Windows have no route that leaves the real input and the foreground
alone, and the isolation options (MXC, Agent Workspace, hidden desktops,
RDP loopback) do not reach the user's own running Apps. Probe rounds 3
to 5 (`probes-round3.md`, `probes-round4.md`, `probes-round5.md` in the
same folder) measured which of the remaining routes work, per framework.

The routes split by one property: whether winuse can read the effect
back. A pattern call whose result winuse reads is as safe as the
Headless Actions of ADR 0010. A posted message that the App may ignore
is not, which is why ADR 0011 made Posted Keys opt-in.

## Decision

1. **A route with a readback may be Headless by default.** The accelerator
   lookup, a menu command with a grey-state check, a context menu with a
   menu-opened check, select-text, a point resolved to an Element in the
   target Window, and the caret splice into a Text-pattern field.
2. **A best-effort message route without a readback is opt-in per
   command, through `--post`.** Posted Keys, Posted Clicks and chords
   through attached key state. It is never an automatic fallback.
3. **Where no route exists, fail before sending.** The failure says
   "nothing was sent" and gives the exact `--headed` retry. winuse never
   falls back to Headed on its own. That covers routing by the live
   Window too (ADR 0011 Decision 4): a `type-text` or `press-key` with
   no background route, and any other Action that would go Headed, is
   refused unless the agent passed `--headed` (ticket 21, decided by the
   user on 2026-11-03). `activate-window` is itself the request for the
   foreground.
4. **No Headless or Posted route attaches to the foreground thread, sends
   input or moves the cursor.** No SendInput, no SetCursorPos, and no
   AttachThreadInput to the thread that owns the foreground: a route
   that needs attached key state refuses with "nothing was sent" when
   the target thread is the foreground thread.
5. **UIA SetFocus is never used in a Headless or Posted path** (the
   user's addition). It takes the foreground for any App, even when the
   caller has no foreground rights (round 3, measured on classic,
   Chrome, Edge and Electron Windows).
6. **Launch and restore are non-activating calls**, and winuse reports
   when an App takes the foreground itself. It does not take the
   foreground back.
7. **Exception to rule 2, chosen by the user: the Chromium and Electron
   message route is automatic** (headless-max ticket 20). Typing, keys
   and clicks into a Chromium or Electron Window without a Browser
   Connection go through sent window messages with no `--post`, even
   where nothing reads the result back. winuse reads the text back
   through the UIA Value pattern where the field has one; where it has
   none, the output says the text was not verified. The measured events
   are trusted and identical in Chrome, Edge and Electron, and the only
   other route for these Windows is Headed.

## Routes

| Route | Rule | Ticket | State |
|---|---|---|---|
| Launch without the foreground, and launch options | 6 | 01 | done |
| restore-window reports self-activation | 6 | 02 | done |
| Detect and report self-activation around restore | 6 | 03 | done |
| Headless tab switch on a launched browser | 1 | 05 | done |
| Accelerator lookup for press-key | 1 | 06 | planned |
| Chords through attached key state, `--post` | 2 | 07 | planned |
| invoke-menu on classic menu bars | 1 | 08 | done |
| ContextMenu Secondary Action | 1 | 09 | planned |
| select-text | 1 | 10 | done |
| Point forms resolve to an Element in the target Window | 1 | 11 | planned |
| Posted Clicks and `scroll @x,y --post` | 2 | 12 | planned |
| Caret-aware typing into Text-pattern fields | 1 | 13 | done (`--into` only; see the ticket) |
| Headless Actions on minimized Windows | 1 | 14 | done |
| Electron and WebView2 Browser Connections at launch; attach to a running Electron | 1 | 15 | done |
| Typing, keys and clicks into Chromium and Electron without CDP | 7 | 20 | planned |
| No Headed Action without `--headed` | 3 | 21 | done |

## Reversals

- **ADR 0010 Decision 1** ("`@x,y` forms are not Headless, because a
  point names the topmost Window there"), by ticket 11. `click @x,y`
  (left, single), `set-value @x,y`, `perform-secondary-action @x,y` and
  `scroll @x,y` resolve the point against the target Window's own UIA
  subtree, never the desktop, so the objection no longer holds. They are
  Headless when the hit-test finds an Element with a route, and the
  output names the Element acted on. Otherwise they refuse with the
  `--headed` retry. Double, right and middle clicks at a point stay
  Headed, or go through Posted Clicks with `--post`. This also reverses
  the headless spec's out-of-scope "Headless `@x,y` actions".
- **The minimized refusal** (`.scratch/headless/spec.md:41`: a minimized
  Window refuses Headless Actions), by ticket 14. UIA pattern Actions
  work on minimized classic Win32, WPF and WinUI 3 Desktop Windows: the
  tree keeps its size, the Window stays iconic and the foreground is
  unchanged (rounds 3 and 4). The refusal stays for packaged Store apps
  (`ApplicationFrameWindow`): minimized, their UIA tree is empty, and
  restoring one takes the foreground (round 5). It stays for the
  Chromium message routes, whose activation messages restore a
  minimized Chromium and take the foreground (round 3), and for
  screenshots.
- **ADR 0011 Decision 5** (Posted Keys are the only posted kind, and
  chords are refused), by tickets 07 and 12. `press-key --post` accepts
  Ctrl, Shift and Alt chords on classic Win32, WinForms and WPF targets
  through attached key state, with the PR #400 activation pair for WPF
  and for Alt chords (round 4). The Windows key stays refused. Posted
  Clicks add double, right and middle clicks on an Element, clicks at a
  point and `scroll @x,y --post`, for an allow-list of classic controls.
- **ADR 0011 Decision 4's "anywhere else they stay Headed"**, by ticket
  21. `press-key` and `type-text` still route by the live Window before
  anything is sent, but where no Headless route exists they refuse with
  the `--headed` retry instead of taking the foreground. The same holds
  for double, right and middle clicks, `@x,y` forms, drag, hover and
  Raise. `tab switch` on a launched, cloned or toggled browser, which
  browser merge ticket 07 made Headed without asking, got its Headless
  tab-strip route in ticket 05; its keys run only with `--headed`. A Batch
  refuses such a step at its turn, so without `--headed` it never shows
  the banner unless it activates a Window.
- **ADR 0012 Decision 6** ("Chromium browsers only", and its consequence
  that Electron and WebView2 stay on UI Automation), by ticket 15.
  `launch-app --cdp` starts an Electron or WebView2 App with a loopback
  debugging port, so its Pages get Browser Connections and Headless Page
  Actions (round 5). Attaching to an Electron App that is already
  running is opt-in: it runs code inside the user's App, so the agent
  asks the user first, and winuse refuses when the App's nodeCliInspect
  fuse is off. WebView2 stays launch-time only. Firefox stays on UI
  Automation.

## What stays impossible

These have no Headless or Posted route on the user's desktop. They
refuse with "nothing was sent" and the `--headed` retry, or, for
administrator Windows, with the advice to start winuse elevated.

- Hover and native drag outside classic Win32, and OLE drag-and-drop
  between Apps. Classic Win32 has partial message routes, not
  ticketed.
- A click or scroll on a surface with no Element and no HWND: canvases,
  custom-drawn views and games.
- Keys into Apps that read raw input or GetAsyncKeyState, and into UWP
  Apps with keystroke semantics.
- IME composition outside CDP.
- activate-window, maximize-window (desktop-parity ticket 11), and Raise
  of a top-level Window.
- A minimized Store app: it has no UI while minimized, and restoring it
  brings it to the front. Launching one takes the foreground too.
- A minimized Chromium or Electron Window through the message routes.
- WPF context menus: neither ShowContextMenu nor a posted WM_CONTEXTMENU
  opens one in a background Window (round 4).
- The tray overflow, whose opening takes the foreground, and the clock
  and notification panel, which does not open from a UIA call (round 5).
- Guaranteeing that an App never takes the foreground itself.
- Administrator Windows, from a helper that is not elevated.
- Attaching CDP to a running WebView2 App, or to a running Electron App
  whose nodeCliInspect fuse is off.

Off the list since the research: keys into Chromium and Electron without
CDP (ticket 20), and attaching to a running Electron App with the fuse on
(ticket 15, opt-in).

## Consequences

- Every route without a readback is named on the command line by
  `--post`, except the Chromium and Electron route, whose output says
  when it did not verify the text.
- `ok` from a Headless route means winuse read the effect back. `ok` from
  a Posted route means only that the messages were delivered, and the
  output says to read the Window State.
- More `press-key` and `type-text` calls stay in the background, so the
  rule "keys outside a classic dialog or edit box take the foreground"
  in the skill narrows ticket by ticket.
- UIA Invoke on a classic `#32768` menu item takes the foreground (round
  4), so the menu routes choose items through posted mnemonics or
  WM_COMMAND, never through UIA Invoke there.
- The page sees a focus and blur pair around each key step of the
  Chromium message route. Clicks skip the activation pair and show no
  such pair (round 4). WebView2, a child window, is not covered by that
  route.
- Pointer routes must be per-monitor DPI aware: a DPI-unaware probe
  landed its input in a user Window (round 3).
- winuse's own Headed set-value and Raise still call SetFocus. They are
  Headed, so rule 5 does not apply to them.
- The impossible list and the routing rules live in the skill, which
  describes only the routes that have landed.
