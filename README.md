# winuse

Let your coding agent use Windows apps.

![The agent's cursor clicks 7, +, 2 and = in Windows Calculator](docs/images/winuse-calculator.gif)

winuse is a command-line tool for Windows. It reads any open window as
text, clicks and fills in its controls, and types and scrolls when a task
needs it. Any agent that can run shell commands can use it, including
Claude Code, Codex and Cursor. The commands and output follow Codex
Computer Use.

## Install

Run this in PowerShell:

```powershell
irm https://raw.githubusercontent.com/hoangvu12/winuse/main/install.ps1 | iex
```

It puts `winuse.exe` in `%LOCALAPPDATA%\winuse`, adds that folder to your
PATH, and installs the skill for every agent it finds. The skill step uses
`npx skills` and needs Node.js. Set `WINUSE_NO_SKILL=1` to skip it.

Then ask your agent something like "open Calculator and work out 7 + 2
using winuse".

To build from source instead, use Go 1.27 or newer:
`$env:CGO_ENABLED=0; go build -o winuse.exe ./cmd/winuse`, then
`npx skills add hoangvu12/winuse`.

## How it works

The agent reads a window as a tree of elements, then acts on them by
index, `name:` or `id:`.

```
> winuse launch-app Calculator
19203320	Application Frame Host	Calculator

> winuse get-window-state 19203320
Window: "Calculator", App: CalculatorApp.exe.
	0 window Calculator Secondary Actions: Raise
	...
					14 text Display is 0 ID: CalculatorResults
	...
						46 button Seven ID: num7Button

> winuse batch 19203320 click name:Seven then click name:Plus then click name:Two then click name:Equals
ok
```

Screenshots are available with `--screenshot` when the tree is not
enough.

## Headless and headed actions

Clicking a button, setting a field and expanding or toggling an element
happen in the background through Windows UI Automation. winuse does not
move your mouse, press keys or bring the window to the front, so you can
keep working in another window. A second cursor shows where the agent is
acting.

Typing, key presses, scrolling, dragging and clicks at screen coordinates
need the real mouse and keyboard. For those, winuse brings the window to
the front and frames it with a banner. Pass `--headed` to make any action
work this way.

Press Esc at any time to stop winuse. It refuses to act on administrator
windows or a locked screen, and it never presses the Windows key.

## Commands

| Command | What it does |
|---|---|
| `list-windows`, `list-apps` | Lists open windows, and running and recently used apps |
| `launch-app <name>` | Starts an app and prints its window |
| `get-window-state <window> [--screenshot]` | Prints a window's element tree, with an optional screenshot |
| `click`, `set-value`, `perform-secondary-action` | Clicks, fills in, expands or toggles an element |
| `type-text`, `press-key` | Types text or presses keys such as `Control_L+a` |
| `scroll`, `drag`, `hover`, `zoom` | Pointer actions and close-ups at screenshot coordinates |
| `batch <window> ... then ...` | Runs several actions in one call |

[SKILL.md](skills/winuse/SKILL.md) is the full guide your agent loads.

## More

- [docs/codex-parity.md](docs/codex-parity.md) compares winuse with Codex Computer Use.
- [CONTEXT.md](CONTEXT.md) and [docs/adr/](docs/adr/) record the terms and design decisions.
- [test/e2e/README.md](test/e2e/README.md) and [tasksuite/README.md](tasksuite/README.md) describe the tests.

Windows only. No telemetry. MIT licensed.
