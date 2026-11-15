# ADR 0023: Jev picks Elements, on request, over its own wire API

## Status

Proposed (2026-11-15), from the user's request "see how something like Jev
can be used with winuse to speed things up ... make it Jev API compatible".
Research and measurements: `.scratch/jev/research.md`, `.scratch/jev/eval.py`.

## Context

An agent finds an Element by reading the whole Window State into its
context and choosing an index: one model turn and, on a big Page, about
8,000 tokens. Jev (TypeSafe's System One model) answers typed questions
about a state with a probability for every option, in about a second, and
generates no text. Servers that speak Jev's wire API exist beside
TypeSafe's: Codiv (api.codiv.ai) serves OpenJev and other open models, and
OpenJev can be self-hosted. use-browser's `find` already picks page
elements this way. The browser-merge review (`.scratch/browser-merge`) said
that route "should not come into winuse", because it sends page content to
a third party.

## Decision

- **`winuse pick <window> <description> [--click | --set-value <text>]`**
  reads the Window inside winuse (the whole State, uncapped; it never
  reaches the agent), asks one System One request which Element matches,
  prints that Element's line with its probability, and acts on it when
  asked. The read is the agent's Window State, so the index works after.
  `--observe` goes through to the Action.
- **Opt-in, and off by default.** Nothing is sent without a key. The skill
  tells agents not to use or suggest it without one, and never in Windows
  with private content unless the user agreed. This replaces the
  browser-merge note for this command only: the user chose the route and
  the server.
- **Jev's wire API and nothing else.** `POST /v1/systemone` with `model`,
  `state` and `questions` only (no server extensions such as `samples` or
  `images`), so TypeSafe, Codiv and self-hosted OpenJev all work by base
  URL. Settings follow the TypeSafe SDKs: `TYPESAFE_API_KEY`,
  `TYPESAFE_BASE_URL` (default `https://api.typesafe.ai`), plus
  `WINUSE_JEV_MODEL` (default `jev-latest`, which Jev-compatible servers
  alias to their newest model). Because an agent's shell environment does
  not outlive one tool call, `winuse jev set` also stores them in
  `%LOCALAPPDATA%\winuse\jev.json`; the environment wins over the file.
  Rate limits and 529 are retried, honouring `retry-after` up to 5 s; a
  used-up quota is not.
- **The question's shape, from the eval** (29 descriptions on three real
  Windows): each option is an Element Index described by its line, at most
  200 to a question plus a `none` option, the state only the Window's line.
  This scored 29/29 on OpenJev; bare indexes with the lines in the state
  (use-browser's shape) scored 13/29. Acting needs p >= 0.5 from the choice
  alone: a separate "does any element match" noul added nothing and
  rejected one right pick.
- **No "content is untrusted" sentence in the question.** It moved one pick
  to the wrong Element. The guard is structural: the answer can only be an
  index into winuse's own list, and nothing acts below p = 0.5.

## Consequences

- An agent can find and act on an Element in one call, without the Window
  State in its context. A pick costs 1.5-4 s on Codiv's free server (about
  1.2 s of it a fixed floor per request), so it pays when the agent would
  otherwise read the Window; it does not speed up an Action whose index the
  agent already has.
- Window text, including what the user may consider private, goes to the
  configured server. Password fields' values never appear in a Window
  State, so they are not sent.
- Page or Window text can still steer a pick within the Elements of that
  Window (a deceptive label). The p gate and the agent's own check after
  the Action are the defence.
- Not done here: noul checks as waits, and screenshot-based picks for
  canvas Apps through Codiv's `images` extension. See the research note.

## Amendment: `run` (2026-11-15)

The user asked for the goal loop after reviewing use-browser's `run` and
browser-use/jev-ultrafast (`.scratch/jev/jev-ultrafast.md`).

- **`winuse run <window> <goal> [--set <text>]... [--max-steps N]`** loops
  read, decide, act. One System One request per step carries the
  operation question (CLICK, TYPE_TEXT, SELECT, SCROLL_DOWN/UP, WAIT,
  DONE, BLOCKED, offered only when the Window allows them), a speculative
  target question for each operation that needs one (jev-ultrafast's
  shape) and, when typing is offered, which supplied value goes next.
  Only the chosen operation's target is used.
