# 05: Routing and core page Actions

**What to build:** Actions route by Element origin. On page Elements, these run Headless over CDP:

- click, with actionability checks and the dialog race;
- set-value, with read-back;
- type-text and press-key: `Input.insertText` and `Input.dispatchKeyEvent` with modifiers;
- scroll: the Element forms, and `@x,y` on a page.

Every page Action shows the Agent Cursor through the CSS-to-screen transform and runs in the passive Esc scope. `--headed` keeps today's v1 behaviour.

See spec: "Routing and page Actions".

**Blocked by:** 04

**Status:** done

- [x] Offline: the routing table per command and origin, the actionability decisions, the transform maths, "nothing was sent" mapping
- [x] Batch: page steps count as Headless for the scope

## Comments

### 2026-11-02: probe decisions (accepted by the user)

From `../probe-results.md`:

- **Agent Cursor transform.** screen px = UIA document Element top-left + CSS rect x `window.devicePixelRatio`, both read fresh for each Action. Measured within 0.8 px at display scale 100%, 150% and 175% and page zoom 100%, 125% and 150%. Never use the monitor scale alone: it is off by 110 to 385 px once the page is zoomed.
- **Headless holds when covered or minimized.** `Input.insertText` and a `dispatchMouseEvent` click worked uncovered, fully covered and minimized, and the foreground never changed. No `--disable-backgrounding-occluded-windows` flag is needed.
- **Address bar.** Posted keys (text or Return) do nothing in Chrome's address bar, and set-value plus a posted Return does not navigate either. With a Browser Connection, type-text and press-key on the address bar are not a way to navigate; ticket 06's `navigate` is.

### 2026-11-02: landed

