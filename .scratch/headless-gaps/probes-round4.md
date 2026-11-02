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

## 2. Right-click menus (headless-max 09)

All targets are the probe's own windows. Foreground and cursor were unchanged unless stated.

**Classic Edit** (the probe's Win32 window, Edit id 201):
- [M] Cross-process **PostMessage** WM_CONTEXTMENU(wParam=edit, lParam=-1): the target got WM_ACTIVATE(1) (internal activation; the Edit sets focus), and a `#32768` appeared (391x435 at the caret, owned by the target pid). Foreground unchanged. SendMessage would block, because TrackPopupMenu is modal.
- [M] MN_GETHMENU on the `#32768` returned an HMENU readable cross-process: Undo 772, Cut 768, Copy 769, Paste 770, Delete 771, Select All 177, RTL 32768, Show Unicode 32769, and the IME items. The menu's UIA tree had 11 MenuItems (ct 50011) with the same names.
- [M] **UIA Invoke on the `#32768` item "Select All" worked** (EM_GETSEL 0-0 to 0-11) and the menu closed, **but it took the foreground** for the classic window (about 1.1 s, until the window was killed). The leg was stopped.
- [M] MN_GETHMENU plus a **posted WM_COMMAND(177) to the owner (the Edit) did nothing**: the menu stayed open and the selection did not change.
  - [I] The Edit uses TrackPopupMenu(TPM_RETURNCMD), so WM_COMMAND never applies. It works only for owners whose menu sends WM_COMMAND (menu bars, app context menus without TPM_RETURNCMD).
- [M] **Posting the mnemonic key to the `#32768`** (WM_KEYDOWN/UP 'A' to the menu window): Select All ran (0-0 to 0-11), the menu closed, foreground unchanged. Down+Enter also closed it (item effect unclear).
- [M] WM_CANCELMODE sent to the Edit (the menu owner) closed the `#32768`. Foreground unchanged.
- [M] IUIAutomationElement3::ShowContextMenu on the Edit: hr 0, **no menu** (no `#32768`, no WM_CONTEXTMENU in the target).

**Chrome** (launched, temp profile):
- [M] ShowContextMenu on the input element: hr 0, and Chrome's own menu popup appeared (Chrome_WidgetWin_1, 425x714, same pid). On the button "B": popup 583x627. Foreground unchanged.
- [M] ShowContextMenu on the top-level frame element (ct 50032) and on the RWHV element (ct 50033): hr 0, no popup. No Document-type element was exposed as a descendant.
- [M] UIA Invoke on the popup's MenuItem "Select all Ctrl+A" (ct 50011): hr 0, the menu closed, foreground unchanged. [I] It acted; the selection was not read back.
- [M] WM_CANCELMODE sent to the top-level closed the Chrome menu popup.

**WPF** (TextBox with its default ContextMenu):
- [M] ShowContextMenu: hr 0, no popup, ContextMenuOpening count 0.
- [M] Posted WM_CONTEXTMENU(hwnd, -1) to the HwndWrapper top-level: no popup, also after a SetValue.
  - [I] WPF routes WM_CONTEXTMENU to the keyboard-focused element, and an inactive window has none (Keyboard.FocusedElement empty).

**WinUI 3** (TextBox with its default flyout):
- [M] ShowContextMenu: hr 0, and two `Microsoft.UI.Content.PopupWindowSiteBridge "PopupHost"` windows appeared (275x90 and 275x102). The window's Activated event fired once (act=1), but the system foreground was unchanged.
- [M] WM_CANCELMODE to the top-level did **not** close them.

**Reading:**
- ShowContextMenu works on Chromium elements and WinUI 3, not on classic windows or WPF.
- For classic windows: open the menu with a posted WM_CONTEXTMENU, then choose an item by posting its mnemonic to the `#32768` (or WM_COMMAND for owners that use it).
- Never use UIA Invoke on `#32768` items: it takes the foreground.
- WPF has no Headless route.
