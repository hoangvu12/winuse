# 08: Polite real input

**What to build:** Headed Actions (in the desktop lane) disturb the user as little as possible, following cua's and Raymond Chen's practice.

- **Wait while the user is typing:** GetLastInputInfo idle under about 500 ms delays the start, up to a limit, then "nothing was sent; the user is typing".
- **Save and restore:** remember the foreground Window and the cursor position before the Action. Afterwards, restore the cursor only if it is within 2 px of where winuse left it, and give the foreground back to the user's Window when winuse took it.
- **Confirm the target:** after activating, confirm the target is exactly the foreground for up to 500 ms (a WM_NULL round trip), else "nothing was sent".
- **One SendInput batch per step**, then a drain sentinel before restoring.
- **Yield to the user:** the low-level hooks tell real input from injected input (LLMHF_INJECTED, LLKHF_INJECTED). Real user mouse or key input during a Headed Action stops it, like Esc.
- **Release held keys and buttons on every exit path** (a ledger of what winuse pressed).
- Chromium re-activates itself after a restore: re-check the foreground for a short while and give it back unless the user changed it.
- Check integrity through TokenIntegrityLevel, never through SendInput's return value (as today).

**Blocked by:** 06

**Status:** ready-for-agent

- [ ] Typing wait; save and restore; target confirmation
- [ ] Injected-vs-real detection and yield; key ledger
- [ ] Offline tests over faked input state and timings
- [ ] Opt-in e2e: a Headed click on a fixture puts the cover back in front and the cursor back where it was
- [ ] SKILL.md Headed section
