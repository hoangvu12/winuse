# 01: CDP client and Browser Connections in the helper

**What to build:**

- **Port use-browser v0.9.0** (`hoangvu12/use-browser` @ `14aac7d`) into new `internal/cdp` and `internal/browser` packages:
  - the WebSocket client with event routing;
  - discovery and endpoint verification (loopback only, port owner);
  - the browser and profile tables;
  - launch, clone and connect.
- **One persistent connection per browser**, held by the helper, with Chrome 144 approval handling and focus emulation on attach.
- **Commands:** `winuse browser status|use|launch|clone|connect|stop`.

See spec: "Browser Connection".

**Blocked by:** headless-parity 01-07 (shared helper and routing code)

**Status:** done

- [x] Offline tests: the CDP client against a fake WebSocket server, discovery and verification over faked process tables, no guessing between two browsers
- [x] The helper keeps the connection across commands and reconnects after a browser restart
- [x] `browser status` explains every failure in one sentence plus the next command
- [x] Source attribution and the license note in `third_party/`

## Comments

### 2026-11-02: landed

Ported from use-browser v0.9.0 (`C:\...\scratchpad\use-browser-v090`, commit `14aac7d`). Nothing from `find.go` or `run.go`.

**Command surface (settled).** One short `ok` line on success; failures through `Fail(sentence, next)`, exit 1. Wire names are `browser-<sub>`.

| Command | Wire name, args | Budget |
|---|---|---|
| `winuse browser status [browser]` | `browser-status`, `protocol.BrowserArgs{Browser}` | 30 s |
| `winuse browser use [browser\|auto]` (no argument prints the pin and the installed browsers) | `browser-use`, `BrowserArgs` | 10 s |
| `winuse browser launch [browser]` | `browser-launch`, `BrowserArgs` | 30 s |
| `winuse browser clone [browser] [--profile <folder>] [--fresh] [--close-source]` | `browser-clone`, `protocol.BrowserCloneArgs{Browser, Profile, Fresh, CloseSource}` | 180 s |
| `winuse browser connect [browser]` | `browser-connect`, `BrowserArgs` | 90 s |
| `winuse browser stop [browser]` | `browser-stop`, `BrowserArgs` | 30 s |

- A `[browser]` is a short name: chrome, brave, edge, chromium, vivaldi or opera. Without one, commands use the pin, then the one debuggable browser.
- `launch`, `clone` and `connect` pin the browser, as use-browser did.
- `status` exits 0 only when connected, so an agent can poll it. Example: `ok chrome connected: winuse's own profile, 3 tabs`.
- Dropped from use-browser: `--port`, `--no-sync`, `profiles`, `clean`, `doctor` (replaced by `status`), sessions, `BU_CDP_URL`/`BU_CDP_WS` (the helper holds one connection, and remote endpoints fail the loopback rule), and the per-session state port (a `winuse-port` file in each own profile replaces it).
- `ext install|status` is ticket 02. The CLI switch in `internal/cli/browser.go` has a marked place for it; `browser ext` is an unknown subcommand until then.
- The browser-* commands are observation-only in the registry (`observationOnlyCommand`), so a 90 s connect or a 180 s clone never holds the action gate.

