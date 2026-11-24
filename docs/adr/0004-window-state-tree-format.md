# Window State tree format and bounded read

Ticket 03 defines what `winuse get-window-state` prints and how the tree is
read. The spec's guiding rule is "follow Codex's documented Windows
behaviour"; where Codex is undocumented (its truncation budgets are unknown)
or buggy (locale-dependent roles, bulk reads), this ADR records what we do
instead. Sources: `.notes/codex-surface.md` §2 (grammar, verified against
the official helper's output) and `.notes/uia-go.md` §5 (bounded read
design).

## Decision

**Line grammar** — adopted verbatim from Codex:
`<index> <role>[ (<states>)][ <name>][ Description: <HelpText>][ Value: <value>][ on|off|indeterminate][ Secondary Actions: A, B][ ID: <AutomationId>]`
(and, from desktop-parity 04, a skeleton's `(+N children)` at the end)
with depth-first pre-order numbering from 0 at the root window, one TAB per
depth level (the root itself gets one TAB), a header line
`Window: "<title>", App: <exe file name>.`, and a tail after a blank line:
the focused Element's sentence, then `Note:` lines. All rendered strings are
whitespace-collapsed so an Element is always exactly one line.

**Divergences from Codex** (deliberate, from the orchestrator decisions and
the spec's bug list):

- **Role strings are English names mapped from the numeric ControlType**
  (wstate.RoleName), never LocalizedControlType. Codex prints localized
  roles, which breaks logic in non-English locales (ko-KR `문서` ≠
  `document`). Unknown ids render as `control(<id>)` and are never dropped.
- **Truncation is explicit and bounded** where Codex's budgets are unknown:
  3000 elements, depth 48, 1000 children per node, plus an 8 s walk budget
  (inside the 10 s request budget, leaving render margin). A child/node cut
  adds Codex's truncation suffix `(truncated: <limit>, omitted <n> children)`
  to the cut Element's line (exact counts only); time/depth cuts add a
  `Note: this Window State is partial: <reasons>.` line. A tree of ≤ 3
  uncut elements gets a nearly-empty note (canvas apps; user story 22).

**Bounded read** — the walk never issues a bulk Subtree read (Codex's freeze
class): one cache request (TreeScope Element|Children, TreeFilter
ControlViewCondition, AutomationElementMode Full, the basic property set)
applied per node via `BuildUpdatedCache`, one cross-process round trip per
expanded node, with `IUIAutomation2` transaction timeouts re-put to
min(remaining, 2 s) before each chunk. No UIA event subscriptions at all.
A worker thread wedged past the request budget is abandoned (leaked, never
killed) and replaced.

**Live-Element store** — the helper keeps the live UIA elements behind each
Window's latest Window State, parallel to the rendered Element Indexes. A
new read of a Window replaces them (releasing the old ones), so an Element
Index resolves to the same Element only within the Window State that
produced it; later Action tickets treat older indexes as stale.

**Window naming** — a Window is named by Window ID, or by App name (display
name or executable base name, case-insensitive, exact) only when that App
has exactly one Window; several matches fail with the candidates listed in
the hint. Titles never match.

## Consequences

- `internal/wstate` owns the model, the rendering and the fixture format
  (JSON, format 1) — pure and offline-testable (seam 2). `internal/uia`
  owns the COM walk; its decision logic (caps, budget, notes, DFS order) is
  generic over an element source and tested against fakes, so the risky COM
  glue stays thin.
- The grammar's rich fields (Secondary Actions, toggled/selected/expanded/
  value) render from ticket 03 but are populated by the live walk only from
  ticket 07; recorded/hand-authored fixtures pin the full format early.
- The walk budget is configurable (`WINUSE_TREE_BUDGET`) so the e2e suite
  can exercise the partial-tree-with-note path deterministically.
- Cycle detection is not implemented: the node and time caps already bound
  a cyclic provider, at the cost of a truncated tree with a note instead of
  a precise cycle marker.

## Amendment: find and partial reads add Element Indexes (2026-11-03)

Desktop-parity ticket 04, numbering decided by the user (option c, of
three: a fresh numbering that replaces the store; agent-desktop's saved,
snapshot-qualified refs with re-identification; or added indexes).

- `find <window>` walks the live tree with the same bounded chunks, up to
  20000 Elements within the read's time budget (past a printed Window
  State's 3000), and prints the matching Elements' lines, one TAB each.
  Its filters are the role (as the tree prints it), a part of the name, a
  part of the name, value or description, and a state; `--first` and
  `--count` shape the answer, and a role that matches nothing lists the
  roles the Window shows.
- `get-window-state <window> --root <element>` reads one Element's
  subtree (`--depth N` levels, `--interactive` keeps the Elements an agent
  acts on), indented from that Element.
- Both **add** the Elements they print to the Agent's live store after the
  ones its latest Window State holds: a full read that ended at 240 makes
  `find` print 241, 242, ... The full read's indexes keep resolving to
  their Elements, and stale ones still fail with the same message. A full
  `get-window-state` still renumbers from 0 and drops the added ones.
  `name:`/`id:` references resolve against the full read's Elements first
  and reach an added one only when none of those matches, so a reference
  that worked before a `find` still names the same Element after it.
- `get-window-state <window> --skeleton` is a full read three levels below
  the Window. Each Element where it stops shows its child count as
  `(+N children)` after the line, and a note says how to read a part. It
  replaces the store like any full read.
- A browser Window's Page is read over CDP whole, so `find` there filters
  a fresh full read (it says that the earlier indexes were replaced), and
  `--root` on a Page Element, or on the document the Page was spliced
  under, prints the stored subtree with the indexes it already has.
- To revisit, by the user's request: if agents are seen failing on stale
  indexes, consider agent-desktop's snapshot-qualified refs with
  re-identification.

### Addendum: live re-resolution and one index per Element (desktop-parity 17)

- A `name:`/`id:` reference resolves against the Agent's cached Window
  State first. When that cannot answer (no Window State, no match, or a
  matching Element whose RuntimeId no longer reads), the same match rules
  run over a fresh bounded walk of the Window, and the Element found is
  added like a `find` match, with a note in the Action's output. Several
  matches still refuse; the fresh read's candidates are added and listed
  under their new indexes. Element Indexes are never re-resolved. An Agent
  whose Window State is missing because another Agent read the Window is
  still refused (ADR 0014 Decision 2).
- The added Elements are keyed by their UIA RuntimeId, so `find`, `--root`
  and a live resolution that reach an Element already added get the index
  it already has instead of a duplicate. The full read's own Elements are
  not keyed (reading every RuntimeId would cost a round trip each).


## Amendment: output budgets are views of a whole read (speed 05, 2026-11-05)

- A full read takes `--interactive`, `--depth N`, `--compact`,
  `--max-chars N` and `--out <file>` (`internal/wstate/view.go`). They
  shape the printed text only. The walk, its caps and the stored Window
  State are the same as a plain read's, so every printed line keeps the
  full read's Element Index and an Element left out still resolves.
- The grammar is unchanged. A view prints each Element one level below its
  nearest printed ancestor and ends the tail with one `Note:` saying how
  many Elements it left out. The root and any line with a cut mark
  (`(truncated: ...)`, `(+N children)`) always print. `--depth` adds
  `(+N children)` where it stops, as the skeleton does.
- `--compact` folds only an Element whose line would carry nothing but its
  index and role. An unnamed container with a unique ID keeps its line:
  `--root id:<ID>` may need it.
- `--max-chars` cuts whole lines and never the header or the tail (the
  focused Element and the walk's notes). `--out` writes the view to the
  file and prints the header, a `Window State: <path> (...)` line and the
  tail.
- `find --limit N` keeps the first N matches and notes how many matched.
  `--first` is `--limit 1`.
- The defaults stay Codex's text. Measured on 14 real Window States from
  transcripts (o200k as a proxy; the notes cost included): `--interactive`
  −36.5%, `--compact` −11.4%, both −38.1%, `--depth 3` −67.5%. Folding
  bare lines alone is −5.8% and dropping shared IDs alone −5.3%. Changing
  a default is left to the user (`.scratch/speed/issues/05-output-budgets.md`).

### Addendum: two budgets are the default (speed 13, 2026-11-05)

The user chose two of the proposed defaults
(`.scratch/speed/issues/13-default-budgets.md`):

- **Shared IDs are left out.** Every read, `find` and `--root` leave out
  the `ID:` of an Element whose ID another Element of the same read
  shares (for `find`, of its walk), the focused Element's sentence
  included. `id:` refuses those IDs as ambiguous, so the line loses
  nothing an agent can name. A unique ID always prints. Folding stays
  opt-in under `--compact`, except before the cap (next bullet).
- **The output stops before 28000 characters.** Claude Code cuts a Bash
  command's output at about 30000 characters, in the middle. A read now
  cuts at a line boundary first, with the `--max-chars` note naming the
  first Element not printed. The cap covers the note itself and, with
  `--screenshot`, room for the Screenshot line, and leaves about 2000
  characters for the shell's own lines. `--max-chars N` sets the cap,
  `--max-chars 0` lifts it, and `--out <file>` writes the read uncut.
  **Fold before the cut** (reliability-council 12, 2026-11-07): a full
  read the cap would cut first folds its bare containers as `--compact`
  does, with a note, and cuts only what still does not fit. Measured on
  Discord (no Browser Connection): 784 Elements, a third unnamed
  wrappers; the cut read printed 306 non-wrapper lines, the folded one
  458, and the whole read shrank from 40,309 to 29,732 characters. A
  read that fits prints as before; `--out` and `--max-chars 0` never
  fold.
- **`--full`** prints Codex's text byte for byte: every ID, no cap
  (`wstate.Render`). It refuses `--interactive`, `--depth`, `--compact`
  and `--max-chars` on a full read. With `--root`, `--depth` and
  `--interactive` still shape the walk. `find --full` does the same for
  find.
- The cap counts the text's characters; under `--json` the escaped text
  is longer.
- Measured on the same 14 real Window States: the default prints 5.3%
  fewer tokens (−4.8% characters). Six of the 14 change, by −1.3% to −9%
  each. The largest is 17,260 characters, so the cap never cuts any of
  them; it only matters on dense Windows (about 40k tokens at the
  3000-Element cap).

## Amendment: an Action's --observe is a full read (speed observe, 2026-11-05)

An Action or Batch sent with `--observe` reads its Window after it as a
full read, so it renumbers from 0 like `get-window-state`. Its diff lists
every Element Index whose line changed, appeared or went since the Agent's
latest Window State, so an index it leaves out still names an Element with
the same line. ADR 0021 has the baseline, the 60% fallback and why indexes
stay positional. The observation takes the read's defaults (shared IDs left
out, the 28000-character cap) and --full.

## Amendment: one slow child list no longer ends the read (learn-from 08, 2026-11-15)

Before, a chunk that hit its transaction timeout (2 s) stopped the whole
walk: one huge list (a classic list box of 20,000 items, a panel of
thousands of buttons) left a partial tree with nothing after it. UI
Automation cannot cap how many children one cached read returns
(agent-desktop #643), so the cap of 1000 children only applied after all
of them came back.

- **A timed-out chunk cuts that node only.** Its children are then read
  one at a time through the control-view tree walker
  (`GetFirstChildElementBuildCache`, `GetNextSiblingElementBuildCache`,
  the tree properties for the child alone), each step bounded like a
  chunk, up to the 1000-children cap, and the walk goes on with the
  node's siblings. A list whose children all fit under the cap is read
  whole, with no note. All of it stays inside the walk's budget
  (`WINUSE_TREE_BUDGET`) and the element cap, which still stop the walk.
- **A provider that does not answer one step stops the walk**, as a
  timed-out chunk did: the App is not answering, not just slow on a long
  list.
- **The note.** No new grammar. A cut slow list is named in the
  partial-tree note, after the other reasons, by its Element Index:
  `the children of Element 12 were too slow to read at once, so the first
  1000 were read one at a time and 19000 more items were not read`. The
  count is the last read child's SizeOfSet minus its PositionInSet when
  its provider gives both, else "and the rest were not read". A list
  whose steps failed from the start: `the children of Element 12 did not
  answer in time and were not read`. Its line gets no
  `(truncated: ...)` suffix, which stays for exact child counts. `find`
  and `--root` name it by the index they added it as, or by its role and
  name when they did not print it.
- **Skeleton counts read nothing per child.** `(+N children)` comes from a
  second cache request with no properties and AutomationElementMode
  None, bounded to 250 ms per count. A count that runs out of time prints
  no number, the walk goes on, and a note (not the partial-tree note: the
  skeleton's shape is chosen) says `the children of Element 12 were not
  counted in time`. Before, a count fetched every child with every
  property and a timed-out count stopped the walk. The Grid pattern's
  RowCount was not used: it is not the child count (header rows, groups)
  and needs another cached property on every Element.
- **Not changed:** a list that comes back as one chunk is still read
  that way and cut at 1000 with the exact suffix; collection types are
  not stepped up front (agent-desktop does): a stepped list costs one
  round trip per child, which small lists, the common case, do not need.
  The live cost of both routes on a 20,000-item list box is measured by
  `winusetasks latency` on the fixture's `-huge-list` Window
  (`.scratch/learn-from/issues/08-huge-lists-in-tree-reads.md`).

## Amendment: hints on unnamed or duplicate controls (learn-from 04, 2026-11-15)

Agents pick the wrong control when several print the same line
(`button Delete` in every row) or when one has no name (an icon button).
agent-browser's ab#2578 adds `hint="icon:trash; in:Blue mug"`. winuse adds
the same information as one more field, the last on the line:

`...[ (+N children)][ Hint: <own>[, in <row>]]`

```
		12 button Hint: icon trash
		14 button Delete Hint: in Blue mug
		19 button Delete Hint: in Red mug
```

- **Who gets one.** A control (button, split button, link, check box,
  radio button, menu item, edit, combo box, slider, spinner, clickable)
  with no name, or whose role and name another control of the same read
  shares. Nothing else.
- **`<own>`** is what the reader knew besides the line, from data it
  already had. On a Page it comes from the DOMSnapshot the read takes for
  the clickables: `icon <name>` (a `<use href="#x">` sprite, an `<svg>`'s
  `data-icon`, `<title>` or id, an icon library's class), `img <file>` (an
  `<img>` inside), and the first of `testid`, `name` or `id`. A value with
  three digits in a row, or a React id, is left out as generated. UIA has
  no icon names, so a native control has none. Each part is cut to 40
  characters.
- **`in <row>`** is the text of the row, list item or card the control
  sits in, cut to 40 characters. On a Page it is the DOM's nearest `tr`,
  `li`, `article`, `[role=row]`, `[role=listitem]`, `[role=article]` or
  element whose class names a card, without the control's own text.
  Otherwise it is the nearest of the control's ancestors in the read (up
  to six levels, never the Window) that holds no other control of the same
  line, and whose text no such control's ancestors have. An ancestor's text
  is its name or, for an unnamed one of at most 16 Elements, the names in
  it.
- **Only where it tells controls apart.** A part two such controls share is
  left out, except an unnamed control's own part, which says what the
  control is. The default full read prints `<own>` only, since the row is a
  few lines up. `--interactive` and `--depth` add `in` unless the row is a
  named Element the view prints; `--root` adds it under either. `find`, an
  `--observe` diff line and an ambiguous reference's candidates always add
  it, as they print no rows. `--compact` keeps a bare line that has a hint.
- **Not a name, not Codex's.** `name:`, `id:` and `find`'s filters never
  match a hint, and a diff compares lines without it. `--full` (Codex's
  text, `wstate.Render`) prints none, so its line stays byte for byte.
- **No reader calls.** The Page parts come from the same captureSnapshot
  answer, in Go (`internal/page/hints.go`), at most 300 per document; a
  read through the clickables.js fallback has none. The tree's row part is
  computed from the read's Elements (`wstate.Hints`). Neither sends a CDP
  or UIA call per Element.
- **Measured** (`.scratch/learn-from/issues/04-control-hints.md`): on 41
  real Window States and the 2 fixtures (o200k tokens), the default read
  +0.0%, `--interactive` +0.64%, a `find` of every control +2.1%. The Page
  hint pass costs about 0.2 ms on the 200-clickable benchmark page and
  0.7 ms with 400 duplicate controls, against a 19-24 ms Page read.

## Amendment: heading levels and link URLs on Page lines (bladebro-ideas 01, 2026-11-24)

getFullAXTree already carries a heading's `level` and a link's `url`; the
Page lines printed neither, so agents ran `eval` to learn where a link
goes, and `find --role heading` could not show an outline.

- **Heading level: a state word.** A Page heading's line ends its states
  with `level N`: `heading (level 1) Sign in to Example`,
  `heading (disabled, level 2) ...`. It is in every view but `--full`:
  `--full` is Codex's text byte for byte, and Codex prints no level, so
  it leaves the word out (Element.Level is still read). A UIA heading has
  no level, so native Windows print exactly as before.
  `find --role heading` is the Page's outline.
- **Link URL: a field, only where asked.** `...[ URL: <url>][ Hint: ...]`,
  after the rest of Codex's line and before a hint. The URL is shortened
  by `page.LinkURL`: the Page's own origin (scheme, host and port) prints
  the path, query and fragment (only `#fragment` for a place on the same
  Page); another origin prints its host without `www.` and the path.
  Every URL is cut at 60 characters with `…`. A `javascript:` URL prints
  nothing; a `data:` URL prints `data:…`. `read` shares the rule
  (`page.SiteURL`) with the Page's site (its registrable domain) in place
  of its origin, and leaves a URL on that site uncut.
- **Where it prints.** Not in a default read: `--urls` adds it to a read,
  `--root` or `--observe`. `find`, a `--diff`/`--observe` diff line and an
  ambiguous reference's candidates always print it, as they print the
  whole hint: their lines have no tree around them, and they are short.
  `--full` prints none and refuses `--urls`; `find --full` prints none.
  `--compact` keeps an unnamed link's line when it prints a URL.
- **Diffs.** `wstate.Diff` compares lines with their URL, so a link whose
  address changed is a changed line, and the diff line shows both.
- **Why opt-in** (estimated in
  `.scratch/bladebro-ideas/issues/01-page-link-urls-and-heading-levels.md`):
  on three link-heavy real pages (Hacker News, a GitHub repository, an
  MDN reference page) a URL on every link would add about 40-50% to the
  read's characters, more in tokens, since URLs tokenize densely. `read`
  measured full URLs at half its output (ticket 12). The agent that needs
  addresses asks with `--urls` or `find --role link`.

## Amendment: Page landmarks, folded before the cut; the cut note by role (bladebro-ideas 08, 2026-11-24)

On a big Page, site chrome (nav menus, headers, footers, sidebars) took
much of the read, and an unnamed landmark had no line, so its links read
like content. The cut note gave a count and an index only.

- **Landmark roles.** A Page's `main`, `navigation`, `banner`,
  `contentinfo` and `complementary` are always lines, named or not, with
  their own role words: `main`, `navigation`, `banner`, `footer`,
  `complementary` (a named one used to print as `group`). Other landmarks
  (`region`, `search`, `form`, ...) stay `group` lines only when named.
  UIA has none of these roles, so native Windows print as before.
- **Folding: a view, only under the cap.** After the bare-container fold,
  a full read still too long for its cap folds, on a Page with main
  content (a `main` or `dialog` Element), each `navigation`, `banner`,
  `footer` and `complementary` that holds 3 or more Elements, the outer
  one of two nested:
  `...[ (+N Elements folded; --root <index> shows them)]`, after the rest
  of the line (on a Tab target, which takes no `--root`,
  `; --out <file> writes them` instead). The
  note names the fold: `folding unnamed containers (as --compact does)
  and Page landmarks to fit --max-chars N left out ...`. The stored Window
  State stays whole and every folded index resolves; `--root` on a Page
  Element prints it from the stored read.
- **Never folded:** a landmark that holds the focused Element, main
  content, or an Element whose line changed since the Agent's earlier
  Window State of the Window or Tab (by `wstate.Diff`, which a plain read
  now takes too). When more than `DiffThreshold` of the read changed, the
  earlier read was another page and nothing counts as changed.
- **Not the default** (measured below): a read that fits prints every
  link, as an agent that needs a nav link should not pay a `--root` round
  trip; so no flag turns it off. `--max-chars 0` and `--out` never fold.
- **Diffs unchanged:** `--diff` and `--observe=diff` lines compare and
  print the whole State; only a whole read printed in a diff's place
  folds.
- **The cut note by role.** `(from Element N on: 30 link, 8 button, 3
  edit)`: the lines left out, the most first, then by role name, so a page
  always gives the same text; past five roles the fifth is `other`. It is
  every cut's (`find`, `--root`, diffs, native reads). A heading's
  `level N` is its state, not its role. The output still fits the cap:
  when the counts make the note longer, one line fewer prints.
- **Measured** (estimated from five real pages' link lists; this run
  could not start Chrome): the chrome's share of a read is large on
  documentation with a sidebar (MDN about 75%) and small on articles and
  long docs (Wikipedia about 20%, Python's `json` docs about 6%,
  go.dev's Effective Go about 3%). See
  `.scratch/bladebro-ideas/issues/08-fold-landmarks-and-cut-roles.md`.
