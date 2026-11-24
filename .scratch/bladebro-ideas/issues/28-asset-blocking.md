# 28: Asset blocking on agent-owned Tabs

Status: ready-for-agent
Type: task
From: tickets 10 and 18

## Ask

- **Where it's allowed.** `--block images,fonts,media|none` on
  `tab new <w> [url]`, and on `navigate <w>:<tab> <url>` for a Tab the agent
  opened. It also applies on any Tab of a browser winuse launched or
  cloned.
  - On the user's own Tab it's refused, with nothing sent, and the hint says
    to open a Tab with `tab new`.
- **Method.** `Network.setBlockedURLs` with `urlPatterns` (URLPattern
  syntax, e.g. `*://*:*/*.png`), the form Chrome 155 supports. Ticket 10's
  findings:
  - It pauses nothing, so nothing can hang.
  - It works through the extension.
  - It ends when the session detaches.
  - It misses images whose URLs have no file extension. Say so in the help.
- **Patterns.**
  - images: png, jpg, jpeg, gif, webp, avif, svg, ico;
  - fonts: woff, woff2, ttf, otf;
  - media: mp4, webm, mp3, ogg, m4a.

  It lasts for the Tab's life, and is re-applied when the helper reattaches.
- **Disclosure.** Reads, `--observe` output and screenshots of a blocking Tab
  end with `Note: images, fonts and media are blocked in this Tab`, naming
  the classes that are on.
- **No tracker blocking in v1.**
- **Docs.** Add it to `tabs.md` and `browser.md`.
- **Tests.** Fake CDP tests for the patterns and the refusal. An e2e test on
  a fixture page with images and fonts, checking that the load event
  arrives sooner and that the note shows.
