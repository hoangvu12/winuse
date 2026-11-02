# 04: Occlusion flag for launched browsers

**What to build:** browsers that winuse launches get `--disable-features=CalculateNativeWinOcclusion`, so a covered window keeps rendering and builds its UIA tree.

Chromium marks covered windows occluded through its native occlusion tracker (`ui/aura/native_window_occlusion_tracker_win.cc`; feature `CalculateNativeWinOcclusion` in `ui/base/ui_base_features.cc`). The Laptop probe measured that a cold, fully covered Chrome kept a 36-node UIA tree for 14 s, and settled in about 0.1 s with this flag. With a Browser Connection the Page does not need UIA, but the frame (tab strip, address bar, dialogs) still does, and the UIA fallback needs the whole tree.

- Merge with any `--disable-features` the launch already passes.
- Consider `--disable-backgrounding-occluded-windows` too, for `Page.captureScreenshot` on a covered Page (CDP docs). Measure first.
- The user's own browser cannot be changed; say nothing about it.

**Blocked by:** none

**Status:** ready-for-agent

- [ ] Flag added in the browser launch arguments
- [ ] Offline test of the argument list
- [ ] Opt-in e2e: launch covered, read the Window State, the frame and the Page are present
