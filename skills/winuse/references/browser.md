# Browser Windows

Read when: any website, browser Window or Electron or WebView2 App: connecting to the user's browser, `navigate`, Page waits, `eval`, `extract`, uploads, downloads, JavaScript dialogs, `browser attach`.

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
   registers winuse and opens an Explorer window with the extension folder.
   It cannot open the browser's extensions page (browsers ignore their own
   pages on the command line). Before you run it, tell the user the steps
   it prints (they cannot see your terminal): open
   `<browser>://extensions`, turn on "Developer mode", then drag the folder
   onto the page or click "Load unpacked" and choose it. Then `winuse browser ext status <browser> --wait 120` waits
   until it connects (exit 0), or prints what is still missing.
   If the user asks you to do it, do it with winuse in that Window:
   `find <window> --name "Load unpacked"`, click it, then in the folder
   dialog `set-value <dialog> id:1152 "<folder>"` and click `id:1`
   (Select Folder). Turn on Developer mode the same way when it is off.
2. **`winuse browser connect <browser>`**, when the extension cannot be
   loaded (a policy blocks unpacked extensions, or the user declines it).
   winuse cannot open `<browser>://inspect/#remote-debugging` (the browser
   ignores its own pages on the command line), and `connect` starts
   nothing: open it yourself, once, in a window of your own, never in one
   of the user's tabs. `winuse launch-app "<browser exe>"` opens a new
   window; `set-value` its address bar to the inspect address and
   `press-key Return`. Turn on "Allow remote debugging for this browser
   instance" there with `perform-secondary-action <window>
   id:remote-debugging-enabled Toggle`, then run `connect`. A `connect` while
   a window shows the page waits for the toggle; without one it fails at
   once, and its hint says all this. Chrome 144+ then shows an "Allow remote
   debugging?" prompt for the connection, and winuse presses its Allow
   itself, Headless, while its own connection waits: you do nothing. Only
   when `connect` still says the browser is waiting for approval (the
   press did not take): `winuse list-windows --app <browser>` finds the
   prompt's Window, then `winuse click <window> name:Allow`, `--headed`
   if that is refused. If the prompt still ignores it, ask the user to
   click Allow; never work around the prompt otherwise. Then
   `winuse browser status <browser>`.

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

### Read a Page's lists and tables as data

```
winuse extract <window>                        # the Page's main list, table or JSON-LD as JSON rows
winuse extract <window> --root <element>       # the list, an ancestor of it, or one item of it
winuse extract <window> --as table|list|jsonld # force a mode; --limit N (default 50), --out rows.json
```

- Use `extract` instead of `eval` or reading the Window State when you
  want the rows of search results, a product grid, a table or a feed. The
  first line names the container and the count, then a JSON array, one
  row per line:
  `list: <ol id="results"> "Search results": 10 of 10 rows (complete)`.
  `(partial: capped at 50 / page says 1,234 results)` says rows are
  missing: raise `--limit`, or the page itself shows only some (load more
  or go to its next page).
- Without `--root` it picks the container itself. If that is the wrong
  one, read `get-window-state` and pass `--root` the Element Index of the
  list, of an ancestor, or of one item; one item gathers its alike
  siblings. A table with headers prints its cells by header.
- An article or a page with no repeated items prints `no list found`;
  read it instead. A block page prints
  `no list found: the Page looks like a <Cloudflare|...> wall; ask the user to handle it`:
  stop and ask the user, as the safety rules say.
- It only reads, in an isolated world the Page cannot see, and prints no
  Element Indexes. It takes `<window>:<tab>`. Pages only: on a native
  Window, use `get-window-state` or `find`.

### Act on a Page

```
winuse navigate <window> <url>                 # open a URL in the Window's Tab (or <window>:<tab>'s), wait for the load, print the URL
winuse back <window>                           # also forward and reload
winuse wait <window> selector <css>            # also text <text>, url <url or *pattern*>, load
winuse wait <window> selector .spinner --gone  # until it is gone (selector and text take --gone)
winuse wait <window> idle                      # until the Page settles: no DOM change, no request finishing
winuse wait <window> js "window.app?.ready"    # until the expression is true (a promise is awaited)
winuse read <window> [--root <element>]        # the Page's main content (or one Element's) as Markdown
winuse eval <window> <expression>              # run JavaScript in the Page, print the value
winuse eval <window> --file x.js [--max 20000] # the expression from a file (- for stdin); a longer cut
winuse dialog <window> accept|dismiss [text]
winuse upload <window> [<element>] <file>...
winuse hover <window> <element>
winuse drag <window> <element> <element|@x,y>
```

- Open a URL with `navigate`; keys sent into the address bar do nothing
  Headless.
- `Note: the Page answered HTTP 404 Not Found` (or 429, 503 ...) after
  `navigate`, `back`, `forward` or `reload` means the Page you see is the
  site's error page, not the content: the navigation still says `ok`.
  `with an empty body; Chrome shows its error page` means the Tab shows
  Chrome's own page for it. On a 429 or 503, wait before you retry, and
  never in a tight loop.
- To load pages faster in a Tab of your own, block their images, fonts
  and media: `winuse tab new <window> <url> --block images,fonts,media`,
  or `winuse navigate <window>:<tab> <url> --block images` on a Tab you
  opened (any Tab of a browser winuse launched or cloned); `--block none`
  turns it off. The text and alt text stay, so reads are the same. It
  matches file extensions, so images whose URLs have none still load.
  On the user's own Tabs it is refused. More in topic `tabs`.
- After an Action that loads or changes the Page, `wait` for what you
  expect (10 seconds by default, `--timeout <seconds>` up to 300), then
  read the Window State. A Page Element goes stale when the Page navigates
  or the node goes away. `--observe` already waits up to 1 s for the Page
  to settle before it reads, and says so in a `Note:` when the Page was
  still changing; wait only for what takes longer.
- `wait text` reads the Page's rendered text, its open shadow roots' and
  its same-origin frames'. `wait js` takes any expression: a syntax error
  fails at once, and a timeout names the last error it threw. Prefer
  `selector` or `text` when they say it: they need no script of yours.
- `set-value` fills a text field, picks a list option by label or value,
  and turns a check box, switch or radio button `on` or `off` (turn another
  radio button on to turn one off).
- `set-value` also picks an option of a custom combobox or listbox (Radix,
  Headless UI, MUI, React Select) by its name, exactly or in any case, in
  one call: name the combobox, its button or the listbox. winuse opens it
  unless it is open, clicks the option and reads the choice back. A
  combobox you type into gets the text first, and the option of that name
  is then clicked; with no option of that name it keeps the text (a note
  lists the options). An option that is not there, or several of one name,
  fails listing the options; a list winuse opened stays open, so retry
  with the right name. Use `type-text` to type into a search box without
  picking a suggestion.
- A covered, disabled, hidden or off-screen Page Element is refused with
  nothing sent, naming any covering Element, such as a cookie banner.
- A Page `click` or `press-key` watches the Page until it settles (up to
  1 s) and says what it saw, still with `ok` since the input was delivered:
  `Note: the Page navigated to <url>`; `Note: the Page is navigating to
  <url> and had not loaded when winuse looked` (then `wait <window> load`);
  `Note: the Page changed (text only)`; or `Note: no change seen in the
  Page; the click was delivered, so read the Page before repeating it`,
  which names an Element that now covers the click point or is gone. Do not
  repeat a click on that note alone: read the Page first, because a late
  reaction or a repeat can act twice. Other changes print no note. Under
  `--observe` only the navigation note prints; the diff shows the rest.
  `--json` gives the same as an `effect` field (`navigated`, `changed`,
  `text`, `none` or `unknown`).
- A click or key that opens a JavaScript dialog (alert, confirm, prompt,
  leave-this-page) prints `ok` and a `Note:` line with its text; do not
  repeat it. Every Page Action then fails naming the dialog until you
  answer it with `dialog` (a prompt's text goes after `accept`).
- In a web app, Escape can open a "discard changes?" prompt inside the
  Page instead of closing what you meant. Close a dialog, panel or menu
  with its own close or Cancel control (`click <window> name:Cancel`).
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
- To read a Page's text (an article, docs, search results, a table), use
  `read`, not `eval` with `innerText`. It prints the main content as
  Markdown with headings, lists, links and tables with their headers,
  without navigation, footers or ads, and leaves your Element Indexes as
  they are. The first line names what it picked; when that is the wrong
  part, pass `--root <element>` (an Element Index, `name:` or `id:` from
  your latest read) to read that Element's subtree. Links on the same site
  print as paths and others as a shortened domain and path; `--urls full`
  prints them whole. It is cut at 28000 characters with a
  `(cut at N of M characters)` line: `--max-chars N` (0 for no cap), or
  `--out <file>` to write it whole. A short Page that looks like a
  CAPTCHA or block wall adds
  `Note: the Page looks like a <kind> wall; ask the user to handle it`.
  On a native Window, read with `get-window-state` instead.
- `eval` prints a string as is and anything else as JSON, cut at 4000
  characters; `--max <n>` raises the cut up to 100000, so do not page a
  long result with several evals. Return plain values, not DOM nodes.
  PowerShell 5.1 strips double quotes inside arguments: for anything
  longer than a line, write the expression to a file and run
  `winuse eval <window> --file x.js`, where no quoting applies. A one-liner
  can single-quote the expression and use backticks for JavaScript strings:
  ``winuse eval 101 'document.querySelector(`h1`).innerText'``.
