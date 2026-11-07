# Fields, keys, text, menus, scrolling and the clipboard

Read when: typing is refused or not verified, combo boxes and lists, keys and chords, `type-text --into`, `select-text`, Posted Keys, `--interval-ms`, `invoke-menu`, scrolling, or the clipboard.

This is one topic of the winuse skill. `winuse skill input` prints it, and `winuse skill` prints the core, SKILL.md, which holds the safety rules, the Esc stop and recovery.

## Fill in fields, keys and text

`set-value` fills a `settable` field in one Headless Action and reads the
value back; sliders take numbers. A toggle without a value takes `on` or
`off` (also `true`, `false`, `checked`, `unchecked`): winuse toggles only
when the state differs, so the box ends where you asked.

The value is everything after the Element, so put flags before the
Element: `set-value 101 --observe id:name Ada`. A value whose last
arguments are winuse flags (`--observe`, `--headed`, `--no-wait`,
`--timeout 5`, ...) is refused with nothing sent, and the hint shows the
line with them moved. A flag word in the middle of the value, or inside
one quoted argument (`"Ada --observe"`), is text; for a value that really
ends with one, put `--` before the value: `set-value 101 id:name -- Ada
--observe`.

On a combo box or list, `set-value` picks the item you name, Headless, by
its exact name, else the same name in any case and spacing, and reads the
selection back. winuse opens a closed combo only when it shows no items
until opened, and closes it again. An editable combo takes the text as its
value. A name that matches several items, or none, is refused with the
names; nothing was sent unless winuse opened the combo to search it, and
then the error says it was opened and closed.

- `type-text` types literal Unicode text, any script and emoji included,
  into the focused Element.
- `press-key` sends control keys and chords by name, such as `Return`,
  `Tab`, `Escape`, `Down`, `Control_L+a`, `Control_L+Shift_L+period` and
  `Numpad_0`. Send every control key through `press-key`, never `type-text`.
- `press-key` refuses Alt+F4, Alt+Tab and Ctrl+Shift+Escape, and any chord
  holding one (such as `Control_L+Alt_L+F4`), with nothing sent; use
  `close-window` or `activate-window`. Pass `--force` (in a Batch, on that
  step) only when you mean the combo itself. The Windows key is sent only
  by `--headed open-shell notifications|quick-settings`.

winuse picks the route from the live Window before it sends anything.

- `press-key` Return (or KP_Enter) or Escape, alone, on a classic Win32
  dialog (message boxes, most Open and Save As dialogs) is Headless: Return
  presses the focused push button or the default button, Escape the Cancel
  button. Return has no Headless route without a default button, or in a
  multi-line box that takes it as a new line.
- `press-key` with Control, Shift or Alt, such as `Control_L+s`, first looks
  for the menu item or ribbon button the Window labels with that shortcut.
  One match runs Headless and is named after `ok`, as in
  `Note: invoked "File > Save" through its shortcut`; a disabled one is
  refused with nothing sent. WinForms, WinUI, ribbons and classic menus
  label shortcuts; WPF never does. With no match, or two, the chord goes
  on to the routes below.
- `type-text` into a focused classic edit box (Notepad, a Win32 dialog's
  text box) is Headless when the text has no newline, tab or other control
  character. It replaces the selection or inserts at the caret, and winuse
  checks the length afterwards.
- In a browser Window with a Browser Connection, both go to the Page
  Headless while a Page Element has the focus.
- In other Chromium browser or Electron Windows, both go as window
  messages without the foreground: `Note: typed as window messages; ...`.
  The page sees a focus and blur around each step. Typed text is read back
  where the field has a value; otherwise the output says it was not
  verified. Click into the field first with
  `click <window> <ref> --count 2`, also sent as window messages. A
  minimized Window, a browser the user is working in, and WebView2 are
  refused with nothing sent.
- Everywhere else, WinUI and WPF included, both need `--headed`, which
  types into whatever has keyboard focus. The refusal's hint names the
  `--headed` command, `--into` for text (Headless, and the way into a
  WinUI or WPF text box) and `--post`.

Before a Headed type, check the focused-Element line. If focus is
elsewhere, click the field with `--headed` or at its `@x,y` point, confirm
focus in a fresh read, then type; documents and canvases too. Some Windows
report no focused control while in the background (a covered Windows 11
Notepad): plain `type-text` is refused there and points at `--into`.

### Type into a named field

```
winuse type-text <window> --into <ref> <text>
```

`--into` goes right after the Window and takes an Element Index, `name:` or
`id:` (not `@x,y`). winuse types into that Element, whatever has the focus,
and picks the route from the Element.

- A classic edit box is Headless when the text has no control character,
  as for focused typing above. Name `Text editor` to type into a covered
  Windows 11 Notepad.
- A Page field with a Browser Connection is Headless: winuse focuses it
  and types, a newline as a real Return and a tab as a real Tab.
- Another field with a text caret, such as a WinUI or WPF text box, is
  Headless when the text has no control character: winuse rewrites the
  value with the text at the caret or over the selection, reads it back
  and moves the caret after it. No key events fire, so typing handlers
  and some validators do not run and undo may not hold it; the output says
  so. Place the caret first with `select-text <ref> <n> 0` when it matters.
