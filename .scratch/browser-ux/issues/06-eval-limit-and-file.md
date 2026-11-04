# 06: eval output past 4000 characters, and expressions PowerShell mangles

Status: done

**Evidence.**

- "The output gets cut at 4000 chars, so I'll page it." The agent stored the result in `window.__out` and printed it with one `eval "window.__out.slice($o, $($o + 3900))"` per 3900 characters, three or more evals per query.
- "PowerShell 5.1 strips double quotes from arguments passed to native executables, so the double-quoted selector would arrive broken": the selectors were rewritten as `a[href*=status]` and `a[href*=''t.co'']`, and every string in the scripts used backticks.

**Root cause.** `page.EvalLimit = 4000` is fixed, and `winuse eval` takes the
expression from argv only, where Windows PowerShell 5.1's native argument
passing drops embedded double quotes.

**Fix.**

- `winuse eval <window> <expression> --max <chars>`: raise the cut, up to 100000 (default stays 4000, so a stray huge result still cannot flood the agent's context).
- `winuse eval <window> --file <path>` (or `--file -` for stdin): the expression comes from a file, read by the CLI, so no shell quoting applies.

## Comments

Done on branch browser-ux: `page.EvalUpTo` / `EvalMaxLimit`, `EvalArgs.Max`, `parseEvalArgs` (`internal/cli/page.go`), usage text. Tests: `TestParseEvalArgs`, `TestEval`.
