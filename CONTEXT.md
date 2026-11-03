# winuse

A Windows computer-use tool that lets any coding-agent harness see and operate desktop apps on the user's own machine, at the quality of Codex Computer Use.

## Language

### Targets

**App**:
An installed or running program, identified for listing, launching and recognition. An App owns zero or more Windows; it is never the thing an Action is aimed at.
_Avoid_: Application, program, process

**Window**:
A single top-level window belonging to an App, including its dialogs and popups (a "Save As" dialog is a new Window of the same App). Every Action is aimed at exactly one Window.
_Avoid_: App (as an action target), screen, form

**Window ID**:
The number that identifies a Window for as long as that Window exists. A Window can also be named by its App, but only when that App has exactly one Window.
_Avoid_: Handle, HWND

**Tab**:
One web page slot in a browser Window. A browser Window shows exactly one active Tab; the others are in the background.
_Avoid_: Target, page (for the slot)

**Page**:
The web content of a browser Window's active Tab, read and acted on through the browser's debugging connection rather than UI Automation. The Page's Elements belong to the Window's State like any other Elements.
_Avoid_: Document, DOM, site

**Browser Connection**:
winuse's live link to one running Chromium browser, through the winuse extension or a browser winuse launched or cloned. The user's own Chrome and a Chrome winuse launched are two browsers with two Browser Connections, and a browser Window is always reached through the one of the browser it belongs to. Without one, that Window is read through UI Automation alone, and a cold or fully covered Chromium may expose its page there late or not at all: the read waits a few seconds for a thin tree to build, then says it is still thin.
_Avoid_: CDP session, debug port, endpoint

### Observation

**Window State**:
What the agent observes about one Window at one moment: its accessibility tree of Elements as text and, when requested, a screenshot.
_Avoid_: App state, snapshot, skyshot

**Element**:
One node in a Window's accessibility tree, such as a button, text field or menu item.
_Avoid_: Control, node, widget

**Element Index**:
The number that identifies an Element within one Window State. It is only valid for the Window State that produced it and goes stale after any change to the Window.
_Avoid_: Element ID, ref

### Operation

**Action**:
A single operation the agent performs on a Window: click, set value, secondary action, scroll, drag, hover, press key, type text, select text, or a Window Action.
_Avoid_: Command, step, tool call

**Window Action**:
An Action on the Window itself rather than on one of its Elements: restore, minimize, move, resize or close.
_Avoid_: Window management, window op

**Desktop command**:
A command aimed at the desktop as a whole rather than at a Window, such as reading, setting or clearing the clipboard. It is not an Action: it shows no Agent Cursor and is not a Batch step, but it runs one at a time with the Actions.
_Avoid_: Global action, system command

**Secondary Action**:
An operation an Element offers besides a plain click, such as Expand, Collapse, Toggle or Scroll Down, listed next to that Element in the Window State.
_Avoid_: Pattern, verb, default action

**Element Reference**:
How an Action names its target: an Element Index, an Element's name, an Element's ID, or screen coordinates. A name or ID that matches more than one Element is an error, never a guess.
_Avoid_: Selector, locator

**Batch**:
Several Actions sent in one call and run in order, stopping at the first one that fails.
_Avoid_: Script, macro, sequence

**Headless Action**:
An Action that winuse can deliver reliably without the foreground: no foreground change, no pointer, no real keys, no banner. Every Window Action is always Headless, and so is selecting text in a field. These are Headless unless the agent asks for Headed: a click on an Element (left button, single), a set value, every Secondary Action except Raise, scrolling an Element, Return or Escape on a classic dialog, and typing into a classic edit box that has the focus or that the agent names as the Element to type into. Actions on a Page Element through a Browser Connection are Headless too, whatever the button or click count, and so are keys and typing while the Page has the focus and scrolling at a point of the Page. The Actions on a Tab's Page as a whole (navigate, back, forward, reload, upload, dialog, wait and eval) are always Headless.
_Avoid_: Background action, silent action, pattern action

**Headed Action**:
An Action performed the way a person would: the Window is brought to the foreground and real pointer or keyboard input is sent, under the in-control banner. These are always Headed: dragging, hovering, any Action aimed at screen coordinates except a scroll at a point of the Page, double, right or middle clicks outside a Page, activating a Window and Raise. Keys and typing are Headed wherever they have no Headless route and are not sent as Posted Keys, except typing into a named Element, which is refused with nothing sent unless the agent asks for Headed. Any other Action is Headed when the agent asks for it.
_Avoid_: Foreground action, physical action, v1 mode

**Posted Keys**:
Keys or text that the agent explicitly asks to post to a Window's focused control without the foreground. Best effort: the app may ignore them, and they cannot carry modifiers. Neither Headless nor Headed.
_Avoid_: Background keys, silent typing

**Blocked Combo**:
A key combination press-key refuses, with nothing sent, unless the agent passes `--force`: Alt+F4, Alt+Tab, Ctrl+Shift+Escape, and any chord that holds one of them. The Windows key is refused always, with no override.
_Avoid_: Dangerous key, forbidden shortcut

**Agent Cursor**:
The second, presentation-only cursor that shows the user each Headless Action: it travels to the Element, says what is happening in a label and ripples on the click. It never takes the user's pointer, keyboard or foreground and never appears in winuse's own screenshots.
_Avoid_: Fake cursor, ghost cursor, pointer

**Esc stop**:
The user pressing the physical Escape key while winuse is acting: the running command stops immediately and the agent is told the user stopped winuse (exit code 130). During a Headed Action the key never reaches the controlled app; during a Headless Action it still reaches whatever app the user is working in.
_Avoid_: Interrupt, cancel, kill switch

**Harness**:
The coding-agent program the user runs (Claude Code, Pi, ...) that drives winuse on the model's behalf.
_Avoid_: Client, agent, host

### Evaluation

**Task Suite**:
The fixed set of real desktop tasks used to judge whether winuse is as good as Codex, each run unattended through a Harness.
_Avoid_: Benchmark, eval, test suite

**Check**:
The script that decides whether one task in the Task Suite passed, by inspecting the end result (files, settings, app state), never by asking a model.
_Avoid_: Judge, grader, assertion
