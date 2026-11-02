# Headless control: winuse vs vercel-labs/agent-desktop (Windows)

Status: research report, read-only. Written 2026-11-02.

## Sources and conventions

- `winuse:` paths are relative to `C:\Users\ADMIN\Desktop\nguyenvu\winuse` at the `main` tip (8e9944a).
- `ad:` paths are relative to the agent-desktop clone at commit 9eb9b29 ("Video remove (#421)", 2026-10-31). `git fetch` on 2026-11-02 reported `origin/main` still at 9eb9b29, so nothing changed upstream after it.
- Line numbers follow `path:line` or `path:start-end`.
- "Inference" marks a conclusion I drew from code or docs but did not see stated or measured. Nothing was run.
- Browser and CDP integration are out of scope (another agent covers them), except where a native technique applies to a browser window.

## Key takeaways

1. **Not 1:1, but close on Element Actions, and each tool wins somewhere.** For click, set value, toggle, select, expand/collapse and ScrollPattern scroll, both tools use UIA patterns only, with no foreground change, pointer or keys. Neither has a headless route for typing, key presses, hover, drag, double/right click or focus on Windows.
2. **agent-desktop is more headless in observation and window management.** Its `screenshot` never activates the Window. It captures through Windows.Graphics.Capture, then PrintWindow (`ad:crates/windows/src/system/capture/screenshot.rs:15-35`, `capture_backend.rs:1-6`). winuse's `get-window-state --screenshot` and `zoom` call `Activate` first (`winuse:internal/helper/screenshot.go:43-56`, `:117-122`), even though WGC is the first capture method. agent-desktop also has `move-window` and `resize-window` (SetWindowPos with SWP_NOACTIVATE), `minimize` and `close-app` (WM_CLOSE). winuse has no window-management Actions at all.
3. **winuse is more headless or safer in Element Actions.** winuse sets `IUIAutomation2::AutoSetFocus` to FALSE (`winuse:internal/uia/thread.go:74-79`). agent-desktop never does: no occurrence in the repo, and its client is a bare `CoCreateInstance(CUIAutomation8)` (`ad:crates/windows/src/tree/automation.rs:190-203`). Microsoft documents the default as "set focus to the element before performing the action". winuse measured that default stealing the foreground on its fixture and on Chrome (ADR 0010 Consequences). By inference, agent-desktop's "headless" Invoke and SetValue can take the foreground on providers that honour that focus call. winuse also refuses a Window that is disabled behind a modal dialog, offers a wider headless click route, and has the passive Esc stop.
4. **The Chrome address bar Enter case is headed in both tools.** agent-desktop's Windows `press` refuses headless delivery to a background app (`ad:crates/windows/src/system/key_dispatch.rs:80-92`). On macOS it does better: `press return` becomes the accessibility action `AXConfirm`, other keys go through `CGEventPostToPid`, and `type` writes `AXSelectedText` (`ad:crates/macos/src/system/key_dispatch.rs:45-65,101-110`, `ad:crates/macos/src/actions/type_text.rs:20-29`). Only partial Windows analogues exist, listed in the last section.
5. **Both tools choose between headless and headed the same way.** The caller chooses with `--headed`, and the tool never escalates on its own. agent-desktop splits that choice into two policy bits (`allow_focus_steal`, `allow_cursor_move`) and fails with `POLICY_DENIED`. It also verifies the outcome once after the action, through a 50 ms poll capped at 400 ms. winuse polls for up to 1 s.
6. **Highest-value changes for winuse, in order:** a headless screenshot and zoom; a headless restore of a minimized Window; headless window move, resize, minimize and close; ScrollPattern scroll by Element with an amount; Toggle and Expand Invoke fallbacks. Headless keys and typing through window messages are possible but unreliable. Each one conflicts with CONTEXT.md's "Headed Action" definition.

## Side-by-side table

"Headless" means no foreground change, no real pointer or keys, and a Window that may stay covered. "Better" names the more headless tool for that action.

