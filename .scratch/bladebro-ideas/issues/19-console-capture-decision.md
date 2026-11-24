# 19: Console capture: scope and visibility

Status: resolved
Type: grilling
From: `map.md`; also the agent-browser list (`.scratch/learn-from/README.md` rank 10)

## Decisions needed

- **When to turn capture on:** always on Tabs the agent works in, only when
  asked, or only on agent-owned Tabs.
- **What to capture,** given what `Log.enable` actually catches without
  `Runtime` (ticket 11).
- **Notes:** a `Note:` after a Page Action that threw.
- **A read command,** its bound and its clearing.
- **Network:** an XHR/fetch endpoint list like bladebro's `logs: "xhr"`
  (method, status, URL, header names only), and whether `Network.enable` is
  acceptable in the user's Tabs.

## Answer

Grilled 2026-11-24 (round 4).

1. **`Log.enable` is always on** in Tabs an agent works in. Pages couldn't
   detect it, and it replays earlier entries (ticket 11). It catches only
   failed requests and CSP or security errors. After an Action, new
   entries show up as a note, e.g.
   `Note: 2 failed requests: 404 /api/save, 500 /api/user`.
2. **JavaScript exceptions** come from a main-world listener installed by
   `Page.addScriptToEvaluateOnNewDocument`. It passes data to an isolated
   world through a randomly named event, and winuse reads it with
   `Runtime.evaluate` + `contextId`, never `Runtime.enable`.
   - On by default in agent-owned Tabs and browsers winuse launched.
   - In the user's own Tabs, only on request (`winuse logs <w> --capture`).
   - It never wraps `console.*`, because that is detectable.
3. **`winuse logs <window>[:<tab>] [--clear]`** prints the last 50 entries:
   failed requests, CSP errors and exceptions with stacks. It's bounded by
   `--max-chars`. bladebro's network endpoint list is skipped, because
   `Network.enable` has a detectable timing cost.

Build tickets: 29 (`Log` notes and `winuse logs`), 30 (the exception
listener).
