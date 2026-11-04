# Browser Windows

Read when: any website, browser Window or Electron or WebView2 App: connecting to the user's browser, `navigate`, Page waits, `eval`, uploads, downloads, JavaScript dialogs, `browser attach`.

This is one topic of the winuse skill. `winuse skill browser` prints it, and `winuse skill` prints the core, SKILL.md, which holds the safety rules, the Esc stop and recovery.

## Browser Windows

A Window of Chrome, Edge, Brave, Chromium, Vivaldi or Opera that has a
Browser Connection has one Window State: the frame (tab strip, address bar,
menus, infobars) through UI Automation and the active Tab's Page through
the browser, numbered together, so act on a Page Element like any other.
The Page is read whole even while the Window is covered, and every Action
on a Page Element is Headless, whatever the button or click count. A click
on a Page field focuses it for `type-text` and `press-key`. `--headed`
still uses the real pointer.

Without a Browser Connection, the Window State ends with a line such as
`Note: the Page is read through UI Automation because no Browser Connection
is open to Chrome; run winuse browser status chrome.` Keys and typing then
go as window messages (see "Fill in fields, keys and text" in `winuse skill input`), and the Page
commands below fail; run the command the note names. Other notes name a Tab that did not match the Window, or a
JavaScript dialog to answer first.

### Get a Browser Connection

Run `winuse browser status` first. On exit 0 it prints how winuse is
connected; otherwise one sentence on what is wrong and the command to run
next. With several browsers and no pin, winuse refuses to guess: ask the
user which, then run `winuse browser use <browser>`, which persists. When
the user names a browser that is not running ("my Chrome") but another one
is (Brave), say so and use the running one only if they agree.

Always work in the user's own browser and profile, with their logins and
tabs, in this order:

1. **The winuse extension.** `winuse browser ext install <browser> --open`
   registers winuse and opens the browser's extensions page and an Explorer
   window with the extension folder. Before you run it, tell the user the
   steps it prints (they cannot see your terminal): turn on "Developer
   mode", then drag the folder onto the page or click "Load unpacked" and
   choose it. Then `winuse browser ext status <browser> --wait 120` waits
   until it connects (exit 0), or prints what is still missing.
   If the user asks you to do it, do it with winuse in that Window:
   `find <window> --name "Load unpacked"`, click it, then in the folder
   dialog `set-value <dialog> id:1152 "<folder>"` and click `id:1`
   (Select Folder). Turn on Developer mode the same way when it is off.
2. **`winuse browser connect <browser>`**, when the extension cannot be
   loaded (a policy blocks unpacked extensions, or the user declines it).
   It opens `<browser>://inspect/#remote-debugging`. Turn on "Allow remote
   debugging for this browser instance" there (`find` it, then `set-value
   <window> <element> on`). Chrome 144+ then shows an "Allow remote
   debugging?" prompt for the connection: `connect` waits 20 seconds and
   says so. The user asked you to use their browser, so allow it
   yourself rather than stopping: `winuse list-windows --app <browser>`
   finds the prompt's Window, then `winuse click <window> name:Allow`. If
   that is refused, retry with `--headed`. If the prompt still ignores it,
   ask the user to click Allow; never work around the prompt otherwise.
   Then `winuse browser status <browser>`.

Only when the user asks for a separate browser, or after both routes
failed (tell the user which and why), use a browser of winuse's own:

- `winuse browser launch [browser]` starts it with winuse's own profile.
- `winuse browser clone [browser]` launches a copy of the user's profile;
  logins copy only while the user's browser is closed. `--close-source`
  closes it and reopens its tabs in the clone, so ask the user first.

`launch` and `clone` open a Window without taking the foreground; if the
browser takes it anyway, `launch` ends its line with
`(the browser took the foreground)`. Find the Window with `list-windows`;
it keeps rendering while covered. When the job is done,
`winuse browser stop [browser]` closes a browser winuse launched or cloned
and keeps its profile.

A running Electron App (Slack, Discord, VS Code) can get a Browser
Connection without a restart: `winuse browser attach <window>` opens the
App's Node.js inspector and runs winuse's code inside it. Ask the user
first, and attach only after they agree. An App that turned its inspector
off, as Discord does, is refused with nothing sent; it stays on UI
Automation, or, if the user agrees to restart it, relaunch it with
`launch-app <exe path> --cdp`. While attached, any program could reach
the inspector, so run `winuse browser detach <window>` when done (the
helper also closes it when it exits).

Through the extension, the browser shows a "started debugging this
browser" bar; it is expected, so never press its Cancel. When the user
turns winuse off with that Cancel or the winuse toolbar icon, commands say
so: stop and tell the user, and switch routes only when they ask.

The Agent Cursor shows Page Actions too: it glides to the Page Element,
presses when the input reaches the Page, and stays just above the browser
Window, so the user sees it only while that Window is uncovered.

### Act on a Page

```
winuse navigate <window> <url>                 # open a URL in the Window's Tab (or <window>:<tab>'s), wait for the load, print the URL
winuse back <window>                           # also forward and reload
winuse wait <window> selector <css>            # also text <text>, url <url or *pattern*>, load
winuse eval <window> <expression>              # run JavaScript in the Page, print the value
winuse eval <window> --file x.js [--max 20000] # the expression from a file (- for stdin); a longer cut
winuse dialog <window> accept|dismiss [text]
winuse upload <window> [<element>] <file>...
winuse hover <window> <element>
winuse drag <window> <element> <element|@x,y>
```

- Open a URL with `navigate`; keys sent into the address bar do nothing
  Headless.
- After an Action that loads or changes the Page, `wait` for what you
  expect (10 seconds by default, `--timeout <seconds>` up to 300), then
  read the Window State. A Page Element goes stale when the Page navigates
  or the node goes away.
- `set-value` fills a text field, picks a list option by label or value,
  and turns a check box, switch or radio button `on` or `off` (turn another
  radio button on to turn one off).
- A covered, disabled, hidden or off-screen Page Element is refused with
  nothing sent, naming any covering Element, such as a cookie banner.
- A click or key that opens a JavaScript dialog (alert, confirm, prompt,
  leave-this-page) prints `ok` and a `Note:` line with its text; do not
  repeat it. Every Page Action then fails naming the dialog until you
  answer it with `dialog` (a prompt's text goes after `accept`).
- A click or key that opens a new Tab adds
  `Note: a new Tab opened: <tab> "<title>" <url>`; read and act on it as
  `<window>:<tab>` (see `winuse skill tabs`).
- A click that opens a file chooser says winuse held it back, so no file
  dialog opens; answer it with `winuse upload <window> <file>...`. A
  browser winuse launched or cloned also holds back a chooser opened up to
  2 seconds after a click that printed only `ok`, so run the same upload
  (it says so when no chooser waits). Through the extension, or later, the
  Open dialog appears as a new Window. Best is giving the files to the
  page's file input: `winuse upload <window> <element> <file>`.
- A click that starts a download says so, and
  `winuse wait <window> download` prints the file's path when done.
  Downloads land in `Downloads\winuse` (shown by `winuse browser status`),
  only on a browser winuse launched or cloned.
- `eval` prints a string as is and anything else as JSON, cut at 4000
  characters; `--max <n>` raises the cut up to 100000, so do not page a
  long result with several evals. Return plain values, not DOM nodes.
  PowerShell 5.1 strips double quotes inside arguments: for anything
  longer than a line, write the expression to a file and run
  `winuse eval <window> --file x.js`, where no quoting applies. A one-liner
  can single-quote the expression and use backticks for JavaScript strings:
  ``winuse eval 101 'document.querySelector(`h1`).innerText'``.