| Action | agent-desktop on Windows | winuse | Better |
|---|---|---|---|
| Click an Element (left, single) | Invoke, then LegacyIAccessible.DoDefaultAction (`ad:crates/windows/src/actions/dispatch.rs:192-249`, `chain.rs:109-113`). Headless with no route fails `POLICY_DENIED` (`ad:crates/core/src/actionability/gates.rs:152-166`). Has no physical single-click leg on Windows even with `--headed`; `--headed` only activates the Window first (`ad:crates/core/src/ref_action/ref_action_single.rs:18-28`). | Invoke, then Toggle, SelectionItem.Select, ExpandCollapse, then LegacyIAccessible.DoDefaultAction; with no route, fails "nothing was sent" and suggests `--headed` (`winuse:internal/uia/headless.go:189-251`, `winuse:internal/helper/headless.go:353-361`). `--headed` sends a real SendInput click at the Element point (`winuse:internal/helper/actions.go:145-158`). | winuse: wider pattern route plus AutoSetFocus FALSE |
| Click at a point (`@x,y` / `mouse-click --xy`) | SendInput, headed only (`ad:crates/core/src/commands/mouse_click.rs:25`, `ad:crates/windows/src/input/mouse/mod.rs:1-14`) | SendInput after Activate, always Headed (`winuse:internal/helper/actions.go:110-123`) | Equal (both headed) |
| Double, triple, right click | SendInput; requires `--headed` and a foreground host Window (`ad:crates/windows/src/actions/physical_click.rs:88-109`, `physical_target.rs:47-55`). Right-click is semantic only if the element advertises `RightClick`, and the Windows tree never emits that capability (`ad:crates/windows/src/tree/actions.rs:51-95`). | SendInput after Activate, always Headed (`winuse:internal/helper/headless.go:132-136`) | Equal |
| Set value | ValuePattern.SetValue, then RangeValue, verified by read-back; never reads a password field's value (`ad:crates/windows/src/actions/value_write.rs:1-5,83-111`) | Headless: ValuePattern or RangeValue with no SetFocus, then read-back (`winuse:internal/uia/headless.go:104-114`, `value.go:551-607`). `@x,y` form is Headed (`winuse:internal/helper/setvalue.go:104-118`). | Equal (winuse slightly safer with AutoSetFocus FALSE) |
| Clear | ValuePattern.SetValue("") (`ad:crates/windows/src/actions/value_write.rs:114-131`) | `set-value <ref> ""` (same mechanism) | Equal |
| Type text | SendInput, `--headed` only, and the element must already hold keyboard focus (`ad:crates/windows/src/actions/physical_keyboard.rs:20-36`, `physical_target.rs:37-45,67-75`). Headless gives `POLICY_DENIED` (`ad:skills/agent-desktop-windows/SKILL.md:65-69`). | SendInput Unicode into whatever has focus, after Activate, always Headed (`winuse:internal/helper/keyboard.go:35-54`, `winuse:internal/win/keyboard.go:79-102`) | Equal |
| Press key / chord (incl. Enter) | `press --app` uses SendInput and requires the app to already own the foreground. A headless press to a background app fails closed (`ad:crates/windows/src/system/key_dispatch.rs:15-44,80-92`). Without `--app` the press goes to whatever has focus (`dispatch.rs:149-157`, `physical_keyboard.rs:50-60`). | SendInput chord after Activate, always Headed (`winuse:internal/helper/keyboard.go:60-77`, `winuse:internal/win/keyboard.go:65-77`) | Equal (agent-desktop headless only when the target is already foreground) |
| Scroll | `scroll @ref <dir> <n>`: ScrollPattern SmallIncrement or SmallDecrement, n times, verified by scroll percent or bounds (`ad:crates/windows/src/actions/scroll.rs:1-5,17-29`). `mouse-wheel` is SendInput and headed only (`ad:crates/core/src/commands/mouse_wheel.rs:20`). | `perform-secondary-action Scroll Up/Down/Left/Right`: ScrollPattern LargeIncrement (one page), Headless, no read-back (`winuse:internal/uia/value.go:77-104,711-723`). `scroll @x,y` is a SendInput wheel, Headed (`winuse:internal/helper/pointer.go:63`, `winuse:internal/win/input.go:253`). | agent-desktop: Element-addressed with an amount and verification |
| Scroll into view | `scroll-to`: ScrollItemPattern.ScrollIntoView, then an ancestor ScrollPattern ladder, headless (`ad:crates/windows/src/actions/scroll_into_view.rs:1-8`, `scroll_ladder.rs:1-8`) | Only inside a Headed element click (`winuse:internal/uia/action.go:85-90,134-150`). No standalone form. | agent-desktop |
| Drag | SendInput, headed only (`ad:crates/core/src/commands/drag.rs:51`) | SendInput after Activate, Headed (`winuse:internal/helper/pointer.go:91`) | Equal |
| Hover | SendInput move, headed only (`ad:crates/core/src/commands/hover.rs:38`) | SendInput move after Activate, Headed (`winuse:internal/helper/pointer.go:124`) | Equal |
| Toggle / check / uncheck | TogglePattern, falling back to Invoke; `check` and `uncheck` are idempotent, and the core turns `toggle` into check or uncheck from the pre-read (`ad:crates/windows/src/actions/toggle_state.rs:1,138-164`, `ad:crates/core/src/actions/post_action.rs:21-29`) | TogglePattern, verified (`winuse:internal/uia/value.go:134-143`, `headless.go:127-138`). No Invoke fallback and no idempotent check/uncheck. | agent-desktop (more routes, same headlessness) |
| Expand / collapse | ExpandCollapsePattern, then Invoke when the state is known to be the opposite (`ad:crates/windows/src/actions/disclosure.rs:1-5`) | ExpandCollapsePattern, verified (`winuse:internal/uia/value.go:105-133`) | agent-desktop (Invoke fallback) |
| Select | SelectionItem.Select on the element, or a bounded search of descendants by name that expands a collapsed combo first (`ad:crates/windows/src/actions/select.rs:1-9`) | SelectionItem.Select on the Element, verified (`winuse:internal/uia/value.go:144-153`) | agent-desktop (by-name in containers) |
| Focus an Element | UIA SetFocus, `--headed` only, because it moves the foreground (A3-4, A19-5) (`ad:crates/windows/src/actions/focus.rs:1-6,34-36,98-107`) | No focus command; Raise (UIA SetFocus) is always Headed (`winuse:internal/uia/value.go:64-76`, `headless.go:116-129`) | Equal |
| Activate a Window | `focus-window`: plain SetForegroundWindow, no policy gate (`ad:crates/windows/src/system/window/window_activate.rs:248-271`) | `activate-window`: SendInput no-op move, AttachThreadInput, SetForegroundWindow, BringWindowToTop, then verify (`winuse:internal/win/activate.go:84-149`) | Neither (foreground by definition) |
| Move / resize Window | SetWindowPos with SWP_ASYNCWINDOWPOS, SWP_NOZORDER and SWP_NOACTIVATE, read back (`ad:crates/windows/src/system/window/window_op.rs:206-229`) | No command | agent-desktop |
| Minimize / maximize / restore | ShowWindowAsync with SW_MINIMIZE, SW_MAXIMIZE or SW_RESTORE, no policy gate (`ad:crates/windows/src/system/window/window_op.rs:171-181`). Per Microsoft docs, SW_MAXIMIZE and SW_RESTORE activate the window, and SW_MINIMIZE activates the next one, so these can move the foreground (inference from the docs). | No command. Headless Actions refuse a minimized Window (`winuse:internal/win/activate.go:64-82`, `helper/headless.go:337-343`); `activate-window` restores with SW_RESTORE and takes the foreground (`activate.go:114-116`). | agent-desktop for minimize; neither is headless for restore |
| Close Window / app | `close-app`: PostMessageW(WM_CLOSE) to the owned window, `--force` for TerminateProcess (`ad:crates/windows/src/system/close.rs:313-331`) | No command | agent-desktop |
| Screenshot of a background or covered Window | WGC CreateForWindow, then PrintWindow(PW_RENDERFULLCONTENT); no activation; minimized is refused before the blit (`ad:crates/windows/src/system/capture/capture_modern.rs:1-5,169`, `capture_window.rs:1-9,101`) | Activates the Window first, then WGC, screen region, PrintWindow (`winuse:internal/helper/screenshot.go:43-57`, `state.go:309-320`). `zoom` also activates (`screenshot.go:117-122`). | agent-desktop |
| Read the tree (no screenshot) | UIA only, no activation | UIA only, no activation | Equal |

