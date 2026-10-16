# 14 — Chromium / Electron apps

**What to build:** Window States for Chrome, Edge and Electron apps include their web content. Because Chromium enables its accessibility tree lazily when a UI Automation client appears, a near-empty first read is retried after a short delay; if the tree is still sparse, the Window State says so, so the agent knows to request a screenshot. Tested against a local HTML page opened in Edge. See spec: Observation details.

**Blocked by:** 03 — Window State text tree

**Status:** ready-for-agent

- [ ] Web content Elements (links, buttons, inputs) appear in the Window State of Chrome and Edge
- [ ] A near-empty first read is retried once after a short delay
- [ ] A persistently sparse tree produces a note in the Window State
- [ ] Offline tests cover sparse-tree detection on recorded Chromium/Electron trees
- [ ] End-to-end: the local HTML fixture page in Edge exposes its Elements (verified by user run)
