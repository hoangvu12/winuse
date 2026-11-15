# 03: Three skill lines (the user's clipboard, an Action and its wait, Escape in web apps)

Status: ready-for-human
Type: task
Branch: learn-skill-and-stdin, off main 03fc3a4
From: `.scratch/learn-from/agent-desktop.md` §3.2 (agent-desktop PR #667's rewritten core skill, lines 89-91 and 194; issue #534); `.scratch/learn-from/README.md`, "Smaller ones"

## Ask

Add to the skill, short and in its own voice:

- Never move a value through the user's clipboard to copy it between Apps (it replaces what the user copied); read it from the Window State and type or set it instead.
- Put an Action and the `wait` that checks it in one Batch (one call, not two).
- In web apps, Escape can open a "discard changes?" prompt; prefer the Window's own close or cancel control.

Check first that the skill does not already say these; keep within the core's size budget (`internal/skill/skill_test.go`).

## What was built

Checked first: the skill said none of the three. The clipboard topic suggested `clipboard-set` then Ctrl+V for long text and said nothing about the user's clipboard; `batch-and-wait` had Batches and waits in separate sections, never together; nothing warned about Escape in a Page.

- **Clipboard.** Core, Gotchas (one bullet): "Never copy a value between Apps through the clipboard: it replaces what the user copied. Read it from the Window State and `set-value` or `type-text` it." Topic `input`, Clipboard: the same rule with the Copy/Paste wording, and for the paste route the topic already gives (`clipboard-set`, then Ctrl+V), `clipboard-get` first and put the user's text back after (agent-desktop's "read it first and put it back").
- **Action and its wait.** Core, The loop: "send them as one `winuse batch` ..., with the `wait` that checks an Action in the same Batch". Topic `batch-and-wait`, Wait for a change: one paragraph with the example `batch 101 click name:Save then wait text Saved`.
- **Escape in web apps.** Topic `browser`, Act on a Page: one bullet, "Escape can open a "discard changes?" prompt inside the Page ... Close a dialog, panel or menu with its own close or Cancel control (`click <window> name:Cancel`)". It sits in the Page topic because a prompt drawn inside a Page is not a new Window, so no `Note: a new Window opened` warns about it (§3.2).

The core grew from 13,611 to 13,828 bytes and 237 lines (limits 16 KiB and 300 lines).

## Tests

No new test: `internal/skill` already pins what these lines touch. `TestSkillCoreKeepsTheRulesFirst` (size and line budget), `TestSkillTopicsAreListedAndResolve`, `TestEveryOldSectionHasAHome` and `TestEmbeddedSkillMatchesTheShippedFiles` pass. The Batch example's step (`wait text Saved`) is a step form `internal/cli/page_test.go` already parses.

`go vet ./...`, `go vet -tags e2e ./test/e2e` and `go test ./...` pass. (`internal/page`'s `TestReadOverlapsItsRoundTrips`, which this branch does not touch, counts round trips under timing and failed in 2 of 5 runs here; it passes on a rerun.)

## Live check (to run by a human)

None needed for behaviour. Optionally read `winuse skill`, `winuse skill input`, `winuse skill batch-and-wait` and `winuse skill browser` from a build of this branch to see the lines in place.

## Comments