## 1. How agent-desktop performs each action on Windows

### The policy model

- `InteractionPolicy` has two bits, `allow_focus_steal` and `allow_cursor_move`. `headless()` clears both, `focus_fallback()` sets only focus, and `headed()` sets both. Headless is the default (`ad:crates/core/src/actions/interaction_policy.rs:3-47`).
- Each Action declares a headed requirement. Click, the multi-clicks, Scroll, Hover and Drag need `FocusedWindowAndCursor`. The others need `FocusedWindow` (`ad:crates/core/src/actions/action.rs:31-55`). The requirement applies only when the caller asked for headed (`ad:crates/core/src/actions/action_request.rs:19-25`).
- In headed mode, before dispatch, the core activates the ref's exact source Window, for semantic actions too (`ad:crates/core/src/ref_action/ref_action_single.rs:18-28`, `ad:crates/core/src/actions/headed_focus.rs:42-64`).
- When the actionability preflight finds a semantic pointer route, the core forces the request's policy back to headless before dispatch (`ad:crates/core/src/ref_action/mod.rs:123-128`).

### Element actions

- The Windows dispatcher maps every Action to a module (`ad:crates/windows/src/actions/dispatch.rs:68-146`). Each pattern rung runs through one chain engine. A rung can be marked `requires_headed`, and a disallowed rung is skipped (`ad:crates/windows/src/actions/chain.rs:122-165`). On Windows no rung sets `requires_headed: true`; a search of the crates finds none. So every pattern chain is the same in both modes.
- **Click:** the chain is Invoke, then the Legacy default action (`ad:crates/windows/src/actions/dispatch.rs:222-249`). The tree advertises `Click` only for InvokePattern or a non-empty LegacyIAccessible DefaultAction (`ad:crates/windows/src/tree/actions.rs:58-60,87-89`). Their probe measured DoDefaultAction "ok, ... non-foreground" (finding A19-6, `ad:probes/windows/FINDINGS.md:315`) and Invoke "ok (non-foreground)" on WPF (A19-1, `FINDINGS.md:310`).
- **SetValue / Clear:** a ValuePattern write, then a RangeValue write (`ad:crates/windows/src/actions/value_write.rs:83-131`).
- **Toggle / Check / Uncheck:** TogglePattern, then Invoke (`ad:crates/windows/src/actions/toggle_state.rs:138-164`).
- **Expand / Collapse:** ExpandCollapsePattern, then Invoke only when the state is known to be the opposite (`ad:crates/windows/src/actions/disclosure.rs:1-5`).
- **Select:** SelectionItem on the target, or a bounded descendant search that expands collapsed containers first (`ad:crates/windows/src/actions/select.rs:1-9`).
- **Scroll / ScrollTo:** ScrollPattern SmallIncrement or SmallDecrement on one axis, n times, verified by percent or bounds delta (`ad:crates/windows/src/actions/scroll.rs:1-5,17-29`). ScrollItem.ScrollIntoView, then an ancestor ladder (`scroll_into_view.rs:1-8`).
- **SetFocus:** UIA SetFocus, refused headless with `"evidence": ["A3-4", "A19-5"]` (`ad:crates/windows/src/actions/focus.rs:98-107`). Finding A19-5 measured that SetFocus on a background window moved the desktop foreground to it (`FINDINGS.md:314`).

