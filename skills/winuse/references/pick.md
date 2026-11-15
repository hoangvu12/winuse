# Pick

Read when: `winuse jev` shows a key and you want an Element found by description with `pick` or a short task done with `run`, or the user asks to set up Jev.

`pick` lets a Jev-compatible server choose the Element. It needs a Jev API key. `winuse jev` shows the server, model and key
(masked) in use. With a key, `pick` saves you reading the Window State:
winuse reads the Window itself, a Jev-compatible System One server chooses
the Element, and only that Element's line comes back.

```
winuse pick 4242 "the Save button"
winuse pick 4242 "the search box" --set-value "invoice 2026"
winuse pick 4242:1a2b3c4d "accept all cookies" --click --observe
```

`ok 7 button Save ID: save  p=0.98` is the pick: the Element's line as
`get-window-state` prints it, and the server's probability. With `--click`
or `--set-value`, the Action's own output follows, and `--observe` prints
the Window State after it. The printed index works afterwards, as after a
`get-window-state`, because pick's read replaces your Window State of that
Window.

## When to use it

- Use it when you would otherwise read a Window just to find one Element:
  a big Page or a Window you have not read. Each pick takes 1-4 s.
- Do not use it when the index is already in your context: act on it.
- Describe the control as the Window shows it ("the email field", "the
  Plus key"), not the goal ("log me in", "add the numbers"). The server
  reads the description literally.
- Do not use it for a chain of steps whose effects you need to check:
  check them as usual, with `--observe` or a read.

## When it fails

- Below p=0.5, pick acts on nothing, prints `candidates:` (the likeliest
  Elements with their probabilities) and exits 1. Choose among them, or
  read the Window and choose yourself.
- `lists no Elements to pick from`: the tree is empty or thin (a canvas,
  a minimized UWP App, a Page not built yet). Read it with `--screenshot`.
- `did not answer`: the server failed or timed out; nothing was clicked.
  `winuse jev check` tests the server and the key.
- `needs a Jev API key` or `the Jev settings do not load`: nothing was
  read or sent. Do the work yourself, as you would without Jev.

## run: a short task, step by step

`run` keeps the wheel for a bounded task in one Window, a Page or a
desktop App: each step reads the Window, asks the server for the next
operation (click, type one of your values, choose a drop-down option,
scroll, wait, done) and its Element, does it, and waits for the Window to
settle. In an App, scroll moves the pane holding most of its content.

```
winuse run 4242:1a2b3c4d "open the Wikipedia article about Kurt Godel" --set "Kurt Godel"
winuse run 4242:1a2b3c4d "search the web for openjev and open its GitHub repository" --set openjev --max-steps 10
winuse run Settings "open the About page"
winuse run 4242 "open the folder named drivers in the file list"
```

- Every text it types comes from `--set` (or a `"quoted"` part of the
  goal); the server only chooses which one and where. Pass texts with
  `--set`: PowerShell 5.1 drops quotes inside an argument.
- Each step prints one line (`2 CLICK 13 list item ... p=1.00 (read 0.6 s,
  jev 1.5 s)`) and the Action's output. `ok done after N steps` (exit 0)
  means a separate check saw the goal achieved in the Window. Still read
  the Window yourself before you report it done.
- It hands back with exit 1 on a sign-in, password or payment window
  (nothing done in that step), when 3 steps in a row change nothing, when
  the server answers `BLOCKED`, when the server fails (`did not answer`),
  or at `--max-steps` (default 25). The physical Esc key stops it, exit
  130. Then read the Window and go on yourself: a run is a shortcut, and
  the task is still yours.
- Typical tasks take 2-5 steps, 1-3 s each on TypeSafe and 2-5 s on
  Codiv. Use it for a short task with a clear end (a page, a folder, a
  setting shown), not for anything you must check step by step, and never
  for sign-ins, payments or sending messages. Key sequences (a calculation
  in Calculator) worked on TypeSafe's Jev but not on Codiv's OpenJev:
  check the result.

## Privacy

The Window's text goes to the server `winuse jev` names. Never pick or run in a
Window with private content (messages, mail, banking, health) unless the
user agreed to send it there. Password values never appear in a Window
State, so they are never sent.

## Setting it up (only when the user asks)

Ask the user for the key and the server. TypeSafe's API is the default;
Codiv (`https://api.codiv.ai`) and a self-hosted OpenJev speak the same
API. Store the key from stdin, so it stays out of the command line:

```
"<key>" | winuse jev set --key - --base-url https://api.codiv.ai
winuse jev check        # ok openjev-0.1 answered in 1400 ms
winuse jev clear        # forget the stored settings
```

`TYPESAFE_API_KEY`, `TYPESAFE_BASE_URL` and `WINUSE_JEV_MODEL` override
the stored settings. The default model, `jev-latest`, is accepted by
every Jev-compatible server. Never write the key into a file you commit.
