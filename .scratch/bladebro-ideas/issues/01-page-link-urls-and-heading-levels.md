# 01: Link URLs and heading levels on Page lines

Status: resolved
Type: task
From: `map.md`; bladebro `bb/src/page/model.rs:906-910` (`→ href` on each line), outline mode `bb/src/page/perception/content.rs:146-152`

## Problem

A Page's Window State prints `link Forgot your password?` with no address,
and `heading Sign in to Example` with no level. The agent has to `eval` to
learn where a link goes, and `find --role heading` can't show a page's
outline.

winuse already receives both. The CDP AX tree carries `url` on links and
`level` on headings (`internal/page/testdata/login-form.json:15,27`), but
`builder.element` (`internal/page/build.go:292-366`) reads neither, so
`internal/page/testdata/login-form.txt:9,14` prints neither.

## Ask

- **Headings.** Print the level on a Page heading line, e.g.
  `heading (level 1) Sign in to Example`. Pick a form that fits ADR 0004's
  state vocabulary.
- **Links.** Print the URL on a Page link line.
  - Shorten a same-origin URL to its path.
  - Cut a long one with a disclosed bound.
  - Never print `javascript:` URLs; print `data:` URLs only as `data:…`.
  - Decide whether the URL is always shown or only with a flag such as
    `--urls`. Measure the token cost on 3–5 real pages first, and put the
    numbers in the ticket.
- **Codex format.** Keep `--full` / Codex-format output byte-for-byte if
  ADR 0004 requires it. Check that before choosing.
- **`find`.** `find --role heading` should then list the outline with levels.
- **Diffs.** `--diff` and `--observe` treat a changed URL as a changed line.
- **Docs.** Update the golden files (`testdata/*.txt`), `reading.md` and
  `browser.md`. Add an ADR 0004 amendment if the line format changes.

## Out of scope

URLs on native hyperlinks.

## URL token cost (2026-11-24, estimated)

Estimated, not measured live: this run could not start Chrome. The link
lists of three real pages were fetched (their `<a href>`s) and shortened by
`page.LinkURL`'s rule; links in closed menus and `<details>` were left
out, as the AX tree leaves them out; the rest of each read was estimated
from its structure.

| Page | Links (in the tree) | Avg shortened URL | Added by ` URL: ...` | Read (est.) | Growth |
|---|---|---|---|---|---|
| news.ycombinator.com | 229 (229) | 24 chars (most are same-site `/item?id=...`, `/vote?...`) | ~6,600 chars | ~13,700 | ~+48% |
| github.com/zed-industries/zed (logged out) | 226 (~155) | ~38 (same-site file paths) | ~6,800 | ~13,000 | ~+50% |
| developer.mozilla.org `<a>` reference | 539 (~340) | ~40 (same-site doc paths) | ~15,600 | ~37,000 | ~+42% |

A Wikipedia article (fourth page) was too long to list (214,616 characters
of Markdown). Tokens grow more than characters, since URLs tokenize at
about 3 characters a token against about 4 for prose. Ticket 12 found full
URLs were half of `read`'s output (69% on HN).

**Decision: opt-in.** `--urls` on a read, `--root` or `--observe`; `find`,
diff lines and ambiguous candidates always carry the URL (their lines have
no tree around them, as with hints). `--full` prints none. Heading levels
are always shown (a `level N` state word: a few characters per heading).
ADR 0004's amendment records the format.

## Answer

Built on branch `agent/bladebro-ideas` (merge c06419a; review fixes in 081a150). Page headings print `(level N)`; link URLs print with `--urls` (and always in `find`, diffs and ambiguity lists), shortened by `page.LinkURL` (same-origin = scheme+host+port). `--full` stays byte-for-byte. Cost figures are estimates (+42–50% with URLs on), so URLs are opt-in.
