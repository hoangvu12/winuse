# Presence cursor references (cursor-look issue 10)

Crops of Mobbin screens found with poppin (`npx -y github:hoangvu12/poppin`),
2026-11-05. **Mobbin's terms restrict redistribution: these stay in
`.scratch/` only, never in `docs/` or the README.** The contact sheet's
reference strip reads them from here.

The screens are 1920 px wide, about 1.33 screen pixels per logical pixel;
sizes below are in logical pixels and approximate.

| File | App | Screen id (version) | Notable |
|---|---|---|---|
| `01-figma-tag-single.png` | Figma (web), onboarding "How do you plan to use Figma?" | `0845547e-e110-47e3-95b4-564f2c4f9e2e` | Classic Figma tag: a flat, solid rectangle in the collaborator's colour (orange), white medium-weight text about 12–13 px, corners 1–2 px, no border, a soft drop shadow. The arrow is the same colour with a white edge. |
| `02-figma-tags-team.png` | Figma (web), onboarding "Will anyone else be joining you?" | `a20ae8c5-969a-41ed-8a7c-d76d62431dad` | Three people (orange, pink, purple): each tag in the cursor's colour, text white even on the lighter pink; the name only, one line. Tag about 21 px tall; its top-left about 14 px right of and 18 px below the tip, just past the arrow's tail. |
| `03-figma-ui3-tags.png` | Figma (web, 2025 UI), the editor | `40de5b1b-78f6-451b-9637-06b6363a9dda` (2025-07-02) | Figma's current tags ("Francis", "Alex"): still flat and white-on-colour, but the corners are now rounded, about 4 px on a 19 px tag (≈ 20 %). Top-left touching the arrow's tail. |
| `04-figjam-cursor-chat.png` | Figma (web), the FigJam file thumbnail | `632552bc-3fe5-43d3-b827-c794f1dccf77` | FigJam cursor chat: what the person is saying in a big bubble in their colour (fully rounded), and the name in a small square tag (about 10 px text) under it: the activity big, the name small. |
| `05-miro-flowchart-tags.png` | Miro (web) | `bf99e3ff-b185-4f06-a8ec-91625f1d0210` (2023-04-27) | Four collaborators on a board. Fully rounded pills (stadium), about 15 px tall, flat colour, no border or shadow; medium-weight text about 10 px. **Text by contrast**: white on blue, pink and purple, **near-black on yellow** ("Matt"). Top-left about 7 px right of and 12 px below the tip. |
| `06-framer-tag.png` | Framer (web) | `a31f07d9-7131-406c-a5dc-cc9f27da6936` (2026-07-22) | "Sam Lee": violet tag, corners about 20 % of its height, white regular-weight text, no shadow; top-left at the arrow's tail. |
| `07-mural-tags.png` | Mural (web) | `e910e1f0-3792-483c-b79d-2b40b00ed322` (2023-04-13) | "Olivia", "★ Andrea", "Simo…": rounded rectangles about 17 px tall with a 5 px radius (≈ 30 %), white semibold text, a faint light edge; the tag touches the arrow's tail (top-left about 8, 9 px off the tip). The facilitator's role is a star icon inline before the name; a second piece of information stays on the one line. |
| `08-mural-visitor.png` | Mural (web) | `3db5969e-0f11-4170-b642-dfdb48a358a4` (2025-04-02) | "Jane Doe (visitor)": a bright green, fully rounded tag with **near-black text**; the guest status is a suffix on the same line. |
| `09-pitch-tag.png` | Pitch (web) | `3a564529-f02d-4453-ab7c-bd6b7a6e8a02` (2022-12-15) | "Jane Smith": a light blue stadium with **dark text** and a soft drop shadow, beside a same-coloured arrow. |
| `10-canva-tag.png` | Canva (web) | `5e774803-9aa8-4191-b53f-5c8fff7839d1` (2024-12-25) | "Charlie": a cyan tag with **near-black text**, corners about 30 %, a thin white edge and a soft shadow, like the arrow. Elsewhere in Canva (Maya orange: dark text; Beau green, Prasad blue, Taylor purple, Sonja pink: white text) the text colour is plainly chosen per colour for contrast. |

## How they were found

Mobbin records apps with test accounts, so other people's cursors are rare
and free-text search does not find them (`--text multiplayer` returns
GitHub, Framer pricing…; `poppin sections` rejects every query; flows
download every frame under one file name). So every web screen of every
version was downloaded (`poppin app <app> --platform web --all -n 5000
--images --json`) for Figma (1692), Miro, Mural, Pitch (2381 together),
Framer, Webflow, Spline (2317), Canva and Notion (3563), and a scratch
detector flagged a small saturated arrow beside a same-hue, wider-than-tall
filled box with white or dark text in it; the hits were tiled and checked
by eye. Webflow, Spline and Notion showed no collaborator tags; FigJam,
tldraw, Excalidraw, Liveblocks, Whimsical and Google Docs/Slides are not in
the catalogue (`poppin find`).

## What they say for the Agent Cursor's label

- **Flat, solid fill in the cursor's own colour** in all seven apps: no
  gradient, no glow. A soft drop shadow in Figma (classic), Pitch and
  Canva; none in Miro, Mural, Framer. A thin white edge only in Canva and
  Mural.
- **Text colour by contrast**: white on saturated colours, near-black on
  light ones (Miro's yellow, Mural's bright green, Pitch's light blue,
  Canva's cyan and orange). Only classic Figma forces white.
- **Corners**: Miro and Pitch fully rounded; Mural, Canva, Framer and
  Figma's current UI about 20–30 % of the tag's height; only classic Figma
  nearly square.
- **Placement**: below and right of the tip, the tag's top-left at or just
  past the arrow's tail.
- **Text**: medium or semibold, 10–13 px, one line. A second piece of
  information is inline (Mural's ★ and "(visitor)"), or, for an activity,
  FigJam puts it big with the name small.
