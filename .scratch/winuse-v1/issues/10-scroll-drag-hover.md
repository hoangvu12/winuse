# 10 — Scroll, drag, hover

**What to build:** Pointer Actions in screenshot coordinates: `winuse scroll <window> <point> <dx> <dy>` scrolls at a point; `winuse drag <window> <from> <to>` drags between points; `winuse hover <window> <point>` moves the pointer to reveal hover-only UI and tooltips. See spec: Command surface (Actions).

**Blocked by:** 08 — Screenshots (fallback methods) + coordinate clicks + zoom

**Status:** done

- [x] Scroll works horizontally and vertically at a point
- [x] Drag moves from start to end with intermediate moves so apps register it
- [x] Hover leaves the pointer at the point so hover-only UI appears
- [x] Offline tests cover argument parsing and coordinate mapping
- [x] End-to-end: scroll the fixture list, drag on the canvas, reveal the hover-only toolbar (verified by user run)

## Comments

- 2026-10-18 — live e2e run (Windows 11, 100% scaling, primary monitor, after the live-fixes merge into winuse-v1): TestScrollDownListAtPoint, TestDragOnCanvas and TestHoverRevealsHoverOnlyToolbar passed, with the rest of pointer_test.go.
