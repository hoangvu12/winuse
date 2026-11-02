# How vercel-labs/agent-desktop handles browsers, and what it means for merging use-browser into winuse

Researched 2026-11-02. Read only: nothing was run, launched or driven.

## Sources and citation format

- **agent-desktop** at commit `9eb9b29` ("Video remove (#421)", 2026-10-31). Paths written `ad/<path>:<lines>` are relative to that repo root, so `ad/README.md:12` is https://github.com/vercel-labs/agent-desktop/blob/9eb9b29/README.md#L12.
- **Upstream check.** `git fetch` and `git ls-remote` on 2026-11-02 show `main` still at `9eb9b29`, so no commits landed after it. Open pull requests and issues filed after it are listed in "Upstream activity after 9eb9b29" below.
- **winuse** paths are written `winuse/<path>:<lines>`, relative to `C:\Users\ADMIN\Desktop\nguyenvu\winuse`.
- **use-browser** paths are written `use-browser/<path>:<lines>`, relative to `C:\Users\ADMIN\.claude\skills\use-browser`. The `SKILL.md` there is dated 2026-11-01 and documents an extension mode (`ext install`, `ext status`) that the `.go` files there (dated 2026-09-06) do not implement: `main.go` has no `ext` command (`use-browser/main.go:143-235`). The Go source I read is older than the skill text.
- Chrome and CDP facts come from developer.chrome.com and chromedevtools.github.io; the URLs are inline.
- **Inferred** marks a conclusion I drew from what I read rather than something a source states. **Absent** means I searched for it and found nothing.

## Summary

1. **agent-desktop does not drive web pages, and it has no CDP client.** It reads Chromium and Electron windows only through the OS accessibility tree (UIA on Windows). For web content it sends the agent to a separate tool, agent-browser. AGENTS.md says it plainly: "It does not automate web pages (use agent-browser)" (`ad/AGENTS.md:7-8`). The docs repeat it (`ad/apps/docs/content/docs/index.mdx:19`).
2. **Its only CDP feature is `launch --cdp`.** That flag starts an app fresh with `--remote-debugging-port=<port>` and `--remote-debugging-address=127.0.0.1`. It then polls `/json/version` until the endpoint answers, checks that the launched process owns the listening port, and returns `data.cdp` (port, http endpoint, websocket URL, product) (`ad/crates/core/src/commands/launch.rs:27-51,185-223`). The docs say: "agent-desktop never speaks CDP itself; it opens and verifies the port, and keeps the native UI" (`ad/apps/docs/content/docs/guides/chromium-and-electron.mdx:7`).
3. **The agent chooses the path; nothing switches automatically.** `launch` on a Chromium app returns `renderer: "chromium"` with a suggestion to relaunch with `--cdp`. A verified endpoint comes back with a suggestion to run `agent-browser connect <port>` (`ad/crates/core/src/commands/launch.rs:67-92`). The skill splits the work: web contents go to agent-browser, and native menus, dialogs, windows, notifications and screenshots stay on agent-desktop (`ad/skills/agent-desktop/references/commands-system.md:46-48`).
4. **`--cdp` refuses a running app and does nothing about Chrome 136.** A running app gets `cdp_requires_fresh_launch`, because quitting it would lose the user's state (`ad/crates/core/src/commands/launch.rs:123-159`). The code does not add `--user-data-dir` or detect the Chrome 136 default-profile rule (absent). The docs example passes `--user-data-dir` by hand without explaining why (`ad/apps/docs/content/docs/guides/chromium-and-electron.mdx:23-24`). use-browser's `clone` and `launch` already handle this better.
5. **The two paths have unrelated element identities.** agent-desktop refs are snapshot-qualified (`@s8f3k2p9:e8`) and persisted to disk. agent-browser refs are bare (`@e3`) (`ad/apps/docs/content/docs/guides/chromium-and-electron.mdx:58-64`), and agent-desktop rejects bare refs (`ad/CONCEPTS.md:95`). Nothing maps one onto the other.
6. **Most of its Chromium work goes into making the UIA path usable.** On Windows it detects Chromium by the root window class `Chrome_WidgetWin_1`. It waits for the renderer's lazy accessibility build to settle, retrying with backoff. It skips the depth of anonymous web wrapper groups, and it resolves unnamed web elements by unique positive-area bounds (`ad/crates/windows/src/tree/chromium.rs:9-33`, `ad/crates/windows/src/tree/observe.rs:46-117`, `ad/CONCEPTS.md:37-40,113-116`). winuse ADR 0005 covers the same ground with a simpler single retry.
7. **The address bar and navigation are handled only as native UI.** There is no navigate or URL command (absent from `ad/src/cli/mod.rs`). The docs find the omnibox as a UIA `textfield` named "Address and search bar" (`ad/apps/docs/content/docs/guides/chromium-and-electron.mdx:74-78`).
8. **Electron gets the same treatment as Chrome.** `RendererKind::Chromium` covers Electron and CEF (`ad/crates/core/src/tree/renderer_kind.rs:3-6`), and `--cdp` targets Electron apps such as Slack, VS Code and Obsidian (`ad/skills/agent-desktop/references/commands-system.md:30`).
9. **The cursor overlay, interaction lease and policies apply only to agent-desktop's own actions.** None of them cover what agent-browser does over CDP (inferred, since agent-desktop never sends CDP). agent-desktop has no user stop key (absent). winuse's Esc stop has no counterpart there.
10. **Upstream is moving toward headless input for background Chromium windows, not toward CDP.** Open PR #400 types into background Chrome, Edge and Electron windows through window messages, without the foreground. The maintainer closed a private-API background-input PR, saying "the plan is to stay headless through accessibility" (issue/PR #227 comment).

