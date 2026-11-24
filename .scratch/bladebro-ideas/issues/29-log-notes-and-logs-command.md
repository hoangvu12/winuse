# 29: Failed requests and CSP errors: notes and `winuse logs`

Status: ready-for-agent
Type: task
From: tickets 11 and 19

## Ask

- **Turn on `Log.enable`** on every Page session an agent works in, both
  Window Pages and Tab targets.
  - Pages couldn't detect it (ticket 11).
  - It replays entries from before it was enabled, so turning it on late
    still catches the page's earlier failures.
  - It catches failed requests (`source=network`, level error) and CSP and
    security errors only.
  - Never `Runtime.enable`.
- **Notes after Actions.** Keep a ring of the last 50 entries per Tab.
  After a Page Action, when new error entries appeared during it, add
  `Note: 2 failed requests: 404 /api/save, 500 /api/user`, with same-site
  paths and at most 3 entries named. It stays `ok`.
- **`winuse logs <window>[:<tab>] [--clear] [--max-chars N | --out <file>]`:**
  - prints the ring, newest last, with time, source, level and text;
  - `--clear` empties winuse's ring (`Log.clear` also empties Chrome's
    replay store);
  - `--json`.

  Ticket 30 adds exceptions to the same ring.
- **Docs.** Add it to `browser.md` and `commands.md`.
- **Tests.** Fake CDP tests: the replay on enable, the note after an Action,
  the bounds, the ring being per Tab.
