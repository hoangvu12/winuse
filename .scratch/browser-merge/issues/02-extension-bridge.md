# 02: Extension bridge

**What to build:**

- **Port use-browser's MV3 extension and native messaging host** into winuse: `winuse browser ext install|status`, with registry registration per browser, self-update and diagnostics.
- **Keep:**
  - background tabs and the tab group;
  - Cancel-to-pause;
  - the idle detach.
- **Extend the bridge** to pass child sessions through, for iframes.
- **Discovery order:** the extension is preferred when connected to that browser.

See spec: "Browser Connection".

**Blocked by:** 01

**Status:** done

- [x] Offline tests for the native messaging framing and the bridge's message routing
- [x] `ext install` prints the exact steps for the user to relay (Developer mode, Load unpacked), and `ext status` diagnoses each state
- [x] Browser-level methods are refused through the extension with a clear message

## Comments

### 2026-11-02: landed

Ported from use-browser v0.9.0 (`ext.go`, `extension/manifest.json`, `extension/background.js`, and how `main.go` and `cdp.go` use them).

**Identity (settled).**

| What | Value |
|---|---|
| Extension name, tab group label | `winuse` |
| Extension version | `0.1.0` (updates are noticed by the build hash, not the version) |
| Extension ID | `mjolifepdnkcigohjhadbidbojmfhcog`, from a new RSA-2048 `key` generated offline. The private key was discarded; an unpacked extension needs only the public key. use-browser's is `blcbodpjbiimmoenmdneeacdogpglgne`, so both can be loaded at once. |
| Native host name | `com.hoangvu12.winuse` |
| Unpacked extension | `%LOCALAPPDATA%\winuse\browser\extension\` (plus `build.json`) |
| Host manifest | `%LOCALAPPDATA%\winuse\browser\native-host\com.hoangvu12.winuse.json`, `path` = the winuse binary |
| Bridge files, host log | `%LOCALAPPDATA%\winuse\browser\bridges\<browser>-<instance>.json`, `host.log` there |
| Registry (HKCU, default value = manifest path) | `Software\Google\Chrome\NativeMessagingHosts\com.hoangvu12.winuse` (also used for Opera), `Software\Microsoft\Edge\...`, `Software\BraveSoftware\Brave-Browser\...`, `Software\Chromium\...`, `Software\Vivaldi\...` |

**Command surface.** Both use `protocol.BrowserArgs`. Both are observation-only, through the existing `browser-` prefix in `observationOnlyCommand`.

| Command | Wire name | Budget |
|---|---|---|
| `winuse browser ext install [browser\|all]` | `browser-ext-install` | 30 s |
| `winuse browser ext status [browser]` | `browser-ext-status` | 30 s |

- **`install`.** With no argument it installs for the pin, else for every installed browser. It writes the extension and the host manifest, sets the registry keys, and prints:
  - `ok extension written to <dir> (id ..., build ...)`;
  - `ok native host com.hoangvu12.winuse registered for <browsers> (<exe>)`;
  - per browser, either `ok <b> is already connected through the winuse extension` or the steps block: `In Chrome: 1. Type chrome://extensions ... 2. Turn on "Developer mode" ... 3. Click "Load unpacked" and choose this folder: <dir>`;
  - then `then run: winuse browser ext status <b> (exit 0 once it is connected)`.
- **What `install` no longer does.** It opens no page, uses no clipboard and does not wait 5 minutes. It refuses a `go run` binary (a `go-build` path), as use-browser did.
- **`status`.** It exits 0 only when connected: `ok chrome connected through the winuse extension (version 0.1.0, instance ab12cd34)`, plus `note: updated the winuse extension in Chrome to build X` after a self-update. With nothing named or pinned, it reports every connected browser, else it diagnoses the first running installed browser.
- **Native host mode.** `cmd/winuse/main.go` checks `bridge.IsHostLaunch(os.Args[1:])` (the first argument starts with `chrome-extension://`) before anything can print, and runs `browser.NativeHost(origin)`.

