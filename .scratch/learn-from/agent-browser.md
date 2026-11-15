# What winuse can learn from agent-browser

## Sources and versions

- **agent-browser: commit `5338477` of https://github.com/vercel-labs/agent-browser** (2026-11-15, after release v0.43.0 of 2026-11-12). Citations use the prefix `ab/`, for example `ab/cli/src/native/snapshot.rs:725-742`. Permalink base: https://github.com/vercel-labs/agent-browser/blob/53384771fbafc261ba67608ac806dd5aa70b84f0/
- **Baseline.** `.scratch/browser-merge/agent-browser-vs-use-browser.md` read v0.41.1, commit `5868f7c` (2026-10-30). This note builds on it and does not repeat it. Between the two commits there are 61 commits and three releases: v0.41.2, v0.42.0, v0.43.0 (`ab/CHANGELOG.md:1-140`).
- **PRs** are cited as `ab#NNNN` (https://github.com/vercel-labs/agent-browser/pull/NNNN). I read the merged diffs with `git show` and the PR bodies with `gh pr view`. Open PRs are marked "(open)".
- **winuse paths** are relative to the repo root and use the prefix `winuse/`. They are from branch `zeron/browser-alternative-concept-discussion` at `03fc3a4`.
- **How it was read.** Shallow clone into a temp folder. Nothing was built or run. No browser was launched. "(inference)" marks a claim that rests on reading code for an absence, or on reasoning rather than a source.
- agent-browser is now about 132.6k lines of Rust in `ab/cli/src`, with 2,293 test attributes. Its e2e file grew from 173 to 201 tests since `5868f7c` (grep of `#[tokio::test]` in `ab/cli/src/native/e2e_tests.rs` at both commits).

## Summary: top recommendations, ranked

winuse already has most of what made agent-browser look ahead in the baseline: observe-after-action diffs, Batches with reads, actionability checks, dialogs, uploads, downloads, focus emulation, background Tabs, Delivery phrases, a 28000-character cap. The ideas left are smaller and sharper.

| # | Idea | Area | Pages or native | Effort |
|---|---|---|---|---|
| 1 | **Hints on unnamed or duplicate controls** (`hint="icon:trash; in:Blue mug"`) | Output | Pages; row context also native in `find` and `--interactive` | S-M |
| 2 | **`set-value` on a custom ARIA combobox or listbox** (open it, click the matching `role=option`) | Features, Speed | Pages | M |
| 3 | **Settle the click point after scrolling, and re-centre once** when the centre is still outside | Robustness | Pages | S |
| 4 | **Page errors and console** as an Action `Note:` and a read command | Features, Speed | Pages, Electron | M |
| 5 | **Skill evals that need no desktop** (command use, topic routing) plus a skill-size measure | Testing | Both | M |
| 6 | **`--if-changed` screenshots**: skip an image identical to the last one | Speed (tokens) | Both | S |
| 7 | **Refuse a CDP call to an already-detached session at send time** | Robustness | Pages | S |
| 8 | **Link URLs on request** (`--urls`) in Page reads | Output, Speed | Pages | S |
| 9 | **`wait <window> js <expr>`** (agent-browser `wait --fn`) | Speed | Pages | S |
| 10 | **Annotated screenshot**: Element Indexes drawn on the image | Output | Both | M |
| 11 | **Better not-actionable reasons**: view transitions, collapsed `<details>`, hidden check boxes behind a label | Robustness | Pages | S |

Saved flows are covered briefly at the end. They are large and experimental, so not recommended now.

## 1. Features

### 1.1 `set-value` on a custom ARIA combobox or listbox (rank 2)

**agent-browser.** `select` used to work only on a native `<select>`. On a Radix or Headless UI widget it "prints `✓ Done`, exits 0, and changes nothing" (ab#2592 body). Since ab#2592 (merged 2026-11-14, after v0.43.0):
- On `role=combobox` it opens the widget only when `aria-expanded` is not `true` (`ab/cli/src/native/interaction.rs:487-488`).
- It waits for visible `role=option` items, scoped to the listbox, then `aria-controls`, then the document (`:494-513`).
- It matches by accessible name (`aria-labelledby`, then `aria-label`, then text) or value. No match is an error that lists the available options (`:535`).
- A matched `aria-disabled` option is an error, not a click (`:542`).
- In an `aria-multiselectable` listbox it leaves already selected options alone, because a click would toggle them off (`:549`).
- Docs: `ab/skill-data/core/references/commands.md:96-98`.

**winuse.** Page `set-value` handles a `<select>` (from page JS), check boxes, radios, switches and date/time/colour pickers (`winuse/internal/page/choice.go:3-16`, `:49-60`). Anything else that takes no typed text is refused: "takes no typed text" (`winuse/internal/helper/typeinto.go:260-262`). So a Radix select costs an agent a click, a read, and a click on the option: three turns, or a Batch with `--observe`. Native combo boxes already work (desktop-parity 12, `.scratch/desktop-parity/issues/12-select-option-by-name.md:3`).

**Mapping.** Pages only. Add a fourth `Choice.Kind`, "aria", for `role=combobox` and `role=listbox`. Open with trusted input (a click, Headless on a Page), wait for options, click the match with trusted input, read back `aria-selected` or the combobox's text, and refuse with the list of options. Keep agent-browser's rules: open only when collapsed, scope to `aria-controls`, never toggle an already selected option off. An input-based combobox (autocomplete) still fills as text first (inference: its option list appears only after typing).

**Effort.** M. One JS helper, the existing click path, e2e probe pages for Radix-style and Headless UI-style widgets.

### 1.2 Page errors and console (rank 4)

**agent-browser.** The daemon records `Runtime.consoleAPICalled` and `Runtime.exceptionThrown` for the page (`ab/cli/src/native/actions.rs:1982-2024`). `console` and `errors` print and clear them (`ab/cli/src/native/actions.rs:8107-8127`; `ab/skill-data/core/references/commands.md:602-605`). v0.42.0 fixed `errors` to print the page's error text (ab#2427). A bounded FIFO for these logs is open as ab#2558.

**winuse.** No Page console or error capture. winuse enables only `Page`, `DOM` and `Accessibility`: a grep for `Runtime.enable`, `Log.enable` and `Runtime.consoleAPICalled` in `winuse/internal` finds nothing, and `Page.enable` appears only at `winuse/internal/page/input.go:493` and `winuse/internal/page/watch.go:199`. An agent must `eval` to look.

**Mapping.** Pages and Electron Windows with a Browser Connection. winuse's users are coding agents, often testing their own web app, so this pays more for winuse than for a general browser tool (inference). Two parts:
- A `Note:` after a Page Action when the Page threw an uncaught exception during it, like the dialog and new-Tab notes (`winuse/skills/winuse/references/browser.md:120-126`). That saves the turn the agent would spend asking "why did nothing happen?" (inference).
- A read: `page-errors <window> [--console] [--clear]`, bounded to the last N entries and the read's character cap.

Trade-off: `Runtime.enable` on the user's own Tabs is visible to page scripts and to some bot detection (inference). Consider `Log.enable` plus exceptions only on Tabs the Agent works in, or turning capture on only when asked.

**Effort.** M. Event routing exists (`winuse/internal/cdp/client.go:241-265`); the store, the note and the command are new.

### 1.3 Smaller Page features

- **`wait --fn <expr>`** (rank 9). agent-browser waits on a JavaScript condition (`ab/skill-data/core/SKILL.md:154`, `ab/skill-data/core/references/commands.md:181`). winuse has selector, text, url, load, download and Element states (`winuse/skills/winuse/references/commands.md:41-43`), but no JS condition. An agent polls with `eval`, one turn each. Effort S: the url wait already runs a polled expression (`winuse/internal/page/nav.go:320`).
- **Link URLs on request** (rank 8). `snapshot -u` adds each link's `href` (`ab/skill-data/core/SKILL.md:86`; `ab/cli/src/native/snapshot.rs:621-631`). winuse prints a link's name only (`winuse/internal/page/testdata/clickable-div.txt:12-16`). With the URL, an agent can `navigate` or `tab new <url>` instead of clicking and then handling a new Tab. winuse's read already has every node's attributes from `DOMSnapshot.captureSnapshot` (`winuse/internal/page/snapshot.go:165`, `:362`), so this costs no extra calls (inference). Effort S: a `--urls` view flag that appends the href to link lines.
- **Element and full-page screenshots.** `screenshot @eN` and `--full` (`ab/skill-data/core/references/commands.md:141-146`). winuse's Tab capture is the visible part only (`winuse/internal/page/background.go:76-97`), and `zoom` crops by coordinates. Low value for winuse's loop. Effort S.
- **Not worth adopting:** `get text|html|attr|box|styles|count` and `is visible|enabled|checked` (`ab/skill-data/core/references/commands.md:113-136`): the Window State and `eval` already answer these. `set viewport|device|geo|media`, cookies, storage, HAR, network routes and the auth vault fit a test-automation browser, not the user's own browser under winuse's safety rules (inference).

### 1.4 Ideas that carry over to native Windows

- **Annotated screenshots** (rank 10). `screenshot --annotate` draws `[N]` labels that map to `@eN` (`ab/skill-data/core/SKILL.md:214`; `ab/cli/src/native/screenshot.rs:105-130`). winuse already knows every Element's bounds and the screenshot's mapping (`winuse/docs/adr/0020-screenshots-sized-to-the-image-budget.md:38-40`). Drawing Element Indexes would let a model on a canvas-heavy or visual task pick an index from the image instead of reading `@x,y` (inference). It must stay opt-in: labels cover pixels the model may need. Effort M.
- **`--if-changed`** (rank 6). See 2.2.
- **Row context for duplicates** (part of rank 1). See 3.1.

## 2. Speed

### 2.1 What winuse already does

- **Act and observe in one call.** `--observe[=diff|full]` with a 60% fallback (`winuse/docs/adr/0021-observe-after-an-action.md:37-80`). agent-browser has only `snapshot --delta` and `diff snapshot` as separate commands (`ab/skill-data/core/references/commands.md:68-72`). winuse is ahead.
- **Batches with reads**, `--stop-on-error`, `batch -` from stdin (`winuse/CONTEXT.md:80-82`). agent-browser's `batch` has `--bail` and, since v0.40.0, `--omit-command` for JSON (`ab/skill-data/core/references/commands.md:58`). Covered.
- **One long-lived connection** in the helper, DOM and Accessibility enabled once per session, page reads of 19-24 ms (commits `6b8739e`, `efc20c2`). agent-browser's v0.43.0 large-page speedup parses the AX tree response once instead of through a JSON `Value`, and makes `--compact` linear (ab#2509). winuse's Go read already decodes once (inference). Nothing to take.
- **Acting by name without a read.** `click <window> name:Submit` resolves against a fresh walk when needed (`winuse/docs/adr/0004-window-state-tree-format.md:109-119`). That is agent-browser's `find role button click --name` (`ab/skill-data/core/references/commands.md:208-223`). Covered. winuse keeps exact matching on purpose.

### 2.2 `--if-changed` screenshots (rank 6)

**agent-browser.** `screenshot --if-changed` decodes the image, compares pixels with the last image of the same tab and scope, and returns `changed:false` with no path when the share of changed pixels is at or under `--threshold` (`ab/cli/src/native/actions.rs:6906-6970`; `ab/skill-data/core/references/commands.md:144-149`). The reference calls it "Recommended: skip unchanged images to save tokens". A suppressed capture keeps the old baseline, so small changes add up (`ab/cli/src/native/actions.rs:6903-6905`). At most 32 baselines are kept (`:6920-6926`).

**winuse.** Every `--screenshot` writes a new image, about 1,560 image tokens for a 16:9 Window (`winuse/docs/adr/0020-screenshots-sized-to-the-image-budget.md:48-49`). There is no unchanged check (grep for `if-changed` and `unchanged` in the screenshot code and skill).

**Mapping.** Both native and Pages. Keep one baseline per Agent per Window (and per Tab target). On no change, print `Screenshot: unchanged since <path>` and write no file. The model then does not open an image it has already seen (inference: the harness reads the image only when the agent opens the path). Effort S.

### 2.3 Fewer turns from the items in section 1

- ARIA combobox `set-value` turns about three turns into one (1.1).
- An error `Note:` saves a diagnostic turn (1.2).
- `wait --fn` replaces `eval` polling (1.3).
- Link URLs let an agent `navigate` directly (1.3).

### 2.4 Skill guidance

agent-browser's core skill changed in three ways since the baseline:
- **Slimmed core, routed detail.** Core went from 5,701 to 1,965 words. The recommended context (stub, list, core) went from 11,735 to 4,541 tokens, measured by `evals/context-footprint.ts` (ab#2303 body). A unit test fails when a reference is missing from the routing table (ab#2303).
- **One reference at a time.** `skills get core --ref <name>` (`ab/skill-data/core/SKILL.md:42-51`; ab#2310).
- **Prefer snapshots.** "Never screenshot to check whether an element exists or the page finished loading ... wait for it ... and snapshot again" (`ab/skill-data/core/SKILL.md:34`). ab#2453: "Agents were falling back to screenshots in cases a snapshot covers, which is slower and costs more tokens."

**winuse already has** the core plus topics, served from the binary (`winuse skill <topic>`, `winuse/skills/winuse/SKILL.md:216-234`), and "Trust the tree first" (`winuse/skills/winuse/SKILL.md:124-126`). winuse's core is 13.6 KB, with 67.8 KB of topics (file sizes). One line worth adding: agent-browser's explicit "never screenshot to check loading; `wait`, then read" (inference that winuse agents do this too; the Task Suite traces would show it).

## 3. Output quality

### 3.1 Hints on unnamed or duplicate controls (rank 1)

**agent-browser** (v0.43.0, ab#2578). After a snapshot, it counts each ref's (role, label) pair (`ab/cli/src/native/snapshot.rs:586-591`). A control whose label is empty, or shared with another ref, gets a hint read from the DOM (`:592-618`). The hint function (`:725-742`) returns these parts, joined with `; `, each cut to 48 characters:
- `icon:` the SVG sprite id (`<use href="#trash">`), an `<svg>` id or `data-icon`, or an icon-library class (`lucide-`, `fa-`, `bi-`, `mdi-`, `ri-`, `ti-`, `icon-`);
- `img:` the file name of an image inside the control;
- `data-testid:`, `name:` or `id:`, skipping values that look generated (three digits in a row);
- `in:` the text of the nearest `tr`, `li`, `article`, `row` or `listitem`, minus the control's own text.

It prints as ` hint="..."` after the ref (`:1381`). Example from the docs (`ab/docs/content/docs/snapshots.mdx`, added by ab#2578):

```
- button [ref=e2] hint="icon:icon-trash; in:Blue mug"
- button [ref=e3] hint="icon:fa-heart; in:Blue mug"
- textbox "Email" [ref=e5] hint="name:login-email"
- textbox "Email" [ref=e6] hint="name:newsletter-email"
```

A hint is not a name: `find --name` and ref recovery ignore it. It costs two CDP calls per hinted node, run in parallel (`ab/cli/src/native/snapshot.rs:744-780`).

**winuse.** An icon-only Page button prints as `N button` with nothing after it, and duplicate names print the same line. `name:` then refuses as ambiguous (`winuse/CONTEXT.md:76-77`). The tree's indentation shows the row in a full read. It is lost in two views:
- `find` prints matches one TAB each, without ancestors (`winuse/docs/adr/0004-window-state-tree-format.md:79-85`);
- `--interactive` prints each Element one level below its nearest printed ancestor and drops plain text (`winuse/docs/adr/0004-window-state-tree-format.md:128-136`).

**Mapping.**
- **Pages.** Compute the hint from the `DOMSnapshot` winuse already reads: attributes, `<use>` hrefs, `<img src>`, row text. No extra round trips (inference from `winuse/internal/page/snapshot.go:165-180`). Render it as a new optional field at the end of the line, for example `Hint: icon trash, in Blue mug`. Only for interactive Elements whose role and name are empty or shared in that read.
- **Native.** The `in:` part carries over at no cost: the nearest named ancestor (list item, row, group) is already in the walk. Print it in `find` and `--interactive` for duplicates only. UIA has no icon names; `ClassName` could stand in where `ID:` is missing (inference).
- **Grammar.** ADR 0004's line grammar is Codex's. A hint is a divergence, so it needs an ADR 0004 amendment, and `--full` should leave it out.
- Measure the token cost on the 14 real Window States, as speed 05 did.

**Effort.** S-M.

### 3.2 Other output conventions

- **Truncation that says how to get more.** agent-browser: `[truncated: showing N of M chars. Use --max-output to adjust]`, off by default (`ab/cli/src/output.rs:36-59`). winuse cuts by default at 28000 characters, on a line boundary, and the note names the first Element not printed (`winuse/docs/adr/0004-window-state-tree-format.md:164-170`). winuse is ahead.
- **Content boundaries** (opt-in). `--content-boundaries` wraps page-derived output in `--- AGENT_BROWSER_PAGE_CONTENT nonce=<random> origin=<url> ---` ... `--- END_AGENT_BROWSER_PAGE_CONTENT nonce=<random> ---`. The nonce comes from a CSPRNG so a page cannot forge the end marker (`ab/cli/src/output.rs:8-17`, `:61-76`). WebMCP metadata is always wrapped (`ab/cli/src/output.rs:563-590`). winuse treats Window content as untrusted in the skill only (`winuse/skills/winuse/SKILL.md:41-43`). A Window State mixes frame and Page lines, so a whole-output wrapper would add little (inference). Low value. Effort S if wanted.
- **Errors that name the next step.** Both tools do this. New in agent-browser: `Target closed: CDP session <id> detached`, with the skill line "Run `agent-browser tab`, switch to a remaining tab, and snapshot" (`ab/skill-data/core/SKILL.md:253`; ab#2576). winuse's internal detach error is "the page session was detached" (`winuse/internal/cdp/client.go:267-269`). Worth checking that the message an agent sees for a closed Tab names `tab list` (inference: I did not trace the helper's wording).
- **Skill examples drift from real output.** agent-browser's skill still shows `@e1 [heading] "Log in"` (`ab/skill-data/core/SKILL.md:99-104`) and `@e1 [header]` (`ab/skill-data/core/references/snapshot-refs.md:47-63`), while the code prints `- heading "Log in" [ref=e1]` (`ab/cli/src/native/snapshot.rs:1320-1390`). The baseline noted this too. They started fixing it per skill (ab#2575). Lesson for winuse: a test that renders the skill's example Window State from a fixture would catch drift (inference: `winuse/internal/skill/parity_test.go` checks Codex coverage, not examples).

## 4. Performance and robustness

### 4.1 Click point: settle and re-centre (rank 3)

**agent-browser** (ab#2587, merged 2026-11-13). The press point is measured in top-level coordinates after `scrollIntoViewIfNeeded`. If the point moved, it waits up to five rounds of two animation frames or 100 ms until it stops moving (`ab/cli/src/native/element.rs:1072-1090`). If the centre is still outside the viewport, it calls `scrollIntoView({block:'center', inline:'center', behavior:'instant'})` on the element and each ancestor frame, settles, and tries once more (`:1091-1095`). Frame offsets include borders, padding and CSS transform scale (`:975-988`, `:1040-1064`). The PR: "Elements straddling the viewport edge were pressed below the fold, and elements moved by scroll-driven reflow kept the stale point."

**winuse.** `DOM.scrollIntoViewIfNeeded`, then one measurement, then refuse with "its centre is outside the visible part of the Page, even after scrolling it into view" (`winuse/internal/page/input.go:88`, `:218-239`). Frame offsets include border and padding but not transform scale (`winuse/internal/page/input.go:150-152`). winuse refuses rather than mis-clicking, so the failure is safe. But it costs the agent a turn (`scroll ... into-view`, then retry) on large or edge-straddling targets (inference). A sticky header that slides in after the scroll can also move the point after it was measured (inference).

**Mapping.** Pages only. In `aimJS`: measure; when the point moved, wait a frame and measure again, up to about 500 ms. When the centre is outside, scroll to centre with `behavior:'instant'` once and measure again. Add the frame scale factor. Effort S. Add probe pages: an edge-straddling button, a lazy-loading list above the target, a scaled iframe.

### 4.2 Fail fast on a detached session (rank 7)

**agent-browser** (v0.43.0, ab#2576). Detached sessions are remembered, up to 1,024 (`ab/cli/src/native/cdp/client.rs:36-75`). Pending commands to the session fail at once, and "senders check the mark under the same lock before registering, so a command is either failed here or refused at send time" (`:283-337`). Chrome's "Session with given id not found" counts as a detach too (`:85-118`).

**winuse.** Pending calls fail at once on `Target.detachedFromTarget` (`winuse/internal/cdp/client.go:241-269`). A call sent after the detach is not refused; it waits for its context deadline (inference from the same code: there is no detached set). That window is small, but it is real for an Action that resolves, aims, then dispatches in several calls while the page runs `window.close()` (inference).

**Mapping.** Keep a bounded set of detached session ids in `cdp.Client`. `CallSession` checks it before registering. Also map Chrome's "Session with given id not found" to `ErrSessionDetached`. Effort S.

### 4.3 Clearer reasons, from agent-browser's open PRs

These are open on 2026-11-15, so treat them as signals, not proven fixes:
- **View transitions** (ab#2612, open). During `document.startViewTransition`, Chrome's transition images take every hit, and the hit test lands on `<html>`. agent-browser counted an ancestor as a hit and reported success. winuse's `within()` walks up from the hit to the element (`winuse/internal/page/input.go:137`), so an ancestor hit becomes "covered by <html ...>": safe, but misleading (inference from reading `aimJS`). Fix: wait up to a few seconds for running transitions before aiming, then say "a page transition is still running".
- **Collapsed `<details>` and `hidden="until-found"`** (ab#2613, open). Use `checkVisibility()` and say "inside a collapsed <details> (click its summary first)". winuse would report "covered by <details ...>" or "hidden" here (inference).
- **Hidden check box behind a label** (ab#2598, open). When an `<input type=checkbox>` is `display:none` and its `<label>` is visible, click the label. winuse already accepts a label hit for its control (`winuse/internal/page/input.go:138-143`). A `display:none` input is not in the AX tree, so this case may never reach winuse (inference). Check with a probe page before building anything.
- **Keep refs when a read fails** (ab#2617, open; issue #2616). agent-browser cleared its ref map before a scoped snapshot that then failed. Worth a winuse test: a failed or timed-out `get-window-state --root` must leave the Agent's Window State intact (I did not check winuse's code for this).

### 4.4 Testing and measurement

- **Skill evals that need no desktop** (rank 5). agent-browser's `evals/` give a model the installed skill and a task, make one call, and pattern-match the commands it writes. Categories: skill-loading, skill-selection, command-usage, reference-routing and context-footprint (`ab/evals/README.md:77-107`). reference-routing checks that the model loads exactly the one reference it needs, and none on a control task (`ab/evals/cases/reference-routing.ts`; ab#2310). `context-footprint.ts` measures the skill's tokens (`ab/evals/README.md:99`).
  - winuse's Task Suite is far stronger evidence: real tasks, Checks, A/B, repeats (`winuse/tasksuite/README.md:113-167`). But it takes over the screen, and only the user may start it (`winuse/tasksuite/README.md:27-37`).
  - A desktop-free eval could run any time, by an agent: "given this Window State and task, does the model use `set-value`, `--observe`, a Batch, `<window>:<tab>`, `skill browser`?" Pass by regex on the commands. It would catch skill regressions between Task Suite runs (inference).
  - A deterministic skill-size test (bytes or o200k tokens of SKILL.md) is cheap and stops the core from growing back (inference).
  - Effort M for the eval, S for the size test.
- **A bug fix lands with an e2e probe.** Each robustness PR above adds a named e2e test with a fixture page, and the body shows before and after (ab#2587, ab#2592, ab#2617). winuse's opt-in e2e pattern can do the same (`winuse/test/e2e/README.md`).
- **Old algorithm as a test oracle.** ab#2509 kept the quadratic `compact_tree` as `compact_tree_reference` in the tests and compared both on generated trees (test `test_compact_tree_matches_reference_on_generated_trees` in `ab/cli/src/native/snapshot.rs`). Useful for winuse when a view or diff is rewritten for speed.
- **Benchmarks.** `ab/benchmarks/bench.ts` measures command latency, cold start, binary size, daemon and browser RSS, and CPU inside a Vercel Sandbox (`ab/benchmarks/README.md:7-27`). No results are committed. The README says per-command latency is "dominated by Chrome DevTools Protocol round trips" (`:68-70`). Nothing to take: winuse's `readbench_test.go` and the Task Suite speed metrics already cover this.

### 4.5 Where winuse is ahead (do not copy agent-browser)

- **Background work.** agent-browser's feature request "Background Mode to prevent stealing focus when connected to already-running Chrome instance" (issue #1247) is still open. winuse has Tab targets (`winuse/docs/adr/0017-background-tabs.md`).
- **Same-site cross-origin iframes.** agent-browser's issue #2472 (open) says refs inside an in-process cross-origin iframe report "outside the viewport". winuse uses content quads when the frame chain stops (`winuse/internal/page/input.go:264-272`).
- **Date, time and colour inputs.** ab#2599 (open) adds them. winuse already writes them by value (`winuse/internal/page/choice.go:49-60`).
- **Lost-response safety.** agent-browser's `outcome_unknown` (v0.39.1) matches winuse's Delivery (`winuse/CONTEXT.md:113-115`).
- **A wedged page.** agent-browser's `page_wedged` (v0.39.4) matches winuse's `NotAnsweringError` (`winuse/internal/page/watch.go:76-124`).
- **Did-you-mean** for unknown arguments (ab#2311) matches `winuse/internal/cli/help.go:881-890`.
- **Click buttons and modifiers** (ab#2594, 2026-11-14) are in winuse already (`winuse/skills/winuse/references/commands.md:21-22`).

## Saved flows (brief)

v0.42.0 added experimental saved flows: record a task with `--capture flow`; `flow save` keeps first-party JSON requests and turns typed values into params; `flow run` tries direct HTTP, then the request from the page, then a replay of the commands (`ab/skill-data/core/SKILL.md:38`; `ab/skill-data/core/references/commands.md:280-300`; ab#2236, ab#2397, ab#2432, ab#2433, ab#2444). At least six more flow fixes are open (ab#2602, ab#2569, ab#2557, ab#2556, ab#2555, ab#2554). The rules for writes, credentials, redirects and private addresses fill one very long paragraph (`commands.md:300`). For winuse this would be a large feature with a large safety surface, and it does not carry over to native Windows. Not recommended now. If repeated tasks become common, a smaller step is a recorded Batch the agent can replay (inference).

## Not recommended

- **MCP profiles and per-tool argument trimming** (ab#2311: shared arguments were "about 83% of the default tools/list payload"). winuse is a skill plus CLI by decision (`winuse/docs/adr/0001-skill-and-cli-not-mcp.md`).
- **WebMCP page tools** (`ab/skill-data/core/SKILL.md:36`; ab#2606 open). Experimental Chrome feature flags in a launched Chrome. Watch it; do not build it.
- **`read <url>`** fetches Markdown or `llms.txt` over HTTP without a browser (`ab/skill-data/core/references/commands.md:18-25`). Coding-agent harnesses already have a web fetch tool (inference).
- **Durable refs.** Already considered and deferred in `winuse/docs/adr/0021-observe-after-an-action.md:89-94`. Nothing new since the baseline, except ab#2617's bug, which shows the cost of the extra state.
