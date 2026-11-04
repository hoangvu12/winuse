# Tabs

Read when: you work in the user's browser (always in a Tab of your own, `<window>:<tab>`), or list, open, switch or close Tabs.

This is one topic of the winuse skill. `winuse skill tabs` prints it, and `winuse skill` prints the core, SKILL.md, which holds the safety rules, the Esc stop and recovery.

### Tabs

```
winuse tab list <window>            # * marks the active Tab, then its id, title and URL
winuse tab new <window> [url]       # open a Tab in the background and print its id
winuse tab switch <window> <tab>
winuse tab close <window> <tab>
```

`<window>` alone means the Tab the Window shows. To work in another Tab,
name it as `<window>:<tab>`: the Window, a colon and the Tab's id. The
user keeps looking at their own Tab while you work in yours, and several
agents can each work in their own Tab of one window at once. In the
user's own browser, always open your own Tab and work there:

```
winuse tab new 4242 https://example.com      # ok 1a2b3c4d
winuse get-window-state 4242:1a2b3c4d         # the Tab's own Window State and Element Indexes
winuse click 4242:1a2b3c4d 7
winuse set-value 4242:1a2b3c4d 9 "Blue"
winuse wait 4242:1a2b3c4d text "Saved"
```

- Never `tab switch` in the user's window to do your work: it changes the
  Tab they are looking at. Use it only to show the user something.
- A Tab target takes `get-window-state` (`--screenshot` too), `click`,
  `set-value`, `scroll` and `hover` on an Element, `drag` from an Element
  to an Element, `select-text`, `type-text` (with or without `--into`),
  `press-key`, `upload`, `navigate`, `back`, `forward`, `reload`,
  `dialog`, the Page `wait`s, `eval` and `batch`. Everything is
  Headless; `--headed`, `--post`, `@x,y` points, `find`, `zoom` and the
  other commands are refused with nothing sent.
- Its Element Indexes are yours for that Tab only; read it with
  `get-window-state <window>:<tab>` before you act, and again after the
  Page changes.
- Keys and text go to the Tab's focused element, so click a field first.
- A click on a drop-down list (`<select>`) or a date, time or colour field
  in a background Tab is refused: its popup would open on the user's
  screen. Use `set-value` (a date as `2026-11-04`, a time as `13:30`, a
  colour as `#ff8800`).
- A link or script that opens a new Tab opens it beside the user's Tab
  without taking its place (through the extension), and the `Note:` line
  names its id.
- A screenshot of a background Tab may not come; the output then says so
  and the tree is still current.
- The Agent Cursor does not show Actions in a background Tab.

Through the extension your new Tabs gather in a tab group named after you
(`WINUSE_AGENT`, else your Harness's name) in your Agent Cursor's colour. These are
Headless. On a browser winuse launched or cloned, `tab switch` selects the
Tab in the tab strip by its title; when two Tabs share a title, or the
strip does not show it, it sends nothing and prints the `--headed` retry.
A tab strip that "did not answer" means the browser's UI Automation
failed; after a download Chrome can stay that way, its Window State
failing too, so use the `--headed` retry or work through the Page.
In the user's own browser, work in Tabs you opened and close only those.
winuse refuses to close a Window's last Tab; use `close-window`.