For the merge, the main point is this. agent-desktop shows a working division of labour (UIA for browser chrome and native UI, CDP for page content), but it leaves the seam to the agent, and that seam is what we want to remove. Its useful parts are the endpoint verification, the Chromium UIA settle and wrapper handling, and the honest "native UI stays on UIA" list. Nothing in it shows how to unify identity, cursor or navigation across the two paths. We have to design that ourselves.

## 1. Architecture and process model

**Crates.** It is a Rust workspace of five crates plus the binary (`ad/Cargo.toml:1-3`, `ad/AGENTS.md:84-99`):

- `crates/core` (`agent-desktop-core`): the `PlatformAdapter` trait, shared types, and one file per command under `commands/`.
- `crates/macos`, `crates/windows` and `crates/linux`: the AX, UI Automation and AT-SPI adapters.
- `crates/ffi`: a C ABI `cdylib`, so other programs can call it in process (`ad/README.md:141-153`).
- `src/`: the `agent-desktop` binary (CLI, dispatch, batch, policy, macOS command host).

Core may not depend on a platform crate, and CI checks this. The platform is chosen at compile time in `build_adapter()` (`ad/AGENTS.md:101-114`). Dispatch is a direct `match`, with no command registry, and batch reuses the same dispatcher (`ad/AGENTS.md:116-118`).

**Process model.**

- **CLI, one process per command.** On Windows and Linux every invocation is a fresh process that prints one JSON envelope (`ad/AGENTS.md:188-203`). State such as refmaps and sessions lives on disk under `~/.agent-desktop` (`ad/CONCEPTS.md:147-150`, `ad/AGENTS.md:227-230`).
- **macOS command host.** On macOS only, CLI calls are forwarded to a private per-namespace host that keeps native AX objects alive and exits after 15 idle minutes (`ad/README.md:14`). The host module is compiled only for macOS (`ad/src/main.rs:6-7,63-70,134-144`).
- **No general daemon.** `key-down`, `key-up`, `mouse-down` and `mouse-up` "refuse to run until a daemon can release a held key or button" (`ad/README.md:110`).
- **Helpers.** The Windows cursor overlay is a detached renderer process. It is the same `agent-desktop` image, started with no inherited handles and driven over a named pipe (`ad/crates/windows/src/system/cursor_overlay/spawn.rs:201-240`, `ad/src/main.rs:46-53`, `ad/skills/agent-desktop-windows/references/permissions-and-elevation.md:90-92`). macOS ships `agent-desktop-macos-helper` and Linux a clipboard-owner helper (`ad/crates/macos/src/bin/`, `ad/crates/linux/src/bin/`).

**Platforms.** macOS, Windows 10 1809+ and Server 2019+ (x64 and ARM64), and Linux (AT-SPI, X11, partly Wayland) (`ad/README.md:124-135`, `ad/skills/agent-desktop-windows/SKILL.md:22-26`).

**Command surface.** It has 60 command names, 56 of them operational (`ad/README.md:108-122`).

## 2. Observing and acting

### Observation

- **Snapshot and refs.** A snapshot is JSON, and every actionable or drillable node gets a ref qualified by the snapshot id, such as `@s8f3k2p9:e8`. A bare `@e8` is rejected (`ad/README.md:12,73-74`, `ad/CONCEPTS.md:90-96`). An element gets a ref when its role is interactive or it advertises a primary action. SetFocus, RightClick and ScrollTo alone do not count, "because web runtimes advertise them on nearly every node" (`ad/AGENTS.md:221-223`).
- **Persistence and resolution.** Refs persist in a RefMap with identity evidence: pid, role, path, stable text, native id and a bounds hash (`ad/AGENTS.md:224-226`). Each action re-identifies its target by strict, graded resolution, and fails with `STALE_REF` or `AMBIGUOUS_TARGET` rather than guessing (`ad/CONCEPTS.md:108-116`).
- **Dense apps.** A skeleton snapshot plus a `--root` drill-down handles them (`ad/README.md:77-78`, `ad/skills/agent-desktop/SKILL.md:34-46`).
- **Screenshots.** `screenshot` is a separate observation command (`ad/README.md:114`). Screenshots are not the main loop: "no pixel matching" (`ad/README.md:12`).

### Acting

