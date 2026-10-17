# ADR 0009: One COM home per technology — UIA MTA, WGC MTA, a throwaway STA for the shell

## Status

Accepted (the winuse-v1 review). Amends ADR 0003's "one goroutine for
all COM".

## Context

ADR 0003 said all COM work runs on one OS-locked goroutine that called
`CoInitializeEx`. The shipped code has three COM-initialised places, which
the review flagged as contradicting that rule:

- the UI Automation worker (`internal/uia/thread.go`): one OS-locked
  thread, `CoInitializeEx(COINIT_MULTITHREADED)`, serving every tree read
  and Element action through a job channel;
- the Windows.Graphics.Capture worker (`internal/wgc/capture.go`): its own
  OS-locked thread, `RoInitialize(RO_INIT_MULTITHREADED)`, serving every
  capture through its own job channel;
- `apps.shellExec` (`internal/apps/launch.go`): a fresh locked thread that
  initialises apartment-threaded COM around one `ShellExecuteExW` call and
  is thrown away after.

None of these can be merged into one thread, and each exists for a
concrete reason:

- **UIA stays MTA on its own thread** (uia-go notes §6): Microsoft
  recommends calling UI Automation from a thread that owns no windows and
  is MTA; MTA needs no message pump (an STA would need one and risks
  cross-apartment marshalling deadlocks). The thread must never host the
  overlay or the Esc hook (ADR 0006/0007).
- **WGC needs its own MTA thread** (wgc.md §2): WinRT activation requires
  `RoInitialize` (a different initialisation entry point than
  `CoInitializeEx`), the WGC runtime classes are agile/free-threaded so
  any MTA thread works, and the Direct3D immediate context is not
  thread-safe and must stay confined to one thread. Keeping it separate
  from the UIA thread also means a WGC stall (GPU, driver) can never
  block a tree read — the same isolation ADR 0006 demands between the UI
  thread and everything else.
- **ShellExecuteEx needs an STA for one call**: the shell documents that
  `ShellExecuteEx` may require COM, and shell COM objects are
  apartment-threaded; a thread that joined the process MTA can be refused
  by the shell (RPC_E_WRONG_THREAD in the worst case). The call happens
  once per `launch-app`, so a short-lived thread — locked, STA-initialised
  around the single call, uninitialised, discarded — costs a goroutine
  spawn and nothing else.

## Decision

1. **One COM home per technology.** All UI Automation work runs on the
   one UIA MTA thread; all Windows.Graphics.Capture/Direct3D work runs
   on the one capture MTA thread; `ShellExecuteEx` runs on a throwaway
   STA thread. ADR 0003's rule is amended accordingly: "all COM work on
   one locked, COM-initialised thread per technology".

2. **COM state never leaves its home.** No interface pointer crosses a
   job boundary in either direction: jobs go in as plain Go values, and
   results come out as plain Go values (trees, points, images, process
   ids). The live UIA Elements the store keeps are an exception in
   *storage* only — they belong to the process MTA, they are AddRef'd on
   hand-out (finding 4) and every COM call on them still goes through
   the UIA thread.

3. **Every home is reached through its job channel**, is OS-locked for
   its lifetime (or for its one call, for the throwaway STA), and is
   never killed: a home wedged past its budget is abandoned and its
   thread leaked (findings 1, 2 and 5 govern the send/close/abandon
   discipline). No home ever pumps messages or owns a window, so none of
   them can host the overlay or the Esc hook.

4. **The UI thread stays COM-free** (ADR 0006/0007): the overlay and the
   Esc hook never touch COM, SendInput waits, or UIA.

5. A new COM need either joins the home that already owns that
   technology or gets its own ADR-justified home — "just initialise COM
   here" is not an option.

## Consequences

- A wedged provider, GPU or shell call leaks one thread per abandon —
  bounded by the request budgets, and unavoidable in Go (threads cannot
  be killed); the replacement worker carries on.
- `launch-app` spawns one extra goroutine per launch, which exits with
  the call; nothing long-lived is added.
- The throwaway STA is initialised with `COINIT_APARTMENTTHREADED` and
  uninitialised on the same thread, so it never coexists with the
  process MTA on one thread; a failure to initialise does not fail the
  launch (the call is attempted anyway — the shell works without COM in
  the common case).
- `RPC_E_CHANGED_MODE` on either MTA (a thread already initialised the
  other way) is tolerated per wgc.md §2 and uia-go §1 rather than
  treated as fatal.
