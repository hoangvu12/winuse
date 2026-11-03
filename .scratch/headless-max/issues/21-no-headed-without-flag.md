# 21: No Headed Action without `--headed`

**What to build:** every Action that would take the foreground, the real pointer or the real keyboard refuses with "nothing was sent" and the exact `--headed` retry, unless the agent passed `--headed`. Today several commands choose Headed on their own, from the live Window or from their arguments, and the user only finds out when their screen is taken.

ADR 0013 rule 3, as the user decided on 2026-11-03: winuse never takes the user's screen on its own. That covers the routing of ADR 0011 Decision 4 too ("anywhere else they stay Headed"), which the first draft of ADR 0013 had left out.

Paths that go Headed without `--headed` today (check each one, and find any others):

- `type-text` with no Headless route, including a covered Windows 11 Notepad that reports no focused control (`internal/helper/keyboard.go`, the "ok (typed Headed: ...)" output).
- `press-key` with no Headless route, anything but Return or Escape on a classic dialog (`internal/helper/keyboard.go`, `keyroute.go`).
- `click` with a double, triple, right or middle button outside a Page (`internal/helper/actions.go`).
- The `@x,y` forms: `click`, `scroll`, `set-value`, `perform-secondary-action` (`pointer.go`, `scroll.go`).
- `drag` and `hover` outside a Page.
- Batch steps that `headlessStep` sends Headed (`internal/helper/batch.go`).

Rules:

1. Decide the route before anything is sent, as today. When it is Headed and `--headed` was not passed, fail with "nothing was sent", say why there is no background route, and give the exact command with `--headed`. Mention `--post` where Posted Keys would fit.
2. `activate-window` stays as it is: the command itself is the request for the foreground.
3. A Batch without `--headed` refuses a Headed step the same way, before it is sent. Whether the Batch continues past that step follows desktop-parity 20.
4. `--headed` keeps today's behaviour exactly.
5. No change to the Headless, Posted or Page routes.

Docs: CONTEXT.md's Headed Action ("Any other Action is Headed when the agent asks for it" becomes the only way in); SKILL.md's routing rules and the "What winuse cannot do headless" list (every item now needs `--headed`); ADR 0011 gets an "Amended by ADR 0013" note on Decision 4; the ADR 0013 route table; docs/codex-parity.md rows that say keys or clicks "take the foreground".

Sequencing: it touches the same routing code as the click chain (11, 12, 20, desktop-parity 06) and the typing chain (13, 07, 06, desktop-parity 23). Build it before either chain starts.

**Blocked by:** none

**Status:** done (live e2e not run yet: see the 2026-11-03 implementation comment)

- [x] Inventory of every path that goes Headed without `--headed`, recorded here
- [x] Each one refuses with "nothing was sent" and the exact `--headed` retry
- [x] Batch steps refused the same way
- [x] Offline tests per path, through the Services fakes
- [x] e2e expectations updated (tests that relied on the automatic Headed route pass `--headed`); Task Suite checked (compiled with `go vet -tags e2e`; not run live)
- [x] CONTEXT.md, SKILL.md, ADR 0011 note, ADR 0013 table, codex-parity rows

## Inventory (2026-11-03, main 117767a)

Every call of `Services.runAction` (the Headed pipeline: activate, real input, banner) in `internal/helper`, and how each was reached without `headed` before this ticket. The gate sits at each call site, after the Headless, Posted and Page routes are decided (`headedgate.go`: `headedOnly` / `headedRefusal`).

