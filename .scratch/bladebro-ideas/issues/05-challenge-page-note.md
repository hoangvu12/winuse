# 05: Note a challenge or block page

Status: ready-for-agent
Type: task
From: `map.md`; bladebro `bb/src/page/js/detect_block.js`, `bb/src/page/perception/block.rs`

## Problem

The skill's Safety rule tells the agent to stop and ask the user at a
CAPTCHA or an auth wall, but the agent has to notice one first. Usually it
reads the tree, finds nothing useful, tries to click, and only then works
out that the page is a Cloudflare interstitial. This costs turns, and
sometimes a click on the challenge widget, which the agent should never
make.

## How bladebro avoids false alarms (detect_block.js)

It flags a page only when two conditions hold together:
- the page is **small**: its body text is under roughly 800–1,500
  characters, depending on the wall;
- it shows **a specific sign:**
  - a title such as "Just a moment";
  - a challenge form, e.g. `#challenge-form`;
  - a challenge iframe, e.g. `iframe[src*=captcha-delivery]`, `#px-captcha`
    or a reCAPTCHA together with "prove you're human" wording;
  - "access denied" together with "reference" or "akamai";
  - "too many requests" or "rate limit".

Because of the small-page rule, a docs page that mentions "rate limit", or a
login form with a Turnstile widget, is not flagged.

## Ask

- **When to check:** after `navigate`, `back`, `forward` and `reload`, and
  after a Page Action whose effect navigated (ticket 06).
- **The check:** a port of the generic part of `detect_block.js`, run in
  one evaluate, with attribution. Leave out the Reddit-specific branches and
  every solving or remediation step.
- **On a hit,** add
  `Note: the Page looks like a <Cloudflare|DataDome|PerimeterX|reCAPTCHA|Akamai|rate-limit> wall; ask the user to handle it`.
  - It stays `ok`.
  - `--json` gets a field.
  - The note matches the skill's Safety wording.
- **Never click or solve anything.** The skill already says to stop at
  CAPTCHAs; add a line pointing at this note.
- **Tests:**
  - fixture pages for each wall kind, plus negatives: a long docs page that
    mentions "rate limit", and a login form with a Turnstile widget;
  - fake CDP tests.
