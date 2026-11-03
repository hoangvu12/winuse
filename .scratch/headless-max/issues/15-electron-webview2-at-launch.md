# 15: Electron and WebView2 Browser Connections at launch

**What to build:** `launch-app <app> --cdp` starts an Electron or WebView2 App with a loopback debugging port, so its Pages get the same CDP Window State and Headless Page Actions as Chromium browsers.

Evidence (`../headless-gaps/headless-100.md` G12):

- Electron: pass `--remote-debugging-port=0` and read `DevToolsActivePort` as for Chrome. Electron's fuses gate `--inspect`, `ELECTRON_RUN_AS_NODE` and `NODE_OPTIONS`, but none gates the Chromium `--remote-debugging-port` switch (https://www.electronjs.org/docs/latest/tutorial/fuses). An App can still strip switches itself.
- WebView2: set `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--remote-debugging-port=<p>` in the launched App's environment (CreateCoreWebView2EnvironmentWithOptions docs). Never set the machine-wide registry policy.
- Launch-time only. A running App needs a restart; winuse never restarts an App without the agent asking and never kills one.
- agent-desktop does `launch --cdp` and refuses running Apps.

Rules:

- Loopback only; verify the port owner is the launched process tree (ADR 0012's checks).
- Detect Electron (an `electron.asar`/`resources\app.asar` next to the exe) and WebView2 (`msedgewebview2.exe` children) after launch; map each top-level Window to its Page target by bounds and title, as for Chrome.
- If no port appears within the launch budget: `ok (launched; no debugging port, the App stays on UI Automation)`.
- Shares the launcher from ticket 01.

**Blocked by:** 01

**Status:** done (the live e2e runs are still to do: paused while the user games)

- [x] `--cdp` in the CLI and protocol
- [x] Electron and WebView2 argument injection; port discovery and owner verification
- [x] Window-to-Page mapping for non-browser hosts
- [x] Offline tests: argument and environment building, owner check, no-port fallback
- [x] Opt-in e2e: VS Code or another installed Electron App, and a WebView2 App, covered
- [x] ADR 0012 amendment, SKILL.md

## Comments

### 2026-11-03: round 3 probes (`../headless-gaps/probes-round3.md`)

New scope, at the research chat's request: **attach to a running Electron App**, opt-in and with the user's consent.

- Recipe (measured on the laptop, Electron 45.2.1): `process._debugProcess(pid)`, then `/json/list` on 127.0.0.1:9229, then main-process `Runtime.evaluate` of `webContents.getAllWebContents()[i].debugger.attach('1.3')`, then Input commands, then `detach()`, then `require('inspector').close()`, then confirm the port is gone. All input was trusted, there was no window activation, and Defender reported nothing.
- It needs the nodeCliInspect fuse on. Discord 1.0.9266 has it off; refuse with a clear reason.
- It runs code inside the user's App. Ask first, never by default. Any same-user process can reopen the port.
- This revises "a running App needs a restart" above: a restart is needed only when the fuse is off. WebView2 stays launch-time only.
- Needs: a Node runtime or an equivalent `_debugProcess` signal (it sends SIGUSR1 to the process on Windows). Find out how to send it without bundling Node (ADR 0002 and 0003).

### 2026-11-03: triage decision (user)

Adopt both: `launch-app --cdp` and attaching to a running Electron App. The attach always asks the user first, never runs by default, and refuses when the nodeCliInspect fuse is off. It reverses ADR 0012 Decision 6, recorded in ADR 0013.

### 2026-11-03: launch-time CDP measured (`../headless-gaps/probes-round5.md` section 3)

Both work, with the foreground unchanged at launch.

- **Electron:**
  - `--remote-debugging-port=0` writes `DevToolsActivePort` into the App's userData folder (`%APPDATA%\<app name>` by default), and prints "DevTools listening on ws://127.0.0.1:<port>/..." on stderr.
  - When winuse launches the App, read stderr for the port, or the file when the userData path is known.
  - The port owner is the launched electron.exe.
- **WebView2:**
  - Set `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--remote-debugging-port=<free port>` in the child's environment only. winuse picks a free loopback port, because WebView2 needs a fixed one.
  - The owner check: msedgewebview2.exe inside the launched App's process tree. The user's other msedgewebview2 processes are unaffected.
- **Typing:** `Input.insertText` needs a DOM focus step first (the existing Page type-text --into route already does `DOM.focus`). No window activation.

### 2026-11-03: launch-time CDP done (worker, branch hm15-electron-cdp, first commit)

- `launch-app <exe> --cdp` (`internal/helper/launchcdp.go`): refused with nothing started for a shortcut or packaged App, or on a helper without Browser Connections. winuse picks an ephemeral loopback port (`System.AppPort`, never 9222-9224). An Electron executable (`browser.IsElectron`: `resources\app.asar`, `electron.asar`, `default_app.asar` or `resources\app\package.json`) gets `--remote-debugging-port=<p>` in front of its arguments plus `--disable-features=CalculateNativeWinOcclusion` (merged into its own). Anything else gets `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS` in its own environment, after a value from `--env` or the helper's own. Never the registry.
- I picked the port myself instead of reading stderr or DevToolsActivePort, because the userData folder is App-defined and the helper does not keep the App's stderr. After the Window appears, `browser.Manager.AwaitApp` polls for what is left of the launch budget. It accepts the port only when its listener is the launched pid or a descendant (`System.ParentPID`, at most 8 hops), and registers it for the Window's process. Note lines: `Note: a Browser Connection reaches the App's pages (Electron App, port <p>): get-window-state <id> reads them through it`, or `Note: no debugging port appeared, so the App stays on UI Automation`.
- `Manager.WindowConnection` dials a registered App endpoint (`ModeApp`, keyed by the Windows' pid). `open("")` (the browser commands and commands without a Window) never picks App connections; `Held()` lists them for `status`. `livePages.Browser` returns the App kind for a registered pid whatever the window class.
- `page.MatchAppTab`: the App's one non-internal page, else the page whose title the Window title shows, else NoTab/Ambiguous. This is because Electron's content-layer DevTools may not answer `Browser.getWindowForTarget`, and a WebView2 page title is not the Window's.
- Docs: ADR 0012 amendment, ADR 0013 table ("launch done; attach planned"), SKILL.md launch section, codex-parity launch_app row, CLI usage, e2e README.
- Tests: `internal/browser/apps_test.go` (switches, env var, Electron detection, owner tree check incl. the user's own msedgewebview2 refused, wait expiry and a late port, WindowConnection/open/Held/DisplayName), `internal/helper/launchcdp_test.go` (Electron and WebView2 launches, refusals, App Window recognised), `TestMatchAppTab`, `TestParseLaunchArgsCDP`. Opt-in e2e `test/e2e/app_cdp_test.go` (covered Electron 45 via `WINUSE_E2E_ELECTRON`: Page read over CDP, Headless set-value read back by eval; WebView2 via `WINUSE_E2E_WEBVIEW2`), compiled, NOT run (live runs paused).
- Live risks: page.Merge needs a UIA Document element in the App's tree. Electron gets occlusion off for that, but it is unmeasured whether a covered Electron/WebView2 exposes one. Without it, the read falls back to UI Automation with the note.

### 2026-11-03: attach to a running Electron App done (worker, branch hm15-electron-cdp, second part)

- `winuse browser attach <window>` / `browser detach <window>` (`internal/helper/attach.go`; budget 30 s). The attach checks the Window's executable is Electron (`browser.IsElectron`) and that its process publishes `node-debug-handler-<pid>`. That mapping is the nodeCliInspect fuse; without it the attach is refused with nothing sent, and the hint says the App stays on UI Automation or can be relaunched with `--cdp` if the user agrees. An App launched with `--cdp` is answered with its existing connection.
- No Node runtime is needed: `win.SignalNodeDebug` does what `process._debugProcess` does on Windows (node_process_methods.cc). It reads the handler address from the mapping and runs CreateRemoteThread at it, waiting at most 5 s. It is refused inside an offline test binary.
- `browser.Manager.AttachElectron` then polls the loopback ports the App's own process listens on (9229 first, `System.ListeningPorts` from the TCP table) for a Node inspector (`/json/list`, type node). It registers the endpoint as `ModeAttach` for the Window's pid and dials at once, so a failed shim install fails the attach.
- Transport (`internal/browser/electron.go`): one `Runtime.evaluate` (includeCommandLineAPI) installs a JS shim in the main process. Every CDP command then becomes `Runtime.evaluate(globalThis.__winuseShim.call(<json>), awaitPromise, returnByValue)`, answered as the CDP reply. Events come back through `Runtime.addBinding("__winuseEmit")` → `Runtime.bindingCalled`, and the inspector's own events are dropped.
- The shim mirrors the extension's routing: browser level emulates only `Target.getTargets`/`attachToTarget`/`detachFromTarget` over `webContents` (window, browserView, webview), with target id `electron-<id>` = session id. Child sessions keep Chromium's id via `debugger.sendCommand(method, params, sessionId)`. `Page.bringToFront`/`Target.activateTarget` are refused.
- Closing (detach, a dropped connection, `Manager.Close` at helper exit) runs the shim's `release()`: every `debugger.detach()`, then `require('inspector').close()`.
- Docs: ADR 0012 addendum, ADR 0013 row 15 done, CONTEXT.md "Browser Connection", SKILL.md (ask the user first; detach when done), codex-parity launch_app row, CLI usage, e2e README.
- Tests: `internal/browser/electron_test.go` (transport translation, errors, exceptions, events, release on close, a shim that does not install; AttachElectron refusals before any signal, inspector found on 9229 among the App's ports, no inspector, detach only of an attach), `internal/helper/attach_test.go`, `internal/win/nodedebug_test.go`, `TestParseBrowserAttach`. Opt-in e2e `TestAttachToRunningElectron` (compiled, NOT run).
- Unmeasured live: the shim itself. The probe ran the same calls by hand (`webContents.getAllWebContents()[0].debugger.attach('1.3')`, Input commands), but not the `sendCommand` session routing, `Runtime.addBinding` on Node's inspector, nor `require` through the command-line API. The live run is the check.

