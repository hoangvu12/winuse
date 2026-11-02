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

## 3. Double and right clicks through sent mouse messages (headless-max 20)

The messages were sent in client coordinates to the top-level `Chrome_WidgetWin_1`. Chrome (stable, temp profile) and Electron 45.2.1 behaved identically. Foreground and cursor were unchanged in every step. [M] unless marked.

- **Double click** (WM_MOUSEMOVE, LBUTTONDOWN(MK_LBUTTON), LBUTTONUP, LBUTTONDBLCLK(MK_LBUTTON), LBUTTONUP) on a button: the page saw `mousedown(detail 1) click(1) mousedown(2) click(2) dblclick(2)`, all button 0 and isTrusted. **This works with and without the activation pair** around it.
- **Right click** (RBUTTONDOWN(MK_RBUTTON), RBUTTONUP) on a button whose contextmenu handler calls preventDefault: `mousedown(button 2) auxclick(2) contextmenu(2)`, all trusted. It also works with and without the activation.
- Right click on a second button with **no** preventDefault (activated): the same three trusted events. No native menu popup appeared within about 300 ms in Chrome or Electron (no new visible top-level of the pid). WM_CANCELMODE afterwards had nothing to close.
  - [I] Electron has no default menu. Chrome's page menu apparently did not open for a message-generated right click. Not investigated further.
- Sent mouse messages work without the activation, while keys need it (round 3). So clicks, double clicks and right clicks can skip the focus/blur pair the page sees from the activation.

## 4. Tab switch on a launched Chrome (headless-max 05)

Setup:
- Chrome 156.0.8078.76 (branded stable), temp profile, two tabs: TAB-A (active) and TAB-B. Launched with SW_SHOWNOACTIVATE.
- Covered: WindowFromPoint at Chrome's centre returned another window. The probe's cover sat directly above Chrome, and the user's Zeron window was above both there.

Results:
- [M] The UIA tab strip had two TabItems (ct 50019): "TAB-A - Memory usage - 17.3 MB" (IsSelected 1) and "TAB-B" (IsSelected 0).
- [M] **SelectionItem.Select on TAB-B: hr 0.** The window title changed to "TAB-B - Google Chrome" and TAB-B's IsSelected became 1. **Foreground and cursor unchanged.**
- [M] Invoke on the now-inactive TAB-A: **no InvokePattern** (GetCurrentPatternAs returned null). The tab did not change; foreground unchanged. Use Select for tab switching.
- [M] **`--load-extension`: no.** The test used an unpacked MV3 dummy that overrides the new tab page (nt.html, title EXT-NEWTAB). Launched on chrome://newtab/, the title stayed "New Tab - Google Chrome". Adding `--disable-features=DisableLoadExtensionCommandLineSwitch` changed nothing. Branded Chrome 156 ignores the switch. (Edge on the laptop is 144.0.3719.82; not tested.)
- Foreground and cursor were unchanged in all steps.

## 5. Chords on classic Apps through attached key state (headless-max 07)

Method:
1. AttachThreadInput(probe thread, target UI thread).
2. SetKeyboardState with VK_CONTROL+VK_LCONTROL (or VK_SHIFT+VK_LSHIFT, VK_MENU+VK_LMENU) at 0x80.
3. PostMessage WM_KEYDOWN to the thread's focus HWND (GetGUIThreadInfo), or to the top-level when there is none.
4. Hold 150 ms, post WM_KEYUP, restore the saved state, detach.

The chord is refused if the target thread owns the foreground (never the case here).

**The user's async key state, GetAsyncKeyState(Ctrl/Shift/Alt), was 0 before, during and after every chord** [M]. Foreground and cursor were unchanged in every step [M].

