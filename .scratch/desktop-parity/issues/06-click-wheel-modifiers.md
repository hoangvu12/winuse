# 06: Modifiers on clicks and wheel

**What to build:** `click ... --modifiers ctrl,shift,alt` and `scroll @x,y ... --modifiers ctrl`, for multi-select (Ctrl+click, Shift+click) and zoom (Ctrl+wheel). agent-desktop has click modifiers (research section 1, Mouse rows).

Routes, most headless first:

- Page Elements and points: CDP `Input.dispatchMouseEvent` with `modifiers`. Headless.
- Multi-select on a UIA list item: `SelectionItem.AddToSelection` for Ctrl+click, a range of AddToSelection for Shift+click, when the container's `CanSelectMultiple` is true. Headless, verified by reading the selection.
- Posted Clicks (`../../headless-max/issues/12-posted-clicks.md`): MK_CONTROL/MK_SHIFT in wParam with `--post`. Best effort.
- Otherwise Headed: hold the modifier with SendInput around the click or wheel, released on every exit path including Esc.

**Blocked by:** none (the Posted leg needs headless-max 12)

**Status:** needs-triage (codex-parity click row: add as extension; decide whether the UIA multi-select route is worth it)

- [ ] `--modifiers` in CLI, protocol, Batch
- [ ] CDP modifiers; UIA AddToSelection route; Headed modifier hold with guaranteed release
- [ ] Offline tests: route choice, release on error
- [ ] Opt-in e2e: Ctrl+click two fixture list items, covered (UIA route)
- [ ] SKILL.md

## Comments

### 2026-11-02: safe live batch (aborted)

- The WPF leg was not reached: attached Ctrl + UIA Invoke, posted click with MK_CONTROL, and AddToSelection on two ListBox items. The batch aborted earlier, when a fixture dialog took the foreground from the user's game client.
- The probe is ready to rerun: `wpf.go`/`wpf.ps1` under the batch's scratchpad.
- Recommendation: keep `needs-triage`. Details: `../../headless-gaps/probes-safe-batch.md`.
