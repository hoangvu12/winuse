# 03: Say when a Page scroll moved nothing

Status: ready-for-agent
Type: task
From: `map.md`; bladebro `ScrollReport` and `scroll_probe_expr` (`bb/src/action/verdict.rs:113-147`), and the verdict at `:421-501`

## Problem

A Headless Page scroll sends a wheel and returns `ok`
(`internal/helper/pageactions.go:682-688`, and the point form at
`:736-766`). At the bottom of a page, or over a nested scroller that is
already at its end, nothing moves, but the agent sees `ok` and keeps
scrolling.

bladebro's comment on this (`verdict.rs:115`): "a wheel burst at the page
bottom reported success and burned agent steps doubting the tool".

## Ask

- **Before and after the wheel,** read the scroll position in one evaluate
  each time:
  - the document scroller, on both axes, with its range;
  - the deepest vertical and horizontal scroller under the wheel point,
    walking up through shadow hosts.

  bladebro's probe is a working reference. Port it to winuse's page JS
  conventions with attribution.
- **When nothing moved,** add one of these notes. It stays `ok`, because the
  wheel was delivered.
  - `Note: nothing scrolled; the Page is at its bottom`
  - `Note: nothing scrolled; the Page is at its top`
  - `Note: nothing scrolled; <scroller> is at its end`
- **When something moved,** print nothing extra, or at most where it now
  sits (`at the bottom`) when it reached the end. Keep it short.
- **Smooth scrolling.** Read "after" once the scroll has settled, not right
  away: wait a couple of animation frames or until the position stops
  changing, with a short bound.
- **`scroll <ref> into-view`:** say when the Element was already in view, if
  that's cheap.
- **Scope.** This covers Page scrolls only. Native scrolls already go
  through UIA ScrollPattern, which reports its position.
- **Tests.** A fake CDP test, and an e2e page with a short body and a nested
  scroller.