- **Headless by default.** A ref action calls accessibility APIs only: no focus, no pointer, no synthesized keys, no clipboard. If no semantic path exists, it fails closed and never falls back to physical input on its own (`ad/CONCEPTS.md:223-236`, `ad/skills/agent-desktop/SKILL.md:67-69`, `ad/apps/docs/content/docs/guides/headed-mode.mdx:9-13`).
- **`--headed`.** This global flag permits physical input. Core first focuses the exact source window, not just the app (`ad/CONCEPTS.md:226,249-252`).
- **Action chain.** Each action walks an ordered ladder of rungs, and every step records its mechanism (`semantic_api` or `physical_synthetic`) and its delivery evidence (`ad/CONCEPTS.md:238-241`, `ad/crates/windows/src/actions/chain.rs:1-15`).
- **Delivery semantics.** Every error says whether input may have landed and whether a retry is safe (`ad/CONCEPTS.md:198-201`, `ad/skills/agent-desktop/SKILL.md:57-65`).

### Windows backend

- **Click.** Click runs `InvokePattern.Invoke`, then `LegacyIAccessible.DoDefaultAction`. Set-value, toggle, expand and select have their own modules (`ad/crates/windows/src/actions/dispatch.rs:1-9,223-257`).
- **Focus.** UIA `SetFocus` moves the desktop foreground (findings A3-4 and A19-5, `ad/probes/windows/FINDINGS.md:81,314`), so `focus` needs `--headed` (`ad/crates/windows/src/actions/focus.rs:1-6`).
- **Headed-only actions.** `right-click`, `double-click`, `triple-click`, `type` and `focus` need `--headed`. `type` uses `SendInput` because UIA has no insert-at-caret write (`ad/skills/agent-desktop-windows/SKILL.md:35-37,65-69,93`, `ad/CONCEPTS.md:226`).
- **Background windows.** A window behind another window can be driven. A minimized one cannot (`ad/skills/agent-desktop-windows/SKILL.md:53-56`).
- **Foreground gate.** Before ref-addressed `SendInput`, the target must still be foreground and, for keys, still hold keyboard focus (`ad/CONCEPTS.md:261-264`).
- **Delivery evidence.** `SendInput`'s return value is never treated as delivery evidence (`ad/CONCEPTS.md:201`).
- **Posted keys.** Findings row A4-4 measured `PostMessage WM_KEYDOWN` as "DEAD for Chromium" (`ad/probes/windows/FINDINGS.md:91`). Open PR #400 gets around this with `SendMessageTimeoutW` plus fake activation messages (see "Upstream activity after 9eb9b29").
- **`AutoSetFocus`.** I found no mention of `IUIAutomation2::AutoSetFocus` in the Windows crate (absent from a grep of `ad/crates/windows/src`). winuse ADR 0010 found it must be set to FALSE so that pattern calls do not raise the window (`winuse/docs/adr/0010-headless-element-actions.md:50-54`). How agent-desktop avoids that side effect, if it does, is not visible in the code I read.

## 3. Browsers and web content

### 3.1 CDP, extension, or accessibility tree only?

- **Observation and action use the OS accessibility tree only.** That is the AX tree on macOS, UIA on Windows and AT-SPI on Linux (`ad/README.md:3`, `ad/AGENTS.md:5-8`).
- **CDP appears only as the `launch --cdp` port opener and verifier.** It probes `/json/version` with a hand-written HTTP read, because "Chromium's DevTools server answers and then holds the connection open, ignoring `Connection: close`" (`ad/crates/core/src/system/cdp_endpoint.rs:46-121`). It accepts only a `ws://127.0.0.1:<port>/...` debugger URL (`ad/crates/core/src/system/cdp_endpoint.rs:167-187`).
- **No browser extension (absent).** The only "extension" hits in the repo are unrelated.
- **Hand-written CDP is discouraged.** The skill lists "Hand-rolled CDP" as an anti-pattern: "Use a real CDP client. Raw WebSocket scripts and app-internal APIs break silently and verify nothing" (`ad/skills/agent-desktop/references/workflows.md:205`). The launch suggestion says "Do not hand-roll raw CDP or call app-internal APIs" (`ad/crates/core/src/commands/launch.rs:73-79`).
- **The design is deliberate.** PR #129, which added `--cdp`, says: "No coupling: agent-desktop never invokes agent-browser; the response carries guidance the calling agent reads" (https://github.com/vercel-labs/agent-desktop/pull/129).

### 3.2 Is a browser tab treated differently from a native window?

On the accessibility path, no. A browser window is just a window whose root has class `Chrome_WidgetWin_1`. That detection changes only how the tree is read:

