# 21: No Headed Action without `--headed`

**What to build:** every Action that would take the foreground, the real pointer or the real keyboard refuses with "nothing was sent" and the exact `--headed` retry, unless the agent passed `--headed`. Today several commands choose Headed on their own, from the live Window or from their arguments, and the user only finds out when their screen is taken.

ADR 0013 rule 3, as the user decided on 2026-11-03: winuse never takes the user's screen on its own. That covers the routing of ADR 0011 Decision 4 too ("anywhere else they stay Headed"), which the first draft of ADR 0013 had left out.

Paths that go Headed without `--headed` today (check each one, and find any others):

- `type-text` with no Headless route, including a covered Windows 11 Notepad that reports no focused control (`internal/helper/keyboard.go`, the "ok (typed Headed: ...)" output).
- `press-key` with no Headless route, anything but Return or Escape on a classic dialog (`internal/helper/keyboard.go`, `keyroute.go`).
- `click` with a double, triple, right or middle button outside a Page (`internal/helper/actions.go`).
- The `@x,y` forms: `click`, `scroll`, `set-value`, `perform-secondary-action` (`pointer.go`, `scroll.go`).
- `drag` and `hover` outside a Page.
- Batch steps that `headlessStep` sends Headed (`internal/helper/batch.go`).

Rules:

1. Decide the route before anything is sent, as today. When it is Headed and `--headed` was not passed, fail with "nothing was sent", say why there is no background route, and give the exact command with `--headed`. Mention `--post` where Posted Keys would fit.
2. `activate-window` stays as it is: the command itself is the request for the foreground.
3. A Batch without `--headed` refuses a Headed step the same way, before it is sent. Whether the Batch continues past that step follows desktop-parity 20.
4. `--headed` keeps today's behaviour exactly.
5. No change to the Headless, Posted or Page routes.

Docs: CONTEXT.md's Headed Action ("Any other Action is Headed when the agent asks for it" becomes the only way in); SKILL.md's routing rules and the "What winuse cannot do headless" list (every item now needs `--headed`); ADR 0011 gets an "Amended by ADR 0013" note on Decision 4; the ADR 0013 route table; docs/codex-parity.md rows that say keys or clicks "take the foreground".

Sequencing: it touches the same routing code as the click chain (11, 12, 20, desktop-parity 06) and the typing chain (13, 07, 06, desktop-parity 23). Build it before either chain starts.

**Blocked by:** none

**Status:** ready-for-agent (decided by the user 2026-11-03)

- [ ] Inventory of every path that goes Headed without `--headed`, recorded here
- [ ] Each one refuses with "nothing was sent" and the exact `--headed` retry
- [ ] Batch steps refused the same way
- [ ] Offline tests per path, through the Services fakes
- [ ] e2e expectations updated (tests that relied on the automatic Headed route pass `--headed`); Task Suite checked
- [ ] CONTEXT.md, SKILL.md, ADR 0011 note, ADR 0013 table, codex-parity rows

## Comments

### 2026-11-03: triage decision (user)

Asked whether plain `type-text` and `press-key` may keep taking the foreground on their own when no background route exists. The user chose to refuse: the agent must ask for `--headed`. Extended here to every Action that goes Headed on its own, since the rule is the same.
