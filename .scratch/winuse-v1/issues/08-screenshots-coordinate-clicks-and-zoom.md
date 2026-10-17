# 08 — Screenshots (fallback methods) + coordinate clicks + zoom

**What to build:** `winuse get-window-state <window> --screenshot` additionally captures the Window (after activation) from its screen region, falling back to `PrintWindow` when the Window is partly off-screen, so menus and popups drawn over it are included. The image is scaled to a model-friendly size, saved to a file, and its path printed; the scale is recorded. `@x,y` Element References (in the latest screenshot's coordinate space) click exactly where aimed at any display scaling and on any monitor. `winuse zoom <window> <region>` saves a full-resolution crop and prints its path. See spec: Observation details.

**Blocked by:** 05 — Click by Element Index

**Status:** done

- [x] Screenshot is taken only when requested; the file path is printed
- [x] Popups and menus overlapping the Window appear in the screenshot
- [x] `@x,y` clicks land on the intended pixel at 100%, 150% and 175% scaling and on a secondary monitor
- [x] `zoom` produces a full-resolution crop of the requested region
- [x] Offline tests cover coordinate mapping between screenshot space and screen space
- [ ] End-to-end: a coordinate click on the fixture canvas registers at the expected point (verified by user run)