- **Wrapper skip.** Anonymous UIA `Group`, `Custom` and `Pane` nodes with no name, value, AutomationId or action consume no logical depth. On Windows this applies only when Chromium provenance is detected, because native stacks are full of such containers (`ad/CONCEPTS.md:37-40`, `ad/crates/windows/src/tree/walk/wrapper.rs:32-52`, `ad/crates/windows/src/tree/walk/walker_source.rs:165-209,269`).
- **Settle.** A full-depth walk of a Chromium root that finds 32 nodes or fewer is treated as the "pre-activation shell". Core's loop then retries with a 25 to 400 ms backoff until the deadline (`ad/crates/windows/src/tree/chromium.rs:15-19,43-93`, `ad/crates/windows/src/tree/observe.rs:101-116,125-136`, `ad/crates/core/src/tree/renderer_accessibility.rs:6-42`). On Windows, "activation" is just connecting plus a short sleep: "connecting to UI Automation already triggered the renderer's asynchronous accessibility build" (`ad/crates/windows/src/system/adapter.rs:68-87`). macOS instead sets `AXManualAccessibility` or `AXEnhancedUserInterface` (`ad/crates/macos/src/tree/renderer_probe.rs:1-65`).
- **Measured settle cost.** First contact returns about 1/13 of the settled tree, and a cold launch takes 10 to 25 seconds to settle (`ad/probes/windows/FINDINGS.md:59`, `ad/skills/agent-desktop-windows/references/chromium-and-electron.md:14-27`). Chrome 138+ exposes UIA natively with no flag (`ad/skills/agent-desktop-windows/references/chromium-and-electron.md:9-12`; confirmed by Chrome at https://developer.chrome.com/blog/windows-uia-support-update).
- **Covered windows.** A fully covered Chromium window can stay at its first-contact node count indefinitely (`ad/skills/agent-desktop-windows/references/chromium-and-electron.md:40-45`).
- **Chromium modals.** A Chromium modal is reported as a `sheet` surface (`ad/crates/windows/src/tree/surfaces.rs:124-139`). DOM context menus are detected as the `menu` surface (`ad/skills/agent-desktop-windows/references/shell-and-overlay.md:148-154`).

There is no concept of a "tab" anywhere in agent-desktop (absent). Tabs exist only in agent-browser.

### 3.3 How the path is chosen

The agent chooses, guided by text in the responses and the skill:

- **After `launch`.** A Chromium app gets `renderer: "chromium"` and the suggestion "for web-content work, run close-app and then launch --cdp" (`ad/crates/core/src/commands/launch.rs:83-90`). The skill calls these "hints. The accessibility path still works. A missing `renderer` never proves the app is native" (`ad/skills/agent-desktop/references/commands-system.md:26`).
- **When to use `--cdp`.** The skill says to use it for apps "whose web contents are dense or slow to walk (Slack, VS Code, Discord, Obsidian, Notion)" (`ad/skills/agent-desktop/references/commands-system.md:30`). The accessibility path "is the default. It is the only path for an app that is already running and for native menus, dialogs and windows" (`ad/skills/agent-desktop/references/commands-observation.md:124-126`).
- **Fallback.** "If no CDP client exists, skip `--cdp` and use the skeleton workflow on the accessibility path. It always works" (`ad/skills/agent-desktop/references/workflows.md:158`).

Nothing routes automatically between UIA and CDP (absent).

### 3.4 How it connects to the browser

- **Launch flags.** `--cdp [PORT]` appends `--remote-debugging-port=<p>` and `--remote-debugging-address=127.0.0.1`. Port 0, or no port, picks a free one (`ad/crates/core/src/commands/launch.rs:27-39,168-183`, `ad/src/cli_args/system.rs:55-63`).
- **Owned switches.** It refuses a caller `--arg` that sets `--remote-debugging-port`, `--remote-debugging-pipe`, `--remote-debugging-address` or `--remote-allow-origins` (`cdp_switch_conflict`) (`ad/crates/core/src/commands/launch.rs:94-121`).
- **Fresh launch only.** It refuses an app that is already running (`cdp_requires_fresh_launch`) and never quits it for you (`ad/crates/core/src/commands/launch.rs:123-159`).
- **Verification.** It polls `/json/version` until the endpoint answers. It then checks that the launched process instance owns the TCP listener (`process_owns_tcp_listener`); otherwise it returns `cdp_listener_unattributed` (`ad/crates/core/src/commands/launch.rs:185-223`, `ad/crates/core/src/system/cdp_endpoint.rs:189-201`). The Windows implementation lives in `ad/crates/windows/src/system/tcp_listener.rs`.
- **Profiles and Chrome 136+.** The code never adds `--user-data-dir` and does not detect the default-profile case (absent). The guide's Chrome example passes `--arg=--user-data-dir=/tmp/chrome-profile` by hand (`ad/apps/docs/content/docs/guides/chromium-and-electron.mdx:23-24`). Since Chrome 136, `--remote-debugging-port` and `--remote-debugging-pipe` "will no longer be respected if attempting to debug the default Chrome data directory" and "must now be accompanied by the `--user-data-dir` switch to point to a non-standard directory" (https://developer.chrome.com/blog/remote-debugging-port, 2025-03-17).
- **Inferred consequence.** A `launch chrome --cdp` without that `--arg` on Chrome 136+ would start Chrome with no endpoint and end in `cdp_endpoint_unavailable` (the error is described at `ad/apps/docs/content/docs/guides/chromium-and-electron.mdx:96`). Electron apps are not affected, because the restriction is Chrome's own.
- **Attaching to the user's running browser.** It has no way to do this (absent). The Chrome 144+ `chrome://inspect/#remote-debugging` toggle, which shows a permission dialog per connection request (https://developer.chrome.com/blog/chrome-devtools-mcp-debug-your-browser-session), is not mentioned anywhere.
- **Port exposure.** "While the port is open, any local process of the same user can control the app's web contents" (`ad/skills/agent-desktop/references/commands-system.md:48`). `close-app` ends it (`ad/apps/docs/content/docs/guides/chromium-and-electron.mdx:80-86`).

For comparison, agent-browser itself can launch its own Chromium, `connect` to a port, `--auto-connect` to a running Chrome, copy a Chrome profile with `--profile`, and load extensions (https://github.com/vercel-labs/agent-browser README, "Options" and "Chrome Profile Reuse"). use-browser covers the same ground with `launch`, `clone`, `connect` and the extension (`use-browser/SKILL.md:39-42`, `use-browser/cdp.go:153-230`).

### 3.5 Element identity across the two paths

Identity is not unified:

- **agent-desktop refs.** `@<snapshot_id>:eN`, persisted to disk and re-resolved strictly from identity evidence (`ad/CONCEPTS.md:90-116`).
- **agent-browser refs.** Bare `@eN` from its own snapshot (`ad/apps/docs/content/docs/guides/chromium-and-electron.mdx:58-64`), and "Surviving DOM elements keep their refs across snapshots" (agent-browser README, "Snapshot Options").
- **Why they cannot collide (inferred).** The two forms differ in syntax, and agent-desktop rejects the bare form with `INVALID_ARGS` (`ad/CONCEPTS.md:95`). That also means no agent-desktop command can act on an agent-browser ref.
- **Unnamed web content on the UIA path.** An element with no accessible name resolves only by a unique positive-area bounds match with the same role. Zero-area or ambiguous geometry needs coordinates or a CDP client (`ad/skills/agent-desktop-windows/references/chromium-and-electron.md:58-69`, `ad/CONCEPTS.md:113-116`).

### 3.6 Address bar and navigation

There is no `navigate`, `open-url` or tab command (absent from `ad/src/cli/mod.rs`; the only `--cdp` mention is the `launch` help at line 106). The omnibox is plain native UI. The guide finds it with `find --app chrome --role textfield --first` and reports `"name": "Address and search bar"` with the current URL as its value (`ad/apps/docs/content/docs/guides/chromium-and-electron.mdx:69-78`). Typing into it needs `--headed` on Windows today (`ad/skills/agent-desktop-windows/SKILL.md:65-69`). Open PR #400 reports that headless `type` into the omnibox "navigates the background window" (https://github.com/vercel-labs/agent-desktop/pull/400).

### 3.7 Electron

Electron is handled the same way as Chrome:

- **Shared detection.** `RendererKind::Chromium` "covers both Electron and CEF" (`ad/crates/core/src/tree/renderer_kind.rs:3-6`), and on Windows the same `Chrome_WidgetWin_1` class detects it (`ad/crates/windows/src/system/launch.rs:91-101`).
- **Flags.** "An Electron app needs no extra arguments" for `--cdp` (`ad/apps/docs/content/docs/guides/chromium-and-electron.mdx:27`).
- **Probes.** The Windows probe corpus measured Obsidian, which bundles Electron 39 and Chromium 142 (`ad/probes/windows/FINDINGS.md:47,58-59`).
- **Workflow.** The workflow "Chromium app: two tools" uses Slack: close-app, wait for exit, `launch --cdp 9222`, then `agent-browser connect 9222` (`ad/skills/agent-desktop/references/workflows.md:146-158`).

## 4. The agent-facing surface

- **Skills ship in the binary.** `agent-desktop skills get desktop` and `skills get platform` print them, matched to the binary's version (`ad/README.md:85-91`, `ad/AGENTS.md:47-48`).
- **Core skill.** It covers the loop, refs, delivery and retry, and headless versus headed. It says "Never switch to headed mode to recover from a headless failure" (`ad/skills/agent-desktop/SKILL.md:32-69`). It never mentions the browser. Browser guidance sits in references: `commands-observation.md:124-126`, `commands-system.md:26-48` and `workflows.md:146-158,205`.
- **Windows skill.** Its first-contact list says only "Chromium and Electron apps read thin before they settle" (`ad/skills/agent-desktop-windows/SKILL.md:59-60`). Details are in `references/chromium-and-electron.md`.
- **Guidance in responses.** The response itself teaches the mode at the moment of use: the `suggestion` field on `launch` (`ad/crates/core/src/commands/launch.rs:67-92`), described in PR #129 as "the response teaches the flow at the moment of use".
- **Errors.** Every CDP failure is a structured `details.kind` with a recovery row: `cdp_requires_fresh_launch`, `cdp_endpoint_unavailable`, `cdp_port_in_use`, `cdp_switch_conflict` (`ad/skills/agent-desktop/references/commands-system.md:39-44`, `ad/apps/docs/content/docs/guides/chromium-and-electron.mdx:88-100`).
- **How the agent is told which tool to use.** Through a table: web contents go to agent-browser over CDP; native menu bar, file dialogs, window management, notifications and screenshots go to agent-desktop; an app you did not launch goes to agent-desktop only (`ad/apps/docs/content/docs/guides/chromium-and-electron.mdx:9-18`).

## 5. Other pieces relevant to a seamless merge

### Cursor overlay

- **Configuration.** The overlay is presentation-only and is configured per session (`session start --cursor` or `cursor-overlay enable`). The renderer acknowledges arrival before the action dispatches (`ad/CONCEPTS.md:228-229`).
- **Headless only on Windows.** On Windows it "stays suppressed for headed input and draws only for headless semantic actions" (`ad/CONCEPTS.md:229`, `ad/skills/agent-desktop-windows/SKILL.md:94`).
- **Cost.** About 355 ms per action (`ad/skills/agent-desktop-windows/references/shell-and-overlay.md:104-106`).
- **Multi-agent.** `--multi-agent` gives each `(session-id, agent-id)` its own cursor. A headless action moves its cursor before it takes the lease, so several cursors travel at once (`ad/CONCEPTS.md:231`).
- **Web actions (inferred).** The overlay never draws for web actions, because agent-browser's CDP actions never pass through agent-desktop's dispatch.
- **winuse's version.** winuse's Agent Cursor already takes "screen rectangles (physical pixels) and label text only, never UI Automation types, so any driver (the CDP browser tool winuse will merge with) can show the same cursor" (`winuse/internal/win/cursor.go:9-13`; API `Travel(origin, target Rect, label)`, `Click(target Rect)`, `HideWhile` at lines 199-262; spec story 13 at `winuse/.scratch/headless/spec.md:33`).

### Permissions and safety

- **Integrity, not grants.** Windows has no permission grant. Integrity level is the boundary: reads cross UIPI and writes do not, which surfaces as `PERM_DENIED` from a token comparison (`ad/skills/agent-desktop-windows/references/permissions-and-elevation.md:1-42`).
- **Blocked combos.** Combos such as `alt+f4`, `win+l` and `alt+tab` are blocked unless `--force` is passed (`ad/skills/agent-desktop-windows/references/permissions-and-elevation.md:53-71`).
- **Protected processes.** A list of critical processes cannot be closed (`ad/CONCEPTS.md:173-176`).
- **Secure fields.** Password fields never echo their values (`ad/CONCEPTS.md:208-211`).
- **CDP port exposure.** Loopback only and verified by ownership, but readable by any same-user process. The docs advise "request `--cdp` only for the steps that need it" (`ad/apps/docs/content/docs/guides/chromium-and-electron.mdx:86`).

### Interaction lease

The lease gives machine-wide exclusivity over desktop input during dispatch only, never while waiting (`ad/CONCEPTS.md:213-216`). On Windows it is an exclusive `CreateFileW` open of a lock file derived from the user's token. A parent can hand it to a child through `AGENT_DESKTOP_INTERACTION_LEASE_HANDLE` (`ad/crates/windows/src/system/interaction_lease/mod.rs:1-45`, `ad/skills/agent-desktop-windows/references/permissions-and-elevation.md:81-88`).

Even `launch` and renderer activation take the lease (`ad/crates/core/src/commands/launch.rs:44`, `ad/crates/core/src/tree/renderer_accessibility.rs:59-63`). Inferred: agent-browser's CDP input does not take it, so the lease does not order CDP actions against desktop actions.

### Stop keys

There are none (absent). A grep for hotkey, low-level hook (`WH_KEYBOARD_LL`, `SetWindowsHookEx`), stop key or kill switch across `crates/`, `src/` and the docs finds no user stop mechanism. The only `GetAsyncKeyState` use is the modifier-release sweep (`ad/crates/windows/src/input/keyboard/keyboard_send.rs:1-12`). winuse's Esc stop (`winuse/CONTEXT.md:65-67`, `winuse/docs/adr/0007-esc-stop-hook.md`) has no counterpart to port.

## Upstream activity after 9eb9b29

`main` has no newer commits. Open items that bear on this decision:

- **PR #400 (open, 2026-10-31), "feat(windows): type into background Chromium and Electron windows headless".** It sends `WM_NCACTIVATE` and `WM_ACTIVATE`, a click message at the field centre, and then `VK_PACKET`/`WM_CHAR` through `SendMessageTimeoutW`, all to a background `Chrome_WidgetWin_1` window. It reports that `GetForegroundWindow` is unchanged and that page events carry `isTrusted: true`. It works for Chrome 156 and Edge 156, including the omnibox. VS Code failed because a covered editor reports `IsOffscreen`. It also documents that Edge reparents `Chrome_RenderWidgetHostHWND` under a hidden `Chrome_WidgetWin_0` while inactive. (https://github.com/vercel-labs/agent-desktop/pull/400)
- **PR #438 (open, 2026-11-01), "press the element at a point in a background app".** It hit-tests at a point and presses through accessibility, on macOS only. (https://github.com/vercel-labs/agent-desktop/pull/438)
- **Issue #463 (open, 2026-11-01).** A dense Chrome tab overflows the 1 MB RefMap cap with `INTERNAL`. PR #466 proposes returning `TREE_TOO_LARGE` instead. (https://github.com/vercel-labs/agent-desktop/issues/463)
- **Issue #465 (open).** Arc fails renderer activation on macOS while Chrome works.
- **PR #227 (closed).** The maintainer declined reverse-engineered background pointer input: "the plan is to stay headless through accessibility" (https://github.com/vercel-labs/agent-desktop/pull/227).

## Implications for merging use-browser into winuse

What agent-desktop gives us as evidence:

1. A tool can keep native UI on UIA and web content on CDP.
2. Chromium's UIA tree is usable but slow and thin on first contact, and dense pages get expensive.
3. Agent-desktop never resolved the seam: two tools, two ref namespaces, two cursors (one of them missing), and the agent decides.

The options below are ordered from least to most merged.

### Option A: copy agent-desktop and delegate

winuse stays UIA-only, and use-browser stays a separate CLI and skill. winuse might add a `launch --cdp` style helper that hands off a verified port, and the skills would cross-reference each other.

- **For.** Least work. Matches a design the agent-desktop maintainers chose on purpose. No new failure modes inside winuse.
- **Against.** This is the seam we want to remove. The agent juggles two index schemes. The Agent Cursor, Esc stop and banner do not cover browser actions (agent-desktop has the same gap, section 5).
- **Weaker than what we already have.** agent-desktop's `--cdp` cannot attach to a running browser and does nothing about Chrome 136 (section 3.4), while use-browser's `clone`, `connect`, `launch` and extension modes already can.

### Option B: one binary, two explicit backends

use-browser's commands move into winuse as a command family, either a `winuse web ...` namespace or a `--web` target form. They share the helper, Agent Cursor, Esc stop, sessions and Batch. The agent still picks UIA or CDP per command, as in agent-desktop, but inside one tool and one skill.

- **For.**
  - Moderate work: the two engines barely touch, and use-browser's Go code ports almost as is.
  - The helper solves a known use-browser limit. It is long-lived, exiting after 5 idle minutes (`winuse/internal/helper/helper.go:17-18`), so it can hold one CDP connection across commands. use-browser opens one connection per invocation, and on Chrome 144+ attach mode each connection prompts the user (`use-browser/SKILL.md:41`, https://developer.chrome.com/blog/chrome-devtools-mcp-debug-your-browser-session). Inferred: one connection per helper lifetime means one prompt.
  - Each driver can feed the Agent Cursor screen rectangles (`winuse/internal/win/cursor.go:9-13`).
- **Against.**
  - The agent still decides when a page is "web".
  - Two index spaces remain: winuse Element Indexes per Window State versus use-browser's page-held `window.__bu` indexes (`use-browser/snap.go:3-8,69-70`).
  - The skill must teach both, which is roughly agent-desktop's table moved into one file.

### Option C: one Window State, automatic routing per Element

A Chromium Window is detected by `Chrome_WidgetWin_1`, which both agent-desktop (`ad/crates/windows/src/tree/chromium.rs:13`) and winuse (`winuse/docs/adr/0005-chromium-lazy-accessibility-retry.md:27-32`) already use. When a CDP endpoint for that Window's browser is reachable, its Window State merges two sources:

- the browser chrome (tabs strip, omnibox, menus, dialogs) from UIA;
- the active tab's document from CDP, replacing the UIA document subtree.

All Elements share one Element Index numbering, and each index records its origin. Actions route by origin: UIA patterns for chrome Elements, CDP for DOM Elements. With no endpoint, the Window falls back to today's UIA-only read, so the agent never has to choose.

- **For.** This is the only option that removes the seam for the agent. Specific gains:
  - Dense pages get use-browser's compact DOM snapshot instead of a 10 to 25 second UIA settle and the RefMap blow-ups agent-desktop sees on dense Chrome tabs (issue #463).
  - Navigation can use `Page.navigate` (`use-browser/commands.go:22-47`) instead of a Headed omnibox type.
  - DOM Elements with no name, which UIA can only address by unique geometry (section 3.5), get stable handles.
- **Against.** This is all new design, and agent-desktop offers no prior art for it. The hard parts:
  - **Mapping a Window to a CDP target (inferred).** Each browser Window has to be mapped to its tab target. Candidate signals:
    - the browser pid owning the debug port, which agent-desktop verifies with `process_owns_tcp_listener` (`ad/crates/core/src/commands/launch.rs:209-216`);
    - the active tab's title and URL against the UIA window title and the omnibox value;
    - `Browser.getWindowForTarget` (https://chromedevtools.github.io/devtools-protocol/tot/Browser/#method-getWindowForTarget).
  - **Coordinates (inferred).** use-browser works in CSS viewport pixels: `resolveJS` returns viewport centres and clicks with `Input.dispatchMouseEvent` (`use-browser/snap.go:123-179`). The Agent Cursor needs physical screen pixels. A DOM rectangle has to be moved by the viewport's screen origin, which the UIA document Element's bounds give, and scaled by `devicePixelRatio`. Without that, the cursor cannot show CDP actions in the right place.
  - **Staleness.** A UIA index goes stale on any Window change (`winuse/CONTEXT.md:31-33`). A use-browser index dies on navigation and fails loudly (`use-browser/snap.go:123-130`). The merged Window State needs one staleness rule and one "run state again" message.
  - **Connection modes.** Automatic routing only helps when an endpoint exists. For the user's everyday browser that means the extension, or the Chrome 144+ toggle with its prompt. A `launch`ed or `clone`d profile, which is fine since Chrome 136 (https://developer.chrome.com/blog/remote-debugging-port), is a different browser window from the one the user has open. The fallback to UIA has to be good, because it will run often.
  - **Domain terms.** "Headless Action" is defined as "through UI Automation alone" (`winuse/CONTEXT.md:53-54`). CDP clicks and `Input.insertText` (`use-browser/snap.go:286`) also need no foreground (inferred: they are protocol messages to the renderer, not OS input, and use-browser's extension mode works in a background tab group per `use-browser/SKILL.md:64`). CONTEXT.md and ADR 0010 would need a new term or a widened definition, and a decision on whether CDP coordinate clicks count as Headless.

### Option D: UIA only, improved; retire use-browser

Skip CDP. Invest in the UIA path instead, using agent-desktop's measured techniques:

- the wrapper depth skip (`ad/CONCEPTS.md:37-40`);
- a settle loop with backoff rather than one fixed retry (compare `ad/crates/core/src/tree/renderer_accessibility.rs:13-41` with `winuse/docs/adr/0005-chromium-lazy-accessibility-retry.md:21-27`);
- headless typing through window messages, if PR #400 holds up.

- **For.** One engine, one identity model, no ports, no Chrome 136 or prompt problems. Works on the user's real, running browser with no setup.
- **Against.** It drops what use-browser does well: `js` and `text` extraction, tabs by stable id, `nav`, cheap snapshots of dense pages, and cookie-isolated profiles for parallel agents (`use-browser/SKILL.md:239-302`). Chromium UIA stays slow on first contact and thin when covered (`ad/skills/agent-desktop-windows/references/chromium-and-electron.md:14-45`). PR #400 is unmerged, and it contradicts the earlier A4-4 measurement for posted keys (`ad/probes/windows/FINDINGS.md:91`). Treat it as promising, not settled.

### Cross-cutting decisions any merged design needs

1. **Which connection modes winuse supports.** Keep use-browser's discovery order: env override, then own port, then profile scan, then common ports (`use-browser/cdp.go:153-230`). Keep its rule that two debuggable browsers mean pin, never guess (`use-browser/SKILL.md:22-25`). agent-desktop's launch-only model is too narrow for "the user's browser".
2. **Endpoint verification.** Borrow agent-desktop's three checks: the loopback-only websocket URL, reading to Content-Length because the DevTools server never closes the connection, and the listener-ownership check (`ad/crates/core/src/system/cdp_endpoint.rs:92-187`, `ad/crates/core/src/commands/launch.rs:185-223`).
3. **Electron.** use-browser targets installed browsers only (`use-browser/browsers.go:273-347`). If a merged winuse should give Electron apps CDP, add an agent-desktop style fresh launch with a port. Be explicit that it cannot attach to a running Electron app (`ad/crates/core/src/commands/launch.rs:123-159`).
4. **Address bar.** In a CDP-attached Window, treat navigation as a first-class Action (`nav <url>`) and keep the omnibox addressable as a UIA Element for reading the URL. Without CDP, navigation is a Headed type into the omnibox, unless the PR #400 technique is adopted.
5. **Cursor, Esc and lease.** CDP actions should go through the same Agent Cursor travel and click (story 13) and arm the same Esc latch. The Esc stop is winuse's own; agent-desktop has none. A machine-wide input lease is out of scope today (`winuse/.scratch/headless/spec.md:56`). If one is added later, decide whether CDP actions take it. agent-desktop's lease covers OS input dispatch only (`ad/CONCEPTS.md:213-216`).
6. **Port exposure.** Document what agent-desktop documents: an open debug port can be driven by any same-user process (`ad/skills/agent-desktop/references/commands-system.md:48`). Keep use-browser's rule that `stop` closes only browsers it owns (`use-browser/SKILL.md:76-80`).

### Judgment (mine, not from a source)

The stated goal is "one tool, no seams", and only Option C meets it. Option B is a safe first step toward C: both use the same helper-held CDP connection, the same cursor wiring and the same connection modes. C then adds the merged Window State and routing on top.

The two pieces of C that need a decision or a probe before committing:

- the Window-to-target mapping;
- the CSS-to-screen coordinate transform for the Agent Cursor.

agent-desktop has measured neither, so a winuse probe should settle both before a spec is written.
