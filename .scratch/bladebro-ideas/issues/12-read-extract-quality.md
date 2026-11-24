# 12: Generic `read` and `extract` on real pages

Status: resolved
Type: prototype
From: `map.md`; bladebro `bb/src/page/js/markdown.js` (main content to Markdown), `bb/src/mcp/server/js/auto_extract.js` (repeated-item scorer), `bb/src/adapters/js/jsonld.js`

## Question

Leave out bladebro's site adapters and site-specific branches. How good are
its generic heuristics on about 12 varied real pages?

- **read:** Did it pick the main content? What junk got in, and what was
  lost? How big is it, in tokens, against `innerText` and against the AX
  tree?
- **extract:** Did it find the right list, with the right item count and
  usable fields? Did partial detection work?

The ported scripts are kept under the chat's scratch folder `exp\extract\`
for reuse. This blocks ticket 17.

## Answer

13 pages, sizes in tokens at chars/4. npm, eBay, Newegg, PyPI and Target
served bot walls, so RubyGems, IKEA and Allbirds stood in for them. Ported
scripts and per-page outputs are in the chat's scratch folder:
- `exp\extract\js\read_markdown.js`
- `extract_list.js` (with `table` and `jsonld` modes)
- `LICENSE.bladebro`
- `run.mjs`
- `out\<page>\`

**Hit rates**
- **read:** 7 good, 4 partial, 2 wrong.
  - Wrong on DuckDuckGo: empty output.
  - Wrong on Allrecipes: it took a "Most-Saved" `<main>` and lost the
    recipe.
- **extract:** 5 good, 3 partial, 5 wrong (unscoped).
  - Good: GitHub search, IKEA, Stack Overflow, DuckDuckGo, the HN Algolia
    SPA.
  - Wrong: Wikipedia (picked the references, not the table), the Guardian
    (section blocks), Allbirds (a footer column), MDN (the sidebar),
    Allrecipes (reviews; its JSON-LD Recipe was perfect but never reached).

**Size:** read averages about 2× `innerText`, because full URLs are 50% of
its characters (69% on HN).

**Failure patterns**
- **Main-content selection.** The first `<main>` wins, and the link-penalty
  fallback picks a search form when every result is a link. The h1 is
  dropped as part of the header.
- **Card grouping.** Cards are grouped by their exact child tags, so
  optional children split one grid into fragments, and the two rows of one
  HN story become two items.
- **Scoring.** It prefers off-site links, so references beat tables and
  navs beat nothing.
- **URL dedupe** drops real table rows: 83 of 115 on Wikipedia.
- **Bot walls** aren't flagged; they come out as 3–13 "items".
- **"Page claims N results"** fired correctly 3 times with no false
  positives, but missed "10.3k", "24,134,885 questions" and "1–30 OF 208".

**Scoped to an element**
- Scoping to the list or any ancestor is right on 6 of 6 pages, and fixes
  the wrong picks (Wikipedia table, 115 of 115 rows with headers; Allrecipes
  ingredients, 11 of 11).
- Scoping to a single item fails, so treat one item as an example and
  gather its similar siblings.
- read works scoped as it is.

**Changes needed from bladebro**
- **read:**
  - Shorten URLs (same-site paths, or numbered footnotes; drop image URLs).
  - Rank every main-content candidate instead of taking the first match.
  - Keep the h1 and drop `[edit]`.
  - Render tables with their headers.
- **extract:**
  - Group cards loosely: same tag and class, or most children shared.
  - Merge sibling rows of one record.
  - Dedupe only on the title link.
  - Prefer JSON-LD over a weak list.
  - Say "no list" on article and doc pages.
  - Flag bot walls (ticket 05).
  - Count patterns: accept "k" counts and nouns such as "questions".

**Verdict**
- **read: ship**, after URL shortening and the main-content fixes.
- **extract: ship scoped**, with a `--root <element>` that's either required
  or strongly preferred, plus direct `table` and `jsonld` modes. Don't
  trust bladebro's unscoped pick on its own.
