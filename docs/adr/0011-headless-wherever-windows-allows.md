# ADR 0011: Headless wherever Windows allows it; Posted Keys are opt-in

## Status

Accepted (headless parity spec, `.scratch/headless-parity/spec.md`).
Amends ADR 0010. It reverses the v1 spec's "capturing the Window's
screen region (after it is in the foreground)" and the headless spec's
out-of-scope "Headless typing".

## Context

ADR 0010 made Element Actions Headless and kept every other Action
Headed. A comparison with vercel-labs/agent-desktop
(`.scratch/headless-parity/agent-desktop-comparison.md`) showed that
winuse still takes the foreground where Windows does not need it:

- Screenshots: Windows.Graphics.Capture and PrintWindow capture a
  covered Window.
- Window placement: SetWindowPos with SWP_NOACTIVATE, ShowWindow with
  SW_SHOWNOACTIVATE or SW_SHOWMINNOACTIVE, and a posted WM_CLOSE.
- Scrolling: ScrollPattern and ScrollItemPattern.
- Classic dialogs: they answer Enter and Esc through a WM_COMMAND that
  the dialog manager sends itself.
- Classic edit controls: they insert text through EM_REPLACESEL.

The user asked for everything to be as headless as possible.

## Decision

1. Observation never activates. `get-window-state --screenshot` and
   `zoom` capture through Windows.Graphics.Capture, then PrintWindow.
   The screen-region method runs only under `--headed`.
2. Window Actions (restore, minimize, move, resize, close) are Headless.
   They use only non-activating window calls and a posted WM_CLOSE.
3. Element scroll (by steps, or into view) is Headless through
   UI Automation. Scroll at `@x,y` stays Headed.
4. Two Actions route by the live Window, not by their arguments alone.
   `press-key Return|Escape` on a classic dialog and `type-text` into a
   focused classic edit control are Headless. Anywhere else they stay
   Headed, as before. The route is decided from the Window before
   anything is sent. "winuse never switches mode on its own" now means:
   never after something was sent.
5. Posted Keys (`--post`) are a third kind, neither Headless nor
   Headed. They post keyboard messages to a background Window's focused
   control. They are opt-in per command, because Windows does not
   promise that an app handles them, and posted messages leave the
   modifier state untouched (chords are refused).

## Consequences

- An agent can no longer tell from the command alone whether a
  `press-key` or `type-text` will take the foreground. The skill teaches
  the rule. A Batch asks the same Window probe before it picks its Esc
  scope.
- A headless screenshot shows only the Window itself. A menu or popup
  outside its bounds needs its own screenshot.
- Restoring a Window that was maximized before it was minimized brings
  it back at its normal size: Windows has no non-activating maximize.
- `close-window` only requests the close. An app that asks to save
  leaves its Window open, and the agent reads what it asks.
- Posted Keys can do nothing and still report `ok`. The output says to
  check the Window State.
- Everything that UIPI blocks (posted messages, SetWindowPos, pattern
  writes) refuses an administrator Window up front with "nothing was
  sent".
