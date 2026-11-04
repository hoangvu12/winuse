# README demo: three Agents at once

`record-demo.ps1` records the README's hero GIF: three named Agents working
at the same time, each in its own browser window, each with its own Agent
Cursor and name tag.

| Agent | Colour | Window | What it does | Actions |
|---|---|---|---|---|
| artist | pink #C73782 | Pixel studio | colours a rocket: picks a colour, bucket-fills a part, five colours, the sky last | 12 clicks |
| planner | violet #6B58CE | Launch board | sets the owner, ticks five tasks (each slides into Done), ships v2.0 (confetti) | 1 set-value, 6 clicks |
| writer | green #1F853E | Release notes | types the title and three highlights, sets the channel, publishes (a stamp) | 4 type-text, 1 set-value, 1 click |

The pages (`site/`) are local files opened in chromeless Chrome `--app`
windows on winuse's own launch profile, over a dark backdrop window that
fills the recorded rectangle, so nothing of the user's desktop shows. Every
Action is a Headless Page Action: no real pointer, no foreground. Page
Actions in different browser windows take different lanes (ADR 0014), so
the three cursors move together. Expected length: about 15–18 s of Agents
plus a 3 s hold, so a 17–22 s GIF.

Run it (it builds this branch, since the installed winuse needs
`WINUSE_CURSOR_CAPTURE`):

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .scratch\readme-demo\record-demo.ps1
```

Outputs: `docs/images/winuse-demo.gif` and `.scratch/readme-demo/out/winuse-demo.mp4`
(the raw recording stays in `%TEMP%\winuse-demo\raw.mkv`). `-SelfTest` runs
only the encode on a synthetic clip; `-Grabber gdigrab` records with gdigrab
instead of ddagrab; `-Exe <path>` uses a built winuse.

While it runs, nothing else may use winuse: a CLI of another build (the
user's installed winuse, the Brave extension's native host) replaces the
demo helper, and the script then fails, since the cursors may be missing.

`readme-hero.patch` is the README change that went in with the GIF, with
`docs/images/winuse-calculator.gif` removed (the first recording, 2026-11-05:
the Agents took 22.3 s, the GIF is 25 s, 960x520, 1.4 MB at 15 fps).

`#done` on a page's URL previews its finished state; the script never uses
it.
