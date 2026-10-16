# 02 — Fixture app + opt-in end-to-end tests

**What to build:** Seam 1 of the spec. A purpose-built fixture app whose Elements are known in advance (buttons, text field, checkbox, list, menu, a "Save As"-style dialog that opens as a new Window, a hover-only toolbar, a canvas), plus an end-to-end test runner that drives the real `winuse` binary against it and asserts on printed output and the fixture's end state. The runner is opt-in only: it never runs as part of the default test command and starts only when the user explicitly enables it, because it takes over the screen. First test: `list-windows` shows the fixture's Window. See spec: Testing Decisions.

**Blocked by:** 01 — Background helper + list windows

**Status:** ready-for-agent

- [ ] Fixture app builds and exposes the listed Elements with stable names and AutomationIds
- [ ] Fixture can report its end state (text field contents, checkbox state, last button pressed, canvas clicks) for assertions
- [ ] End-to-end tests are skipped unless the user explicitly opts in (e.g. an environment variable)
- [ ] Agents must not enable the opt-in; this rule is stated in the test runner's documentation
- [ ] First end-to-end test: `list-windows` lists the fixture Window (verified by user run)
