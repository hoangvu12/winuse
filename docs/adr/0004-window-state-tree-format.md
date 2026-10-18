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
