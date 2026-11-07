# 02: The Agent Cursor never holds an Action back

Status: wontfix (user, 2026-11-07)

**What to build:** `AgentCursor.TravelAbove` returns only when the travel completes, bounded by twice the travel time (`internal/win/cursor.go:216`). Travel is 150-400 ms (`cursor_motion.go:36-37`), so every Action whose target differs from the last one waits about 260-300 ms before it is sent (measured: Seven, Equals, Clear in turn, 610-670 ms per click by default, 299-349 ms with only the settle off, 37-64 ms with both off).

The cursor is presentation only (CONTEXT.md: Agent Cursor). Send the Action while the cursor travels: start the travel, dispatch, and let the click effect play when the cursor lands. A Headed Action keeps today's order (the cursor shows where the real pointer goes).

**Blocked by:** none

- [ ] Headless Actions dispatch without waiting for the travel
- [ ] The ripple/outline still play at the target
- [ ] Offline tests with a fake cursor that records ordering
- [ ] Benchmark with alternating targets

## Comments

### 2026-11-07: conflict, for the user

The travel is also the Esc window before a Headless Action: `runHeadless` checks `esc.Stopped()` after the travel and stops before the pattern call (`internal/helper/headless.go:385-390`, `escStopBeforeActError`). Not waiting for the travel means the Action fires before the user can stop it. Options: (a) dispatch at once, the cursor finishing its glide and playing the click effect after (fastest, no Esc window); (b) cap the wait (cua caps a batch step's cursor wait at 120 ms, `cua/libs/cua-driver/rust/Skills/cua-driver/EMBEDDING.md:267-273`); (c) keep it. Measured after tickets 01 and 03: alternating-target clicks 293-328 ms in the helper, about 40 ms without the travel.

### 2026-11-07: decision (user)

Keep the cursor as it is: not much of an issue. For the record, agent-desktop and cua also wait for the glide when their cursor is on (agent-desktop up to 900 ms, `CURSOR_ARRIVAL_TIMEOUT_MS`; cua `animate_cursor_to` suspends until the path lands); theirs is off by default, winuse's is always on. An off switch (`WINUSE_CURSOR=off`) is the option if speed ever matters more.
