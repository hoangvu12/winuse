# 03: A skeleton read skips the Chromium settle

Status: done (branch perf-catch-up)

**What to build:** `get-window-state <w> --skeleton` on Discord takes 5.0 s every time (16 of 16 runs; agent-desktop's skeleton: 784 ms). `readTree` treats a Chromium/Electron tree whose Documents hold fewer than `thinDocumentNodes` Elements as thin and settles it for `DefaultChromiumSettle` (`internal/uia/retry.go:145,183`). A skeleton reads three levels, so the Document's content is never in it: the tree always looks thin, never grows, and the whole 5 s budget is spent. A full read in between finds the page built and clears the thin mark, so the next skeleton pays again.

A depth-limited read must not decide thinness from content it chose not to read: skip the settle (and the thin mark) for skeleton and depth-limited region reads, or count the hidden children the skeleton reports under the Document.

**Blocked by:** none

- [x] Skeleton reads never settle or touch the thin mark
- [x] Offline test: a skeleton of a built Chromium tree returns after one read
- [x] Benchmark: Discord skeleton

## Comments

### 2026-11-07: landed (branch `perf-catch-up`)

`readTree` returns a skeleton read after its first read (the sparse-tree retry still applies to a nearly empty one) and never touches the thin mark (`internal/uia/retry.go`). Test: `TestReadTreeSkeletonIsNotSettled`. Live, helper trace: Discord skeleton 24-30 ms (was 4988-5136 ms whole-CLI).
