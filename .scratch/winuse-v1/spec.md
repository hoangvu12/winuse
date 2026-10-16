Status: ready-for-agent

# winuse v1: Codex-quality computer use on Windows, from any Harness

## Problem Statement

The user wants their coding agents to operate desktop apps on their own Windows PC: open apps, click, type, fill dialogs, read what's on screen. The best existing experience is Codex Computer Use, but it is tied to the Codex desktop app, which the user doesn't have, and to Codex as the Harness. The user's everyday Harnesses (Claude Code with Opus 5.5, Pi with glm-5.3) have no comparable Windows computer use. Existing open-source Windows tools either move the user's mouse for everything without a good observation model (Windows-MCP), silently do nothing in many apps while reporting success (open-computer-use), or are large background-first platforms with telemetry and pre-release churn (Cua Driver). Most of them are MCP servers whose tool definitions sit in every session's context even though computer use is only needed occasionally.

## Solution

winuse is a small open-source (MIT) Windows computer-use tool delivered as an Agent Skill plus a single Go command-line binary (ADR 0001, ADR 0003), with our own native layer rather than Cua Driver (ADR 0002). It reproduces Codex Computer Use's Windows tool surface and behaviour: the agent lists Apps and Windows, reads a Window State (Codex-format accessibility tree as text, screenshot on request), and performs Actions on a Window by Element Index or other Element Reference. Like Codex on Windows it works in the foreground, shows an on-screen overlay while in control, and stops when the user presses Esc. It fixes Codex's known Windows bugs (DPI offsets, untargetable dialogs, UI Automation freezes, screenshot timeouts) and adds hover, zoom, Batches and name/ID/coordinate Element References. Any Harness that can run shell commands and load skills can use it; v1 is verified on Claude Code and Pi.

**Guiding rule:** any behavioural detail not decided in this spec follows Codex Computer Use's documented Windows behaviour (tool names and parameters, key names, timeouts, tree format), except where that behaviour is one of Codex's known bugs.

## User Stories

### Installing and loading

1. As a user, I want to install winuse with one PowerShell line, so that I don't need Go, Rust or any runtime installed.
2. As a user, I want to install the skill with `npx skills add`, so that it lands where both Claude Code and Pi discover skills.
3. As a user, I want the skill to cost only its name and description in context until it is used, so that sessions that never touch the desktop pay nothing for it.
4. As an agent, I want the skill to tell me how to install the binary if `winuse` is not on PATH, so that I can recover without the user's help.
5. As a user, I want winuse to work in any Harness that can run shell commands, so that I'm not locked into one agent product.
6. As a user, I want winuse to send no telemetry, so that my desktop activity stays on my machine.
7. As a user, I want the README to explain how to allow the unsigned binary if Windows Defender or SmartScreen flags it, so that a false positive doesn't block me.

### Discovering Apps and Windows

8. As an agent, I want to list Apps, with whether each is running and which Windows it has, so that I can choose what to work with.
9. As an agent, I want to list Windows with their Window ID, App and title, so that I can target one precisely.
10. As an agent, I want to launch an App by name or executable path, so that I can start work in an App that isn't open.
11. As an agent, I want to name a Window by its App name when that App has exactly one Window, so that common cases need no ID lookup.
12. As an agent, I want naming a Window by App to fail with a list of candidates when the App has several Windows, so that I never act on the wrong one.
13. As an agent, I want a dialog such as "Save As" to appear as its own Window of the same App, so that I can target it directly.
14. As an agent, I want hidden, cloaked and tool windows excluded from listings, so that I only see Windows a person could see.

### Observing a Window