**What landed, per file.**
- `internal/bridge/extension/manifest.json` and `background.js` (embedded with go:embed). They are use-browser's extension with these changes:
  - **Background tabs.** New tabs open with `active:false` in the "winuse" group. The user's tabs are never adopted unless targeted.
  - **User control kept.** Cancel-to-pause (the debugging bar and the toolbar icon), the 5 minute idle detach, and `Winuse.reload` for self-update.
  - **Foreground rule.** The `chrome.windows` API is never called. `Page.bringToFront` and `Target.activateTarget` are refused. `Winuse.activateTab {targetId}` does only `chrome.tabs.update(tabId, {active:true})`; it is for ticket 07 and is not wired to `Target.activateTarget`.
  - **Browser-level methods.** They are refused with the message below. `Browser.getVersion` is no longer emulated; `Winuse.status` replaces it.
  - **Child sessions.** Events from `source.sessionId` are forwarded with that sessionId instead of being dropped. `Target.attachedToTarget` and `Target.detachedFromTarget` keep a child-to-tab table. A command with a child sessionId goes to `chrome.debugger.sendCommand({targetId: tab, sessionId: child}, ...)`. When a tab detaches, its children get a `Target.detachedFromTarget` first.
  - **Mapping fields.** `Target.getTargets` also returns `tabId`, `windowId` and `active` per tab, for ticket 04's mapping.
  - **Chunked commands.** Chunks `{c, chunk, last}` are reassembled.
- `internal/bridge/bridge.go`:
  - the identity constants;
  - the error codes: `CodeFailed -32000`, `CodeRefused -32010`, `CodePaused -32011`, `CodeTooLarge -32012`, `CodeForeground -32013`;
  - `Refusal(method, sessionID) (code, msg)`, the one rule background.js mirrors;
  - `IsHostLaunch`, `Version`, `Build` (the content hash), `WriteExtension` and `HostManifest`.
- `internal/bridge/frame.go`: `WriteFrame` and `ReadFrame`, a 4-byte little-endian length plus a body that must be a JSON object. A zero length, an over-limit length (a BOM or text in the header reads as about 2 GB), or a body that is not JSON fails with `ErrBadFrame`. A short read waits for the rest. Limits:
  - `MaxToExtension` 1 MB;
  - `ChunkSize` 256 KB;
  - `MaxCommand` 64 MB;
  - `MaxFrame` 256 MB.
- `internal/bridge/files.go`: `Info`, the bridge file:
  - the host's pid, exe, port and token;
  - `browser_pid` and `browser_exe`;
  - the hello's browser, instance, version and build.

  `List(dir, alive)` prunes stale files.
- `internal/bridge/host.go`: `ServeHost(cfg, stdin, stdout)`.
  - It checks the origin (`chrome-extension://<our id>/` only) and validates the hello (instance and browser must be safe for a file name).
  - It listens on `127.0.0.1:0` with a 16-byte random token, writes the bridge file, and removes it on exit.
  - Helper connections authenticate within 5 s and get a `{"bridge":"ready"}` or `{"bridge":"refused"}` frame.
  - Commands of 1 MB and over go out in rune-safe chunks, with HTML escaping off, so every native message stays under 1 MB. Commands over 64 MB are answered locally.
  - A helper connection that stops reading for 5 s is closed rather than having replies dropped (use-browser dropped them).
- `internal/bridge/host_windows.go`:
  - `HostMain`, which refuses in a test binary (LiveDesktopGuard), plus `host.log`, which starts over above 256 KB;
  - `ParentBrowser`, which walks up from the parent past `cmd.exe` and `conhost.exe` (Chromium starts hosts through `cmd /c`);
  - `HostAlive` (the pid is alive and its image is the recorded exe), `ProcessImage` and `LastLogLine`.
- `internal/bridge/conn.go`: `Dial(ctx, addr, token)` and `Conn`, the `cdp.Transport`.
  - It applies `Refusal` and the size limit locally, answering with a CDP error frame that the cdp.Client sees as a normal error response.
  - One reader goroutine feeds one queue. Close unblocks a pending `ReadMessage` with `net.ErrClosed`.
- `internal/bridge/bridgetest/extension.go`: a fake extension that runs a real `ServeHost` over `io.Pipe`.
  - It mirrors background.js: the emulated Target methods, tab sessions named by target id, the child table, event forwarding with sessionId, detach cascades, pause, `Winuse.status`, `Winuse.reload` (with an `OnReload` hook) and `Winuse.activateTab`.
  - It fails the test on any native message over 1 MB.
