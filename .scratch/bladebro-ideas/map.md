# What winuse can take from bladebro

Started 2026-11-24, from bladebro at `d50ee9e` (v4.8.3, 2026-11-21),
https://github.com/dondai44423/bladebro. Citations use the prefix `bb/`, for
example `bb/src/action/verdict.rs:113`.
Permalink base: https://github.com/dondai44423/bladebro/blob/d50ee9e/

## Notes

- **What bladebro is.** A Rust CDP driver for agents: five MCP tools (`act`,
  `see`, `state`, `run`, `vision`) in one binary. Most of its code goes to
  reading the open web, including sites behind anti-bot protection:
  - stealth injection plus humanized mouse and typing input, scored against
    an external anti-bot benchmark (`bb/STEALTH_BENCH.md`);
  - extraction, pagination and site adapters;
  - resource blocking for speed;
  - headless Linux servers on Xvfb.

  Its default lane launches its own Chromium. `rb on` drives the user's
  browser with page injection off, which is winuse's model.
- **Shared ground.** It already shares winuse's principles: single dispatch,
  readback before reporting success, disclosed read bounds, and refusing to
  guess on an ambiguous target (`bb/AGENTS.md` Hard Invariants 7 and 8). What
  carries over are smaller additions, mostly to Pages.
- **Already covered or decided.** Ideas `.scratch/learn-from/` already
  shipped (wait before refusing, ARIA combobox, `--stdin`/`--file`, observing
  the opened Window) or rejected (`read <url>`, durable refs) are left out.
  bladebro's `changes: true` is `--diff`, and its `scope` is `--root`.
- **One lesson from its stealth work that applies here:**
  `bb/src/stealth/mod.rs:3` avoids `Runtime.enable` because DataDome detects
  it, and winuse's Page code never sends it either (only
  `internal/browser/electron.go:151` does). Console capture must keep it that
  way (ticket 11).

## Not adopting

- **Stealth and fingerprint masking, humanized input.** These serve a launched
  browser on a server. winuse runs in the user's real browser on real
  hardware.
- **Automatic cookie-banner dismissal** (`bb/src/page/perception/consent.rs`).
  It changes the user's privacy choices without asking, which the skill's
  Safety rule forbids. Naming the banner's buttons in a hint stays possible
  (ticket 16).
- **Saved sessions that dump cookies and storage**
  (`bb/src/state.rs`, `bb/src/logins.rs`). A risk in the user's own profile.
- **Loose name matching, click fallbacks (js, mouse, Enter, Space) and
  fingerprint-healed refs** (`bb/src/mcp/server/resolve.rs:31-98`,
  `bb/src/action/perform.rs:193`, `bb/src/page/heal.rs`). They break
  "exact, never a guess". bladebro's own comments record a forwarded click
  firing a Delete button.
- **Site adapters** (`bb/src/adapters/js/*`, about 2,000 lines). They break
  whenever those sites change.
- **Artifact paging and `items: true`.** These exist for MCP clients that
  can't read files. winuse's `--out` covers it.
- **`body: true` fetch and PDF export.** The agent's shell already does the
  first; the second is low value.

## Decisions so far

(Resolved tickets append a line here: gist + link.)

- **15** (2026-11-24): the facade bug reproduces, a false `delivered, do not
  repeat`. The repro also found that multi-line `set-value` into any
  contenteditable fails its readback, and that `type-text --into` silently
  scrambles text on a remounting editor. Ticket 09 is unblocked and
  rewritten. [15](issues/15-facade-editor-repro.md)
- **11** (2026-11-24, Chrome 155):
  - `Log.enable` catches only network, security and CSP entries, never
    console calls or exceptions. It replays, and pages couldn't detect it.
  - `Runtime.enable` is still detectable by timing (a 30–40× slowdown).
  - A main-world listener installed by `addScriptToEvaluateOnNewDocument`,
    passing data to an isolated world, catches exceptions without Runtime.
  - `responseStatus` gives the HTTP code with no domain, including on
    Chrome's own error page.
  - Ticket 02 is unblocked; ticket 19 can be grilled.
  [11](issues/11-console-without-runtime.md)
- **14** (2026-11-24): 172 real sessions ran winuse, in about 5,290 calls.
  - **Fixed waits:** 43% of calls had a fixed `sleep` or `wait` (about 3.4 h
    in total), on claude.ai, localhost, AWS, Zeron and Discord.
  - **Page text through `eval`:** 857 calls in 71 sessions.
  - **Silent `ok` clicks:** about 12 sessions.
  - **Error pages after `navigate`:** 9 sessions.
  - **Scrolls past the end:** 1 session. **Batch typos:** none.
  - **New lead:** native `set-value` false failures (ticket 20).

  Priority below is reordered from this.
  [14](issues/14-transcript-frequencies.md)