**What routes where.** An Element-taking Action first asks whether its target is a Page Element (`pageRoute`: the Window, the Element Index from an index or a `name:`/`id:` reference, then 04's `pageOrigin`). Anything that is not a Page Element, or does not resolve, falls through to the command's own path unchanged, so every failure message for UIA Elements is the same as before.

| command | UIA Element | Page Element | Page Element with `--headed` |
|---|---|---|---|
| click, any button, count 1 to 3 | today's routes | CDP mouse events, Headless | v1 click at the transformed screen point; every check runs before the activation |
| set-value | today's routes | `DOM.focus`, select all, `Input.insertText`, read back | refused before anything is activated, with "drop --headed" |
| scroll `<el> <dir> [n]` | today's routes | CDP `mouseWheel` at the Element's centre, Headless | v1 wheel at the transformed point, checks before the activation |
| scroll `<el> into-view` | today's routes | `DOM.scrollIntoViewIfNeeded`, read back | the same CDP scroll, after the activation |
| perform-secondary-action | today's routes | refused: "click it instead" (Raise: "activate-window") | refused |
| scroll `@x,y` on a point of the Page | (outside the Page: today's Headed wheel) | CDP `mouseWheel` at the inverse-transformed point, Headless | (no flag) |
| type-text, press-key | today's routes | when the Page has the focus: `Input.insertText` and `Input.dispatchKeyEvent`, Headless | v1 (`--headed` skips the probe) |

click `@x,y`, hover and drag are unchanged (Headed). hover and drag take no Element Index, so there is nothing to refuse until ticket 06.

**Per file.**
- `internal/page/input.go` (new): everything sent to a Page, and the checks before it.
  - `Origin.Aim(ctx, AimOptions{Scroll, Enabled, Visible, HitTest}) (Aim, error)`: `DOM.scrollIntoViewIfNeeded` (a node with no layout object is "hidden"), then one `Runtime.callFunctionOn` of `aimJS` in the node's own session. It returns the box, its centre, `devicePixelRatio` and a short name (aria-label, title, placeholder, label text or innerText; never a field's value), all in CSS px of the Tab's viewport. The checks: disabled (`:disabled`, `aria-disabled`, `[inert]`), hidden (visibility, display, zero size), centre outside the viewport, and covered: a hit test with `elementFromPoint` (through shadow roots) in the node's frame, accepting the node, its descendants and its own `<label>`. Then it walks up the same-origin frames, adding each iframe's content-box offset and hit-testing the iframe element at the point. A cross-origin iframe node (`Owner` set) adds the owner's content offset (04's `contentJS`) and runs `aimJS` on the owner at that point (covered there names the blocker in the Tab). A same-process frame the page cannot see into takes its offset from `DOM.getContentQuads`. Failures are `*NotActionableError{Why, Blocker}`: "it is covered by <div class="cookie-banner"> at its centre".
  - `Input` (`NewInput(ctx, client, tabSession, stopped, otherSessions...)`): subscribes to `Page.javascriptDialogOpening` on the Tab session and the node's session, runs `Page.enable` on each, and sends every event into the Tab session. `send` polls the Esc checkpoint first (`ErrStopped`), then races the call against a dialog event and a 5 s `EventTimeout`. A dialog gives `*DialogError{Type, Message}` and counts the event as delivered. A `*cdp.Error` reply gives `*SendError{Sent}` and the event as not delivered. No reply gives `*SendError{NoReply}`, counted as maybe delivered. `Sent` counts the events that went out.
  - `ClickEvents`/`Click`: `mouseMoved`, then `mousePressed` and `mouseReleased` per click with `clickCount` 1..n and the `buttons` mask. `WheelEvent`/`Wheel`: `mouseWheel` with deltas in CSS px. `InsertText`. `PressKeys`. `TypeText`/`TextParts`: text runs through `insertText`, `\n`, `\r` and `\r\n` as a real Return, and `\t` as a real Tab, like Headed type-text.
  - `KeyEvents(keys.Chord)`: all key downs in order, then the ups in reverse, with the CDP modifier bits held so far (Alt 1, Control 2, Shift 8). `namedKeys` maps winuse's canonical names to DOM key and code, and location for left, right and the numpad. Letters, digits, F-keys, numpad digits and the OEM punctuation map by virtual key, US layout, with Shift picking the shifted character. A key that types text gets `keyDown` with `text` and `unmodifiedText`. Without text, or with Control or Alt held, it gets `rawKeyDown`. `windowsVirtualKeyCode` and `nativeVirtualKeyCode` are winuse's VK.
  - `Origin.ReadField`, `Field.Refusal`, `Origin.SetValue(ctx, in, field, value)` and `ValueMatches`. `Origin.TopSession()`. `ReadFocus`/`Focus` (`focusJS`: the active element through same-origin iframes and shadow roots). `ReadDPR`.
  - `ToScreen(docLeft, docTop, Rect)`, `PointToScreen` and the inverse `ToCSS`: the transform, rounded to whole px.
- `internal/helper/pageactions.go` (new): the routing and the pipelines.
  - `pageRoute`, `pageDoc` (the first Page origin of the latest Window State: its Tab, session and `DocIndex`), and `pageFresh` (`pageTabCheck` plus 04's `checkPageOrigin`).
  - `pageTabCheck` asks `Services.Pages` for `PageActiveTab` (implemented on `*livePages` with `page.FindTab`). A switched Tab gives the UIA stale message. A fake without the method skips the check.
  - `runPage` (Element Actions) and `runPageInput` (typing, keys, a wheel at a point), with `pageFailure` mapping the errors.
  - `pageClick`, `pageSetValue`, `pageSecondaryRefusal`, `pageScroll`, `pageScrollAt`, `pageKeyTarget` plus `pageHasFocus`, `pageTypeText`, `pagePressKey`, and `pageStep` for the Batch.
- `CONTEXT.md`: the Headless Action and Headed Action definitions now say that Page Element Actions over a Browser Connection are Headless whatever the button or click count, and so are keys and typing while the Page has the focus, and a scroll at a point of the Page. No new ADR: ADR 0012 covers it.

**Decisions, with reasons.**
- **Every button and click count on a Page Element is Headless.** CDP needs no pointer and no foreground, so the reason double, right and middle clicks were Headed (SendInput) does not apply. `--headed` still gives v1.
- **The Headed click on a Page index no longer activates and then refuses (04's flaw).** It runs the freshness check, the scroll and every actionability check over CDP *before* `runAction`. Then, after the activation, it re-reads the document Element and clicks the real pointer at `PointToScreen`. The agent asked for the real pointer, so it gets one, and it lands where the Page says the Element is. The CDP hit test cannot see another OS Window covering the browser, but the activation takes care of that.
- **set-value `--headed` on a Page Element is refused before activation.** There is no UIA Element to write, and a real-keys fill would be a different Action. The refusal says to drop `--headed`.
- **perform-secondary-action on a Page Element is refused, not mapped to a click.** Toggle, Select, Expand and Collapse each have a target state, and a click toggles whatever the state is, so Expand on an open disclosure would close it. The hint names `winuse click <window> <element>` and says what a click does. Raise points at `activate-window`. Ticket 06's `check` and `select` can take these over.
- **set-value fires no synthetic events.** `Input.insertText` already fires trusted `beforeinput` and `input`, which React, Vue and plain listeners use. A synthetic `input` on top would be untrusted and fire handlers twice. `change` fires by itself when the focus leaves the field. use-browser fired both; we don't. An empty value is a Backspace over the selection, because `insertText("")` deletes nothing.
- **set-value accepts** textarea, contenteditable, and inputs other than button, submit, reset, checkbox, radio, file, image, range, color, hidden and the date and time types. The others refuse with "it takes no typed text" and the click hint. Disabled and read-only fields refuse before anything is sent. The read-back is exact for inputs. For contenteditable it normalizes nbsp and the trailing newline. A password mismatch reports only the lengths. The Agent Cursor label is `set value "<name>"` and never shows the value.
- **Key events follow Puppeteer's shape, not use-browser's.** A text key is one `keyDown` with `text`. use-browser sent a `keyDown` with text plus a `char`, which can type twice. With Control or Alt held there is no text, so Control+a reaches Blink's editing commands. Browser accelerators (Control+T, Control+L, Control+W) belong to the browser UI, not the renderer, so they do nothing over CDP.
- **Wheel step = 100 CSS px**, Chrome's own notch on Windows. Element scroll by steps sends one `mouseWheel` of steps x 100. `@x,y dx dy` converts wheel units (120 a notch) to the same 100 px a notch. There is no read-back for wheels: the wheel may scroll an inner container.
- **The keyboard's focus decision (`pageHasFocus`, pure).** The Page holds the focus when its active element is a field, any other element, or a cross-origin iframe. When it is the body, or there is nothing, keys and typing keep today's routes, and that includes the address bar. The probe runs only when the Window's latest Window State holds its Page and its Tab is still the active one (2 s budget), so other Windows pay nothing but the store scan. I did not use the UIA focus. Nothing reads it live outside a full Window State read, and with focus emulation on, `document.hasFocus()` is always true. See the live risks.
- **Refusals.** The Page route refuses only a locked desktop. CDP input works on minimized, covered and administrator Windows (probe section 4), so it has no Ready or integrity check. Every refusal before the first input event says "nothing was sent". That includes the actionability refusals that come after `scrollIntoViewIfNeeded`: a scroll is not input. Stale and switched Tabs use exactly the UIA stale message.
- **Dialog result.** A click whose handler opens a dialog is a failure (exit 1): `the click was delivered, and the Page opened a confirm dialog: "Leave this page?"`, with the hint `run winuse dialog W accept or winuse dialog W dismiss (a prompt takes its text: winuse dialog W accept <text>); do not repeat the click`. It fails so that a Batch stops: later steps would hang on the blocked Page. The cursor still ripples.
- **Esc.** Page Actions run in the passive scope. The checkpoints are after the cursor's travel (nothing sent yet: the "nothing was sent" stop) and before every input event (`ErrStopped`). After the act, `escStopBeforeActError` applies if no event went out, `escStopError` otherwise.
- **Batch.** `stepHeadless` asks `pageStep` first. Element forms on Page Elements without `--headed`, scroll `@x,y` on the Page, and unposted, unheaded press-key and type-text all count as Headless, so an all-Page Batch is one passive scope with no banner. For the keyboard steps, the Window's latest Window State must hold its Page and the Tab must be current, but the focus is *not* probed there: an earlier step (a click in a field) usually moves it. Each step still probes when it runs. If one ends up Headed, it arms its own swallowing scope, as headless-parity already allows.
- **Agent Cursor.** Target = `ToScreen(document Element's live top-left from HeadlessActor.ElementBounds(DocIndex), Aim.Rect)`. The label is the verb (`click`, `double click`, `right click`, `set value`, `scroll down`, `scroll into view`) plus the Element's name from the latest Window State (via `StoredElements`, falling back to the name `aimJS` reads), as `cursorLabel` writes it. For keys: `type`, or `press <chord>` at the focused element's transformed rect. For a wheel at a point: a small rect around the point.
- **No `third_party/agent-browser`.** Nothing was copied. `aimJS`, `fieldJS`, `selectJS`, `readJS` and `focusJS` are our own, written from the ideas only (scrollIntoViewIfNeeded, the box centre, mouseMoved before the press, name the blocker, the dialog race). 04 had not created the folder either.

**For ticket 06 to reuse.**
- **Dispatch a mouse sequence:** `page.NewInput(ctx, o.Client, o.TopSession(), esc.Stopped, o.SessionID)`, then `in.Click`, `in.Wheel`, or `in.send` through a new method for hover moves and drag (`mousePressed`, then interpolated `mouseMoved` with `buttons: 1`, then `mouseReleased`).
- **Dialog race:** every `Input` event already races. Ticket 06's dialog command should own a persistent `Page.javascriptDialogOpening` subscription, so other Actions can fail fast while a dialog is open. Today an Action started after a dialog is already open waits up to `EventTimeout` (5 s) and reports `SendError{NoReply}`: "a dialog may be open".
- **Actionability:** `Origin.Aim` with `AimOptions`. **Transform:** `page.ToScreen`, `PointToScreen`, `ToCSS`. **Helper pipeline:** `runPage` (an Element Action: set `check`, `aim` and `act`) and `runPageInput` (no Element), with `pageFailure`.
- **Key mapping:** `page.KeyEvents`, `describeKey`. **Focus:** `page.ReadFocus` and `pageHasFocus`. **Field writes:** `ReadField` and `SetValue`.

**Tests (offline).**
- `internal/page/input_test.go`:
  - the transform against 8 measured rows of the probe reports (100%, 150% and 175%, zoom 100 to 150), each within 1 px, plus the inverse round trip;
  - key mapping for 15 chords (modifiers, Shift characters, numpad, F-keys, Alt_R, Control+Shift+period), and every name maps to a DOM key and code;
  - `TextParts`; click sequences (double, right, middle);
  - `ValueMatches` (editable normalization, password lengths only); `Field.Refusal`;
  - `Aim` over a cdptest fake: ok, disabled, hidden, outside, covered with the blocker, gone, and checks not asked for;
  - `Aim` in a cross-origin iframe (offset, owner covered) and a same-process frame (quads);
  - `Input`: the click sequence, `Page.enable`, the dialog race with a held press (reported at once, nothing more sent), the Esc stop before the first event, a refused event (`Sent 0`), typing with a newline, no reply as `NoReply`;
  - `SetValue` (focus, select, insert; empty value as Backspace; password read-back); `ReadFocus`, `ReadDPR`, closed connection.
- `internal/helper/pageactions_test.go`, over a cdptest Page and the headless spy:
  - the routing table (17 rows: command x origin x `--headed`, including `name:`, double and right click, UIA rows unchanged), checking the spy steps, CDP input sent or not, no foreground seam, and the passive Esc scope;
  - the click sequence plus the cursor rect (115,230,190,245 from document 100,200 and CSS 10,20,50x10 at DPR 1.5) and the label;
  - the four actionability refusals in both modes (exact messages with "nothing was sent", no input, no activation, no ripple);
  - stale on navigation and on a switched Tab (the UIA message), Headless and Headed;
  - the dialog race message and hint;
  - the Headed click at the transformed point (153,238) after the activation;
  - set-value: the sequence, the label, the password mismatch without the value, and a checkbox refusal;
  - the Secondary Action refusals; scroll by steps, deltas per direction, into-view and its failed read-back;
  - `@x,y` inside the Page (inverse transform, 240 units = 200 px) and outside it (today's wheel);
  - keys: typing with a newline, Control+a, body and none falling back to v1, no Page falling back;
  - `pageHasFocus`; Esc at the travel and between events;
  - Batch: all-Page is passive, mixed is swallowing, and `pageStep` per kind.
- 04's `TestActionsRefusePageElements` now expects the click to go through the Page route; there, the Tab check fails with "nothing was sent".
- Checks run: `go build ./...`, `go vet ./...`, `go vet -tags e2e ./test/e2e` and `go test ./...` are all green. go.mod is unchanged. Every file I touched has LF endings.

**Shared files edited (minimal, each re-read first):**
- `internal/helper/actions.go`: 4 lines in `click`, routing a Page Element before the `@x,y` form.
- `internal/helper/setvalue.go`: 4 lines in `setValue` and 3 in `performSecondaryAction`.
- `internal/helper/scroll.go`: 4 lines in `scrollElement`.
- `internal/helper/pointer.go`: 4 lines in `scroll` (`@x,y`).
- `internal/helper/keyboard.go`: 6 lines each in `typeText` and `pressKey`, before the KeyRoute probe.
- `internal/helper/headless.go`: 3 lines at the top of `stepHeadless`. The tab worker's `tab-*` case is untouched.
- `internal/helper/pagestate_test.go`: the one click assertion in 04's refusal test, and its comment.
- `CONTEXT.md`: two sentences, the Headless and Headed Action definitions.
- `refs.go` and `pagestate.go` are unchanged. `refusePageElement` stays as the UIA side's guard, which the routed commands no longer reach.
- My interface is named `pageStoredElements`, to avoid a clash with `storedElements` in the tab worker's `tabs.go`.

**Live risks (unmeasured).**
- `DOM.getContentQuads` coordinates for same-process cross-origin frames under page zoom. They are used only when the frame chain is not script-visible.
- Input events sent to the Tab session reaching an OOPIF that has the focus, for set-value and typing after `DOM.focus` in a child session.
- A right click shows Chrome's native context menu on the user's screen, which may also take the foreground. Measure it, and consider `Input.dispatchMouseEvent` plus `Emulation`, or refusing the right click without `--headed`.
- The address bar. When the omnibox has the focus but the page still has a focused input, typing goes into the page input, not the omnibox. Real Chrome may also keep a page `activeElement` after the user clicks the omnibox. `--headed` reaches the omnibox.
- `Page.enable` re-emitting `javascriptDialogOpening` for a dialog already open (assumed no; there is a 5 s fallback). Whether a click blocked by a dialog leaves the button "pressed" in the renderer once the dialog closes, because the release is never sent.
- `scrollIntoViewIfNeeded` on the extension route, and `Input.*` through `chrome.debugger` (allowed by the API, but not measured).
- Covered-window and minimized behaviour for wheel and key events. The probe measured only `insertText` and a click.
- Mixed-DPI monitors: the transform uses the live document Element bounds, so it should hold, but it is unmeasured.
