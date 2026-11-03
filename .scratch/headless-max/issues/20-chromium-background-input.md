# 20: Background typing, keys and clicks for Chromium and Electron without CDP

**What to build:** `type-text`, `press-key` and `click` into a Chromium or Electron Window (the user's own Chrome, Edge or Brave without the extension, or any Electron App) without the foreground and without a Browser Connection. Today these go Headed. The route is agent-desktop PR #400 (open, not merged), measured in `../headless-gaps/probes-round3.md`.

Route (SendMessageTimeout with SMTO_ABORTIFHUNG, all to the top-level `Chrome_WidgetWin_1`):

1. Fake activation: WM_NCACTIVATE(1), WM_ACTIVATE(WA_ACTIVE), WM_SETFOCUS.
2. Click: WM_MOUSEMOVE, then button down and up in client coordinates (per-monitor DPI aware).
3. Text: per UTF-16 unit, WM_KEYDOWN(VK_PACKET), WM_CHAR, WM_KEYUP. Enter: VK_RETURN (scan 0x1C) with a CR WM_CHAR. Tab: VK_TAB (scan 0x0F) with a tab WM_CHAR.
4. Chords (Ctrl+A, Shift+Tab): AttachThreadInput plus SetKeyboardState on Chrome's UI thread around the key, restored and detached on every exit path. Shares the mechanism with ticket 07.
5. Undo the activation: WM_KILLFOCUS, WM_ACTIVATE(WA_INACTIVE), WM_NCACTIVATE(0).

Rules:

- **Never on a minimized Window.** The activation messages make Chromium restore itself and take the foreground. Refuse with "nothing was sent" and the `restore-window` hint.
- **Never UIA SetFocus** (it takes the foreground for any App).
- Refuse when Chrome's UI thread owns the foreground (attaching would share the user's key state).
- Ctrl+V only with clipboard save and restore: text-only HGLOBAL formats; refuse on GDI, OLE or delayed formats; set the temporary data with the `ExcludeClipboardContentFromMonitorProcessing` format; check GetClipboardSequenceNumber before restoring. Prefer typing the text instead.
- Read back through the UIA Value pattern where the field has one. Otherwise the output says it was not verified.
- Limits stated in the output and the skill: the page sees a focus/blur pair around each step; WebView2 (a child window) is not covered.

Automatic, no `--post` (user decision, see Comments).

**Blocked by:** none

**Status:** done (live e2e not run yet: see the 2026-11-03 implementation comment)

- [x] Chromium detection (top-level class and process) and the iconic refusal (by class only: see the comment)
- [x] Activation, mouse, text and key message sequences; chords through attached key state
- [x] Ctrl+V clipboard save and restore with the refusal rules (not built: no paste route, see the comment)
- [x] Routing in type-text, press-key and click after the Browser Connection and before Headed; Batch predicate follows
- [x] Offline tests: message sequences per step, surrogate pairs, the minimized refusal, the foreground-thread refusal, clipboard rules (no clipboard rules to test)
- [x] Opt-in e2e: a covered launched Chrome without CDP and the e2e Electron binary (`WINUSE_E2E_ELECTRON`), foreground unchanged (written and vetted, not run)
- [x] SKILL.md, CONTEXT.md

## Comments

### 2026-11-03: created from the round 3 probes

Written for the "winuse multi-agent research" chat (690cfc8b), from measurements on the laptop (Chrome, Edge, Electron 45.2.1) in `../headless-gaps/probes-round3.md`. It overlaps 13 (caret-aware typing) only for Chromium-frame fields: there this route gives real key events, so 13 keeps WPF and WinUI.

### 2026-11-03: triage decision (user)

Automatic: no `--post`. Read back through UIA Value where it exists; where it does not, the output says the text was not verified. ADR 0013 records this as an exception to its opt-in rule. The laptop probe also checks double and right clicks through the sent mouse messages.

### 2026-11-03: double and right clicks measured (`../headless-gaps/probes-round4.md` section 3)

- Sent WM_LBUTTONDOWN/UP/DBLCLK/UP and WM_RBUTTONDOWN/UP in client coordinates gave trusted `dblclick` and `contextmenu` (with `auxclick`) in Chrome and Electron, with the foreground and cursor unchanged.
- **Mouse messages need no activation pair. Send clicks without it**, so the page sees no focus/blur. Keys still need it.
- `click --count 2` and `click --button right` on a Chromium or Electron Window without a Browser Connection go through this route, automatically, as the user decided.
- A message right click does not open Chrome's own page menu (nothing appeared within 300 ms). The page's contextmenu handler runs. To open Chrome's native menu, use ticket 09's ShowContextMenu route.

### 2026-11-03: implemented (branch hm20-chromium-messages)

