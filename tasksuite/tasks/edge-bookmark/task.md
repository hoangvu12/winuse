---
title: Open two pages in tabs and bookmark one in Edge
category: chromium
tags: tabs, address-bar, bookmark
apps: msedge
http: yes
---

Start Edge with an isolated profile by running this command:

    "{{.EdgePath}}" --user-data-dir="{{.ScratchDir}}\edge-profile" --no-first-run --no-default-browser-check --window-size=1200,900 "{{.BaseURL}}/page-a.html"

If a welcome or sign-in page appears, dismiss it; no sign-in is needed.

Then:

1. In a new tab, open {{.BaseURL}}/page-b.html (type the address in the
   address bar).
2. Bookmark the page in that tab, naming the bookmark "Field guide".

Close the browser when you are done (closing flushes the profile's
bookmarks).
