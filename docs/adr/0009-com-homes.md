# ADR 0009: One COM home per technology — UIA MTA, WGC MTA, a throwaway STA for the shell

## Status

Accepted (the winuse-v1 review). Amends ADR 0003's "one goroutine for
all COM".

Amended by ADR 0014 (multi-agent ticket 07, landed): the one UIA MTA
thread is a pool of UIA MTA workers with per-App affinity. See
"Amendment: the UIA worker pool" below; Decision 1 reads "one COM home
per technology" with UI Automation's home being that pool.

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

## Amendment: the UIA worker pool (ADR 0014, multi-agent ticket 07)

With several agents at once, one UIA thread let one hung App stall every
agent: a provider that does not answer holds the thread until the
request's budget abandons it. Probes round 3
(`.scratch/headless-gaps/probes-round3.md`) measured that, with one App
hung, other MTA threads still clicked a healthy App in 17 ms (sharing one
IUIAutomation) and 210 ms (each with its own), and that the hung App's
own ElementFromHandle returned UIA_E_TIMEOUT after about 1 s, never
delivering the click late.

1. **UI Automation's home is a pool** of up to four UIA MTA workers
   (`internal/uia/pool.go`, `PoolSize`), each exactly the worker Decision
   1 described: OS-locked, MTA, no window, no pump, its own
   IUIAutomation2 with the 1 s connection timeout and AutoSetFocus off.
   A worker starts on first use, so a helper that reads one App runs one.
2. **Affinity by App.** Every UIA call names its Window, and the Window's
   App (its process id; a UWP App's content process rather than
   ApplicationFrameHost, `win.AppPID`) is bound to one worker: all of an
   App's calls run there, in order. A new App is bound to the worker with
   the fewest Apps that have a job in flight (a hung App's wedged job
   counts, so new Apps avoid its worker), then the fewest Apps. A binding
   idle for 10 minutes with nothing in flight is dropped when a new App
   is bound. Calls that name no Window share one binding.
3. **Abandon and replace per worker.** Decision 3's discipline is
   unchanged and applies to each worker alone: a job that outlives its
   budget abandons its worker (the thread is leaked), and the next call
   bound to that slot starts a replacement; the other workers carry on.
4. **Workers start one at a time** (`workerInit`): the first UI Automation
   call is not re-entrant across threads, so each worker initialises COM
   and creates its IUIAutomation2 alone, and takes work only after.
5. **Decision 2 still holds.** No interface pointer crosses a job
   boundary. The store's live Elements belong to the one process MTA,
   which every worker joined, so any worker may Release them; a held
   Element is read on its Window's App's worker and released on the
   worker of the calls that name no Window, so a wedged worker cannot hold
   a release up.
6. **A hung pre-check guards Actions** (`internal/helper/hung.go`,
   `win.WindowHung`): once a command holds its lanes and passes the Window
   identity check, an Action that reaches the Window's UI thread asks
   IsHungAppWindow, then sends a WM_NULL with SMTO_ABORTIFHUNG and a
   300 ms timeout (SendMessageTimeout waits the full timeout even when
   Windows already marks the App hung, so it is short). A hung App is
   refused: "window N's App is not responding; nothing was sent". Page
   Actions, which reach the Page through the browser, and reads are not
   checked.

Consequences of the amendment:

- A hung App stalls only the agents working in it, and only for its own
  calls' budgets; the hung App's reads still answer when UIA times out.
- More Apps than workers share workers, so two Apps on one worker still
  wait for each other's calls. Four keeps the thread count small; the
  pool size is a constant to raise if that shows up.
- Reads of different Apps now run at once; reads of one App stay in
  order on its worker. The store and the thin-Window memory were already
  guarded by their own mutexes.
- A busy App that needs more than 300 ms to answer a WM_NULL is refused
  as not responding; the refusal sent nothing, so a retry is safe.

