# Batches and waits

Read when: you want to see an Action's effect in the same call (`--observe`, reads as Batch steps), send a Batch (`then` steps, `batch -` from stdin, `in <window>` and `in new`, failed steps, its budget) or wait for an effect (every `wait` form).

This is one topic of the winuse skill. `winuse skill batch-and-wait` prints it, and `winuse skill` prints the core, SKILL.md, which holds the safety rules, the Esc stop and recovery.

## Batches

A Batch runs a routine sequence on a Window in one call, in order, and
prints each step as `Action N (<action>): <output>` (more lines indented),
then any `Note:` lines.

```
winuse --headed batch 101 click id:field then type-text hello world then press-key Return
```

Steps take the standalone arguments without the Window. A separate `then`
starts a new step; quote text containing it. `winuse batch -` reads stdin:
the Window and flags on line 1, then one step per line without `then`; `"`
groups words (`\"` inside), and blank and `#` lines are skipped. A
malformed Batch or unknown Window sends nothing. Batch only sequences you
understand well. A Batch has 60 s; `--timeout <s>` (up to 300) after the
Window widens it (a later one is a `wait` step's).

A step starting `in <window>` acts in that Window, and `in new` in the one
the step before it opened, printed as `(<action> in <id>)`:
`batch 101 click name:Delete then in new press-key Return`. A new Window
has no Window State: name its Elements by `name:`/`id:`, or send keys.

A failed step prints `failed: <message>` and its hint, and the rest run.
The Batch exits 1 naming them: retry only those, reading the Window State
first for one marked `uncertain` (it may have acted). `--stop-on-error`
after the Window stops at the first failure, as Esc and a spent budget do.

Without `--headed` a step with no Headless route is refused with nothing
sent; `--headed` makes every step Headed under one banner, as the example
needs. Page and tab commands are steps too, and so are the reads:
`get-window-state` and `find` take their own flags and no Window, so one
Batch can act, read what opened and act again:
`batch 101 click name:Open then in new get-window-state --interactive`.

## See the effect in the same call: --observe

`--observe` after any Action on a Window or Tab, or after `batch <window>`
(before its first step), reads the Window once the Action is done and
prints its Window State under the output, saving the turn a separate
`get-window-state` costs:

```
winuse --observe set-value 101 id:qty 3
ok
Window: "Order", App: shop.exe.
Changed since this agent's last read of window 101 (~ changed, * new, - gone); Element Indexes not listed are unchanged:
		~12 edit (settable, string) Quantity Value: 3 ID: qty (was: edit (settable, string) Quantity Value: 1 ID: qty)
		~19 text Total: 29.97 (was: text Total: 9.99)
		*41 button Remove ID: remove
```

- `--observe` (diff) compares by Element Index with your last read of that
  Window: an index not listed names an Element with the same line as before,
  `~` one whose line changed (the old line in `(was: ...)`), `*` one past
  your last read's end, `-` one no Element has now (an Action on it fails as
  stale, nothing sent). `No change since ...` means nothing changed: wait
  (`wait <window> text ...`) if the effect comes later.
- It prints the whole State instead, with a `Note:`, when you had no earlier
  read or when an Element inserted or removed in the middle moved indexes
  (the diff would not be shorter). Use its indexes as printed.
- `--observe=full` always prints the whole State. Both replace your Window
  State as `get-window-state` does, and both take its budgets and defaults:
  shared IDs left out and a 28000-character cap, `--interactive`,
  `--depth N`, `--compact`, `--max-chars N` (0: no cap), `--out <file>`,
  and `--full` for Codex's text, every ID, uncut.
  They go before the command or anywhere after it, but not inside type-text's
  text or set-value's value, where they would be text (as `--headed`).
- A failed Action prints no observation. A Batch observes after its last
  step, failed steps included. A read that fails is a `Note:`; the Action's
  own result stands.
- On a Page, the read comes right after the Action: a page that reacts late
  may not show it yet.
- To read again with no Action, after a `wait` or something the user did,
  `winuse get-window-state <window> --diff` prints the same diff, under the
  same rules and budgets (not with `--root` or `--skeleton`, which read part
  of the Window). It is a Batch step too: `... then get-window-state --diff`.

## Wait for a change

When an Action starts something slow, wait for its effect instead of
reading the Window State over and over:

```
winuse wait window "Save As"                   # a Window of that App, or whose title holds the text, shows: prints its line
winuse wait window "Save As" --gone            # no such Window is left
winuse wait <window> text <text>               # the text shows in the Window
winuse wait <window> <ref> enabled             # also disabled, gone, value <v>, checked, unchecked, expanded, collapsed
winuse wait 500                                # a plain pause, in milliseconds (at most 300000)
```

- A wait lasts 10 seconds by default (`--timeout <seconds>` up to 300);
  one that runs out fails with what it saw last, such as
  `it reads disabled`.
- `<ref>` comes from your latest Window State and resolves once, when the
  wait starts. `gone` also succeeds when its Window closes; any other wait
  whose Window closes fails with `the Window closed`. A wait for something
  the Element cannot show, such as `value` without `Value:`, fails at once.
- A wait only reads: it does not renumber your indexes, and an Action can
  run meanwhile.
- With a Browser Connection, `text` waits in the Page (see "Act on a Page"
  in `winuse skill browser` for the other Page waits), and an element wait takes the frame's
  Elements, not the Page's.
