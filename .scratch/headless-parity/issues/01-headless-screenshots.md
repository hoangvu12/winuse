# 01: Headless screenshots and zoom

**What to build:** `get-window-state --screenshot` and `zoom` capture without activating the Window.

- The chain is Windows.Graphics.Capture, then PrintWindow. The screen-region method runs only under `--headed`.
- A minimized Window is refused, with a hint to run `restore-window` or pass `--headed`.
- The protocol carries `headed` on get-window-state and zoom, and the CLI forwards the global flag.
- The screenshot line gains the Window's screen rectangle.

See spec: "Screenshots and zoom".

**Blocked by:** none

**Status:** done

- [x] The headless screenshot and zoom never call Activate (offline, fakes)
- [x] The headless chain has no region method; `--headed` keeps activation and the full chain
- [x] A minimized Window is refused, nothing is captured, and the hint names restore-window
- [x] The screenshot line reads `Screenshot: <path> (WxH px, scale S, window at X,Y WxH)`
- [x] The CLI forwards `--headed` to get-window-state and zoom

## Comments

### 2026-11-02: landed

- `internal/helper/screenshot.go`: `captureObserved` is shared by get-window-state --screenshot and zoom. Default: refuse a minimized Window (`Services.Minimized`), then `Services.CaptureHeadless`, no Activate. With `headed`: Activate, then the full `Services.Capture` chain, as before. The refusal reads "window <id> is minimized; there is nothing on screen to capture", with a hint naming `winuse restore-window <id>` and `--headed`.
- The Screenshot line is now `Screenshot: <path> (WxH px, scale S, window at X,Y WxH)`. The rectangle is the captured frame's screen rect in physical px (`frame.Rect`), printed for headless and headed alike.
- `internal/helper/overlay.go`: `headlessCaptureMethods` (first method, then PrintWindow) next to `captureMethodsHiding` (first, region, PrintWindow), both through one `hideDuringCapture` wrapper. `internal/helper/state.go`: `defaultServices` builds both chains over one shared wgc method; `Minimized` defaults to the new `win.IsMinimized` (IsIconic, `internal/win/activate.go`).
- The screenshot branch and zoom stay serialized on the action gate.
- Protocol: `headed` on `protocol.ZoomArgs` and on get-window-state's args (`windowRef` in the helper, `windowArgs` in the CLI). `internal/cli`: `windowStateRequest` and `zoomRequest` build the requests with the global flag, and `Main` passes `headed` to both commands. The usage text says screenshots and zoom are headless.
- Tests: `internal/helper/screenshot_test.go` (headless never activates, headed activates and runs the full chain, minimized refusal captures nothing, line format with a negative origin, the headless chain has no region method). `fakeCapture` fakes both chains and the probe in the pointer and @x,y spies. `internal/cli/screenshot_test.go` checks that the flag is forwarded. The e2e regex in `test/e2e/screenshot_test.go` accepts the new line. SKILL.md has no example line, so it is unchanged.