- `internal/browser/extension.go`:
  - **`extensionEndpoint`** (`System.Extension` in `Live()`).
    - It picks the bridges of the wanted browser. The browser is named by the host's parent process image, falling back to the extension's brand guess, so Chrome and Chromium are told apart.
    - Two browsers with nothing pinned: `Ambiguous`, naming both. Two profiles of one browser: `ExtSeveralProfiles`.
    - It self-updates when the build differs (rewrite the folder, `Winuse.reload`, wait up to 15 s for the new bridge), then probes with `Winuse.status`.
    - It returns `Endpoint{Mode: ModeExtension, WSURL: "winuse extension 127.0.0.1:<port> (instance <id>)", PID: browser pid, Open: OpenBridge}`. The label changes when the host restarts, so the manager re-dials.
  - **`AsProblem(err, browser)`.** A `*Problem` passes through. `CodeRefused` becomes `ExtCannot` and `CodePaused` becomes `ExtPaused`. A foreground refusal stays a plain error: it is a programming error, not a user state.
  - **`ExtInstall`, `ExtStatus`, `diagnoseExt`, `hostProblem` and `extInProfiles`.** `extInProfiles` reads Default, `Profile *` and Opera's root Preferences.
  - The paths, `HostKey` and `LoadSteps`.
- `internal/browser/extension_windows.go`:
  - `NativeHost(origin)`;
  - `liveOpenBridge` (guarded) and `liveExecutable`;
  - `liveRegistry` over `golang.org/x/sys/windows/registry`, which is already in the module. `SetDefault` is guarded; reads are not.
- `internal/browser/problem.go`: 11 new kinds, each with one sentence and one next command:
  - `ExtNotInstalled` (plus the variant "host points at a missing binary");
  - `ExtNotRegistered`, `ExtNotLoaded`, `ExtDisabled`, `ExtBrowserClosed`, `ExtNotConnected` (with the last host log line);
  - `ExtOutdated`, `ExtPaused`, `ExtUnreachable`, `ExtSeveralProfiles`, `ExtCannot`.

  There is also a `kindEnd` sentinel for the coverage test. `NoDebugging`'s next command now offers `winuse browser ext install <b>` first.
- **Small edits to ticket 01's files:**
  - `system.go`: the fields `Bridges`, `OpenBridge`, `Registry` and `Executable`.
  - `system_windows.go`: `Live()` wires them, plus `Extension`. `Bridges` returns nil in a test binary, so a test never prunes the real bridge files.
  - `discover.go`: an `ExtPaused` or `ExtUnreachable` from the extension is remembered while the other routes are tried, and returned if none works. A paused extension never blocks a browser winuse launched. Anything else, including `Ambiguous`, still stops discovery.
  - `manager.go`: a failed bridge dial is `ExtUnreachable`, not "port 0 stopped answering".
  - `commands.go`: `Status` maps a paused or broken extension the same way.
  - `browsers.go`: a comment.
  - `internal/helper/browser.go`: `ExtInstall` and `ExtStatus` in `BrowserService`, the two registry entries, `browserError` through `browser.AsProblem`, and the usage hints.
  - `internal/cli/browser.go`: `parseBrowserExtArgs`, `browserUsage`. `all` is refused for status.
  - `cmd/winuse/main.go`: native host detection.
  - `third_party/use-browser/NOTICE`: the extension, the host and extension.go are now listed as ported.

**The bridge wire protocol.**
- **Helper to host.** Loopback TCP, with frames of a 4-byte LE length plus JSON.
  - First frame `{"token":"<32 hex>"}`; the answer is `{"bridge":"ready"}` or `{"bridge":"refused"}` and a close.
  - After that, plain CDP JSON-RPC both ways. `sessionId` is on the wire as on a DevTools socket: a tab's session id is its target id, and a child keeps Chrome's session id. Events carry their sessionId, and tab detaches arrive at the browser level.
- **Host to extension.** Native messaging, same framing, with these envelopes:
  - `{"c":<conn>,"m":<cmd>}`;
  - `{"c":<conn>,"chunk":"<text>","last":bool}` for commands over 1 MB;
  - `{"c":<conn>,"close":true}`.
- **Extension to host.** `{"hello":{browser,instance,version,build}}` first, then `{"c":<conn>,"m":<reply or event>}`.
- **Bridge methods** (browser-level only):
  - `Winuse.status` returns `{browser, version, build, instance, paused}`, even while paused;
  - `Winuse.reload` works even while paused;
  - `Winuse.activateTab {targetId}`.
