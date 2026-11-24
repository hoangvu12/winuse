# 02: HTTP status on navigate

Status: resolved
Type: task
From: `map.md`; bladebro `bb/src/page/navigate.rs:150-181,485-488`, `bb/src/page/logs.rs:17-134`

## Problem

`page.Navigate` (`internal/page/nav.go:105-129`) reports only `errorText`
(a network failure) and downloads. A page that answers 404, 429 or 503 with
a body counts as a successful navigation, so the agent reads an error page
as if it were the content, and may not notice for several turns.

bladebro reports, for example:

```
HTTP 429 Too Many Requests (retry-after: 45s)
```

It does this both when the error page is the site's own and when Chrome
shows its own error page for an empty body.

## Ask

- After `navigate`, `back`, `forward` and `reload` land, read the main
  document's status. When it is 4xx or 5xx, add
  `Note: the Page answered HTTP <code> <reason>` and, if present,
  `(Retry-After: <s>)`.
  - It stays `ok`: the navigation was delivered and did land.
  - Under `--json`, add a field.
- **How to read it** (settled by ticket 11, tested on Chrome 155):
  - Read `performance.getEntriesByType('navigation')[0].responseStatus` in an
    isolated world once the navigation lands. No domain is needed, and it
    stays correct through redirect chains.
  - Take the reason phrase from a fixed table of status codes. Don't print
    Retry-After by default, because the only way to get headers is
    `Network.enable`, which costs a detectable timing signal (300 local
    fetches were about 20% slower) and buffers response bodies.
    Retry-After would come only with a future explicit option.
  - **Empty-body 4xx/5xx.** Today these fail as a `NavigateError` with
    `net::ERR_HTTP_RESPONSE_CODE_FAILURE`, and the frame is
    `chrome-error://chromewebdata/` with `unreachableUrl` set. Read
    `responseStatus` there too, since it still gives the real code, and turn
    the failure into the same note, e.g. `Note: the Page answered HTTP 429
    Too Many Requests with an empty body; Chrome shows its error page`.
    Keep the Delivery honest: the navigation was delivered and landed on an
    error page.
- Show the note on a Tab target too.
- Tests: a fake CDP test for each status, and an e2e page that serves 404
  and 429.

## Answer

Built on branch `agent/bladebro-ideas` (merge 98ce7df; review fixes in 081a150). `navigate`/`back`/`forward`/`reload` read `responseStatus` in an isolated world and add `Note: the Page answered HTTP <code> <reason>`; empty-body 4xx/5xx prints `ok` with the error page noted. `--json` has `http_status` (a real Response field after the review).
