---
title: Change the download setting and save a download in a specific folder
category: chromium
tags: settings, address-bar, native-dialog, download
apps: msedge
http: yes
---

Start Edge with an isolated profile by running this command:

    "{{.EdgePath}}" --user-data-dir="{{.ScratchDir}}\edge-profile" --no-first-run --no-default-browser-check --window-size=1200,900 about:blank

If a welcome or sign-in page appears, dismiss it; no sign-in is needed.

Then:

1. In the address bar, go to edge://settings/downloads and turn on "Ask
   where to save each file before downloading".
2. Open {{.BaseURL}}/download.html and download the report file linked
   there. In the save dialog, save it as
   "{{.ScratchDir}}\report.bin".

Close the browser when you are done.
