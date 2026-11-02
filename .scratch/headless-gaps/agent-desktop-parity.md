# winuse vs vercel-labs/agent-desktop: command and feature parity

Status: research report, read-only. Written 2026-11-02. Extends
`.scratch/headless-parity/agent-desktop-comparison.md` (headless mechanics) and
`.scratch/browser-merge/agent-desktop-research.md` (browsers). It does not repeat
their per-mechanism analysis; it covers the whole command surface and the
cross-cutting features.

## Sources and conventions

- `ad:` paths are relative to an agent-desktop shallow clone of `main` at
  **9eb9b29** ("Video remove (#421)", 2026-11-01), made with `gh repo clone` on
  2026-11-02. `ad:docs/...` abbreviates `ad:apps/docs/content/docs/...`.
- `winuse:` paths are relative to `C:\Users\ADMIN\Desktop\nguyenvu\winuse`, read
  from the **uncommitted working tree of branch `headless-parity`** on 2026-11-02.
  Line numbers can move when that work is committed.
- "Inference" marks a conclusion drawn from code or docs and not stated or
  measured. Nothing was run.

### Upstream state (checked 2026-11-02 with `gh api`)

- Latest release **v1.0.2** (2026-10-30, commit a47ed66). No release since.
  Branch `chore/release-1.1.0` holds only "prepare version 1.1.0" (832bfd1) and is
  30 commits behind `main`, so 1.1.0 is unreleased.