### Physical input

- Keyboard: SendInput is the only seam (`ad:crates/windows/src/input/keyboard/keyboard_send.rs:1-4`). Typing and element-targeted key presses require `allow_focus_steal` (`ad:crates/windows/src/actions/physical_target.rs:37-45`) and that the element already has keyboard focus. Otherwise they fail with "Target element lost focus before physical input delivery", marked not delivered (`physical_keyboard.rs:67-90`, `physical_target.rs:126-132`).
- Mouse: SendInput. All bare-coordinate commands are headed-only (`ad:crates/windows/src/input/mouse/mod.rs:1-14`, `ad:crates/core/src/commands/point_resolve.rs:21-34`).
- Ref-addressed physical clicks also check that the host Window is the foreground root before they inject, and fail otherwise (`ad:crates/windows/src/actions/physical_target.rs:57-65,134-148`).
- No PostMessage or SendMessage carries input anywhere in the Windows crate. A search for `PostMessage`, `SendMessage`, `WM_CHAR`, `WM_KEYDOWN`, `WM_SETTEXT` and `BM_CLICK` finds only WM_CLOSE in `close.rs`, SendMessageTimeout(WM_NULL) hang probes and test fixtures.

### Window and capture

- Window ops use asynchronous writes so a hung app cannot block them, then read the placement back (`ad:crates/windows/src/system/window/window_op.rs:17-25`).
- Screenshot orchestration has no activation step (`ad:crates/windows/src/system/capture/screenshot.rs:15-35`). WGC is used when `IsSupported` says so, with a fallback to PrintWindow (`capture_backend.rs:1-6`).