- **Win32 TranslateAccelerator loop** (the probe's window; accelerator table Ctrl+S=301, Ctrl+Shift+S=302) [M]: Ctrl+S reached the loop as WM_KEYDOWN 0x53, TranslateAccelerator handled it, and WM_COMMAND 301 fired. Ctrl+Shift+S gave WM_COMMAND 302. Both worked with no activation messages.
- **WinForms** (.NET 8, MenuStrip items with ShortcutKeys Ctrl+O and Ctrl+Shift+O) [M]: posted to the form HWND (no focused control). Both fired (o=1, os=1). The form's Activated count stayed 0. No activation needed.
- **WPF** (.NET 8, Window.InputBindings KeyBinding Ctrl+S and Ctrl+Shift+S) [M]:
  - Without activation, **neither fired** (s=0, ss=0; Keyboard.FocusedElement empty).
  - **With the PR #400 activation pair** sent to the HwndWrapper top-level (NCACTIVATE 1, ACTIVATE WA_ACTIVE, SETFOCUS; afterwards KILLFOCUS, ACTIVATE 0, NCACTIVATE 0), **both fired** (s=1, ss=1). Keyboard.FocusedElement became the Window during the pair. The WPF Activated event fired once, but the system foreground was unchanged.
- **Alt+F on the Win32 window** (WM_SYSKEYDOWN 'F' with lParam bit 29, attached VK_MENU) [M]:
  - Without activation, the window got WM_SYSKEYDOWN, WM_SYSCHAR 'f' and WM_SYSCOMMAND SC_KEYMENU 'f', but **no menu opened** (no `#32768`, no WM_ENTERMENULOOP). A directly posted WM_SYSCOMMAND(SC_KEYMENU, 'f') did nothing either.
  - **With the activation pair, the File menu opened** (WM_ENTERMENULOOP, `#32768` 291x111; MN_GETHMENU items Save 301, Save As 302, Open 303). Posting the mnemonic 'S' to the `#32768` chose Save (WM_COMMAND 301, the menu closed). Foreground unchanged throughout.
- MFC: skipped; no MFC app was easy to build or find.

## 6. Multi-select through AddToSelection (desktop-parity 06)

All windows were covered: WindowFromPoint at each window's centre returned another window. Foreground and cursor were unchanged in every step [M].

| Control | Steps | Result [M] |
|---|---|---|
| WPF ListBox, SelectionMode=Extended | AddToSelection it2, then it4 (hr 0 each) | SelectedItems it2, then it2,it4; IsSelected 1 and 1 |
| WPF ListBox, SelectionMode=Multiple | AddToSelection m1, then m3 (hr 0 each) | SelectedItems m1,m3; IsSelected 1 and 1 |
| Win32 SysListView32 (LVS_LIST, multi-select allowed) | AddToSelection row2, then row4 (hr 0 each) | LVM_GETSELECTEDCOUNT 1, then 2; IsSelected 1 and 1 |
| WinUI 3 ListView, SelectionMode=Extended | AddToSelection it1, then it3 (hr 0 each) | SelectedItems it1,it3; IsSelected 1 and 1 |

AddToSelection works Headless on all three stacks, with no activation.

## 7. Shell surfaces (desktop-parity 09 and 18), partial on purpose

Foreground and cursor were unchanged throughout [M].

- **Shell_TrayWnd UIA tree** [M]: 24 descendants in 23 ms. The taskbar is at {0,1368 2560x72}.
  - Start: StartButton (ToggleButton). Search: SearchButton (ToggleButton).
  - TaskbarFrame (Taskbar.TaskbarFrameAutomationPeer).
  - Tray buttons, all with AutomationId "SystemTrayIcon": "Show Hidden Icons" (SystemTray.NormalButton), the input indicator, Network (AccentButton), Volume (OmniButtonCenter), Power (AccentButton), "Clock 06:14 11/3/2026" (OmniButton), and "Show Desktop" (ShowDesktopButton).
  - Panes: TrayNotifyWnd, and MSTaskSwWClass "Running applications" with 10 app buttons (names withheld).
  - There is no separate "notification center" button: the Clock OmniButton is that entry point.
- **Tray overflow** [M]: `TopLevelWindowForOverflowXamlIsland` "System tray overflow window." exists, hidden, at {1984,1257 351x111}. UIA gives **0 descendants while it is hidden**. There is no NotifyIconOverflowWindow (the Windows 10 class) on this build.
- **Invoking the chevron and the Clock: not run.** These are the user's own shell windows, and opening a flyout will most likely take the foreground. It needs the user's direct OK.
- **Own toast: blocked by a user setting.** A toast raised from Windows PowerShell's AUMID with a system "dismiss" action returned from Show() with no error, but no toast window appeared within 6 s. HKCU\...\PushNotifications ToastEnabled = 0 (toasts are off on this laptop). The setting was not changed. Unmeasured.

The batch (probes 1-7) is done. No probe processes, browsers or profiles are left running, port 9229 is closed, and the repo is unchanged.
