# 07: Settle before `--observe`; `wait idle` and `wait js`

Status: resolved
Type: task
From: `map.md`; bladebro `bb/src/page/perception/wait.rs:89-179` (settle), `bb/src/page/route.rs` (SPA route changes), `bb/src/action/verdict.rs:626-925` (wait conditions)

## Problem

- **`--observe` reads too early.** ADR 0021 says the observation is read
  right after the Action (`docs/adr/0021-observe-after-an-action.md:119`).
  A Page that reacts late, such as a client-side route change or a fetch
  followed by a render, shows "no change", and the skill tells the agent to
  `wait` and read again, which costs a turn.
- **The `wait` kinds are limited.** `page.WaitExpr`
  (`internal/page/nav.go:301-335`) has `selector`, `text`, `url`, `load` and
  `download`. Its `text` reads `document.body.innerText` only, which misses
  open shadow roots and same-origin frames.

## How bladebro settles

One in-page promise:
1. Resolve once there have been 110 ms with no DOM mutation.
2. Then wait for in-flight requests to finish, giving up after a 280 ms
   plateau, so that pages with websockets or long polling don't stall it.

A hard bound caps both.

## Ask

- **Settle before observing.** After a Page Action under `--observe`, settle
  before reading:
  - a DOM-quiet window, then a short network-quiet window if requests can be
    counted cheaply;
  - a hard bound of about 1 s, or calibrated per site later (ticket 16);
  - when the bound is hit, say so in a Note rather than read silently.

  Ticket 11 measured `Network.enable` as a detectable timing cost, about 20%
  slower on 300 local fetches. So don't enable `Network` in the user's Tabs
  to count requests. Count them in the page instead, with a
  PerformanceObserver on resource entries read from an isolated world.
  It sees requests only once they finish, so treat it as "no new resource
  for N ms" rather than an exact in-flight count.
- **Amend ADR 0021** with the settle rule and its bound.
- **`wait <window> idle [--timeout]`:** waits for the same settle,
  explicitly.
- **`wait <window> js <expr> [--timeout]`:** polls a predicate.
  - A syntax error fails at once.
  - On timeout, report the last error the predicate threw.
  - This was already on the agent-browser list ("smaller ones"); bladebro is
    a second source for it.
- **`--gone`** for `wait selector` and `wait text`.
- **`wait text`** pierces open shadow roots and same-origin frames.
- **Update** the help, `batch-and-wait.md` and `browser.md`.
- **Tests:**
  - fake-clock tests for settle;
  - e2e fixtures: a late render, a route change, a page with a websocket.

## Answer

Built on branch `agent/bladebro-ideas` (merge bf192ff; review fixes in 081a150). `--observe` on a Page settles first (110 ms DOM quiet + 280 ms request quiet, 1 s bound, note when still busy). New `wait idle`, `wait js <expr>`, `--gone` for selector/text; `wait text` searches open shadow roots and same-origin frames. ADR 0021 amended.
