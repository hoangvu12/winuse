# 14: How often real sessions hit these problems

Status: resolved
Type: research
From: `map.md`

## Question

Go through local agent transcripts and winuse traces. For each pattern,
count how many sessions hit it:
- `eval` used to read page text or lists (12, 17)
- scrolling past the bottom (03)
- a silent HTTP error or a challenge page (02, 05)
- an `--observe` read too early (07)
- a readback failure on a rich-text editor (09)
- console guesses (19)
- native tables read out of the tree (13)
- repeated waits on the same site or App (16)
- a Batch typo after earlier steps had acted (04)
- an `ok` click that did nothing (06)

Use the counts to reorder tickets 01–09. Quote only short, redacted
snippets.

## Answer

**What was scanned**
- 3,242 transcripts in total:
  - 1,936 Claude Code transcripts (1,322 main sessions, 614 subagent runs);
  - 1,306 Zeron journals, of which the 51 from the pi harness are new data.
- 172 sessions ran winuse, in about 5,290 calls.
- There are no winuse trace files on this machine.

| # | Pattern | Count | Tickets |
|---|---|---|---|
| 8 | Fixed `sleep` or `wait` | **2,289 of 5,290 calls (43%) in 130 sessions, about 3.4 h of fixed sleep.** `action; sleep N; read` in one command 948 times in 73 sessions. Main targets: claude.ai (476 calls, 64 polling loops), localhost dev servers (430), the AWS console (8 waits that timed out at 20–90 s), Zeron, Discord. About 35 waits timed out in about 16 sessions. | 07, 16 |
| 1 | `eval` to read page text or lists | **857 calls in 71 sessions** (of 77 that used eval at all). `document.body.innerText` alone: 396 calls in 52 sessions. This is a floor: the 512 `eval --file` scripts weren't inspected. | 12, 17 |
| 10 | Page click printed `ok` but did nothing | about 20 cases in about 12 sessions, at 2–5 turns each (checked by hand) | 06 |
| 3 | `navigate` said ok on an error page | about 14 cases in 9 sessions: 404, 403, an expired sign-in, AWS access denied. 2 Cloudflare pages, which the state read already labels. | 02, 05 |
| 6 | Console or errors read through eval | 13 calls in 8 sessions | 19 |
| 5 | `set-value` "delivered, do not repeat" | 8 in 8 sessions, only 1 on a Page rich-text box. **Several were native false failures:** Notepad multi-line text read back with `\r`, and an Explorer rename that actually worked. | 09, new 20 |
| 4 | `--observe` read too early | none directly. `--observe` was used only 14 times; agents add `sleep` instead (see #8). | 07 |
| 7 | Native table or list pulled out of the tree | 22 calls in 5 sessions, Explorer only | 13, 17 |
| 2 | Scrolls that changed nothing | 1 session | 03 |
| 9 | Batch typo after earlier steps acted | 0 (75 batches, 19 sessions) | 04 |

**What this changes in the ranking:**
- **Highest:** waits (07, 16), page text as data (12, 17) and click outcome
  (06).
- **Medium:** HTTP and challenge notes (02, 05), and console capture (19).
- **Low:** scroll note (03), native extract and Batch checks (04).

The extracts used for this were deleted after the counts were recorded,
because they held unredacted transcript text.
