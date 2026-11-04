# 08: The screenshot path

Status: ready-for-human
Type: task
Branch: speed-output (worktree `winuse-wt/speed-output`)
From: `.scratch/research-speed/report.md`, recommendation 8 (`notes-local-perf.md` §4)

## Ask

- (a) The faster scaler (research: 130–195 ms → 26–33 ms).
- (b) Capture and tree read in parallel (`screenshot.go:61`).
- (c) Size frames to Anthropic's 28 px tile budget, so the API doesn't resize them again, and keep `@x,y` mapping exact.

## (a) Fixed-point, separable, parallel box filter

`shot.ScaleImage` keeps the same box filter: each destination pixel's footprint, weighted by overlap. It now runs as a horizontal then a vertical pass, with 16.16 fixed-point taps precomputed per column and row, and each pass split across `GOMAXPROCS`. Every channel is within 1 of the exact float64 filter, which is kept in the tests as `scaleReference` (`TestScaleImageWithinOneOfTheExactFilter`, sub-images too). Scale 1 is still an exact copy, and the dimensions still round like `scaledSize`.

`go test ./internal/shot -run '^$' -bench Scale -benchtime=20x` on an i9-12900HK, with a synthetic screenshot-like frame scaled to the new target:

| Frame | float64 (before) | fixed, 1 CPU | fixed, 4 CPUs | fixed, 20 threads |
|---|---|---|---|---|
| 1920×1080 | 114 ms | 25 ms | 9.1 ms | **6.1 ms** |
| 2560×1440 | 174 ms | 31 ms | 10.3 ms | **7.8 ms** |

Scale plus JPEG q80 (a scratch benchmark, not committed): 1080p went from 173–179 ms (float filter to 1568×882) to 49–65 ms (fixed filter to 1456×819). 1440p went from 285–289 ms to 115–142 ms. JPEG encoding is now the larger share. It is still Go's `image/jpeg`; a faster encoder is the next lever, not done here.

## (b) Capture and tree read run together

`windowStateScreenshot` now does the following:

1. Refuses a locked desktop.
2. Gets the Window ready (`prepareObserved`: activate under `--headed`, refuse a minimized Window headless).
3. Starts the tree read in a goroutine, while it captures, scales and saves the JPEG on its own goroutine.

The Mapping is recorded only when both succeed, as before. A failed capture cancels the read's context and waits for it, then returns the capture error. Both still run inside the serialized (desktop-lane) section. The section now lasts for the slower of the two instead of their sum: the research estimated 50–300 ms saved [I], more on a Chromium Window whose thin-tree settle waits seconds. This isn't measured live (no desktop). `TestScreenshotReadsTheTreeWhileCapturing` proves the overlap: its capture waits for the read to start and its read waits for the capture to end, so the old sequential code fails it. It also checks that a failed capture cancels the read.

## (c) Sized to the image budget (ADR 0019)

Anthropic's docs, read 2026-11-05 (Vision, "Resolution and token cost"; "How Claude resizes and pads images"):

- An image costs ⌈w/28⌉×⌈h/28⌉ visual tokens.
- The standard tier (models before Claude 4.7) allows 1,568 px a side and 1,568 tokens. The high-resolution tier (Claude 4.7 and later, every Claude 5) allows 2,576 px and 4,784 tokens.
- A bigger image is resized to the largest aspect-preserving size that fits, and the model's coordinates are then in that size.
- The computer use and browser use toolsets reject an oversized `tool_result` image instead.

`shot.FitScale` now returns the largest scale at which the saved image fits the **standard** tier, so every model takes it as sent:

| Window | Before | Tokens | After | Tokens |
|---|---|---|---|---|
| 1920×1080 | 1568×882 | 1,792 (standard tier resized it to 1456×819: ~7% drift) | **1456×819** | 1,560 |
| 2560×1440, 3840×2160 | 1568×882 | 1,792 | **1456×819** | 1,560 |
| 1500×1000 | 1500×1000 (unscaled) | 1,944 (resized) | 1344×896 | 1,536 |
| 1366×768 | 1366×768 | 1,372 | unchanged | 1,372 |

- The scale is still one number, taken from a whole long edge, with both sides rounded by `scaledSize`. The Mapping and `@x,y` are as exact as before.
- `TestFitScaleNeverResizedByTheAPI` checks every Window size from 200 px to 4K in 7 px steps. Each saved size fits the budget and matches Anthropic's published resize function, ported into the test, within a pixel where an exact half rounds.
- Display screenshots and Tab target screenshots use `FitScale` and get the same sizing. Zoom crops stay at full resolution; their pixels never map back to `@x,y`.
- Tradeoff: a high-resolution-tier model, which every current Claude 5 is, took 1568×882 as sent. It now sees 7% fewer pixels per side, for 13% fewer image tokens.
- **Proposed opt-in, not built:** a high-resolution sizing (2,576 px / 4,784 tokens; a 1080p Window unscaled at 2,691 tokens) for agents that know their model and Harness pass images through unchanged.

## Tests changed

The screenshot, display and pointer tests that pinned 1568×882 or a 0.5 scale now pin the budget's sizes. The 0.5-scale tests use a 2184×2184 frame, because 1092×1092 is the largest square within the budget.

## Comments
