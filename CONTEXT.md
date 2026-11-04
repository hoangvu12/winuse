# winuse

A Windows computer-use tool that lets any coding-agent harness see and operate desktop apps on the user's own machine, at the quality of Codex Computer Use.

## Language

### Targets

**App**:
An installed or running program, identified for listing, launching, closing and recognition. An App owns zero or more Windows; it is never the thing an Action is aimed at. Closing an App asks each of its Windows to close, as a Window Action does, and is not an Action itself.
_Avoid_: Application, program, process

**Window**:
A single top-level window belonging to an App, including its dialogs and popups (a "Save As" dialog is a new Window of the same App). Every Action is aimed at exactly one Window.
_Avoid_: App (as an action target), screen, form

**Window ID**:
The number that identifies a Window for as long as that Window exists. A Window can also be named by its App, but only when that App has exactly one Window.
_Avoid_: Handle, HWND

**Shell surface**:
A Window of the Windows shell itself rather than of an App a person works in: the taskbar, the tray overflow (the hidden icons), Start, the Action Center and the toast banner that shows a new notification. `list-windows` leaves them out; `list-windows --shell` lists the taskbar always and the others while they are open (ADR 0015). A shell surface is named by its Window ID only. Its Window State reads Headless. Acting on its Elements is Headed: a button opens a flyout or activates an App, either of which takes the user's foreground; `activate-window` brings an App forward the explicit way, and `--headed open-shell` opens Start, the notification panel, quick settings or the tray overflow. Escape on the open tray overflow is the one Headless Action, and a toast's own buttons are pressed Headless by the notification commands only.
_Avoid_: System surface, shell window, taskbar App

**Tab**:
One web page slot in a browser Window. A browser Window shows exactly one active Tab; the others are in the background. Through the winuse extension, the Tabs an Agent opens join that Agent's tab group: its Agent Cursor's name ("winuse" for the default Agent) in the nearest tab group colour to its cursor's.
_Avoid_: Target, page (for the slot)

**Page**:
The web content of a browser Window's active Tab, read and acted on through the browser's debugging connection rather than UI Automation. The Page's Elements belong to the Window's State like any other Elements.
_Avoid_: Document, DOM, site

**Browser Connection**:
winuse's live link to one running Chromium browser, through the winuse extension or a browser winuse launched or cloned. An Electron or WebView2 App winuse launched with `--cdp`, or a running Electron App winuse attached to with the user's consent, has one too, reached only through its own Windows. The user's own Chrome and a Chrome winuse launched are two browsers with two Browser Connections, and a browser Window is always reached through the one of the browser it belongs to. Without one, that Window is read through UI Automation alone, and a cold or fully covered Chromium may expose its page there late or not at all: the read waits a few seconds for a thin tree to build, then says it is still thin.
_Avoid_: CDP session, debug port, endpoint

### Observation

**Window State**:
What the agent observes about one Window at one moment: its accessibility tree of Elements as text and, when requested, a screenshot.
Each Agent keeps its own Window State per Window (ADR 0014), so another Agent's read never replaces it.
_Avoid_: App state, snapshot, skyshot

**Element**:
One node in a Window's accessibility tree, such as a button, text field or menu item.
_Avoid_: Control, node, widget

**Element Index**:
The number that identifies an Element within one Window State. It is only valid for the Window State that produced it and goes stale after any change to the Window.
Element Indexes are per Agent (ADR 0014), valid only for the Agent whose Window State produced them.
_Avoid_: Element ID, ref

### Operation

**Action**:
A single operation the agent performs on a Window: click, set value, secondary action, scroll, drag, hover, press key, type text, select text, invoke a menu command, or a Window Action.
_Avoid_: Command, step, tool call

**Window Action**:
An Action on the Window itself rather than on one of its Elements: restore, minimize, maximize, move, resize or close.
_Avoid_: Window management, window op

