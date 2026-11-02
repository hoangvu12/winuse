# 05: Blocked dangerous combos with `--force`

**What to build:** `press-key` refuses `Alt+F4`, `Alt+Tab`, `Ctrl+Shift+Escape` and any superset of them (extra modifiers) with "nothing was sent", unless `--force` is passed.

agent-desktop blocks `alt+f4`, `win+l`, `win+d`, `alt+tab`, `win+r`, `win+x`, `win+e`, `ctrl+shift+esc` and supersets (`ad:crates/windows/src/input/blocked_combo.rs:27-60`). winuse already refuses every Windows-key combo in code with no override (`internal/keys/keys.go:146`; `internal/helper/keyboard.go:166-171`); the other three go through today.

- Check after key-name parsing, before routing, on every route (Headed, Posted, Page). On a Page, Alt+F4 and Ctrl+Shift+Esc reach the browser frame, not the Page; block them there too.
- Batch steps accept `--force` per step.
- Hint: "close the Window with close-window" for Alt+F4; "use activate-window" for Alt+Tab.

**Blocked by:** none

**Status:** done

- [x] Combo list and superset match in `internal/keys`
- [x] `--force` in CLI, protocol, Batch
- [x] Offline tests: each combo, superset, `--force`, hint text
- [x] SKILL.md keys section; codex-parity row

## Comments

### 2026-11-02: landed

- `internal/keys`: `Blocked(chord)` names the Blocked Combo a chord presses (`Alt+F4`, `Alt+Tab`, `Ctrl+Shift+Escape`). Each combo is a list of key groups (Alt, Control and Shift match left, right and the generic VK); a chord blocks when it holds a key from every group, so any superset (extra modifiers, or extra keys) is blocked too, whatever the order. Near misses (Control_L+F4, Shift_L+Tab, Control_L+Escape, Alt_L+F5) pass.
- `internal/helper/keyboard.go`: `blockedComboError` runs right after `keys.Parse` in press-key, before the Posted, Page, dialog and Headed routes, so every route refuses, including a Page (Alt+F4 and Ctrl+Shift+Esc would reach the browser frame). Message `press-key refuses <chord>: <combo> <effect>; nothing was sent`; hints: "close the Window with winuse close-window <window>" for Alt+F4, "use winuse activate-window <window>" for Alt+Tab, "only when the user asked for Task Manager" for Ctrl+Shift+Escape, each naming `--force`. `--force` with `--post` still gets Posted Keys' modifier refusal.
- Batch (`internal/helper/batch.go`): `blockedStep` refuses a press-key step with a Blocked Combo and no `force` of its own while the Batch prepares its steps, as `batch: Action N (press-key): ...`, so no earlier step acts. `force` is per step; the Batch's `--headed` does not imply it.
- Protocol: `PressKeyArgs.Force` (`force,omitempty`). CLI: `--force` anywhere after the Window on press-key and in a batch press-key step (`stripPost` became `stripFlag`); usage line `press-key <window> <key> [--post] [--force]` and the usage text list the combos.
- Tests: `internal/keys/keys_test.go` `TestBlocked`; `internal/helper/blockedcombos_test.go` (each combo and supersets on the default, `--headed` and `--post` routes with message, hint and no desktop steps; `--force` sends; the Page rig refuses and sends nothing, and sends with `--force`; a Batch refuses up front and a forced step presses); CLI parsing in `keyboard_test.go` and `batch_test.go`. `TestPostRefusesChordsWithModifiers` now uses Alt_L+F5, since Alt_L+F4 gets the blocked refusal first. No e2e test.
- Docs: SKILL.md (keys bullets, Safety, command reference), CONTEXT.md (**Blocked Combo**), README, docs/codex-parity.md "key names" now **matched (extended)**.
