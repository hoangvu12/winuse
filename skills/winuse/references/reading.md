# Reading a Window

Read when: you need Element References in full (stale indexes, re-found names, points), the tree's details, screenshots and `zoom`, a Chromium tree that stays thin, a slow or partial read, or a big Window (`find`, `--skeleton`, `--root`, `--interactive`, `--compact`, `--max-chars`, `--out`).

This is one topic of the winuse skill. `winuse skill reading` prints it, and `winuse skill` prints the core, SKILL.md, which holds the safety rules, the Esc stop and recovery.

## Read the Window State

```
winuse get-window-state <window>                # the Element tree
winuse get-window-state <window> --screenshot   # the tree, then a Screenshot: line
winuse zoom <window> <x>,<y>,<w>,<h>            # full-resolution PNG crop of the latest screenshot
```

The tree uses the Codex Computer Use format. A header line comes first,
then one line per Element, depth-first and numbered from 0, with one TAB per
depth level.

```
Window: "Example", App: app.exe.
	0 window Example Secondary Actions: Raise
		1 edit (settable, string) Search ID: searchBox
		2 check box Notify me off Secondary Actions: Toggle
		3 button OK ID: okButton

The focused UI element is 1 edit (settable, string) Search ID: searchBox.
```

Each line gives the Element's role, name and state (`disabled`, `selected`,
`expanded`, `settable`, a toggle's `on` or `off`, its `Value:`), its `ID:`,
and the Secondary Actions it offers besides a click. The last line names the
focused Element, where typed text lands. The tree includes the web content
of Electron apps and browser Windows (see `winuse skill browser`).

Start with the tree (a few hundred tokens); take a screenshot (image
tokens) for canvas apps, visual layout questions, or a tree that looks
sparse or stale. When a tree stays nearly empty, a note says so; trust it
and switch to `--screenshot` with `@x,y` points.

Chromium builds its accessibility tree lazily. A thin read of a browser
Window without a Browser Connection, an Electron or a WebView2 app waits up
to 5 s for the tree, once per Window; later thin reads take about half a
second. If it stays thin, a note says Chromium may not build it while the
Window is fully covered: connect the browser (`winuse browser status` says
how), ask the user to uncover the Window, take a screenshot, or use
`launch-app --cdp` for an App you start. winuse never uncovers or
activates the Window itself.

The screenshot comes after the tree as
`Screenshot: <path> (WxH px, scale S, window at X,Y WxH)`: the image's size
and scale, then the Window's frame in physical pixels. `@x,y` points and
`zoom` regions use the image's pixels; `zoom` reads dense UI the scaled
screenshot makes illegible. A new screenshot or a helper restart
invalidates old points.

Screenshots and `zoom` capture the Window even behind other Windows and
leave the foreground alone. They show the Window alone: a menu or popup
outside its frame is a Window of its own, named by the Action's note or
found in `list-windows`. A minimized Window is refused; run
`restore-window` first. `--headed` activates the Window and captures it the
old way, for an app whose headless capture comes out blank.

A slow app can return a partial tree with a note; that is progress, not a
failure. A read has 10 seconds; pass `--timeout <seconds>` (up to 60) for a
slow Electron or Office tree or a cold Chromium page, which also extends
the wait for a thin Chromium tree.

### Dense Windows

A big Window (Outlook, Excel, an IDE, a long page) can fill the tree with
thousands of lines, or cut it at its caps with a note. Narrow the read
instead:

```
winuse find <window> --role button --name save     # search the whole live tree, past the caps
winuse find <window> --text "invoice" --first      # also --state disabled|enabled|selected|expanded|collapsed|settable|on|off|interactive
winuse find <window> --role "list item" --count    # just how many
winuse find <window> --role "list item" --limit 20 # the first 20, and how many matched
winuse get-window-state <window> --skeleton        # three levels, with "(+N children)" where it stops
winuse get-window-state <window> --root 42 --depth 2 --interactive
winuse get-window-state <window> --interactive --compact   # a full read, printing only what you act on
winuse get-window-state <window> --max-chars 20000 # stop the tree before 20000 characters, with a note
winuse get-window-state <window> --out state.txt   # write it to a file, uncut; prints the path and the notes
winuse get-window-state <window> --full            # Codex's text byte for byte: every ID, no cap
```

By default a read, `find` and `--root` stop before 28000 characters
(under the Harness's 30000-character cap on a command's output) with a
note naming the first Element not printed, and leave out the IDs two or
more Elements share, which `id:` refuses as ambiguous; a unique ID always
prints. `--max-chars N` sets the cap, `--max-chars 0` lifts it, and
`--out <file>` writes the whole read to the file.

On a full read, `--interactive`, `--depth N` and `--compact` (folds
unnamed containers) print less, but the Window State is kept whole:
every index printed is its own, and the ones left out still work.

`--role` takes the role as the tree prints it, and `--name` and `--text` a
part of the name (or the value and description), ignoring case. A role
that matches nothing lists the roles the Window shows. Start a dense
Window with `--skeleton`, then read the part you need with `--root`.

`find` and `--root` number the Elements they print after the Window
State's own (if the tree ended at 240, `find` prints 241, 242...), and the
earlier indexes keep working. The next full read, `--skeleton` and an
Action's `--observe` included, renumbers from 0. In a browser Window with a Browser Connection, `find`
reads the Window State afresh and says so; use its indexes.

## Name an Element

An Element Reference takes one of four forms.

- The Element Index the tree printed, as in `winuse click 101 3`.
- `name:<text>`, the name exactly as the tree printed it, as in
  `winuse click 101 name:OK`. Quote a name with spaces:
  `winuse click 101 "name:Save As"`.
- `id:<AutomationId>`, the value after `ID:`, as in
  `winuse click 101 id:okButton`.
- `@x,y`, a point in the latest screenshot's pixels, as in
  `winuse click 101 @412,88`. A left click, `set-value`,
  `perform-secondary-action` and `scroll` act Headless on the Element under
  the point, even while another Window covers it, and a note after `ok`
  names it: `Note: clicked element 5 button "Save" at @412,88`. If it is not the
  Element you meant, name the Element instead. Drag, hover and double,
  right or middle clicks aim at the point itself, with `--headed`.

Matching is exact and case-sensitive. A name or ID that matches several
Elements fails and lists the candidates.

An Element Index and an `@x,y` point are valid only for the Window State
and screenshot that printed them; a stale index fails and asks for a fresh
read. A `name:` or `id:` reference outlasts them: when the Window State you
read cannot answer it (the helper restarted, the Window changed, the
Element went away), winuse looks for it in a fresh read and acts on what it
finds, saying
`Note: found name:Save in a fresh read of window 101, as Element Index 245`;
your Window State's indexes still work. Prefer `name:` and `id:` for an
Element you will act on again. A fresh read that finds the name twice
refuses, with the candidates under new indexes.

An Action's `--observe` is a full read too, so it renumbers the same way;
its diff lists every index whose line changed, and an index it does not
list names an Element with the same line as before, so you can keep using
it. An index it lists as gone (`-`) is stale (topic `batch-and-wait`).
