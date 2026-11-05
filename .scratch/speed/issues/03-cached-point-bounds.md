# 03: Point Actions match the read's cached bounds

Status: ready-for-human (done on branch `speed-waits`; the live check is to run)

**From:** research recommendation 10(c) (`notes-local-perf.md` §3 and §9 #6).

**What to build:** `@x,y` Actions (`uia/point.go`, `ElementAtPoint`) read
`CurrentBoundingRectangle` for every Element of the stored Window State,
one UIA round trip each. That is about 300 round trips on a 300-Element
Window, ≈0.3–1 s per point Action.

- [x] The tree read caches BoundingRectangle (property 30001 in
  `treeProperties`). `get_CachedBoundingRectangle` is slot 75.
- [x] `pointBounds`:
  - match against the cached bounds;
  - then read live only what decides the hit: the root, every Element the
    cached bounds put under the point, and the chain above the smallest of
    them;
  - if any of those moved or cannot be read, or an Element has no cached
    bounds, read every Element live as before.
- [x] Offline tests (`internal/uia/pointbounds_test.go`):
  - 4 live reads instead of 302 on a 302-Element Window;
  - the same hit as the live pass at every point of an unchanged Window;
  - a moved Window, a scrolled-away button or a resized pane falls back
    to the live pass and gives the live hit;
  - no cached bounds reads everything live;
  - an Element with no live Element stays unreadable;
  - the request deadline stops it.
- [ ] Live check: `click <id> @x,y` on a 300+ Element Window, before and
  after (research experiment 9).

**Residual:** suppose an Element of the stored state moved onto the point
since the read, while none of the Elements the read put there moved. The
live pass would hit the moved Element; the cached one does not. One case
is a list item scrolled under a point that was on the list's empty space.
The point was aimed at the screenshot of that read, and the Element
Indexes are already stale after such a change.

**Not done:** IsOffscreen and IsInvokePatternAvailable are not cached. The
point pass uses neither, and the click route's live pattern probes stay
live.

## Comments
