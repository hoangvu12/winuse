# 25: `winuse read`: a Page's main content as Markdown

Status: ready-for-agent
Type: task
From: tickets 12 and 17

## Ask

- **The command.** `winuse read <window>[:<tab>] [--root <el>]
  [--urls full] [--max-chars N | --out <file>]`, Pages only.
  - It prints Markdown with no Element Indexes and leaves the Window State
    alone.
  - `--root` takes an Element Reference from the agent's latest read and
    renders that subtree, through `Runtime.callFunctionOn` on its node.
  - On a native Window it is refused, and the hint says to use
    `get-window-state`.
- **Start from the port** in `../prototypes/extract/read_markdown.js`, with
  `LICENSE.bladebro`. It's
  Apache-2.0: keep the attribution, and run it in an isolated world. Fix
  what ticket 12 found:
  - **URLs:**
    - same-site links print as paths;
    - external links print as domain plus path, cut if long;
    - no image URLs;
    - `--urls full` prints them unshortened.
  - **Main content:** rank every candidate (`main`, `[role=main]`,
    `article`, common selectors, text density) instead of taking the first
    `<main>` over 200 characters. Never return empty: DuckDuckGo html
    returned nothing.
  - **Headings:** keep the page h1 even when it sits in `<header>`, and drop
    `[edit]`-style links.
  - **Tables:** render with headers. No silent 50-row cutoff; a cut is
    disclosed with its bound.
  - **Block walls:** say so (ticket 05's check).
- **Bound.** It prints a `(cut at N of M characters)` line when it is
  bounded, the same as other reads.
- **Docs.** Add it to `browser.md` and `commands.md`, plus one line in
  SKILL.md's command table: "read a Page's text: `read`, not
  `eval innerText`". Ticket 14 found 396 `innerText` evals.
- **Tests.**
  - Golden files from saved HTML for: a Hacker News-like table page, an
    article, a search-results page where every result is a link, a page
    with a misleading `<main>`, a table page, and a docs page.
  - An e2e test on one live fixture page.