**What landed, per file.**
- `internal/cdp/ws.go`: the RFC 6455 client. `DialWS(ctx, url, handshake)` checks `Sec-WebSocket-Accept`, returns `*HandshakeError` on a non-101 answer, and lets ctx cancel a held handshake. Writes are mutex-guarded because the reader answers pings. Server frames are unmasked if masked. Unknown opcodes fail. Messages are capped at 256 MB.
- `internal/cdp/client.go`: the `Transport` interface (use-browser's msgConn) and the `Client`.
  - One reader goroutine matches responses to calls by id.
  - `Subscribe(sessionID, method)` gives per-session routing; method "" means every method of that session. Queues are unbounded, so a slow reader never stalls calls or loses a dialog.
  - `Target.detachedFromTarget` fails that session's pending calls with `ErrSessionDetached`.
  - On close, every pending and later call fails with `ErrClosed`.
  - A call without a ctx deadline gets 15 s.
- `internal/cdp/cdptest/server.go`: a fake DevTools endpoint shared by the cdp and browser tests. It does its own server-side framing, and supports fragmenting, pings, held handshakes, rejections, a bad accept key and `/json/version` with a Content-Length.
- `internal/browser/browsers.go`: the browser table (Order, the user-data-dirs, the install paths from ub/browsers.go:32-70 and 273-327), `IdentifyImage` (install folder first, so Chrome and Chromium are told apart; then the image name) and the inspect URLs.
- `internal/browser/system.go`: the `System` seam, the pin file (`%LOCALAPPDATA%\winuse\browser\state.json`, BOM-tolerant), the `DevToolsActivePort` reader, the `winuse-port` file and a poll helper on the System clock.
- `internal/browser/system_windows.go`: `Live()`.
  - The listener owner comes from `GetExtendedTcpTable` (IPv4, then IPv6), with pure row parsers.
  - Images come from `QueryFullProcessImageName`; running processes from a Toolhelp snapshot.
  - Browsers start with `CREATE_BREAKAWAY_FROM_JOB` (falling back without it), so they outlive the helper's idle exit.
  - `taskkill /IM <exe>` runs without /F, and with no console window.
  - `/json/version` is read to its Content-Length, with keep-alive off.
  - Start, CloseApp, Dial, VersionWS and PortAlive refuse in a test binary (`win.LiveDesktopGuard`).
- `internal/browser/discover.go`: discovery and verification.
  - Discovery order: extension (`System.Extension`), then winuse's launch and clone profiles, then the real user-data-dirs (toggle), then ports 9222 to 9224.
  - Each candidate must pass `verify`: a loopback ws URL on the probed port with a `/devtools/` path, and a port owner whose image is the expected browser. With nothing named, the owner names the browser; a process that is not a known browser is refused.
  - `choose` refuses two distinct browsers.
  - `nothingFound` returns the most specific cause.
- `internal/browser/problem.go`: 19 failure kinds, each with one `Sentence()` and one `Next()`.
- `internal/browser/manager.go`: the `Manager` and `Connection`.
  - It keeps one connection per browser, reused while alive and re-discovered and re-dialed once its reader has ended.
  - At most one dial per browser is in flight. A caller waits `ApprovalWait` (20 s), then gets `AwaitingApproval`. The dial keeps going for 2 minutes, so a retry joins it.
  - Dial errors map to Refused, AwaitingApproval or Unreachable.
  - `Connection.AttachPage` sends `Target.attachToTarget {flatten:true}`, then `Emulation.setFocusEmulationEnabled {enabled:true}` and `Runtime.runIfWaitingForDebugger` on the new session. `Connection.Pages` lists page targets, without internal ones.
  - Nothing calls `Page.bringToFront` or `Target.activateTarget`.
- `internal/browser/commands.go`: Status, Use, Launch (flags `--remote-debugging-port`, `--user-data-dir`, `--no-first-run`, `--no-default-browser-check`), Connect (opens `<scheme>://inspect/#remote-debugging`, waits 55 s for the toggle, then dials) and Stop (launch and clone profiles only; `Browser.close`, then waits for the profile to be released). Also `profileInUse` (lockfile plus a live DevToolsActivePort).
- `internal/browser/clone.go`: all of ub/clone.go: the incremental sync with SQLite groups and orphan-sidecar removal, pruning, the Local State copy, cache skipping, cookie-lock detection, metadata (`winuse-clone.json`), and `--close-source` (CloseApp, wait for the cookie DB, wait for the clone to exit, then `--restore-last-session`). Notes follow the ok line as `note: ...` lines.
- `internal/helper/browser.go` (new): the `BrowserService` interface, the handlers and `browserError` (a Problem becomes `Fail(Sentence(), Next())`).
- `internal/cli/browser.go` (new): parsing (local usage errors exit 2) and `browserUsage`.
- `third_party/use-browser/NOTICE`: the source, the commit, what was ported, and the relicensing under winuse's MIT license by the same author. It also notes that agent-browser ideas were re-implemented and no agent-browser code or text was copied, so there is no third_party/agent-browser note.

**Services hooks (for ticket 02).**
- `Services.Browser BrowserService` (state.go). `defaultServices` falls back to `browser.NewManager(browser.Live())`; `helper.Main` builds its own and closes it on exit. Ticket 04 reaches the live connection with `s.Browser.(*browser.Manager).Connection(ctx, name)`.
- To plug in the extension bridge as another transport:
  - Implement `cdp.Transport`: `WriteText([]byte) error`, `ReadMessage(deadline time.Time) ([]byte, error)` and `Close() error`. This is use-browser's msgConn; its frameConn fits as is. The client calls ReadMessage from one goroutine with a zero deadline, serializes WriteText itself, and needs Close to unblock a pending read.
  - Set `System.Extension func(ctx, pin string) (*browser.Endpoint, error)` in `browser.Live()`. Discovery calls it first ("extension first"). Return nil for no bridge, or an `Endpoint{Browser, Mode: browser.ModeExtension, WSURL: <a label>, Open: func(ctx, handshake) (cdp.Transport, error)}`. The manager calls `Open` instead of dialing a WebSocket. Ambiguity between bridges is the hook's own error to return (use-browser's pickBridge).
  - `Mode.Describe()` already has the extension wording. Add the `ext` subcommands in `internal/cli/browser.go` and two more `browser-ext-*` entries in `browserCommands`.

