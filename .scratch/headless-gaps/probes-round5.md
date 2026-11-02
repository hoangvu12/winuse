# Probes, round 5 (2026-11-03): laptop

Laptop probe chat "winuse headless probes, round 5 (laptop)" (Zeron 11f0a0c0), sent by the PC ticket orchestrator. Laptop: Win11 26200, 2560x1440 at 150%. The probe is per-monitor-v2 DPI aware and drives its own windows, plus the exceptions the user approved (the taskbar, notifications, Calculator). Confidence: [M] measured live, [I] inference.

## 1. Typing into modern text boxes without keys (headless-max 13)

Setup:
- Targets: the probe's own WPF TextBox and WinUI 3 TextBox ("hello world"), never activated.
- A cover window sat above each target in z-order (19 vs 21 for WPF, 19 vs 20 for WinUI).
- AutoSetFocus=false; SetFocus never called.

Foreground and cursor were unchanged at every step in both apps.

- [M] **There is no findable focused Element in a background window.**
  - WPF: no Element had HasKeyboardFocus=1 anywhere in the subtree, before or after TextRange.Select. The app's own Keyboard.FocusedElement and FocusManager focus were empty too.
  - WinUI: the only hit was the island's InputSiteWindowClass hwnd, never the TextBox. The XamlRoot's focused element was empty.
  - System focus stayed in the user's app. The agent must name the target box.
- [M] **GetSelection works in the background.** Offsets come from DocumentRange.Clone + MoveEndpointByRange(End, sel, Start) + GetText(-1).Length. Before: 0..0, doclen 11. After placing the caret: 5..5. The app's own SelectionStart agreed (5,0).
- [M] **The caret recipe:** collapse the range, Move(Character, n), then collapse again before Select. Without the second collapse, WPF selected one character ([5..6]), and a splice replaced it.
- [M] **The Character unit differs per framework.**
  - WPF counts the emoji 😀 as one unit: Move(8) gave UTF-16 offset 9, and Move(9) gave 10.
  - WinUI counts UTF-16 units: Move(9) gave 9, and Move(8) landed mid-pair and snapped to 9.
  - [I] So place the caret, read it back with GetSelection, and adjust.
- [M] **The splice works.** Value.SetValue("helloxy😀 world") returned S_OK in both, and the read-back matched exactly. The caret reset to 0..0 after SetValue in both, so it has to be placed again. TextChanged fired once in both (tc=1).
- [M] **Undo:**
  - WPF: CanUndo went from False to True after SetValue, and the app's Undo button (UIA Invoke) restored "hello world" in one step.
  - WinUI: CanUndo stayed False and Undo did nothing, so SetValue makes no undo step there.
  - [I] Existing undo history was not tested (there is no keyless way to create a user edit). WinUI likely drops it.
- [M] **Detection:**
  - PasswordBox has IsPassword=1 in both. Its Value is "" in WPF and "●●●●●●" in WinUI.
  - A read-only TextBox has Value.IsReadOnly=1 in both, and SetValue fails with UIA_E_INVALIDOPERATION (0x80131509), leaving the text unchanged.
  - All of them have the Text pattern.
