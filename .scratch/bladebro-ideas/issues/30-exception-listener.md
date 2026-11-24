# 30: JavaScript exceptions without `Runtime.enable`

Status: resolved
Type: task
Blocked by: 29
From: tickets 11 and 19. A prototype is in `../prototypes/console/exp2.mjs`.

## Ask

- **The listener.** Install it with `Page.addScriptToEvaluateOnNewDocument`
  with no worldName, so it runs in the main world.
  - It listens for `error` and `unhandledrejection` and catches message,
    file, line and stack.
  - It saves `addEventListener`, `dispatchEvent`, `CustomEvent` and the
    string and JSON methods it uses when it installs, so later page patches
    can't see it.
  - It hands each record to an isolated world through a randomly named
    `CustomEvent` (per Tab), and never sets `window.onerror` or globals.
  - It never wraps `console.*`.
- **Reading.** An isolated world (`Page.createIsolatedWorld`) keeps a bounded
  buffer. winuse reads it with `Runtime.evaluate` + `contextId`; ticket 11
  confirmed this needs no `Runtime.enable`. Records join ticket 29's ring
  and its notes, e.g. `Note: the Page threw: TypeError: x is undefined
  (app.js:120)`.
- **When.**
  - On by default in agent-owned Tabs (`tab new`) and browsers winuse
    launched or cloned.
  - In the user's own Tabs, only after `winuse logs <w> --capture`, which
    lasts for that Tab's life.
  - It covers documents loaded after it is installed. Say so on the first
    `--capture`, and offer `reload`.
- **Risk.** Reading `e.error.stack` can trigger a page's
  `Error.prepareStackTrace`. Read `stack` only through a saved property
  read, and cap its length.
- **Tests.**
  - An e2e fixture that throws synchronously, in a timer and in a promise.
  - A detection fixture checking that `window.onerror` stays null, there
    are no new globals, the page's own handler still runs, and
    `defaultPrevented` is unchanged.

## Answer

Built on branch `agent/bladebro-ideas` (merge 3b1fe8e; review fixes in 081a150). A main-world exception listener installed with `addScriptToEvaluateOnNewDocument` (after `Page.enable`) passes records to an isolated-world buffer; notes say `the Page threw: …`. On by default in agent-owned Tabs (`tab new` opens on about:blank first), opt-in `logs --capture` on the user's. Never Runtime.enable.