- **10** (2026-11-24): asset blocking makes the load event 5–33% sooner
  (64–689 ms) and cuts bytes by 17–73%. It doesn't change how much there is
  to read.
  - `Network.setBlockedURLs` with `urlPatterns` is safe on both routes,
    because nothing is paused.
  - Fetch can hang images when the driver stays connected but stops
    answering. Through the extension, that could last up to 5 minutes.
  - Ticket 18 can be grilled. [10](issues/10-measure-asset-blocking.md)
- **12** (2026-11-24): measured on 13 real pages.
  - **read:** right on 11 (7 good, 4 partial). It costs about 2×
    `innerText` until URLs are shortened.
  - **extract:** unscoped, right on only 5. Scoped to a list or an ancestor,
    right on 6 of 6. A table mode got 115 of 115 rows with headers.
  - Ship `read` with fixes, and `extract` scoped plus `table` and `jsonld`
    modes. [12](issues/12-read-extract-quality.md)
- **13** (2026-11-24): native tables work through one bounded cached read
  of the rows, with Table/Grid used only to detect them and get headers.
  - `GetItem` is broken or null in Services and .NET 8 WinForms, and scrolls
    WPF grids.
  - Virtualized lists (Explorer, WPF) expose only the rows on screen.
  - An unbounded read crashed Event Viewer at 22,915 rows, so a row cap is
    required.
  - Ticket 17 can be grilled. [13](issues/13-native-table-patterns.md)
- **20** (2026-11-24): the native false failures from ticket 14 were bugs
  already fixed in v0.2.0; the failing sessions ran older builds. Leftover
  line-break gaps in `wait value`, `wait text` and the rename fallback are
  ticket 21. [20](issues/20-native-set-value-false-failures.md)
- **16** (2026-11-24, grilled): **Notes.**
  - Agents or the user leave one-line lessons per site (registrable domain)
    or App (exe name).
  - winuse works out the key from a Window ID, URL, domain or exe name. It
    shows the newest 10 once per Agent and never acts on them.
  - There is no count limit, and `note list` shows them all.
  - Learned timing is dropped; waiting goes to 07.
  - Written as `docs/adr/0024-notes.md` and the `CONTEXT.md` term **Note**.
    Build tickets 22–24. [16](issues/16-site-and-app-knowledge.md)
- **17** (2026-11-24, grilled): new commands `read` (Markdown, Pages) and
  `extract` (JSON rows, Pages and native).
  - `extract --root` is optional, but the output always names the container
    it used, and says "no list found" rather than guess.
  - Row caps: 50 on Pages, 500 on native Apps.
  - `read` shortens URLs unless `--urls full` is given.
  - Build tickets 25–27. [17](issues/17-read-extract-surface.md)
- **18** (2026-11-24, grilled): asset blocking, opt-in on agent-owned Tabs
  only (`tab new`/`navigate --block`), using `Network.setBlockedURLs`
  `urlPatterns`. A note in reads and screenshots. No trackers in v1.
  Build ticket 28. [18](issues/18-asset-blocking-decision.md)
- **19** (2026-11-24, grilled): `Log.enable` always on, with failed-request
  and CSP notes plus `winuse logs`.
  - JS exceptions come from a main-world listener: on by default in
    agent-owned Tabs, opt-in (`logs --capture`) in the user's.
  - Never `Runtime.enable`; never `Network.enable`, except on a Tab
    where `--block` is on (ticket 28).
  - Build tickets 29–30. [19](issues/19-console-capture-decision.md)

