# 06: Modifiers on clicks and wheel

**What to build:** `click ... --modifiers ctrl,shift,alt` and `scroll @x,y ... --modifiers ctrl`, for multi-select (Ctrl+click, Shift+click) and zoom (Ctrl+wheel). agent-desktop has click modifiers (research section 1, Mouse rows).

Routes, most headless first:

- Page Elements and points: CDP `Input.dispatchMouseEvent` with `modifiers`. Headless.
- Multi-select on a UIA list item: `SelectionItem.AddToSelection` for Ctrl+click, a range of AddToSelection for Shift+click, when the container's `CanSelectMultiple` is true. Headless, verified by reading the selection.
- Posted Clicks (`../../headless-max/issues/12-posted-clicks.md`): MK_CONTROL/MK_SHIFT in wParam with `--post`. Best effort.
- Otherwise Headed: hold the modifier with SendInput around the click or wheel, released on every exit path including Esc.

**Blocked by:** none (the Posted leg needs headless-max 12)

**Status:** done (live e2e not run yet: see the 2026-11-03 implementation comment)

- [x] `--modifiers` in CLI, protocol, Batch
- [x] CDP modifiers; UIA AddToSelection route; Headed modifier hold with guaranteed release
- [x] Offline tests: route choice, release on error
- [x] Opt-in e2e: Ctrl+click two fixture list items, covered (UIA route) (only the refusal: the fixture's list is single-select, see the comment)
- [x] SKILL.md

## Comments

### 2026-11-02: safe live batch (aborted)

- The WPF leg was not reached: attached Ctrl + UIA Invoke, posted click with MK_CONTROL, and AddToSelection on two ListBox items. The batch aborted earlier, when a fixture dialog took the foreground from the user's game client.
- The probe is ready to rerun: `wpf.go`/`wpf.ps1` under the batch's scratchpad.
- Recommendation: keep `needs-triage`. Details: `../../headless-gaps/probes-safe-batch.md`.

### 2026-11-03: triage decision (user)

Prefer Headless, agent-desktop's way as the fallback: CDP modifiers on Pages; UIA AddToSelection on lists once a probe proves it; Posted with MK_* once headless-max 12 is decided; otherwise agent-desktop's held-modifier SendInput click, which needs `--headed` like every Headed Action. The codex-parity click row gains this as an extension.

### 2026-11-03: UIA multi-select measured (`../../headless-gaps/probes-round4.md` section 6)

AddToSelection read back correctly, Headless and covered, on WPF ListBox (Extended and Multiple), Win32 SysListView32 and WinUI 3 ListView. Build it:

- `click <item> --modifiers ctrl` on a SelectionItem whose container has CanSelectMultiple uses AddToSelection.
- Shift is a range of AddToSelection from the container's last selected item.
- Read the selection back afterwards.
- This route is preferred over the Headed modifier hold.

### 2026-11-03: implemented (branch dp06-modifiers)

- One set of modifiers (`win.Modifiers`, parsed from `--modifiers ctrl,shift,alt`; `control` is accepted for `ctrl`; an unknown name is refused) is carried to every route of `click` and of `scroll @x,y`. The routes, in order:
  - `--post`: the MK_CONTROL/MK_SHIFT key state in every posted mouse message's wParam (the low word for the wheel). Alt cannot be posted and is refused with nothing sent (`postAltRefusal`).
  - A Page Element (click) or a point on a Page (scroll): CDP's `modifiers` bitmask (Alt 1, Control 2, Shift 8) on every mouse event (`page.ClickEventsWith`, `Input.ClickWith`, `Input.WheelWith`), Headless.
  - Ctrl or Shift alone, with a left single click on an Element: `uia.Client.MultiSelect` (`internal/uia/multiselect.go`), Headless.
    - It needs the item's SelectionItem, its SelectionContainer and the container's Selection with CanSelectMultiple true; otherwise `ErrNotMultiSelect` is refused with nothing sent and the exact `--headed` retry.
    - Ctrl toggles: AddToSelection, or RemoveFromSelection when the item is selected, as Ctrl+click does.
    - Shift adds the range from the container's last selected item (the last of GetCurrentSelection, found among the container's SelectionItems by CompareElements), or just the item when nothing is selected.
    - The selection is read back.
  - `--headed`: the real modifier keys held with SendInput around the click or wheel (`win.HoldModifiers`, released once through a deferred release on every exit path, a failed click or Esc included; a failed press releases what it pressed). This covers the point, Element and Page forms.
  - Anything else without `--headed` (a point, double, right or middle clicks, Ctrl and Shift together, Alt, a non-Page wheel) is refused with nothing sent and the `--headed` retry with `--modifiers` (`modifierRefusal`). The point, Chromium-message and UIA-scroll routes of headless-max 11 and 20 carry no modifiers, so they never run with them (Ctrl+wheel means zoom, not scroll).
- `scroll`'s Element forms refuse `--modifiers`.
- `win.PostClick` and `win.PostWheel` (and their Services fields) take the modifiers.
- CLI: `--modifiers` on click (`parseClickFlags`, shared with the Batch step) and on scroll (`takeModifiers` in `scrollRequest` and in the Batch step).
- Offline tests:
  - win: names, MK and CDP encodings, the hold's order, posted key state, the live guard.
  - uia: the Shift range.
  - page: CDP modifiers on every event.
  - cli: the flags and Batch steps.
  - helper: Ctrl and Shift to MultiSelect, the not-multi-select refusal with its retry, the refusals per form, the Headed hold around the click and its release after a failed click, posted Ctrl and refused Alt, and Ctrl+wheel refused and held under `--headed`.
- e2e (vet only): `TestCtrlClickOnASingleSelectListIsRefused`. The fixture's list reports CanSelectMultiple false, so the ticket's "Ctrl+click two fixture list items" cannot pass on it. The positive leg needs a multi-select list in the fixture (a follow-up; probes round 4 section 6 measured AddToSelection on WPF, SysListView32 and WinUI 3).
- Docs: the SKILL.md command reference (two lines), codex-parity (a modifiers row, extension), usage text, test/e2e/README, spec row.

### 2026-11-03: the positive e2e leg (branch e2e-fixes)

- The fixture's Classic Dialog gained a multi-select list box (`fixture.ClassicListID`, LBS_EXTENDEDSEL, items `fixture.ClassicFruits`) as its last control, with a "Fruits:" label, so no earlier Element Index moves; the dialog grew to 158 dialog units high.
- `TestCtrlAndShiftClickSelectInAMultiSelectList`: Ctrl+click Apple, Ctrl+click Cherry, Shift+click Elder (Cherry to Elder), Ctrl+click Apple again (deselected), covered, with the selection read back by LB_GETSEL. `TestCtrlClickOnASingleSelectListIsRefused` stays.
- Not measured yet: that UI Automation's Win32 ListBox proxy reports CanSelectMultiple and implements AddToSelection for LBS_EXTENDEDSEL. If the live run says no, switch the control to a SysListView32 without LVS_SINGLESEL (measured in probes round 4).
