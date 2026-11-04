# 03: The Agent Cursor shows the Action after it happened

Status: done

**Evidence.** Reported by the user: clicking the browser's search bar, the
click has happened about 200 ms before the cursor's animation shows it.

**Root causes (code).**

1. **The ripple waits for the whole act.** Every pipeline glides the cursor
   (`Travel`, which blocks until it lands), runs the act, and only then calls
   `cursorClick`:
   - `internal/helper/headless.go` `runHeadless`: the act is the UI Automation pattern call *and its read-back* (`uia.VerifyError`), often 100–300 ms on Chromium.
   - `internal/helper/pageactions.go` `runPage` / `runPageInput`: the act opens `page.NewInput` (a `Page.enable` round trip per session), then sends mouseMoved, mousePressed and mouseReleased, each waiting for the Page's answer, then reads back where the Action has one. The press reaches the Page well before the ripple.
   - `internal/helper/pagecommands.go` `runPageWindow`: navigate's act sends `Page.navigate` and then **waits for the new document to load**; the ripple on the address bar comes seconds after the navigation started.
2. **The label appears after the click.** `cursorModel.labelReveal` (`internal/win/cursor_motion.go`) holds a new label until the travel lands, then fades it in over 180 ms. `Travel` returns at landing, so the Action runs while the label is still invisible: the user reads `click "Search"` after the click.

Not the cause: the travel itself is ordered before the act and its bound
(twice the estimated duration plus 50 ms) only matters when the UI thread is
stuck; the timer runs at the system tick (about 16 ms).

**Fix.**

- `page.Input.OnDelivered`: called once when the first event that acts (anything but a bare mouseMoved) is accepted by the Page. `runPage` and `runPageInput` ripple there.
- `runPageWindow`: acts call `markDelivered(ctx)` right after the browser accepts the request (Page.navigate, history move, reload, dialog answer, file set), and the ripple plays then; the post-act ripple stays as the fallback for an act that does not mark.
- `runHeadless` (UI Automation): unchanged; see the comment below for why.
- `cursorModel`: a new label starts revealing `cursorLabelReveal` before the travel lands (from the start for a shorter travel), so it is whole on landing.

**Measuring.** The helper logs the cursor's timeline when `WINUSE_CURSOR_TRACE=1` is set in its environment: travel start, landing, delivery and ripple, in milliseconds since the Action began.

## Comments

Done on branch browser-ux for the label (`internal/win/cursor_motion.go`) and the Page routes (`page.Input.OnDelivered`, `rippleOnDelivery`, `markDelivered` for navigate/back/forward/reload). The UIA route keeps the ripple after the act: an act may refuse with nothing sent and tests pin that a refusal never ripples; a click's Invoke has no read-back, so it already follows the delivery closely. Moving it earlier for Toggle/Select/Expand needs a delivered hook in `internal/uia`'s write paths: follow-up. `WINUSE_CURSOR_TRACE=1` logs the timeline (`internal/helper/cursortrace.go`). Tests: `TestLabelRevealIsWholeOnArrival`, `TestPageClickSendsTrustedInputAndShowsTheCursor` (ripples when each release arrived). Not measured live.
