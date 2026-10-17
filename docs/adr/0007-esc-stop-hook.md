# ADR 0007: The Esc stop — a low-level keyboard hook on the shared UI thread, swallowing while armed

## Status

Accepted (ticket 12 — Esc stop).

## Context

The spec (Control, user story 45) requires the physical Esc key to
immediately stop the current Action or Batch, so the user can take back
control at any moment, and the stopped command to exit with Codex's
interrupt code (130) and a message telling the agent to stop and ask.
The notes pin the details:

- win32.md §8: the stop is a `WH_KEYBOARD_LL` hook in the helper,
  installed on a pumping, OS-locked thread; the callback must stay to a
  few comparisons plus a non-blocking send, because Windows silently
  removes a hook whose callback outlives `LowLevelHooksTimeout`; the
  thread must never block in SendInput or UIA.
- ADR 0006 left the hook a ready home: the overlay's UI thread (the
  shared thread), with the instruction that a helper without an overlay
  still needs the hook (its own fallback thread).
- codex-surface.md §4: Codex's hook ignores injected keystrokes, **the
  key is swallowed** (the app never receives it), and the helper exits
  130. winuse has no per-turn helper, so the latch must not persist
  across commands (§7's proposal).
- decisions.md: exit code 130, the message says the user stopped winuse
  with the physical Escape key, tell the agent to stop and ask.

## Decision

1. **One watcher, one shared thread.** `win.EscWatcher`
   (`internal/win/eschook.go`) owns the hook. The helper installs it on
   the overlay's UI thread through `Overlay.Do` (ADR 0006's handover);
   when there is no overlay (its thread could not start), it starts its
   own pumping thread — a helper without an indicator still stops on
   Esc. A watcher that cannot install (a test binary, a hook failure)
   disables itself: commands run, logged, without Esc stopping — the
   same degradation the overlay uses.

2. **The callback is a decision table plus atomic stores** (never a
   channel send — an atomic store is even cheaper than the sketched
   non-blocking send and satisfies the same never-block constraint):
   `nCode < 0` passes unread; every injected event (`LLKHF_INJECTED`)
   passes — winuse's own `press-key Escape` and any other injector's
   keys reach the app untouched; a physical Esc **key-down while armed**
   is latched and swallowed; the swallowed press's **key-up is swallowed
   too, paired** (swallowing an up without its down would leave an app
   believing Esc is held — the user may be playing a game); a physical
   Esc while *not* armed passes through — the hook never eats the user's
   own keyboard between commands.

3. **Armed scopes, not a global latch.** `Arm`/`Disarm` bracket every
   scope the user can stop, nestable: an Action arms inside
   `runAction` (from the activation on — winuse is not in control while
   it refuses); a Batch arms once around the whole loop, so a press
   between steps — or in the last step's tail — still stops the Batch;
   a launch-app wait arms around the wait. The latch clears when the
   outermost scope ends, so a stop never leaks into the next command
   (winuse has no turn concept; the next command starts clean).

4. **Abort checkpoints.** The checks are: after the activation and
   around the act step in `runAction` (a press outranks the step's own
   outcome — the agent must act on the user's signal); between typing
   batches in `win.TypeText` (ticket 06's designed pause, returning
   `win.ErrEscStopped` — the batches already injected landed); at the
   top of the Batch run loop and after it; in `apps.WaitForWindow`'s
   poll (a launch-app wait stopped by Esc surfaces as the stop, not the
   NoWindow error, and the already-started App keeps running).

5. **The stop's contract is one line everywhere**: exit 130 with
   "winuse was stopped by the user with the physical Escape key" and a
   hint telling the agent to stop and ask (the interrupted Action may
   have partly taken effect). A stopped step inside a Batch keeps the
   wrapper's step naming; the server reports the stop even when it races
   the request timeout (the user caused it, the budget did not).

6. **The pointer Actions became Batch steps** (ticket 10's scroll, drag,
   hover): `protocol.BatchStepKinds` and the CLI's step parser grew the
   three, dispatched to the same commands the standalone forms use.

## Consequences

- Only a physical press stops winuse, everywhere: injected keys pass the
  hook by OS contract (LLKHF_INJECTED), not by trusting our own
  `injectedTag`.
- While a command runs, the user's Esc is swallowed (Codex parity): the
  controlled app sees neither the key-down nor its key-up.
- The hook lives for the helper's lifetime (installed at start, unhooked
  at exit, removed by the OS if the process dies); between commands it
  adds a few comparisons per keystroke system-wide.
- The offline tests cover the latch's decision table, the checkpoints
  and the exit-130 mapping with fakes; the live hook (and the swallow)
  is pinned only by the user-run e2e suite, which needs a human at the
  keyboard to press the key.
- Input that cannot be checkpointed mid-way (a click's single SendInput
  batch, a chord, a wheel scroll) stops at the step boundaries instead:
  the events already injected landed, which the stop's hint says
  outright. The drag's interpolation could gain a checkpoint later if
  drags ever grow long.
