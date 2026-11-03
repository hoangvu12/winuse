# 08: Display list and full-screen capture

**What to build:** `list-displays` (index, bounds in physical pixels, effective scale, primary) and `screenshot --display N`. agent-desktop: `list-displays` and display capture by index (`ad:crates/windows/src/system/display.rs:5-13`).

- EnumDisplayMonitors plus GetDpiForMonitor (winuse is per-monitor-aware v2, `internal/win/dpi.go:9-31`).
- Capture with WGC `CreateForMonitor`, falling back to BitBlt of the monitor rectangle. No activation: Headless.
- A display screenshot is observation only. It gets no `@x,y` binding, because `@x,y` binds to a Window's latest screenshot (CONTEXT.md "Window State"). The output says so.
- The Agent Cursor and overlay stay excluded from the capture (ADR 0006).
- Desktop commands (term from ticket 01).

**Blocked by:** 01 (the "Desktop command" term)

**Status:** done

- [x] Monitor enumeration and capture
- [x] CLI, protocol, helper
- [x] Offline tests of the listing format and DPI maths
- [x] Opt-in e2e: list and capture the primary display
- [x] SKILL.md

## Comments

### 2026-11-03: triage decision (user)

Do it like agent-desktop: `list-displays` and a capture by display index, Headless. The display capture has no `@x,y` binding and says so.

### 2026-11-03: landed (laptop, branch `laptop/dp-08-displays`)

What landed, like agent-desktop (`crates/windows/src/system/display.rs`, `capture/capture_display.rs` at 9eb9b29):

- `list-displays`: one line per display, `<n>\t<x>,<y> <w>x<h>\tscale <s>[\tprimary]`. EnumDisplayMonitors, GetMonitorInfo (rectangle in physical pixels, the primary flag) and GetDpiForMonitor (effective DPI, scale = DPI / 96). The primary display is 0, as agent-desktop's index 0; the others follow top to bottom, then left to right (agent-desktop sorts them by HMONITOR value, which says nothing about where a display is). A monitor whose rectangle or DPI cannot be read fails the listing instead of being dropped, as in agent-desktop. Only reads: it does not wait for the action gate.
- `screenshot --display <n>` (agent-desktop's `screenshot --screen N`): Windows.Graphics.Capture through `IGraphicsCaptureItemInterop::CreateForMonitor` (slot 4), falling back to a BitBlt of the display's rectangle (`win.CaptureScreenRegion`) when it fails or comes back blank. agent-desktop only BitBlts. No activation; the indicator and the Agent Cursor are hidden around the capture (ADR 0006). Serialized on the action gate like a Window screenshot. The image is scaled and saved like a Window's (long edge at most 1568 px, JPEG q80, `%TEMP%\winuse\winuse-display-<n>-<ts>.jpg`), printed as `Screenshot: <path> (WxH px, scale S, display N at X,Y WxH)` and a `Note:` line saying it has no `@x,y` points or zoom. No Mapping is recorded. A locked desktop and an index past the last display are refused before anything is captured.
- The WGC window capture was split so the session, frame poll, pixel readback and blank retry (`thread.captureItem`) serve both Windows and displays; `Method.run` holds the shared watchdog. The log lines stay `wgc: captured window N` / `wgc: window N fell back`, plus `wgc: captured display at X,Y WxH`.
- CONTEXT.md's **Desktop command** now says which ones wait for the Actions: those that touch the clipboard or capture the screen, not a plain listing.

Files: `internal/win/display.go`, `internal/wgc/capture.go` and `com.go` (CreateForMonitor, the shared session), `internal/helper/display.go`, `internal/helper/state.go` (Services.Displays, CaptureDisplay, registry, observation list), `internal/protocol/protocol.go` (`ScreenshotArgs`), `internal/cli/display.go`, `internal/cli/cli.go` (usage), CONTEXT.md, SKILL.md (Displays section, reference), README, `docs/codex-parity.md` (extensions), `test/e2e/README.md`.

Tests: offline `internal/win/display_test.go` (primary first, then by position; the listing format, negative origins, scale = DPI / 96 for 96 to 288), `internal/helper/display_test.go` (listing, the Screenshot line and note, the 1568 cap, no Mapping recorded, refusals, WGC → region fallback on error and on a blank frame, both failing, capture inside the overlay's and the cursor's HideWhile), `internal/cli/display_test.go` (parsing, usage exits). `go build ./...`, `go vet ./...`, `go test ./...` and `go vet -tags e2e ./test/e2e` pass.

Live (laptop, Windows 11 26200, one display 2560x1440 at 150%, `WINUSE_E2E=1`): `TestListDisplaysAndCapturePrimary` passed (listing matches SM_CMONITORS and SM_CXSCREEN/SM_CYSCREEN, captured through Windows.Graphics.Capture, 1568x882 JPEG, foreground unchanged, index 1 refused) and `TestDisplayScreenshotFallsBackToTheScreenRegion` passed (WINUSE_WGC=off, screen region). Because the WGC window capture was refactored, the existing `TestWGC*` and `TestCoordinateClickExactViaWGC` were run too and passed. Not run: a second monitor (this laptop has one), so the ordering of several displays and a negative-origin capture are offline only.

### 2026-11-03: `--screen` alias landed (laptop, branch `laptop/dp-08b-screen-alias`)

`winuse screenshot --screen <n>` (and `--screen=<n>`) is accepted as agent-desktop's spelling of `--display <n>`: same meaning, same validation, error messages name the spelling given. Giving both is giving the option twice, which the CLI already refuses (`--app is given twice`, `--format is given twice`): usage error, exit 2, "--display and --screen are the same option; give one". CLI parsing only; the protocol (`ScreenshotArgs.Display`) and the helper are unchanged.

Files: `internal/cli/display.go` (`parseScreenshotArgs`), `internal/cli/cli.go` (usage), `skills/winuse/SKILL.md` (Displays section, reference), `docs/codex-parity.md`.

Tests: `internal/cli/display_test.go`: `TestParseScreenshotScreenAlias` (`--screen 0`, `--screen=2`, `--screen 10` parse as `--display`; missing, empty, negative and non-numeric values, `--screen` twice, `--display` with `--screen` in either order, `--screens`), and `TestDisplayUsageErrorsExitTwo` now covers a bad `--screen` value, both flags (exit 2) and the alias in the usage text. `go build ./...`, `go vet ./...`, `go test ./...` and `go vet -tags e2e ./test/e2e` pass. No live run: the e2e display test was not touched and the helper path is unchanged.
