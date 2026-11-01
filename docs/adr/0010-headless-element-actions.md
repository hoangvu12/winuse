# ADR 0010: Element Actions are Headless by default; physical Actions stay Headed

## Status

Accepted (headless spec, `.scratch/headless/spec.md`). Reverses the v1
spec's "Every Action activates its target Window first" and its
out-of-scope "Background operation".

## Context

v1 copied Codex's Windows model: every Action activates the Window,
injects real input and raises the "in control" banner, and the physical
Esc key stops it. That makes the user's PC unusable while the agent
works.

vercel-labs/agent-desktop measured on Windows that UI Automation pattern
calls (Invoke, Toggle, SelectionItem, ExpandCollapse, Value, RangeValue,
LegacyIAccessible.DoDefaultAction) take effect without the foreground,
that UIA `SetFocus` does move the desktop foreground (its finding A3-4),
and that pattern writes into a higher-integrity Window fail (A24-16).
It also ships a presentation-only cursor overlay so that headless work
stays visible.

## Decision

1. Element Actions — `click` by Element reference (left, single),
   `set-value` and `perform-secondary-action` (except Raise) — are
   Headless: pattern calls only, no activation, no pointer, no keys, no
   banner. `@x,y` forms are not Headless, because a point names the
   topmost Window there.
2. Physical Actions stay Headed exactly as in v1, with no flag needed.
3. A Headless click with no pattern route fails with nothing sent and a
   hint to pass `--headed`. winuse never switches mode on its own.
4. `--headed` restores v1 behaviour for any Action or Batch.
5. Headless Actions show an Agent Cursor instead of the banner. A
   physical Esc during a Headless scope still stops winuse but is not
   swallowed.

## Consequences

- The user keeps their pointer, keyboard and foreground for most Element
  Actions.
- Some apps react differently to a pattern call than to a real click
  (custom-drawn controls, some web widgets). The agent sees a failure or
  a verification mismatch and retries with `--headed`.
- `click` gains a pattern route the Window State does not show directly;
  the Secondary Actions column still lists Toggle/Select/Expand/Collapse.
- The Esc contract splits: swallowed in Headed scopes (Codex parity),
  passed through in Headless ones.
- The UIA client must set `IUIAutomation2::AutoSetFocus` to FALSE. Its
  default (TRUE) makes UIA focus the Element before every control pattern
  call, which brings the Window to the foreground; the opt-in e2e run
  measured this on the fixture and on Chrome. A Headed set-value focuses
  explicitly instead.
- A dialog opened by a Headless click is never activated, so
  `GetLastActivePopup` does not find it; modal-dialog detection falls
  back to the disabled Window's enabled owned popup (`GW_ENABLEDPOPUP`).