15. As an agent, I want a Window State as text in Codex's indented tree format, so that I can read the UI cheaply and get Element Indexes immediately.
16. As an agent, I want each Element line to show its role, name, ID, state (enabled, focused, selected, toggled, value) and Secondary Actions, so that I know what I can do with it.
17. As an agent, I want a screenshot only when I ask for one, so that I spend image tokens only when the tree isn't enough.
18. As an agent, I want the screenshot saved as an image file whose path is printed, so that I can open it with my Harness's file-reading tool.
19. As an agent, I want the screenshot to include menus, dropdowns and popups drawn over the Window, so that what I see matches what a person sees.
20. As an agent, I want screenshot coordinates that map exactly to click coordinates at any display scaling and on any monitor, so that clicks land where I aim.
21. As an agent, I want to zoom into a region of a Window at full resolution, so that I can read dense UI that is illegible in the scaled screenshot.
22. As an agent, I want the Window State to tell me when a Window's tree is nearly empty (canvas apps, some Electron apps), so that I know to rely on the screenshot instead.
23. As an agent using a text-only model (Pi with glm-5.3), I want the tree alone to be enough for ordinary tasks, so that I can work without vision.
24. As an agent, I want Chromium-based browsers and Electron apps to expose their web content in the tree, so that I can operate web pages and apps like Discord or VS Code.
25. As a user, I want reading a Window's tree to never freeze that app or my system, so that winuse doesn't make my PC sluggish.
26. As an agent, I want tree reads that exceed a time limit to return what was gathered plus a clear note, so that a slow app doesn't hang my turn.

### Acting on a Window

27. As an agent, I want to click an Element by Element Index, so that I don't have to compute coordinates.
28. As an agent, I want to click by screen coordinates taken from the screenshot, so that I can operate apps with poor accessibility trees.
29. As an agent, I want double-click and right-click, so that I can open files and context menus.
30. As an agent, I want to set an Element's value directly, so that filling a text field is one reliable Action.
31. As an agent, I want to perform an Element's Secondary Action (Expand, Collapse, Toggle, Scroll Down, …), so that I can operate controls semantically.
32. As an agent, I want to scroll a Window at a point, so that I can reach content below the fold.
33. As an agent, I want to drag from one point to another, so that I can reorder items, select ranges and draw.
34. As an agent, I want to hover at a point, so that I can reveal hover-only toolbars and tooltips.
35. As an agent, I want to press keys and key combinations by Codex's key names, so that shortcuts work the same way they do in Codex.
36. As an agent, I want to type arbitrary Unicode text, so that I can enter any language.
37. As an agent, I want to activate a Window explicitly, so that I can recover when focus is somewhere else.
38. As an agent, I want every Action to bring its target Window to the foreground automatically, so that input lands in the right place.
39. As an agent, I want to refer to an Element by name or ID instead of Element Index, so that a step still works after the UI shifts.
40. As an agent, I want a name or ID that matches several Elements to fail with the candidates listed, so that winuse never guesses.
41. As an agent, I want to send a Batch of Actions in one call that stops at the first failure, so that routine sequences cost one round-trip.
42. As an agent, I want each Action or Batch to print just `ok` or a failure with a hint, Codex-style, so that output stays short and I fetch a new Window State when I need one.
43. As an agent, I want using an Element Index whose Element no longer exists to fail as stale and tell me to read the Window State again, so that I don't click the wrong thing.

### Control, stopping and errors

44. As a user, I want a Codex-style on-screen overlay while winuse is in control, so that I know the agent is driving my PC.
45. As a user, I want pressing Esc to stop the current Action or Batch immediately, so that I can take back control at any moment.
46. As an agent, I want a stopped Action to report that the user pressed Esc, so that I stop and ask instead of retrying.
47. As an agent, I want an Action aimed at a Window running as administrator to fail with a clear explanation, so that I don't believe dropped input succeeded.
48. As an agent, I want a locked desktop to be reported as such, so that I stop instead of acting on the lock screen.
49. As an agent, I want winuse's errors to be specific (Window gone, Element stale, ambiguous reference, app not found, timeout), so that I can recover sensibly.
50. As a user, I want winuse's background helper to start by itself and exit after a period of inactivity, so that I never manage it.
51. As a user, I want only my own user account to be able to talk to the background helper, so that other processes can't drive my desktop through it.

### Proving quality

52. As the maintainer, I want a Task Suite of about 10 real tasks with automatic Checks, so that "as good as Codex" is measured, not felt.
53. As the maintainer, I want to run the Task Suite unattended through Claude Code with Opus 5.5, so that I get a score without supervising it.
54. As the maintainer, I want a text-only smoke run through Pi with glm-5.3, so that the text-first path is proven for non-vision models.
55. As the maintainer, I want Task Suite runs to work only in scratch folders, so that my real files and settings are never touched.
56. As the maintainer, I want feature parity with Codex's Windows tool surface checked off explicitly, so that nothing Codex can do is missing.

