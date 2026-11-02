# 04: `find` and a region read

**What to build:**

- `find <window> [--role <r>] [--name <text>] [--text <text>] [--state <s>] [--first | --count]`: prints the matching lines in the ADR 0004 grammar, with Element Indexes the agent can act on. On a role miss, list the roles present (agent-desktop's `roles_present`).
- `get-window-state <window> --root <element> [--depth N] [--interactive]`: reads one subtree.

agent-desktop: `find` with filters and shaping; `--skeleton` (depth 3 with child counts) and `--root REF` drill-down (`ad:docs/skeleton-and-drill-down.mdx:7-153`). winuse reads the whole tree every time, bounded by caps and a truncation note (`internal/uia/walk.go:101-103`).

Numbering decision (user, 2026-11-03, option c): `find` and `--root` **add** their Elements to the Window's live store with new Element Indexes after the existing ones (a full read ends at 240, `find` prints 241, 242, ...). The full read's indexes keep working. A full `get-window-state` still renumbers from 1 and replaces the store. Staleness checks are unchanged: an index whose Element changed still gets the stale message. No snapshot-qualified refs. **Revisit** if agents are seen failing on stale indexes: then consider agent-desktop's snapshot-qualified refs with re-identification (see Comments).

- `find` searches the live tree with a bounded walk, not only the last printed state, so it reaches Elements past the truncation caps.
- `--skeleton` (depth 3, `(+N children)` markers) is part of this ticket.
- Pages: route `find` to the CDP snapshot with the same filters.

**Blocked by:** none

**Status:** ready-for-agent (ADR 0004 amendment for the added numbering)

- [ ] Filters and output in CLI, protocol, helper
- [ ] Region read and skeleton in the walk
- [ ] `find` and `--root` add to the live store after the last index; a full read replaces it; stale messages unchanged
- [ ] Offline tests over recorded trees (`internal/wstate/testdata`)
- [ ] ADR 0004 amendment; SKILL.md "dense Windows"

## Comments

### 2026-11-03: triage decision (user)

Search options like agent-desktop's `find` (role, name, text, state; `--first`, `--count`; `roles_present` on a role miss; `--skeleton`, `--root`). Numbering: winuse's Element Index stays, with option (c): `find` and region reads add indexes after the existing ones instead of replacing the store. Options considered: (a) fresh numbering that replaces the store; (b) agent-desktop's saved, snapshot-qualified refs with re-identification. (b) was not chosen: it replaces the Codex `element_index` model and a re-identification can pick the wrong Element. **The user asked to revisit this if agents are seen failing because their indexes went stale.**
