Status: needs-triage

# How close can winuse get to "100% headless"?

Research for the question: can every Action run without the user's foreground, real cursor or keyboard (no SetForegroundWindow, no SetCursorPos, no SendInput)? Where can it not, and what should winuse do there?

Date: 2026-11-02, branch `headless-parity` (uncommitted tree). Windows 11 Pro 10.0.26200.

**Short answer:** no, not 100%. About two thirds of today's remaining Headed paths can move to a Headless or Posted route for **classic Win32** targets: double, right and middle clicks, context menus, menu commands, chords through accelerators, selection, and launch. For **XAML/WPF/WinUI/UWP, Qt, Java and the native Chromium frame**, hover, drag, point clicks, point scrolls and real keystroke semantics have no foreground-free route on the user's desktop. The only way around that is a separate session or a VM. Microsoft's new isolated-session container blocks SendInput too (section 5), so even that does not buy hover or drag. The honest end state is: Headless where a route exists, fail-closed with "nothing was sent" and a `--headed` hint everywhere else. ADR 0010's policy stands.

Legend for evidence:
- **[M]** measured on this PC by this research (probe source and raw output below).
- **[D]** Microsoft or vendor docs.
- **[C]** source code read.
- **[I]** inference.

---

## 0. What was verified as already done

- `AutoSetFocus` is FALSE (`internal/uia/thread.go:79`). It is set once on the UIA worker for every pattern call.
- Headless click route, in order: Invoke, Toggle, SelectionItem, ExpandCollapse, then LegacyIAccessible.DoDefaultAction (`internal/uia/headless.go:198-263`). Toggle and Expand/Collapse fall back to Invoke (`:329-407`).
- Screenshots never activate by default (`internal/helper/screenshot.go:93-108`). `--headed` activates (`:96`).
- Window Actions use ShowWindowAsync with SW_SHOWNOACTIVATE / SW_SHOWMINNOACTIVE, SetWindowPos with SWP_NOACTIVATE, and a posted WM_CLOSE (`internal/win/placement.go:64-170`).
- Element scroll goes through the UIA path (`internal/helper/scroll.go:151-159`).
- Dialog Return/Escape and EM_REPLACESEL typing go through `keyroute.go`, wired in `internal/helper/keyboard.go:70-81` and `:135-146`.
- `--post` is wired in `internal/helper/keyboard.go:53-61` and `:115-126`.
- CDP Page actions: `internal/helper/pageactions.go` and `pageelements.go`.

**One correction to a done item:** `restore-window` is not reliably foreground-free.
- **[M]** charmap.exe, minimized and then restored with ShowWindow(SW_SHOWNOACTIVATE) from a background process, became the foreground window in 6 of 6 runs. The same happened with SetWindowPlacement(showCmd=4).
- **[M]** It still happened with a temporary WS_EX_NOACTIVATE on the window, with EnableWindow(FALSE) around the call, and after waiting 10 s.
- **[M]** The probe's own plain window, restored the same way, stayed in the background.
- So the OS call is non-activating [D: ShowWindow, SW_SHOWNOACTIVATE "the window is not activated", https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-showwindow], but some apps take the foreground themselves when they are restored.
- The mechanism inside charmap is unknown [I: it is a `#32770` dialog app]. winuse uses ShowWindow**Async** (`placement.go:74-79`); I measured ShowWindow, and the two should behave the same [I].
- `restoreWindow` (`internal/helper/windowactions.go:57`) verifies the iconic state only, never the foreground.

---

## 1. Inventory: every remaining Headed path

"Headed" here means the path goes through `runAction` (`internal/helper/actions.go:199-241`). That function calls `s.Activate` at `:225` (`internal/win/activate.go:101-156`: a SendInput zero move at `:132`, AttachThreadInput to the foreground thread at `:140`, then SetForegroundWindow and BringWindowToTop at `:144-145`), then injects through `internal/win/input.go` (SetCursorPos `:146`, SendInput `:228`) or `internal/win/keyboard.go`.

