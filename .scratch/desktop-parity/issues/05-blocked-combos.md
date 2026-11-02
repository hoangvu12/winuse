# 05: Blocked dangerous combos with `--force`

**What to build:** `press-key` refuses `Alt+F4`, `Alt+Tab`, `Ctrl+Shift+Escape` and any superset of them (extra modifiers) with "nothing was sent", unless `--force` is passed.

agent-desktop blocks `alt+f4`, `win+l`, `win+d`, `alt+tab`, `win+r`, `win+x`, `win+e`, `ctrl+shift+esc` and supersets (`ad:crates/windows/src/input/blocked_combo.rs:27-60`). winuse already refuses every Windows-key combo in code with no override (`internal/keys/keys.go:146`; `internal/helper/keyboard.go:166-171`); the other three go through today.

- Check after key-name parsing, before routing, on every route (Headed, Posted, Page). On a Page, Alt+F4 and Ctrl+Shift+Esc reach the browser frame, not the Page; block them there too.
- Batch steps accept `--force` per step.
- Hint: "close the Window with close-window" for Alt+F4; "use activate-window" for Alt+Tab.

**Blocked by:** none

**Status:** ready-for-agent (codex-parity "key names" row: reword as matched, extended)

- [ ] Combo list and superset match in `internal/keys`
- [ ] `--force` in CLI, protocol, Batch
- [ ] Offline tests: each combo, superset, `--force`, hint text
- [ ] SKILL.md keys section; codex-parity row
