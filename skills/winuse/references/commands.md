# Command reference

Read when: you need a command's exact syntax at a glance; `winuse help <command>` prints one command's usage and examples.

This is one topic of the winuse skill. `winuse skill commands` prints it, and `winuse skill` prints the core, SKILL.md, which holds the safety rules, the Esc stop and recovery.

## Command reference

Run `winuse` with no arguments for the full usage text.

```
winuse [--headed] <command> [arguments]

list-windows [--details] [--app <name>] [--shell]
list-apps | get-window <window>
launch-app <name|exe path> [--minimized] [--cdp] [--arg <a>]... [--cwd <dir>] [--env KEY=VALUE]... [--timeout <s>]
close-app <app> [--force]           # --force ends what is still running: the only command that ends a process
get-window-state <window> [--screenshot] [--skeleton | --root <element> [--depth N] [--interactive]] [--timeout <s>]
find <window> [--role <r>] [--name <text>] [--text <text>] [--state <s>] [--first | --count] [--timeout <s>]
zoom <window> <x>,<y>,<w>,<h>
click <window> <ref> [--count 1|2|3] [--button left|middle|right] [--post] [--modifiers ctrl,shift,alt]
# --modifiers: Headless on a Page and for ctrl or shift on a multi-select list item; else --post or --headed
set-value <window> <ref> <value|on|off|item name>
perform-secondary-action <window> <ref> <action>   # Raise, Scroll Up/Down/Left/Right, Expand, Collapse, Toggle, Select, ContextMenu
type-text <window> [--post | --into <ref>] [--interval-ms <n>] <text>
press-key <window> <key> [--post] [--force]
select-text <window> <ref> <start> <length> | select-text <window> <ref> all
invoke-menu <window> "<menu> > <item>"   # a classic menu bar command, Headless
scroll <window> <ref> <up|down|left|right> [steps] | scroll <window> <ref> into-view
scroll <window> @x,y <dx> <dy> [--post] [--modifiers ctrl]   # Headless: the Page, or the Window's Element under the point; --post posts the wheel
drag <window> @x,y @x,y [--drop-delay <ms>]   # --headed: left button, point to point
drag <window> <element> <element|@x,y> [--drop-delay <ms>]   # a Page Element Headless; other Elements --headed
hover <window> @x,y                 # --headed: moves the pointer and leaves it there, to reveal tooltips
hover <window> <element>            # a Page Element Headless; other Elements --headed, at the clickable point
activate-window <window>            # takes the foreground, no flag needed
restore-window <window> | minimize-window <window> | close-window <window>
move-window <window> <x> <y> | resize-window <window> <width> <height>
maximize-window <window>            # needs --headed: takes the foreground
batch <window>|- [--timeout <s>] [--stop-on-error] [in <window>|new] <action> [args] [then ...]
navigate <window> <url> [--wait load|domcontentloaded|none] | back | forward | reload <window>
wait <window> selector <css> | text <text> | url <url> | load | download [--timeout <s>]
wait <window> <ref> enabled|disabled|gone|value <v>|checked|unchecked|expanded|collapsed [--timeout <s>]
wait window <app|title> [--gone] [--timeout <s>] | wait <ms>
eval <window> <expression> | --file <path|-> [--max <n>] | dialog <window> accept|dismiss [text]
upload <window> [<element>] <file>...
tab list <window> | tab new <window> [url] | tab switch <window> <tab> | tab close <window> <tab>
browser status | launch | connect | stop [browser]
browser use [browser|auto]          # pin a browser (it persists); auto removes the pin
browser clone [browser] [--profile <folder>] [--fresh] [--close-source]
browser ext install [browser|all] [--open] | browser ext status [browser] [--wait <s>]
browser attach <window> | browser detach <window>   # a running Electron App; ask the user first
clipboard-get [--format text|image|files] [--out <path>]
clipboard-set <text> | --image <png> | --file <path>... | clipboard-clear
list-displays | screenshot --display <n>   # --screen <n> is the same
list-notifications [--app <a>] [--text <t>]   # the showing toast banners, numbered
notification-action <n> <button> | notification-dismiss <n>   # Headless; refused if the toast changed
wait notification [--app <a>] [--text <t>] [--timeout <s>]
open-shell start|notifications|quick-settings|overflow   # needs --headed: changes the user's screen
record-tree <window> <file>         # development only, records a tree fixture for winuse's own tests
skill [<topic> | list | all]        # print the skill from the binary: its core, one topic, the topic list, or all
skill install <dir>                 # write the skill folder (SKILL.md and references) to <dir>\winuse
help [<command> | global | all]     # the command index, one command's examples and usage, or everything
status                              # read-only: versions, helper uptime, lock, integrity, DPI, browsers, agents, Windows build
trace show <file>                   # summary of a --trace <file> / WINUSE_TRACE session trace
version
```

`<window>` is a Window ID, or an App name when that App has exactly one
Window; for the Page commands it can also be `<window>:<tab>`, a Tab of
that browser shown or not. `<ref>` is an Element Index, `name:<text>`,
`id:<AutomationId>` or `@x,y`. `<tab>` is a Tab id from `tab list`.
