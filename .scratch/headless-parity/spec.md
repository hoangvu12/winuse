Status: ready-for-agent

# Headless parity with agent-desktop, and beyond

## Problem statement

The headless feature (ADR 0010) made Element Actions Headless, but winuse still takes the user's foreground, pointer or keyboard more often than it has to. The comparison with vercel-labs/agent-desktop (`agent-desktop-comparison.md` in this folder) found:

- Every `get-window-state --screenshot` and `zoom` activates the Window, even though Windows.Graphics.Capture and PrintWindow capture a covered Window without it.
- winuse has no Window management at all. agent-desktop moves, resizes, minimizes and closes Windows without activating them.
- Scrolling an Element by an amount, and scrolling an Element into view, need the real wheel.
- Toggle and Expand/Collapse have no Invoke fallback, so some web widgets that ignore the pattern call need `--headed`.
- Every key and every typed character is real input. That includes Enter in a dialog and typing into a classic edit box, which Windows can deliver without the foreground.

The user wants winuse to act as headlessly as possible: "it's better if it doesn't take my control".

## Solution

Make every Action that Windows can perform without the foreground Headless, and keep the rest Headed exactly as today.

- Screenshots and zoom capture without activating the Window.
- New Window Actions restore, minimize, move, resize and close a Window without activating it.
- `scroll` gains an Element form that scrolls through UI Automation by an amount, or into view.
- Toggle and Expand/Collapse fall back to Invoke when the pattern call does not change the state. `set-value` can set a check box on or off.
- `press-key Return` and `press-key Escape` on a classic dialog press its default or Cancel button through the dialog manager's own message, without the foreground.
- `type-text` into a focused classic edit control inserts the text through the control's own message, without the foreground.
- An opt-in `--post` option on `press-key` and `type-text` posts keystrokes to a background Window's focused control. It is best effort: modifiers do not work, and some apps ignore posted keys.

`--headed` still forces the v1 behaviour for every Action that has a Headless form.

## User stories

1. As a user, I want screenshots of a Window to leave my foreground alone, so that the agent can look while I work.
2. As a user, I want the agent to restore, minimize, move, resize or close a Window without bringing it to the front.
3. As an agent, I want to scroll a list or page by Element, by a number of steps, and know whether it moved, so that I don't need the real wheel.
4. As an agent, I want to scroll an Element into view headless before I act on it.
5. As an agent, I want Toggle, Expand and Collapse to still work on widgets that only respond to Invoke.
6. As an agent, I want `set-value <checkbox> on` to leave the box checked whatever its state was, so that I never toggle it the wrong way.
7. As an agent, I want `press-key <dialog> Return` and `Escape` to press the dialog's default or Cancel button without the foreground.
8. As an agent, I want `type-text` into a classic edit box (Notepad-style editors, Win32 dialogs) to type without the foreground.
9. As an agent, I want an explicit `--post` option to try keys and text on a background Window that has no Headless route, and a clear failure when posting cannot work (a chord with modifiers, an administrator Window).
10. As an agent, I want every Headless failure raised before anything was sent to say "nothing was sent", as today.
11. As an agent, I want `--headed` to give me exactly today's behaviour for every Action.

## Implementation decisions

ADR 0011 records the decisions below. CONTEXT.md is updated with them (Headless Action, Headed Action, Window Action, Posted Keys).

### Screenshots and zoom (ticket 01)

- `get-window-state --screenshot` and `zoom` no longer call `Activate`. The headless capture chain is Windows.Graphics.Capture, then PrintWindow (PW_RENDERFULLCONTENT). The screen-region method is left out, because it captures whatever is on top.
- A minimized Window is refused before capture, with a hint to run `restore-window` (ticket 02) or pass `--headed`. Nothing is captured.
- With `--headed`, today's path runs unchanged: activate, then the full chain (Windows.Graphics.Capture, screen region, PrintWindow).
- The protocol carries `headed` on get-window-state and zoom. The CLI forwards the global flag to them.
- Known limit: a popup outside the Window's bounds, such as a menu, is a Window of its own and is not in the capture. The skill says to screenshot it by its own Window ID.

### Window Actions (ticket 02)

New commands. Each is aimed at one Window and prints `ok`. None activates the Window, moves the pointer or sends keys.

- `restore-window <window>`: ShowWindow(SW_SHOWNOACTIVATE). It verifies the Window is no longer minimized. A Window that was maximized before it was minimized comes back at its normal size; Windows offers no non-activating maximize. The output says so in that case.
- `minimize-window <window>`: ShowWindow(SW_SHOWMINNOACTIVE), then verifies that it is minimized.
- `move-window <window> <x> <y>`: moves the frame's top-left to the screen point, in physical pixels.
- `resize-window <window> <width> <height>`: sets the frame size, in physical pixels.
  - Both use SetWindowPos with SWP_NOACTIVATE | SWP_NOZORDER | SWP_ASYNCWINDOWPOS, then read the placement back with an 8 px tolerance.
  - A maximized or minimized Window is refused, with a hint to restore it first.
