# Probes, round 4 (2026-11-03): laptop

Laptop probe chat (Zeron 358cd6b7), asked by the PC ticket orchestrator. Laptop: Win11 26200, 2560x1440 at 150%. The probe is per-monitor-v2 DPI aware and drives only windows it built or launched itself. Probe code stays in the laptop scratchpad. Confidence: [M] measured live, [I] inference.

## 1. Minimized windows (headless-max 14)

Setup:
- Targets: the probe's own .NET 8 WPF app and WinUI 3 app (Windows App SDK 1.7, unpackaged, self-contained), shown without activation (WPF `ShowActivated=false`; WinUI `AppWindow.Show(false)`).
- Minimized with `ShowWindow(SW_SHOWMINNOACTIVE)`.
- The UIA client has AutoSetFocus FALSE.
- Readback: each app writes its control state into its title.
- Calculator was not used, because launching an inbox UWP app would probably take the foreground.

**WPF** [M]

| Step | Result |
|---|---|
| UIA descendants | 29 (74 ms) before minimizing; 29 (80 ms) minimized; 29 at the end |
| Invoke Button | hr 0; click count 0 to 1 |
| Toggle CheckBox | hr 0; IsChecked True; ToggleState read back 1 |
| Value.SetValue TextBox "hello" | hr 0; Text "hello"; Value read back "hello" |
| SelectionItem.Select | hr 0; Extended ListBox it2 gave SelectedItems=it2; Multiple ListBox m3 gave m3 |

IsIconic stayed true after every step. Foreground and cursor were unchanged at every step. The window's Activated count stayed 0.

**WinUI 3** [M]

| Step | Result |
|---|---|
| UIA descendants | 20 (41 ms) before; 20 (13 ms) minimized; 20 at the end |
| Invoke Button | hr 0; clicks 0 to 1 |
| Toggle CheckBox | hr 0; IsChecked True; ToggleState 1 |
| SetValue TextBox "hello" | hr 0; read back "hello" |
| Select ListViewItem it2 (Extended ListView) | hr 0; SelectedItems=it2 |

IsIconic stayed true throughout. Foreground and cursor were unchanged. Activated count 0.

**Reading** [I]:
- With the PC's classic result (round 3), UIA patterns work on minimized classic, WPF and WinUI 3 windows alike, and the tree does not shrink when minimized.
- headless-max 14 can lift the minimized refusal for UIA-pattern Actions on these frameworks.
- It must still refuse the Chromium message routes, because activating a minimized Chromium window takes the foreground (round 3).
- Not covered: packaged UWP apps (`ApplicationFrameWindow`, such as Calculator) and Chromium through UIA patterns.
