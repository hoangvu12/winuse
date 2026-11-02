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

**Status:** ready-for-agent

- [ ] Win32 layer: child at point, posted sequence, WM_CANCELMODE
- [ ] Class allow-list and refusals
- [ ] CLI and Batch `--post` on click
- [ ] Offline tests: message sequence per button and count, allow-list, flag conflicts
- [ ] Opt-in e2e: fixture edit box double and right click, covered
- [ ] CONTEXT.md, SKILL.md

## Comments

### 2026-11-03: round 3 probes (`../headless-gaps/probes-round3.md`)

- **Posted wheel: measured working** on a classic LISTBOX: WM_MOUSEWHEEL(-120) moved the top index from 0 to 3, and WM_VSCROLL(SB_LINEDOWN) from 3 to 4. Foreground unchanged. Add `scroll @x,y --post` for classic controls to this ticket.
- Pointer work must be per-monitor DPI aware: a DPI-unaware laptop probe landed its input in a user window.

### 2026-11-03: triage decision (user)

Adopt as opt-in `--post`. It now also covers `scroll @x,y --post` (posted WM_MOUSEWHEEL and WM_VSCROLL, measured in round 3). Right-click on an Element prefers ticket 09's verified menu route once it exists. Chromium and Electron double and right clicks belong to ticket 20's sent-message route (the laptop probe checks them). Middle click is best effort only.
