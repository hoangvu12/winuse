# 09: ContextMenu Secondary Action

**What to build:** `perform-secondary-action <window> <element> ContextMenu` opens the Element's context menu without the foreground, verifies that a menu opened, and lets the agent act on it.

Routes (`../headless-gaps/headless-100.md` G6):

1. UIA `IUIAutomationElement3::ShowContextMenu`. Chromium implements it (`ax_platform_node_win.cc:5849-5858`). Measured: on a classic Edit and on RichEdit it returned S_OK and showed no menu, so it needs a readback.
2. Classic HWND: post WM_CONTEXTMENU (wParam = the control, lParam = -1). Measured: the Edit's context menu appeared, foreground unchanged.

Readback: a new `#32768` window, a UIA MenuOpened event, or a new popup Window of the App within the verify wait. None: VerifyError "delivered; no menu opened".

Then:

- Print the menu's Window ID so the agent reads it with `get-window-state` and clicks an item (the Headless click route). Unmeasured: whether UIA Invoke on a classic `#32768` item works headless. Fallback: MN_GETHMENU on the menu window, then post WM_COMMAND to the owner.
- A menu left open is closed with WM_CANCELMODE (measured) when the agent's next Action targets another Window, or by `press-key <menu> Escape`.
- The menu is topmost and appears over the user's work. It takes nothing, but it is visible. The Agent Cursor shows it.

**Blocked by:** none

**Status:** done

- [x] Probe: cross-process WM_CONTEXTMENU, item Invoke on `#32768`, ShowContextMenu on Chrome and a WinUI app
- [x] Route selection and readback
- [x] Menu Window reported in the output; cancel path
- [x] Offline tests: route per Element kind, readback timeout
- [x] Opt-in e2e: fixture edit box and Chrome frame, covered
- [x] CONTEXT.md Secondary Action list; SKILL.md

## Comments

### 2026-11-02: safe live batch (aborted)

- Fixture's drawn text field, cross-process [M]: UIA ShowContextMenu returned S_OK and opened no menu, and a posted WM_CONTEXTMENU(-1) opened no menu either. This confirms the readback requirement.
- Not measured: the real Edit routes, item choice (UIA Invoke on `#32768`, MN_GETHMENU, posted mnemonic), and the WPF TextBox. The guard fired while the fixture's Classic Dialog opened: its own `ShowWindow(SW_SHOW)` took the foreground from the League client.
- Recommendation: keep `needs-triage`. Rerun with the user away. Details: `../../headless-gaps/probes-safe-batch.md`.

### 2026-11-03: triage decision (user)

