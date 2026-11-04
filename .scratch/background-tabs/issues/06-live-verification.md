# 06: Live verification in the user's own browser

Status: ready-for-human

Everything above is verified offline only (bridgetest, no browser). The
installed winuse and extension were not touched; this needs the new
build installed and the extension reloaded by it, which the user does.

Checks (exact commands in the final report of the background-tabs work):

1. A screenshot of a background Tab with focus emulation on: does
   `Page.captureScreenshot` return a real frame on Windows within 3 s, or
   does the Note fallback show?
2. A `<select>` in a background Tab: the click is refused, `set-value`
   picks the option, and no popup appears on the screen.
3. A `target=_blank` link clicked in a background Tab: the new Tab opens
   beside the user's Tab, the user's Tab stays selected, the new Tab is in
   the agent's group, the browser window does not come to the front, and
   the Note names the Tab.
4. Two agents (`WINUSE_AGENT=a` and `=b`) each working in their own
   background Tab while the user scrolls their own Tab: both finish, the
   user's Tab never changes, nothing flickers.

Also watch: a Tab left alone for over 5 minutes is released (the badge
count drops when the extension detaches), and Energy Saver's freeze of a
hidden Tab is undone by the next command.

## Comments
