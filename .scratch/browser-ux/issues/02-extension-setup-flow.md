# 02: Extension setup in fewer steps, with a wait instead of a poll loop

Status: done

**Evidence.**

- `winuse browser status` with Brave running answered with four routes and no recommendation:
  `hint: run winuse browser ext install brave to drive the running browser through the winuse extension, winuse browser clone brave for a copy with your logins (--close-source moves your open tabs into it), winuse browser launch brave for a winuse profile, or winuse browser connect brave to use the running browser through its inspect toggle`
- `ext install brave` ends with "only the user can do the last step ... Tell them:" and three steps. The agent had to stop, relay them, and wait for the user's reply (07:11:54 → 07:12:16).
- The user then asked the agent to do it. The agent did it with winuse (click `Load unpacked`, `set-value <dialog> id:1152 <folder>`, click `id:1`), which worked, but the skill never says this is possible.
- The agent then polled `ext status` with `winuse wait 3000` / `winuse wait 5000` in between (07:12:51, 07:12:52, 07:13:09).
- The `ExtNotConnected` hint repeats "run winuse browser ext install brave" although that had just run and could not have helped (issue 01).

**Root cause.** `ExtInstall` deliberately "opens nothing in the user's
browser" and prints steps; nothing waits for the connection; the
`NoDebugging` hint lists routes without saying which fits the user's own
browser.

**Fix.**

- `winuse browser ext install [browser] --open`: also opens `<scheme>://extensions` in that browser (a new Tab of the running browser; it takes the foreground, so only on request) and an Explorer window with the extension folder selected, so the user can drag the folder onto the page or paste its path. The output says what was opened.
- `winuse browser ext status [browser] --wait <seconds>`: polls until connected (exit 0) or the time runs out (the last diagnosis, exit 1). Runs in the CLI, one status call every second, so no helper request runs long.
- `NoDebugging` hint: recommends the extension for the user's own browser first, and keeps the other routes as alternatives.
- `ExtNotConnected` hint: says that `ext install` re-registers the host under every key the browser reads, then the reload.
- SKILL.md: the setup flow in order (install with `--open`, tell the user, `ext status --wait 120`), and the exact winuse steps to load the extension when the user asks the agent to do it.

## Comments

Done on branch browser-ux: `ExtOpen` (`internal/browser/extension.go`), `browserExtInstall` (`internal/helper/browser.go`), `protocol.BrowserExtArgs`, `--open` / `--wait` parsing and `extStatusWait` (`internal/cli/browser.go`), hints in `internal/browser/problem.go`, SKILL.md, README. Tests: `TestExtOpen`, `TestExtStatusWait`, `TestParseBrowserArgs`. Not verified live: that `<scheme>://extensions` opens from the command line in a running Brave, and the drag-a-folder load.