- `close-window <window>`: PostMessage(WM_CLOSE), then waits up to 2 s for the Window to go away.
  - If it is still there, the command succeeds with a note: the close was requested, the Window is still open, and it may be asking to save. Read list-windows or the Window State next.
  - Nothing is ever terminated.
- All five refuse a locked desktop and an administrator Window (UIPI blocks posted messages and SetWindowPos), with "nothing was sent".
- list-windows and get-window lines gain nothing. A new `get-window <window> --bounds` is not added either: the agent reads the frame rectangle from the screenshot line instead. The screenshot line gains the Window's screen rectangle: `Screenshot: <path> (WxH px, scale S, window at X,Y WxH)`.
- They are not Batch step kinds: a Batch acts inside one Window.
- The Agent Cursor does not show Window Actions: the Window itself moves visibly.

### Element scroll (ticket 03)

- New form: `scroll <window> <element index | name: | id:> <up|down|left|right> [steps]`. `steps` defaults to 3 and must be between 1 and 50.
  - Headless: ScrollPattern.Scroll with SmallIncrement or SmallDecrement on one axis, `steps` times.
  - Use the Element's own ScrollPattern, else the nearest ancestor that has one (the Window State lists Elements inside a list or page, rarely the scroll container itself).
  - It verifies by the scroll percent on that axis. No change at all fails with "the Element did not scroll; it may already be at the end", marked delivered. A partial move is `ok`.
  - It fails "nothing was sent" when no Element up the ancestor chain scrolls on that axis.
- New form: `scroll <window> <element index | name: | id:> into-view`.
  - Headless: ScrollItemPattern.ScrollIntoView. It verifies that the Element's IsOffscreen property reads false within the verify wait.
  - It fails "nothing was sent" when the Element has no ScrollItem pattern.
- The `@x,y dx dy` form is unchanged and stays Headed.
- `--headed` with an Element form: activate, resolve the Element's point (as a Headed click does), then a wheel scroll of `steps` × 120 at that point. `into-view` with `--headed` scrolls the Element into view through the same ScrollItem call that a Headed click already uses, after activation.
- The routing predicate (`headlessStep`) treats the Element forms as Headless. The Batch picks this up automatically.
- Cursor labels are `scroll down "<name>"` and `scroll into view "<name>"`.

### Invoke fallbacks and set-value on toggles (ticket 04)