- Anything else is refused with nothing sent: a password box, a read-only
  or rich text field, a field with no caret, and control characters
  outside a Page. The hint gives the `--headed` retry, which clicks the
  Element and types with real keys.

`--into` works on a Batch step too (`type-text --into id:1001 hello`), but
not with `--post`.

### Select text

```
winuse select-text <window> <ref> <start> <length>   # length characters from start, counting from 0
winuse select-text <window> <ref> all
```

`select-text` selects text in a field without keys or the mouse, always
Headless, by Element Index, `name:` or `id:` (not `@x,y`), and reads the
selection back. Offsets count UTF-16 units (an emoji counts two); a length
of 0 puts the caret at the start.

- In a classic edit box, a `type-text` afterwards replaces the selection
  Headless, so the pair replaces a word without the foreground; where the
  box is not focused (a covered Windows 11 Notepad), use `type-text --into`
  on the same Element. In a Page field winuse focuses the field too.
- Elsewhere it selects through UI Automation and leaves keyboard focus
  alone; check the focused-Element line before you type.
- A span past the end, or an Element with no text, is refused with nothing
  sent.

### Posted Keys

`press-key <window> <key> --post` and `type-text <window> --post <text>`
post the key or text to the focused control of a background Window, with no
foreground change and no real input (in `type-text`, `--post` goes right
after the Window). Posting is best effort.

- Some apps ignore posted keys, and winuse cannot tell. Success prints `ok`
  and `Note: posted; the app may ignore posted keys, read the Window State to check`.
  Confirm the effect in the Window State; if nothing changed, send the
  command again without `--post`.
- A chord with Control, Shift or Alt, such as `Control_L+s` or `Alt_L+f`,
  is posted with its modifiers held for that Window's thread only, which
  reaches shortcuts and menus in classic Win32, WinForms, WPF and browser
  Windows. Control and Alt together, modifiers alone and the Blocked Combos
  are refused with nothing sent, even with `--force`, and so is a chord to
  a Window whose App is in front: use `--headed` there.
- An administrator, minimized or modal-blocked Window and a locked desktop
  are refused with nothing sent. `--post` and `--headed` do not combine.

Some Apps drop characters typed fast. `type-text <window> --interval-ms 50
<text>` pauses that many milliseconds (0 to 1000) between characters typed
key by key, with `--headed` or `--post`; it sits right after the Window
with `--post` or `--into`. A route that puts the text in at once ignores
it, and a `Note:` after `ok` says so.

## Run a menu command

```
winuse invoke-menu <window> "File > Save As..."
```

`invoke-menu` runs a command of a classic Win32 menu bar (older Windows
tools, Win32 and MFC Apps and their dialogs) without opening the menu,
always Headless. Name one item per level, separated by `>`, and quote the
path, because PowerShell reads a bare `>` as a redirect. When the Window
State shows a menu bar, try `invoke-menu` first.

- Names match without case, `&` markers or the shortcut, and a trailing
  `...` is optional, so `"file > save as"` finds `Save &As...`.
- A greyed item, a name that matches no item or several, a path that ends
  at a submenu, and a Window with no classic menu bar are refused with
  nothing sent. A classic submenu's items are missing from the Window
  State; the failure lists the items at that level.
- `ok` means the command was sent; read the Window State for its effect.
  A dialog it opens is a new Window.
- WinUI, WPF, WinForms, Office ribbons and browsers draw their own menus,
  which `invoke-menu` refuses; click their menu Elements by `name:`.

## Scroll

```
winuse scroll <window> <ref> <up|down|left|right> [steps]   # steps 1 to 50, default 3
winuse scroll <window> <ref> into-view
winuse scroll <window> @x,y <dx> <dy>    # 120 per notch, positive dy down, positive dx right
```

At a point, winuse scrolls Headless up to 16 notches per axis; more is
refused with nothing sent, so split it or pass `--headed` to turn the real
wheel there.

By Element, name anything inside the list or page; winuse scrolls the
nearest container above it that scrolls on that axis and reads the
position back. `ok` means it moved, even partly. "the Element did not
scroll; it may already be at the end" means nothing moved. `into-view`
scrolls the Element itself into view and checks it is on screen. When
nothing scrolls it Headless, nothing was sent; use
`winuse --headed scroll <window> <ref> <direction>`, which takes the
foreground.

## Clipboard

```
winuse clipboard-get                                  # the clipboard's text
winuse clipboard-get --format files                   # copied files, one path per line
winuse clipboard-get --format image [--out <path>]    # saves the image as a PNG, prints its path and size
winuse clipboard-set <text>                           # or --image <png>, or --file <path>...
winuse clipboard-clear
```

The clipboard commands are Desktop commands: they name no Window, need no
foreground, show no Agent Cursor and are not Batch steps. `clipboard-get`
prints the text exactly, with no newline added; without `--out` an image
goes to a new file in `%TEMP%\winuse`. When the clipboard holds nothing in
the format you asked for, the hint says what it holds.

`clipboard-set` replaces the clipboard with one thing: its arguments as
literal text joined by spaces, like `type-text`, or a PNG file, or a list
of existing files (paths from your working directory). For a long text in
a field with no Headless route, `clipboard-set` it, then paste with
`press-key <window> Control_L+v`, which takes the key routes above.

A clipboard whose owner stopped responding is refused with nothing sent;
wait until that program responds, then retry.
