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

Open for triage: Headless by default where the read-back exists, or opt-in with `--post` everywhere (the page sees focus/blur).

**Blocked by:** none (the ADR 0013 policy from 17 decides default vs `--post`)

**Status:** needs-triage (default vs `--post`; moves "keys into native Chromium without CDP" off the impossible list; ADR 0011 Decision 4)

- [ ] Chromium detection (top-level class and process) and the iconic refusal
- [ ] Activation, mouse, text and key message sequences; chords through attached key state
- [ ] Ctrl+V clipboard save and restore with the refusal rules
- [ ] Routing in type-text, press-key and click after the Browser Connection and before Headed; Batch predicate follows
- [ ] Offline tests: message sequences per step, surrogate pairs, the minimized refusal, the foreground-thread refusal, clipboard rules
- [ ] Opt-in e2e: a covered launched Chrome without CDP and the e2e Electron binary (`WINUSE_E2E_ELECTRON`), foreground unchanged
- [ ] SKILL.md, CONTEXT.md

## Comments

### 2026-11-03: created from the round 3 probes

Written for the "winuse multi-agent research" chat (690cfc8b), from measurements on the laptop (Chrome, Edge, Electron 45.2.1) in `../headless-gaps/probes-round3.md`. It overlaps 13 (caret-aware typing) only for Chromium-frame fields: there this route gives real key events, so 13 keeps WPF and WinUI.
