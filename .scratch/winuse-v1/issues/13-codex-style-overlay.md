# 13 — Codex-style overlay

**What to build:** While an Action or Batch is running, a Codex-style on-screen indicator shows that winuse is in control, with an Esc hint. It is click-through, never takes focus, and is excluded from winuse's own screenshots. See spec: Control.

**Blocked by:** 08 — Screenshots (fallback methods) + coordinate clicks + zoom

**Status:** done

- [x] Overlay appears for the duration of each Action or Batch and disappears afterwards
- [x] Overlay never receives clicks or focus and does not affect input delivery
- [x] Overlay does not appear in screenshots taken by winuse
- [ ] End-to-end: the overlay is visible during a fixture Action and absent from the captured screenshot (verified by user run)