- Toggle (Secondary Action, and the click route's Toggle leg): the fallback fires when the Toggle call fails with a WriteError, or delivers but the state read back still equals the state before. If the Element offers Invoke and the state before was readable, call Invoke once and verify again.
- Expand and Collapse work the same way. Invoke fires only when the read-back state still equals the known state before the call (collapsed for Expand, expanded for Collapse).
- The fallback fires at most once per Action and never when the state before was unreadable.
- `set-value <element> on|off` works on an Element with the Toggle pattern and no Value or RangeValue pattern. The accepted values are `on`, `off`, `true`, `false`, `checked` and `unchecked`, case-insensitive.
  - The call is idempotent: if the state already matches, nothing is sent and the output is `ok`.
  - Otherwise it toggles, at most twice, because a three-state box can pass through indeterminate. It verifies the target state.
  - Headless by default, like every set-value. With `--headed`, it activates first, then makes the same pattern calls.

### Headless Return and Escape in classic dialogs (ticket 05)

- `press-key <window> Return` (also KP_Enter) and `press-key <window> Escape` are Headless when the target Window is a classic dialog (window class `#32770`) and the key has no modifiers. Otherwise press-key is Headed, as today.
  - Return: SendMessageTimeout(DM_GETDEFID). With DC_HASDEFID set, PostMessage(WM_COMMAND, MAKEWPARAM(id, BN_CLICKED), the button's HWND) goes to the dialog. This is the message the dialog manager sends when the user presses Enter.
  - Escape: PostMessage(WM_COMMAND, MAKEWPARAM(IDCANCEL, BN_CLICKED), the IDCANCEL control or 0).
  - Return with the focus on an enabled push button presses that button, as the dialog manager does.
  - These route Headed as today: Return with no default button, Return with focus in a multi-line edit that wants Return (ES_WANTRETURN), and Escape with a disabled Cancel button. The routing decides this before anything is sent.
  - The output is `ok`. There is nothing readable to verify.
- The Agent Cursor travels to the default (or Cancel) button's rectangle with the label `press Return "<button text>"`.
- The routing for press-key and type-text depends on the live Window, unlike click. The routing predicate gains a Window probe (`Services.KeyRoute`, faked offline). The Batch probes each keyboard step after its Window resolves and before its scope is picked. Each step probes again when it runs, because the focus can change between steps. Either route is safe inside either scope.

### Headless typing into classic edit controls (ticket 06)

- `type-text <window> <text>` is Headless when the target Window's focused control is a classic edit control and the text contains no control characters (`\n`, `\r`, `\t` or other C0 characters). Otherwise it is Headed, as today.
  - The focused control is GetGUIThreadInfo(the Window's thread).hwndFocus, and it must be the Window or a descendant of it.
  - Classic edit control means class `Edit`, `RichEdit20A`/`RichEdit20W`, `RICHEDIT50W` or `RichEditD2DPT`, not read-only (ES_READONLY) and enabled.
- The Headless route is SendMessageTimeout(EM_REPLACESEL, TRUE, text) to that control, with a 2 s timeout. This replaces the selection, or inserts at the caret, exactly like typing, and it is undoable.
- It verifies through WM_GETTEXTLENGTH. The length after must equal the length before, minus the selection length (EM_GETSEL), plus the text length in UTF-16 units. A mismatch is a VerifyError ("delivered; do not repeat"). A password edit (ES_PASSWORD) is verified the same way and never echoed.
- The Agent Cursor travels to the control's rectangle with the label `type`. It never shows the text, which could be a password.

### Posted keys, opt-in (ticket 07)

- `press-key <window> <key> --post` and `type-text <window> --post <text>`. On type-text, `--post` must come right after the Window, because everything after it is the text.
  - They post WM_KEYDOWN and WM_KEYUP (press-key), or WM_CHAR per UTF-16 unit (type-text), to the Window's focused control (GetGUIThreadInfo hwndFocus, falling back to the Window itself).
  - A newline, a carriage return, or a CRLF pair is posted as one carriage return, which is what the Enter key types.
  - There is no foreground change and no SendInput.
- press-key `--post` refuses a chord with modifiers (Control, Alt, Shift, Super), with "nothing was sent". Posted messages do not change the keyboard state that apps read with GetKeyState.
- A posted message has no readable outcome. The output is `ok (posted; the app may ignore posted keys, read the Window State to check)`.
- It refuses an administrator Window and a locked desktop with "nothing was sent". Like every Headless Action, it also refuses a Window blocked by a modal dialog or minimized, because a posted key would land in the disabled owner.
- `--post` and `--headed` together are a usage error.
- Batch steps accept `--post` on press-key and type-text. A step with `--post` counts as Headless for the Batch's scope.
- The Agent Cursor shows the Window's focused control with the label `press <key>` or `type`.
- The CONTEXT.md term is **Posted Keys**. They are not Headless Actions: Headless means a route that winuse can deliver reliably. They are not Headed either.

### Everything else

- No Action switches mode after something was sent. The routing for press-key and type-text is decided from the live Window before anything is sent, and the CLI never retries in the other mode.
- Esc stop: the Headless and Posted forms run in the passive Esc scope, like the other Headless Actions.
- Every new pattern call runs with AutoSetFocus FALSE, already set client-wide.

## Testing decisions

- Offline, in the default `go test ./...` run:
  - the routing per Action, flag and Window probe;
  - the CLI argument parsing for every new form;
  - the failure mapping ("nothing was sent", delivered, may have taken effect);
  - the capture chain without activation;
  - the Window Actions' read-back logic;
  - the scroll verification maths;
  - the toggle state machine for `set-value on|off`;
  - the dialog key routing;
  - the EM_REPLACESEL length check;
  - the posted-key chord refusal.
  All through fakes, as the existing helper tests do.
- Opt-in e2e (`WINUSE_E2E=1`, run by the user only), against the fixture app and Notepad where noted, with another Window covering the target:
  - a screenshot without a foreground change;
  - restore, minimize, move, resize and close without a foreground change;
  - Element scroll and into-view on the fixture's list;
  - Return and Escape on a fixture message box;
  - type-text into the fixture's edit box;
  - posted Return into a covered Chrome address bar, reported as a measurement, not an assertion.
  The tests are written and compiled (`go vet -tags e2e ./test/e2e`), never run by an agent.

## Out of scope

- The CDP browser backend (the browser-merge folder). Posted keys are the stopgap for browsers until then.
- A non-activating maximize (Windows has none).
- Headless hover, drag, and double, right or middle clicks. They need the real pointer.
- Posting chords with modifiers.
- Headless typing into WPF, WinUI or Chromium text fields. They have no window handle per control; `set-value` covers them.