**Tests.**
- cdp:
  - matching responses by id when they arrive out of order, and sessionId on calls;
  - CDP error responses;
  - 64 concurrent calls;
  - routing per session and method, plus browser-level events;
  - 500 unread events not stalling a call, delivered in order;
  - Close and a server drop failing pending calls (and later calls, and ending subscriptions);
  - a detached session failing its calls;
  - ctx timeouts;
  - 64-bit length frames both ways;
  - fragments with pings in between, client masking checked;
  - a refused handshake, a bad accept key, a held handshake timing out, and a non-ws scheme.
- browser:
  - the activePort reader, `checkLoopbackWS` and `IdentifyImage` tables;
  - discovery: own launch, two browsers giving an error that names both, the pin winning (directly and through the stored pin), wrong owner, an unknown or unreadable owner, non-loopback, a stale DevToolsActivePort, common ports with the owner attributed, the extension first, and the nothing-found causes;
  - TCP table parsing;
  - the manager: reuse across commands (1 handshake), a re-dial after a drop, one approval prompt at a time (3 concurrent callers plus status give 1 handshake, then a success after release), a refused handshake, the attach sequence, Pages filtering, and Close;
  - commands: launch (flags, profile under %LOCALAPPDATA%\winuse, the pin, reuse), launch never coming up, a busy profile, stop leaving the user's toggle browser alone and closing winuse's own, connect opening the inspect page and waiting, toggle off, use, a pin to an uninstalled browser, clone (copy, cache skipped, args), clone sync with a note, missing profiles, close-source (a clean close, `--restore-last-session`), taskkill never using /F, `versionWS` against a kept-alive server, and `Live()` refusing in tests;
  - the four use-browser clone tests (TestSyncTreeMovesSQLiteGroupTogether's fixture was fixed: its two databases had the same size, which made it flaky upstream), plus incremental-sync and cache-skipping tests;
  - `TestProblemSentences` (22 cases, one per state) and `TestEveryKindHasASentence`.
- helper: the registry list, handler arguments, Problem to hint, and browser commands never waiting for the action gate.
- cli: parsing, usage exits 2, the usage text, and the budgets.

**headless-parity-modified files I edited (minimal):**
- `internal/protocol/protocol.go`: the budgets plus `BrowserArgs` and `BrowserCloneArgs`.
- `internal/helper/state.go`: the import, the `Services.Browser` field, its default, the registry loop and `observationOnlyCommand`.
- `internal/helper/helper.go`: build, wire and close the manager.
- `internal/helper/registry_test.go`: six names.
- `internal/cli/cli.go`: the usage lines and `case "browser"`.

**Risks to measure live (ticket 03 probe):**
- Whether `GET /json/version` or a bare TCP connect to a toggle-mode port raises Chrome's approval prompt. Discovery does both before the dial; use-browser did the same.
- That Chrome 144+ really holds the WebSocket handshake while its prompt is up, and what a declined prompt answers. Refused assumes a non-101 status; a dropped socket would read as Unreachable.
- Whether the prompt is once per connection, so one held connection means one prompt per helper. The helper exits after 5 minutes idle, which drops the connection and brings the prompt back on the next command.
- The listener owner: that the DevTools port belongs to the browser's main process (it does for launch), that `QueryFullProcessImageName` works across integrity levels, that Opera's versioned folder and Edge under Program Files (x86) are identified, and Chromium installed elsewhere (read as Chrome).
- Whether `CREATE_BREAKAWAY_FROM_JOB` is allowed from the helper's job, and whether a launched or cloned window takes the foreground (the helper has no foreground rights, so it may only flash).
- `connect` opens a tab in the user's browser, which may raise it.
- `taskkill /IM` without /F closing every window of that browser cleanly, and `--restore-last-session` bringing the tabs back in the clone.
- Focus emulation is sent on attach, but nothing attaches yet (ticket 04 does). Whether background-tab input works with it alone, or also needs `--disable-backgrounding-occluded-windows` (not added).
