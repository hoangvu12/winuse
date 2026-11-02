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

**Status:** ready-for-agent

- [ ] Win32 `CreateProcess` with STARTUPINFO show state; ShellExecuteEx with `SW_SHOWNOACTIVATE`
- [ ] `browser launch` uses the same launcher
- [ ] `--minimized`, `--arg`, `--cwd`, `--env` parsed in the CLI and carried in the protocol
- [ ] Foreground compared before and after; the note when the App took it
- [ ] Offline tests: show state per mode, argument quoting, the note
- [ ] Opt-in e2e: launch the fixture and Notepad behind a covering Window, foreground unchanged
- [ ] Usage text and SKILL.md launch section

## Comments

### 2026-11-03: triage decision (user)

Kept as written.

### 2026-11-03: packaged-app launch measured

IApplicationActivationManager.ActivateApplication(AO_NONE) for Calculator made its ApplicationFrameWindow the foreground about 170 ms after the call, from a caller without the foreground (`../headless-gaps/probes-round5.md` section 5). The AUMID path cannot be kept in the background. Keep the plan: report `ok (the App took the foreground)`.
