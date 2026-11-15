# ADR 0022: Wait briefly before refusing a target that is not ready yet

## Status

Accepted (learn-from 02, `.scratch/learn-from/issues/02-wait-before-refusing.md`,
2026-11-15), from rank 2 of `.scratch/learn-from/README.md`
(agent-desktop §2.2, agent-browser §4.1). It changes when an Action
refuses, not what a refusal says about Delivery. The live check is still
to run.

## Context

An Action whose target is disabled, or whose `name:`/`id:` reference
matches nothing yet, was refused at once with "nothing was sent". The
usual cause is timing: the agent acts on the button the step before it
enables, or on a dialog's Element that is still opening. The agent then
spends two more turns, `wait <window> <ref> enabled` and the retry, and a
model turn costs 3 to 10 s. agent-desktop waits up to 5 s for every ref
action to resolve and become actionable (`--timeout-ms`, 0 tries once), and
its guide says that is why an agent "rarely need[s] a `wait`".
agent-browser (ab#2587) waits for a Page click point to stop moving after
the scroll and re-centres an Element still outside the viewport once.

winuse's constraints: an Action's budget is 10 s (`protocol.RequestTimeout`);
a fresh read of a reference takes up to 3 s of it (`liveRefBudget`); the
Action runs in its Lanes, and another Agent waits up to ~12 s for a Lane
before it is refused (ADR 0014); an Element Index names one Element of one
Window State and never re-resolves (ADR 0004, ADR 0021); the Esc stop ends
whatever winuse is doing (ADR 0007).

## Decision

1. **Transient refusals are tried again for up to 2 s** (`targetWait`,
   every 200 ms), then refused as before with `, still after waiting 2s`
   before `; nothing was sent`. Transient means:
   - a `name:`/`id:` reference that matches no Element in the cached Window
     State or a fresh read (the fresh read runs again; only where a fresh
     read is possible);
   - a native Element whose pattern call UI Automation refused as disabled
     (`uia.ErrDisabled` with nothing delivered: the same call again);
   - a Page Element refused as disabled by its check or its aim (both run
     again; neither sends input).

   Everything else refuses at once: an ambiguous reference (never a
   guess), a stale Element Index, a hidden Page Element, a missing pattern
   or Secondary Action, a write that may have taken effect.
2. **2 s, not agent-desktop's 5 s.** It fits the 10 s budget with the
   first fresh read (3 s), a last one, the Agent Cursor's glide and the
   Action itself; it adds at most 2 s to a waiting Agent's ~12 s Lane wait,
   so that Agent is not refused because of it. Controls that enable after
   a click or keystroke, and dialog Elements that appear, mostly do so
   well within it. Longer waits stay explicit: `wait <window> <ref>
   enabled`, `wait window`, up to 300 s. The wait also stops 3 s before the
   request's deadline, so a Batch step near the end of its budget refuses
   instead of timing out.
3. **Inside the Lanes, not before them.** agent-desktop polls before it
   takes its lease. winuse's Lanes are taken by the registry before the
   command runs, and the reference resolves inside the command; moving the
   poll ahead of the Lanes would split every Action in two. With a 2 s
   bound the cost to another Agent is small and bounded.
4. **Esc ends the wait** at the next pause: exit 130, nothing was sent.
   A try runs on the command's goroutine and is never abandoned, because a
   try can act.
5. **An Element Index never re-resolves.** A disabled one is tried again on
   the same Element; a stale one fails at once.
6. **The Page click point settles and re-centres** (`page.Aim`, any Action
   that scrolls its Element into view): after `DOM.scrollIntoViewIfNeeded`
   it waits for the box to match across two animation frames (100 ms when
   frames do not come, five rounds, 700 ms at most, at once in a hidden
   document); an Element still outside the viewport or covered at its
   centre is scrolled to the centre once (`scrollIntoView({block:
   'center', inline: 'center', behavior: 'instant'})`, a cross-origin
   iframe's element too), settled and measured again before the refusal.
   Covered is not polled for 2 s: a cookie banner does not leave by itself,
   and the re-centre already handles a sticky bar.
7. **No opt-out flag.** The wait runs only when the Action would otherwise
   refuse, so it never delays an Action that succeeds, and it is short.
   `Services.TargetWait` (below 0: refuse at once) is the seam for tests.

## Consequences

- A typical "act on what the last step enabled" or "act on the dialog that
  is opening" is one call instead of three.
- A target that never becomes ready costs 2 s more before the refusal,
  plus the repeated fresh reads of a reference (each bounded as before).
- The Agent Cursor glides to a disabled native Element once and stays there
  while the Action waits; it ripples only on delivery.
- A Page Action that scrolls pays one more round trip and about two frames
  for the settle.
- A Page `name:`/`id:` reference is not waited on: it resolves against the
  stored Page, and a fresh UI Automation read does not read the Page.

## Considered

- **Poll before refusing for hidden or covered Page Elements too**
  (agent-desktop's "visible" and "receives events"). Left out: hidden
  usually means a closed menu or section that needs an Action, and covered
  usually a banner that needs one; both would only add 2 s.
- **A `--no-wait` flag** (the research's suggestion). Left out (Decision 7);
  easy to add on the request, like `--observe`, if an agent needs it.
- **5 s like agent-desktop.** Too much of a 10 s budget once a fresh read
  and the Action are counted.