**Desktop command**:
A command aimed at the desktop as a whole rather than at a Window, such as reading, setting or clearing the clipboard, listing the displays or capturing one, or listing and pressing the notifications the toast banners show. It is not an Action: it shows no Agent Cursor and is not a Batch step. `open-shell` is the one Headed Desktop command: it runs only with `--headed`. One that touches the clipboard or captures the screen runs one at a time with the Actions; one that only lists, such as the displays, does not wait for them.
_Avoid_: Global action, system command

**Secondary Action**:
An operation an Element offers besides a plain click, such as Expand, Collapse, Toggle or Scroll Down, listed next to that Element in the Window State. ContextMenu, which opens the Element's right-click menu and names the menu's Window, is never listed: any Element may take it, and the menu is closed when the same Agent next acts on another Window.
_Avoid_: Pattern, verb, default action

**Element Reference**:
How an Action names its target: an Element Index, an Element's name, an Element's ID, or screen coordinates. A name or ID that matches more than one Element is an error, never a guess.
_Avoid_: Selector, locator

**Batch**:
Several Actions sent in one call and run in order, each one's output printed under its number. A failed Action does not stop the Batch unless the agent asks for that; the Batch then fails as a whole, naming every Action that failed and marking those that may have taken effect. The Actions act on the Batch's Window, unless one names its own Window or the Window the Action before it opened.
_Avoid_: Script, macro, sequence

**Headless Action**:
An Action that winuse can deliver reliably without the foreground: no foreground change, no pointer, no real keys, no banner. Every Window Action except maximize is always Headless, and so are selecting text in a field and running a command of a classic menu bar. These are Headless unless the agent asks for Headed: a click on an Element (left button, single), a set value, every Secondary Action except Raise, scrolling an Element, the same four at a point of a screenshot (they act on the Window's own Element under the point, never on whatever covers it), Return or Escape on a classic dialog, Escape on the open tray overflow, a toast's button through `notification-action` or `notification-dismiss`, a chord that runs the command the Window labels with it (its shortcut), and typing into a classic edit box that has the focus or that the agent names as the Element to type into, and typing into another text field the agent names that has a text caret (its value rewritten at the caret, with no key events). Actions on a Page Element through a Browser Connection are Headless too, whatever the button or click count, and so are keys and typing while the Page has the focus and scrolling at a point of the Page. In a Chromium or Electron Window without a Browser Connection, keys, typing and double, right or middle clicks go as window messages without the foreground (typed text is read back where the field has a value). The Actions on a Tab's Page as a whole (navigate, back, forward, reload, upload, dialog, wait and eval) are always Headless. A click, set value, Secondary Action or text selection by Element reaches a minimized Window too, except a packaged Store App's.
_Avoid_: Background action, silent action, pattern action

**Headed Action**:
An Action performed the way a person would: the Window is brought to the foreground and real pointer or keyboard input is sent, under the in-control banner. An Action is Headed only when the agent asks for it with `--headed`. Activating a Window is the one exception: the command itself asks for the foreground. These have no other route, so without `--headed` they are refused with nothing sent and the exact `--headed` retry: dragging, hovering, an Action aimed at screen coordinates where no Element of the Window under the point can take it, double, right or middle clicks outside a Page or a Chromium or Electron Window and not sent as Posted Clicks, maximizing a Window (Windows has no maximize that leaves the foreground alone), Raise, a WPF Element's ContextMenu, a click, set value or Secondary Action on an Element of a Shell surface (it opens a flyout or activates an App; a toast's buttons have the notification commands), and keys and typing that have no Headless route and are not sent as Posted Keys. winuse never takes the user's screen on its own. ADR 0013 narrowed this set to what is left: a route that reads its effect back is Headless, and one that cannot is opt-in Posted. A Headed Action is polite (ADR 0014): it waits while the user is typing or clicking, stops when they take over, and afterwards gives back their foreground and pointer, unless it left a menu or Window of the App open.
_Avoid_: Foreground action, physical action, v1 mode

