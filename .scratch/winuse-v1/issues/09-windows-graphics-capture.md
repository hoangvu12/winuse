# 09 — Windows.Graphics.Capture screenshots

**What to build:** Screenshots use Windows.Graphics.Capture for the target Window first, as Codex does, with hand-written WinRT and Direct3D interop from Go (no cgo). When it fails, returns black frames or times out, winuse falls back automatically to ticket 08's methods, so screenshots never depend on it. This is the riskiest piece of the Go choice (ADR 0003); if it proves impractical, report the findings rather than forcing it. See spec: Observation details, Further Notes.

**Blocked by:** 08 — Screenshots (fallback methods) + coordinate clicks + zoom

**Status:** done

- [x] Windows.Graphics.Capture produces the Window screenshot when available
- [x] Failure, black frames or timeout fall back automatically to the screen-region / `PrintWindow` path
- [x] Coordinate mapping from ticket 08 remains exact with this capture path
- [x] No cgo introduced
- [x] End-to-end: a screenshot of the fixture via this path matches expectations; a forced failure triggers the fallback (verified by user run)

## Comments

- 2026-10-18 — live e2e run (Windows 11, 100% scaling, primary monitor, after the live-fixes merge into winuse-v1): TestWGCScreenshotCapturedViaWindowsGraphicsCapture, TestWGCDisabledFallsBackToGDI, TestWGCTimeoutFallsBackToGDI, TestCoordinateClickExactViaWGC and TestWGCZoomStillFullResolution passed (none skipped).
