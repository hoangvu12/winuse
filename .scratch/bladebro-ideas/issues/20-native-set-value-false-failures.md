# 20: Native `set-value` false readback failures

Status: resolved
Type: prototype
From: ticket 14 (pattern 5); not from bladebro

## Question

Real sessions saw `set-value` report "delivered, do not repeat" when the
value had in fact gone in:
- **Notepad, multi-line text:** the readback came with `\r` line endings.
  `ValueMatches` is meant to treat CRLF and LF alike (`internal/linebreak`),
  so find out which path missed it. Candidates are the Notepad version (the
  Win11 RichEdit Notepad stores CR only) or a different route.
- **Explorer rename:** a file rename that the agent then confirmed had
  worked.

Reproduce both against the real Apps through winuse:
- Notepad on a new, unsaved document.
- An Explorer rename in a scratch folder the test creates.

Record the exact readback string and code path, and suggest the fix.
Together with ticket 09, this is the "false failure" family. Each one costs
a turn and makes agents distrust correct results.

## Answer

**Neither failure reproduces on v0.7.0.** Both were real, and fixed in v0.2.0
(commits `9f5a564` and `3231656`,
`.scratch/headless-max/issues/24-set-value-readback-false-alarms.md`). The
helper log shows this machine ran v0.1.0 and older helpers until about
2026-11-07, so the sessions in ticket 14 came from builds before the fix.
Scripts and traces are in the chat's scratch folder: `exp\scripts\`,
`exp\notepad\`.

**Notepad**
- `RichEditD2DPT` stores CR only: LF, CRLF and lone-CR input all read back
  as `a\rb\rc`.
- `set-value` (`--file`, arguments, `--observe`, a Batch step) passes in
  every variant, through `internal/uia/headless.go` `verifyValue` →
  `linebreak.Same`.
- `type-text --into` with a newline is refused, `NO_HEADLESS_ROUTE`, with
  nothing sent. That's by design.

**Explorer**
- Renaming through the rename box, the details Name cell, or the list item
  itself all return `ok`, and the file is renamed on disk.
- The original cause was a write to the list item, which Explorer rebuilds,
  so the item read back `""`. That's now handled by
  `internal/uia/renamed.go` `renamedInPlace`.
- F2 and Return are refused Headless. Context menu > Rename plus
  `press-key Return --post` works.

**Leftover gaps, now ticket 21**
- `wait <ref> value` compares exactly (`internal/helper/waits.go:509`), so
  multi-line text in Notepad never matches. Inside a Batch, the timeout is
  reported as `delivery_uncertain`.
- `wait text` with a newline can never match, because the tree prints line
  breaks as spaces.
- `renamedInPlace` accepts only an exact `""` readback. A stale readback of
  the old name would bring the false alarm back.

**Side effects of the run, reported to the user**
- `launch-app notepad` took the foreground on its own.
- Notepad restored the user's previous session tabs. Two of them, for files
  missing on disk, showed an error the experiment dismissed with OK, which
  dropped them from Notepad's session.