| # | Action / form | Current route (file:line) | Headed? | Why it is Headed today |
|---|---|---|---|---|
| 1 | `click <el> --count 2/3` or `--button right/middle` (UIA Element) | `actions.go:150-163` (ElementPoint, then `s.Click`, which is `win.MouseClick` at `input.go:125`). Predicate: `headless.go:186-195` | always | UIA has no double-click or right-click pattern (ADR 0010) |
| 2 | `click @x,y` (any button) on a non-Page point | `actions.go:115-129` | always | A point names whatever is topmost there (ADR 0010, Decision 1) |
| 3 | `click <el> --headed` (opt-in) | `actions.go:150` | opt-in | The agent asked for it |
| 4 | `set-value @x,y` | `setvalue.go:108-121` (`SetValueAtPoint` after Activate) | always | Point form |
| 5 | `perform-secondary-action @x,y` | `setvalue.go:185-199` | always | Point form |
| 6 | `perform-secondary-action <el> Raise` | `setvalue.go:201-210`, then `uia/value.go:67-76` (UIA SetFocus). Refused headless at `uia/headless.go:137` | always | SetFocus moves the foreground (agent-desktop A3-4) |
| 7 | `scroll @x,y dx dy` on a non-Page point | `pointer.go:76-85` (wheel at `input.go:267+`) | always | ADR 0011, Decision 3 |
| 8 | `scroll <el> … --headed` | `scroll.go:161-198` | opt-in | |
| 9 | `drag @x,y @x,y` (native) | `pointer.go:107-121` (`s.Drag`, `input.go:362+`) | always | Needs a real button-down and moves |
| 10 | `hover @x,y` (native) | `pointer.go:143-152` | always | Needs the real pointer position |
| 11 | `type-text` without a classic-edit route and without a Page focus | `keyboard.go:83-91` (`s.TypeText`, SendInput KEYEVENTF_UNICODE) | always unless `--post` | No per-control HWND for WPF/WinUI/UWP/Qt/Java/Electron/WebView2 (spec, out of scope) |
| 12 | `press-key` other than dialog Return/Escape and not on a Page | `keyboard.go:148-153` | always unless `--post` | Posted keys carry no modifier state (`keyboard.go:121-123`, ADR 0011, Decision 5) |
| 13 | `press-key/type-text --post` with a modifier | `keyboard.go:121` | refused | Posted chords are out of scope (spec) |
| 14 | `activate-window` | `actions.go:172-182` | always | It is the Action's purpose |
| 15 | `get-window-state --screenshot --headed`, `zoom --headed` | `screenshot.go:95-99` | opt-in | |
| 16 | Every Headless Action on a **minimized** Window | refused: `win/activate.go:85-87`, mapped at `helper/headless.go:480-487`; the screenshot at `screenshot.go:100-101` | refused, then Headed via `--headed` | Treated as "Elements are offscreen" |
| 17 | `tab switch` on a launched/cloned browser | `tabs.go:509-560` (runAction, then Ctrl+<n> / Ctrl+Tab via `s.PressKeys`) | always | `Target.activateTarget` was never measured and is forbidden (`tabs.go:17-25`) |
| 18 | Page click/scroll `--headed` | `pageactions.go:408-417`, `:510-519`, `:537-546` | opt-in | |
| 19 | `launch-app` | `commands.go:149` (StartApp), then `apps/launch.go:48-67` (`exec.Command`, **no STARTUPINFO show state**) or `:125-147` (ShellExecuteEx, **nShow = SW_SHOWNORMAL**, `:133`) | not an Action, but can take the foreground | The App may activate its first window |
| 20 | `browser launch` | `browser/system_windows.go:298-315` (`exec.Command`, no show state) | takes the foreground (`probe-results.md`, "Launch took the foreground: yes") | |
| 21 | `restore-window` | `placement.go:64` | nominally Headless | **[M]** Some apps self-activate (section 0) |
| 22 | Batch steps of kinds 1-18 | `batch.go:105-106` (`withHeaded`), predicate `headless.go:254-269` | follows each step | |

These have no command at all today, so they are a gap rather than a Headed path:
- context menu as an action;
- menu-path invoke;
- text selection or caret placement;
- clipboard paste;
- file drop;
- hover or drag on a **native Element** (hover and drag accept only `@x,y` or a Page Element, `pointer.go:100-101` and `:136-137`).

---

## 2. Per gap: techniques, evidence, frameworks, verdict

Framework shorthand:
- **W32**: classic HWND controls (Edit, RichEdit, ListView, TreeView, buttons, dialogs, owner-drawn Win32, MFC, WinForms).
- **XAML**: UWP / WinUI 2 and 3.
- **WPF**.
- **CHR**: native Chromium frame, Electron, WebView2 without CDP.
- **CDP**: a Page with a Browser Connection.
- **Qt / Java**: Qt Widgets/Quick and Swing/JavaFX. They are HWND-less inside the top-level window, like WPF [I].

### G1. Double, right and middle click on an Element (row 1)

Ranked techniques:

1. **A UIA semantic equivalent instead of the gesture.**
   - Double-click usually means "open": Invoke or SelectionItem plus Invoke on list items [I].
   - Right-click means "context menu": see G6.
   - This is headless with the existing machinery, but it is not a double-click, so winuse should never silently substitute it. It belongs in the skill as advice, not as a rewrite.
