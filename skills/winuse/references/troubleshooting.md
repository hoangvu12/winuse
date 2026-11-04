# Results, refusals and recovery in full

Read when: an output phrase or note is not in the core's tables, a request timed out, you want `--json` or `--trace`, or failures make no sense (`winuse status`).

This is one topic of the winuse skill. `winuse skill troubleshooting` prints it, and `winuse skill` prints the core, SKILL.md, which holds the safety rules, the Esc stop and recovery.

## Read the result

An Action prints `ok` and any `Note:` lines, or one failure line and a
`hint:` line. The exit code is 0 for success, 1 for a failure, 2 for a
usage error and 130 for an Esc stop. The hint usually names the recovery.
These phrases tell you whether anything reached the app.

- `nothing was sent` means winuse did not touch the app. Fix the cause the
  line names and retry; a retry is safe.
- `cannot be clicked headless` or `has no background route`, with
  `nothing was sent`, means the Action needs the real pointer or keyboard;
  the hint prints the exact `--headed` retry, and `--post` or `--into` when
  they fit. Tell the user before a Headed Action if they are working in
  another Window.
- `the Action was delivered; do not repeat it` means the call went through
  but the Element reads differently afterwards, such as a toggle still
  `off`. Decide from a fresh Window State, because repeating could act
  twice.
- `may have taken effect` means the write failed part way. Read the Window
  State before you repeat anything.
- `is minimized` means this Headless Action cannot reach the Window. Run
  `restore-window` (no foreground), `activate-window`, or pass `--headed`,
  then read the Window State and retry once.

When a failure does not say whether anything was sent, treat the outcome as
unknown and decide from a fresh Window State.

Put `--json` before the command for one JSON line instead:
`{"ok":false,"command":"click","out":"","error":{"code":"STALE_REF",...}}`.
`error` carries the message, hint, a code, and the phrases above as
`delivery` (`not_delivered`, `delivered_unverified`, `delivery_uncertain`)
and `retry` (`safe` or `unsafe`). A Window State stays text in `out`.

After `click`, `perform-secondary-action` Expand or Select, `press-key`,
`set-value` and a Batch, winuse watches the App's Windows for a moment and
adds a line after `ok` for each change. Every note an Action prints, such
as a route's or a point's, is such a `Note:` line:

- `Note: a new Window opened: <id> <App> "<title>"` for a dialog, menu or
  popup the Action opened. Read it next with `get-window-state <id>`. A
  menu has no title and is left out of `list-windows`, but its Window ID
  works in every command.
- `Note: window <id> closed` when the Window you acted on closed, such as a
  dialog after its OK button. Go back to its owner.
- `Note: the App took the foreground; winuse does not take it back.` after
  a Headless, Window or Page Action whose App took the foreground, most
  often by bringing forward a dialog the click opened. The user may have
  been typing elsewhere: tell them, and do not repeat the Action.

Tooltips and notifications are never reported. No note does not prove
nothing opened: a slow App can open its dialog later, so check
`list-windows` when you expected one.

Any command may end with `Note: the helper was restarted for another
winuse build, …`: another winuse binary was using the helper, and the
Window States and Element Indexes you read are gone. Read the Window
again, or use `name:` or `id:`.

## Refusals and recovery

- An administrator Window is refused with an explanation: Windows blocks
  input, posted messages, placement and UI Automation writes into an
  elevated Window. Tell the user it needs winuse started from an elevated
  terminal, and report the Action as not done.
- A locked desktop is reported as locked. Stop and ask the user to unlock
  it, and leave the lock screen alone.
- A modal dialog blocking the target is named in the failure, with its
  Window ID. Handle that dialog's Window, then return to the owner.
- `window <id>'s App is not responding; nothing was sent` means the App
  is hung. Wait until it responds and retry, or work in another App's
  Window meanwhile; other Apps are not held up.
- A timeout fails with `winuse request timed out: <command>`. Requests have
  10 seconds; `launch-app` 15, a Batch 60, Page, tab and most browser
  commands 30, `browser connect` 90 and `browser clone` 180. Only
  `get-window-state` (up to 60), `launch-app` (120), `batch` and `wait`
  (300) take `--timeout <seconds>`; a slow Action means a bug or a hung
  App. Wait two seconds and retry once, with a longer `--timeout` where it
  fits; if it fails again, report the exact error and stop.
- A stale Element, shifted UI or moved Window needs a fresh Window State
  (and screenshot) and one retry.
- A launcher screen, splash screen or unexpected permission prompt is a
  reason to stop, observe and rethink, never to click through.
- When failures make no sense, run `winuse status`: winuse and helper
  versions, helper uptime, lock state, integrity level, DPI awareness,
  Browser Connections, agents served and the Windows build. It only reads
  and never starts the helper; include its output when you report a
  problem.
- To record what you did, put `--trace <file>` before the command, or set
  `WINUSE_TRACE=<file>` for the session. Each command appends one JSON
  line: time, command, Agent, Window, arguments, routes (Headless, Posted,
  Headed, CDP), outcome, up to 4 KiB of output, screenshot paths and
  duration. A Window State is kept as its line count, and typed text,
  set-value values, clipboard content and eval expressions and results as
  their length; pass `--trace-unredacted` only when the user asks for it.
  Past 32 MiB the file is renamed `<name>.1`. `winuse trace show <file>`
  summarizes it. Put the trace in a folder the user chose or a temp folder.
