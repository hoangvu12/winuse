# trycua/cua on Windows, 2026-11-07

Research for the user's question "is cua good / one of the leading". Reading only; nothing of cua was run. `cua/` is a depth-50 clone of trycua/cua main on 2026-11-07; `PW/` is `cua/libs/cua-driver/rust/crates/platform-windows/src`.

## Verdict

Keep ADR 0002, but amend it: its "built around background operation, which we dropped" reason no longer holds. winuse went headless since (ADRs 0010, 0011, 0013), so cua's design now points the same way. The other reasons hold or are stronger: cua is not faster on Windows, ships default-on telemetry (PostHog, `cua-driver/src/telemetry.rs:28-29,242`), churns hard (7 driver pre-releases in two days), and is Rust and MCP-first (against ADRs 0003, 0001).

## What cua is

A platform: Cua Spaces (agent desktops on macOS and Linux), Cua Driver (local background driver for macOS, Windows and Linux, MCP first, `cua-driver call` CLI second), Lume VMs, models, Cua Bench. 31.9k stars, 2.4k forks, about 6.5k commits, MIT, still pre-release. Windows crate: 113 Rust files (64 at ADR 0002's time); Windows e2e on `windows-latest` (`.github/workflows/e2e-rust-windows.yml:237`).

## Side by side on Windows

| | cua (verified in code unless marked) | winuse |
|---|---|---|
| Process | Daemon on `\\.\pipe\cua-driver`; `call` needs it (`cua-driver/src/cli.rs:2569-2584`); optional UIAccess worker | Helper behind a named pipe |
| Tree read | One `BuildUpdatedCache(TreeScope_Subtree)`, control view, 3 retries (`PW/uia/mod.rs:326-409`); new `CUIAutomation` per walk (`mod.rs:300-303`) | One cache request per expanded node; persistent COM homes (ADR 0009) |
| Read cost | Docs: 2-3 s for Chrome's ~5,000 nodes (`get_window_state.rs:189-191`); issue #4906 (not verified): get_window_state 89-272 ms, list_windows ~1.2 s | Faster (`spec.md`) |
| Actions | Background by default (Invoke/Expand, point hit-test to UIA, PostMessage), else `background_unavailable` until the agent opts into foreground (`WINDOWS.md`) | Same idea |
| Output | Markdown tree plus element rows plus a screenshot by default; 250-element default cap (`get_window_state.rs:144-149,176-180,212`) | Text tree, 3,000-element cap |
| Browsers | CDP with endpoint-ownership checks | CDP or the extension |

## Worth taking, ranked

1. Late opened-Window report: return at once, keep watching, report on the next result (`rust/Skills/cua-driver/EMBEDDING.md:265-309`, macOS only: element click 570-750 ms to 250-350 ms). Done here as ticket 01.
2. Don't block on cursor travel: cua skips the glide without a session key (`PW/overlay.rs:368-385`) and caps a batch step's cursor wait at 120 ms. Ticket 02.
3. Diff reads: `since: <snapshot_id>` returns only added/changed/removed rows plus a new id (`get_window_state.rs:150-157`). winuse has nothing like it; it would cut tokens in loops that re-read one Window. Would live in `internal/wstate` and the element store.
4. Read fallbacks: retry the read on a transient E_FAIL (`mod.rs:381-409`); walk from the desktop root filtered by pid for an empty CoreWindow/UWP wrapper (`mod.rs:430-478`); an MSAA walker for LibreOffice and other SAL windows (`mod.rs:337-364`, `PW/msaa.rs`). No MSAA tree walker in winuse.
5. A query filter on reads that keeps original indexes, with a completeness flag (`docs/tool-output-format.md`).
6. Measure cua's EnableWindow guard (`PW/uia/fg_bypass.rs:1-33`, claimed 91% to 0 of 507) on XAML and Chromium Invoke; winuse left it unmeasured (`../headless-max/issues/03-enablewindow-guard.md:41`).

## Not to copy

A whole-subtree read (one stalled Chromium renderer blanks it, ~10 s; cua had to add a level walker, `mod.rs:713-727`, #4121); a new `CUIAutomation` per walk; a screenshot on every read; pen and touch injection, the UIAccess worker, telemetry; the AGPL `cua-perception` and FSL `cua-driver-bench`.

Verified in code: the walk, process model, background-first actions, `fg_bypass`, `since` and the element cap, CDP ownership, telemetry, CI. From docs or issues only: cua's latency numbers and the deferred window watch (macOS).
