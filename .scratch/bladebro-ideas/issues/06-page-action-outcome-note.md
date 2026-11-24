# 06: Say what a Page click or key did

Status: resolved
Type: task
From: `map.md`; bladebro `bb/src/action/perform.rs:61` (an observer armed before the click), and verdicts at `bb/src/action/verdict.rs:211-414,955-1002`

## Problem

A Headless Page click or key prints `ok` plus notes. When it navigated,
changed only text, or did nothing at all, the agent can't tell without a
read. `--observe` helps, but:
- its accessibility-tree diff doesn't say "navigated";
- it misses changes that touch only text;
- it reads before the Page settles (ticket 07).

So an `ok` that did nothing leads to a repeat, and a repeat can act twice.

## What bladebro does

Before the click, it arms a MutationObserver that also covers open shadow
roots and same-origin iframes. After the click it waits for the page to
settle, then prints one of these:
- navigated → the URL;
- the DOM changed: +added and −removed nodes, or text only;
- a toggle's state changed;
- no-effect (nothing was sent);
- "clicked (no observable DOM change)", naming what sits at the click point.

It never clicks twice.

## Ask

- **Arm a cheap observer** before a Headless Page click, key or chord
  (`internal/helper/pageactions.go` and its run path). It counts mutations
  in the target's document, open shadow roots and same-origin frames, and
  notices a navigation (loaderId or URL change).
- **After delivery and a short settle** (ticket 07), add at most one note.
  It stays `ok`, because the input was delivered.
  - `Note: the Page navigated to <url>`
  - `Note: the Page changed (text only)`. Decide whether to print the
    counts.
  - `Note: no change seen in the Page; the click was delivered, so read the Page before repeating it`
- **When nothing changed** and the click point is now covered, or the
  target is gone, name that, reusing `NotActionableError`'s blocker wording
  (`internal/page/input.go:77-96`).
- **Never re-send** anything because no change was seen. That would break
  single dispatch and Delivery.
- **Under `--json`,** add an `effect` field with one of `navigated`,
  `changed`, `text`, `none` or `unknown`.
- **Agree with `--observe`.** When `--observe` is given, its diff still
  prints. The note shouldn't repeat it, but "navigated" is still worth
  saying.
- **Tests:**
  - fake CDP tests per outcome;
  - e2e fixtures: a link, a button that changes text, a button with no
    handler, and a click inside a shadow root.

## Answer

Built on branch `agent/bladebro-ideas` (merge cf7ea0d; review fixes in 081a150). A Page click or press-key watches the Page (isolated world, no Runtime.enable) and ends `ok` with at most one note: navigated, still navigating, changed (text only), or no change seen. Nothing is re-sent. `--json` has `effect`. After the review the watch uses page.Settle's quiet rule and `--observe` reuses it.
