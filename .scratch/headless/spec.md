Status: ready-for-agent

# Headless Actions and the Agent Cursor

## Problem Statement

winuse v1 works like Codex Computer Use: every Action brings its target Window to the foreground, moves the real pointer or types real keys, and frames the Window with an "in control" banner. While the agent works, the user cannot use their own PC: their pointer jumps, their focus is stolen, and pressing Esc for their own reasons stops the agent.

vercel-labs/agent-desktop showed a better default. Most Element Actions — click a button, set a field, toggle a checkbox, expand a menu — can be performed entirely through UI Automation patterns, without the foreground, the pointer or the keyboard. The Window can stay behind the user's own work. A presentation-only Agent Cursor (a second, fake cursor that glides to the Element and ripples on the click) keeps what the agent does visible without taking anything away from the user.

## Solution

Element Actions become **Headless by default**: `click` on an Element, `set-value` and `perform-secondary-action` (except Raise) run through UI Automation patterns on a Window that stays where it is. They never activate the Window, move the pointer, inject input or show the in-control banner. An Agent Cursor shows each Headless Action: it travels to the Element, the label pill says what is happening, and the click ripples.

Actions that are physical by nature — typing, keys, scrolling, dragging, hovering, `@x,y` coordinates, double/right/middle clicks, activate-window, Raise — stay exactly as in v1 (**Headed**): foreground, real input, banner, Esc stop. A global `--headed` flag forces the v1 behaviour for any Action.

A Headless click whose Element offers no pattern route fails with nothing sent and tells the agent to pass `--headed`. winuse never silently switches from Headless to Headed: taking the user's foreground is a decision the agent makes explicitly.

## User Stories

1. As a user, I want the agent to click buttons and fill fields without taking my mouse, keyboard or foreground, so that I can keep working while it works.
2. As a user, I want to see what the agent is doing through a second cursor that travels to each Element and shows a short label, so that headless work is never invisible.
3. As a user, I want the Agent Cursor to stay out of my way — click-through, never focused, gone after a few idle seconds — so that it never blocks my own work.
4. As a user, I want pressing Esc while a Headless Batch runs to still stop it, without my Esc being swallowed from the app I am typing in.
5. As an agent, I want `click <window> <element>` to work on a Window behind other Windows, so that I don't fight the user for the foreground.
6. As an agent, I want a Headless click on an Element with no pattern route to fail with "nothing was sent" and a hint to pass `--headed`, so that I know retrying is safe and what to do next.
7. As an agent, I want Toggle, Select, Expand, Collapse and set-value to read the state back and fail if it does not match, so that I know whether the Action took effect.
8. As an agent, I want a failure that may have changed the app to say so ("do not repeat it"), so that I never double-submit.
9. As an agent, I want `--headed` to give me exactly the v1 behaviour, so that apps that need real input still work.
10. As an agent, I want typing, keys, scroll, drag, hover and coordinate clicks to keep working without any flag, so that physical Actions cost no extra round trip.
11. As an agent, I want a Headless Action on a minimized Window to fail with a hint to restore it, so that I don't act on invisible UI.
12. As an agent, I want Headless set-value to not steal focus, so that the user's typing is not redirected into the agent's field.
13. As the maintainer, I want the Agent Cursor driven by screen rectangles rather than UI Automation Elements, so that the merged winuse/use-browser tool can show the same cursor for CDP actions.

## Implementation Decisions

- ADR 0010 records the reversal of v1's foreground-only rule.
- **Headless Action set:** `click` with an Element Index or `name:`/`id:` reference, left button, count 1; `set-value` and `perform-secondary-action` with an Element Index or `name:`/`id:` reference, except the Raise Secondary Action. Everything else is Headed. `@x,y` forms are always Headed: a screen point names whatever is topmost there, which for a covered Window is another Window's Element.
- **Headless click route**, first offered pattern wins: Invoke → Toggle → SelectionItem.Select → ExpandCollapse (Expand when collapsed, Collapse when expanded) → LegacyIAccessible.DoDefaultAction when the default action is non-empty (agent-desktop findings A19-6, A15-5). None offered: fail, nothing sent, hint `--headed`.
- **Headless set-value** skips the v1 focus-first step (UIA SetFocus moves the desktop foreground, agent-desktop A3-4). Headed set-value keeps it.
- **Refusals stay:** locked desktop, administrator Window (UIA writes across the integrity boundary fail, agent-desktop A24-16). New: a minimized Window refuses Headless Actions with a hint to `activate-window` it or pass `--headed`. (Narrowed 2026-11-03 by ADR 0013 and landed with headless-max 14: on a minimized classic Win32, WPF or WinUI 3 Desktop Window, click, set-value, the Secondary Actions and select-text run Headless; Element scroll, the keyboard and menu routes, Posted Keys and screenshots stay refused, and so do minimized Store apps and Chromium; `../headless-max/issues/14-minimized-windows.md`.)
- **Verification:** after a Headless Toggle, Select, Expand, Collapse or set-value (Value pattern, non-password), the Element's state is read back live. A mismatch fails with the observed state and "the Action was delivered; do not repeat it, read the Window State". Invoke and Legacy default actions have no readable outcome and report `ok`.
- **Retry safety in failures:** failures raised before any pattern call or input say "nothing was sent", so the agent knows a retry is safe.
- **No banner for Headless Actions.** The Agent Cursor replaces it. Headed Actions keep the banner and the Esc stop unchanged.
- **Esc in Headless scopes:** a physical Esc press still latches the stop (exit 130), but is passed through to the user's app instead of swallowed. A Batch with any Headed step arms the v1 swallowing scope for its whole run.
- **Agent Cursor:** a per-pixel-alpha layered window on the overlay's UI thread (ADR 0006): an arrow cursor plus a label pill, travelling to the target's centre along an eased path (about 300 ms) before the pattern call, then a ripple and a brief outline of the Element's bounds. It starts at the centre of the target Window, fades after 6 s idle, is excluded from winuse's own captures like the banner, and is best effort: a cursor failure never fails an Action. The API takes screen rectangles and label text only.
- **CLI:** `--headed` is a global flag (before or after the command) and a Batch-wide flag; the protocol carries `headed` on click, set-value, perform-secondary-action and batch.

## Testing Decisions

- Offline (default run): the Headless/Headed routing per Action and flag, the click route order and its "nothing sent" failure, the verification mismatch mapping, the passive Esc latch, and the cursor's path/layout maths, all with fakes.
- Opt-in e2e against the fixture app (user-run only): Headless click of a button and checkbox while another Window covers the fixture, the foreground unchanged afterwards; Headless set-value without focus change; a pattern-less Element failing with the `--headed` hint; the Agent Cursor visible in a screen capture taken by the test.

## Out of Scope

- ~~Per-agent cursors and a machine-wide input lease for several agents (merge work).~~ Reversed 2026-11-03 by ADR 0014: a cursor per agent, and lanes instead of one lease (`../multi-agent/spec.md`).
- Headless typing (UIA has no insert-at-caret write; agent-desktop's Windows `type` is Headed too).
- ~~Headless `@x,y` actions.~~ Reversed 2026-11-03 by ADR 0013 for points that resolve to an Element in the target Window (`../headless-max/issues/11-point-forms-to-element.md`).
- Notifications, shell surfaces, clipboard.