## 2. Where agent-desktop is more headless than winuse

### Screenshots of covered Windows

- agent-desktop captures with WGC `CreateForWindow`, then `PrintWindow(PW_RENDERFULLCONTENT)`, and never activates the Window (see the table).
- winuse's own chain is the same at its core (WGC first, `winuse:internal/helper/state.go:309-320`), but `windowStateScreenshot` and `zoom` call `s.Activate` first. The stated reason is that "the screen-region capture needs the foreground" (`winuse:internal/helper/screenshot.go:43-56`). That is true only of the second method, the screen-region fallback.
- The research notes say activation matters for the region fallback, for minimized Windows and for popups outside the bounds (`winuse:.notes/wgc.md:186-209`).
- Codex's documentation is quoted as saying WGC screenshots "work even when windows are occluded" (`winuse:.notes/codex-surface.md:267`).
- Caveats from agent-desktop: a minimized Window is refused, not captured (`ad:crates/windows/src/system/capture/capture_window.rs:101`). WGC needs `IsSupported`, not a build number (A22-1). A Chromium window that stays fully covered during settle can keep a thin tree (`ad:skills/agent-desktop-windows/references/chromium-and-electron.md:40-45`).

### Window management without activation

- `move-window` and `resize-window` call SetWindowPos with `SWP_NOACTIVATE | SWP_NOZORDER | SWP_ASYNCWINDOWPOS` (`ad:crates/windows/src/system/window/window_op.rs:206-229`). Per the ShowWindow and SetWindowPos semantics, this does not touch the foreground.
- `close-app` posts WM_CLOSE and treats the post as a request, not proof that the window closed (`ad:crates/windows/src/system/close.rs:313-331`). Finding A21-3 measured the post and the wait (`FINDINGS.md:340`).
- Caveat: `minimize`, `maximize` and `restore` use SW_MINIMIZE, SW_MAXIMIZE and SW_RESTORE (`window_op.rs:171-181`). Microsoft's ShowWindow table says SW_MAXIMIZE "Activates the window", SW_RESTORE "Activates and displays the window" and SW_MINIMIZE "activates the next top-level window in the Z order" (https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-showwindow). So agent-desktop's `restore`, which its skill recommends for minimized windows (`ad:skills/agent-desktop-windows/SKILL.md:53-57`), probably takes the foreground. That is an inference: no finding measures it. The same table offers SW_SHOWNOACTIVATE and SW_SHOWMINNOACTIVE, which do not activate.

### Scroll by Element with an amount and a read-back

- agent-desktop's `scroll` is a ref action: ScrollPattern SmallIncrement n times. Each axis is checked for scrollability, and the scroll is verified by percent or bounds. A partial scroll reports how much landed (`ad:crates/windows/src/actions/scroll.rs:1-5,242`).
- winuse has only a page-sized secondary action with no read-back (`winuse:internal/uia/value.go:711-723`). Its fine-grained scroll is a Headed wheel.

### More pattern fallbacks for stateful actions

- agent-desktop falls back to Invoke for Toggle and for Expand/Collapse, and offers idempotent `check` and `uncheck` (see the table). These are not more headless, but they keep more cases headless that winuse would send to `--headed`.

### What agent-desktop does not have on Windows

The prompt named these as candidate techniques. None is in agent-desktop's Windows code:

- Typing through ValuePattern append, TextPattern or EM_REPLACESEL.
- Keys through PostMessage to a background window.
- Any SendMessage-based control message such as BM_CLICK or WM_SETTEXT.

UIA's Text pattern is read and select only. Microsoft says IValueProvider "complements ITextProvider by providing a programmatic way to change the text" (https://learn.microsoft.com/en-us/windows/win32/winauto/uiauto-implementingtextandtextrange). The headless techniques agent-desktop does have for text and keys live in the macOS adapter:

- `type` writes `AXSelectedText`, which inserts at the caret without focus (`ad:crates/macos/src/actions/type_text.rs:20-29,50-55`).
- `press return` and `press escape` map to `AXConfirm` and `AXCancel` on the focused element (`ad:crates/macos/src/system/key_dispatch.rs:45-49,101-110`).
- Other keys go to the process through `CGEventPostToPid`, which needs no foreground (`key_dispatch.rs:52-65`, `ad:crates/macos/src/input/keyboard/keyboard_event.rs:134-140`).

