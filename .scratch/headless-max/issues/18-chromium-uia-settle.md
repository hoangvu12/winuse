# 18: Longer settle for a cold Chromium UIA tree

**What to build:** when a Chromium Window has no Browser Connection (the user's own browser without the extension, Electron, WebView2), wait longer for its UIA tree to build, within the request budget, and say what happened.

ADR 0005 retries once after 500 ms. The Laptop probe measured that a cold Chromium can take 10-25 s, and a fully covered cold Chrome never built its web tree (36 nodes for 14 s), while one that was partly visible settled in about 0.1 s. Ticket 04 fixes browsers winuse launches; this ticket covers the rest.

- Poll the document subtree size every 250-500 ms until it grows and then stops growing, up to a settle budget (proposal 5 s, or the caller's `--timeout` from desktop-parity 14).
- If it never settles: return what is there with a note: "the page's accessibility tree is still thin; Chromium may not build it while the Window is fully covered. Connect the browser (winuse browser ...) or uncover the Window."
- Never activate or un-cover the Window to make it build.

**Blocked by:** none

**Status:** ready-for-agent (amends ADR 0005)

- [ ] Settle loop with growth detection
- [ ] The note
- [ ] Offline tests with a faked growing tree
- [ ] Opt-in e2e: a covered Electron App read cold
- [ ] ADR 0005 amendment
