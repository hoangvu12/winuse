# 11: Console and errors without Runtime; HTTP status method

Status: resolved
Type: research
From: `map.md`; bladebro `bb/src/page/logs.rs`, `bb/src/page/navigate.rs`, `bb/src/stealth/mod.rs:3` (no `Runtime.enable`, because DataDome detects it)

## Questions

- **A. Console and errors.**
  - With `Log.enable` alone (no `Runtime.enable`), which events arrive:
    console.log/warn/error, uncaught exceptions (sync and in a timer),
    unhandled rejections, failed fetches, CSP violations, deprecations?
  - Does it replay earlier entries?
  - Can a page detect it, using the getter-on-a-logged-object trick that
    gives `Runtime.enable` away?
  - Is there another way to catch uncaught exceptions that a page can't
    detect?
- **B. HTTP status.**
  - Does `performance.getEntriesByType('navigation')[0].responseStatus`
    work in the installed Chrome for 200, 404, 429 with Retry-After, 503,
    and a redirect chain that ends in 404?
  - How does it compare with `Network.responseReceived`?
  - What does `Network.enable` cost?

This blocks tickets 02, 07 (request counting) and 19.

## Answer

Tested on Chrome 155.0.8059.85 (headless=new). The scripts and raw results
are in `../prototypes/console/` (`exp.mjs`, `exp2.mjs`); the raw
`results*.json` stayed in the chat's scratch folder `exp\console\`.

**A. What each domain catches**

| Event | `Log.enable` | `Runtime.enable` |
|---|---|---|
| console.log/warn/error/info/debug | nothing | `consoleAPICalled` |
| Uncaught exception (sync, timer) | nothing | `exceptionThrown` |
| Unhandled rejection | nothing | `exceptionThrown` "(in promise)" |
| Failed fetch (404, network error) | `entryAdded` network/error | nothing |
| CSP violation | security/error | nothing |
| Deprecation | nothing | nothing |

Deprecations arrive only as `Audits.issueAdded` (DeprecationIssue). CSP
violations arrive there too.

- **Replay.** `Log.enable` replays earlier entries with their original
  timestamps; `Log.clear` empties that store. `Runtime.enable` also replays.
- **Detection.**
  - **`Log` and `Audits`:** none of the probes caught them (getters, Proxy
    traps, toString, and timing of 50 `console.debug(bigObject)` calls,
    which was unchanged).
  - **`Runtime.enable`:** the old getter trick no longer fires in Chrome
    155, but timing still gives it away: the same loop is about 30–40 times
    slower (8–13 ms against 0.2–0.3 ms).
- **Uncaught exceptions without Runtime.** A **main-world** listener
  installed by `Page.addScriptToEvaluateOnNewDocument` catches throws and
  rejections, with stack.
  - An isolated-world listener does not: it sees only resource errors and
    CSP.
  - The listener can hand data to an isolated world through a randomly
    named `CustomEvent`, and the page saw nothing: `window.onerror` stayed
    null, there were no new globals, and its own handlers ran and weren't
    prevented.
  - winuse reads that isolated buffer with `Runtime.evaluate` and
    `contextId`, which needs no `Runtime.enable`.
  - It covers only documents loaded after registration.
  - **Residual risks (reasoned, not tested).** The script must save
    `dispatchEvent` and the other built-ins it uses when it installs.
    Reading `e.error.stack` can trigger a page's `prepareStackTrace`.
  - Wrapping `console.*` is detectable, so skip it.

**B. HTTP status**
- **`responseStatus`.**
  `performance.getEntriesByType('navigation')[0].responseStatus` is correct
  for 200, 404, 429 and 503, and for a redirect chain that ends in 404
  (redirectCount=2 same-origin, 0 cross-origin). It also works read from an
  isolated world. It gives no headers and no status text.
- **Empty-body 4xx/5xx.**
  - `Page.navigate` returns `net::ERR_HTTP_RESPONSE_CODE_FAILURE`.
  - The frame becomes `chrome-error://chromewebdata/` with `unreachableUrl`
    set.
  - Its navigation entry still gives the real status.
- **`Network.responseReceived`** (type Document) has the status,
  statusText, `retry-after` and a matching `loaderId`.
- **Cost of `Network.enable`:** request headers are unchanged, but 300 local
  fetches are about 20% slower (a timing signal), and it buffers response
  bodies (from docs).

**Recommendation**
- In the user's Tabs, `Log` (and `Audits`) is safe to enable, as
  network/security/deprecation messages.
- Never enable `Runtime` there.
- Console calls and exceptions need the opt-in main-world listener.
- HTTP status comes from `responseStatus`, plus `errorText` and
  `unreachableUrl` for Chrome's own error page.
- `Network` only when headers are asked for.
