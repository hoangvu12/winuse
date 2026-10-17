---
title: Fill and submit a web form in Edge
category: chromium
tags: web-form, select, radio, checkbox, submit
apps: msedge
text: yes
http: yes
---

Start Edge with an isolated profile by running this command:

    "{{.EdgePath}}" --user-data-dir="{{.ScratchDir}}\edge-profile" --no-first-run --no-default-browser-check --window-size=1200,900 "{{.BaseURL}}/form.html"

If a welcome or sign-in page appears, dismiss or navigate past it; no
sign-in is needed. The browser should show a "Team registration" form.

Fill the form with exactly these values:

- Full name: Marina Ionescu
- Email: marina.ionescu@example.org
- Team: Cascade
- Role: Operations
- Send me the newsletter: checked
- Notes: I can bring a spare keyboard to the meetup.

Then click Register. The page should confirm your registration. Close
the browser when you are done.
