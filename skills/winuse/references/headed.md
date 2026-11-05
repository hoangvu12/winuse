# Headless and Headed

Read when: before your first `--headed`, to understand a refusal, to keep the user's foreground, or to plan a step with no background route (drag, hover, games, canvases, maximize, the shell).

This is one topic of the winuse skill. `winuse skill headed` prints it, and `winuse skill` prints the core, SKILL.md, which holds the safety rules, the Esc stop and recovery.

## Choose Headless or Headed

A Headless Action leaves the user's pointer, keyboard and foreground
untouched, and the Window can stay behind their work. These are Headless by
default:

- By Element or at an `@x,y` point: a left single `click`, `set-value`,
  `perform-secondary-action` except Raise, and `scroll`; by Element also
  `select-text`, which has no Headed form. At a point winuse picks the
  smallest Element there, or the nearest one around it that can take the
  Action; a point on nothing but the Window, on a canvas, or on two
  overlapping Elements that act differently is refused with nothing sent.
  A scroll scrolls the list or page under the point, three steps per
  wheel notch.
- `invoke-menu`, a command of a classic menu bar (no Headed form).
- Every Window Action but `maximize-window`: `restore-window`,
  `minimize-window`, `move-window`, `resize-window`, `close-window`, and
  `close-app`, which closes every Window of an App.
- The key and text routes of "Fill in fields, keys and text"
  (`winuse skill input`), and every Page Action through a Browser
  Connection ("Browser Windows", `winuse skill browser`).

Every other Action is Headed and runs only with `--headed`: winuse brings
the Window to the foreground, uses the real pointer and keyboard, and
frames the Window with a banner saying winuse is in control and Esc stops
it. That covers a point that names no usable Element, `--count 2|3` and
`--button right|middle` outside a Chromium or Electron Window (there they
go as window messages), `drag`, `hover`, `maximize-window`, Raise, and keys
and text no Headless route reaches. Without `--headed` these are refused
with a reason ending in `nothing was sent`, and the hint gives the exact
`--headed` command, with `--post` or `--into` where they fit.
`activate-window` alone takes the foreground without the flag; a Headed
Action activates its Window itself, so use it only to restore drifted
focus. A Headed Action waits while the user types or clicks (past 3 s it is
refused with `nothing was sent`), stops when they take over (exit 130, like
Esc: ask before retrying), and then gives back their foreground and pointer,
unless it left a menu or dialog open, which a `Note:` names. After a right
click it waits up to 2.5 s for a late context menu (Explorer's) first.

The user sees each Headless Action through the Agent Cursor, a second cursor
that glides to the Element with a label such as `click "Save"` and presses
on a click. It sits just above its Window, leaves the user's pointer alone,
never appears in your screenshots, and needs nothing from you.

A Headless click uses the Element's first pattern among Invoke, Toggle,
Select and Expand/Collapse, else its default action. Toggle, Select,
Expand, Collapse and a text set-value are read back, so `ok` means the
Element shows the new state; when a Toggle, Expand or Collapse changes
nothing, winuse tries Invoke once. An Invoke or default action has no
read-back, so check the next Window State.

`--headed`, as in `winuse --headed click 101 3`, forces the Headed way; on a
Batch it makes every step Headed. It goes before the command or anywhere
after it, except inside `type-text`'s text, `set-value`'s value and
`clipboard-set`'s text, where it is literal text; put it first there. Use
it when a refusal's hint offers it and the step needs the real pointer or
keyboard, when a Headless Action had no effect (custom-drawn controls and
some web widgets respond only to a real click), or when you need keyboard
focus in a field: a Headless click never moves focus. winuse never
switches modes by itself: taking the user's foreground is always your
decision, made with `--headed`.

### Keep the user's foreground

Pick the Headless way whenever one does the job.

- `set-value` a `settable` field, a check box to `on` or `off`, or a combo
  box or list item by name, rather than clicking and typing.
- Confirm or cancel a dialog with a Headless click on its button by `name:`.
- Run a classic menu bar command with `invoke-menu`; open other menus,
  trees and combo boxes with a click or
  `perform-secondary-action <ref> Expand`.
- Name the Element rather than an `@x,y` point when the tree has it, so
  the Action cannot land on a neighbour.
- Show, hide, place and close Windows with the Window Actions rather than
  `activate-window`.
- In File Explorer a click opens the item: a folder in place, a file in its
  App. Select files with `perform-secondary-action <ref> Select` (add more
  with `click <ref> --modifiers ctrl`).

### What winuse cannot do headless

These have no background route; plan for `--headed` (or `--post` where it
fits) from the start. Without it they are refused with nothing sent.

- Hover and drag in a desktop App (only a Page with a Browser Connection
  takes them Headless). With `--headed` they take an Element as well as
  `@x,y`: `winuse --headed drag 101 id:card id:done` drags between the
  Elements' clickable points, and `--drop-delay <ms>` holds the drag
  before the drop, for a target that needs hover time.
- A minimized browser, Electron App or Store app: run `restore-window`
  first. A Store app such as Calculator has no UI while minimized and comes
  to the front when restored, so tell the user first. Other minimized
  Windows still take `click`, `set-value`, `perform-secondary-action` and
  `select-text` by Element; scrolling, keys, `invoke-menu` and screenshots
  need `restore-window`.
- A WPF App's right-click menu (other Apps' open Headless with
  `perform-secondary-action <ref> ContextMenu`, which names the menu, up
  to 8 s later on File Explorer; `click --button right --post` posts one
  to a classic control).
- The shell: opening Start, the hidden icons or the notification panel
  (`--headed open-shell`), and any click on it but a toast's buttons (see
  "The taskbar and the shell", `winuse skill shell`).
- Games, canvases and Apps that read raw input: they expose no Elements
  and ignore posted keys, so use `--headed` with `@x,y` points.
- An administrator Window, in any mode (see "Refusals and recovery" in `winuse skill troubleshooting`).

`--post` fits a single key or plain text, or a click or wheel scroll on a
classic control, when the user's foreground matters more than certainty;
read the Window State afterwards. `--headed` fits the rest and any step
that must work the first time; it takes the foreground, so tell the user
first if they are working in another Window.
