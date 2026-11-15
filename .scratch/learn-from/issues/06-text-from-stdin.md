# 06: Text from stdin or a file (--stdin, --file)

Status: ready-for-human
Type: task
Branch: learn-skill-and-stdin, off main 03fc3a4
From: `.scratch/learn-from/agent-desktop.md` §1.1 (agent-desktop v1.2.0's `--stdin` for `type`, `set-value`, `clipboard-set`, #373); `.scratch/learn-from/README.md`, rank 9

## Ask

- Pass the literal text of `type-text`, `set-value` and `clipboard-set` from standard input or a file instead of the command line: keeps secrets out of the process command line, and avoids PowerShell 5.1 stripping quotes in native arguments.
- Decide the flag shape, how it combines with Batches (`batch -` already reads stdin), the encoding, the trailing newline, and the refusals.
- Help and the skill references.

## What was built

```
winuse type-text <window> [--post | --into <element>] [--interval-ms <n>] --stdin | --file <path>
winuse set-value <window> <element> --stdin | --file <path>
winuse clipboard-set --stdin | --text-file <path>
```

- **Shape.** The flag stands where the text would start and replaces it (`internal/cli/textsource.go`). On `type-text` it comes after `--post`, `--into <element>` and `--interval-ms <n>`. `--file <path>`, `--file=<path>`, and `--file -` for stdin, as `eval --file` does. `clipboard-set` spells its file flag `--text-file`, because its `--file` already copies files to the clipboard (unchanged); `--stdin` is the same on all three.
- **Where it is read.** In the CLI, before the helper is dialed. The request carries the text in the same field as a typed one (`TypeTextArgs.Text`, `SetValueArgs.Value`, `ClipboardSetArgs.Text`): no protocol or helper change, and nothing new crosses the pipe. `--trace` already redacts these fields by default.
- **Encoding.** UTF-8. A UTF-8 byte order mark is dropped. A UTF-16 byte order mark (LE or BE) decodes the rest as UTF-16, so a file written by Windows PowerShell 5.1's `>` or `Out-File` works. Anything else that is not valid UTF-8 (an ANSI code-page file) is refused: `the text from <path> is not UTF-8: save it as UTF-8`.
- **Trailing newline.** Exactly one trailing `\n` or `\r\n` is dropped; inner newlines and a second trailing one are kept. Files and pipes end with a newline the text did not mean (PowerShell's pipe to a native program adds one), and typing it would press Return after the text, which can submit a form. To type a final newline, end with two (or `press-key Return`).
- **Global flags after it.** With a source there is no literal text, so `--headed`, `--json`, `--trace`, `--observe` and its budgets after `--stdin`/`--file <path>` are flags again (`stripFlagsAroundText` in `headed.go`). Before it, in literal text, they stay text as before: `type-text 101 hello --file x` types `hello --file x`.
- **Refusals** (usage errors, exit 2, nothing sent, the helper never dialed):
  - text after the flag: `type-text takes the text or --stdin, not both; "hello" follows --stdin (type-text's own flags go before it)`;
  - `--file` with no path;
  - an unreadable or missing file: `reading the text from <path>: ...`;
  - more than 1 MiB (a request line holds at most 4 MiB);
  - not UTF-8 (above);
  - `--stdin` (or `--file -`) when stdin is a console or NUL: `--stdin reads piped or redirected input, and stdin is a terminal: pipe the text in, or use a file` (agent-desktop refuses terminal input too; reading would wait for the user).
- **Batches.** A step takes neither flag: `a batch type-text Action takes its text in the step: --stdin and --file go on the standalone command, and `winuse batch -` reads the whole Batch from stdin`. `batch -` owns stdin, so the two never fight; `batch -` is already the way to keep a Batch's text off the command line. A value that only contains the word (`set-value 4 say --stdin`) stays literal.
- **Behaviour change.** `type-text 101 --stdin`, `set-value 101 3 --file x` and `clipboard-set --stdin` used to type, set or copy those words literally. They now read the source. Text that merely contains the word elsewhere is unchanged.
- **Docs.** Help (`type-text`, `set-value`, `clipboard-set` blocks and one example each), the skill core (the PowerShell 5.1 gotcha points at `--file`), topic `input` (a "Text from a file or stdin" section, the Read-when line, the clipboard line), topic `commands`, `docs/codex-parity.md` (`type_text.text`, `set_value.value`).

Not done (left for later if wanted): agent-desktop also leaves a `--stdin` value out of the result's read-back. winuse's helper prints its usual read-back and failure text for a sourced value, as for a typed one; password fields expose no value through UI Automation, so a password is not echoed there. `--trace-unredacted` keeps the text, as it does for typed text.

PowerShell note, in the skill: Windows PowerShell 5.1 pipes text to a native program in `$OutputEncoding` (ASCII by default), so non-ASCII piped text arrives as `?`. Use `--file` there; PowerShell 7 pipes UTF-8.

## Tests

- `internal/cli/textsource_test.go`:
  - `TestDecodeText`: UTF-8, BOM, UTF-16 LE/BE with a surrogate pair, one `\n` or `\r\n` dropped (two kept), a lone `\r` kept, ANSI and odd UTF-16 refused.
  - `TestTypeTextReadsTheText`: `--file`, `--file=`, with `--into`, `--post` and `--interval-ms`, `--headed` after the source is the flag, literal text before it stays text, `--stdin` and `--file -` from a swapped stdin.
  - `TestTextSourceRefusals`: text after the source, flags after it, no path, a missing file, over 1 MiB, a terminal stdin.
  - `TestSetValueReadsTheValue`, `TestClipboardSetReadsTheText`: the same, `--file` on `clipboard-set` still copying files, `--headed` and `--json` after the source.
  - `TestObserveAfterATextSource`: `--observe` after the source.
  - `TestBatchStepsTakeNoTextSource`: steps refused on the command line and in `batch -`; a literal word kept.
  - `TestTextSourceUsageErrorsExitTwo`: exit 2 with the usage line through `Main`, without dialing.
- The existing keyboard, headed, observe, help and batch tests pass unchanged.

`go vet ./...`, `go vet -tags e2e ./test/e2e` and `go test ./...` pass. (`internal/page`'s `TestReadOverlapsItsRoundTrips`, which this branch does not touch, counts round trips under timing and failed in 2 of 5 runs here; it passes on a rerun.)

## Live check (to run by a human)

These start the build's own helper, which makes an installed helper of another build exit.

- Notepad (`launch-app notepad`, read it): `Set-Content -Encoding utf8 q.txt 'He said "stop" — ok'`, then `winuse type-text <window> --into "name:Text editor" --file q.txt`: the quotes and the dash arrive, no newline after them.
- The same text from Windows PowerShell 5.1's `'He said "stop"' > q16.txt` (UTF-16): typed the same.
- `'abc' | winuse type-text <window> --into "name:Text editor" --stdin` in PowerShell 5.1 and 7 (no Return typed after `abc`); a non-ASCII pipe in 5.1 shows `?` (expected, documented).
- `winuse set-value <window> <field> --file q.txt` on a settable field, read back.
- `winuse clipboard-set --text-file q.txt`, then `winuse clipboard-get`: the text without the trailing newline.
- `winuse type-text <window> --stdin` run from an interactive console with nothing piped: refused at once, no wait.
- `winuse --trace t.jsonl type-text <window> --into "name:Text editor" --file q.txt`, then `winuse trace show t.jsonl`: the text is redacted.

## Comments
