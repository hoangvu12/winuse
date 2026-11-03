# 01: Clipboard commands

**What to build:** `clipboard-get [--format text|image|files] [--out <path>]`, `clipboard-set <text> | --image <png> | --file <path>...`, `clipboard-clear`. agent-desktop has all three (`../../headless-gaps/agent-desktop-parity.md` section 1, Clipboard rows).

- OpenClipboard needs no foreground, so all three are Headless.
- Copy agent-desktop's hung-owner guard: OpenClipboard retries for a short bounded time; a delay-rendered format whose owner does not answer is abandoned on a worker with a timeout, never blocking the helper.
- Text as CF_UNICODETEXT; image as CF_DIB/PNG written to `--out` (default a temp file, path printed); files as CF_HDROP paths, one per line.
- Output for get: the content (text), or the file path. A secret in the clipboard is printed as-is, as agent-desktop does.

These are not aimed at a Window. CONTEXT.md says every Action is aimed at exactly one Window (`CONTEXT.md:13-14,49-51`). Add the term **Desktop command**: not an Action, no Agent Cursor, not a Batch step, serialized on the action gate.

**Blocked by:** none

**Status:** done

- [x] Win32 clipboard layer with the hung-owner guard
- [x] Three commands in CLI, protocol and helper
- [x] Offline tests: format selection, owner timeout
- [x] Opt-in e2e: set then get text, image and files
- [x] CONTEXT.md "Desktop command"; SKILL.md

## Comments

### 2026-11-03: triage decision (user)

Do it like agent-desktop: get, set and clear for text, image and files, with the hung-owner guard. No clipboard-history exclusion formats and no "ask the user first" rule in the skill (both were winuse extras; the user declined them). The CONTEXT.md term **Desktop command** is accepted (not an Action, no Agent Cursor, not a Batch step, serialized on the action gate); this ticket adds it.

### 2026-11-03: round 3 probes (`../headless-gaps/probes-round3.md`)

Note only (the user's decision stands: no history exclusion on `clipboard-set`): the registered `ExcludeClipboardContentFromMonitorProcessing` format was measured to keep data out of Win+V history. Headless-max 20 uses it for its temporary Ctrl+V data.

### 2026-11-03: landed (laptop, branch `laptop/dp-01-clipboard`)

What landed, like agent-desktop (`crates/windows/src/input/clipboard/` at 9eb9b29):

- `clipboard-get [--format text|image|files] [--out <path>]`: the text exactly as it is, no newline added (default format text); files one path per line; an image written as a PNG to `--out` or a new `%TEMP%\winuse\winuse-clipboard-<n>.png`, printed as `<path> (WxH px)`. Images: the registered "PNG" format as is, else CF_DIBV5, else CF_DIB, converted to PNG. A clipboard without the format fails (exit 1) with `the clipboard holds no <format>` or `the clipboard is empty`, and the hint names the formats it holds (agent-desktop answers `found: false` with success; the text CLI has no such field, so it fails, which ticket 07's `--json` can map back).
- `clipboard-set <text> | --image <png> | --file <path>...`: one representation replaces everything. Text is literal like type-text's (args joined by spaces; a `--headed` in it stays text). `--image` writes "PNG" plus a 32-bit CF_DIB; `--file` writes CF_HDROP (every path must exist; `--file` may repeat per path, as agent-desktop's `--file-url`). Bad arguments are refused before the clipboard is opened.
- `clipboard-clear`.
- Guards: OpenClipboard 5 attempts 5 ms apart (agent-desktop's Chromium budget), then "another program has the clipboard open (window N); nothing was sent". A clipboard owner that does not answer WM_NULL (SendMessageTimeout, SMTO_ABORTIFHUNG, 250 ms; IsHungAppWindow when UIPI refuses the message to an elevated owner) is refused before any read or write (EmptyClipboard's WM_DESTROYCLIPBOARD waits on the owner too). Every session runs on a fresh OS-locked thread the caller waits for at most 5 s; a worker parked in a delay render past that is abandoned, and every clipboard command is refused until it returns. A read is retried once when the sequence number moved. Writes open with a message-only owner window on its own pumping thread and re-check ownership afterwards (a failure after EmptyClipboard says "may have taken effect").
- Desktop commands: serialized on the action gate, no Agent Cursor, not Batch steps, no headed form.

One fix beyond agent-desktop, found live: the CF_DIBV5 that Windows synthesizes from a CF_BITMAP (.NET's `Clipboard.SetImage`) repeats its three masks after the 124-byte header, so the pixels start 12 bytes later; agent-desktop's decoder reads it shifted. winuse skips the repeated masks when they are there, honours a V4/V5 alpha mask (zero is opaque), and refuses non-BGRA masks instead of misreading them.

Files: `internal/win/clipboard.go` (sessions, guards, live calls), `internal/win/clipdata.go` (text, CF_HDROP, DIB and PNG marshalling), `internal/helper/clipboard.go`, `internal/cli/clipboard.go`, `internal/protocol/protocol.go` (`ClipboardGetArgs`, `ClipboardSetArgs`), `internal/helper/state.go` (Services.Clipboard, registry), `internal/cli/cli.go` (usage), `internal/cli/headed.go`, CONTEXT.md (**Desktop command**), SKILL.md (Clipboard section, reference), README, `docs/codex-parity.md` (extensions, one request at a time), `test/e2e/README.md`.

Tests: offline `internal/win/clipboard_test.go` (format selection: PNG before CF_DIBV5 before CF_DIB; hung owner refused before opening, for reads and writes; the owner timeout abandons the worker and refuses get/set/clear until it returns; open retry and the busy holder; re-read on a sequence change; write formats, ownership check, bad content refused first), `internal/win/clipdata_test.go` (round trips, orientation, alpha, the measured synthesized CF_DIBV5), `internal/helper/clipboard_test.go` (outputs, hints, file checks, the action gate), `internal/cli/clipboard_test.go` (parsing, literal `--headed`, usage exits). `go build ./...`, `go vet ./...`, `go test ./...` and `go vet -tags e2e ./test/e2e` pass.

Live (laptop, Windows 11 26200, `WINUSE_E2E=1`, `TestClipboard` only): passed. Text (Unicode, a literal `--headed`, CRLF) round trips and PowerShell's `Get-Clipboard` sees it; a PNG round trips pixel-exact and .NET's `Clipboard.GetImage` reads it as 6x4; a .NET bitmap reads back 7x5 with the right pixel at 0,0 (failed before the DIBV5 fix); files round trip and PowerShell's `FileDropList` matches; a missing `--file` changes nothing; clear empties it; the foreground never moved. The user's clipboard first held text marked `ExcludeClipboardContentFromMonitorProcessing`, so the test skipped; the user said to replace it, and the run used a plain-text placeholder, which the test put back. Not run live: a hung or slow clipboard owner (offline only).
