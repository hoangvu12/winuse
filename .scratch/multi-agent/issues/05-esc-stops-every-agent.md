# 05: Esc stops every agent

**What to build:** an Esc press stops every agent's running and waiting Actions, not only the one command that runs now.

Today the Esc hook (`internal/win/eschook.go`) latches a stop for the running command.

- One stop latch fans out to every agent's running Action and to every request waiting for a lane (ticket 06).
- Each stopped command gets today's wording: "nothing was sent" before delivery, or the delivered message after it, with exit 130.
- After a stop, new requests run again: Esc stops what is in flight, it is not a lock.
- During Headless work Esc still reaches the user's App (passive scope), as today. During a Headed Action it is swallowed, as today.
- A tray icon is out of scope.

**Blocked by:** 02 (and 06 for waiting requests, once it lands)

**Status:** ready-for-agent

- [ ] Fan-out of the latch to every in-flight request
- [ ] Offline tests: two agents running, both stopped; waiting requests stopped; a new request after the stop runs
- [ ] SKILL.md: Esc stops every agent
