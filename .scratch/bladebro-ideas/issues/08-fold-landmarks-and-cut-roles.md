# 08: Fold Page landmarks; break the cut note down by role

Status: ready-for-agent
Type: task
From: `map.md`; bladebro `bb/src/page/model.rs:556-614` (folding), `:631-641` and `:979-990` (the omitted-by-role note)

## Problem

- **Site chrome reads like content.** On a big Page, nav menus, headers,
  footers and sidebars take up much of the read. An unnamed landmark has no
  line of its own (`internal/page/roles.go:102-113`), so its links read
  exactly like content links. Example: a news article whose read is 70%
  nav.
- **The cut note is vague.** When a read hits its budget, the note gives
  only a count and the first left-out index (`internal/wstate/view.go:168`),
  so the agent can't tell whether the cut held buttons or just links.

## What bladebro does

- **Folding.** When the page has a content landmark (main, dialog or
  search), each nav, banner, footer or aside with 3 or more items collapses
  to one line:

  ```
  nav ▸ N items folded (filter=nav to expand)
  ```

- **Cut note.** It ends with `…(N more: 12 link, 3 button)`, sorted so the
  same page always gives the same text.

## Ask

- **Fold as a view, like `--compact`** (`internal/helper/views.go`). The
  stored Window State stays whole, and the indexes of folded Elements still
  work and still resolve.
  - Fold only when the Page has a main or dialog landmark.
  - Never fold the landmark that holds the focus, or one that holds an
    Element changed since the last read.
  - The fold line names the landmark and its item count, and points at
    `--root <index>` to expand it.
- **Fold before cutting,** so that hitting the budget drops chrome first.
- **Default or opt-in?** Decide whether folding is the default for Page
  reads, or happens only under budget pressure like `--compact`. Measure on
  5 real pages and put the numbers in the ticket. If it's the default, give
  a flag to turn it off.
- **Cut note:** list the left-out Elements by role, e.g.
  `(cut 41 Elements from 212: 30 link, 8 button, 3 edit)`. This is
  worthwhile on its own and applies to native reads too.
- **Diffs.** `--diff` and `--observe` diffs are unchanged: they diff the
  whole State, not the view.
- **Docs.** Update the golden files and `reading.md`, plus an ADR 0004 note
  if the format changes.

## Chrome share on five pages (2026-11-24, estimated)

Estimated, not measured live: this run could not start Chrome (no winuse
or browser processes). Each page's links were counted by region from its
fetched HTML, as `navigation`/`banner`/`footer`/`complementary` hold them;
menus that are closed (mega-menus, the mobile nav, Wikipedia's language
list) were left out, as the AX tree leaves them out. A line is taken at
about 45 characters for a link and 70 for a text run. BBC News, AP and
Reuters refused the fetch, so no news front page is in the set.

| Page | Chrome links | Content links, text | Read (est.) | Chrome share | Over 28,000? |
|---|---|---|---|---|---|
| MDN `<nav>` reference | ~300 (sidebar 258, header ~15, footer 29) | ~52, ~3,800 chars | ~26,000-32,000 (with list items) | ~75% | near or over |
| Wikipedia "Landmark" | ~85 (menus, tools, footer 30) | ~140, ~4,500 chars | ~18,000 | ~20% | no |
| github.com/golang/go | ~55 (header, repo tabs, footer) | ~60 plus the sidebar ~14 | ~9,000 | ~30% | no |
| docs.python.org `json` | ~43 (nav bars, sphinx sidebar, footer) | ~160, ~18,000 chars | ~30,000+ | ~6% | yes |
| go.dev Effective Go | ~60 (header, footer) | ~28, ~85,000 chars | ~90,000 | ~3% | yes |

**Decision: fold only under the cap, not by default.** Three of the five
reads fit the cap, where folding would hide nav links an agent may need
and cost a `--root` round trip to get back. Where a read is cut, folding
the chrome frees about 75% of an MDN-style docs page for content. On a
long article it frees little, but whatever it frees now goes to content
first. Since it is not the default, no flag turns it off: `--max-chars 0`
or `--out` read every line. Built in `wstate.RenderView` after the
bare-container fold. ADR 0004 has an amendment with the format.

Unnamed landmarks now have lines of their own (`main`, `navigation`,
`banner`, `footer`, `complementary`). This adds about one line per
landmark, roughly 5 lines and 100 characters on a typical page, and it
lets an agent tell chrome links from content even when nothing folds.
