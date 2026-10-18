# 13 — Codex-style overlay

**What to build:** While an Action or Batch is running, a Codex-style on-screen indicator shows that winuse is in control, with an Esc hint. It is click-through, never takes focus, and is excluded from winuse's own screenshots. See spec: Control.

**Blocked by:** 08 — Screenshots (fallback methods) + coordinate clicks + zoom

**Status:** done

- [x] Overlay appears for the duration of each Action or Batch and disappears afterwards
- [x] Overlay never receives clicks or focus and does not affect input delivery
- [x] Overlay does not appear in screenshots taken by winuse
- [x] End-to-end: the overlay is visible during a fixture Action and absent from the captured screenshot (verified by user run)

## Comments

- 2026-10-18 — live e2e run (Windows 11, 100% scaling, primary monitor, after the live-fixes merge into winuse-v1): TestOverlayDuringAction passed: overlay shown over the fixture with the click-through/topmost/no-activate styles, the fixture stays foreground, the desktop hit-tests the fixture under the pill, a concurrent screenshot queues behind the Action and has no overlay pixels, and the overlay is gone afterwards. TestOverlayOnlyDuringActions passed.
