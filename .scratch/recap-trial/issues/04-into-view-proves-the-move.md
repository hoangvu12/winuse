# 04: `scroll … into-view` should prove the Element is in view

Status: resolved
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

## Answer

Merged as def20d6 (commit 44a6d45). `into-view` reads back by bounds: the Element must lie inside the visible part of its nearest scroller (a Scroll-pattern ancestor, or the list ticket 02 steps), clipped to the Window; IsOffscreen is an extra signal and the fallback when bounds are empty. Still outside is a delivered failure, never `ok`; already inside prints `element N was already in view; nothing scrolled`. Live on Recap: a row at y 1253..1329 below the pane (205..881) moved to 781..857. The "still outside" path is tested offline only.
