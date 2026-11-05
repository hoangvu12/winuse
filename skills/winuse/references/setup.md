# Installing winuse

Read when: `winuse version` fails (winuse is missing), SmartScreen blocks the first run, or you need to build winuse or install the skill.

This is one topic of the winuse skill. `winuse skill setup` prints it, and `winuse skill` prints the core, SKILL.md, which holds the safety rules, the Esc stop and recovery.

## If winuse is missing

Run `winuse version`. If the command is not found, install the release
binary with PowerShell. It needs no Go toolchain.

```powershell
irm https://raw.githubusercontent.com/hoangvu12/winuse/main/install.ps1 | iex
```

The installer puts `winuse.exe` in `%LOCALAPPDATA%\winuse` and adds that
folder to the user PATH. The current shell does not see the new PATH, so
restart the terminal and run `winuse version`, or call
`$env:LOCALAPPDATA\winuse\winuse.exe` by its full path in this session.

The binary is unsigned, so Windows Defender SmartScreen may show "Windows
protected your PC" on the first run. That is a false positive. Tell the
user to click More info, then Run anyway, or to unblock the file once with
`Unblock-File "$env:LOCALAPPDATA\winuse\winuse.exe"`. Leave Defender and
antivirus settings to the user. To build from source instead, clone
`https://github.com/hoangvu12/winuse` and run
`$env:CGO_ENABLED=0; go build -o winuse.exe ./cmd/winuse` with Go 1.27 or
newer.

`winuse skill` prints the skill's core, SKILL.md, as the installed binary
has it, `winuse skill <topic>` one topic, `winuse skill list` the topics
and `winuse skill all` everything. After an upgrade, read it again with
`winuse skill`. `winuse skill install <dir>` writes the skill's folder
(SKILL.md and `references\`) to `<dir>\winuse` for a skills folder:
`~\.claude\skills` for Claude Code, `~\.agents\skills` for Codex, Copilot,
Cursor, Gemini CLI and most others. The installer writes both.
