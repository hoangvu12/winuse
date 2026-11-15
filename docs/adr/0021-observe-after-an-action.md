# ADR 0021: Observe after an Action, diffed by Element Index

## Status

Accepted (speed ticket 10, `.scratch/speed/issues/10-observe-after-action.md`,
2026-11-05), from research recommendation 1 in
`.scratch/research-speed/report.md`. It amends how ADR 0004's Element
Indexes are read after an Action, not what a Window State is. The live
check is still to run. Amended by learn-from 01 (2026-11-15): the Window
the Action opened is read too (see the amendment at the end).

## Context

Time in a computer-use loop goes to model turns, not to observing or
acting (the research: LLM calls are 75–94% of OSWorld task time). winuse
Actions print `ok`, so an agent that checks its step spends a second turn
on `get-window-state`, and a Batch could not read at all. Prior art acts
and observes in one call: cua's `run_steps` (an end-of-batch read that
diffs `since` the latest snapshot and falls back to the full read past 60%
changed), UFO²'s multi-action (up to 51.5% fewer LLM steps), Terminator's
`include_tree_after_action`, chrome-devtools-mcp's `includeSnapshot`,
Anthropic's computer toolset (several actions per turn), agent-browser's
`diff snapshot` and browser-use's `*` marks for new elements. Playwright
shipped incremental snapshots by default and removed them (#39777), so a
diff must be opt-in with a full fallback.

The hard part is Element Indexes. They are positions in a depth-first read
(ADR 0004): any new read renumbers from 0, and an Element inserted in the
middle moves every index after it. A diff that printed only the changed
Elements in new numbering would leave the agent holding old indexes that
silently name other Elements now. agent-browser and chrome-devtools-mcp
avoid that with durable refs keyed on a node identity; winuse's store and
its Page splice assume positional, depth-first indexes, and the full read
reads no RuntimeIds (ADR 0004 addendum: a round trip each).

## Decision

1. **`--observe[=diff|full]` on every Action aimed at one Window or Tab and
   on `batch`** (`protocol.ObservableCommands`). The request carries it
   (`protocol.Request.Observe`), and its budget grows by `ObserveTimeout`
   (10 s, a plain read's); the Action itself keeps its own budget. Default
   output stays `ok`, so Codex parity holds.
2. **The observation is a full `get-window-state` of the Action's Window**
   (or Tab target), read after the Action and its opened-Window settle,
   once the Action released its lanes (a read takes none). Like any full
   read it replaces the Agent's Window State and renumbers from 0. A Batch
   observes its own Window once, after its last step. A failed Action
   prints no observation; a Batch that ran past failed steps does, since
   the agent must read before repeating an uncertain step anyway. A read
   that fails is a `Note:` after the Action's output, which stands.
3. **The diff baseline is the Agent's latest Window State of that Window**,
   exactly as the helper stores it when the read starts: per Agent (ADR
   0014) and per Window, or per Agent per Tab for a Tab target (ADR 0017),
   including the Elements `find`, `--root` and a live `name:`/`id:`
   resolution added after the full read. That is every index the Agent
   could still use. Whatever made it (`get-window-state`, `--skeleton`, a
   Page `find`, an earlier `--observe`) does not matter.
4. **The diff compares by Element Index, never by a guessed identity**
   (`wstate.Diff`): an index both reads have is `~` changed when its printed
   line or depth differs (the old line follows in `(was: ...)`), an index
   only the new read has is `*` new, and one only the baseline has is `-`
   gone. So the rule the agent relies on holds by construction: **an index
   the diff does not list names an Element whose line is exactly what the
   agent saw before.** A gone index is past the new read's end: an Action on
   it is refused as stale, nothing sent, and its number is reused only by a
   later read that prints it.
5. **The whole Window State prints instead, with a one-line note,** when
   there is no baseline or when the diff is longer than 60% of the whole
   (cua's and dev-browser's threshold). An insertion or removal in the
   middle shows as every later index changed, so it lands here: the agent
   gets the new numbering whole, as from `get-window-state`.
6. **The output budgets and their defaults shape the observation** as they
   shape a full read (`--interactive`, `--depth N`, `--compact`,
   `--max-chars N`, `--out <file>`, `--full`; ADR 0004's speed 13 addendum:
   shared IDs left out and a 28000-character cap unless `--full`, which
   prints Codex's text): a diff prints only the changes the view prints,
   leaves out the shared IDs on both sides of a `(was: ...)`, and says how
   many changes it left out; the 60% test compares against the whole State
   under the same view, uncapped. On an Action they go anywhere outside literal text, as
   `--headed` does; on a Batch before its first step, so a step keeps its
   own.
7. **`get-window-state` and `find` are Batch steps**, with their own flags
   and the Batch's Window (`in <window>` and `in new` too), so a Batch can
   act, read what opened and act on it. A read step takes no lane, except
   the desktop lane a `--screenshot` takes, which the Batch takes up front;
   the screenshot path no longer waits for a lane its own Batch holds.

## Considered

- **Durable indexes** (survivors keep their number, new Elements get fresh
  ones, as agent-browser does): the store, the Page splice
  (`page.Origin.DocIndex`, `holdsPage`'s `index+1`, `storedSubtree`) and
  every view assume a positional, depth-first list, and the read has no
  cheap identity. It would make a middle insertion a small diff. It stays
  the next step if agents are seen re-reading after fallbacks (rec 6).
- **Matching by content (LCS) and printing index moves**: shorter diffs,
  but the agent would have to apply "41–240 are now 42–241" to the indexes
  it holds, and one that does not acts on the wrong Element.
- **Diff by default**: Playwright's experience; no.

## Consequences

- Token cost, measured on 16 Window States (the two recorded fixtures and
  14 distinct real ones from local transcripts, 5 to 341 Elements) with
  simulated Action effects, tiktoken o200k as the proxy, both sides under
  the read's defaults: a local effect (a field's value, a check box, a
  status line, Elements appearing at the end) prints **13.9% of the whole
  State** (4,130 against 29,776 tokens): 46.9% under 30 Elements, 12.5% at
  30–149, 3.6% at 150 or more. No change prints 8.5%. A node expanding in
  the middle falls back to the whole State and costs 3.7% more than
  `full` (the note). Numbers in the ticket.
- The turn saved is the larger win: one model turn per checked step, which
  `--observe=full` gives too.
- The observation reads right after the Action. A Page or App that reacts
  late can show no change yet; the skill says to `wait` then.
- Real transcripts held almost no whole read–act–read pairs: agents piped
  big reads through `Select-String` to save tokens, which the diff and the
  budgets now do inside winuse.

## Amendment: --observe reads the Window the Action opened too (learn-from 01, 2026-11-15)

Ticket `.scratch/learn-from/issues/01-observe-opened-window.md`, from
`.scratch/learn-from/agent-desktop.md` §2.1. Decision 2 read only the
Action's Window, so after `click name:File` the agent got the diff of the
main Window and `Note: a new Window opened: 5678 ...`, then spent a turn on
`get-window-state 5678`. Menus and dialogs are among the most common
effects of an Action. agent-desktop lists the surfaces an Action opened
but does not inline them. Decision 2 now reads:

- **The Window the Action opened is read too**, when the opened-Window
  settle (ADR 0019) found exactly one new Window, a dialog, a menu or a
  popup of the target's App (or a shell surface for a shell target), or a
  Headless ContextMenu opened its menu. It is read after the Action's own
  Window, as `get-window-state <Window ID>` reads it: a full read that
  replaces the Agent's Window State of that Window.
- **It prints whole, never as a diff**, under the line `The Window the
  Action opened, <Window ID>, read whole; its Element Indexes go with window
  <Window ID>:`, in both modes. A new Window has no earlier read worth
  diffing against; a menu's Window ID can be reused, so a stored State of
  it may be of another menu.
- **Several new Windows: none is read.** A note lists their Window IDs
  (`Note: --observe read none of the 2 Windows the Action opened (777,
  778); read the one you need with winuse get-window-state <Window ID>`):
  which one the agent wants is not winuse's guess, and reading every one
  could cost more than the turn it saves.
- **A Batch** follows what its settle around all its steps found: a
  Window open at the end that was not open before the Batch. A Window the
  Batch opened and closed again (an `in new` dialog it filled and
  confirmed) is not read.
- **A Tab target** keeps its observation of the Tab. A native Window its
  browser opened (a Chromium menu popup) is read like any opened Window. A
  new Tab and a JavaScript dialog are notes, as before, never read.
- **Budget and caps.** The two reads share the observation's
  `ObserveTimeout` (10 s), the Action's Window first; the request budget is
  unchanged. Each read takes the observation's view on its own (the
  28000-character cap, `--interactive`, `--depth`, `--compact`, `--full`),
  as two `get-window-state` steps of a Batch would. With `--out <file>`
  the opened Window's State goes to the file's name with `-<Window ID>`
  before its extension (`observed-5678.txt`).
- **Failures are notes.** A read that fails prints `Note: --observe could
  not read <Window ID> after the Action, ...`, and so does a Window that
  closed between the settle and the read. The Action's output and its own
  observation stand. A failed Action and an Esc stop read nothing, as
  before.

Considered: reading the topmost of several new Windows (cheap, but a
dialog and a tooltip-like popup of the same App can open together and the
agent would act on the wrong one); a shared character cap across both
reads (a whole main Window would leave the menu nothing); growing the
request budget by a second `ObserveTimeout` (the client's budget would
grow for every observed Action, for a read that rarely happens and is
usually fast).
