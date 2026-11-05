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
  index and role, and drops only the `ID:`s that several Elements share,
  which an `id:` reference refuses as ambiguous anyway. An unnamed
  container with a unique ID keeps its line: `--root id:<ID>` may need it.
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
