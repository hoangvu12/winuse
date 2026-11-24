# 01: `name:` matches a control and its own label

Status: ready-for-agent
Type: task
From: a real session on 2026-11-24, reading the user's Recap App (`recap.exe`)

## Problem

In Recap, `winuse click 529714 "name:Open Meeting"` refused:

```
name "Open Meeting" matches 2 elements in the cached Window State for window 529714; nothing was sent
hint: candidates:
56 button Open Meeting
57 text Open Meeting
```

`57` is the button's own child, its label. The same happened with
`perform-secondary-action 595280 name:Transcript Select` (`8 tab Transcript`
and `9 text Transcript`, its child). Each cost the agent a round trip to
retry by index, although the two matches are one control.

## Ask

- When a `name:` reference matches exactly two Elements, one an actionable
  Element and the other a non-interactive `text` Element that is its own
  descendant with the same name, resolve to the actionable one. Say so in
  a `Note:` only if it helps; the usual output is fine.
- Keep "exact, never a guess": any other ambiguity (two buttons, a text
  that is not the control's own descendant, a descendant that is itself
  interactive) still fails and lists the candidates.
- Apply the same rule wherever `name:` resolves (Actions, `wait`, Batch
  steps, `find`'s candidate list if relevant), and on both the desktop tree
  and Pages.
- Tests: the Recap shapes above (button + child text, tab + child text),
  a text that is a sibling (still ambiguous), two buttons (still
  ambiguous), a child that is a link (still ambiguous).
- Update the skill's "Name an Element" wording only if it no longer holds.
