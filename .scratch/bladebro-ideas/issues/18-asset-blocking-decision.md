# 18: Asset blocking: build it, and how

Status: resolved
Type: grilling
From: `map.md`

## Decisions needed

- **Go or no-go,** from ticket 10's numbers.
- **Where it's allowed:** only Tab targets the agent opened and browsers
  winuse launched, never the user's own Tab.
- **Surface:** a flag on `tab new` or `navigate`, or a per-Tab setting that
  persists.
- **Classes:** images, fonts, media and third-party trackers.
- **Disclosure:** how blocked assets show up in reads and screenshots.
- **Method:** `Fetch` or `Network.setBlockedURLs`.

## Answer

Grilled 2026-11-24 (round 4).

1. **Build it, opt-in, on agent-owned Tabs only:** Tabs from `tab new` and
   browsers winuse launched or cloned. Refused on the user's own Tabs,
   with nothing sent.
2. **Surface.** `tab new <w> [url] --block images,fonts,media`, or
   `navigate <w>:<tab> <url> --block ...` on an agent-owned Tab.
   - It lasts for the Tab's life; `--block none` turns it off.
   - Method: `Network.setBlockedURLs` with `urlPatterns` (ticket 10). It
     pauses nothing, so nothing can hang.
   - Tracker blocking is not in v1.
3. **Disclosure.** Reads and screenshots of a blocking Tab end with
   `Note: images, fonts and media are blocked in this Tab`.

Build ticket: 28.