- `main` has 16 commits after v1.0.2, all on `main` at 9eb9b29. The user-visible
  ones: `type --interval-ms` (#348); an insertion caret drawn by the overlay after
  `type`/`set-value`/`press` (#349, #350, #353); the cursor moves before taking the
  interaction lease (#347); image cursors on Windows (#345); CR LF typed as one
  line break on Windows (#368); overlay DPI, black-square and exit-crash fixes
  (#342-#344); label pill and accent colour (#357, #361). The docs now say the
  overlay draws a caret on Windows (`ad:docs/commands/system.mdx:118`).
- Open PRs that would change parity if merged: #400 headless typing into
  background Chromium/Electron via window messages; #436 actions on different
  apps run concurrently; #438 press the element at a point in a background app
  (macOS); #439 batch JSON from stdin; #441/#442 new ref re-identification;
  #466 `TREE_TOO_LARGE`; #467 `--rest-ms` for the cursor; #477 `element-at`
  (ref under a screen point); #483 hosted (UWP) apps in `list-apps`/`close-app`.
  #445 and #447 merged into the feature branch `laf/identity-linux-windows`,
  not into `main`.

## Key takeaways

1. agent-desktop has **60 commands** (`ad:src/cli/mod.rs:33-169`,
   `ad:docs/commands/index.mdx:7`). winuse has **33 top-level commands**, 43
   counting the `browser` and `tab` subcommands
   (`winuse:internal/cli/cli.go:252-305`). Of agent-desktop's 60, winuse matches
   or beats **21**, partly covers **17**, and lacks **22**. Four of the 22 are
   agent-desktop's reserved fail-closed names, so 18 are real gaps.
2. The **headless gap is closed**. Every item in the earlier comparison's ranked
   list shipped (headless-parity issues 01-09 are `done`). On Windows winuse is now
   more headless than agent-desktop in click, restore, minimize, dialog
   Return/Escape, classic-edit typing, Posted Keys and all browser Page work.
3. The **real gaps are breadth, not depth**: no clipboard, no desktop `wait`,
   no `find`/drill-down for dense trees, no post-action "a new window opened"
   report, no display/full-screen capture, no shell surfaces (taskbar, tray,
   Start, Action Center), no notifications, no sessions/traces, no structured
   (JSON) output with stable error codes, no blocked dangerous key combos, no
   click modifiers.
4. **Design conflicts** sit in three places. A default JSON envelope breaks
   Codex's text tree and error style (ADR 0004; codex-parity "tree format" and
   "error style" rows). Snapshot-qualified persistent refs replace Codex's
   Element Index. Clipboard, displays, notifications and shell surfaces are not
   aimed at a Window, which CONTEXT.md's Action model requires. The
   recommendation: add them as opt-in extensions (`--json`, `find`, new
   non-Window commands) and keep the Codex default.
5. **Proposed: 22 tickets** (section 4), 7 of them P1.

## 1. Every agent-desktop command

Columns: agent-desktop's Windows implementation (H = headless, Hd = needs
`--headed`, R = reserved, fails closed), the winuse equivalent, and the gap.
"=" means equivalent; "winuse+" means winuse does more; "gap" means agent-desktop
does something winuse cannot.

### App and window (10)

| # | Command | What it does | Windows implementation (ad) | winuse equivalent | Gap |
|---|---|---|---|---|---|
| 1 | `launch` | Start an app or attach to a running one; `--arg`, `--env`, `--cwd`, `--no-attach`, `--activate`, `--cdp [PORT]`, `--timeout` (`ad:docs/commands/app-window.mdx:17-89`) | H (`--activate` takes the foreground). `CreateProcessW` of an absolute path or a bare name under System32 or the Windows directory; no display-name lookup (`ad:crates/windows/src/system/launch.rs:22-30`, `launch_path.rs:53-84,180-208`; `ad:docs/platforms/windows.mdx:48-49`). `--cdp` opens and verifies a DevTools port (`ad:crates/core/src/commands/launch.rs:27-51,185-223`) | `launch-app <name\|exe path>`: catalog name, Start-menu shortcut, AUMID or path (`winuse:internal/cli/cli.go:400-419`; `docs/codex-parity.md:65-75`). Args shape is only `{app}` (`winuse:internal/protocol/protocol.go:118-120`). CDP launch is `browser launch` | gap: no `--arg`/`--env`/`--cwd`/`--no-attach`. winuse+: name resolution, recent apps |
| 2 | `close-app` | Quit an app and all its windows, succeed only after the process exits; `--force` terminates; protected processes refused | H. `PostMessageW(WM_CLOSE)` to owned windows, exit verified by pid plus instance; `--force` is `TerminateProcess` (`ad:crates/windows/src/system/close.rs:191-229,313-331`). Protected list: csrss, wininit, winlogon, services, lsass, smss, lsaiso, dwm, explorer (`ad:crates/windows/src/system/app_ops.rs:7-17`; `ad:crates/core/src/commands/close_app.rs:10-40`) | `close-window <window>`: one Window, posted WM_CLOSE, waits 2 s, "winuse never ends a process" (`winuse:internal/helper/windowactions.go:200-230`; `skills/winuse/SKILL.md:324-327`) | gap: no app-wide close, no exit verification, no force |
| 3 | `list-apps` | Running GUI apps; `--app` filter; `presentation`; `complete:false` on partial reads | H. Process inventory (`ad:crates/windows/src/system/app_ops.rs`) | `list-apps`: running Apps with their Windows, then recently used (UserAssist) (`winuse:docs/codex-parity.md:55-63`) | winuse+ (recent apps). Minor gap: no filter |
| 4 | `list-windows` | Windows with id, title, bounds, `is_focused`, `minimized`, pid, `accessible`; `--app` filter | H. EnumWindows inventory (`ad:crates/windows/src/system/window/window_enum.rs`) | `list-windows`: `<Window ID> <App> <title>` only (`winuse:internal/cli/cli.go:308-320`) | gap: no bounds, focus, minimized or pid columns; no filter. winuse+: owned dialogs listed (`winuse:docs/codex-parity.md:253-258`) |
| 5 | `focus-window` | Bring a window to the front; verify on two reads; `--window-id`, `--app`, `--title` (partial) | Takes the foreground without `--headed`. Plain `SetForegroundWindow` (`ad:crates/windows/src/system/window/window_activate.rs:248-271`) | `activate-window <window>`: input-event and AttachThreadInput workarounds, verify (`winuse:internal/win/activate.go:94-160`) | winuse+ (reliability). Minor gap: title match |
| 6 | `resize-window` | Set width and height; read back within a tolerance | H. `SetWindowPos(SWP_NOACTIVATE\|SWP_NOZORDER\|SWP_ASYNCWINDOWPOS)` (`ad:crates/windows/src/system/window/window_op.rs:204-229`) | `resize-window <window> <w> <h>` (`winuse:internal/helper/windowactions.go:166`) | = |
| 7 | `move-window` | Move to screen coordinates | H. Same call (`window_op.rs:204-229`) | `move-window <window> <x> <y>` (`windowactions.go:127`) | = |
| 8 | `minimize` | Minimize, read back | H by policy, but `ShowWindowAsync(SW_MINIMIZE)` (`window_op.rs:171-181`) activates the next window per Microsoft's ShowWindow table (inference, from the earlier comparison) | `minimize-window` with `SW_SHOWMINNOACTIVE` (`winuse:internal/helper/state.go:142`) | winuse+ |
| 9 | `maximize` | Maximize, read back | `ShowWindowAsync(SW_MAXIMIZE)`, which activates (`window_op.rs:171-181`) | missing. ADR 0011: Windows has no non-activating maximize (`winuse:docs/adr/0011-headless-wherever-windows-allows.md` Consequences) | gap |
| 10 | `restore` | Restore minimized or maximized; reported `delivered_unverified` | `ShowWindowAsync(SW_RESTORE)`, which activates (inference) | `restore-window` with `SW_SHOWNOACTIVATE`, verified (`winuse:internal/helper/state.go:141`; `windowactions.go:52-96`) | winuse+ (cannot restore to maximized) |

### Observation (8)

| # | Command | What it does | Windows implementation (ad) | winuse equivalent | Gap |
|---|---|---|---|---|---|
| 11 | `snapshot` | JSON tree with qualified refs; `-i`, `--compact`, `--skeleton`, `--root REF`, `--max-depth` (10, at most 50), `--include-bounds`, `--timeout-ms` (3000; a partial tree returns `complete:false`), `--surface`, `--force-electron-a11y` (`ad:docs/commands/observation.mdx:11-44`) | H. UIA walk with the Chromium wrapper-depth skip and settle loop (`ad:crates/windows/src/tree/walk/wrapper.rs:32-52`; `tree/chromium.rs:15-93`) | `get-window-state <window> [--screenshot]`: Codex text grammar (ADR 0004), caps of 3000 nodes, depth 48 and 1000 children plus an 8 s budget, with a partial-tree note (`winuse:internal/uia/walk.go:101-103`; `docs/adr/0004-window-state-tree-format.md`). Pages through CDP (ADR 0012) | gap: no `--root` drill, depth control, interactive-only filter, bounds or timeout flag. winuse+: one-call tree plus screenshot, CDP Pages |
| 12 | `find` | Search by role, name, value, text, description, native id and state tokens; fuzzy or `--exact`; `--count/--first/--last/--nth/--limit`; `--root`, `--surface`; saves a snapshot (`ad:docs/commands/observation.mdx:46-106`) | H. Core locator over a live walk (`ad:crates/core/src/commands/find.rs`, `find_live.rs`) | Only `name:`/`id:` exact references, resolved against the cached latest Window State (`winuse:internal/helper/refs.go:90-117`; `internal/wstate/refs.go:116`) | gap |
| 13 | `get` | One live property: text, value, title, bounds (with `live` flag), role, states | H. Shared live read (`ad:crates/windows/src/tree/live_read.rs:1-6`) | none; the tree line carries role, name, Value and states | gap (low value) |
| 14 | `is` | One boolean state with `applicable` | H. Same live read | none | gap (low value) |
| 15 | `screenshot` | PNG of a window or a display (`--screen N`); to PATH or base64 | H. WGC then PrintWindow, never activates; display via BitBlt of the primary or indexed monitor (`ad:crates/windows/src/system/capture/screenshot.rs:15-73`; `capture_display.rs:1-5`) | `get-window-state --screenshot` and `zoom`: WGC then PrintWindow, headless; JPEG q80, long edge 1568 px, path printed (`winuse:internal/helper/overlay.go:157-164`; `internal/helper/state.go:482`; ADR 0011) | gap: no display capture. winuse+: tree plus image, `zoom` crop, `@x,y` mapping |
| 16 | `list-displays` | Displays with bounds, effective-DPI scale, primary flag | H. `EnumDisplayMonitors` plus `GetDpiForMonitor` (`ad:crates/windows/src/system/display.rs:5-60`) | missing | gap |
| 17 | `list-surfaces` | Overlays an app shows now: window, focused, sheet (Chromium modal), menu with `item_count` | H. `ad:crates/windows/src/tree/surfaces.rs:124-139`, `tree/surface_inventory.rs` | missing as a command. Popups are their own Windows in `list-windows` (`winuse:skills/winuse/SKILL.md:122-126`) | partial |
| 18 | `open-system-surface` | Raise Start, taskbar, tray, tray overflow or Action Center and return its window | Hd. Shell accelerators such as Win+A, Esc to close (`ad:crates/windows/src/system/shell/shell_surface_open.rs:13-27`; `shell_surface_kinds.rs:116,195-210`) | missing. winuse drops `Progman`, `WorkerW`, `Shell_TrayWnd` from listings (`winuse:internal/win/enum.go:64-71`) and refuses the Windows key (`internal/keys/keys.go:146`) | gap |

### Interaction (14)

| # | Command | What it does | Windows implementation (ad) | winuse equivalent | Gap |
|---|---|---|---|---|---|
| 19 | `click` | Primary action | H. Invoke, then LegacyIAccessible.DoDefaultAction; `--headed` only activates first (`ad:crates/windows/src/actions/dispatch.rs:192-249`) | `click <window> <ref>`: Invoke, Toggle, Select, Expand/Collapse, Legacy (`winuse:internal/uia/headless.go:11-31`); `--headed` is a real SendInput click at the Element's point (`internal/helper/actions.go:149-160`) | winuse+ |
| 20 | `double-click` | Double-click a ref | Hd. SendInput at the element, foreground check (`ad:crates/windows/src/actions/physical_click.rs:88-109`; `physical_target.rs:47-55`) | `click <ref> --count 2` (Headed) | = |
| 21 | `triple-click` | Triple-click a ref | Hd, same | `click <ref> --count 3` | = |
| 22 | `right-click` | Context menu | Hd on Windows; the tree never emits `RightClick` (`ad:crates/windows/src/tree/actions.rs:51-95`) | `click <ref> --button right` (Headed) | = |
| 23 | `type <ref> <text> [--interval-ms]` | Insert text at the caret, read back | Hd. SendInput; the element must already hold keyboard focus (`ad:crates/windows/src/actions/physical_keyboard.rs:1-20`; `physical_target.rs:37-45`). `--interval-ms` paces keystrokes (`ad:crates/core/src/commands/type_text_interval.rs:1-40`) | `type-text <window> [--post] <text>` into the focus: Headless via EM_REPLACESEL into classic edits (`winuse:internal/helper/keyroute.go:12,229`), Headless on a Page, Posted Keys, Headed otherwise | winuse+ headless. Gap: no ref target, no pacing |
| 24 | `set-value` | Direct value write, read back | H. ValuePattern, then RangeValue; never echoes a password (`ad:crates/windows/src/actions/value_write.rs:1-5,83-111`) | `set-value`: Headless ValuePattern/RangeValue, read back, password-safe (`winuse:internal/uia/headless.go:409-420`); `on`/`off` for toggles; Page options | = / winuse+ |
| 25 | `clear` | Empty a field | H. `SetValue("")` (`value_write.rs:114-131`) | `set-value <ref> ""` | = (no alias) |
| 26 | `focus` | Keyboard focus without a click | Hd. UIA SetFocus moves the foreground (A3-4, A19-5) (`ad:crates/windows/src/actions/focus.rs:1-6,98-107`) | none for an Element; Raise acts on the Window | small gap (workaround `--headed click`) |
| 27 | `select <ref> <value>` | Pick an option by display text | H. SelectionItem on the target, or a bounded descendant search by name that expands a collapsed combo first (`ad:crates/windows/src/actions/select.rs:1-9`; `select_search.rs`) | `perform-secondary-action <item> Select`; by label only on a Page list (`winuse:internal/cli/cli.go:82-87`) | gap: by-name pick in a UIA combo or list |
| 28 | `toggle` | Flip | H. TogglePattern, then Invoke (`ad:crates/windows/src/actions/toggle_state.rs:138-164`) | `perform-secondary-action <ref> Toggle` with Invoke fallback (`winuse:skills/winuse/SKILL.md:186-188`) | = |
| 29 | `check` | Idempotent on | H | `set-value <ref> on` (`SKILL.md:223-228`) | = |
| 30 | `uncheck` | Idempotent off | H | `set-value <ref> off` | = |
| 31 | `expand` | Expand, verify | H. ExpandCollapse, then Invoke (`ad:crates/windows/src/actions/disclosure.rs:1-5`) | `perform-secondary-action <ref> Expand` | = |
| 32 | `collapse` | Collapse, verify | H. Same | `perform-secondary-action <ref> Collapse` | = |

### Scroll (2)

| # | Command | What it does | Windows implementation (ad) | winuse equivalent | Gap |
|---|---|---|---|---|---|
| 33 | `scroll <ref> --direction --amount` | Scroll a scroll area by N units | H. ScrollPattern SmallIncrement N times, verified (`ad:crates/windows/src/actions/scroll.rs:1-29`) | `scroll <window> <ref> <dir> [steps]` (`winuse:internal/uia/scroll.go:4-13`) | = |
| 34 | `scroll-to` | Bring into view | H. ScrollItem.ScrollIntoView, then an ancestor ladder (`ad:crates/windows/src/actions/scroll_into_view.rs:1-8`) | `scroll <window> <ref> into-view` | = |

### Keyboard (3)

| # | Command | What it does | Windows implementation (ad) | winuse equivalent | Gap |
|---|---|---|---|---|---|
| 35 | `press COMBO [--app\|--window-id] [--force]` | Key combo to the focus or one app | Unscoped press is SendInput to whatever has focus. A scoped press fails closed unless the app already owns the foreground (`ad:crates/windows/src/system/key_dispatch.rs:80-92`). Dangerous combos need `--force` (`ad:crates/windows/src/input/blocked_combo.rs:27-36`) | `press-key <window> <key> [--post]`: Headed after Activate; Headless Return/Escape on classic dialogs (`winuse:internal/helper/keyroute.go:61`); Posted Keys; Page keys Headless. Only the Windows key is refused (`internal/keys/keys.go:146`) | gap: blocked-combo list. winuse+: headless routes |
| 36 | `key-down` | Reserved; parses, applies the blocked check, then `ACTION_NOT_SUPPORTED` (`ad:docs/commands/keyboard.mdx:59-83`) | R | none | none in practice |
| 37 | `key-up` | Reserved, same | R | none | none |

### Mouse (7)

| # | Command | What it does | Windows implementation (ad) | winuse equivalent | Gap |
|---|---|---|---|---|---|
| 38 | `hover <ref>\|--xy` | Move the cursor to an element centre or point | Hd. SendInput (`ad:crates/windows/src/input/mouse/mod.rs:1-14`) | `hover <window> @x,y` (Headed); `hover <window> <Page element>` (Headless) | gap: hover by UIA Element |
| 39 | `drag --from/--to (ref or xy)` | Press, move, release; `--duration`, `--drop-delay` (500 ms) | Hd. SendInput with a release guard (`ad:crates/windows/src/input/drag.rs:1-8`) | `drag <window> @x,y @x,y` (16 interpolated moves); Page Elements Headless (`winuse:docs/codex-parity.md:148-160`) | gap: UIA Element endpoints, drop delay |
| 40 | `mouse-move --xy` | Move to absolute screen point | Hd | `hover @x,y` (screenshot pixels) | = (different coordinate space, by design) |
| 41 | `mouse-click --xy --button --count 1-100 --modifiers` | Click at a point, optionally holding modifiers | Hd | `click <window> @x,y --count 1-3 --button` | gap: modifiers (Ctrl+click, Shift+click) |
| 42 | `mouse-down` | Reserved | R | none | none |
| 43 | `mouse-up` | Reserved | R | none | none |
| 44 | `mouse-wheel --x --y --dx --dy --modifiers` | Wheel at a point | Hd | `scroll <window> @x,y <dx> <dy>` | gap: modifiers (Ctrl+wheel zoom) |

### Notifications (4)

| # | Command | What it does | Windows implementation (ad) | winuse equivalent | Gap |
|---|---|---|---|---|---|
| 45 | `list-notifications` | Notification rows (app, title, body, actions); `--app`, `--text`, `--limit` | UIA read of the Action Center, keyed on `MainListView` and `ClearAllButton`; H only while the center is open, otherwise Hd raises it (`ad:crates/windows/src/notifications/list.rs:12-15`; `ad:skills/agent-desktop-windows/references/shell-and-overlay.md:37-51`) | missing | gap |
| 46 | `dismiss-notification INDEX --expected-app/--expected-title` | Dismiss one, with an identity guard | Hd | missing | gap |
| 47 | `dismiss-all-notifications` | Dismiss all, or one app's | Hd | missing | gap |
| 48 | `notification-action INDEX ACTION` | Press a notification's button | Hd | missing | gap |

### Clipboard (3)

| # | Command | What it does | Windows implementation (ad) | winuse equivalent | Gap |
|---|---|---|---|---|---|
| 49 | `clipboard-get --format text\|auto\|image\|file-urls [--out]` | Read text, image (to a file) or file list | H. CF_UNICODETEXT, CF_DIB/CF_DIBV5/"PNG", CF_HDROP; delay-rendered reads on an abandonable worker; hung owner refused first (`ad:crates/windows/src/input/clipboard/mod.rs:1-6`; `clipboard_formats.rs:5-11`) | missing (no clipboard code anywhere in `winuse:internal`) | gap |
| 50 | `clipboard-set TEXT \| --image \| --file-url` | Write one representation | H. Reported `delivered_unverified` on Windows (`ad:docs/commands/clipboard.mdx:77`) | missing | gap |
| 51 | `clipboard-clear` | Empty the clipboard | H | missing | gap |

### Wait (1)

| # | Command | What it does | Windows implementation (ad) | winuse equivalent | Gap |
|---|---|---|---|---|---|
| 52 | `wait` | One mode per call: `MS`; `--element REF --predicate exists\|enabled\|visible\|actionable\|value`; `--window TITLE`; `--text TEXT [--count N]`; `--menu`/`--menu-closed`; `--notification`; `--event window-opened\|window-closed\|app-launched\|app-terminated\|focus-changed\|surface-appeared\|surface-dismissed`. Default timeout 30 s, up to 24 h (`ad:docs/commands/wait.mdx:9-172`) | H, polling about every 100 ms (`ad:crates/windows/src/system/wait.rs:10-30`; `system/signal/`). `--notification` is Hd while the center is closed | `wait <window> selector\|text\|url\|load\|download`, Page only, 10 s default, up to 25 s (`winuse:internal/cli/cli.go:187-193`; `internal/cli/page.go:185-193`) | gap: no desktop wait |

### System (7)

| # | Command | What it does | Windows implementation (ad) | winuse equivalent | Gap |
|---|---|---|---|---|---|
| 53 | `status` | Health, platform, permissions, latest snapshot, session, `supported_surfaces`, state root | H (`ad:crates/core/src/commands/status.rs`) | `version`; `browser status` for the Browser Connection only | gap |
| 54 | `permissions [--request] [--guide]` | Permission states | On Windows: `accessibility` granted when UIA answers, `automation` not required; `--guide` unsupported (`ad:crates/windows/src/system/permissions.rs:1-10`; `ad:docs/platforms/windows.mdx:15-21`) | none; elevated targets are refused per Action (`winuse:internal/win/integrity.go:62-78`) | low value on Windows |
| 55 | `version` | Version, arch, OS as JSON | H | `version` (text) | = |
| 56 | `skills list\|get NAME [REF] [--full]\|path` | Print the skills compiled into the binary, matched to its version | `include_str!` of every SKILL.md and reference (`ad:crates/core/src/commands/skills.rs:4-30`) | none; the skill is installed from the repo (`winuse:skills/winuse/SKILL.md:15-36`) | gap (S) |
| 57 | `session start\|end\|list\|gc` | On-disk namespace for snapshots and a trace; `--screenshots`, `--cursor`, `--multi-agent` (`ad:docs/sessions.mdx`) | H | missing | gap |
| 58 | `cursor-overlay enable\|disable [style]` | Configure the presentation-only cursor per session: label, accent, size, images, motion, multi-agent | Detached renderer process over a named pipe, click-through, non-activating; about 355 ms per action; caret after text entry (`ad:skills/agent-desktop-windows/references/shell-and-overlay.md:79-137`) | Agent Cursor, always on for Headless Actions, no configuration (`winuse:CONTEXT.md:81-83`; `internal/helper/headless.go:87,320,390`) | partial: no config, no caret, one cursor |
| 59 | `trace show\|export` | Read the merged trace; export a self-contained HTML viewer | H (`ad:crates/core/src/commands/trace.rs`; `ad:docs/guides/traces.mdx`) | missing (the helper writes a debug log only, `winuse:internal/helper/helper.go:217`) | gap |

### Batch (1)

| # | Command | What it does | Windows implementation (ad) | winuse equivalent | Gap |
|---|---|---|---|---|---|
| 60 | `batch JSON [--stop-on-error] [--timeout-ms]` | Up to 64 JSON entries across any windows and commands; continues past failures unless told; one envelope per entry; validated before the first entry (`ad:docs/commands/batch.mdx:9-116`) | Same dispatcher as the CLI | `batch <window> <action> ... then ...`: one Window, stops at the first failure, prints only `ok`, 60 s (`winuse:internal/cli/batch.go`; `skills/winuse/SKILL.md:448-473`) | gap: per-step output, several Windows, continue-on-error |

## 2. Cross-cutting features

| Feature | agent-desktop | winuse | Gap / note |
|---|---|---|---|
| Output format | One JSON envelope per command on one line: `version` (contract 2.5), `ok`, `command`, `data` or `error`; omission rules; OperationReport for 13 commands (`ad:docs/json-output.mdx:7-128`) | Text: `ok` or command output on stdout; one failure line plus `hint:` on stderr (`winuse:internal/cli/cli.go:464-517`). The helper's Response carries `out`, `err`, `hint`, `exit` strings (`internal/protocol/protocol.go:355-362`) | gap. A default JSON switch conflicts with ADR 0004 and the codex-parity "tree format" and "error style" rows. Recommend an opt-in `--json` |
| Error codes | 16 codes (`PERM_DENIED`, `STALE_REF`, `AMBIGUOUS_TARGET`, `POLICY_DENIED`, `TIMEOUT`, ...) plus `details.kind` (`ad:docs/errors-and-recovery.mdx:45-69`) | No codes. Exit 0, 1, 2 and 130 (Esc) (`winuse:skills/winuse/SKILL.md:475-479`) and stable phrases | gap: nothing machine-readable beyond exit codes |
| Delivery disposition and retry | `not_delivered/safe`, `delivery_uncertain`, `delivered_unverified`, `delivered_verified` (all `unsafe`), `unknown`, on every error and on action success (`ad:docs/errors-and-recovery.mdx:71-90`) | The same three outcomes as phrases: "nothing was sent", "the Action was delivered; do not repeat it", "may have taken effect" (`winuse:internal/helper/headless.go:294,450`; `SKILL.md:480-499`) | Semantics match; the form does not. Fold into `--json` |
| Recovery strategies | `recovery.strategy` (refresh snapshot, inspect state, request explicit policy), `retry_after_ms` 250 (`ad:docs/errors-and-recovery.mdx:92-109`) | `hint:` line with the exact next command | = in intent |
| Refs and re-identification | Snapshot-qualified `@<snap>:eN`, persisted to disk, re-identified per command from stored evidence (process, role, name, native id, bounds hash, path); `STALE_REF`, `AMBIGUOUS_TARGET`, `SNAPSHOT_NOT_FOUND`; 1 MB refmap cap, 128 snapshots kept (`ad:docs/snapshots-and-refs.mdx:94-199`) | Element Index valid only for the latest Window State; live UIA elements kept in the helper (ADR 0004 "Live-Element store"); `name:`/`id:` resolved against that cached state, ambiguity lists candidates (`winuse:internal/helper/refs.go:90-140`); stale message (`internal/helper/actions.go:265`) | Different by design (Codex `element_index`). Gap: refs do not survive a helper restart or a re-read, and `name:`/`id:` never search the live tree. Replacing the model conflicts with Codex parity and CONTEXT.md "Element Index". Recommend live re-resolution for `name:`/`id:` only |
| Skeleton and `--root` drill-down | `--skeleton` clamps depth to 3 with `children_count`; `--root REF` reads a region and adds refs to the same snapshot; re-drill replaces only that region (`ad:docs/skeleton-and-drill-down.mdx:7-153`) | Full tree every time, bounded by caps and a note (`winuse:internal/uia/walk.go:101-103`) | gap. A region read needs an ADR 0004 amendment, because a new read replaces the whole live store and renumbers |
| find / query | `find` with filters and shaping; `roles_present` on a role miss (`ad:docs/commands/observation.mdx:46-106`) | Exact `name:`/`id:` only | gap |
| wait (all predicate kinds) | See row 52, plus post-action `--wait-for`/`--wait-for-gone` with `role:text` selectors on snapshot, press, hover, drag and every ref action (`ad:docs/configuration.mdx:32-52`) | Page waits only: CSS selector, text, URL with `*`, load/domcontentloaded, download (`winuse:internal/cli/cli.go:187-193`) | gap: window appear/disappear, tree text, element state, value, menu, events, post-action wait |
| Actionability preflight | visible, stable, enabled, receives_events (hit test, names the occluder), supported_action, policy, editable; polled up to `--timeout-ms` 5000 (`ad:docs/commands/interaction.mdx:61-85`; `ad:crates/windows/src/tree/hit_test/mod.rs:1-12`) | Page: refuses covered, disabled, hidden or off-screen and names the cover (`SKILL.md:407-409`). UIA: disabled, minimized, modal-blocked, elevated and locked refusals, no polling (`winuse:internal/helper/headless.go:524-527`; `internal/win/activate.go:163-171`) | partial: no UIA occlusion check, no wait-until-enabled |
| Post-action surfaces | `click`, `expand`, `select`, `press` and the multi-clicks report open menus, sheets and alerts in `data.surfaces` (`ad:docs/surfaces.mdx:53-66`) | None. A modal is found only when the next Action is refused. A Page file chooser is reported (`winuse:internal/helper/pageelements.go:442`) | gap |
| Clipboard | 3 commands, text, image, files | none | gap |
| Notifications | 4 commands plus `wait --notification` | none | gap |
| Shell surfaces | Start, taskbar, tray, overflow, Action Center (`ad:docs/platforms/windows.mdx:53-55`) | Excluded from listings (`winuse:internal/win/enum.go:64-71`) | gap |
| Sessions, traces, redaction | Session namespaces, JSONL traces per process, `trace show`/`export` HTML, sensitive fields replaced by `{"redacted":true}`, screenshots unredacted, budget 32 MiB (`ad:docs/guides/traces.mdx:7-109`; `ad:crates/core/src/trace/trace_sanitize.rs`); `--trace PATH`, `--debug --screenshot PATH.html` (`ad:docs/configuration.mdx:21-27`) | Helper log only | gap (audit and debugging) |
| Blocked key combos | `alt+f4`, `win+l`, `win+d`, `alt+tab`, `win+r`, `win+x`, `win+e`, `ctrl+shift+esc` and modifier supersets need `--force` (`ad:crates/windows/src/input/blocked_combo.rs:27-60`) | Windows key refused in code with no override (`winuse:internal/keys/keys.go:146`; `internal/helper/keyboard.go:166-171`); `Alt_L+F4`, `Alt_L+Tab` and `Control_L+Shift_L+Escape` go through | gap (S) |
| Protected processes | close-app refuses 9 session-critical images (`ad:crates/windows/src/system/app_ops.rs:7-17`) | No process killing at all | n/a until a close-app ticket |
| Interaction lease | Machine-wide exclusive `CreateFileW` lock, taken only during dispatch; a child can inherit it; the cursor travels before the lease (#347) (`ad:crates/windows/src/system/interaction_lease/mod.rs:1-46`) | One helper per user serializes every Action through an action gate; queued requests time out instead of acting late (`winuse:internal/helper/state.go:281-300`) | = for one user (inference: every winuse caller goes through the one helper pipe) |
| Multi-agent | `--agent-id`, per-agent cursors, required in `--multi-agent` sessions (`ad:docs/sessions.mdx:98-102`) | One Agent Cursor; no agent identity | gap (low priority) |
| `skills get` | Skills embedded in the binary | Installed separately | gap (S) |
| Cursor overlay | Configurable per session, image cursors, caret after typing, multi-agent, ~355 ms per action | Agent Cursor always on for Headless, hidden from captures, never activates (CONTEXT.md) | partial |
| Headed policy | Two policy bits, `POLICY_DENIED` with `request_explicit_policy_then_retry_original` (`ad:crates/core/src/actions/interaction_policy.rs:3-47`) | `--headed` flag, "nothing was sent" with the exact `--headed` retry (ADR 0010, ADR 0011) | = in intent |
| Timeouts | Per command and caller-settable: snapshot 3000 ms (partial), find 5000, ref actions 5000 plus 30 ms per typed char, wait 30000, batch 60000, launch 30000 (`ad:docs/skeleton-and-drill-down.mdx:332-343`; `ad:docs/commands/interaction.mdx:61-63`) | Fixed: 10 s, launch 15 s, batch 60 s, browser 30-180 s; only Page `wait --timeout` up to 25 s (`winuse:internal/protocol/protocol.go:28-74`) | gap: no caller-set budget for slow apps |
| Integrity checks | Token comparison before input, `PERM_DENIED` with `E_ACCESSDENIED` (`ad:docs/platforms/windows.mdx:15-21`) | Integrity-level comparison, refused with "nothing was sent" (`winuse:internal/win/integrity.go:62-78`) | = |
| Locked desktop / session 0 | Documented as unsupported (`ad:docs/platforms/windows.mdx:12`) | Detected and refused with a named message, at the CLI and in the helper (`winuse:internal/win/desktop.go:26-37`; `internal/cli/cli.go:421-434`) | winuse+ |
| Launch options | `--arg`, `--env`, `--cwd`, `--no-attach`, `--activate`, `--cdp`, `--timeout` | App identifier only | gap (S) |
| Multi-monitor and DPI | Per-monitor-aware v2; `list-displays` effective scale; display capture by index; multi-monitor overlay placement unit-tested only (`ad:crates/windows/src/system/display.rs:5-13`; `shell-and-overlay.md:126-128`) | Per-monitor-aware v2 (`winuse:internal/win/dpi.go:9-31`); screenshot pixels mapped to physical and re-anchored to live bounds (`docs/codex-parity.md:260-265`) | gap: displays are invisible to the agent |
| Process model | One process per command; state on disk; macOS-only command host (`ad:AGENTS.md:188-203`) | Persistent helper over a SID-restricted pipe, idle exit after 5 min (`winuse:docs/codex-parity.md:186-194`) | winuse+ (latency, live UIA objects) |
| Chromium | UIA only, settle loop, `launch --cdp` hands off to agent-browser | CDP Page inside the Window State, Tabs, navigation, waits, uploads, dialogs, eval (ADR 0012) | winuse+ |
| Library API | C ABI `cdylib` (`ad:docs/ffi/`) | none (ADR 0001: skill plus CLI) | not recommended |

## 3. What winuse has that agent-desktop lacks (do not regress)

- **Browser Pages through CDP inside the Window State**: one numbering for frame
  and Page, headless typing, keys, hover, drag and multi-clicks on a Page,
  `navigate`, `back`, `forward`, `reload`, `wait`, `eval`, `dialog`, `upload`,
  `wait download`, Tabs, and four connection routes including the extension
  (ADR 0012; `winuse:internal/cli/cli.go:131-196`). agent-desktop sends web work
  to a second tool (`ad:AGENTS.md:7-8`).
- **AutoSetFocus FALSE** so pattern calls do not raise the Window
  (`winuse:internal/uia/thread.go:74-79`). agent-desktop never sets it (earlier
  comparison, section 3).
- **Wider headless click route**: Toggle, Select and Expand/Collapse before the
  legacy default action (`winuse:internal/uia/headless.go:11-31`).
- **Headless keys and text outside browsers**: Return/Escape on classic dialogs
  through DM_GETDEFID, EM_REPLACESEL typing, and opt-in Posted Keys (ADR 0011;
  `winuse:internal/helper/keyroute.go`). agent-desktop refuses all three headless
  on Windows (`ad:docs/platforms/windows.mdx:74-75`).
- **Non-activating restore and minimize** (`SW_SHOWNOACTIVATE`,
  `SW_SHOWMINNOACTIVE`) versus agent-desktop's activating `SW_RESTORE`/`SW_MINIMIZE`.
- **Modal-dialog refusal that names the dialog**, including dialogs never
  activated (`winuse:internal/win/activate.go:163-171`).
- **Esc stop** through a low-level keyboard hook, exit 130
  (`winuse:internal/win/eschook.go:189-256`). agent-desktop has no stop key.
- **Headed in-control banner and border** for physical input (codex-parity
  "overlay" row).
- **Reliable activation** (AttachThreadInput and verify) versus a plain
  `SetForegroundWindow` that agent-desktop measured failing (A21-6).
- **`launch-app` by display name, Start-menu shortcut or AUMID**, and `list-apps`
  with recently used Apps. agent-desktop takes only a path or a System32 name.
- **`@x,y` in screenshot pixels**, re-anchored to the live Window, and
  `set-value`/`perform-secondary-action` on the Element under a point (agent-desktop's
  `element-at` is still PR #477).
- **`zoom`** full-resolution PNG crop; **tree and screenshot in one call**.
- **Codex text tree**: a few hundred tokens, English roles, explicit truncation
  notes (ADR 0004), against agent-desktop's JSON tree.
- **`--headed` click is a real click** at the Element; agent-desktop's
  `--headed click` stays semantic.
- **Windows key refused in code**, locked-desktop detection, owned dialogs listed,
  16-step interpolated drag.
- **`record-tree`** fixtures and the **Task Suite** with Checks (ADR 0008).

## 4. Proposed tickets

Effort: S under a day, M a few days, L a week or more. "Conflicts" names what
must change with the ticket. The guard test only fails when a Codex tool,
parameter or topic row disappears or loses its status, or when the checklist
names a command the CLI lacks (`winuse:internal/skill/parity_test.go:17-43,142`),
so new commands never trip it. Rows whose text a ticket contradicts must be
reworded, keeping a status.

### P1

1. **Clipboard commands** (S).
   `clipboard-get [--format text|image|files] [--out]`, `clipboard-set <text>|--image|--file`,
   `clipboard-clear`. OpenClipboard needs no foreground. Copy agent-desktop's
   hung-owner guard and abandonable worker for delay-rendered formats.
   Conflicts: CONTEXT.md says every Action is aimed at exactly one Window
   (`winuse:CONTEXT.md:13-14,49-51`). Add a term for desktop-level commands
   (proposal: "Desktop command", not an Action, no Agent Cursor). Not a Batch step.
2. **Report Windows that an Action opened** (S-M).
   After `click`, `perform-secondary-action Expand|Select`, `press-key` and
   `set-value`, diff the App's Windows before and after, and append
   `Note: a new Window opened: <id> <App> "<title>"` (and "the Window closed").
   This is agent-desktop's `data.surfaces`.
   Conflicts: codex-parity "process model" row says "no state echo"
   (`winuse:docs/codex-parity.md:186-194`). Reword it; status stays
   intentionally different.
3. **Desktop waits** (M).
   Extend `wait`: `wait window <app|title> [--gone]`, `wait <window> text <text>`
   for UIA Windows (today Page only), `wait <window> <ref> enabled|value <v>|gone`,
   `wait <ms>`. Poll the bounded walk. Reuse `--timeout` (raise the cap above 25 s).
   Conflicts: none. The Page `wait` syntax stays; a UIA Window routes to the new
   predicates (same Element-origin routing as ADR 0012).
4. **`find` and a region read** (M-L).
   `find <window> [--role] [--name] [--text] [--state] [--first|--count]` prints
   matching lines in ADR 0004 grammar with their Element Indexes.
   `get-window-state <window> --root <ref> [--depth N] [--interactive]` reads one
   subtree.
   Conflicts: ADR 0004. A region read must either keep indexes outside the region
   or renumber the whole state. Recommend: `find` and `--root` print a new full
   numbering of the region only and replace the live store for that Window
   (simplest, matches "an index is valid only for the latest Window State");
   record in an ADR 0004 amendment. Do not adopt snapshot-qualified refs.
5. **Blocked dangerous combos with `--force`** (S).
   Refuse `Alt_L+F4`, `Alt_L+Tab`, `Control_L+Shift_L+Escape` and their supersets
   (agent-desktop's list minus the Windows-key entries, which winuse already
   refuses) unless `--force`.
   Conflicts: codex-parity "key names" row is "matched"; Codex blocks nothing in
   code. Reword as matched (extended).
6. **Modifiers on point clicks and wheel** (S).
   `click @x,y --modifiers ctrl,shift` and `scroll @x,y ... --modifiers ctrl`, for
   multi-select and zoom. Headed only.
   Conflicts: codex-parity click row; add as extension.
7. **Opt-in `--json` output with stable codes and dispositions** (L).
   Global `--json`: one envelope `{ok, command, out, error:{code, message, hint, delivery, retry}}`.
   Add `code` and `delivery` to `protocol.Response`
   (`winuse:internal/protocol/protocol.go:355-362`) and map today's phrases. Keep
   the text tree inside `out`; no JSON tree.
   Conflicts: none if opt-in. A default switch would conflict with ADR 0001's CLI
   framing, ADR 0004 and the "tree format"/"error style" rows. Recommend opt-in.

### P2

8. **Display list and full-screen capture** (S).
   `list-displays` (bounds, scale, primary) and `screenshot --display N` through
   WGC CreateForMonitor or BitBlt.
   Conflicts: CONTEXT.md "Window State" is per Window, and `@x,y` binds to a
   Window's latest screenshot. A display shot gets no `@x,y` binding (observation
   only), or a new rule.
9. **Shell surfaces** (M).
   Let `list-windows --shell` (or a `shell` App) expose the taskbar, tray and
   overflow, and read them as Window States; open Start or the Action Center as
   a Headed Action with `Control_L+Escape`/the Action Center accelerator.
   Conflicts: `winuse:internal/win/enum.go:64-71` excludes these on purpose, and
   the Windows key is refused (Win+A needs an alternative or an exception).
   Needs a short ADR.
10. **`close-app`** (S-M).
    Post WM_CLOSE to every Window of the App, wait for process exit; `--force`
    terminates with agent-desktop's protected list.
    Conflicts: SKILL.md "winuse never ends a process" (`winuse:skills/winuse/SKILL.md:326-327`).
    Recommend graceful only, without `--force`, unless the user wants it.
11. **`maximize-window`** (S).
    Conflicts: CONTEXT.md "Every Window Action is always Headless"
    (`winuse:CONTEXT.md:69-70`) and ADR 0011. Recommend a Headed Window Action
    (`ShowWindow(SW_MAXIMIZE)` after activation) and amend both.
12. **Select an option by name in UIA combos and lists** (S-M).
    `set-value <combo> <label>` on a UIA ComboBox or List: expand if collapsed,
    find the item by name in a bounded search, SelectionItem.Select, read back.
    Conflicts: none (extends `set-value` the way Page lists already work).
13. **Hover and drag by UIA Element, with a drop delay** (S).
    `hover <window> <ref>` and `drag <window> <ref> <ref|@x,y> [--drop-delay ms]`,
    Headed at the Element's clickable point.
    Conflicts: CONTEXT.md lists hover and drag as always Headed; still true.
14. **Caller-set timeouts** (S).
    `--timeout <s>` on `get-window-state`, `launch-app` and `batch`, clamped, for
    slow Electron or Office trees.
    Conflicts: codex-parity "timeouts" row (10 s matched); add an extension note.
15. **`launch-app` options** (S).
    `--arg` (repeatable), `--cwd`, `--env`; not `--cdp` (browser launch owns it).
    Conflicts: none; Codex `app` row stays matched.
16. **`winuse skill` prints the embedded skill** (S).
    `go:embed` `skills/winuse/SKILL.md`; `winuse skill [path]`, so the text matches
    the binary.
    Conflicts: none (ADR 0001 is satisfied either way).
17. **`status`** (S).
    Helper running and its version, input desktop state, own integrity level,
    DPI awareness, Browser Connection summary.
    Conflicts: none.
18. **Live re-resolution of `name:`/`id:`** (M).
    When the cached state is stale or missing (helper restart), resolve
    `name:`/`id:` against a fresh bounded walk of the Window instead of failing.
    Conflicts: codex-parity "stale elements" row (already "matched (extended)").
    Keep Element Indexes strict.

### P3

19. **Notifications** (M).
    `list-notifications` and `notification-action`/`dismiss` over the Action
    Center through UIA, Headed when the center is closed, with agent-desktop's
    identity guard. Conflicts: as ticket 1 (not aimed at a Window) and ticket 9
    (shell windows).
20. **Session traces with redaction** (M-L).
    `--trace <file>` JSONL of command, outcome, duration and refs, with names,
    values and text redacted. Conflicts: none.
21. **Batch improvements** (S-M).
    Print each step's output (for `eval`, `tab list`), `--continue` past failures.
    Conflicts: SKILL.md "A Batch prints only ok" (`winuse:skills/winuse/SKILL.md:471-473`).
22. **Agent Cursor polish** (S).
    A caret after typing (Text pattern), optional label or accent through an
    environment variable. Conflicts: CONTEXT.md says the cursor "needs nothing from
    you"; keep defaults.

### Not recommended

- **Snapshot-qualified, disk-persisted refs as the default.** They replace Codex's
  Element Index (`winuse:internal/skill/parity_test.go:23` `element_index`) and
  CONTEXT.md's model. Tickets 4 and 18 get most of the benefit.
- **A JSON tree.** It costs more tokens than ADR 0004's grammar for the same
  information.
- **`key-down`, `key-up`, `mouse-down`, `mouse-up`.** agent-desktop ships them only
  as fail-closed names. winuse's helper could hold input, but the Esc stop and
  release guarantees would need design first.
- **`get`/`is`.** The tree line already carries the states; `wait` (ticket 3)
  covers the polling use.
- **A C library (FFI).** ADR 0001.
- **Multi-agent cursors and agent ids.** Out of scope while one helper serializes
  all Actions (`winuse:.scratch/headless/spec.md` out of scope: machine-wide lease).

## Count

agent-desktop commands: 60.

- Equal or better in winuse (21): rows 3, 5-8, 10, 19-22, 24, 25, 28-34, 40, 55.
- Partial (17): rows 1, 2, 4, 11, 15, 17, 23, 26, 27, 35, 38, 39, 41, 44, 52, 58,
  60. In rows 11, 15 and 23 winuse leads on the main use and lacks only flags.
- Missing (22): rows 9, 12-14, 16, 18, 36, 37, 42, 43, 45-51, 53, 54, 56, 57, 59.
  Rows 36, 37, 42 and 43 are reserved names that fail closed in agent-desktop.

Proposed tickets: 22 (7 P1, 11 P2, 4 P3).
