# 05: set-value on custom ARIA combobox and listbox widgets (Pages)

Status: ready-for-human
Type: task
Branch: learn-aria-select, off main 03fc3a4
From: `.scratch/learn-from/README.md` rank 4, `.scratch/learn-from/agent-browser.md` §1.1 (agent-browser PR #2592)

## Ask

- On a Page, `set-value <combobox-or-listbox> <option text>` works on custom widgets built with `role=combobox` / `role=listbox` / `role=option` (Radix Select, Headless UI Listbox, MUI Autocomplete, React Select) instead of refusing ("takes no typed text"). Picking an option took about three turns: click to open, read, click the option.
- Open the widget when needed (its trigger; find the list through `aria-controls` / `aria-owns` / `aria-activedescendant`), find the option by accessible name (exact first; decide case and whitespace; ambiguous is an error, never a guess, per CONTEXT.md's Element Reference), select it with the Headless Page input clicks use, and read back that the widget's value changed (Delivery: delivered but unverified when it did not show).
- Typeahead comboboxes: type the text, then pick the matching option.
- Refuse clearly when the option is not found, listing a few options.

## What was built

```
winuse set-value <window> <combobox | listbox-button | listbox> <option name>
winuse set-value 301 12 "Banana"        # Radix-like trigger: opened, option clicked, read back
winuse set-value 301 15 "hamburg"       # typeahead: typed, option "Hamburg" clicked
```

- **Which Elements** (`page/choice.go` `choiceJS`, new Choice Kind `aria`): `role=combobox`, a button with `aria-haspopup=listbox` (Headless UI's), and `role=listbox` itself, after the native forms (a `<select>` stays a select). A combobox that is a text input, text area or contenteditable, not read-only, is *editable* (a typeahead). `aria-expanded`, `aria-disabled`/`:disabled` and `inert` are read too.
- **Finding the list** (`page/aria.go` `ariaFindJS`): the listbox itself; else `aria-controls` and `aria-owns` (a listbox, a listbox inside the target, or the target when it holds options); the listbox of `aria-activedescendant`; a listbox inside the widget. Only shown lists count. When none is linked (Headless UI v2 sets `aria-controls` only sometimes, portals), a listbox that **appeared since the read before the Action** is taken when it is the only one (the "before" read records the shown listboxes in a `WeakSet` on the window); several new ones are an error naming the count, never a guess. An unrelated listbox already on the page is never taken.
- **Options:** the shown `role=option` elements of the list (and of its `aria-owns`), up to 500, named by `aria-labelledby`, else `aria-label`, else their text (Radix names its items through `aria-labelledby`). Selected: `aria-selected` or `aria-checked` true, Radix's `data-state=checked`, or `data-selected` (Headless UI v2, React Aria). Disabled: `aria-disabled=true` or `:disabled`.
- **Matching** (`page.PickARIAOption`): by name only. Rule 1: the name exactly, spaces collapsed on both sides (and zero-width characters dropped, `page.Collapse`). Rule 2: the same, ignoring case. Never a prefix or a part of a name. Several hits at the first rule that has any is an error listing them with their positions (`"Apple (option 1 of 2)", "APPLE (option 2 of 2)"`); no hit lists the options (`OptionError`, the first 12, then "and N more"). A `<select>`'s value rule is not used: custom options carry no value winuse can trust.
- **The Action** (`helper/pagearia.go`), through `runPage` (lane, Esc scope, staleness, Agent Cursor, dialog race):
  - *Trigger* (combobox or `aria-haspopup=listbox` button), collapsed: aimed with the click checks (enabled, visible, not covered), opened by the Headless trusted click (`page.Input.Click`, the same as `click`). Open already (`aria-expanded=true`): no click and no hit test on the trigger (an open popup may cover it or take the page's pointer events, as Radix does).
  - *Typeahead*: DOM.focus, select all, `Input.insertText` (set-value's text route without its read-back, now `Origin.ReplaceText`), then the option.
  - *Listbox*: the option directly.
  - The list is polled (50 ms) until the option shows, up to 2 s (`page.ARIAListWait`): lists that load or filter after typing. Then the option node is resolved (`Runtime.callFunctionOn` returning the node, `DOM.describeNode` for its backendNodeId, checked to still carry the name), aimed with the click checks (scrolled into view, enabled, visible, not covered) and clicked with the same trusted input. After the click that opened the list, the Agent Cursor glides on to the option (label `select "<option>"`) and presses there.
  - An option already selected in a multi-select list (`aria-multiselectable`), or in a listbox named itself, is left alone: a click would turn it off (agent-browser's rule). With a list winuse opened, a `Note:` says it is still open.
  - An editable combobox with an empty value keeps the text route (clears the field).
- **Read-back** (`page.ARIAChosen`, polled up to 1 s): the option reads selected afterwards; or a trigger's shown text (its text without its list) holds the name and did not already hold it while the option was unselected (a placeholder holding the name proves nothing); or, for a typeahead, its list closed and the field or the text beside it (up to the nearest ancestor holding another control: React Select shows the choice next to its input) holds the name. Otherwise: `the option "X" was clicked, but element N does not show it chosen afterwards (...)`, hint `the Action was delivered; do not repeat it` (delivered but unverified).
- **Refusals and Delivery:**
  - Nothing sent yet (the list was open already, or a listbox): `cannot set element 8: the list has no option "Kiwi"; its options are "Apple", "Banana"; nothing was sent`. Same for several matches, a disabled option (`its option "Durian" is disabled`), an option that cannot be clicked (covered, hidden), an empty name.
  - After winuse opened the list: the same text plus `; the click opened its list, which is still open`, delivered (hint `do not repeat it`). **The list is left open on purpose**: the error lists the options, and a retry with the right name needs no second click (the widget reads `aria-expanded=true`). Closing it would take Escape or a second trigger click, either of which a page may handle differently (Escape can trigger "discard changes?").
  - No list showed after the click: `it shows no list of options (role=listbox with role=option items) that winuse can find; the click was delivered, and no list showed`.
  - Typeahead: no list within 600 ms (`page.ARIANoListWait`) means a plain text field (free text, or a search box with no suggestions): the value is read back exactly as before. A list without that option keeps the typed text, read back, and prints `ok` plus `Note: element N holds the text typed, but no option was picked: the list has no option "cat"; its options are ...`. Several options of the name: delivered failure ("the text was typed into it, and its list is open").
- **Tab targets:** works the same in a background Tab (ADR 0017): a custom list is the page's own DOM, no native popup on the user's screen; the Agent Cursor stays hidden there as for every Action.
- **Docs:** help (`set-value` line), the skill's `browser` topic (one bullet), `test/e2e/README.md` (the subtest), code comments in `page/aria.go`, `page/choice.go`, `helper/pagearia.go`, `helper/pageelements.go`. No CONTEXT.md change (no new term; Element Reference's rule applies to option names as written) and no ADR (the Action uses ADR 0012/0016's Headless Page input as it is).

## Tests

- `internal/page/aria_test.go`: `PickARIAOption` (exact, collapsed spaces, any case, several at the case rule with positions, no prefix or part matches, no options), `ARIAChosen` (option selected; trigger text; another option shown; a placeholder holding the name; already chosen; typeahead closed on its value, the choice beside it, still open, closed empty), the JS shares one finder and reads a `<select>` before any role.
- `internal/helper/pagearia_test.go` (CDP fake: nodes resolved per backendNodeId, the option node returned by `Runtime.callFunctionOn` and described): a Radix-like trigger opened at its centre and the option clicked at its own point, both Agent Cursor labels, no foreground; an opened list without the option (delivered, still open), an open list without it and several of a name (nothing sent, no input), no list at all, a disabled option, an empty name; an unverified click and a verified one through the option's own state; a multi-select listbox leaving a selected option alone and clicking another; a typeahead (focus, select, insertText, then the option), free text with no list (no click), a list without the text (the note), clearing through the text route.
- `test/e2e/browser_pages_test.go` subtest `aria-lists` (user-run) on the new `test/e2e/testdata/page-aria.html`.

`go vet ./...`, `go vet -tags e2e ./test/e2e` and `go test ./...` pass.

## Live check (to run by a human)

These start the build's own helper, which makes an installed helper of another build exit.

1. `go test -tags e2e -v -run 'TestBrowserPages$/.*/aria-lists' ./test/e2e` (Chrome and Edge, winuse's launch profile): the Radix-like pointerdown/pointerup select, the unlinked Headless UI-like list, the typeahead, the multi-select listbox, the not-found / disabled / duplicate refusals, the foreground staying with the cover.
2. By hand on real libraries, each with `set-value` then `get-window-state` (or `--observe`): the Radix Select docs demo (radix-ui.com/primitives/docs/components/select), Headless UI Listbox and Combobox (headlessui.com), MUI Autocomplete "Combo box" demo (mui.com), React Select's first demo (react-select.com). Check: one call picks; the trigger or input shows the option; the Agent Cursor glides from the trigger to the option; a wrong name lists the options and leaves the list open, and a retry picks.
3. A search box with suggestions (a `<textarea role=combobox>` such as a web search): `set-value` with text that is also a suggestion clicks that suggestion (see Comments); with text that is not, it keeps the text and notes the suggestions after about 2 s.

## Comments

- **Unsure: typeahead search boxes.** On an editable combobox, set-value now clicks the suggestion whose name equals the text, when the list shows one. On a web search box that runs the search (as pressing Enter would), where set-value used to only fill the field. This is what the item asks ("type the text, then pick the matching option"), and the skill says to use `type-text` to type without picking; but it changes behaviour for a field that worked before. A narrower rule (pick only when `aria-autocomplete` is `list` or `none`, or only for non-free-text widgets) has no reliable signal across libraries. Decide after live check 3.
- **Unsure: read-back heuristics.** The trigger text and the "text beside the field" checks are containment, ignoring case. A trigger whose text holds the name for another reason after the click (a label like "Fruit: Banana or Apple") would read as chosen; the placeholder case is guarded. React Select's ancestor walk stops at an ancestor holding another control or at 4 levels.
- Free-text typeahead with a list shown but no match now waits up to 2 s for the list to settle before keeping the text; a combobox field that never shows a list costs 600 ms more than before.
- Virtualized lists (options rendered only when scrolled to) find only the rendered options; the error lists those. Options inside open shadow roots other than the widget's own root are not searched.
- The not-found case after opening keeps the list open (see What was built); native combo boxes close theirs again (desktop-parity 12). Pages differ on purpose.
