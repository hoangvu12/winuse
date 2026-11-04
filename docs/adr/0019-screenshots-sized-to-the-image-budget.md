# ADR 0019: Screenshots are sized to the model's image budget

## Status

Accepted (speed issue 08, `.scratch/speed/issues/08-screenshot-path.md`,
2026-11-05). It replaces the orchestrator decision of ticket 08 to cap the
long edge at 1568 px (`docs/codex-parity.md`, screenshots) and keeps
everything else about the screenshot: the capture chains (ADR 0011), JPEG
q80, the recorded Mapping and `@x,y` in the saved image's pixels.

## Context

Claude reads an image in 28×28 px patches, one visual token each,
⌈w/28⌉×⌈h/28⌉ in all. Each model has a long-edge limit and a token limit
(platform.claude.com, Vision, "Resolution and token cost", checked
2026-11-05):

| Tier | Models | Long edge | Visual tokens |
|---|---|---|---|
| Standard | before Claude 4.7 (Sonnet 4.6, Opus 4.6, Haiku 4.5, ...) | 1,568 px | 1,568 |
| High resolution | Claude 4.7 and later | 2,576 px | 4,784 |

An image past either limit is resized by the API to the largest
aspect-preserving size that fits ("How Claude resizes and pads images"),
and the model's coordinates are then in the resized image's pixels. A
1920×1080 Window was saved at 1568×882: 56×32 = 1,792 tokens. The standard
tier resized it to 1456×819, so a point the model read off it sat about 7%
short of the pixel the agent's `@x,y` named. Computer use and browser use
toolsets reject an oversized `tool_result` image outright.

## Decision

- `shot.FitScale` returns the largest scale at which the saved image fits
  the standard tier: each side's 28 px patches within 1,568 px and
  ⌈w/28⌉×⌈h/28⌉ ≤ 1,568. Every model takes the image as sent. 16:9 Windows
  save at 1456×819 (1,560 tokens), Anthropic's own target. A Window
  already within the budget stays 1:1.
- The scale is still one number, derived from a whole long edge, and both
  sides round with `scaledSize`. The Mapping and `@x,y` stay exact. A test
  checks every size from 200 px to 4K against Anthropic's published resize
  function (within a pixel where an exact half rounds).
- The same budget applies to display screenshots and Tab target
  screenshots, which use `FitScale` too. Zoom crops stay at full
  resolution: their pixels never map back to `@x,y`.

## Consequences

- A 1080p or larger 16:9 Window costs 1,560 image tokens instead of 1,792
  (−13%), and standard-tier models no longer resize it.
- High-resolution-tier models would have taken the 1568×882 image as sent,
  so they now get 7% fewer pixels per side. Sizing to their 4,784-token
  budget (a 1920×1080 Window at full size, 2,691 tokens) is a possible
  opt-in. It is not the default, because a standard-tier model or a
  harness that downsizes images would resize that image again.
