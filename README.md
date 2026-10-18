# winuse

Let your coding agent use Windows apps.

winuse is a command-line tool that reads any open window as text (its
accessibility tree), takes screenshots when needed, and clicks, types and
scrolls for you. Any agent that can run shell commands can use it: Claude
Code, Pi, Codex, and others. The commands and output match Codex Computer
Use.

```
> winuse list-windows
101     Example    Example App

> winuse get-window-state 101
Window: "Example", App: app.exe.
	0 window Example Secondary Actions: Raise
		1 edit (settable, string) Search ID: searchBox
		2 button OK ID: okButton

> winuse set-value 101 id:searchBox "hello"
ok
> winuse click 101 name:OK
ok
```

## Install

Build it (Go 1.27+):

```powershell
$env:CGO_ENABLED=0; go build -o winuse.exe ./cmd/winuse
```

Once a release is published, this one-liner installs the binary instead:

```powershell
irm https://raw.githubusercontent.com/hoangvu12/winuse/main/install.ps1 | iex
```

Then give your agent the skill:

```
npx skills add hoangvu12/winuse
```

Now ask it something like "open Notepad and type hello world using winuse".

## Commands

| Command | What it does |
|---|---|
| `list-windows`, `list-apps` | See what's open |
| `launch-app <name>` | Start an app |
| `get-window-state <window> [--screenshot]` | Read a window as text, optionally with a screenshot |
| `zoom <window> <x>,<y>,<w>,<h>` | Full-resolution crop of a screenshot region |
| `click`, `type-text`, `press-key` | Click an element, type text, press keys like `Control_L+a` |
| `set-value`, `perform-secondary-action` | Fill a field, or expand/toggle/select an element |
| `scroll`, `drag`, `hover` | Pointer actions at screenshot coordinates |
| `batch <window> ... then ...` | Several actions in one call |

Elements are referenced by index, `name:OK`, `id:okButton`, or `@x,y` from
the latest screenshot. The full guide, which your agent loads with the
skill, is [SKILL.md](SKILL.md).

## Staying in control

While winuse acts, a banner frames the window: "winuse is in control — press
Esc to stop". Pressing the physical Esc key stops it right away. winuse
refuses to type into administrator windows or a locked screen, and it never
presses the Windows key.

## More

- [SKILL.md](SKILL.md): how to use every command
- [test/e2e/README.md](test/e2e/README.md): the end-to-end tests
- [tasksuite/README.md](tasksuite/README.md): real tasks for measuring agent performance
- [docs/codex-parity.md](docs/codex-parity.md): comparison with Codex Computer Use
- [CONTEXT.md](CONTEXT.md) and [docs/adr/](docs/adr/): terms and design decisions

Windows only. No telemetry. MIT licensed.
