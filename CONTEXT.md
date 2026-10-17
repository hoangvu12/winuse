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
A single operation the agent performs on a Window: click, set value, secondary action, scroll, drag, hover, press key or type text.
_Avoid_: Command, step, tool call

**Secondary Action**:
An operation an Element offers besides a plain click, such as Expand, Collapse, Toggle or Scroll Down, listed next to that Element in the Window State.
_Avoid_: Pattern, verb, default action

**Element Reference**:
How an Action names its target: an Element Index, an Element's name, an Element's ID, or screen coordinates. A name or ID that matches more than one Element is an error, never a guess.
_Avoid_: Selector, locator

**Batch**:
Several Actions sent in one call and run in order, stopping at the first one that fails.
_Avoid_: Script, macro, sequence

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
