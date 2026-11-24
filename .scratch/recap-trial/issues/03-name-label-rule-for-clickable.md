# 03: The label rule should cover Page Elements marked `clickable`

Status: resolved
Type: task
From: ticket 01's Answer

## Problem

Ticket 01 resolves a `name:` match on a control and its own non-interactive
label, using `ActedOn()` to decide what is a control. A Page Element that is
only marked `clickable` (a div with a click handler) is not `ActedOn()`, so
`name:` on a clickable with its own text inside still fails as 2 matches.

## Ask

- In the `name:` resolution only (`wstate.ResolveElementRef`), count an
  Element marked `clickable` as the control. Do not change `ActedOn()` or
  what `--interactive` shows.
- Everything else stays ambiguous, as in ticket 01 (two clickables, a
  sibling text, an interactive descendant, three matches).
- Tests: generic Page trees, a clickable div with a text child resolves;
  the existing still-ambiguous table still holds.

## Answer

Merged as def20d6 (commit 2de5bc8). In `name:` resolution only, a Page Element marked `clickable` counts as the control over its own non-interactive label; `ActedOn()` and `--interactive` are unchanged. Two clickables, nested clickables, a link inside, a sibling text still fail.