- **Emulated without a session:** `Target.getTargets`, `Target.attachToTarget` (which returns `sessionId = targetId`), `Target.detachFromTarget`, `Target.createTarget` (background, grouped) and `Target.closeTarget`.
- **Refusals.**
  - `CodeRefused`: `<method> needs a launched or cloned browser; the winuse extension cannot do it. Run winuse browser launch or winuse browser clone.` This covers `Browser.*`, `SystemInfo.*`, `Tethering.*`, and any other method without a session.
  - `CodeForeground`: `<method> would bring the browser window to the front, and winuse never takes the foreground.` This covers `Page.bringToFront` and `Target.activateTarget`.
  - The helper turns `CodeRefused` into `ExtCannot`: "Browser.setDownloadBehavior needs a launched or cloned browser; the winuse extension cannot do it." Its next command is "run winuse browser launch chrome or winuse browser clone chrome, which take browser-level commands".

**Tests.**
- **bridge, framing:**
  - a round trip with the little-endian header checked byte by byte;
  - three frames read one byte at a time;
  - `ErrBadFrame` for each of: over the limit, zero length, a BOM header, a text header, a BOM body, not JSON, a JSON array, a JSON string, broken JSON, trailing junk;
  - a cut body and a cut header;
  - rune-safe chunk splits (1 to 4 byte runes);
  - the worst-case escaping envelope staying under 1 MB.
