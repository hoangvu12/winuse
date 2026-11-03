# 12: Posted Clicks (opt-in)

**What to build:** `click ... --post` posts mouse messages to the classic Win32 control under the target, for double, right and middle clicks on an Element and for clicks at a point. Best effort, like Posted Keys.

Measured (`../headless-gaps/headless-100.md` G1): posted WM_LBUTTONDOWN/UP/DBLCLK/UP to a background `Edit` selected a word; posted WM_RBUTTONDOWN/UP opened its context menu; foreground unchanged both times. The same messages to charmap's `RICHEDIT50W` did nothing. cua uses this as its Win32 background route (`platform-windows/src/input/mouse.rs:138-200`, MIT).

- Target HWND: the deepest child of the target Window at the point, from `ChildWindowFromPointEx` (skip invisible and transparent), never `WindowFromPoint`, so a covered Window is still addressed.
- Messages: WM_MOUSEMOVE first, then the button sequence for the button and count (DBLCLK for the second click of a double), client coordinates, MK_* flags in wParam. WM_CANCELMODE afterwards to drop a stale capture.
- Only for HWND-backed classic controls (class allow-list: Edit, RichEdit*, SysListView32, SysTreeView32, Button, ComboBox, ListBox, `#32770` children, MFC/WinForms classes as measured). Anything else: "nothing was sent" with the `--headed` hint.
- Output: `ok (posted; the app may ignore posted clicks, read the Window State to check)`.
- Refuse administrator Windows, a locked desktop, a modal-blocked or minimized Window, as Posted Keys do.
- `--post` and `--headed` together are a usage error. Batch steps accept `--post`.

**Blocked by:** none (shares the point mapping with 11)

**Status:** done (live e2e not run yet: see the 2026-11-03 implementation comment)

- [x] Win32 layer: child at point, posted sequence, WM_CANCELMODE
- [x] Class allow-list and refusals
- [x] CLI and Batch `--post` on click
- [x] Offline tests: message sequence per button and count, allow-list, flag conflicts
- [x] Opt-in e2e: fixture edit box double and right click, covered (written and vetted, not run)
- [x] CONTEXT.md, SKILL.md

## Comments

### 2026-11-03: round 3 probes (`../headless-gaps/probes-round3.md`)

- **Posted wheel: measured working** on a classic LISTBOX: WM_MOUSEWHEEL(-120) moved the top index from 0 to 3, and WM_VSCROLL(SB_LINEDOWN) from 3 to 4. Foreground unchanged. Add `scroll @x,y --post` for classic controls to this ticket.
- Pointer work must be per-monitor DPI aware: a DPI-unaware laptop probe landed its input in a user window.

### 2026-11-03: triage decision (user)

Adopt as opt-in `--post`. It now also covers `scroll @x,y --post` (posted WM_MOUSEWHEEL and WM_VSCROLL, measured in round 3). Right-click on an Element prefers ticket 09's verified menu route once it exists. Chromium and Electron double and right clicks belong to ticket 20's sent-message route (the laptop probe checks them). Middle click is best effort only.

### 2026-11-03: implemented (branch hm12-posted-clicks)

- Win32 (`internal/win/postclick.go`):
  - `ChildAtPoint` descends from the target Window through `ChildWindowFromPointEx(CWP_SKIPINVISIBLE | CWP_SKIPTRANSPARENT)`, converting with `ScreenToClient` at each level, and never uses `WindowFromPoint`. It returns the control, its client point and its class.
  - `clickMessages`: WM_MOUSEMOVE, then per click the button down (MK_* in wParam) and up, with the second click as the button's DBLCLK, then WM_CANCELMODE. lParam holds signed 16-bit client coordinates.
  - `wheelMessages`: WM_MOUSEWHEEL with delta = -dy (positive dy scrolls down) and WM_MOUSEHWHEEL with delta = dx. The point is in screen coordinates, as the wheel messages carry it. A large amount is chunked like the SendInput wheel.
  - `PostClick` and `PostWheel` post in order, return the count posted, and refuse inside a go test binary.
  - WM_VSCROLL was not used: the wheel alone was measured to scroll the LISTBOX, and it keeps the agent's amount.
- Allow-list (`win.PostableClass`):
  - Allowed: Edit, RichEdit*, Button, ComboBox, ComboBoxEx32, ComboLBox, ListBox, SysListView32, SysTreeView32, and their WinForms wrappers (`WindowsForms10.<CLASS>.app...`).
  - Not allowed: Static, `#32770` itself, MFC `Afx:` classes, Chromium, XAML and DirectUI surfaces. None of those was measured.
  - RichEdit stays on the list as the ticket says, although charmap's RICHEDIT50W ignored the probe's clicks; the output tells the agent to read the Window State.
- Helper (`internal/helper/postclick.go`): `click --post` with any button and count, at a point or on an Element, and `scroll @x,y <dx> <dy> --post`.
  - Order: resolve the Window and trace the route as Posted; refuse a locked desktop, an administrator Window, a modal or minimized Window (Posted Keys' refusals, with the message hint); take the screen point (the screenshot mapping for a point, the centre of the live bounds for an Element); find the control from the Window down.
  - A class off the list, the Window itself, or no control there is refused with nothing sent and the exact `--headed` retry.
  - Then, in a passive Esc scope, the Agent Cursor travels and the messages are posted. Output: `ok (posted; the app may ignore posted clicks, read the Window State to check)`, with "posted scrolls" for a scroll. Nothing posted is "nothing was sent"; a partial post "may have taken effect".
  - `--post` with `headed` is refused in the CLI (a usage error), in the helper, and while a Batch prepares (`postMouseFlagsError`). `scroll --post` on an Element form is refused.
  - A posted step counts as Headless for the Batch scope (`postedStep`).
  - Keyboard files were not touched: keyroute.go's `postFlagsError` stays as it is, and the click-specific message lives in postclick.go.
- CLI: `--post` anywhere on `click` (`parseClickFlags`, shared with the Batch step) and on `scroll` (`scrollRequest`; the Batch step strips it anywhere).
- Offline tests:
  - win: the message sequences per button and count, MK flags only on the downs, signed lParam, wheel signs and chunks, the allow-list, the live guard.
  - helper: a point and an Element (its centre), refusals by class, the Window itself, no control, `--headed`, a locked desktop and no screenshot, nothing posted and a partial post, the posted wheel and the Element refusal, a Batch of posted steps in the passive scope, and a headed Batch refused up front.
  - cli: the flags, `scrollRequest`, and the Batch steps.
- e2e (vet only): `TestPostedClicksOnACoveredEdit` on the covered fixture's Classic Dialog name Edit. A posted double click must select the first word, and a posted right click must open a `#32768` context menu. The cover stays the foreground in both. The posted wheel has no e2e target, because the fixture has no classic list box.
- Docs: CONTEXT.md (Posted Clicks), SKILL.md (a "Posted Clicks" subsection after Scroll, the right-click item of "What winuse cannot do headless", the fallback paragraph, the command reference), the usage text, codex-parity (the click and scroll rows), the ADR 0013 table (done), test/e2e/README.