| # | Path | Call site | Reached unheaded before? | Now, without `--headed` |
|---|---|---|---|---|
| 1 | `click @x,y` (any button, any count) | `actions.go` point branch | yes | refused; alt: name the Element |
| 2 | `click` on an Element, `--count 2|3` or `--button right|middle`, outside a Page | `actions.go` Element branch | yes | refused |
| 3 | `click` on an Element, left single, when the element side is not a HeadlessActor | same | only with v1 test fakes (the real `uia.Client` is one) | refused |
| 4 | `activate-window` | `actions.go` | yes (by design) | **unchanged**: the command asks for the foreground |
| 5 | plain `type-text` with no Headless route: no probe, no focused control (covered Windows 11 Notepad, the old `ok (typed Headed: ...)`), a non-edit focus (WinUI, WPF, Electron, Chromium without a Browser Connection), a read-only edit, control characters | `keyboard.go` | yes | refused; the reason names which; alt: `--into`, `--post` |
| 6 | `press-key` with no Headless route: anything but Return/KP_Enter/Escape alone on a classic dialog, or Return there with no default button or a multi-line focus | `keyboard.go` | yes | refused; alt: `--post` for a key without modifiers; `--force` carried into the retry |
| 7 | `set-value @x,y` | `setvalue.go` | yes | refused; alt: name the Element |
| 8 | `set-value` on an Element, non-HeadlessActor | `setvalue.go` | v1 fakes only | refused |
| 9 | `perform-secondary-action @x,y` | `setvalue.go` | yes | refused; alt: name the Element |
| 10 | `perform-secondary-action ... Raise` (Element form) | `setvalue.go` | yes (`headlessStep` excludes Raise) | refused; alt: `activate-window` |
| 11 | `scroll @x,y` outside a Page | `pointer.go` | yes | refused; alt: Element scroll |
| 12 | `scroll` Element forms, non-HeadlessActor | `scroll.go` | v1 fakes only | refused |
| 13 | `drag @x,y @x,y` | `pointer.go` | yes (DragArgs had no `headed`; the CLI did not forward it) | refused |
| 14 | `hover @x,y` | `pointer.go` | yes (same) | refused |
| 15 | `tab switch` on a launched, cloned, toggled or port browser (not the extension) | `tabs.go` `tabSwitchHeaded` | yes (TabArgs had no `headed`) | refused; alt: `navigate` the active Tab |
| 16 | Batch steps of 1-15 | `batch.go` (each step's own command) | yes; a Headed step also made the whole Batch Headed (swallowing Esc, banner) | the step is refused at its turn, nothing sent for it; the Batch stops there (desktop-parity 20 decides continuation later). An unheaded Batch now picks the passive scope unless a step is `activate-window` or carries its own `headed` |
| — | Page Element click/scroll/set-value/type-into, the Page scroll at a point, `get-window-state --screenshot` and `zoom` under `--headed`, `type-text --into` | `pageactions.go`, `screenshot.go`, `typeinto.go` | no: Headed only when `headed` was passed | unchanged |

The CLI now forwards the global `--headed` to `drag`, `hover`, `tab switch` and the point form of `scroll` (it reached only the Element forms), and `takesHeaded` injects a headed Batch's flag into drag, hover and tab-switch steps.

## Comments

### 2026-11-03: triage decision (user)

Asked whether plain `type-text` and `press-key` may keep taking the foreground on their own when no background route exists. The user chose to refuse: the agent must ask for `--headed`. Extended here to every Action that goes Headed on its own, since the rule is the same.

### 2026-11-03: implemented (branch hm21-no-auto-headed)

- One gate, `internal/helper/headedgate.go` (`headedOnly`, `headedRefusal`), called at each routing decision point in the inventory right before `runAction`. `runAction` itself is unchanged, so the merge with desktop-parity 02 stays small. The Window resolves first, so a bad reference still fails as itself. Message: `<command> has no background route: <why>; nothing was sent`. Hint: another way without the foreground where one fits (`--into`, `--post`, an Element reference, Element scroll, `navigate`, `activate-window`), then `pass --headed ...: winuse --headed <exact command>`. The retry uses the resolved Window ID and the agent's own reference, button, count, `--force`, Secondary Action and point. It never repeats the text of `type-text` or the value of `set-value`, which could be a password: those are `<text>` and `<value>`, as `--into`'s refusal already did.
- `type-text`'s reason names the case: no probe, no focused control (the covered Windows 11 Notepad case, which used to print `ok (typed Headed: ...)`, now removed), a disabled or read-only edit, control characters, or a non-edit focus with its class. The refusal does not look at the foreground: a Window that is already in front is refused too, since Headed typing still uses the real keyboard.
- Batch: a step is refused at its turn by its own command (rule 3). The steps before it ran, and continuing past it is left to desktop-parity 20. An unheaded Batch now picks the passive scope with no banner, unless a step is `activate-window` or carries its own `headed`: it can no longer run a Headed step. `--headed batch` is unchanged and also injects `headed` into drag, hover and tab-switch steps (`takesHeaded`).
- Protocol and CLI: `DragArgs`, `HoverArgs` and `TabArgs` gained `headed`. The CLI forwards the global `--headed` to `drag`, `hover`, `tab switch` and `scroll @x,y`, which only got it for the Element forms before. `dragRequest`, `hoverRequest` and `tabRequest` are pure, so the CLI test pins this.
- Offline tests:
  - `headedgate_test.go`: `TestEveryHeadedOnlyPathIsRefusedWithItsRetry` covers every inventory path, with the exact retry, the alternative, no text in the hint, and nothing run unheaded; with `headed` the activation runs. Also `TestActivateWindowNeedsNoFlag` and `TestARefusedPathResolvesTheWindowFirst`.
  - The v1-pipeline tests now send `headed`.
  - The "stays Headed" tests now check both sides: refused without the flag, v1 with it. They are `TestPressKeyNeedsHeadedOffTheDialogRoute`, `TestTypeTextNeedsHeadedOffTheEditRoute`, `TestTabSwitchOnALaunchedBrowserNeedsHeaded`, `TestTypeTextSaysWhyItHasNoBackgroundRoute`, and the Batch scope, page and tab Batch tests.
  - CLI: `TestHeadedReachesDragHoverAndTabSwitch`.
- e2e (compiled with `go vet -tags e2e ./test/e2e` only; live runs are paused while the user games):
  - The tests that used the automatic Headed route now pass `--headed`: type_text, pointer, the `@x,y` screenshot, wgc and set_value tests, the double and right click canvas test, overlay and esc.
  - Converted to the refusal: `TestTypeTextIntoDialogEdit` (plain type-text into the Save As dialog is refused, then `--headed` types) and `TestHeadlessTypeTextIntoNotepad` (a new first step: plain type-text into the covered Notepad is refused).
  - New: `TestHeadedOnlyActionsAreRefusedWithoutTheFlag` (`headed_gate_test.go`). It sends eight Actions without the flag against the covered fixture, plus one Batch that stops at its press-key step with the earlier click done.
- Task Suite: no code or task change. Its Checks read only the end state, and its fixed preamble names no route. The canvas, drag and Electron tasks (paint-draw, paint-rect, vscode-edit, probably explorer-organize and the Edge tasks) now depend on the agent reading the refusal and retrying with `--headed`, as SKILL.md teaches. A rerun is a live run still to do.
- Docs: CONTEXT.md (Headed Action), SKILL.md (routing, "What winuse cannot do headless", keyboard routes, tab switch, Batches, result phrases, command reference), ADR 0011 (amendment notes on Decisions 3 and 4 and the ticket 19 amendment), ADR 0013 (table row done, the Reversals entry names Raise, tab switch and the Batch scope), codex-parity (an intro row, and the click, press-key, type-text, scroll, set-value, drag and secondary-action rows), README, usage text.
- Live runs still to do: `TestHeadedOnlyActionsAreRefusedWithoutTheFlag`, `TestTypeTextIntoDialogEdit` and `TestHeadlessTypeTextIntoNotepad`, whose expectations changed in substance. Then every test that only gained `--headed`, to confirm nothing else moved, and a Task Suite pass.
