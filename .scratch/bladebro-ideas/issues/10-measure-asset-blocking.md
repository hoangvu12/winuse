# 10: Measure asset blocking

Status: resolved
Type: prototype
From: `map.md`; bladebro `bb/src/page/intercept.rs` (CDP `Fetch` domain; its rules: never block first-party JS or XHR, or bot-detection vendors; resource types are always safe)

## Question

Would an opt-in "block images, fonts and media" on an agent-owned Tab make
pages load meaningfully faster, and can winuse do it safely?

- **Speed.** Measure 6 heavy sites, each with no blocking, with `Fetch`
  limited to resource types (Image, Font, Media), and with
  `Network.setBlockedURLs`. Report medians of DOMContentLoaded, load and
  bytes, plus AX node counts.
- **Safety.** When the driver disconnects or answers slowly, do paused
  requests hang? Do HTML, scripts and XHR never pause under patterns limited
  by type?
- **Extension route.** Does `chrome.debugger` allow `Fetch.enable` and its
  `requestPaused` events, and does winuse's extension forward them?

The experiment runs in a headless Chrome with a temp profile, under the
chat's scratch folder `exp\assets\`.

## Answer

**Setup.** Chrome 155, `--headless=new`, 60 public loads: 6 sites × 3 modes
× 3 cold runs, each in a fresh context, plus local tests. The scripts (`bench.mjs`, `safety.mjs`, `exttest.mjs`) are
copied to
`../prototypes/assets/`, and the raw `results.jsonl` stayed in the chat's
scratch folder `exp\assets\`.

**Speed: a moderate gain.**
- With the type-limited Fetch method, the median load event was 5–33%
  sooner (64–689 ms), and bytes at load dropped 17–73%.
- The URL-pattern method did about as well on most sites. It misses images
  with no file extension: 14 of 30 on Wikipedia, 13 of 15 on YouTube.

| Site | load ms: none → Fetch → URL patterns | KB: none → Fetch |
|---|---|---|
| Guardian | 671 → 494 → 441 | 1098 → 543 |
| IKEA listing | 1409 → 1345 → 1382 | 1593 → 1321 |
| Recipe blog | 2322 → 1633 → 2904 (noise) | 3195 → 2156 |
| YouTube home | 1525 → 1192 → 1080 | 6624 → 5755 |
| Wikipedia "Bird" | 676 → 592 → 664 | 749 → 595 |
| MDN | 1030 → 694 → 705 | 427 → 116 |

**Reading:** no gain. AX node counts and text length are unchanged within 1%,
because blocked images keep their alt text. This was measured on a fast
connection; slow links should gain more (inference).

**Safety (tested)**
- Type-limited patterns paused only Image, Font and Media. HTML, CSS, script,
  fetch and XHR were never paused.
- A slow driver (2 s per answer) leaves DOMContentLoaded alone, but adds the
  whole delay onto the load event.
- Closing the websocket, detaching, or `Fetch.disable` releases every paused
  request.
- **A driver that stays connected and never answers leaves images hung for
  more than 60 s**, and Chrome doesn't time them out.

**Extension route**
- `chrome.debugger` allows Fetch and Network (docs). On Chromium 159, Fetch
  pauses and `Network.setBlockedURLs` with `urlPatterns` both worked through
  a test extension. Branded Chrome 155 ignores `--load-extension`, so this
  is untested there.
- winuse's `background.js:562-574` forwards every event without filtering.
- **The catch:** when a winuse connection closes, the extension only
  forgets that connection; the tab stays attached until the 5-minute idle
  detach. So if the helper dies, images in that tab could hang for up to 5
  minutes.

**Recommendation**
- Worth building as an opt-in.
- Default to `Network.setBlockedURLs` with `urlPatterns`: there's nothing to
  answer, so nothing can hang, it works on both routes, and it ends on
  detach.
- Offer type-limited Fetch only on browsers winuse launched, where a dropped
  connection releases the requests.
- On the extension route, Fetch would need the extension itself to answer
  the pauses and to detach when the last connection closes.
