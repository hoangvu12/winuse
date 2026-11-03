# 05: Esc stops every agent

**What to build:** an Esc press stops every agent's running and waiting Actions, not only the one command that runs now.

Today the Esc hook (`internal/win/eschook.go`) latches a stop for the running command.

- One stop latch fans out to every agent's running Action and to every request waiting for a lane (ticket 06).
- Each stopped command gets today's wording: "nothing was sent" before delivery, or the delivered message after it, with exit 130.
- After a stop, new requests run again: Esc stops what is in flight, it is not a lock.
- During Headless work Esc still reaches the user's App (passive scope), as today. During a Headed Action it is swallowed, as today.
- A tray icon is out of scope.

**Blocked by:** 02 (and 06 for waiting requests, once it lands)

**Status:** done (live e2e, if wanted, still to do: paused while the user games)

- [x] Fan-out of the latch to every in-flight request
- [x] Offline tests: two agents running, both stopped; waiting requests stopped; a new request after the stop runs
- [x] SKILL.md: Esc stops every agent

### 2026-11-03: done (worker, branch ma05-esc-all)

- Running work already shared the one latch: every armed or passive scope of any agent sees the same press. Until lanes (06) land, only one Action holds the action gate at a time, so "waiting" means the requests queued on the gate.
- `internal/win/eschook.go`: the latch gains a stop generation (`gen`, advanced on every recorded press) and a waiting count. `EscWatcher.BeginWait`/`EndWait`/`StoppedSince(gen)`.
  - A press while only requests wait is recorded too, and the key passes through: they are not in control.
  - The waiting scope never holds the running scope's `pressed` latch, so a request that arrives after the press starts clean. Esc is not a lock.
- `internal/helper/state.go` `serialized`: a request snapshots the generation before it waits for the gate (`gateWait`, polling every 25 ms). When a press lands while it waits, or in the instant the gate opens, it returns the before-the-Action stop (`escStopBeforeActError`: exit 130, "nothing was sent") and never runs. The running Action stops through its own checkpoints, as before (delivered wording after delivery). `WaitingEscWatch` is optional on `Services.Esc`, so the fakes and the no-op keep today's behaviour.
- When ticket 06 replaces the gate with lanes, its lane wait should take the same `gateWait` (BeginWait before, StoppedSince while waiting).
- Docs: SKILL.md "When the user presses Esc", codex-parity "esc stop" (statuses kept), ADR 0014 status (Decision 5 landed).
- Tests: `TestEscLatchStopsWaitingRequests` (win: generation, waiting-only press passes and leaves no latch, nothing in flight records nothing) and `internal/helper/escall_test.go` (one running and two waiting requests: both waiting stopped with nothing sent and never run, the running one stopped, a request after the press runs; without the waiting side the old wait). No e2e: the ticket lists none; the stop's live path is the existing hook.