- Win32 (`internal/win/chromium.go`): everything is sent with `SendMessageTimeout(SMTO_ABORTIFHUNG)`, 500 ms per message, to the top-level `Chrome_WidgetWin_1`.
  - Text: the activation pair (WM_NCACTIVATE 1, WM_ACTIVATE WA_ACTIVE, WM_SETFOCUS; undone with WM_KILLFOCUS, WM_ACTIVATE WA_INACTIVE, WM_NCACTIVATE 0 on every exit path) around WM_KEYDOWN(VK_PACKET), WM_CHAR, WM_KEYUP per UTF-16 unit, so surrogate pairs are two units. A newline is VK_RETURN (scan 0x1C) with CR, CR LF counts as one, and a tab is VK_TAB (scan 0x0F) with a tab. The Esc stop and the deadline are checked between units.
  - One key: the key down, the WM_CHAR a real keyboard would add (Return, Tab, BackSpace, Escape, space, letters, digits, numpad digits), and the key up, inside the pair.
  - Clicks: ticket 12's `clickMessages` (move, down/up, DBLCLK for the second click, WM_CANCELMODE) in client coordinates, sent with no activation pair, as round 4 measured.
  - Keys and text refuse when the Window's thread owns the foreground (`ErrChordForeground`).
  - Chords reuse ticket 07's `PlanPostedChord` and `PostChord` with the Window as target. The plan asks for the activation pair for `Chrome_WidgetWin_*`, and ticket 07 records that Chromium chords through this state and pair were measured in round 3.
- Detection: by the top-level class `Chrome_WidgetWin_1` only, which covers Chrome, Edge, Brave and Electron. No process check was added: the class is what the route needs, and WebView2 hosts are other classes, so they are not covered, as the ticket says.
- UIA read-back (`internal/uia/focusvalue.go`): `FocusedValue` walks the stored live Elements of the Window's latest Window State for the one whose live HasKeyboardFocus is true, and reads its Value pattern; a password field is never read. `VerifyTyped` polls for up to 1 s. If the value contains the typed text (newlines normalized), the output is `ok (typed as window messages; the page saw a focus and blur around it)`. If no focused field with a value is found, it is `ok (typed as window messages, not verified: ...; read the Window State to check)`. A read value without the text fails with "delivered; do not repeat it".
- Helper (`internal/helper/chromium.go`), one route reached three ways:
  - type-text: after the Page route and the classic edit route, before ticket 21's refusal.
  - press-key: after the Page, shortcut and dialog routes, before ticket 21's refusal.
  - click: double, triple, right and middle clicks at a point (the screenshot mapping) or on an Element (the centre of its live bounds), before ticket 21's refusal. A left single click keeps its UIA routes.
  - All three use the Headless refusals (minimized among them: Chromium would restore itself), a passive Esc scope and the Agent Cursor, and the route is traced as Headless.
  - The thread-owns-foreground refusal sends nothing and gives the `--headed` retry. Nothing sent says so; a partial send "may have taken effect".
  - A Blocked Combo is never sent as messages, `--force` or not: it falls back to ticket 21's refusal.
  - The Batch predicate (`chromiumStep`) counts unheaded, unposted keys, plain text and non-left-single clicks on a Chromium Window as Headless steps.
- Ctrl+V clipboard save and restore: not built. winuse types through VK_PACKET and never pastes, so it never touches the clipboard. An agent's own `press-key Control_L+v` pastes the user's clipboard as the agent asked. The refusal rules belong to a paste route, and none exists.
- Behaviour change pinned in tests: in a browser Window whose Page does not hold the focus (`TestPageKeys`) or that has no Page in its Window State, plain type-text and press-key now take this route instead of ticket 21's refusal. So does a double click on a frame Element (`TestBatchPageStepsArePassive`).
- Offline tests:
  - win: `chromium_test.go` covers the unit sequence, Return and Tab, surrogate pairs and CR LF, key characters per key, the activation pair's order, and the live guard.
  - uia: `focusvalue_test.go` covers the verdicts.
  - helper: `chromium_test.go` covers read-back shown, missing and unverified (with and without a FocusReader); one key and a chord (activation planned); a Blocked Combo refused even with --force; double and right clicks at their points; the minimized refusal; the foreground-thread refusal with the exact retry; nothing sent versus partial; a non-Chromium Window left alone; and a Batch of Chromium steps in the passive scope.
- e2e (vet only): `TestChromiumMessagesOnACoveredBrowser` and `TestChromiumMessagesOnACoveredElectron` (`WINUSE_E2E_ELECTRON`) start the browser or Electron on the fixture page with no debugging port, cover it, then double click the input, type, press Ctrl+A and type again. They check the Window State's value and that the cover stays the foreground.
- Not measured, for the live run:
  - whether `FocusedValue` finds Chromium's focused input while the Window is in the background (if not, the output says "not verified");
  - Edge's and Brave's top-level class (Chromium's own, `Chrome_WidgetWin_1`, expected).
- Docs: SKILL.md (the keyboard routes and the Headed list), CONTEXT.md (Headless Action), codex-parity (the press-key and type-text rows), the ADR 0013 table (done), test/e2e/README.

### 2026-11-03: a failure after the activation pair is uncertain (branch chromium-uncertain)

- A key or text send that failed with no key message sent was reported "nothing was sent", though the activation pair had already gone out: the page saw a focus change, and a Chromium menu could close on it (hm-09's comment of the same day). win now marks such a failure ErrAfterActivation (withActivation, and PostChord's pair), keeping its message; runChromium and the Posted Keys route keep "nothing was sent" only for a failure before anything went out, and otherwise give the uncertain hint ("may have taken effect", dp-07's delivery_uncertain). Offline: TestAfterActivationMarksTheFailure, TestChromiumRefusals (type-text and press-key after the pair), TestPostChordFailureAfterActivationIsUncertain.