- **Jev never writes text.** Typed texts come only from `--set` and quoted
  parts of the goal (use-browser's rule); jev-ultrafast's small LLM for
  values is left out.
- **Target options in a deliberate order.** A choice's criteria go on the
  wire as an ordered JSON object (`jev.Options`): a Go map sorted the
  indexes as strings, and a run picked an unrelated link at p = 0.52. One
  question holds at most 254 targets: Elements new since the last read
  first (the suggestions a typed query opens), then those sharing words
  with the goal or an unused value, then Window order. Split across
  several questions, each normalized on its own, a distractor won.
- **Each guard is a noul in a request of its own.** Next to other
  questions, a noul drifts on Codiv's OpenJev: in the step's request,
  `danger` gave 0.00-0.04 on a login form that alone it rated 0.99, and
  `achieved` gave 0.59-0.80 on a page showing only a typed query, against
  0.09 alone (0.99-1.00 alone on pages that did show the goals); paired
  with `danger`, `achieved` still gave 0.62 (`.scratch/jev/achieved.py`,
  `danger.py`, `guards.py`). So `danger` (use-browser's question) goes in
  a second request sent concurrently with every step's, costing no wall
  time, and `achieved` in a third, sent only when a step chooses DONE. A
  DONE below 0.5 is a stall.
- **SELECT** chooses an option of a drop-down list (a combo box that
  takes no typed text, as a Page's `<select>` reads) and sets it with
  `set-value <list> <option label>`: a click on the list or its option
  opens a popup, or is refused in a background Tab. The list and its
  options leave the click targets.
- **Settle after each Action, then read.** The next step reads the Window
  once it stops changing: a Page's load (`wait load`), then two reads in
  a row that agree, and, while the reads still match the one before the
  Action, up to 1 s more; 3 s at most. Typing waits at least 400 ms for
  suggestions. The last settle read is the next step's read. On the
  DuckDuckGo task this removed two wasted steps of five (a read before the
  results were there), and Wikipedia's suggestions now arrive in time to
  be clicked. jev-ultrafast's other freshness check, re-checking the
  target right before input, is the helper's already: it refuses a
  hidden, covered or stale Element and sends nothing.
- **The operation is weighed by its target.** run takes the operation
  with the highest P(operation) x P(its best target is an Element, not
  "none"). On the dropdown page TypeSafe's Jev answered CLICK 0.70 and
  SELECT 0.29, with the click target "none" at 1.0 and the select target
  Option 2 at 1.0; the operation alone stalled three times.
- **A size budget.** Jev's context is 32k tokens for the state plus any
  one question and 64k for the state plus all questions, on every plan
  (docs.typesafe.ai/models; over it, `400 max_tokens_exceeded`; measured
  with `.scratch/jev/limit.py`), and a Wikipedia article's whole state is
  more. The long-context header lifts both to 262k, but the docs call it
  slower and less accurate, so run stays inside the defaults. The state keeps 36,000
  characters of Element lines (the Elements `rank` puts first, sent in
  Window order, with `elements_left_out`), each target question 24,000.
- **What is on screen, plus what the goal names.** jev-ultrafast sends only
  the viewport (visible controls, at most 250, and 6,000 characters of
  visible text) and so never meets the context limit. A Page read now
  knows which Elements are scrolled away: the snapshot it already takes
  has every node's box, and `Page.getLayoutMetrics`, sent in the same
  burst, the viewport (`Element.Offscreen`; not printed;
  `get-window-state --visible` leaves them out). A step sends every
  Element on screen plus at most 40 off-screen ones that share words with
  the goal or an unused value, marked "(off screen)": a click scrolls its
  target into view first, so "the hospital where he died" at the end of
  Wikipedia's Gödel article is one click, not twenty scrolls, as a
  viewport-only run would need. Requests fell from about 17,700 input
  tokens to 3,700-5,600 on a long article. A goal word is matched past
  the line's role, so "the link to ..." does not match every link.
- **SCROLL on a Page scrolls its window** by 80% of the viewport (eval),
  as jev-ultrafast does: `scroll` on Element 0 is refused on a Tab
  target, which run had sent until then.
- **Hand back, never push on:** danger >= 0.6 stops before acting; three
  steps in a row that change nothing, BLOCKED, `--max-steps` (default 25,
  at most 100) and the physical Esc key (exit 130) end the run. An Element
  whose Action winuse refused is not offered again until the Window
  changes; a target below p = 0.5 is a stall, not an action.
- **Measured** on the user's Brave with Codiv's free server: the Wikipedia
  article task in 3 steps, 9.3-10.6 s; the DuckDuckGo-to-GitHub task in 5
  steps, 17-22 s (one DONE rejected, or one stale index, on the way); a
  practice login page handed back before step 1 (danger 0.99). One
  Wikipedia run handed back after clicking a "Create the page" link on the
  search results, whose editor asks to log in: the guard working, the
  path a detour. Codiv's ~1.2-1.6 s floor per request dominates.
  On TypeSafe (jev-1.14.0, 0.3-0.7 s a request, at times 2 s), with the
  settle, SELECT, the budget and the weighing: the dropdown in 2.2 s,
  Wikipedia in 8.2-11.5 s (2 actions), DuckDuckGo-to-GitHub in 9.6-10.2 s
  (3 actions, none wasted), the login page handed back at once. Most of
  the rest is reading: a Wikipedia article's read takes 0.5-0.7 s, and the
  settle reads it more than once. Waiting for `domcontentloaded` instead
  of `load` changed nothing measurable. With on-screen requests: the
  dropdown 2.2 s, Wikipedia 7.3 s, DuckDuckGo 8.6 s, the hospital link far
  down the Gödel article 5.5 s; Jev 0.3-0.8 s a step.
- **Desktop Apps too.** A desktop text editor reads as a settable
  `document` (Notepad's) and takes TYPE_TEXT. SCROLL in an App scrolls,
  through its ScrollPattern, the Element that scrolls vertically and holds
  the most Elements (Settings' page pane over its navigation pane, File
  Explorer's Items View), 10 small increments a step; until then SCROLL was
  offered only on a Page, and a virtualized list's far items were out of
  reach. Desktop reads mark nothing off screen: their trees are small, and
  a virtualized list holds only the realized rows. Measured on TypeSafe:
  Settings "open the About page" in 1 click, 4.2 s; Calculator 7 x 8 in 4
  clicks, 7.9 s; Notepad typing a value, 2.2 s; File Explorer "open the
  drivers folder" in System32, 3 scrolls and a click, 8.0 s (19.2 s with 5
  increments a scroll).
- **Jev is a shortcut, never a requirement.** Without a key, or with
  settings that do not load, `pick` and `run` read and send nothing and
  exit 1 with a hint to do the work as without Jev; a server that fails
  mid-run (`did not answer`, a 401, a used-up quota) ends the run with
  nothing done in that step and the same hint. No other command reads the
  Jev settings.
- **Known limits:** a sequence of keys is weak on Codiv's OpenJev: asked
  to compute 12 + 30 in Calculator, it pressed 2, 3, + and lost the order
  (TypeSafe's Jev did 7 x 8 right). A run suits tasks of distinct controls
  (fields, suggestions, links, lists), and its result is still checked.
