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

**Status:** needs-triage (ADR 0012 Decision 6 and Consequences; browser-merge spec out-of-scope)

- [ ] `--cdp` in the CLI and protocol
- [ ] Electron and WebView2 argument injection; port discovery and owner verification
- [ ] Window-to-Page mapping for non-browser hosts
- [ ] Offline tests: argument and environment building, owner check, no-port fallback
- [ ] Opt-in e2e: VS Code or another installed Electron App, and a WebView2 App, covered
- [ ] ADR 0012 amendment, SKILL.md