**Posted Keys**:
Keys or text that the agent explicitly asks to post to a Window's focused control without the foreground. Best effort: the app may ignore them. A chord's modifiers are held for the Window's thread only, never the user's keyboard (ADR 0013); Control and Alt together are refused. Neither Headless nor Headed.
_Avoid_: Background keys, silent typing

**Posted Clicks**:
Clicks and wheel scrolls that the agent explicitly asks to post as mouse messages to the classic control under a point or an Element of a background Window, without the foreground or the real pointer. Only classic controls take them (edit boxes, buttons, list, tree and combo boxes). Best effort: the app may ignore them. Neither Headless nor Headed.
_Avoid_: Background clicks, silent clicks

**Blocked Combo**:
A key combination press-key refuses, with nothing sent, unless the agent passes `--force`: Alt+F4, Alt+Tab, Ctrl+Shift+Escape, and any chord that holds one of them. The Windows key is refused always, with no override.
_Avoid_: Dangerous key, forbidden shortcut

**Agent Cursor**:
The second, presentation-only cursor that shows the user each Headless Action: it travels to the Element, says what is happening in a label that is whole by the time it lands, and ripples when the Action is delivered (a Page's first press, key or text, a navigation the browser accepted), not when the Action and its read-back end. It never takes the user's pointer, keyboard or foreground and never appears in winuse's own screenshots. On a Page it is the same native cursor, placed through the browser's content area (ADR 0016), never drawn inside the Page.
Each Agent has its own Agent Cursor (ADR 0014), with its own colour and its name in the label, placed just above the Window it acts on rather than topmost; it closes when its Agent has gone idle. After a Headless set-value or typing into a named field it briefly shows an insertion caret where the field's caret now is.
_Avoid_: Fake cursor, ghost cursor, pointer

**Esc stop**:
The user pressing the physical Escape key while winuse is acting: the running command stops immediately and the agent is told the user stopped winuse (exit code 130). During a Headed Action the key never reaches the controlled app; during a Headless Action it still reaches whatever app the user is working in. The user's own key press, click or wheel turn during a Headed Action stops it the same way; it reaches its app, and the agent is told the user took over.
_Avoid_: Interrupt, cancel, kill switch

**Delivery**:
What a failure says about its Action reaching the app: not delivered ("nothing was sent", a retry is safe), delivered but unverified ("the Action was delivered; do not repeat it"), or uncertain ("may have taken effect"). Under `--json` it is a field beside the failure's code (desktop-parity 07); in the text it is the phrase.
_Avoid_: Sent state, side effect

**Harness**:
The coding-agent program the user runs (Claude Code, Pi, ...) that drives winuse on the model's behalf.
_Avoid_: Client, agent, host

### Agents

**Agent**:
One caller of winuse, with an identity: by default the Harness process the command came from, or the name in `WINUSE_AGENT`; a command with neither belongs to one shared default Agent. Every request names its Agent, each Agent has its own Window States, and `winuse status` lists the Agents the helper served. Each Agent has its own Agent Cursor. Several Agents run at once, each Action in its Lane (ADR 0014).
_Avoid_: Session, client, user

**Lane**:
The line an Action waits in before it runs (ADR 0014). A Window lane is shared by the Windows of one UI thread, a Page Action has a lane per Tab, and the one desktop lane is taken by every Headed Action. Actions in different lanes run at the same time. An Action that waits about 12 seconds for its lane is refused with nothing sent, naming the Agent that holds it.
_Avoid_: Lock, lease, queue

### Evaluation

**Task Suite**:
The fixed set of real desktop tasks used to judge whether winuse is as good as Codex, each run unattended through a Harness.
_Avoid_: Benchmark, eval, test suite

**Check**:
The script that decides whether one task in the Task Suite passed, by inspecting the end result (files, settings, app state), never by asking a model.
_Avoid_: Judge, grader, assertion
