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

## Comments

- 2026-11-24, implementation: `Network.setBlockedURLs` only takes effect with the Network domain on in the session. `TestAssetBlocking` (headless) showed the blocking Tab still fetching every image, the font and the video without `Network.enable`, and ticket 10 measured with it on. So `page.BlockAssets` sends `Network.enable` before the patterns, and `--block none` empties the list and sends `Network.disable` (and sends nothing on a Tab that wasn't blocking). That is the only place winuse turns Network on, and only in a blocking Tab that is winuse's. Ticket 19's "never `Network.enable`" rule is about capturing on the user's Tabs, where `--block` is refused.
- 2026-11-24, review: ticket 19 (line 38) records that `Network.enable` has a detectable timing cost, so a Tab with `--block` on is detectable by the site it shows. Accepted: `--block` is opt-in, and only on agent-owned Tabs (it is refused on the user's), so the user's own Tabs never carry that signal. map.md, `page/logs.go`, `helper/logs.go`, `page/httpstatus.go` and `browser.md` now say "never, except on a Tab where `--block` is on".
