# agent-browser vs use-browser as the base for winuse's CDP backend

## Sources and versions

- **use-browser: v0.9.0, commit `14aac7d` of https://github.com/hoangvu12/use-browser** (pushed 2026-11-01). Citations use the prefix `ub/`, for example `ub/cdp.go:431-437`. Permalink base: https://github.com/hoangvu12/use-browser/blob/14aac7dd43cd8ecdc4feb7a9a9ac834bb8c49433/
  - **This is the newest source.** The local skill folder `C:\Users\ADMIN\.claude\skills\use-browser` holds an older v0.6.0 source (`main.go:10` there says `const version = "0.6.0"`) that has no `ext.go`, `extension/`, `find.go`, `run.go` or `key.go`. Its `SKILL.md`, however, is the v0.9.0 text: it is identical to `ub/SKILL.md` except for a trailing newline. That is why the skill describes `ext install/status` while the local Go files don't implement it.
  - **The installed binary matches v0.9.0.** `C:\Users\ADMIN\AppData\Local\use-browser\use-browser.exe` prints `use-browser 0.9.0` and is 6,870,016 bytes, the same size as the v0.9.0 `use-browser_windows_amd64.exe` release asset (https://github.com/hoangvu12/use-browser/releases/tag/v0.9.0).
- **agent-browser: v0.41.1, commit `5868f7c` of https://github.com/vercel-labs/agent-browser** (2026-10-30). Citations use the prefix `ab/`. Permalink base: https://github.com/vercel-labs/agent-browser/blob/5868f7c05020e047c00e68e4abf9253cce864f41/
- **winuse paths** are relative to the winuse repo root and use the prefix `winuse/`.
- **How the reading was done.** Everything was read only; no browser was launched. agent-browser is about 115k lines of Rust in `ab/cli/src`, so parts of it were read by helper agents. I re-read their key citations before using them; they are marked "(inference)" wherever they rest on a grep for something absent rather than on reading code.

## Verdict

**agent-browser is the better browser tool overall.** It has more actions, a real accessibility-tree snapshot, durable refs, cross-origin iframe support, dialog and download handling, actionability checks and a large test suite.

**use-browser is the better base for winuse's CDP backend.** Four reasons:
- It is already Go with zero dependencies, so it matches ADR 0003 (`winuse/docs/adr/0003-go.md:3`).
- It is small enough to own outright (about 6.5k lines including the extension).
- It is Windows-first: Edge, Vivaldi and Opera support, the cookie-lock-aware profile clone, and registry-based native messaging.
- It already does the one thing that matters most for "never take the foreground": `Emulation.setFocusEmulationEnabled` on every attach, and background tabs in extension mode.

**The recommendation is a mix.** Build the backend from use-browser's Go code: transport, discovery, launch, clone and extension bridge. Port agent-browser's ideas into it:
- an accessibility-tree snapshot keyed by `backendNodeId`;
- flattened auto-attach for cross-origin iframes;
- `mouseMoved` before clicks;
- actionability and cover checks that refuse rather than warn;
- JavaScript dialog handling, file uploads and downloads;
- waits;
- one persistent connection.

The persistent connection belongs in winuse's existing background helper, which already owns live Elements (`winuse/docs/adr/0004-window-state-tree-format.md:45-49`, `winuse/docs/adr/0003-go.md:10`).

Better on each axis:

1. **Architecture.** For winuse, use-browser: Go, 6.9 MB, no dependencies. agent-browser's daemon model is the better process model, but winuse already has a helper to host it.
2. **Connecting to the browser.** use-browser: the extension bridge, an incrementally synced clone, the explicit Chrome 136 handling, and six Chromium browsers including Edge. agent-browser handles the Chrome 144 approval prompt better, because one daemon connection means one prompt.
3. **Page observation.** agent-browser: the accessibility tree, `backendNodeId` refs, out-of-process iframes and delta snapshots. use-browser's output is shorter.
4. **Actions.** agent-browser on breadth. use-browser on foreground discipline: focus emulation and background tabs. Each has its own foreground leaks.
5. **Robustness.** agent-browser, clearly.
6. **Agent-facing surface.** use-browser for winuse: small, one `ok` line per command, and close to winuse's own style. agent-browser's surface is far larger than winuse needs.
7. **License.** agent-browser is Apache-2.0, so it can be ported into MIT winuse with attribution, but it is Rust, so it is a source of ideas, not code. use-browser has no license file but is the same author's code.

## Comparison table

| Axis | use-browser 0.9.0 | agent-browser 0.41.1 | Better for winuse |
|---|---|---|---|
| Language, size | Go, ~6.5k lines, no `require` in go.mod (`ub/go.mod:1-3`). Windows amd64 binary 6.9 MB (release v0.9.0). | Rust, ~115k lines in `ab/cli/src`; tokio, tungstenite, rustls, reqwest (`ab/cli/Cargo.toml:14-35`). Windows x64 binary 18.1 MB (release v0.41.1). | use-browser |
| Process model | No daemon. One WebSocket per invocation; batch mode shares one (`ub/cdp.go:15-18`, `ub/main.go:267-319`). | Daemon per session holding one CDP WebSocket, 1 h idle timeout (`ab/README.md:1788-1793`, `ab/cli/src/native/daemon.rs:165-173`). | agent-browser's model, hosted in winuse's helper |
| Windows | Builds windows/amd64 and arm64 (`ub/.github/workflows/release.yml:19-26`). | Windows x64 only, mingw cross-build (`ab/.github/workflows/release.yml:124-127`). Job Object and private desktop (`ab/cli/src/native/cdp/windows_process.rs:1-10`). | Both work; use-browser covers arm64 |
| Browsers | Chrome, Brave, Edge, Chromium, Vivaldi, Opera (`ub/browsers.go:273-304`, `:327`). | Chrome for Testing, Chrome, Brave on Windows. No Edge detection. Plus Lightpanda, Obscura, Safari/iOS, cloud providers. | use-browser (Edge matters on Windows) |
| Real logins | Extension bridge over `chrome.debugger`; persistent clone with incremental sync; inspect toggle. | `--profile <name>` copies to a temp dir per launch; `--auto-connect`. | use-browser |
| Chrome 136+ | Explicit: clone to a non-default dir (`ub/clone.go:8-13`). | Not addressed by name; profile copy avoids it as a side effect (inference). | use-browser |
| Chrome 144+ prompt | Prompts on every invocation; tells the agent to batch or use the extension (`ub/SKILL.md:41`, `ub/cdp.go:475-486`). | Waits 20 s for approval and never opens a second prompt (`ab/cli/src/native/cdp/chrome.rs:1175-1182`, `:1243-1248`). | agent-browser (one connection) |
| Snapshot | One JavaScript DOM walk, interactive elements only, flat `[5]<button> "Sign in"` (`ub/snap.go:18-122`). | `Accessibility.getFullAXTree` plus a JavaScript walk for cursor-pointer elements, tree `- button "Submit" [ref=e2]` (`ab/cli/src/native/snapshot.rs:327-334`, `:771-874`). | agent-browser |
| Refs | Array `window.__bu` in the page; a new index each snapshot (`ub/snap.go:115`). | `backendNodeId` map in daemon memory; durable across snapshots; role+name fallback (`ab/cli/src/native/element.rs:8-31`, `:425-511`). | agent-browser |
| Iframes, shadow DOM | Same-origin iframes and open shadow roots; cross-origin skipped (`ub/snap.go:104-113`). | Same-origin and cross-origin frames via flattened auto-attach, one level deep (`ab/cli/src/native/snapshot.rs:641-708`). | agent-browser |
| Actions | nav, click, fill, type, key, scroll, tabs, js, cdp. | Also hover, drag, select, check, upload, wheel, tap, dialogs, downloads, waits, find-by-role, and much more. | agent-browser |
| Foreground | Focus emulation on every attach (`ub/cdp.go:431-437`); extension tabs open in the background (`ub/extension/background.js:174-177`). But `tab` activates. | `Page.bringToFront` on tab switch (`ab/cli/src/native/browser.rs:2022-2025`); no focus emulation. | use-browser |
| Robustness | Cover check only warns; no dialog handling; 4 unit tests, all for clone sync (`ub/clone_test.go:27-102`). | Refuses covered or disabled targets; dialog-blocked detection; 174 e2e tests; CI on Windows. | agent-browser |
| Agent surface | ~25 commands, one 20 KB SKILL.md, `ok` lines. | Hundreds of commands, MCP server, skill served by the CLI (43 KB core). | use-browser |
| License | No license file (GitHub reports none). Same author. | Apache-2.0 (`ab/LICENSE:1-3`); bundled axe-core is MPL-2.0. | Either; agent-browser is ideas-only (Rust) |

## 1. Architecture

### use-browser

- **One process per command, no daemon.** "each invocation dials the browser WS, attaches to the current page target, acts, and exits" (`ub/cdp.go:15-18`; also `ub/README.md:123`).
  - The current tab id, browser pin and port live in a small JSON state file per session (`ub/cdp.go:686-721`).
  - Snapshot refs live in the page (`ub/cdp.go:17-18`).
- **Batch mode** runs many lines over one connection and stops at the first error (`ub/main.go:267-319`). This is also its answer to the Chrome 144 prompt (`ub/SKILL.md:41`).
- **No dependencies.**
  - `go.mod` has no `require` block (`ub/go.mod:1-3`).
  - The WebSocket client is a hand-written RFC 6455 client of 167 lines ("no compression, no TLS", `ub/ws.go:3-4`).
  - The CDP client is a plain request/response loop that discards every event (`ub/cdp.go:333-373`, events dropped at `:362-364`).
- **Extension mode adds one long-lived process.** The browser starts the same binary as a native messaging host, which lives as long as the extension's port and relays CLI connections to the extension over loopback TCP with a token (`ub/ext.go:3-20`, `:256-340`).
- **Size and platforms.** It cross-compiles with `CGO_ENABLED=0 ... -ldflags "-s -w"` for linux, darwin and windows, each on amd64 and arm64 (`ub/.github/workflows/release.yml:19-26`). The v0.9.0 Windows amd64 asset is 6,870,016 bytes (https://github.com/hoangvu12/use-browser/releases/tag/v0.9.0).
- **Windows care shows throughout:**
  - BOM stripping for PowerShell pipes (`ub/main.go:282-284`, `ub/cdp.go:731-734`);
  - `tasklist` and `netstat` to check which process owns a debug port (`ub/browsers.go:686-743`);
  - `taskkill` without `/F`, so Chromium shuts down cleanly (`ub/clone.go:416-425`).

### agent-browser

- **Client plus daemon.** "Rust CLI ... communicates with daemon"; "Rust Daemon - Pure Rust daemon using direct CDP" (`ab/README.md:1788-1791`).
  - The daemon starts on the first command and exits after 1 hour idle (`ab/README.md:1793`; `DEFAULT_IDLE_TIMEOUT_MS` at `ab/cli/src/native/daemon.rs:165-173`).
  - The default timeout never closes a headed or user-attached browser (`ab/cli/src/native/daemon.rs:166-172`).
- **One persistent CDP WebSocket per daemon.**
  - `DaemonState.browser` holds a `BrowserManager` (`ab/cli/src/native/actions.rs:625-626`) with an `Arc<CdpClient>` (`ab/cli/src/native/browser.rs:490-492`).
  - The client sends a keepalive ping every 30 s (`ab/cli/src/native/cdp/client.rs:48-50`).
  - Event subscribers, such as the dialog handler, run as background tasks (`ab/cli/src/native/actions.rs:1168-1190`).
- **CLI-to-daemon IPC.** On Windows it is loopback TCP; unix uses a domain socket (`ab/cli/src/connection.rs:1064-1080`).
  - The port is derived from a hash of the session name (`ab/cli/src/connection.rs:422-431`).
  - If that bind fails, it falls back to an ephemeral port written to a `.port` file (`ab/cli/src/native/daemon.rs:364-378`).
- **Dependencies:** tokio, tokio-tungstenite with rustls, reqwest, aes-gcm, zip, rust-embed, and windows-sys on Windows (`ab/cli/Cargo.toml:14-47`).
- **npm wrapper.**
  - The npm package needs Node >= 24 (`ab/package.json:7-8`).
  - Its postinstall downloads the native binary from GitHub releases (`ab/scripts/postinstall.js:50-52`).
- **Size and platforms.**
  - The v0.41.1 `agent-browser-win32-x64.exe` is 18,063,872 bytes (https://github.com/vercel-labs/agent-browser/releases/tag/v0.41.1).
  - The only Windows target is `x86_64-pc-windows-gnu`, cross-compiled on Ubuntu (`ab/.github/workflows/release.yml:124-127`). There is no Windows arm64 build; the postinstall falls back to x64 there (`ab/scripts/postinstall.js:38`).
- **Windows-specific code** (`ab/cli/src/native/cdp/windows_process.rs:1-10`):
  - Chrome runs inside a Job Object with kill-on-close.
  - Headless Chrome runs on a private desktop so hidden windows can't paint on the user's desktop.

### What this means for winuse

- **winuse already has a long-lived helper.** It is reached over a named pipe (`winuse/docs/adr/0003-go.md:10`) and keeps live Elements per Window (`winuse/docs/adr/0004-window-state-tree-format.md:45-49`).
- **So it can host agent-browser's persistent-connection model with use-browser's Go code** (inference). A connection held in the helper avoids:
  - the per-invocation dial;
  - the per-invocation re-attach and re-enable;
  - the per-connection Chrome 144 prompt;
  - in-page ref storage.

## 2. Connecting to the browser

### use-browser: four routes, chosen automatically

`discover()` resolves an endpoint in this order (`ub/cdp.go:154-241`):
1. `BU_CDP_WS` / `BU_CDP_URL`.
2. The port it remembered from its own launch.
3. A live extension bridge.
4. A scan of its own profile dirs and the browsers' real user-data dirs for a port file or `DevToolsActivePort`. The ws-path fallback is used only if the port is alive (`ub/cdp.go:184-210`).
5. Ports 9222 to 9224, filtered by process ownership when a browser is pinned (`ub/cdp.go:232-239`, `ub/browsers.go:686-714`).

It refuses to guess when two browsers are debuggable and nothing is pinned (`ub/cdp.go:268-277`).

- **Launch.** `launch` starts the browser with a persistent per-session automation profile (`ub/browsers.go:122-132`).
  - Flags: `--remote-debugging-port`, `--user-data-dir`, `--no-first-run`, `--no-default-browser-check` (`ub/browsers.go:514-519`).
  - It picks a free port from 9222 upward (`ub/browsers.go:182-192`).
  - There is no headless option. The window is a normal headed one (`ub/browsers.go:514-521`).
- **Attach (inspect toggle).** `connect` opens `<browser>://inspect/#remote-debugging` and polls for up to 90 s (`ub/browsers.go:444-482`). It reads the `DevToolsActivePort` file, because the toggle disables `/json/*` (`ub/cdp.go:8-10`, `:78-92`).
- **Profile cloning.** `clone` copies the real profile to `profile-<browser>-clone` and launches it with flag-based debugging (`ub/clone.go:3-13`, `:445-636`).
  - It copies `Local State` so the copied cookies can be decrypted (`ub/clone.go:564-566`).
  - It skips cache directories (`ub/clone.go:151-159`).
  - Later runs do an incremental sync that also prunes deleted files (`ub/clone.go:573-601`; tests at `ub/clone_test.go:24-100`).
  - It knows that on Windows a running browser holds `Network\Cookies` without share-read. It warns, and offers `--close-source`, which closes the browser cleanly, syncs, and relaunches the clone with `--restore-last-session` (`ub/clone.go:528-548`, `:581-585`, `:608-619`).
  - The clone is shared by all sessions (`ub/browsers.go:134-140`).
- **Extension.** `ext install` does the setup: embedded unpacked MV3 extension, native host manifest, `HKCU\...\NativeMessagingHosts` keys for each browser (`ub/ext.go:449-458`, `:460-562`).
  - The extension emulates `Target.getTargets/attachToTarget/createTarget/activateTarget/closeTarget` on `chrome.tabs`. It sends page commands to `chrome.debugger.sendCommand` (`ub/extension/background.js:120-151`, `:200-205`).
  - It refuses `Browser.close` and every other browser-level method (`ub/extension/background.js:146-149`).
  - It needs Chrome 125+ and the `debugger`, `tabs`, `tabGroups` and `nativeMessaging` permissions (`ub/extension/manifest.json:7-8`).
  - The user stays in control. Cancel on the "started debugging" bar or the toolbar icon pauses it (`ub/extension/background.js:257-272`), and idle tabs are detached after 5 minutes (`ub/extension/background.js:20`, `:222-227`).
  - It updates itself on version mismatch (`ub/ext.go:629-674`).
  - `ext status` diagnoses each failure state with a sentence for the agent to relay (`ub/ext.go:748-783`).
- **Chrome 136+ default-profile restriction.** Handled explicitly.
  - Documented at `ub/browsers.go:6-13`.
  - Avoided by `launch` (dedicated dir) and `clone` (copy at a non-default path, `ub/clone.go:8-13`).
  - Avoided by the extension, which needs no debug port.
  - Background: https://developer.chrome.com/blog/remote-debugging-port
- **Chrome 144+ "Allow remote debugging" prompt.** Acknowledged but not avoided in toggle mode.
  - It prints a note before dialing and allows 60 s for the click (`ub/cdp.go:472-486`).
  - The skill says the prompt is per connection, so every invocation prompts again and only batching helps (`ub/SKILL.md:41`).
  - `doctor` tells the agent to switch to `clone` or batch mode (`ub/commands.go:477-482`).
  - The extension is the real fix ("no permission popup", `ub/README.md:45`).
- **Browsers supported:** Chrome, Brave, Edge, Chromium, Vivaldi, Opera on Windows, macOS and Linux (`ub/browsers.go:32-70`, `:273-327`).

### agent-browser

- **Launch.**
  - It finds Chrome for Testing (installed by `agent-browser install` from googlechromelabs, `ab/cli/src/install.rs:7-8`), then system Chrome, then Brave in `%LOCALAPPDATA%` (`ab/cli/src/native/cdp/chrome.rs:1120-1143`), then Puppeteer and Playwright caches.
  - Headless is the default (`ab/cli/src/native/cdp/chrome.rs:397`) and uses `--headless=new` (`:509-510`).
  - Launch flags include `--remote-debugging-port=0` and `--disable-backgrounding-occluded-windows` (`ab/cli/src/native/cdp/chrome.rs:466-481`).
  - With no profile, it uses a throwaway temp user-data-dir (`ab/cli/src/native/cdp/chrome.rs:539-551`).
- **Attach.**
  - `--cdp <port|url>` tries `/json/version`, then `/json/list`, then a direct WebSocket. The code notes that "Chrome 136+ with UI-based remote debugging (chrome://inspect) ... does not serve HTTP discovery endpoints" (`ab/cli/src/native/cdp/discovery.rs:97-117`).
  - `--auto-connect` reads `DevToolsActivePort` from the default user-data dirs of Chrome, Canary, Chromium and Brave, then probes 9222 and 9229 (`ab/cli/src/native/cdp/chrome.rs:1287-1336`; `ab/docs/content/docs/cdp-mode.mdx:65-68`).
- **Chrome 144+ prompt.** Handled well.
  - Auto-connect holds the handshake open for 20 s (`ab/cli/src/native/cdp/chrome.rs:1243-1248`).
  - If the prompt is still showing, it fails with "Chrome is waiting for remote-debugging approval ... Approve the prompt in Chrome and retry" rather than opening a second prompt (`ab/cli/src/native/cdp/chrome.rs:1175-1182`; `ab/docs/content/docs/cdp-mode.mdx:70`).
  - It also resumes targets paused after attach with `Runtime.runIfWaitingForDebugger` (`ab/docs/content/docs/changelog.mdx:740`).
  - Because the daemon keeps one connection, a user approves once per daemon lifetime, not once per command (inference from the persistent client above).
- **Chrome 136+ restriction.** No explicit handling or documentation found. `--profile <name>` copies the profile to a temp dir (`ab/cli/src/native/cdp/chrome.rs:737-758`, `:1465-1542`), which sidesteps the restriction (inference).
- **Profile reuse.** `--profile Default` makes a read-only copy that is deleted on close, for Chrome, Canary, Chromium and Brave. The Windows note says to close Chrome first (`ab/docs/content/docs/sessions.mdx:57-86`). Unlike use-browser's clone, it is not kept or synced between runs (`ab/docs/content/docs/sessions.mdx:83`).
- **Extension.** None for control. `--extension` only loads unpacked extensions into a launched Chrome (inference from `ab/README.md:1106` and a grep that found no `chrome.debugger` relay).
- **State.** `state save/load` covers cookies and storage, with optional AES-GCM encryption (`ab/cli/src/native/state.rs:256-262`, `:727-753`). There is also a credential vault (`ab/cli/src/native/auth.rs:9-49`).
- **Browsers and engines.**
  - Chrome, Brave, Lightpanda and Obscura over CDP (`ab/README.md:1795`).
  - Safari and iOS through WebDriver.
  - Cloud providers (`ab/cli/src/native/providers.rs:57-93`).
  - Edge is not auto-detected: there are no `msedge` references in `ab/cli/src` (grep), so it would need `--executable-path` or `--cdp` (inference).

## 3. Page observation

### Snapshot format

**use-browser** runs one injected JavaScript pass (`ub/snap.go:18-122`).
- **What it keeps** (`ub/snap.go:22-45`):
  - links with `href`, buttons, selects, textareas, summaries, non-hidden inputs;
  - 16 ARIA widget roles;
  - `onclick` / `jsaction` attributes;
  - top-level contenteditables;
  - elements with `cursor: pointer`, unless the whole body is pointer.
- **What it drops:**
  - children of an already-interactive ancestor, and pointer containers with long text or pointer children (`ub/snap.go:77-87`);
  - zero-size, hidden or transparent elements, and elements clipped by an overflow ancestor (`ub/snap.go:52-67`, `:88-92`).
- **Line format:** `[N]<tag[:type][ role=r]> "label"` plus ` [x]`, ` [ ]` or ` disabled` (`ub/snap.go:93-102`). Labels come from the value, placeholder, innerText, aria-label, title or alt, capped at 80 characters (`ub/snap.go:68-76`).
- **Header and limits:** a header line with the URL, title and scroll position (`ub/snap.go:154`); 150 lines by default, with a "... N more" footer (`ub/snap.go:135`, `:162-164`).
- **No page structure.** There are no headings or landmarks. Page text comes from a separate `text` command, which is `document.body.innerText` capped at 4000 characters (`ub/commands.go:164-193`).

**agent-browser** uses the browser's accessibility tree.
- **Source:** `DOM.enable` and `Accessibility.enable`, then `Accessibility.getFullAXTree` (`ab/cli/src/native/snapshot.rs:327-334`, `:428-434`).
- **A second JavaScript pass** finds `cursor:pointer`, onclick or tabindex elements that have no ARIA role. It tags them with `data-__ab-ci` (`ab/cli/src/native/snapshot.rs:771-874`).
- **Who gets a ref** (`ab/cli/src/native/snapshot.rs:107-137`):
  - interactive roles and table cells always;
  - headings, list items and landmarks only when they have a name.
- **Line format:** an indented tree, `- role "name" [level=1, checked=..., ref=eN, url=...]` (`ab/cli/src/native/snapshot.rs:1267-1344`).
  - The docs show other formats: `[ref=e1] [level=1]` (`ab/docs/content/docs/selectors.mdx:14`) and `@e1 [heading] "Log in"` (`ab/skill-data/core/SKILL.md:121-126`). The skill teaches a format the code does not print.
- **Flags:**
  - `-i` interactive only; `-c` compact; `-d N` depth; `-s <css>` scope (`ab/cli/src/native/snapshot.rs:337-403`, `:1243-1265`, `:1355-1391`);
  - `--delta`, which falls back to a full snapshot when the delta is 70% or more of the tree (`ab/cli/src/native/actions.rs:6222-6349`).
- **Fit for winuse's "one view" (inference).** The AX tree's roles and names map naturally onto winuse's Window State grammar, which renders English role names (`winuse/docs/adr/0004-window-state-tree-format.md:13-27`). use-browser's tag-based lines would need a second vocabulary.

### Refs and stability

**use-browser.**
- Refs are the array `window.__bu` in the page's main world (`ub/snap.go:115`). Each `snap` renumbers from 1.
- An index resolves through `resolveJS` (`ub/snap.go:168-190`), which fails loudly:
  - "no snapshot in this page; run: use-browser snap" after navigation;
  - "out of range";
  - "no longer in DOM" (`ub/snap.go:172-174`).
- Refs survive across CLI invocations because the page holds them (`ub/snap.go:5-8`).
- Page scripts can see and overwrite `window.__bu` (inference).

**agent-browser.**
- The `RefMap` in daemon memory maps each ref to `{backend_node_id, role, name, nth, selector, frame_id}`, with buckets per document (`ab/cli/src/native/element.rs:8-31`).
- Refs are durable for surviving nodes in the same document (keyed by session and loaderId). Numbers are never reused, and the snapshot reports `removedRefs` (`ab/cli/src/native/element.rs:145-182`; `ab/cli/src/native/actions.rs:6210-6225`).
- To resolve a ref (`ab/cli/src/native/element.rs:425-511`, `:876-937`):
  1. `DOM.getBoxModel` on the cached `backendNodeId`.
  2. If that fails, it re-reads the AX tree and matches role+name. It refuses when duplicates existed or several nodes now match.
- Error texts:
  - `Unknown ref: eN` (`ab/cli/src/native/element.rs:436`);
  - `Element ref eN is stale` (`:471`);
  - "Stale ref ... cannot be recovered ... Run snapshot again" (`:887`, `:917`).
- **What fits winuse.** winuse defines an Element Index as valid only for the Window State that produced it (`winuse/CONTEXT.md:31-33`). So winuse needs agent-browser's storage (node ids held outside the page, in the helper) more than its durability (inference).

### Token cost

- **Neither is benchmarked.**
  - use-browser claims 200 to 600 tokens per look (`ub/README.md:5`).
  - agent-browser claims "~200-400 tokens" (`ab/skill-data/core/SKILL.md:9`).
  - agent-browser's `benchmarks/` measure latency, memory and binary size, not snapshot size (`ab/benchmarks/README.md:7-25`).
  - Its `evals/context-footprint.ts` estimates context as characters divided by 4 (`ab/evals/context-footprint.ts:27-29`).
- **Inference:** use-browser's flat lines are shorter per element and it lists fewer elements. agent-browser's `-i` output adds tree indentation and named headings, list items and table cells.

### Iframes, shadow DOM, cross-origin frames

**use-browser.**
- It walks open shadow roots and same-origin iframes inside the same JavaScript pass (`ub/snap.go:104-113`).
- Cross-origin frames throw and are silently skipped (`ub/snap.go:108`).
- Click points add up frame offsets (`ub/snap.go:177-183`).
- Closed shadow roots are not reachable from page JavaScript (inference).

**agent-browser.**
- It turns on `Target.setAutoAttach {flatten:true}` (`ab/cli/src/native/browser.rs:1043-1059`) and records each iframe's session (`ab/cli/src/native/actions.rs:1886-1891`).
- Cross-origin frames use their own session; same-origin frames use the parent session with a `frameId` (`ab/cli/src/native/element.rs:842-859`).
- The snapshot inlines one level of iframes under the iframe line (`ab/cli/src/native/snapshot.rs:641-708`; `ab/docs/content/docs/snapshots.mdx:107`).
- There is a `frame` command to scope to a frame (`ab/cli/src/native/actions.rs:10267-10411`).
- Shadow DOM content appears through the AX tree (inference). `-s` scoping uses `describeNode pierce:true` (`ab/cli/src/native/snapshot.rs:375-381`).
- The cursor-pointer JavaScript pass uses `querySelectorAll('*')` on the top document only (`ab/cli/src/native/snapshot.rs:483`, `:798`), so pointer-only elements inside shadow roots and iframes are missed (inference).

**Extension-mode caveat for winuse (inference).** use-browser's extension forwards only events whose source has no `sessionId` (`ub/extension/background.js:240-242`) and addresses sessions by target id only (`ub/extension/background.js:200-205`). So agent-browser's out-of-process iframe approach would need the bridge extended before it works on the user's real browser.

### Screenshots

**use-browser.** `Page.captureScreenshot` as PNG to a temp path (`ub/commands.go:235-272`). `--full` sets `captureBeyondViewport` with no clip, so it likely still captures only the viewport (inference: the CDP docs make the clip define the area, https://chromedevtools.github.io/devtools-protocol/tot/Page/#method-captureScreenshot).

**agent-browser.** `Page.captureScreenshot` with `fromSurface:true` (`ab/cli/src/native/screenshot.rs:163-213`).
- Full page uses a clip from `Page.getLayoutMetrics` (`ab/cli/src/native/screenshot.rs:175-188`).
- An element or `@ref` clip is supported (`ab/cli/src/native/screenshot.rs:189-201`).
- `--annotate` draws ref labels (`ab/cli/src/native/screenshot.rs:227-318`).
- `--if-changed` diffs pixels against a baseline (`ab/cli/src/native/actions.rs:6392-6456`).

**Both** capture from the renderer, not the screen, so neither needs the window visible or in front (inference). In winuse, the Window State screenshot already comes from Windows.Graphics.Capture (`winuse/docs/adr/0003-go.md:9`). A CDP capture would be an extra option for page-only or full-page images.

## 4. Actions

"Trusted" means an `Input.dispatch*` or `Input.insertText` event, which the page sees with `isTrusted=true`. "JS" means the page's own JavaScript is run to fake it.

| Action | use-browser | agent-browser |
|---|---|---|
| Click | Trusted. JS `scrollIntoView` and center point, then `mousePressed` and `mouseReleased` only, with no `mouseMoved` (`ub/snap.go:175-189`, `:215-224`). A covered point only adds a warning to the output (`ub/snap.go:184-188`, `:265-267`). Coordinate clicks too. | Trusted. `DOM.scrollIntoViewIfNeeded`, then box-model center (`ab/cli/src/native/element.rs:442-457`, `:686-694`). Then `mouseMoved`, `mousePressed`, `mouseReleased` (`ab/cli/src/native/interaction.rs:1056-1165`). It refuses if the target is covered, invisible, disabled or out of the viewport (`ab/cli/src/native/element.rs:1003-1047`, `:1115-1127`). |
| Double, right | Trusted, `clickCount` 2 or `button` right (`ub/snap.go:228-240`). | Trusted (`ab/cli/src/native/interaction.rs:98-115`). |
| Fill | Mixed. JS `focus()` and `select()`, then trusted `Input.insertText`, then JS `input` and `change` events (`ub/snap.go:287-340`). | Mixed. JS `focus`, then JS clear (`value=''` plus an `input` event), then trusted `Input.insertText` (`ab/cli/src/native/interaction.rs:155-219`). |
| Type | Trusted `Input.insertText` of the whole string into the current focus. No key events (`ub/commands.go:310-320`). | JS focus, then one `Input.insertText` per character. Only `\n`, `\r` and `\t` become key events (`ab/cli/src/native/interaction.rs:280-344`, comment at `:323-325`). |
| Keys | Trusted `Input.dispatchKeyEvent` keyDown, char, keyUp with modifiers. About 16 named keys plus single characters (`ub/commands.go:322-411`). | Trusted keyDown and keyUp with modifiers (`ab/cli/src/native/interaction.rs:357-407`). Raw `keydown`/`keyup` commands too (`ab/cli/src/native/actions.rs:7946-7967`). |
| Scroll | JS `scrollBy` / `scrollTo` on the window only (`ub/commands.go:274-308`). | JS `scrollBy` on the window or an element (`ab/cli/src/native/interaction.rs:409-459`). Trusted `mouseWheel` via `wheel` (`ab/cli/src/native/actions.rs:9577-9602`). |
| Hover | None (only through `cdp`). | Trusted `mouseMoved` (`ab/cli/src/native/interaction.rs:117-153`). |
| Drag | None. | Trusted press, interpolated moves, release (`ab/cli/src/native/actions.rs:11238-11352`). An e2e test checks it starts a native HTML5 drag (`ab/cli/src/native/e2e_tests.rs:4442-4503`). |
| Select | None (fill or `js`). | JS: sets `selected`, fires `change` (`ab/cli/src/native/interaction.rs:461-546`). |
| Check | Click only. | Trusted click, re-read the state, JS `.click()` fallback (`ab/cli/src/native/interaction.rs:548-720`). |
| Upload | None (only `cdp DOM.setFileInputFiles` by hand). | `DOM.setFileInputFiles` (`ab/cli/src/native/browser.rs:2317-2357`). |
| Navigation | `Page.navigate`, then poll `readyState` for up to 12 s, not fatal (`ub/commands.go:22-50`, `ub/cdp.go:672-682`). No back, forward or reload. | `Page.navigate` with `waitUntil` of load, domcontentloaded, networkidle or none; 25 s default (`ab/cli/src/native/browser.rs:392-408`, `:1428-1518`). Back and forward are JS `history` calls plus a 500 ms sleep (`ab/cli/src/native/actions.rs:7207-7242`). |
| Tabs | `tabs`, `tab`, `open`, `close` with stable 8-character ids (`ub/cdp.go:565-596`). `--tab` targets one tab without state (`ub/cdp.go:507-521`). | `tab new/switch/close`, popups adopted automatically, `--pin-tab` (`ab/cli/src/native/browser.rs:1909-2136`, `:2489-2521`). |
| Waits | None as a command. After a click: 400 ms sleep, then a readiness wait if the URL changed (`ub/snap.go:272-278`). | `wait` for a selector with state, text, url glob, `--fn`, `--load`, or ms (`ab/cli/src/native/actions.rs:7039-7100`). No Playwright-style auto-wait before actions (inference, `ab/cli/src/native/element.rs:425-536`). |
| JS dialogs | None. Events are discarded (`ub/cdp.go:362-364`), so an `alert` likely leaves the next `Runtime.evaluate` waiting until the 15 s timeout (inference, `ub/cdp.go:34`). | `alert` and `beforeunload` are auto-accepted. `confirm` and `prompt` are held for `dialog accept/dismiss`, and other commands fail fast with a message saying so (`ab/cli/src/native/actions.rs:1168-1225`, `:2287-2314`, `:3140-3180`, `:9001-9049`). |
| File chooser | None. | None. No `Page.setInterceptFileChooserDialog` anywhere (grep). |
| Downloads | None. | `Browser.setDownloadBehavior` and `downloadWillBegin` / `downloadProgress` tracking (`ab/cli/src/native/browser.rs:2563-2590`, `ab/cli/src/native/actions.rs:8223-8332`). |

### Foreground

This is the most important part of the comparison for winuse.

**Trusted input does not need the OS foreground.** `Input.dispatch*` is delivered to the renderer through the DevTools session, not through the OS input queue (inference). use-browser found one real catch: "when the window is occluded or the tab backgrounded, Chrome throttles the renderer and silently drops synthetic input", while `Runtime.evaluate` keeps working.

**use-browser.**
- **Good:**
  - `Emulation.setFocusEmulationEnabled {enabled:true}` on every attach fixes the dropped-input problem (`ub/cdp.go:431-437`; https://chromedevtools.github.io/devtools-protocol/tot/Emulation/#method-setFocusEmulationEnabled).
  - In extension mode, agent tabs are created with `active:false` in a "use-browser" tab group, and none of the user's tabs are adopted (`ub/extension/background.js:174-196`, `ub/cdp.go:549-555`).
- **Leaks:**
  - `tab` calls `Target.activateTarget` (`ub/commands.go:125`). Through the extension that becomes `chrome.tabs.update({active:true})` plus `chrome.windows.update({focused:true})` (`ub/extension/background.js:133-137`), which raises the window.
  - `open` in CDP mode calls `Target.createTarget` without `background:true` (`ub/cdp.go:441-454`), so the tab comes to the front in its window (inference).
  - `launch` and `clone` start a headed window (`ub/browsers.go:514-521`, `ub/clone.go:608-621`), which Windows will usually activate (inference).
  - `connect` and `ext install` open a page in the user's browser (`ub/browsers.go:468`, `ub/ext.go:539`).

**agent-browser.**
- **Leaks:**
  - `Page.bringToFront` on every tab switch (`ab/cli/src/native/browser.rs:2022-2025`), on the `bringtofront` command (`ab/cli/src/native/browser.rs:2233-2239`), and after auto-connect opens a tab (`ab/cli/src/native/actions.rs:3664-3673`).
  - `Target.createTarget` has only a `url` field, so new tabs are never background tabs (`ab/cli/src/native/cdp/types.rs:142-144`).
  - No focus emulation: a grep for `setFocusEmulationEnabled` in `ab/cli/src` finds nothing.
- **Mitigations:**
  - `--disable-backgrounding-occluded-windows`, but only on browsers it launches (`ab/cli/src/native/cdp/chrome.rs:470`).
  - A headless default on a private desktop, which keeps launched browsers off the user's screen entirely (`ab/cli/src/native/cdp/windows_process.rs:6-10`).
  - For an attached, occluded browser, nothing keeps input alive (inference).

**For winuse, CDP can upgrade Actions inside page content (inference).** Several Actions that winuse defines as always Headed could become Headless there: drag, hover, double and right clicks, keys with modifiers (`winuse/CONTEXT.md:57-62`). Trusted CDP input needs neither the pointer nor the foreground. The glossary would need a line for this.

## 5. Robustness

**Waiting and stability.**
- **use-browser** has fixed sleeps and a `readyState` poll (`ub/snap.go:272-278`, `ub/cdp.go:672-682`), and no wait command.
- **agent-browser** has explicit waits with a 25 s default kept under the 30 s IPC timeout (`ab/cli/src/native/actions.rs:882-888`, `:918-922`). Network idle means no in-flight requests for 500 ms (`ab/cli/src/native/browser.rs:2593-2662`). The skill teaches when not to use networkidle (`ab/skill-data/core/SKILL.md:200-227`).

**Error messages.** Both write errors meant for agents.
- **use-browser** errors carry the next command: "run: use-browser snap" (`ub/snap.go:172-174`), "current tab ... is gone ... run: use-browser tabs" (`ub/cdp.go:313-321`), and the ambiguous-browser message (`ub/cdp.go:270-277`).
- **agent-browser** errors explain the cause: "Element '...' is covered by <...> at its click point ... (it is often a dialog, banner, or sticky header)" (`ab/cli/src/native/element.rs:1115-1120`), and the dialog-blocking message (`ab/cli/src/native/actions.rs:3140-3180`).
- The real difference is that **use-browser clicks anyway when the point is covered** (`ub/snap.go:265-269`), while agent-browser refuses.

**Retries.**
- **use-browser** has none, apart from the extension re-attaching (`ub/extension/background.js:200-220`).
- **agent-browser** does:
  - role+name ref recovery (`ab/cli/src/native/element.rs:476-511`);
  - a check/uncheck re-verify with a JavaScript fallback (`ab/cli/src/native/interaction.rs:548-720`);
  - up to 3 launch retries (`ab/cli/src/native/cdp/chrome.rs:764-787`).

**Testing.**
- **use-browser:** four Go tests, all about clone sync and metadata (`ub/clone_test.go:27`, `:67`, `:88`, `:102`). Nothing tests CDP behaviour.
- **agent-browser:**
  - 174 e2e tests in one file, which launch real Chrome and are `#[ignore]` by default (`ab/cli/src/native/e2e_tests.rs:1-8`; count from a grep of test attributes);
  - about 1,700 test attributes across `ab/cli/src` (same grep);
  - parity tests that every documented action has a handler (`ab/cli/src/native/parity_tests.rs:1-6`);
  - CI that runs fmt, clippy and tests on Ubuntu, cross-platform tests including Windows, an e2e job, and a Windows integration job (`ab/.github/workflows/ci.yml:80-104`, `:176-296`);
  - LLM evals of skill use (`ab/evals/README.md:1-4`);
  - probe fixtures such as `ab/cli/src/native/test_fixtures/html5_drag_probe.html`.
- **winuse** already has an opt-in e2e pattern for Chromium (`winuse/docs/adr/0005-chromium-lazy-accessibility-retry.md:63-67`), which the CDP backend can reuse.

## 6. The agent-facing surface

**use-browser.**
- About 25 commands (`ub/main.go:12-98`, `:153-159`).
- Each prints one `ok` line or compact data; errors are `error: ...` on stderr with exit 1 (`ub/SKILL.md:377`, `ub/README.md:127`).
- One 20 KB `SKILL.md`.
- The skill teaches this loop (`ub/SKILL.md:87-99`):
  1. Pin a browser.
  2. Pick the lightest lifecycle.
  3. `nav`, `snap`, act by index, re-`snap`.
- It teaches batch mode for multi-step work (`ub/SKILL.md:304-317`).
- It spends many lines on connection setup and clone hygiene (`ub/SKILL.md:10-80`, `:170-237`).
- Optional `find` and `run` commands send the snapshot to a third-party API, api.typesafe.ai (`ub/find.go:38-45`, `ub/find.go:84-87`). That route should not come into winuse.

**agent-browser.**
- A very large surface: more than 30 command groups in `ab/docs/content/docs/commands.mdx`, from Core through auth vault, network, React, streaming and MCP (`ab/docs/content/docs/commands.mdx:6-731`).
- An MCP server with tool profiles (`ab/skill-data/core/SKILL.md:81-93`).
- `batch` (`ab/docs/content/docs/commands.mdx:696-705`).
- The installed `SKILL.md` is a stub that tells the agent to run `agent-browser skills get core`, so the instructions always match the binary (`ab/skills/agent-browser/SKILL.md:16-23`). The served core skill is 43 KB (`ab/skill-data/core/SKILL.md`).
- The skill teaches:
  - a named session per task (`ab/skill-data/core/SKILL.md:36-44`);
  - `snapshot -i`, act on `@eN`, re-snapshot (`ab/skill-data/core/SKILL.md:27-34`);
  - a fallback order: refs, then `find role/text/label`, then CSS (`ab/skill-data/core/SKILL.md:174-198`);
  - a section on waits (`ab/skill-data/core/SKILL.md:200-227`).

**For winuse**, which ships as a skill plus CLI and deliberately not as MCP (`winuse/docs/adr/0001-skill-and-cli-not-mcp.md:3`), use-browser's brevity is the right model. The one agent-browser idea worth taking is the "serve the skill from the binary" stub, which use-browser half-has with `use-browser skill` (`ub/main.go:187-189`).

## 7. License and reuse in Go

**agent-browser** is Apache-2.0, "Copyright 2025 Vercel Inc." (`ab/LICENSE:1-3`; `ab/cli/Cargo.toml:6`).
- winuse is MIT (`winuse/LICENSE:1-3`). Porting Apache-2.0 code into it is allowed if we:
  - keep the license text and copyright notice for the derived parts;
  - state that we changed them.

  agent-browser has no NOTICE file. winuse's `third_party/` folder is the natural home for that text (inference about where to put it).
- It is Rust, so in practice we port algorithms and JavaScript snippets, such as `BLOCKER_AT_JS` and `ACTIONABILITY_JS` (`ab/cli/src/native/element.rs:1003-1047`) and the cursor-interactive walk. We don't link code.
- The bundled `axe.min.js` is MPL-2.0 (`ab/cli/src/native/a11y/LICENSE-axe-core.txt:1`) and is not needed.

**use-browser** has no LICENSE file in the repo (file listing at commit 14aac7d), and GitHub reports no license for it (`gh api repos/hoangvu12/use-browser`). Strictly, that means all rights reserved. It is the same author's code, though, so it can be moved into MIT winuse by its owner. It would be worth adding a LICENSE to use-browser anyway.

## What winuse's CDP backend should take from each

### From use-browser (code to lift, in Go)

1. **The transport.** The zero-dependency WebSocket client (`ub/ws.go`) and the `msgConn` abstraction that lets a WebSocket or the extension bridge sit under one CDP client (`ub/ext.go:86-108`). Rework the client so it routes events instead of dropping them (`ub/cdp.go:362-364`); dialogs, downloads and auto-attach all need events.
2. **Discovery.**
   - The ordered discovery in `discover()` (`ub/cdp.go:154-241`).
   - The `DevToolsActivePort` reader (`ub/cdp.go:78-92`).
   - Port-owner checks through `netstat` / `tasklist` (`ub/browsers.go:686-743`).
   - The refusal to guess between two debuggable browsers (`ub/cdp.go:268-277`).
3. **The browser table.** The Windows browser and profile tables, including Edge, Vivaldi and Opera (`ub/browsers.go:32-70`, `:273-327`).
4. **The clone flow**, all of it (`ub/clone.go`):
   - the persistent, incrementally synced, pruned copy;
   - the `Local State` copy;
   - cache skipping;
   - detecting the cookie lock;
   - a clean `--close-source` with session restore.
5. **The extension bridge** (`ub/ext.go`, `ub/extension/`). It is the only route to the user's real, running browser with no port, no copy and no prompt, and it fits winuse's "one Window, one view" model best: the Window the user sees is the one CDP drives. Keep:
   - background tabs;
   - the tab group;
   - Cancel-to-pause;
   - the idle detach;
   - self-update;
   - `ext status` diagnostics.
6. **Focus emulation on every attach** (`ub/cdp.go:431-437`), the key to Headless page input.
7. **Stable short tab ids, and per-call tab targeting** without shared state (`ub/cdp.go:565-596`, `:507-521`).
8. **Output and error style:** one `ok` line, errors that name the next command, and BOM tolerance for PowerShell.

### From agent-browser (ideas to port)

1. **A persistent connection**, held in winuse's helper rather than a separate daemon. This gives one Chrome 144 approval per helper lifetime, cheap repeated actions, and event subscriptions (`ab/cli/src/native/actions.rs:625-626`, `ab/cli/src/native/cdp/client.rs:48-50`).
2. **The Chrome 144 approval handling.** Hold the handshake for about 20 s, and never open a second prompt while one is pending (`ab/cli/src/native/cdp/chrome.rs:1175-1182`, `:1243-1248`). After attach, resume with `Runtime.runIfWaitingForDebugger` (`ab/docs/content/docs/changelog.mdx:740`).
3. **An AX-tree snapshot.** `Accessibility.getFullAXTree`, plus a cursor-pointer pass for role-less clickables (`ab/cli/src/native/snapshot.rs:327-334`, `:771-874`). Render it into winuse's Window State grammar so the frame (UIA) and the page (CDP) read as one tree.
4. **Refs as `backendNodeId` plus frame/session**, stored in the helper's live-Element store (`ab/cli/src/native/element.rs:8-31`). Keep winuse's "valid for one Window State" rule. Durable numbering is optional.
5. **Flattened auto-attach for cross-origin iframes**, with per-frame sessions (`ab/cli/src/native/browser.rs:1043-1059`, `ab/cli/src/native/element.rs:842-859`). Extend the extension bridge to pass child sessions through (`ub/extension/background.js:240-242`).
6. **Click hygiene:**
   - `DOM.scrollIntoViewIfNeeded`;
   - the box-model center;
   - `mouseMoved` before the press;
   - covered, disabled or outside the viewport means refuse with the blocker named, not warn (`ab/cli/src/native/element.rs:1003-1047`, `:1115-1127`; `ab/cli/src/native/interaction.rs:1056-1165`);
   - racing each mouse event against `Page.javascriptDialogOpening` (`ab/cli/src/native/interaction.rs:1009-1054`).
7. **The missing Actions**, all as trusted CDP input:
   - hover (`mouseMoved`);
   - drag (press, moves, release);
   - wheel scrolling (`mouseWheel`), so element scrolling stops being JS;
   - select;
   - check;
   - upload via `DOM.setFileInputFiles`.

   With focus emulation these can be Headless Actions in page content.
8. **JavaScript dialog handling.** Subscribe to `Page.javascriptDialogOpening`, and fail other commands fast with a message that names `dialog accept/dismiss` (`ab/cli/src/native/actions.rs:1168-1225`, `:3140-3180`). Decide deliberately whether `alert` is auto-accepted. A tab-modal dialog is page content, so it belongs on the CDP side of the routing (inference).
9. **Downloads** through `Browser.setDownloadBehavior` and the download events (`ab/cli/src/native/browser.rs:2563-2590`) on launched and cloned browsers. Note that browser-level methods are not available through the extension (`ub/extension/background.js:146-149`).
10. **Waits:** selector, text, URL and load state, with a single default timeout kept under winuse's request budget (`ab/cli/src/native/actions.rs:7039-7100`, `:882-888`).
11. **Testing:** an opt-in e2e suite against real Chrome and Edge with probe pages for drag, pointer capture, dialogs and iframes (`ab/cli/src/native/e2e_tests.rs:1-8`, `ab/cli/src/native/test_fixtures/`). Also a parity test that every documented command has a handler (`ab/cli/src/native/parity_tests.rs:1-6`).

### Gaps neither solves (to design ourselves)

1. **Never take the foreground.**
   - Never call `Page.bringToFront` or `Target.activateTarget`.
   - Create tabs with `Target.createTarget {background:true}` (https://chromedevtools.github.io/devtools-protocol/tot/Target/#method-createTarget).
   - Treat "activate a tab" as a Headed Action.
   - Both tools break this rule today (`ub/commands.go:125`, `ab/cli/src/native/browser.rs:2022-2025`).
2. **File chooser interception.** Use `Page.setInterceptFileChooserDialog` and answer with `DOM.setFileInputFiles`, so a click on a file input never opens a native Windows dialog (inference: without interception, that dialog would appear as a new Window that UIA would have to drive).
3. **Mapping a winuse Window (HWND) to its CDP target(s).** Neither tool needs it, so neither does it: agent-browser calls `Browser.getWindowForTarget` only to resize (`ab/cli/src/native/browser.rs:2163-2193`). Automatic routing between UIA and CDP inside one Window depends on it.
4. **Routing between UIA and CDP.** Per the prior report, agent-desktop leaves this seam to the agent (`.scratch/browser-merge/agent-desktop-research.md:27`). Neither tool here helps.