2. **Posted mouse messages to the control's own HWND** (W32 only).
   - Send WM_LBUTTONDOWN/UP, WM_LBUTTONDBLCLK, WM_RBUTTONDOWN/UP, WM_MBUTTONDOWN/UP with client coordinates, to the deepest child from `ChildWindowFromPointEx` of the target Window, not `WindowFromPoint`. That way a covered Window is still addressed correctly.
   - **[M]** A posted WM_LBUTTONDOWN/UP/DBLCLK/UP to a background `Edit` selected the word ("hello ", sel 0..6). The foreground was unchanged.
   - **[M]** A posted WM_RBUTTONDOWN/UP to a background `Edit` opened its context menu (one visible `#32768`). The foreground was unchanged.
   - **[M]** The same messages to charmap's `RICHEDIT50W` did nothing: RichEdit has no built-in menu, and the posted double-click did not select there.
   - **[C]** cua uses this as its Win32 background path (`libs/cua-driver/rust/crates/platform-windows/src/input/mouse.rs:138-200`, MIT). It is documented not to work for UWP context menus.
   - **[C]** agent-desktop keeps these Headed: `crates/windows/src/actions/physical_click.rs:1-3`, Apache-2.0.
   - Risk: the app may read GetKeyState or GetCursorPos (drag thresholds, hit testing outside the message). Posted button-down can leave a stale capture, so send WM_CANCELMODE after. There is no readback for a generic double-click.
3. **Synthetic pen or touch** (`InjectSyntheticPointerInput`).
   - **[C]** cua: `input/inject.rs:237-375`. It works only when the target is topmost at the point. It sets WS_EX_NOACTIVATE, then calls SetForegroundWindow on the user's previous window afterwards. Its own comments say pen injection "drags the user's mouse pointer along" (`inject.rs:589-591`).
   - This violates "no real cursor", so it is rejected.

**Verdict.** Feasible for W32 as Posted Clicks (opt-in, best effort, like Posted Keys). Partial overall. Impossible for XAML/WPF/CHR/Qt/Java without the real pointer. CDP Pages already do every button and count.

### G2. Point forms `@x,y`: click, set-value, secondary action, scroll (rows 2, 4, 5, 7)

1. **Resolve the point inside the target Window**, never against the desktop.
   - For UIA: walk the target Window's own subtree for the smallest Element containing the point. That is cua's "(x, y) mode" UIA hit-test (`WINDOWS.md`). Then run the existing Headless Element route on it.
   - This removes ADR 0010's objection: the point no longer names the topmost Window.
   - Works for all frameworks that expose UIA bounds.
   - Risk: overlapping siblings, or a canvas that is one big Element.
2. W32 point clicks: Posted Clicks to the child HWND under the point (G1).
3. **Scroll at a point.**
   - Best: ScrollPattern on the smallest scrollable Element containing the point (this reuses `ScrollElement`).
   - W32: post WM_MOUSEWHEEL (screen coordinates in lParam) or WM_VSCROLL to the child HWND [I, unmeasured].
   - **[C]** cua refuses point scroll in background mode (`tools/scroll.rs:236-244`).

**Verdict.** Feasible for click, set-value and secondary action by re-targeting the point to an Element (all UIA frameworks). Scroll at a point is partial: feasible where a ScrollPattern ancestor exists, impossible for a raw wheel into a canvas. Conflicts with ADR 0010, Decision 1 and the CONTEXT.md "Headed Action" definition.

### G3. Hover (row 10) and native drag (row 9)

- **Hover**: a posted WM_MOUSEMOVE to a W32 child makes the control believe the mouse is there [I]. Tooltips (TTF_SUBCLASS / TrackMouseEvent) partly follow posted moves [I, unmeasured], but the real cursor elsewhere sends the next real WM_MOUSEMOVE only if the user moves over that window.
  - XAML, WPF and CHR derive hover from the real pointer device. No message reaches them [I]. cua has no background hover at all (`tools/move_cursor.rs:27-31,56`).
- **Drag**:
  - W32 in-control drags: posting DOWN, MOVEs, UP works for simple trackers [C: cua `mouse.rs:285-340`], but many controls call `DragDetect`/GetAsyncKeyState [I].
  - **OLE drag-and-drop** (`DoDragDrop`) runs a modal loop driven by the real mouse state, and the drop target is registered through a private, marshaled `RegisterDragDrop` registration [I]. There is no message route.
  - cua's touch route for WPF "drags the system cursor… can't be prevented" (`inject.rs:595-682`).
  - CDP Pages already drag Headless; CDP also has `Input.dispatchDragEvent` for HTML5 drops with data and files.

**Verdict.** Native hover: **impossible** (W32 partial and unreliable). Native drag: **impossible** except W32 in-control trackers (partial). OLE drag-and-drop between apps: **impossible**.

### G4. Chords: Ctrl+S, Ctrl+V, Alt+F… (rows 12, 13)