## Implementation Decisions

### Delivery

- Two deliverables: a skill (one SKILL.md with Codex-derived usage guidance) and a single Go binary `winuse`. No MCP server (ADR 0001).
- Distribution mirrors use-browser: an `install.ps1` one-liner downloads the release binary from GitHub Releases onto PATH; the skill is installed via `npx skills add` into both Claude Code's and Pi's skill locations. The binary is unsigned for v1.
- Written in Go with no cgo; Windows APIs are called directly, COM interfaces through hand-written vtable calls (ADR 0003). y3owk1n/neru (MIT) is the reference for UI Automation from Go; zzl/go-win32api (MPL-2.0) for interface layouts; Microsoft/go-winio (MIT) for named pipes. Code from GPL/AGPL/FSL projects (dagu, Cua's perception module, Cua Driver Bench) must not be copied.

### Process model

- The CLI is a thin client. The first command starts a detached background **helper** process; later commands talk to it over a named pipe restricted to the current user's SID. The helper exits after an idle period (Codex-like default).
- The helper owns all UI Automation work on a single OS-locked thread with COM initialised; everything else reaches it through a queue. It also owns the Esc keyboard hook on its own locked thread with a message loop, and the overlay.
- The helper keeps, per Window, the live Elements behind the most recent Window State, so Element Indexes resolve to the same Element across CLI calls. Reading a new Window State for a Window replaces the previous one.
- The helper does no background UI Automation event monitoring (a known cause of Codex freezes); it only touches the accessibility tree when a command asks.

### Command surface

Commands mirror Codex's Windows "window2" API names and parameters (rule: follow Codex), expressed as CLI subcommands:

- Discovery: list apps; list windows; get a window (by Window ID or App name, exactly one match required); launch an App (name or executable path).
- Observation: get Window State (text tree always; `--screenshot` adds a capture saved to a file whose path is printed); zoom (full-resolution crop of a region, saved and printed as a path).
- Actions: click (Element Reference, click count, mouse button), set value, perform Secondary Action, scroll (point plus horizontal/vertical amounts), drag (from/to points), hover (point), press key (Codex/X keysym names, e.g. `Return`, `Control_L+a`), type text, activate window.
- Batch: a sequence of Actions on one Window in a single call, run in order, stopping at the first failure.
- Element Reference forms: Element Index, `name:<text>`, `id:<AutomationId>`, `@x,y` (coordinates in the latest screenshot's space). Ambiguous name/ID matches are errors listing candidates.
- Output contract: Window State and listings go to stdout as text; Actions and Batches print `ok` or a single-line failure with a hint; failures exit non-zero; an Esc stop exits with Codex's interrupt code (130). Timeouts follow Codex (about 10 s per request, 15 s for launching an App).

### Observation details

- Window State uses Codex's tree format: depth-first, indented, numbered lines of role, name, AutomationId, state and Secondary Actions.
- Tree capture uses UI Automation cache requests scoped to the target Window, bounded by a time limit; on timeout it returns the partial tree with a note.
- Chromium/Electron: their accessibility trees switch on lazily when a UI Automation client appears, so a near-empty first read is retried after a short delay before being reported as sparse.
- Screenshots follow Codex and use Windows.Graphics.Capture for the target Window; when that fails or times out, fall back to capturing the Window's screen region (after it is in the foreground), then to `PrintWindow`. Screenshots are scaled to a model-friendly size, and the scale is recorded so coordinate Element References and zoom regions map back exactly.
- The process is per-monitor DPI aware (v2) so coordinates are physical pixels everywhere, fixing Codex's 175%-scaling offset bug.

### Acting details

- Every Action activates its target Window first (Codex behaviour), using the standard foreground-lock workarounds, and verifies the Window is in the foreground before injecting input.
- Clicks by Element use the Element's clickable point (or the centre of its bounds), after scrolling it into view if needed. Set value and Secondary Actions use the corresponding UI Automation patterns.
- Mouse and keyboard input use SendInput; text is typed as Unicode key events.
- Before acting, winuse compares the target Window's integrity level with its own and refuses admin Windows with an explanatory error (Windows would silently drop the input). It also detects a locked desktop and refuses.
- An Element Index whose Element is no longer available fails as stale with an instruction to read the Window State again.

### Control

- Overlay: a Codex-style on-screen indicator while an Action or Batch is running, click-through and never focusable, excluded from winuse's own screenshots.
- Esc: a low-level keyboard hook in the helper; a physical Esc press (injected keystrokes are ignored) aborts the current Action or Batch.
- Safety beyond Esc is left to the Harness: no permission prompts, no app deny list.

### Skill guidance

- SKILL.md adapts Codex's Windows Computer Use guidance: observe, then do one Action (or a Batch), then observe again; treat Element Indexes as stale after any change; request a screenshot only when the tree is insufficient; check focus before typing; use press key for control keys; stop and ask when the user pressed Esc or the desktop is locked; explain admin-Window refusals to the user.
- The skill is self-contained: it does not mention or depend on use-browser. Web pages are operated through the browser's accessibility tree like any other Window.

## Testing Decisions

- **Good tests check external behaviour only**: what `winuse` prints and what actually happened in the target app. They never assert on internal structures, COM call sequences or helper internals.
- **Seam 1 (primary): the `winuse` binary against a fixture app.** Tests invoke the real CLI exactly as an agent would, against a purpose-built fixture app with known Elements (buttons, text field, checkbox, list, menu, a "Save As"-style dialog, a hover-only toolbar, a canvas) and a local HTML page in Edge for the Chromium path. Assertions cover printed output and the fixture's end state. These tests take over the screen, so they are **opt-in only**: they never run as part of the default test command and must be started explicitly by the user (e.g. via an environment variable). No agent may run them without the user's explicit go-ahead.
- **Seam 2 (fast): Window State rendering and Element Reference resolution from a recorded tree.** Recorded accessibility trees (captured once from real apps and stored as fixtures) are fed to the pruning/printing and reference-resolution logic and compared with expected text. No desktop needed; this is the default test run and is safe to run any time.
- **Task Suite (quality, not correctness):** about 10 tasks spread across ordinary Windows apps, Chromium/Electron apps, a dialog-heavy flow and a canvas app, each with a Check script that inspects end results (files in a scratch folder, settings, app state); no model judges. A runner executes each task unattended via `claude -p` with Opus 5.5 under fixed time and turn limits and prints a pass/fail table; a smaller text-only subset runs through Pi with glm-5.3. Task ideas may be adapted from WindowsAgentArena (MIT); method borrowed from Cua Driver Bench (checkers not judges, same Harness/model/prompt across runs) without copying its FSL code. Runs take over the screen and are started only by the user.
- **Prior art**: android-control's offline tests (rendering and element pickers tested with no device) and its evaluation rubrics; use-browser's Go tests; pane's Windows smoke script (find Element by name, Toggle/Invoke/click-centre fallback, confirm foreground before input).

## Out of Scope

- Background operation (acting without taking the foreground, child sessions, VMs, Agent Workspace).
- An MCP server or SDK.
- macOS and Linux.
- Code signing.
- Permission prompts, app deny lists or URL policies (left to the Harness).
- Controlling administrator windows, UAC prompts or the secure desktop.
- Full-desktop screenshots.
- Integration with use-browser or any browser-extension/CDP path.
- Running WindowsAgentArena or other VM-based benchmarks.
- Telemetry of any kind.

## Further Notes

- Codex's Windows implementation for reference: a bundled helper (`codex-computer-use.exe`) driven via the `@oai/sky` package over JSON-RPC, using UI Automation, Windows.Graphics.Capture and SendInput; its tool surface and SKILL.md guidance were recovered from public dumps of the plugin and openai/codex issues.
- Windows.Graphics.Capture from Go is the riskiest piece (no Go WinRT bindings); the GDI fallbacks must work on their own so screenshots never depend on it.
- Codex bugs to avoid, from its issue tracker: DPI offsets at 175% scaling, untargetable owned dialogs, UI Automation freezes from background monitoring and bulk reads, screenshot timeouts/black frames/wrong window, locale-dependent control-type checks.
- A leftover scaffold from an earlier, abandoned Rust attempt exists at `C:\Users\ADMIN\computer-use-mcp`; it is unrelated to this repo.
