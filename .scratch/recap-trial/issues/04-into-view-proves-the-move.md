# 04: `scroll … into-view` should prove the Element is in view

Status: ready-for-agent
Type: task
From: ticket 02's Comments

## Problem

`scroll <w> <ref> into-view` reads back the Element's `IsOffscreen`. Some
providers never report offscreen: in Recap (AccessKit), rows above the
list's top read `IsOffscreen = false`. So `into-view` can print `ok` without
proving the row moved into view.

## Ask

- Read back by geometry as well: after the scroll, the Element's bounds must
  lie inside the visible bounds of its nearest scrolling container (the
  ScrollPattern Element, or the list ticket 02's item stepper found),
  clipped to the Window. Use `IsOffscreen` only as an extra signal.
- If the bounds are still outside, report it as delivered but not in view
  (the existing "did not scroll" style), never `ok`.
- If the Element was already inside the visible bounds before, keep the
  existing "already in view; nothing scrolled" wording.
- General, no App-specific cases. Tests: offline generic trees where
  `IsOffscreen` lies (always false) and bounds decide; a provider whose
  bounds are empty falls back to the current behaviour.
- Live check (Headless only) on the user's Recap meeting window, Transcript
  tab: `into-view` on a row far down, then read the row's bounds against
  the list. Leave Recap on the Summary tab.