The last section lists what carries over to Windows.

## 3. Where winuse is more headless or safer

- **AutoSetFocus FALSE.** Microsoft: "By default, most UI Automation methods that perform an action on an element, such as IUIAutomationInvokePattern::Invoke and IUIAutomationValuePattern::SetValue, set focus to the element before performing the action ... you can set AutoSetFocus to FALSE" (https://learn.microsoft.com/en-us/windows/win32/api/uiautomationclient/nf-uiautomationclient-iuiautomation2-get_autosetfocus).
  - winuse turns it off (`winuse:internal/uia/thread.go:74-79`). It measured the bug on its fixture and on real Chrome: SetFocus arrived 2 to 3 ms before each pattern call (`winuse:.scratch/headless/issues/05-opt-in-e2e.md`, comment of 2026-11-02; `winuse:docs/adr/0010-headless-element-actions.md:50-54`).
  - agent-desktop creates `CUIAutomation8` and sets only the connection and transaction timeouts (`ad:crates/windows/src/tree/automation.rs:190-203`). Inference: its headless pattern calls run with AutoSetFocus TRUE and can move the foreground wherever the provider honours SetFocus. Its own A19-1 probe saw no foreground change on WPF (`FINDINGS.md:310`), and A19-5 shows that SetFocus does move the foreground (`FINDINGS.md:314`). This is the most important point to report upstream.
- **Wider headless click route.** winuse adds Toggle, SelectionItem and ExpandCollapse between Invoke and the Legacy default action (`winuse:internal/uia/headless.go:201-237`). agent-desktop's click fails `POLICY_DENIED` headless on a checkbox or tab item that lacks Invoke and a legacy default action (`ad:crates/core/src/actionability/gates.rs:152-166`). Its agent must call `toggle` or `select` instead.
- **Modal dialog refusal.** winuse refuses a Headless Action on a Window that a modal dialog has disabled, and names the dialog. It finds dialogs that were never activated through GW_ENABLEDPOPUP (`winuse:internal/win/activate.go:64-82,160-174`). agent-desktop has no IsWindowEnabled or owned-popup check in the Windows crate, so a pattern call can act behind an open modal (inference from a search of the code).
- **Esc stop.** winuse keeps a physical Esc stop in Headless scopes without swallowing the key (`winuse:internal/helper/headless.go:219-225`, CONTEXT.md "Esc stop"). agent-desktop has no keyboard hook: there is no `SetWindowsHookEx` in the crates.
- **Headed clicks that really click.** `winuse --headed click <ref>` sends a SendInput click at the Element's point (`winuse:internal/helper/actions.go:145-158`). On agent-desktop, `click @ref --headed` still uses only Invoke and Legacy, so a custom-drawn control needs `mouse-click --xy` (`ad:crates/windows/src/actions/dispatch.rs:192-213`). This is not more headless, but it makes winuse's escalation path one flag instead of a change of command.
- **Activation reliability.** When winuse must take the foreground, it uses the input-event and AttachThreadInput workarounds and verifies the result (`winuse:internal/win/activate.go:121-148`). agent-desktop calls plain SetForegroundWindow (`ad:crates/windows/src/system/window/window_activate.rs:270`). Its own A21-6 measured that call landing on 0 of 5 first attempts and 0 of 5 second attempts (`FINDINGS.md:343`). Again not more headless, but fewer failed headed attempts.

## 4. How agent-desktop chooses between headless and real input

- **The caller chooses, with no automatic escalation.** A headless request that would need focus or the cursor fails at preflight with `POLICY_DENIED` (`ad:crates/core/src/actionability/gates.rs:152-195`, `point_resolve.rs:21-34`). The error text names the missing bit and suggests `--headed` (`ad:crates/core/src/response/adapter_error.rs:276-280`). This matches winuse's rule: "winuse never switches mode on its own" (ADR 0010 Decision 3).
- **Headed downgrades to semantic when it can.** If a semantic pointer route exists, dispatch runs headless even under `--headed` (`ad:crates/core/src/ref_action/mod.rs:123-128`). It still activates the Window first (`ref_action_single.rs:18-28`).
- **One delivery, then a read-back.** Every stateful action runs once, then polls every 50 ms up to 400 ms (`ad:skills/agent-desktop-windows/references/troubleshooting.md:132-155`).
  - The outcome is `delivered_verified`, `ACTION_FAILED` with `post_action_verification` and `retry: unsafe`, or `delivered_unverified`.
  - A pattern error raised after an earlier rung already delivered is reclassified as delivered (`ad:crates/windows/src/actions/chain.rs:1-15,139-145`).
  - winuse has the same three outcomes: `ok`, "the Action was delivered; do not repeat it" and "may have taken effect". It polls up to 1 s (`winuse:internal/uia/headless.go:84-89,331-344`, `helper/headless.go:353-376`).
- **Focus stealing.** agent-desktop's answer is policy: SetFocus, keys and physical clicks are headed (A3-4, A19-5). It does not address AutoSetFocus (see section 3). Its cursor overlay was measured not to take the foreground, using WS_EX_NOACTIVATE and SW_SHOWNOACTIVATE (A29-1, `FINDINGS.md:567`). That is the same style set winuse's Agent Cursor uses (`winuse:internal/win/cursor_view.go:232`).
- **Dialogs opened headless.** agent-desktop reports surfaces (menus, sheets) after any action that may raise one (`ad:crates/core/src/ref_action/mod.rs:155-181`). winuse instead refuses the next Headless Action on the disabled owner and points at the dialog. Both work; agent-desktop's tells the agent earlier.

## 5. Ranked changes that would make winuse more headless

Effort: S is under a day, M is a few days, L is a week or more. Every change adds an Action, or a Headless form of one. So each one must also update CONTEXT.md "Headless Action" and "Headed Action" (`winuse:CONTEXT.md:53-59`), which today lists typing, keys, scrolling, dragging, hovering, `@x,y`, double/right/middle clicks, activation and Raise as "always Headed".

1. **Headless screenshot and zoom.** Effort S. Risk low.
   - Do not call `Activate` before `Capture`. Run WGC, then PrintWindow, without the foreground. Use the screen-region method (and activation) only when the Window has popups outside its bounds, or under `--headed`.
   - Keep refusing a minimized Window with the existing hint.
   - Fit: observation is not an Action, so CONTEXT.md does not change. But it reverses the v1 spec rule that `screenshot.go:43-46` cites ("after it is in the foreground"), so record it in an amendment to ADR 0010 or a new ADR.
   - Validate on a covered Chrome window. agent-desktop warns that covered Chromium may not repaint or settle (`chromium-and-electron.md:40-45`).
   - Biggest gap, smallest change: every `--screenshot` today takes the user's foreground.
2. **Headless restore of a minimized Window.** Effort S. Risk medium.
   - Restore with `ShowWindow(SW_SHOWNOACTIVATE)`, which per Microsoft "Displays a window in its most recent size and position ... the window is not activated". Use it for a new `restore-window` command, or as the `is minimized` recovery.
   - Some apps activate themselves when restored. That is an inference to measure on the fixture and on Chrome.
   - Fit: today's spec refuses minimized Windows with a hint (`winuse:.scratch/headless/spec.md:41`). An explicit command keeps "winuse never switches mode on its own". Silent auto-restore would need an ADR 0010 amendment.
3. **Headless window move, resize, minimize and close.** Effort M. Risk low.
   - Move and resize: `SetWindowPos(SWP_NOACTIVATE | SWP_NOZORDER | SWP_ASYNCWINDOWPOS)` with a placement read-back (agent-desktop's 8 px tolerance, A21-5).
   - Minimize: SW_SHOWMINNOACTIVE, not SW_MINIMIZE.
   - Close: PostMessage(WM_CLOSE), reporting delivered but unverified.
   - Fit: these are new Actions aimed at a Window, consistent with CONTEXT.md "Action" once the list there grows. No ADR conflict. Refuse administrator Windows, because PostMessage is UIPI-gated: "a thread ... can post messages only to message queues of threads in processes of lesser or equal integrity level" (https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-postmessagew).
4. **Element-addressed scroll with an amount, plus scroll-into-view.** Effort S to M. Risk low.
   - `scroll <window> <ref> <direction> [n]` through ScrollPattern SmallIncrement, verified by percent or bounds as in agent-desktop.
   - A `Scroll Into View` Secondary Action through ScrollItem.
   - Fit: CONTEXT.md says "scrolling ... always Headed". Narrow that to "scrolling at a point" and record the change in an ADR 0010 amendment.
5. **Invoke fallbacks for Toggle and Expand/Collapse, and idempotent check and uncheck.** Effort S. Risk low.
   - Copy agent-desktop's rule: fire Invoke only when the pre-read state is known and opposite, then verify.
   - Fit: no definition changes. It only keeps more cases Headless.
6. **Headless Enter in dialogs, the AXConfirm analogue.** Effort M. Risk medium.
   - Map `press-key Return` to Invoke on the dialog's default button. For classic dialogs, that is `DM_GETDEFID` on the dialog, then `GetDlgItem`, then the UIA element and Invoke. Map `Escape` to the Cancel button (IDCANCEL).
   - Fail with "nothing was sent" and the `--headed` hint when no default button exists.
   - This covers message boxes and file dialogs, not Chrome's address bar: Chrome's omnibox has no default button, and there is no UIA confirm action. That is an inference.
   - Conflicts: CONTEXT.md "keys ... always Headed" and spec Out of Scope. Needs an ADR.
7. **Headless text insertion for classic Edit and RichEdit controls.** Effort M. Risk medium.
   - `SendMessage(EM_REPLACESEL)` to the control's HWND. "If there is no selection, the replacement text is inserted at the caret" (https://learn.microsoft.com/en-us/windows/win32/controls/em-replacesel).
   - It works only on real Win32 edit HWNDs. UIA elements in WPF, WinUI and Chromium have no such HWND.
   - The lParam is a pointer, so it must be SendMessage, not PostMessage: Microsoft's PostMessage doc says asynchronous system messages "cannot include pointers". It is UIPI-gated.
   - A generic fallback, ValuePattern read-modify-write as an append, loses caret position and IME composition. Keep `set-value` for that case.
   - Conflicts: CONTEXT.md typing "always Headed", and spec Out of Scope "Headless typing (UIA has no insert-at-caret write ...)" (`winuse:.scratch/headless/spec.md:57`).
8. **Background keys through PostMessage.** Effort M. Risk high. Opt-in flag only.
   - Post WM_KEYDOWN, WM_CHAR and WM_KEYUP to the focus HWND of the target thread (GetGUIThreadInfo `hwndFocus`).
   - Microsoft's own guidance says this is unreliable. Raymond Chen: posted messages don't "update the keyboard shift states", so code calling GetKeyState "is going to see the 'real' shift state" (https://devblogs.microsoft.com/oldnewthing/20050530-11/?p=35513). Chords such as Ctrl+A, IME input and dead keys break.
   - For Chrome's address bar Enter, the omnibox is a views control inside the top-level `Chrome_WidgetWin_1`. Whether Chrome routes a posted VK_RETURN to it while the window is inactive is unknown, and must be measured before anyone relies on it. That is an inference.
   - The CDP path the other agent is researching is the likelier headless route for browsers.
   - Conflicts: CONTEXT.md "keys ... always Headed", and ADR 0010's split of Headless as "pattern calls only".
9. **Report the AutoSetFocus finding upstream.** Effort S. No risk to winuse. agent-desktop's Windows headless claim depends on it (section 3).

Not recommended: making `@x,y` actions Headless. A point names the topmost Window there, as ADR 0010 Decision 1 and the spec (`winuse:.scratch/headless/spec.md:38`) state. Posting WM_LBUTTONDOWN to a covered window's child at client coordinates bypasses that, but it fails on DirectComposition and Chromium content, which hit-test inside the process. That is an inference, consistent with agent-desktop shipping no such route.

## Answer to "does winuse match agent-desktop 1:1?"

No, but the gap is narrow and runs both ways.

- **Element Actions:** winuse matches and in places beats agent-desktop. It turns AutoSetFocus off, has a wider click route, refuses a Window behind a modal dialog, and keeps the passive Esc stop.
- **Where agent-desktop is ahead:** screenshots of covered Windows without activation, window move, resize, minimize and close without activation, element-addressed scroll with an amount and scroll-to, and Invoke fallbacks for toggle and disclosure.
- **Physical input:** typing, keys (including Chrome's address bar Enter), hover, drag, multi-clicks and focus are headed in both tools on Windows.
