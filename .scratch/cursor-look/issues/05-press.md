# 05: The click's press replaces the disc ripple

Status: done

Spec decision 5 (ADR 0018 Decision 6).

**Done.**

- `pressScale`: cua's action_click keyframes, 1 → 0.93 (35 %) → 1.03
  (60 %) → 1 over 667 ms, each segment eased with cubic-bezier(0.2, 0.75,
  0.25, 1); the model's `press`, the frame's `Press`, the view's pressed
  sprites (scaled about the tip).
- The ripple, its constants and tests are gone; its window is the trail's
  now. The element outline (with its pop) and the insertion caret are
  unchanged. The helper's delivery-timed `Click` (browser-ux 03, ADR 0016)
  now starts the press.
- Tests: `TestPressSqueezesAndSettles`, `TestClickTimelinePressAndOutline`,
  `TestClickWithoutTravelAppearsAtTarget`.

## Comments