Build it once the probe has measured the routes. It is also the preferred route for right-clicks on an Element (ticket 12's posted right-click is the fallback). Probe: cross-process WM_CONTEXTMENU on a classic Edit, ShowContextMenu on Chrome and on a WPF or WinUI window the probe builds, and item choice (UIA Invoke on `#32768`, MN_GETHMENU). the laptop probe chat (358cd6b7) runs it; the user is never at the laptop during probes.

### 2026-11-03: routes measured (`../headless-gaps/probes-round4.md` section 2)

Routes per framework:

| Framework | Open the menu | Pick an item | Close |
|---|---|---|---|
| Classic Win32 | posted WM_CONTEXTMENU(control, -1); readback: a new `#32768` of the target pid | read the items through MN_GETHMENU (names and ids), then **post the item's mnemonic key to the `#32768`**; WM_COMMAND to the owner only when the menu is not TPM_RETURNCMD (verify by the menu closing and the effect) | WM_CANCELMODE to the owner |
| Chromium (frame and page Elements) | UIA ShowContextMenu on the Element (not the top-level or RWHV); readback: a new Chrome_WidgetWin_1 popup of the same pid | UIA Invoke on the popup's MenuItem (foreground unchanged) | WM_CANCELMODE to the top-level |
| WinUI 3 | UIA ShowContextMenu; readback: new `PopupHost` windows | not yet measured | WM_CANCELMODE does not close it; use Escape through Posted Keys or UIA, not yet measured |
| WPF | no Headless route (ShowContextMenu and WM_CONTEXTMENU both open nothing in an inactive window) | | |

Rules:

- **Never UIA Invoke an item of a classic `#32768` menu**: it took the foreground (measured).
- **Never SendMessage WM_CONTEXTMENU**: TrackPopupMenu is modal and would block the helper.
- WPF and anything with no readback refuse with "nothing was sent" and the `--headed` hint.
- Choosing an item is part of this ticket: `perform-secondary-action <window> <element> ContextMenu` reports the menu's Window ID, and the item is clicked by its index in the menu's Window State. The helper routes that click by the menu's class (mnemonic for `#32768`, Invoke for Chromium).
- The laptop probe for this ticket is done.

### 2026-11-03: WinUI and Chrome items measured (`../headless-gaps/probes-round5.md` section 2)

This updates the route table above.

- **WinUI 3: pick and close are measured.**
  - The menu is a `PopupHost` (PopupWindowSiteBridge) top-level. Dedupe its items against the main tree.
  - **UIA Invoke on a MenuFlyoutItem or AppBarButton works and keeps the foreground** (unlike classic `#32768`).
  - Close it with Window.Close on the Popup element, or Escape posted to the popup's InputSiteWindowClass. Not WM_CANCELMODE, not Escape to the PopupHost or the top-level.
  - The App sees a Window.Activated event, but the system foreground does not change.
- **Chrome:**
  - Items are Invoke-able and named with their shortcut. Close with Escape posted to the menu window (Collapse fails).
  - **Edit commands in the menu act on Chrome's current focus, not on the right-clicked Element** (Select all selected the document). Only report and allow commands that do not depend on focus, or tell the agent that edit items may act elsewhere. Page Elements should keep using CDP.
  - A fully covered Chrome without the occlusion flag has no page tree at all. The UIA route applies only to browsers winuse launched (ticket 04's flag) or to visible ones.

### 2026-11-03: never Collapse a Chromium menu

On a second open Chrome menu, UIA Collapse on the SubmenuView hung the UIA client indefinitely in 1 of 3 runs (`../headless-gaps/probes-round5.md` section 4). Close Chromium menus only with Escape posted to the menu window.

### 2026-11-03: landed (laptop, branch `laptop/hm-09-context-menu`)

The scope decisions, from the orchestrators: the #32768 item pick is a narrow click branch; both close paths are built, and the automatic close applies only to a menu the Agent's own Action opened; ContextMenu is never listed in the Window State.

What landed:

- **Open.** `perform-secondary-action <window> <element> ContextMenu` picks a route from the Element's UIA FrameworkId and NativeWindowHandle:
  - Win32 or WinForms (or no framework) with its own HWND: posted WM_CONTEXTMENU(control, -1), never sent.
  - WPF: refused with "nothing was sent" and the `--headed` retry, which right-clicks the Element with the real pointer (ADR 0013, "What stays impossible").
  - Everything else (Chromium, XAML and the rest): `IUIAutomationElement3::ShowContextMenu` (vtable slot 91, SDK 10.0.26100.0).
  - The @x,y form is refused with the `--headed` retry. Page Elements keep the Page refusal (use CDP).
- **Read-back.** The desktop-parity 02 snapshot (`opened.go`, `snapshotOf`): a new menu or popup Window of the target's process within 2 s. If none appears, the Action fails as delivered ("... but no menu opened") with the `--headed` retry.
- **Output.** `ok (the context menu opened: read it with winuse get-window-state <id>, click an item, or close it with winuse press-key <id> Escape)`, plus `Note: a new Window opened: ...` for each new Window. A Chromium menu adds that its edit items act on the browser's focus, not on the Element (round 5). The Headed form (a right click) reports the menu through the opened-Window diff.
- **Picking a #32768 item** (`menuitem.go`). `click <menu> <ref>` (left, single, no modifiers) never reaches UIA Invoke:
  - MN_GETHMENU gives the HMENU, read with `win.ReadMenu`, and the Element's name is matched with invoke-menu's `matchMenuItems`.
  - If the item's mnemonic is unique among the menu's enabled items (submenus count), it is posted to the #32768. Otherwise WM_COMMAND(id) is posted to GetGUIThreadInfo's hwndMenuOwner.
  - Read-back: the menu closed within 2 s. If it is still open, it is closed with WM_CANCELMODE and the click fails with the exact `--headed` retry: for a menu this Agent's ContextMenu opened, the command that reopens it, then the click.
  - A submenu, a greyed item, an unknown or ambiguous name, a menu that cannot be read, an item with no owner, an ID over 0xFFFF, and a click at a point are all refused with nothing sent and the `--headed` retry.
  - Chromium and WinUI 3 items keep today's Headless click (UIA Invoke, which keeps the foreground there).
- **Closing.**
  - `press-key <menu> Escape` on a #32768 or a WinUI 3 PopupHost posts Escape to the #32768, or to the PopupHost's island input windows (`InputSiteWindowClass`), and reads the close back. It is also Headless in a Batch's scope.
  - A Chromium menu popup is a Chrome_WidgetWin_1, so its Escape takes ticket 20's window-message route. **Unmeasured on a menu popup**: `TestContextMenuOnACoveredBrowserPage` is for the PC's live run.
  - The Agent's next Action on another Window closes the menu its own ContextMenu opened, tracked per Agent in `Services.Menus`: WM_CANCELMODE to the menu owner for a #32768, Escape otherwise (never Collapse). It says so in a Note (`Note: closed context menu <id>, which this Agent's ContextMenu had opened and left open`), or in the hint of a failed Action. A menu the user or another Agent opened is never closed. The wrapper runs after the hung-App refusal.

Files:

- New: `internal/helper/contextmenu.go`, `internal/helper/menuitem.go`, `internal/uia/contextmenu.go`, `internal/win/contextmenu.go`, `internal/helper/contextmenu_test.go`, `test/e2e/context_menu_test.go`.
- Edited: `internal/uia/value.go` (the canonical table's ContextMenu row, never offered), `internal/helper/{actions,setvalue,keyboard,headless,opened,state}.go` (one hook each, the Services seams, the wrapper).
- Docs: CONTEXT.md, SKILL.md (the WPF bullet and the usage list), docs/codex-parity.md, ADR 0013's route table, spec row 09.

Tests:

- New offline tests: route per Element kind, the classic post and its report, Chromium ShowContextMenu and its warning, the WPF refusal, the read-back timeout, the @x,y and no-actor refusals, the Headed right click, the automatic close (same Agent only, once; not for the menu itself or a gone menu; Escape for Chromium), press-key Escape (closed, and still open), the mnemonic parse and uniqueness, picking by mnemonic and by WM_COMMAND, the refusals, and the ignored pick.
- Updated: the expected-set strings in the uia, helper and e2e tests.

Checks: `go build ./...`, `go vet ./...`, `go test ./...` and `go vet -tags e2e ./test/e2e` all pass.

Live on the laptop: `TestContextMenuOnACoveredEdit` passed (9.5 s, run alone under `.live-lock`, test-launched fixtures only). On the covered Classic Dialog's name Edit:
- ContextMenu opened the Edit's own #32768, a menu of the fixture's process.
- `click <menu> name:Select All` posted the mnemonic: the menu closed and the Edit selected all 11 characters.
- `press-key <menu> Escape` closed the second menu.
- A click on the cover closed the third, with the Note.
- The cover kept the foreground throughout. No visible #32768 was left afterwards.

The browser leg (`TestContextMenuOnACoveredBrowserPage`) did not run here: it is left to the PC.

Not measured: WinForms ContextMenuStrip through a posted WM_CONTEXTMENU; WinUI 3 live; Chrome live (the PC runs it).

### 2026-11-03: hm-09b live measuring (laptop, branch `laptop/hm-09b-context-menu-measure`)

These are the three unmeasured routes.

**Setup.**
- The user approved the run through the laptop orchestrator. It ran under `.live-lock`, one target at a time.
- Each target was the run's own build, in the scratchpad, never in the repo:
  - a csc.exe (.NET Framework 4.x) WinForms App;
  - an unpackaged WinUI 3 App (Windows App SDK 1.7 from the NuGet cache, .NET 8);
  - for case 3, the same WinForms App in a second mode.
- Every target was shown without activation at 300,300. The fixture covered it, and `activate-window` gave the cover the foreground.
- One Agent drove it all (`WINUSE_AGENT`), through a winuse.exe built from c017f4c.
- Before every pick or Escape, I checked that the menu window existed and was visible (IsWindowVisible).
- Afterwards each target and cover was killed by its PID, and no visible window of those PIDs was left.
- One incident: the first cover was started with PowerShell's Start-Process without `-NoNewWindow`. It opened its console in a new Windows Terminal, which took the foreground. That was the run's own window, not a target. It was killed at once, and every later cover started in the hidden console.

**1. WinForms ContextMenuStrip** (a TextBox with a ContextMenuStrip of &Alpha and &Bravo; FrameworkId `WinForm`, own HWND):
- Open: posted WM_CONTEXTMENU. The menu opened as a `WindowsForms10.Window.20808.app.0.*` drop-down of the App's process (plus a SysShadow, which is not listed). The ok named it, and the cover kept the foreground. The App logged the system foreground as the cover when the strip opened.
- Menu Window State: `menu DropDown` with `menu item Alpha` and `menu item Bravo`. It also adds the "tree is nearly empty" note, wrongly for a menu (cosmetic, not changed).
- Pick: `click <menu> name:Alpha` took today's Headless click (UIA Invoke on the ToolStripMenuItem). It returned `ok` and `Note: window <menu> closed`; the App logged `picked Alpha` and CloseReason ItemClicked. The foreground stayed on the cover.
- `press-key <menu> Escape` was **refused** ("no background route ... only Return or Escape on a classic dialog"), even though the ContextMenu's ok offers it. Nothing was sent, and the menu stayed open.
- The Agent's next Action elsewhere (a click on the cover's check box) closed it with the posted Escape: `Note: closed context menu ...`, CloseReason Keyboard. The foreground stayed on the cover.
- **Fixed:** `win.WinFormsDropDown` (a `WindowsForms10.Window.<style>` class whose style has CS_SAVEBITS, which only the drop-down sets) now joins `#32768` and PopupHost in the menu-Escape route (`menuEscapeStep`, `pressMenuEscape`; also in a Batch's scope). Re-measured live with the fix: `ok`, `Note: window <menu> closed`, CloseReason Keyboard, the foreground on the cover.

**2. WinUI 3 MenuFlyout** (a TextBox whose ContextFlyout is a MenuFlyout of Alpha and Bravo; Native 0):
- Open: ShowContextMenu. A `Microsoft.UI.Content.PopupWindowSiteBridge "PopupHost"` of the App opened, and the note read `cmwinui "PopupHost"`. The system foreground stayed on the cover. The App saw one `CodeActivated` Window.Activated event, as round 5 found.
- Menu Window State: `pane PopupHost > pane > window Popup > menu > menu item Alpha / menu item Bravo`, focus on Alpha.
- Pick: `click <menu> 4` (Alpha) went through UIA Invoke: `ok`, `Note: window <menu> closed`, `picked Alpha`, the foreground kept.
- The PopupHost is hidden, not destroyed, and reused: the same HWND came back at every open. The read-back still sees it as new, because it was not a visible surface before.
- `press-key <menu> Escape` (posted to the InputSite) returned `ok` and closed the menu, foreground kept.
- The next Action elsewhere closed the third open with the Note, foreground kept.
- No fix needed.

**3. FrameworkId "" with an HWND:**
- A raw custom-class child with no provider of its own (RegisterClass, `DefWindowProc` plus WM_CONTEXTMENU) reports FrameworkId **`Win32`** through the system proxy, not "". The empty FrameworkId comes only from a provider of the control's own that sets it. The measured control was a WinForms child whose managed UIA provider returns "" (Name "Provided"): FrameworkId `''`, NativeWindowHandle its HWND.
- Open: the posted WM_CONTEXTMENU (route `menuRoutePost`). Its WM_CONTEXTMENU ran TrackPopupMenu, and a `#32768` of the App opened, foreground kept.
- `click <menu> name:Alpha` posted the mnemonic: `ok`, the menu gone, `picked provided Alpha`.
- `press-key <menu> Escape` closed the second menu.
- The cover click closed the third, with the Note.
- The foreground stayed on the cover throughout. No fix needed: treating "" with an HWND as classic opens the menu when the window answers WM_CONTEXTMENU.

**Fixtures and tests.**
- Case 3 is now a Go fixture control: the Classic Dialog's bare control (`fixture.ClassicBareID` 1005, class `WinuseFixtureBare`, `internal/fixture/bare.go`). Its own provider reports FrameworkId "", Name "Bare control", AutomationId "1005" and Pane. Its `#32768` holds &Fill Name and Select &All, which run the dialog's commands (classicMenu).
- It is the dialog's last control, so the other Element Indexes hold, and the dialog grew 20 DLUs (cy 158 to 178).
- WinForms and WinUI 3 get opt-in e2e tests behind `WINUSE_E2E_WINFORMS_APP` and `WINUSE_E2E_WINUI_APP`. The App sources and build steps are in `test/e2e/README.md`, "Context menus in WinForms and WinUI 3". No C# is in the repo.

Files:
- New: `internal/fixture/bare.go`, `internal/fixture/bare_test.go`, `test/e2e/context_menu_apps_test.go`.
- Edited: `internal/win/contextmenu.go` (`WinFormsDropDown`), `internal/helper/contextmenu.go` (`escapeMenuWindow`), `internal/fixture/{app,classic,elements,win32}.go`, `test/e2e/README.md`.

Tests:
- Offline: `TestWinFormsDropDown`, `TestPressEscapeOnAWinFormsMenu`, `TestLeftWinFormsMenuClosedWithEscape`, `TestMenuEscapeStepPerMenuKind`, `TestClassicBareControlIsLastAndInside`, `TestBareProviderProperties`, `TestBareProviderInterfaces`.
- Opt-in e2e: `TestContextMenuOnACoveredBareControl`, `TestContextMenuOnACoveredWinFormsApp`, `TestContextMenuOnACoveredWinUIApp`.