1. **Accelerator through attached key state** (new, W32).
   - Steps: AttachThreadInput(helper thread, the target Window's thread), then SetKeyboardState with VK_CONTROL/VK_LCONTROL down, then post WM_KEYDOWN/UP 'S', then restore the key state and detach.
   - **[M]** A separate background process with a Ctrl+S accelerator (TranslateAccelerator loop) got `WM_COMMAND id=201` (an accelerator). The foreground was unchanged. The user's async Ctrl state was 0 before and after.
   - **[M]** The same post without the attached key state reached WM_KEYDOWN with GetKeyState(CTRL)=0 and no accelerator.
   - This contradicts the "cannot carry modifier state" finding in agent-desktop A4-5 and cua `hotkey.rs:303-330`. Both tried plain posting only.
   - Risks:
     - Never attach to the **foreground** thread: that shares the user's keyboard state. Refuse in that case.
     - AttachThreadInput to a hung thread can stall the helper [I]: do it on a disposable thread with a timeout.
     - Apps that read GetAsyncKeyState or raw input (games, some custom editors) will not see it [I].
     - Alt chords that open menus need WM_SYSKEYDOWN and the menu loop: unmeasured.
   - Frameworks: W32 (TranslateAccelerator), MFC, WinForms [I]. WPF reads modifiers through Win32KeyboardDevice/GetKeyState [I, unmeasured]. Chromium ignores posted keys entirely (agent-desktop A4-4; `probe-results.md` section 5).
2. **The UIA AcceleratorKey lookup**: find the Element whose AcceleratorKey matches, then Invoke it.
   - **[C]** cua `tools/hotkey.rs:262-300`.
   - Fully Headless and verifiable through the Element, for every UIA framework. Coverage is limited to commands that have a visible, labelled Element.
3. **Menu command id** (G7): Ctrl+S is usually File > Save. Post WM_COMMAND with the menu id.

**Verdict.** Feasible (W32 via 1 and 3; XAML/WPF partial via 2). Impossible for CHR without CDP and for raw-input apps. Conflicts with the ADR 0011 Decision 5 refusal of posted modifiers and with the spec out-of-scope list ("Posting chords with modifiers").

### G5. Typing into fields without a per-control HWND (row 11)

1. **Caret-aware ValuePattern splice.**
   - Read the selection or caret with TextPattern.GetSelection, measured against DocumentRange in UTF-16 units (agent-desktop already reads these: `crates/windows/src/tree/text_selection.rs`).
   - Then `SetValue(prefix + text + suffix)` and read it back.
   - This is better than cua's append-only `SetValue(current + text)` (`tools/type_text.rs:405-430`).
   - Frameworks: WPF TextBox, XAML TextBox, CHR fields with Value+Text.
   - Risks:
     - It is a whole-value replace: it loses the undo stack and rich formatting and fires no keydown events, so web/Electron validators may not run.
     - Fields without TextPattern (password boxes) get append-only or a refusal.
     - The Electron "echo is not proof" caveat applies (cua).
2. **W32 RichEdit or Edit**: EM_REPLACESEL is already done.
3. **Electron/WebView2 via CDP** (G12): then real `Input.insertText`.
4. Posted WM_CHAR does nothing in CHR (A4-4) or XAML [I].

**Verdict.** Partial. set-value already covers whole-value writes. Keystroke-faithful typing (IME, key events, autocomplete) is impossible headless outside W32 and CDP.

### G6. Context menus (no command today)

1. **UIA `IUIAutomationElement3::ShowContextMenu`** [D: https://learn.microsoft.com/en-us/windows/win32/api/uiautomationclient/nf-uiautomationclient-iuiautomationelement3-showcontextmenu, "Programmatically invokes a context menu on the target element"].
   - **[C]** Chromium implements it as AX action `kShowContextMenu` (`ui/accessibility/platform/ax_platform_node_win.cc:5849-5858`).
   - **[M]** On a W32 `Edit`, through the MSAA proxy, it returned S_OK and showed **no** menu. On charmap's RichEdit: S_OK, no menu. A W32 control without its own provider therefore gets a silent no-op, so the route needs a readback (a menu-opened event or a new `#32768` window).
   - XAML: providers implement `IRawElementProviderSimple2::ShowContextMenu` [I, unmeasured].
2. **Posted WM_CONTEXTMENU (wParam = control, lParam = -1)** to a W32 HWND.
   - **[M]** The `Edit` context menu appeared with the foreground unchanged.
3. **Choosing the item.** The popup is a topmost `#32768` window owned by a background thread. Options:
   - UIA Invoke on the menu item [unmeasured];
   - MN_GETHMENU, then post WM_COMMAND with the item id to the owner [I];
   - dismiss with WM_CANCELMODE. **[M]** This closed it.
4. **UX risk.** A topmost menu pops up over the user's work and closes if the user clicks anywhere. It is visible but steals nothing. It is acceptable with the Agent Cursor if it is short-lived.

**Verdict.** Feasible for W32 (route 2) and CHR (route 1). Partial for XAML (unmeasured). Impossible by posted messages for UWP (cua docs).

### G7. Menu bars (no command today)

1. **W32 `GetMenu` + `GetMenuString`/`GetMenuItemID`/`GetMenuState` (MF_GRAYED), then post WM_COMMAND(id).**
   - **[M]** A posted WM_COMMAND(101) to a background window was handled, with the foreground unchanged.
   - Caveat [I]: no WM_INITMENUPOPUP is sent, so apps that enable or disable items lazily may show stale grey states. Optionally send WM_INITMENUPOPUP first.
2. **UIA ExpandCollapse on a W32 menu bar item.**
   - **[M]** Expand on "File" in a background window failed with HRESULT 0x80131509 (invalid operation) and opened no menu. So the existing Headless click route cannot open classic menu bars.
3. XAML/WPF/Office ribbons: ExpandCollapse/Invoke on their own UIA menus is the existing route. It is headless if the provider does not self-activate [I].
   - cua wraps calls in `EnableWindow(FALSE/TRUE)` on the host and measured that this stops UWP/Chromium self-activation on Invoke: 91% steals before, 0/507 after (`uia/fg_bypass.rs:1-30`, MIT). That is worth adopting for the existing click route.
4. CHR/Electron app menus could not be opened even with real input (agent-desktop A23-3, A24-6, A24-12).

**Verdict.** Feasible for W32 (route 1). Partial elsewhere.

### G8. Text selection and caret (no command today)

- UIA `TextRange.Select` (ITextRangeProvider::Select) on WPF, XAML, CHR and RichEdit providers. It is headless with AutoSetFocus off [I, unmeasured].
- EM_SETSEL for Edit and RichEdit (W32). **[M]** It worked cross-thread in the probe.
- CDP: a `Runtime` / `Selection` API.

**Verdict.** Feasible.

### G9. Clipboard paste

- WM_PASTE works for Edit and RichEdit only [I], and EM_REPLACESEL already covers those better.
- Anything else needs Ctrl+V (G4) plus setting the clipboard. That clobbers the user's clipboard and is recorded by clipboard history [I].

**Verdict.** Partial, low value: prefer set-value or splice. Never touch the user's clipboard without opt-in.

### G10. Raise (row 6) and activate-window (row 14)

- **Raise**:
  - For MDI children: post WM_MDIACTIVATE to the MDI client. This activates inside the frame without the frame becoming foreground [I].
  - For top-level windows: SetWindowPos(HWND_TOP, SWP_NOACTIVATE) changes only the z-order and covers the user's work without focus [I]. That is visible intrusion, and the "Raise" semantics expect focus.
  - Verdict: partial (MDI); otherwise keep Headed.
- **activate-window** is impossible headless by definition. Keep it.

### G11. launch-app and browser launch (rows 19, 20)

Measured **[M]**: charmap.exe launched from a background process (this probe's parent was not the foreground app).

| Launch mode | Foreground changed | New window iconic |
|---|---|---|
| CreateProcess, no STARTUPINFO show | no | no |
| CreateProcess SW_SHOWNOACTIVATE | no | no |
| CreateProcess SW_SHOWNA | no | no |
| CreateProcess SW_SHOWMINNOACTIVE | no | **yes** (STARTUPINFO honoured) |
| ShellExecuteEx nShow=1 or 4 | no | no |

- Even the default did not steal from a background launcher. The foreground lock applies [D: SetForegroundWindow rules, https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setforegroundwindow].
- **Inference:** winuse's helper sometimes *does* hold foreground rights. `Activate` injects a SendInput move (`activate.go:132`), which makes the helper "the process that received the last input event" until the user's next input, and children it launches inherit that right. This explains the Chrome probe ("launch took the foreground").
- **Fix:**
  - Launch with STARTUPINFO `STARTF_USESHOWWINDOW` + SW_SHOWNOACTIVATE, or SW_SHOWMINNOACTIVE when `--minimized`. Go's `syscall.SysProcAttr` has only `HideWindow`, so this needs a direct `CreateProcess` with `StartupInfo.ShowWindow` (probe `main.go`, `launchCP`).
  - ShellExecuteEx nShow=SW_SHOWNOACTIVATE.
  - The first ShowWindow uses the STARTUPINFO value [D: ShowWindow, "This parameter is ignored the first time an application calls ShowWindow, if the program that launched the application provides a STARTUPINFO structure"].
  - Verify the foreground after the launch and report it honestly when the App took it.
  - cua does the same, plus `LockSetForegroundWindow` and a SendInput `VK_NONAME` to burn the new process's activation right (`tools/launch_app.rs:207-222`; `inject.rs:66-105`). The VK_NONAME trick is SendInput, so it is rejected here.
- **Packaged/UWP apps:**
  - `ACTIVATEOPTIONS` in SDK 10.0.26100 has only AO_NONE, AO_DESIGNMODE, AO_NOERRORUI, AO_NOSPLASHSCREEN and AO_PRELAUNCH. **There is no no-foreground option** [C: `shobjidl_core.h:28942-28946`; D: https://learn.microsoft.com/en-us/windows/win32/api/shobjidl_core/nf-shobjidl_core-iapplicationactivationmanager-activateapplication].
  - So packaged launches are partial: they rely on the foreground lock only.
- **Browser launch:**
  - Pass the same STARTUPINFO. Chrome's honouring of it is unmeasured.
  - Or `--no-startup-window`, then `Target.createTarget {newWindow:true, background:true}`. The probe measured that this call keeps the foreground (`probe-results.md` section 2). Whether Chrome stays alive with zero windows under remote debugging is unmeasured [I].

**Verdict.** Feasible for Win32 apps (rely on the lock plus a no-activate show state, then verify). Partial for packaged apps and apps that call SetForegroundWindow themselves.

### G12. Electron and WebView2 apps (CHR without a connection)

- **Electron**: launch the app with `--remote-debugging-port=0` on loopback and read `DevToolsActivePort`, as for Chrome.
  - Electron's fuses gate `--inspect` (`nodeCliInspect`), `ELECTRON_RUN_AS_NODE` and `NODE_OPTIONS`. **No fuse gates the Chromium `--remote-debugging-port` switch** [D: https://www.electronjs.org/docs/latest/tutorial/fuses, which lists 9 fuses].
  - An app can still strip switches in its own code [I].
- **WebView2**: set `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--remote-debugging-port=…` in the launched App's environment. It is appended to the app's own arguments when the environment is created [D: https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/webview2-idl, CreateCoreWebView2EnvironmentWithOptions]. A registry policy `…\Edge\WebView2\AdditionalBrowserArguments` also exists. Machine-wide policy is too invasive.
- Both are **launch-time only**. An already running app needs a restart, and the user must agree to it.
- agent-desktop does `launch --cdp` and refuses running apps (`apps/docs/.../chromium-and-electron.mdx`).
- Risk: an open port is drivable by any same-user process. Reuse ADR 0012's loopback and owner checks.

**Verdict.** Feasible for apps winuse launches; impossible for a running one. Conflicts with ADR 0012 ("Electron, WebView2 stay on UI Automation until a later decision") and the browser-merge spec's out-of-scope list.

### G13. Tab switch on a launched browser (row 17)

1. UIA SelectionItem.Select or Invoke on the tab-strip TabItem: the existing Headless click route on a frame Element.
   - **[C]** Chromium's Select goes to `ISelectionItemProviderSetSelected` (`ax_platform_node_win.cc:3205-3209`).
   - Whether the tab strip activates the window is unmeasured [I]. cua's EnableWindow guard applies.
2. Load the winuse extension into launched profiles, then use the extension's `chrome.tabs.update`. This path is already measured Headless.
   - `--load-extension` is no longer honoured by branded Chrome 137+ [I, from memory; verify].
   - CDP `Extensions.loadUnpacked` needs `--enable-unsafe-extension-debugging` [I; verify].

**Verdict.** Feasible pending one probe.

### G14. Minimized windows (row 16)

- **[M]** charmap's UIA raw tree had 14 Elements both minimized and restored. The root bounds were Empty when minimized.
- **[M]** The same probe's `FindFirst` found no Button, so pattern calls on a minimized Window were not measured here.
- cua claims UIA Invoke works on minimized windows (`tools/launch_app.rs:177`).
- Electron's tree drops to 9 nodes when minimized vs 30 when restored (agent-desktop A24-5).
- CDP Pages work minimized (`probe-results.md` section 4).
- The Agent Cursor has nowhere to travel. Show it at the taskbar button, or skip it.

**Verdict.** Partial: for W32 and WPF (HWND-backed trees), allow Headless pattern calls on a minimized Window after a probe. Keep refusing CHR via UIA.

### G15. A cold, covered Chromium UIA tree

- Chromium marks covered windows OCCLUDED through its native occlusion tracker (`ui/aura/native_window_occlusion_tracker_win.cc:268,581`). The feature is `CalculateNativeWinOcclusion`, in `ui/base/ui_base_features.cc`.
- For browsers winuse launches, `--disable-features=CalculateNativeWinOcclusion` should keep them rendering and building the tree [I].
- With a Browser Connection, the Page does not need UIA anyway.

**Verdict.** Feasible for launched browsers; impossible to force on the user's own browser.

### G16. IME composition, file dialogs, admin windows

- **IME**: composition needs real keystrokes into the IME. `Input.imeSetComposition` exists on CDP only.
  - Verdict: impossible natively; CDP only.
- **File dialogs** (IFileDialog): the name field is a W32 Edit/ComboBox, so `set-value` or EM_REPLACESEL, then dialog Return. This is already Headless today [I].
- **Administrator windows**: UIPI blocks posted messages and pattern writes alike (A24-16, ADR 0011).
  - Verdict: impossible from a medium-integrity helper. This is the existing refusal.

---

## 3. What is impossible to do headless, and the fallback

Impossible on the user's interactive desktop, with no SendInput, no SetCursorPos and no foreground change:

1. Hover on anything other than W32: XAML, WPF, CHR frame, Qt, Java, games.
2. Native drag in non-W32 frameworks, and OLE drag-and-drop between apps.
3. Click or scroll at an arbitrary point of a surface with no Element and no HWND (canvases, custom-drawn Qt/Java/game views).
4. Keys and chords into CHR without CDP (A4-4), into apps that read GetAsyncKeyState or raw input, and into UWP with keystroke semantics.
5. IME composition outside CDP.
6. activate-window (by definition), and Raise of a top-level window (it means "take focus").
7. Guaranteeing that a launched or restored App never takes the foreground itself. Apps can self-activate when they hold the right (**[M]** charmap on restore). There is no ACTIVATEOPTIONS flag for packaged apps.
8. Driving administrator windows from a medium-integrity helper.
9. Attaching CDP to an Electron or WebView2 app that is already running.

**Isolation alternatives.** The agent's research is summarised here; its citations are in its report.

- **Microsoft Execution Containers (MXC) session isolation.**
  - It went GA on 2026-10-07 (https://blogs.windows.com/windowsdeveloper/2026/10/07/microsoft-execution-containers-policy-driven-containment-for-ai-agents/). It runs a separate account and session, but **SendInput returns ERROR_ACCESS_DENIED inside it** (microsoft/mxc design doc `docs/development/architecture/backends/isolation-session/state-aware-rust.md`).
  - The App also has to run in that session, invisible to the user.
  - So it isolates winuse rather than making hover or drag possible.
  - The OS API may still be behind an internal flag: the sources conflict.
- **Agent Workspace and Copilot Actions.** Private preview, first-party or trusted-signed packaged agents only (https://learn.microsoft.com/en-us/windows/security/book/operating-system-agentic-security).
- **CreateDesktop hidden desktops.** SendInput reaches only the input desktop, and messages flow only within a desktop (https://learn.microsoft.com/en-us/windows/win32/winstation/desktops).
- **RDP loopback.** A second session on Pro is not supported. UFO²'s "Picture-in-Picture" RDP-loopback desktop is documented as planned but not shipped (`documents/docs/faq.md:309`).
- **Windows Sandbox or a VM.** Full input, but a separate machine with no access to the user's apps.

None of these lets winuse operate the **user's own** running apps headless with real input.

**Recommended fallback: keep fail-closed, opt-in Headed (ADR 0010, Decision 3).**
- Every impossible case refuses before anything is sent, with "nothing was sent" and the exact `--headed` retry.
- New best-effort message routes (Posted Clicks, attached-state chords) stay **opt-in like `--post`**, because Windows does not promise that apps honour them and most have no readback. They are never an automatic fallback.
- Routes with a readback (point to Element, UIA accelerator lookup, WM_COMMAND with a grey-state check, ShowContextMenu with a menu-opened check) can be Headless by default.

---

## 4. Proposed tickets

| # | Title | Scope | Effort | ADR / doc conflicts |
|---|---|---|---|---|
| T1 | Launch without the foreground | `apps/launch.go` and `browser/system_windows.go`: CreateProcess with STARTUPINFO SW_SHOWNOACTIVATE (`--minimized`: SW_SHOWMINNOACTIVE); ShellExecuteEx nShow=4; verify the foreground after and say when the App took it. Avoid launching right after a Headed Action's SendInput (foreground right) [I]. | S | none (launch is not an Action) |
| T2 | restore-window: verify the foreground, document self-activation | After ShowWindowAsync, compare the foreground; output `ok (the App brought itself to the foreground)`; add an e2e test on a `#32770` dialog app (charmap). | S | ADR 0011 Decision 2 wording ("only non-activating calls" is true, the outcome is not) |
| T3 | Point forms resolve to an Element inside the target Window | `@x,y` for click / set-value / secondary action, and scroll-at-point through ScrollPattern: hit-test the target Window's own UIA subtree, then the Headless route. Refuse when only a canvas Element matches. | M | ADR 0010 Decision 1; CONTEXT.md "Headed Action" |
| T4 | Posted Clicks for classic controls (opt-in) | `click --post` for double/right/middle and point clicks; posted WM_*BUTTON* and WM_MOUSEWHEEL to the child HWND from ChildWindowFromPointEx; WM_CANCELMODE after; W32 class allow-list; best-effort output like Posted Keys. | M | ADR 0011 Decision 5 (extends Posted to the mouse); headless-parity spec out-of-scope list; CONTEXT.md new term |
| T5 | Chords via attached key state (opt-in) | `press-key --post` accepts modifiers: AttachThreadInput (refuse if the target thread is the foreground thread) + SetKeyboardState + posted keys, on a disposable thread with a timeout; restore the state. | M | ADR 0011 Decision 5 and its "chords are refused" consequence; spec out-of-scope |
| T6 | Accelerator lookup route | `press-key <chord>` (no flag) first looks for a UIA Element whose AcceleratorKey matches and Invokes it, Headless and verifiable; otherwise the existing routing. | S | ADR 0011 Decision 4 (one more live-Window route) |
| T7 | invoke-menu for classic menu bars | `invoke-menu <window> "File>Save As…"`: GetMenu, item strings, grey check, optional WM_INITMENUPOPUP, posted WM_COMMAND. UIA menus stay on click/expand. | M | none |
| T8 | Context menu Secondary Action | `perform-secondary-action <el> ContextMenu`: UIA ShowContextMenu, else WM_CONTEXTMENU to the HWND; verify a menu opened (`#32768` or MenuOpened event), then the agent acts on the menu Window; probe item Invoke first. | M | CONTEXT.md Secondary Action list |
| T9 | EnableWindow guard against self-activation | Wrap Headless Invoke/Select/Expand on XAML/CHR hosts with EnableWindow(FALSE)…(TRUE) (cua's measured fix); measure on Chrome tab items. | S | none |
| T10 | Text selection | `select-text <window> <el> <start> <len>` / `all`: TextRange.Select, or EM_SETSEL; read back. | S | CONTEXT.md Action list |
| T11 | Caret-aware typing into TextPattern fields | Headless `type-text` for a focused WPF/XAML/CHR field with Value + Text patterns: splice at the caret, verify, refuse rich text and password fields; keystroke-faithful typing stays Headed. | M | headless-parity spec out-of-scope ("Headless typing into WPF, WinUI or Chromium") |
| T12 | Tab switch on launched browsers | Probe UIA Select on the tab item for a foreground change; if clean, make it the Headless route; else load the extension into launched profiles. | S | ADR 0012 / browser-merge spec "Tabs" (Headed on launched browsers) |
| T13 | Electron and WebView2 Browser Connections at launch | `launch-app <app> --cdp`: Electron `--remote-debugging-port`, WebView2 via the environment variable; port-owner verification; map Window to Page. | L | ADR 0012 Decision 6 and its Consequences; browser-merge spec out-of-scope |
| T14 | Headless actions on minimized W32 windows | Probe pattern calls on minimized W32/WPF; if they work, relax `HeadlessReady`'s minimized refusal for HWND-backed trees; cursor at the taskbar button. | S | ADR 0010 (the minimized refusal); CONTEXT.md |
| T15 | Launched-browser occlusion flag | Add `--disable-features=CalculateNativeWinOcclusion` to winuse-launched browsers. | S | none |
| T16 | Document the impossible list | The skill and CONTEXT.md state what can never be Headless (section 3), with the refusal wording. | S | CONTEXT.md "Headed Action" |
| T17 | Spike: MXC session container | Evaluate running winuse plus launched apps inside an MXC session (UIA, PostMessage and CDP only); confirm API availability. | M (spike) | ADR 0002/0003 if it needs the Rust/NuGet SDK |

**17 tickets:** 9 S, 7 M (one of them a spike), 1 L.

---

## 5. Probe record

- **Source:** `C:\Users\ADMIN\AppData\Local\Temp\claude\C--Users-ADMIN-Desktop-nguyenvu-winuse\7f81330a-865c-4df8-b176-5cc7ba425b1b\scratchpad\probe\` (`main.go`, `selfwin.go`, `restore*.go`, `chord.go`, `minuia.ps1`).
- **Targets:** only charmap.exe instances and windows the probe created. Each was closed with WM_CLOSE, or terminated if it was still open after 3 s. No user window was touched.
- **Foreground during the runs:** the user's Chrome, and at times a `WinuseFixtureWindow` belonging to another process. Every "fgChanged" figure compares the window before and after each step.
- **Caveat on G6/G7:** the UIA client and the window shared one process (different threads). Cross-process behaviour of ShowContextMenu and menu Expand was not measured.
- **Raw results:**
  - Launch modes: all `fgChanged=false`; SW_SHOWMINNOACTIVE gave `iconic=1`.
  - Restore of charmap with ShowWindow / SetWindowPlacement / SC_RESTORE: `targetBecameFg=true` for every guard. The probe's own window with ShowWindow / SetWindowPlacement: `false`; with posted SC_RESTORE: `true`.
  - UIA ShowContextMenu on an Edit: `S_OK`, 0 menus. Posted WM_CONTEXTMENU: 1 menu. Posted right-click: 1 menu. WM_CANCELMODE: 0 menus. Posted double-click: `sel=0..6`. In every case `fgChanged=false`.
  - UIA Expand on the "File" menu bar item: `hr=0x80131509`. Posted WM_COMMAND(101): handled.
  - Chord: posted 'S' gave `WM_KEYDOWN GetKeyState(CTRL)=0` and no command. Attached state gave `WM_COMMAND id=201`. User async Ctrl before and after: `0x0`. `fgChanged=false`.
  - charmap UIA tree: 14 Elements minimized and 14 restored.
