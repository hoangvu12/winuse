# 07 — Set value and Secondary Actions

**What to build:** `winuse set-value <window> <element> <value>` sets an Element's value directly; `winuse perform-secondary-action <window> <element> <action>` performs one of the Element's Secondary Actions (Expand, Collapse, Toggle, Select, Scroll Down, …). The Window State now lists each Element's Secondary Actions and state (toggled, selected, expanded, value), following Codex's format. See spec: Observation details, Acting details.

**Blocked by:** 05 — Click by Element Index

**Status:** ready-for-agent

- [ ] Window State lines show Secondary Actions and Element state in Codex's format
- [ ] `set-value` works on settable Elements; non-settable Elements fail with a hint to use `type-text`
- [ ] Unsupported Secondary Actions fail listing the ones the Element offers
- [ ] Offline tests cover rendering of Secondary Actions and state from recorded trees
- [ ] End-to-end: set the fixture's text field, toggle its checkbox, expand its menu (verified by user run)
