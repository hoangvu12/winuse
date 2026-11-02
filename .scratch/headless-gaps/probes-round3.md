# Probes, round 3 (2026-11-03): PC and laptop

Research report from the "winuse multi-agent research" chat (Zeron 690cfc8b) and its laptop probe chat (358cd6b7). No repo code was changed by the probes. Confidence: [M] measured live, [I] inference.

## PC (Win11 Pro 26200, 100% scaling)

Probe kit: `C:\Users\ADMIN\AppData\Local\Temp\claude\C--Users-ADMIN-Desktop-nguyenvu\1e3bb1b4-fa13-4854-b18d-7cc8bca28d79\scratchpad\probe`, logs in `..\out\run-*.log`. Targets: only the probe's own Win32 windows, on DISPLAY2.

- **Posted wheel.** WM_MOUSEWHEEL(-120, screen point) posted to a background LISTBOX: top index 0 to 3. Posted WM_VSCROLL(SB_LINEDOWN): 3 to 4. Foreground unchanged [M].
- **Minimized classic window** (SW_SHOWMINNOACTIVE). It stayed iconic and the foreground was unchanged [M] while:
  - UIA FindAll returned 120 Elements;
  - Invoke on a BUTTON delivered WM_COMMAND;
  - Value.SetValue on an EDIT read back;
  - EM_REPLACESEL worked;
  - SelectionItem.Select on a LISTBOX item gave LB_GETCURSEL=7.
- **Hung app** (UI thread asleep 12 s).
  - ElementFromHandle with ConnectionTimeout 1000 returned UIA_E_TIMEOUT 0x80131505 after 1023 ms. The click was never delivered late [M].
  - Meanwhile other MTA threads Invoked on a healthy app in 17 ms (sharing the same IUIAutomation) and 210 ms (each with its own) [M].
  - SendMessageTimeout(WM_NULL, SMTO_ABORTIFHUNG, 1000) took the full 1000 ms, both before and after IsHungAppWindow became 1 [M]. A hung probe needs a short timeout.

## Laptop (Win11 26200, 2560x1440 at 150%)

Probe code: the laptop scratchpad `C:\Users\HPMEDI~1\AppData\Local\Temp\claude\C--Users-HP-MEDIA-Desktop-nguyenvu-winuse\b9fe24c1-250b-4611-b8bb-e59510809dbb\scratchpad\probe`. Chrome and Edge current stable; Electron 45.2.1 (official zip, checksum verified).

### agent-desktop PR #400 route (open, not merged)

The route: SendMessageTimeout to the top-level `Chrome_WidgetWin_1`.
1. WM_NCACTIVATE(1), WM_ACTIVATE(WA_ACTIVE), WM_SETFOCUS.
2. Mouse move, down and up in client coordinates.
3. Per UTF-16 unit: WM_KEYDOWN(VK_PACKET 0xE7), WM_CHAR, WM_KEYUP.
4. Then WM_KILLFOCUS, WA_INACTIVE, WM_NCACTIVATE(0).

Results:
- Trusted keydown, beforeinput and input; é and 😀 fine. Trusted pointerdown, mousedown and click. Identical in Chrome, Edge and Electron [M].
- It works while covered by another window. Chrome reports visibilityState "hidden", Electron "visible" [M].
- Foreground and cursor unchanged throughout [M].
- The page sees a focus/blur pair around each activation pair [M].
- Without the activation messages, WM_CHAR (posted or sent) does nothing [M].
- **Keys:**
  - Enter in a textarea (VK_RETURN, scan 0x1C, CR WM_CHAR) inserts a newline [M].
  - Tab moves the focus [M].
  - Shift+Tab and Ctrl+A work through AttachThreadInput + SetKeyboardState on Chrome's UI thread. The step was refused if that thread owned the foreground [M].
- **Ctrl+V:** a trusted paste of the real clipboard [M].
  - Saving and restoring a text-only clipboard worked. The `ExcludeClipboardContentFromMonitorProcessing` format kept the temporary data out of Win+V history.
  - Restore rules are needed: refuse on GDI, OLE or delayed formats, and check GetClipboardSequenceNumber before restoring.
- **Minimized:** keys do nothing, and the activation messages make Chromium restore itself and take the foreground for about 5 s [M].
  - `Chrome_RenderWidgetHostHWND` stays a child of the top-level while inactive. It is reparented to a hidden `Chrome_WidgetWin_0` only when minimized [M].

### UIA SetFocus

- **SetFocus takes the foreground for any app** (classic, Chrome, Edge, Electron) within 1-5 ms, even when the caller has no foreground rights [M].
- UIA Invoke and Value.SetValue on Chromium and Electron left the foreground alone [M].
- In winuse only Raise and the Headed set-value call SetFocus.

### Attaching to a running Electron app

1. `process._debugProcess(pid)` from plain Node opened the main-process inspector on 127.0.0.1:9229 within 70 ms.
2. Runtime.evaluate of `process.mainModule.require('electron').webContents.getAllWebContents()[0].debugger.attach('1.3')`.
3. Then Input.insertText, dispatchKeyEvent and dispatchMouseEvent: all trusted, with no window activation and no focus/blur noise [M].
4. `detach()`, then `require('inspector').close()`: port 9229 is gone and the app is still alive [M].

Defender gave no detection and no event [M].

Caveats:
- It needs the nodeCliInspect fuse on. That is the Electron default, but Discord 1.0.9266 has it off (measured on the PC: no node-debug-handler mapping).
- It executes code inside the user's app, so it runs only with the user's consent.
- Any same-user process can reopen the port.

### Self-activation

- Not reproducible on the laptop. ForegroundLockTimeout is 2147483647 there and on the PC.
- LockSetForegroundWindow(LOCK) from a process without foreground rights returns access denied [M].
- The PC's lapse on 2026-11-02 came from the last-input receiver (the League game) exiting [I]. A short-lived zero-move SendInput child does not reproduce it, because the receiver, not the injector, holds the right [M].

### Pen (InjectSyntheticPointerInput): dropped

- The cursor moves whenever the app lets DefWindowProc promote pointer input (classic, WPF, WinUI). A tap activates, and routing is by hit-test.
- A laptop run with a DPI-unaware probe landed in a user window: injection takes physical pixels, while WindowFromPoint and GetCursorPos are virtualized. Any pointer work must be per-monitor DPI aware.

## Not ticketed yet

These need the user's multi-agent decisions: a UIA worker pool (cheap, per the hung-app measurement), agent identity, and per-agent Window States and cursors.
