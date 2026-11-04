# 09: Prefer the user's own browser and profile

Status: done

**Request (user, via the multi-agent research chat).** The skill should always
prefer the user's existing browser profile: the extension in their running
browser first, else `browser connect`; `browser launch` / `browser clone` only
when the user asks for a separate browser or their own cannot be connected
(and say so when falling back). When the browser asks to allow debugging, the
agent allows it instead of stopping to ask: the user wants that on their own
machine.

**Evidence from the session.** The agent picked the extension route on its
own (good), but only after `browser status` offered four routes with no
recommendation. **No debugging consent prompt appeared**: the extension route
shows only Chromium's "started debugging this browser" infobar (the agent told
the user to expect it). The prompt the request is about is Chrome 144+'s
"Allow remote debugging?" for `browser connect`, which the session never
reached.

**What contradicts the preference today.**

- SKILL.md "Get a Browser Connection" lists ext / launch / clone / connect as peers, with launch for "no logins needed", and calls connect a route to use "only while the user is at the keyboard".
- `NoDebugging` hint: four peers (fixed with issue 02: the extension first).
- `NotRunning` hint: "run winuse browser launch ... or winuse browser clone ..." when the user's browser is merely closed: the user's own browser is never offered.
- `AwaitingApproval` hint: "click Allow ... ; winuse browser clone never asks", which steers towards a clone.
- Discovery itself (`discover.go`) already tries the extension first, then the debuggable browsers, so a connected user's browser wins. It falls to a browser winuse launched or cloned only when one is already running and the user's is not connected: no change.

**Fix.**

- SKILL.md: the order is the user's browser through the extension, else `browser connect`; launch/clone only on request or after both failed, saying so to the user. The consent steps the agent may take itself (turn on the inspect toggle, click Allow in the prompt) with exact commands, `--headed` as the fallback, and asking the user only if the prompt ignores automation. The extension's infobar is never cancelled.
- `NotRunning`: ask the user to open their browser first (then the extension); launch/clone as the alternative for a separate browser.
- `AwaitingApproval`: how to click Allow with winuse; no clone advertising.
- Nothing in winuse clicks a consent prompt by itself: the agent does it as an Action the user can see and stop, on this user's instruction. An automatic bypass inside the binary would let any caller of winuse attach to the user's browser silently.

**Not verified.** Whether Chrome's "Allow remote debugging?" button takes a Headless UI Automation Invoke: no live check was possible without driving the user's browser. The skill tells the agent to fall back to `--headed`, then to the user.

## Comments

Done on branch browser-ux: SKILL.md (Get a Browser Connection, Safety), README, `NotRunning` / `AwaitingApproval` / `Refused` / `NoDebugging` hints. Not verified live: whether Chrome's prompt takes a Headless Invoke.
