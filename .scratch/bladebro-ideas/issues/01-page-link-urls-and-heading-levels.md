# 01: Link URLs and heading levels on Page lines

Status: ready-for-agent
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