- **bridge, rules and identity:**
  - a `Refusal` table with codes;
  - `IsHostLaunch`;
  - the manifest key giving `ExtensionID` (Chromium's derivation), and the id differing from use-browser's;
  - background.js using the host name, group, idle time, codes and child routing, with no `chrome.windows.update` and no `focused: true`;
  - `WriteExtension` and `HostManifest`, and `List` pruning stale files.
- **bridge, routing over a real host and the fake extension:**
  - the bridge file records the browser pid and exe, and is removed on stdin EOF;
  - getTargets and attach, with 8 concurrent session calls matched by id;
  - events: a tab's event on its session, a child's attach on the tab session, and a child's event with the child's sessionId; an unattached tab's events are not forwarded;
  - `setAutoAttach` on the tab, then a child-session command reaching the child, and the tab detach cascading to the child;
  - `Browser.*`, `Page.bringToFront` and `Target.activateTarget` refused locally, never reaching the extension;
  - `activateTab`, and the paused code, with `status` still answering while paused;
  - a 3.4 MB command with every escape case arriving intact in chunks, with all envelopes under 1 MB;
  - a 64 MB+ command answered locally with `CodeTooLarge`;
  - Close unblocking a pending read, and the host telling the extension the connection closed;
  - the host exiting, which ends the client;
  - a wrong or empty token rejected, with a good token still working afterwards;
  - another extension's origin refused, a garbled hello (BOM), a hello with a path in it, and garbage after a good hello ending the host and removing its file.
- **browser:**
  - discovery: the extension preferred for the pinned browser (with Status `ok chrome connected: your running browser through the winuse extension, 2 tabs`; AttachPage giving session `T1` and focus emulation reaching it; zero WebSocket dials; and a refused `Browser.*` becoming `ExtCannot`);
  - the user pausing, which turns the held connection into `ExtPaused`;
  - the host exiting, after which discovery moves on to the launch profile;
  - Edge's extension not used for Chrome;
  - Chromium named by the host's parent despite the extension saying chrome;
  - two extension browsers unpinned giving `Ambiguous` "chrome,edge", and two Chrome profiles giving `ExtSeveralProfiles`;
  - a paused extension falling through to a launched browser;
  - self-update: reload, a new host, and the folder rewritten;
  - `ext install` against a fake registry and `t.TempDir`: the exact printed output, the files, the manifest, one key for chrome, two for chrome, edge and opera (Opera shares Chrome's key), the already-connected line, a `go run` binary refused, and an uninstalled browser refused;
  - `ext status`, one subtest per state: not installed, host binary gone, not registered, registered elsewhere, not loaded, loaded but off, browser closed, not connected (with the host log line), outdated with a failed update, paused, not answering (wrong token), several profiles, connected, and outdated then updated;
  - `ext status` with nothing named;
  - `AsProblem`;
  - `Live()`'s new seams refusing in tests (OpenBridge, Registry.SetDefault, Bridges, HostMain) and the paths;
  - the problem table, with 12 new cases.
- **helper:** the two new handlers, malformed-argument hints, a CDP refusal becoming a Problem, an ext Problem, and the action gate never held.
- **cli:** ext parsing (10 cases), exit 2, the usage text and the budgets.
- `node --check` passes on background.js.
- **Checks run:** `go build ./...`, `go vet ./...`, `go vet -tags e2e ./test/e2e` and `go test ./...` are all green. go.mod is unchanged. The browser and bridge tests pass 10 times in a row (`-count=10`).

**headless-parity-modified files I edited (minimal):**
- `internal/protocol/protocol.go`: two budget entries.
- `internal/helper/registry_test.go`: two names.
- `internal/cli/cli.go`: the usage lines for `browser ext install` and `browser ext status`.

**Risks for the ticket 03 probe to measure:**
- **Child-session passthrough.** Does `chrome.debugger.sendCommand({tabId|targetId}, "Target.setAutoAttach", {autoAttach:true, flatten:true, waitForDebuggerOnStart:false})` work for an extension, and does it produce `Target.attachedToTarget` with a `params.sessionId`? Do the child's events arrive in `onEvent` with `source.sessionId`, and does `sendCommand({targetId, sessionId}, ...)` reach a cross-origin iframe? The bridge attaches tabs by `targetId`, not `tabId`; check that `source.targetId` is set on child events too. If the browser hands out `waitForDebuggerOnStart` children, `Runtime.runIfWaitingForDebugger` must reach them.
- **Tab activation.** Does `chrome.tabs.update(tabId, {active:true})` leave the browser window unfocused and below other windows, with the foreground unchanged, including when the window is minimized? Does `chrome.tabs.create({active:false})` plus `tabs.group` ever raise it?
- **Window mapping.** Is the `windowId` now returned by `Target.getTargets` stable, and can it be matched to an HWND (bounds through `chrome.windows.get`, which the extension does not call yet; title of the active tab)? Ticket 04 needs to choose.
- **Host process and browser identity.** Does Chromium on Windows start the host through `cmd.exe /d /s /c`, so that `ParentBrowser` finds the browser one level up? Is `browser_exe` the main browser process (for Edge, `msedge.exe` under Program Files (x86))?
- **Registry keys per browser.** Does Opera read Chrome's key, and do Brave and Vivaldi read their vendor keys?
- **Debugging bar.** Does the "started debugging this browser" bar appear once per attached tab, and does Cancel arrive as `canceled_by_user`? Does it take focus?
- **Prompts.** No remote-debugging prompt should appear on the extension route; confirm.
- **Several profiles.** Profiles with the extension each run a host, and today that is `ExtSeveralProfiles` (no instance selector). Measure how common this is; ticket 04 may want one bridge per profile, matched by window.
- **Updating winuse while the browser runs.** The browser keeps `winuse.exe` running as the host, so `install.ps1` overwriting `winuse.exe` may fail while a browser with the extension is open (use-browser had the same issue). Ticket 08's installer may need to stop the host or rename first.
- **Self-update.** Does `chrome.runtime.reload()` of an unpacked extension pick up the rewritten folder and reconnect within 15 s?
- **Developer mode warnings.** Edge and Chrome may show a "Disable developer mode extensions" bubble on startup, which could take focus. Check.

### 2026-11-02: build check reads the loaded manifest

**The bug (seen live, ticket 09's run).** The user's Chrome ran extension code 0.1.0 while the unpacked folder held 0.1.2 files, and `winuse browser ext status chrome` said connected with no update. background.js read its build from `build.json` on disk at every `connect()` (the 30 s reconnect alarm included), so once the folder was rewritten, the old loaded code reported the new hash and self-update never fired.

**Decision.** The build is judged by something only a load or reload changes: the manifest the browser loaded. `WriteExtension` stamps `"version_name": "<version> build <hash>"` into the manifest it writes, and background.js reports the hash from `chrome.runtime.getManifest()`. `version_name` is a documented MV3 field (shown as the version on the extensions page); the id still comes from `key`. A service worker restart may load a newer background.js from disk under the old manifest; it then still reports the old stamp, so it is still outdated. A manifest without a stamp (every 0.1.x install) reports `""`, which never equals `Build()`, so the existing check (`b.info.Build != bridge.Build()`) already treats it as outdated. No Go-side check changed.

**build.json is gone.** Nothing else read it (`hostProblem` checks only `manifest.json`). `WriteExtension` now also deletes a leftover `build.json`: extension 0.1.2 and older read it at each connect, so without it they report no build and get reloaded into the stamped manifest.

**What changed, per file.**
- `internal/bridge/bridge.go`: `WriteExtension` writes the manifest through `stampManifest` (the embedded manifest plus `version_name`, two-space indent, no HTML escaping, LF) and removes `build.json`. New `StampedBuild(manifest) string`, the Go reading of the stamp (BOM tolerated, `""` without one), and the `buildMark` (`" build "`). The embedded manifest is unchanged apart from the version; `Version()` and `Build()` still read and hash the embedded files only.
- `internal/bridge/extension/background.js`: the `build` global and the `build.json` fetch are gone; `loadedBuild()` splits `chrome.runtime.getManifest().version_name` on `" build "`; the hello and `Winuse.status` send `build: loadedBuild()`.
- `internal/bridge/extension/manifest.json`: version 0.1.3, following tickets 04 and 07 (the build hash changes anyway).
- `internal/bridge/files.go`: the comment on `Info.Build`.
- `internal/bridge/bridgetest/extension.go`: the fake models what the browser loaded. A new `Dir` field is the folder it is loaded from: `Start` reads `Version` and `Build` from the `manifest.json` there once (an unstamped one gives `Build ""`). `Reconnect()` is a new host with the same loaded manifest (the folder is not re-read). `Reload()` is `chrome.runtime.reload()`: it loads `Dir` again. With a `Dir` and no `OnReload`, `Winuse.reload` reloads by itself. The off switch carries over both, as `chrome.storage` does. Without `Dir`, it behaves as before (defaults `bridge.Version()` and `bridge.Build()`). `Start` also stops waiting for its bridge file once the extension is closed. This fixes a race older than this change: a reload hook's `Start` could still be polling when the code under test had already seen the new file and ended the test, whose cleanup closed the host, so `Start` timed out after the test and panicked (1 in about 25 runs of `-count=5`).

**Tests.**
- bridge: `TestWriteExtensionAndHostManifest` now checks that a leftover `build.json` is deleted, that the written manifest's stamp is `Build()`, that `version_name` is `<Version()> build <Build()>`, that it equals the embedded manifest plus the stamp (key included), and its LF ending, and that `StampedBuild` gives `""` for the embedded manifest, a `version_name` with no build, and garbage. `TestExtensionIdentity` checks that background.js reads `chrome.runtime.getManifest().version_name`, splits on the same mark as Go, sends `loadedBuild()` in the hello and the status, and never mentions `build.json`.
- browser: new `TestSelfUpdateJudgesTheLoadedExtension` reproduces the bug, with one subtest each for `ext status` and discovery. An unstamped 0.1.0 folder is loaded, `WriteExtension` rewrites it, and the extension reconnects without a reload. The bridge file must say version 0.1.0 with build `""`. Then each route must send exactly one `Winuse.reload`, report `note: updated ...` (status) or an extension endpoint on the new host (discovery), and leave one bridge with the current version and build. `TestDiscoverUpdatesAnOutdatedExtension` and `TestExtInstall` check the manifest stamp instead of `build.json`.
- `node --check` passes on background.js. `go build ./...`, `go vet ./...`, `go vet -tags e2e ./test/e2e` and `go test ./...` are green. The bridge and browser tests passed 30 times in a row (`-count=5`, six rounds).

**Live risk.**
- The first update of an existing 0.1.x install relies on the old code: once `build.json` is deleted, its next hello (a host restart) reports `""`. Until then, its bridge file still holds the hash it read earlier. That hash is a 0.1.2 build, which differs from this binary's, so self-update fires anyway.
- Confirm in a real Chrome that `chrome.runtime.getManifest().version_name` returns the stamp, that chrome://extensions shows `0.1.3 build <hash>` with no warning, and that `chrome.runtime.reload()` after a rewrite brings back a bridge with the new build within 15 s.
- Two winuse binaries with different extensions (for example a dev build and the installed one) will keep reloading each other's extension, as before.
