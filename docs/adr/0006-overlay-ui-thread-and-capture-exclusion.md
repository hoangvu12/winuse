# ADR 0006: The overlay lives on a locked UI thread and is hidden around captures

## Status

Accepted (ticket 13 — Codex-style overlay).

## Context

The spec (Control) requires a Codex-style on-screen indicator while an
Action or Batch is running: click-through, never focusable, and excluded
from winuse's own screenshots. The process model also assigns the helper
an Esc keyboard hook on its own locked thread with a message loop, and
win32.md §8/§9 adds two hard facts:

- Low-level keyboard hooks are called on the installing thread while it
  pumps messages, and that thread must never block inside SendInput (every
  injected event passes the hook synchronously) or UI Automation. The
  hook and the overlay should share one "UI thread".
- It is **unverified** whether `SetWindowDisplayAffinity`
  (`WDA_EXCLUDEFROMCAPTURE`) also hides a layered window from GDI
  screen-region BitBlt — winuse's screenshot fallback path — and
  pre-2004 Windows degrades the value to `WDA_MONITOR`, which composites
  a black box into captures instead of excluding the window.

Codex's own overlay is a full-screen layered window whose pill is
excluded from capture via `WDA_EXCLUDEFROMCAPTURE`, and it lingers until
the agent's whole turn ends (a known bug).

## Decision

1. **One UI thread in the helper** (`internal/win/uithread.go`): an
   OS-locked thread with a `GetMessage`/`DispatchMessage` loop. It owns
   the overlay window today; ticket 12 installs the Esc low-level
   keyboard hook on the same thread. Other goroutines reach it through
   posted jobs with bounded waits (2 s) — a wedged UI thread never hangs
   an Action or a capture. Jobs must stay quick and must never inject
   input or touch UI Automation.

2. **The overlay window** (`internal/win/overlay.go`): a `WS_POPUP`
   window with `WS_EX_LAYERED|WS_EX_TRANSPARENT|WS_EX_TOPMOST|
   WS_EX_TOOLWINDOW|WS_EX_NOACTIVATE`, magenta colour-key transparency
   (magenta reads as fully transparent; hit-testing passes through
   transparent pixels), and `WM_NCHITTEST → HTTRANSPARENT` as belt and
   braces. It draws a 4 px accent border framing the target Window and a
   dark pill banner — "winuse is in control — press Esc to stop" — at the
   top centre, with GDI, text only on the solid pill (colour-key fringes
   otherwise). Created hidden at helper start, positioned over the
   target Window's extended frame bounds on each Show.

3. **Shown for the duration of every Action**: the shared Action pipeline
   (`runAction` in `internal/helper/actions.go`) raises it after the
   pre-input refusals (locked desktop, administrator Window — winuse is
   not in control when it refuses) and before the activation, and hides
   it afterwards whatever the outcome. Observation commands never show
   it. The indicator is best-effort: a Show/Hide failure is logged and
   the Action proceeds. Ticket 11's Batches bracket themselves through
   the same pipeline; if they want one bracket for a whole Batch instead
   of one per step, that is their choice to make.

4. **Exclusion from winuse's own screenshots is deterministic**: every
   capture method in the chain runs inside `Overlay.HideWhile` — hidden,
   captured, restored (only if it was shown). `WDA_EXCLUDEFROMCAPTURE`
   is still applied, best-effort, for *other* software's captures, with
   no `WDA_MONITOR` fallback (a black box in a recording is worse than a
   visible indicator).

5. The overlay stays out of winuse's own window listings and capture
   regions by the existing `OwnProcess` rule (windows of the helper's own
   exe) and the tool-window style.

## Consequences

- winuse's screenshots never contain the indicator regardless of Windows
  version or display-affinity behaviour; third-party capture excludes it
  only where the OS supports it.
- A capture taken while an Action runs briefly hides the indicator —
  a sub-frame flicker, accepted, because captures during Actions are rare
  (the e2e test does it on purpose).
- Ticket 12 gets a ready thread: it installs
  `SetWindowsHookExW(WH_KEYBOARD_LL)` as a job, keeps the callback to a
  few comparisons plus a non-blocking channel send (the hook is silently
  removed if it stalls), and never calls SendInput or UIA from the
  thread.
- The paint code is exercised only by the user-run e2e tests (the same
  split as every other Win32 side in the repo); the offline tests cover
  the layout functions, the thread's job mechanics, and the helper-level
  brackets with fakes.
