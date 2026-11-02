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

## 2. Modern right-click menus (headless-max 09)

Foreground and cursor were unchanged at every step.

**WinUI 3** (the probe's own app, covered, never activated):
- [M] ShowContextMenu on a TextBox with a custom MenuFlyout: S_OK. A top-level `Microsoft.UI.Content.PopupWindowSiteBridge` "PopupHost" appeared (218x162, with its own InputSiteWindowClass child).
  - Its tree: Popup (Window pattern) > MenuFlyout (Menu, Legacy only) > MenuFlyoutItem "Alpha" and "Beta" (Invoke).
  - The items also show in the main window's tree, so dedupe by PopupHost.
- [M] Side effect: the app's Window.Activated fired once (act 0 to 1) at ShowContextMenu, and XAML focus moved to the first item. GetForegroundWindow did not change.
- [M] **Invoke on the MenuFlyoutItem: S_OK.** Its Click ran (alpha=1), the flyout closed (0 popups), and the foreground was unchanged. Unlike a classic `#32768`, this does not take the foreground.
- [M] **Closing without picking** (MenuFlyout):
  - These close it: Window.Close on the "Popup" element (S_OK), Escape (WM_KEYDOWN/UP) posted to the popup's own InputSiteWindowClass, and Escape posted to the main window's island InputSiteWindowClass.
  - These do not: Escape posted to the PopupHost hwnd, and Escape posted to the top-level. The MenuFlyout has no ExpandCollapse.
- [M] The TextBox's default TextCommandBarFlyout uses two PopupHost windows. Its items are AppBarButtons (ct MenuItem): Paste, Select All and a "MoreButton". Invoke on "Select All": S_OK, and the box's selection became 0,11.
  - Escape to either input site closes it; Escape to the PopupHost or the top-level does not.
  - Collapse on its ApplicationBar (S_OK) only empties it: the popup shell stays open with focus on it, and the next ShowContextMenu just closes the shell. [I] Use Escape to the input site instead.

**Chrome** (the probe's own Chrome, temp profile, covered):
- [M] **A covered Chrome exposes no page content through UIA over 15 s**: 36 Elements, browser UI only, even through Chrome_RenderWidgetHostHWND. With `--disable-features=CalculateNativeWinOcclusion`, the textarea showed up in 340 ms. [I] Chrome's occlusion tracking hides covered WebContents, which removes their accessibility tree. This matters for every covered-Chrome UIA path; CDP avoids it. (headless-max 04 already passes the flag to browsers winuse launches.)
- [M] With occlusion off, ShowContextMenu on the textarea returned S_OK, and a new Chrome_WidgetWin_1 menu window appeared (425x715) with 12 items: Invoke on most, ExpandCollapse on Spell check and Writing Direction. Item names include the shortcut ("Select all Ctrl+A", "Undo Ctrl+Z").
- [M] Invoke on "Select all": S_OK and the menu closed. **But it selected the document ("winuse probe page"), not the textarea**: activeElement stayed empty and the textarea's selection stayed 0,0. ShowContextMenu does not focus its target, so edit commands act on whatever Chrome considers focused.
- [M] Closing: Collapse on the menu's SubmenuView fails with E_FAIL (0x80004005). Escape posted to the menu window closes it.

## 3. Electron and WebView2 started in "controllable" mode (headless-max 15)

Foreground and cursor were unchanged at launch and at every step, in both.

**Electron 45.2.1** (a minimal app; main.js sets userData to `<app>\ud-<pid>` and uses showInactive):
- [M] Launched with `--remote-debugging-port=0`, it wrote DevToolsActivePort into the userData folder (`<app>\ud-19264\DevToolsActivePort`), containing "16065 | /devtools/browser/<uuid>". The same line was printed on stderr ("DevTools listening on ws://127.0.0.1:16065/..."). Both appeared within about 2.8 s of launch.
- [M] The port is owned by electron.exe itself (the launched pid). /json/version: Chrome/156.0.8078.75, Electron/45.2.1. /json/list: one page target (file:///.../page.html).
- [I] Without app.setPath, the file lands in %APPDATA%\<app name>. winuse should read the userData path, or parse stderr when it launches the App itself.

**WebView2** (the probe's own WinForms app with Microsoft.Web.WebView2 1.0.2903.40; runtime 156.0.4314.58; user data folder in the scratchpad; ShowWithoutActivation):
- [M] Launched with `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--remote-debugging-port=16075` in the child's environment only (no registry). The port answered /json/version (Edg/156.0.4314.58) by 1.5 s; the page had loaded at 0.56 s.
- [M] Owner: msedgewebview2.exe(22592), child of wv2probe.exe(6012), inside the app's process tree. The user's 6 existing msedgewebview2 processes were untouched, and none of them owned the port. /json/list: exactly one page target, the probe's page.
- [M] Killing the app tree left none of the probe's msedgewebview2 processes, and the user data folder was deleted.

**Input.insertText with the window covered** (same in both):
- [M] With nothing focused in the page (activeElement empty, hasFocus=false), insertText returns {} and does nothing.
- [M] After Runtime.evaluate("t.focus(); t.setSelectionRange(5,5)"), insertText("xy😀") gave the value "helloxy😀 page", one input event and caret 9,9 (UTF-16). document.hasFocus() became true, and the foreground was unchanged.
- [I] So insertText needs a DOM focus step first. No window activation is required.

## 4. Which Apps fill UIA AcceleratorKey (headless-max 06)

Read-only. Foreground and cursor were unchanged throughout, including the launches of Character Map and Paint.

| App | AcceleratorKey | Where the shortcut is instead | AccessKey |
|---|---|---|---|
| WPF (MenuItem InputGestureText, RoutedUICommand KeyGesture, Button KeyBinding) | never filled | nowhere (Name is just "Save") | "Alt+F" on the bar, "S" in a submenu |
| WinUI 3 MenuFlyoutItem with a KeyboardAccelerator | "Ctrl+S", "Ctrl+O" (readable once the MenuBarItem is expanded) | | empty unless set |
| WinUI 3 Button with a KeyboardAccelerator | "" | | |
| WinForms MenuStrip ShortcutKeys | "Ctrl+S", "Ctrl+O", "Ctrl+Shift+O" | | "Alt+f" on the bar, "o" in the dropdown |
| Classic Win32 HMENU ("&Save\tCtrl+S") | "" | the Name: "Save\tCtrl+S" | "Alt+s" |
| Character Map (`#32770`) | none | | "Alt+s", "Alt+c", "Alt+v" |
| Paint (MSPaintApp, 64 Elements) | Save "Ctrl+S", Undo "Ctrl+Z", Redo "Ctrl+Y" | | key-tip sequences ("Alt, F"; "Alt, S, A") |
| Chrome page context menu | the mnemonic letter ("a", "u") | the Name: "Select all Ctrl+A" | "" |

- [M] Expand and Collapse opened and closed the WPF (HwndWrapper popup), WinUI (PopupHost) and WinForms (WindowsForms10.Window.20808) menus, with no foreground change.
- [M] On a classic HMENU, ExpandCollapse.Expand fails with UIA_E_INVALIDOPERATION (0x80131509), so its items cannot be read over UIA without a real open. But GetMenu, GetSubMenu and GetMenuStringW(MF_BYPOSITION) read the whole tree cross-process without opening it: "&File" > ["&Open...\tCtrl+O", "Save &As\tCtrl+Shift+S"], "&Save\tCtrl+S".
- [M] Where AcceleratorKey holds a real shortcut, the format is "Ctrl+S" or "Ctrl+Shift+O" (plus-joined, modifiers first).
- [I] AcceleratorKey cannot be trusted alone. Also parse the text after "\t" in classic Names and HMENU strings, and the " Ctrl+X" suffix in Chromium Names. Treat a single letter as a mnemonic, not a shortcut.
- [M] Addendum to section 2: on a second open Chrome menu, Collapse on the SubmenuView hung the client indefinitely in 1 of 3 runs, and returned E_FAIL in about 600 ms in the other two. [I] Never Collapse a Chromium menu; post Escape instead.

## 5. A minimized Store app: Calculator (headless-max 14)

6 runs, all closed cleanly.

- [M] **Launch takes the foreground.**
  - IApplicationActivationManager.ActivateApplication("Microsoft.WindowsCalculator_8wekyb3d8bbwe!App", AO_NONE) returned S_OK.
  - The ApplicationFrameWindow (owned by ApplicationFrameHost, not the app pid) appeared 168-304 ms after the call returned, and it was the foreground at that moment (polled every 10 ms: the Calculator frame at +169 ms).
  - So the launch takes the foreground even from a non-foreground caller. Nothing was clicked to give it back.
- [M] Loaded and restored, before minimizing: CalculatorResults ("Display is 0") was reachable 56-75 ms after the frame appeared. The frame subtree had 56 descendants (FindAll 15-19 ms), with the CoreWindow as a child of the frame.
- [M] Minimize: ShowWindow(frame, SW_SHOWMINNOACTIVE) returned TRUE, and the foreground went back to the user's previous window within 0.8 s (4 of 4 runs). It stayed minimized (IsIconic=1, rect -32000) for the whole leg, 20+ s.
- [M] **Minimized, the UIA tree is gone.**
  - The Windows.UI.Core.CoreWindow detaches from the frame and becomes a top-level window of CalculatorApp.
  - Frame descendants: 0 (2-46 ms). CoreWindow descendants: 0, with FindAll taking about 220 ms (vs 7 ms), at both +0.8 s and +20 s.
  - The same held when minimized 200 ms after launch (before load) and after load.
  - So nothing could be Invoked: "Seven", "Plus", "Two" and "Equals" were not found, and the display was unreadable.
- [I] A minimized UWP app is unreachable through UIA. The likely cause is suspension (PLM): the frame has no content. In contrast, minimized classic, WPF and WinUI 3 Desktop windows all work (rounds 3 and 4).
- [M] **Restore with ShowWindow(SW_SHOWNOACTIVATE) took the foreground**: the ApplicationFrameWindow became the foreground. The leg should have stopped there but did not, so the following was observed with the foreground taken:
  - the tree came back in 68 ms (56 Elements);
  - Invoke on Seven, Plus, Two and Equals each returned S_OK in 22-44 ms, and the display read "Display is 9";
  - re-minimizing left the foreground on the minimized Calculator frame. Closing gave it back to the user's window.
- [M] Never-minimized leg: Invoke on Seven gave "Display is 7", but the foreground was already Calculator from the launch, so this is not a background measurement.
- [M] Close: WindowPattern.Close on the frame returned S_OK, and no CalculatorApp process remained after 2.5 s (all runs).
- [I] **Store apps: launch and restore both cost the foreground, and minimizing kills the tree.** The only background state is "restored but covered", which can only be reached through a foreground-taking launch. winuse should treat a minimized UWP app as not Headless and say so.
