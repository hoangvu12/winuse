# 07: Posted Keys (`--post`)

**What to build:** `press-key <window> <key> --post` and `type-text <window> --post <text>` post keys to the Window's focused control without the foreground. press-key posts WM_KEYDOWN and WM_KEYUP. type-text posts WM_CHAR.

- A chord with modifiers is refused with "nothing was sent".
- The output is `ok (posted; ...)`.
- `--post` together with `--headed` is a usage error.
- Batch steps accept `--post`, and those steps count as Headless for the Batch's scope.

See spec: "Posted keys, opt-in".

**Blocked by:** 05, 06 (shared key routing)

**Status:** done

- [x] win: posting with the right lParam (repeat count, scan code, transition bits)
- [x] Protocol `post` on PressKeyArgs and TypeTextArgs; CLI parsing in both positions; Batch steps
- [x] Refusals: modifiers, administrator Window, locked desktop
- [x] Offline tests

## Comments

### 2026-11-02: landed

- `internal/win/postkeys.go`: `PostKeys(target, chord)` posts WM_KEYDOWN for each key in order, then WM_KEYUP in reverse (`postedKeyMessages`); `PostText(target, text)` posts WM_CHAR per UTF-16 unit with lParam 1 (`postedCharUnits`: a newline, a carriage return or a CRLF pair becomes one carriage return, a tab stays a tab). Both return how many messages were posted and refuse in a test binary (pinned in `liveguard_test.go`). The pure `keyLParam(scan, extended, up)`: repeat count 1, scan code (MapVirtualKey, low byte) in bits 16-23, extended bit 24, context code 0, previous-state and transition bits for a key up.
- `internal/keys`: `Key.Modifier()` (Control, Shift, Alt, left, right and generic).
- `internal/helper/keyroute.go`: `postKeys` resolves the Window, reads the focused control through the KeyRoute probe (falling back to the Window itself and its bounds for the cursor), and runs `runKeyHeadless` with `posted` set: locked desktop and administrator Window refused with "nothing was sent", no Ready check (the spec lists only those two refusals), passive Esc scope, cursor `press <key>` or `type` at the focused control. Output `ok (posted; the app may ignore posted keys, read the Window State to check)`. A post that sent nothing is "nothing was sent"; a partial post may have taken effect. press-key refuses a chord with a modifier before anything (`modifierRefusal`, "nothing was sent", hint to drop --post or use set-value). `post` with `headed` fails in the helper too (`postFlagsError`), so a headed Batch's posted step stops the Batch.
- Routing: `headlessStep` treats a `post` step as Headless for the Batch's scope without a probe (documented: Posted Keys are not Headless Actions); `keyRoute` skips the probe for posted and headed steps.
- Protocol `Post` on `PressKeyArgs` and `TypeTextArgs`. CLI: `--post` right after the Window on type-text (later it is text), anywhere after the Window on press-key (`stripPost`); Batch steps parse it the same way; `--post` with `--headed` (standalone or a headed Batch with a posted step, `batchPosts`) exits 2. Usage lines now read `type-text <window> [--post] <text>` and `press-key <window> <key> [--post]`; the helper's usage hints match. SKILL.md is left to ticket 08 (the skill tests check commands only).
- Tests: `TestKeyLParam`, `TestPostedKeyMessages`, `TestPostedCharUnits` (win), `TestModifier` (keys), `TestPressKeyPostPostsToTheFocusedControl`, `TestTypeTextPostPostsCharacters`, `TestPostRefusesChordsWithModifiers`, `TestPostRefusalsAndFailures`, `TestPostWithHeadedIsAUsageFailure`, `TestHeadlessStepPostedKeys`, `TestBatchPostedStepsKeepTheHeadlessScope` (helper), and the CLI parsing, usage-exit and batch-step cases.
- Risks to measure live: whether Chrome's address bar or other Chromium/WinUI apps act on posted WM_KEYDOWN Return (the spec's e2e measurement); posted keys to a Window disabled by a modal dialog are not refused (no Ready check), they just land in the disabled owner.

- 2026-11-02 orchestrator: changed after review. Posted Keys now run the Ready check like other Headless Actions (modal dialog or minimized refused, nothing was sent). A key posted to an owner disabled behind its modal dialog would act behind the dialog. `headlessRefusals` lost its `ready` parameter; the two step-order tests expect "ready".

- 2026-11-02 review fix: a Batch now refuses a step with `post` that is headed, by its own flag or the Batch's, while it prepares its steps (`postsHeaded` in batch.go, message `batch: Action N (kind): kind: --post and --headed cannot be combined` with postFlagsError's hint), so no earlier step acts. `TestBatchRefusesPostWithHeadedBeforeAnyStep` covers a normal step 1 and a bad step 2. The commands' own postFlagsError check stays: standalone requests from other clients still reach it, and it is defence in depth for a Batch.
