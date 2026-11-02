# 08: Skill, README and usage text

**What to build:** teach the new behaviour in three places: SKILL.md, the README command list, and the CLI usage text. Cover:

- which Actions are Headless;
- the screenshot change;
- Window Actions;
- Element scroll;
- set-value on toggles;
- dialog Return and Escape;
- classic edit typing;
- `--post`.

For SKILL.md, load the writing-for-agents skill first. Follow the unslop rules and use plain ASCII. TestSkillShipsAlone and the other skill tests must pass.

**Blocked by:** 01-07

**Status:** done

- [x] SKILL.md updated; skill tests green
- [x] README updated
- [x] Usage text covers every new form

## Comments

### 2026-11-02: landed

- `skills/winuse/SKILL.md` (written with writing-for-agents, plain ASCII): "Choose Headless or Headed" lists the Headless Actions per CONTEXT.md, notes the Invoke fallback, and gains a "Keep the user's foreground" decision guide. "Read the Window State" teaches the `Screenshot: <path> (WxH px, scale S, window at X,Y WxH)` line, headless capture, the minimized refusal, `--headed`, and screenshotting popups by their own Window ID. "Fill in fields, keys and text" covers `set-value <ref> on|off`, the dialog Return/Escape and classic-edit routes (both print `ok`, so the agent judges by the Window), and a "Posted Keys" subsection: opt-in, best effort, no modifiers, the posted output, and the admin, locked, minimized and modal-blocked refusals. New "Scroll" and "Manage Windows" sections cover the limits from tickets 02 and 03. The Batch section and command reference are updated.
- `README.md`: the background-actions paragraph lists the new Headless Actions and `--post`, screenshots no longer take the foreground, and the command table gains `scroll` and the Window Actions.
- `internal/cli/cli.go`: the `--headed` option is short again. A separate "headless" paragraph is the single list of default-Headless forms. The type-text and press-key lines no longer repeat each other. set-value names `on|off` for toggles.
- `go build ./... && go vet ./... && go test ./...` green.
