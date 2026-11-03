# 01: Launch without the foreground, and launch options

**What to build:** `launch-app` and `browser launch` start the App with a non-activating show state, then check the foreground and say when the App took it. `launch-app` also gains agent-desktop's launch options.

Today:

- `internal/apps/launch.go:48-67` uses `exec.Command` with no STARTUPINFO show state.
- `internal/apps/launch.go:125-147` uses ShellExecuteEx with `nShow = SW_SHOWNORMAL` (`:133`).
- `internal/browser/system_windows.go:298-315` uses `exec.Command`. The browser-merge probe measured "Launch took the foreground: yes".

Measured (`../headless-gaps/headless-100.md` G11): charmap launched from a background process never took the foreground in any mode. SW_SHOWMINNOACTIVE in STARTUPINFO was honoured. The likely cause of the browser case is that `win.Activate` sends a SendInput zero move (`internal/win/activate.go:132`), which gives the helper the foreground right until the user's next input, and its children inherit it (inference).

Route:

- Win32: a direct `CreateProcess` with `STARTF_USESHOWWINDOW` and `SW_SHOWNOACTIVATE`. Go's `SysProcAttr` only has `HideWindow`, so this needs its own call (the probe's `launchCP`).
- `--minimized`: `SW_SHOWMINNOACTIVE`.
- ShellExecuteEx (shortcuts, documents): `nShow = SW_SHOWNOACTIVATE`.
- Packaged apps (AUMID): `IApplicationActivationManager` has no no-foreground option (`ACTIVATEOPTIONS` in SDK 10.0.26100). Rely on the foreground lock and report the result.
- After the first Window appears, compare the foreground with the one before the launch. If it changed to the App: `ok (the App took the foreground)`.

Launch options (from desktop-parity research ticket 15): `--arg <a>` (repeatable), `--cwd <dir>`, `--env KEY=VALUE` (repeatable). Not `--cdp`: `browser launch` owns that (ticket 15 covers apps).

**Blocked by:** none

**Status:** done

- [x] Win32 `CreateProcess` with STARTUPINFO show state; ShellExecuteEx with `SW_SHOWNOACTIVATE`
- [x] `browser launch` uses the same launcher
- [x] `--minimized`, `--arg`, `--cwd`, `--env` parsed in the CLI and carried in the protocol
- [x] Foreground compared before and after; the note when the App took it
- [x] Offline tests: show state per mode, argument quoting, the note
- [x] Opt-in e2e: launch the fixture and Notepad behind a covering Window, foreground unchanged (written and vetted; not run yet, see the comment)
- [x] Usage text and SKILL.md launch section

## Comments

### 2026-11-03: triage decision (user)

Kept as written.

### 2026-11-03: packaged-app launch measured

IApplicationActivationManager.ActivateApplication(AO_NONE) for Calculator made its ApplicationFrameWindow the foreground about 170 ms after the call, from a caller without the foreground (`../headless-gaps/probes-round5.md` section 5). The AUMID path cannot be kept in the background. Keep the plan: report `ok (the App took the foreground)`.

### 2026-11-03: implemented (branch hm01-launch-noactivate)

- `win.StartProcess` (`internal/win/launch.go`) is the one launcher: a direct `CreateProcess` with `STARTF_USESHOWWINDOW` and `SW_SHOWNOACTIVATE` (`SW_SHOWMINNOACTIVE` with `--minimized`), the quoted command line (`windows.EscapeArg` per argument), the working directory (default: the executable's folder) and a `CREATE_UNICODE_ENVIRONMENT` block when `--env` is given. Same creation flags as before (new process group, breakaway retried without). It refuses in a test binary (`LiveDesktopGuard`); so does `shellExec` now.
- `apps.StartApp(spec, opts)`: an executable goes through `win.StartProcess`; a shortcut or AUMID through ShellExecuteEx with `nShow` from the same show state. `browser launch` and `browser clone` start through `win.StartProcess` (`startDetached`).
- Output decisions:
  - `launch-app` prints a Window line, not `ok`, so the note is a second line: `Note: the App took the foreground; winuse does not take it back.` Appending it to the Window line would have changed the title field that agents parse. The check is `selfActivated` from ticket 02: the foreground changed, to a Window of the opened Window's process, and was not that App's before. It runs when the Window is found, with no extra settle.
  - `browser launch` ends its `ok` line with ` (the browser took the foreground)` when the foreground changed to a Window of the started pid (`System.Foreground`). `browser clone` reports nothing new.
- `--arg`, `--cwd`, `--env` need an executable. A name or path that resolves to a shortcut or a packaged App is refused before anything starts ("nothing was started"). A shortcut's hint names its target executable. Why: the shell starts a shortcut with its own command line and folder, and a packaged App through its activation, so the options would be dropped without anyone noticing. `--minimized` goes to every kind; a packaged App may ignore it.
- CLI: flags anywhere around the App; `--arg`/`--env` repeat and keep their order; the token after `--arg`/`--cwd`/`--env` is the value even when it starts with `-`, and `--flag=value` works. `--cwd` is resolved against the CLI's folder, and the helper checks it is an existing absolute folder. `--env` must be `KEY=VALUE` with a non-empty key, checked in the CLI and the helper. The global `--headed` is not taken from an option's value (`stripHeadedLaunch`).
- Offline tests: `internal/win/launch_test.go` (show state per mode, quoting incl. spaces, quotes, trailing backslash, empty argument; working folder; env merge and block; the live guard), `internal/apps/launch_test.go` (ShellExecuteEx `nShow` per mode, executable-only refusal, the guard), `internal/helper/launch_test.go` (Window line alone, the note, no note when the App already had the foreground, options reach the launcher, refusals start nothing), `internal/cli/launch_test.go`, `internal/browser/commands_test.go` `TestLaunchReportsTheForeground`.
- Opt-in e2e (`test/e2e/launch_noactivate_test.go`): `TestLaunchAppKeepsTheForeground` (fixture by path with `--arg -state`, `--env WINUSE_FIXTURE_ORIGIN`, `--cwd`, behind a cover activated through `activate-window`, so the helper holds the foreground right), `TestLaunchAppMinimized`, `TestLaunchAppNotepadReportsTheForeground` (Notepad by AUMID; asserts the note matches what happened and logs it). Notepad by `System32\notepad.exe` path is not tested: on this PC it is the packaged Notepad behind a redirect, and whether launch-app recognizes that Window was not checked.
- **Live e2e not run.** `go vet -tags e2e ./test/e2e` passes. When the checks were done, the user was in a League of Legends match (its game client held the foreground), so no live run was started. Still to measure: whether the fixture's first `ShowWindow(SW_SHOW)` honours the STARTUPINFO show state, and whether packaged Notepad takes the foreground.

### 2026-11-03: the origin check at 150 % (laptop/fx-gone-wait-window-closed)

- Live failure (full laptop run at 0693039, 2560x1440 at 150 %): `the launched fixture is at 134,116, want --env's origin 125,116`. It was the test's scale assumption, not launch-app's placement: WINUSE_FIXTURE_ORIGIN goes to CreateWindowEx, which places the window rect, while the test read the DWM frame bounds, which leave out the invisible left border (9 px at 150 %, past `near`'s 8; 6 px fits at 100 %). The top has no invisible border, so y matched exactly.
- Fix (`test/e2e/launch_noactivate_test.go`): compare `win.WindowRect` (after SetPerMonitorV2DPIAwareness, so it reads physical pixels) with the origin.
- Live: `TestLaunchAppKeepsTheForeground` passed under the lock (10.3 s).