- **21** (2026-11-24, built): `wait value` treats LF/CRLF/CR alike; native `wait text` and `wait notification --text` match whitespace-collapsed text; rename read-back accepts a stale old name when the new name appeared. [21](issues/21-wait-value-line-breaks.md)
- **22** (2026-11-24, built): `%LOCALAPPDATA%\winuse\notes.json` store (`internal/notes`), keys via `golang.org/x/net/publicsuffix` (user decision) with hosts converted to ASCII by `x/net/idna`; `winuse note add|list|remove|clear` with `--json`; corrupt files moved to `.bad`, `.bad.2`, …. [22](issues/22-notes-store-and-commands.md)
- **23** (2026-11-24, built): The newest 10 Notes for a site or App print once per Agent above the Window State (site then browser on a browser Window), in `"notes"` under `--json`, shrinking first under `--max-chars`. [23](issues/23-notes-in-window-state.md)
- **24** (2026-11-24, built): SKILL.md Safety says Notes come from earlier agents and never grant permission, with topic `notes` (`skills/winuse/references/notes.md`) covering how they show, when to leave one and how to remove a wrong one. [24](issues/24-notes-in-the-skill.md)
- **25** (2026-11-24, built): `winuse read <window>[:<tab>] [--root] [--urls full] [--max-chars | --out]` prints a Page's main content as Markdown (bladebro port, Apache-2.0 attribution in `third_party/bladebro`). [25](issues/25-read-command.md)
- **26** (2026-11-24, built): `winuse extract` on Pages prints a header naming the container and JSON rows (default 50), `--root` scoped, `--as table|jsonld`, "no list found" rather than a guess. [26](issues/26-extract-pages.md)
- **27** (2026-11-24, built): `extract` on native Windows reads the only Table/Grid or the longest list (default 500 rows), never `GetItem`, bounded cached reads; `record-tree --table` records fixtures. [27](issues/27-extract-native.md)
- **28** (2026-11-24, built): `tab new … --block images,fonts,media` and `navigate <w>:<tab> --block` on agent-owned Tabs only, via `Network.setBlockedURLs` urlPatterns plus `Network.enable` on that Tab (see Comments); reads, screenshots and extract on a blocking Tab end with a note. [28](issues/28-asset-blocking.md)
- **29** (2026-11-24, built): `Log.enable` in every Tab session (never Runtime/Network on the user's Tabs); failed-request and CSP notes after Page Actions; `winuse logs <w>[:<tab>] [--clear] [--max-chars | --out]`. [29](issues/29-log-notes-and-logs-command.md)
- **30** (2026-11-24, built): A main-world exception listener installed with `addScriptToEvaluateOnNewDocument` (after `Page.enable`) passes records to an isolated-world buffer; notes say `the Page threw: …`. [30](issues/30-exception-listener.md)
- **01** (2026-11-24, built): Page headings print `(level N)`; link URLs print with `--urls` (and always in `find`, diffs and ambiguity lists), shortened by `page.LinkURL` (same-origin = scheme+host+port). [01](issues/01-page-link-urls-and-heading-levels.md)
- **02** (2026-11-24, built): `navigate`/`back`/`forward`/`reload` read `responseStatus` in an isolated world and add `Note: the Page answered HTTP <code> <reason>`; empty-body 4xx/5xx prints `ok` with the error page noted. [02](issues/02-http-status-on-navigate.md)
- **03** (2026-11-24, built): Page `scroll` reuses ticket 06's effect watch in a scroll mode and says when nothing moved and why (Page at its bottom/top/edge, does not scroll, nested scroller at its end), or when a scroll reached an end; `into-view` says when the Element was already in view.. [03](issues/03-page-scroll-result.md)
- **04** (2026-11-24, built): A Batch runs every step's argument-only checks while it prepares and sends nothing on a bad value (`batch: Action N (kind): …; nothing was sent`). [04](issues/04-batch-upfront-value-checks.md)
- **05** (2026-11-24, built): One shared wall check (`internal/page/wall.js`, `page.WallKind`, ported from bladebro) used by `read`, `extract` and a note after navigations and after a click that navigated (once the new body has loaded). [05](issues/05-challenge-page-note.md)
- **06** (2026-11-24, built): A Page click or press-key watches the Page (isolated world, no Runtime.enable) and ends `ok` with at most one note: navigated, still navigating, changed (text only), or no change seen. [06](issues/06-page-action-outcome-note.md)
- **07** (2026-11-24, built): `--observe` on a Page settles first (110 ms DOM quiet + 280 ms request quiet, 1 s bound, note when still busy). [07](issues/07-page-settle-and-waits.md)
- **08** (2026-11-24, built): Page landmarks (main, navigation, banner, footer, complementary) are always lines; when a read is too long, chrome landmarks with 3+ Elements fold to one line (never the focused, main, cut or changed ones). [08](issues/08-fold-landmarks-and-cut-roles.md)
- **09** (2026-11-24, built): `set-value` and `type-text --into` follow a placeholder's focus hand-off and a remounted editor (with a note), never read a detached node, and read multi-line editors back by blocks. [09](issues/09-facade-editor-set-value.md)

## Priority

From ticket 14's real-session counts:

1. **Waits:** 07 (settle before `--observe`, `wait idle`/`js`), then Notes
   (22 → 23 → 24).
2. **Page text as data:** 25 (`read`), 26 (`extract` on Pages), later 27
   (native).
3. **What a click did:** 06.
4. **False failures:** 09 (rich-text editors) and 21 (line breaks in
   waits).
5. **Error and challenge pages:** 02 and 05.
6. **Failed requests and exceptions:** 29 → 30.
7. **Smaller:** 01 (link URLs and heading levels), 08 (landmark folding), 28
   (asset blocking).
8. **Low:** 03 (scroll note) and 04 (Batch checks), which real sessions
   barely hit.

## Fog

None. Every research and grilling ticket is resolved.

Prototypes from the experiments are copied to `prototypes/` (extract and
read JS, console and asset scripts, the UIA probe, the facade repro test).

## Tickets

| # | Ticket | Type | Status |
|---|---|---|---|
| 01 | [Link URLs and heading levels on Page lines](issues/01-page-link-urls-and-heading-levels.md) | task | resolved |
| 02 | [HTTP status on navigate](issues/02-http-status-on-navigate.md) | task | resolved |
| 03 | [Say when a Page scroll moved nothing](issues/03-page-scroll-result.md) | task | resolved |
| 04 | [Batch: check step values before the first step](issues/04-batch-upfront-value-checks.md) | task | resolved |
| 05 | [Note a challenge or block page](issues/05-challenge-page-note.md) | task | resolved |
| 06 | [Say what a Page click or key did](issues/06-page-action-outcome-note.md) | task | resolved |
| 07 | [Settle before `--observe`; `wait idle` and `wait js`](issues/07-page-settle-and-waits.md) | task | resolved |
| 08 | [Fold Page landmarks; break the cut note down by role](issues/08-fold-landmarks-and-cut-roles.md) | task | resolved |
| 09 | [Placeholder and remounting editors in `set-value`](issues/09-facade-editor-set-value.md) | task | resolved |
| 10 | [Measure asset blocking](issues/10-measure-asset-blocking.md) | prototype | resolved |
| 11 | [Console and errors without Runtime; HTTP status method](issues/11-console-without-runtime.md) | research | resolved |
| 12 | [Generic `read` and `extract` on real pages](issues/12-read-extract-quality.md) | prototype | resolved |
| 13 | [UIA table patterns in native Apps](issues/13-native-table-patterns.md) | research | resolved |
| 14 | [How often real sessions hit these problems](issues/14-transcript-frequencies.md) | research | resolved |
| 15 | [Reproduce the placeholder-editor readback bug](issues/15-facade-editor-repro.md) | prototype | resolved |
| 16 | [Per-site and per-App knowledge → Notes](issues/16-site-and-app-knowledge.md) | grilling | resolved |
| 17 | [`read` and `extract`: command surface](issues/17-read-extract-surface.md) | grilling | resolved |
| 18 | [Asset blocking: build it, and how](issues/18-asset-blocking-decision.md) | grilling | resolved |
| 19 | [Console capture: scope and visibility](issues/19-console-capture-decision.md) | grilling | resolved |
| 20 | [Native `set-value` false readback failures](issues/20-native-set-value-false-failures.md) | prototype | resolved |
| 21 | [Line breaks in `wait value`, `wait text` and the rename fallback](issues/21-wait-value-line-breaks.md) | task | resolved |
| 22 | [Notes: the store and `winuse note`](issues/22-notes-store-and-commands.md) | task | resolved |
| 23 | [Notes: shown in the Window State](issues/23-notes-in-window-state.md) | task | resolved |
| 24 | [Notes: the skill](issues/24-notes-in-the-skill.md) | task | resolved |
| 25 | [`winuse read`](issues/25-read-command.md) | task | resolved |
| 26 | [`winuse extract` on Pages](issues/26-extract-pages.md) | task | resolved |
| 27 | [`winuse extract` on native lists and grids](issues/27-extract-native.md) | task | resolved |
| 28 | [Asset blocking on agent-owned Tabs](issues/28-asset-blocking.md) | task | resolved |
| 29 | [Failed requests and CSP: notes and `winuse logs`](issues/29-log-notes-and-logs-command.md) | task | resolved |
| 30 | [JavaScript exceptions without `Runtime.enable`](issues/30-exception-listener.md) | task | resolved |
