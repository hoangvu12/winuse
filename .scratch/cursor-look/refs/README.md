# Presence cursor references (cursor-look issue 10)

Crops of Mobbin screens found with poppin (`npx -y github:hoangvu12/poppin`),
2026-11-05. **Mobbin's terms restrict redistribution: these stay in
`.scratch/` only, never in `docs/` or the README.** The contact sheet's
reference strip reads them from here.

Measurements are in logical pixels (the screens are 2× captures, 1920 wide).

| File | App | Screen id | Notable |
|---|---|---|---|
| `01-figma-tag-single.png` | Figma (web), onboarding "How do you plan to use Figma?" | `0845547e-e110-47e3-95b4-564f2c4f9e2e` | The classic Figma multiplayer tag: a flat, solid rectangle in the collaborator's colour (#E44D26-ish orange), white medium-weight text about 12–13 px, no border, a soft drop shadow below. The cursor is the same colour with a white edge. |
| `02-figma-tags-team.png` | Figma (web), onboarding "Will anyone else be joining you?" | `a20ae8c5-969a-41ed-8a7c-d76d62431dad` | Three people at once (orange, pink, purple): each tag is the cursor's colour, text always white, even on the lighter pink. One line, the name only. |
| `03-figma-tag-zoom.png` | same screen, zoomed | `a20ae8c5-969a-41ed-8a7c-d76d62431dad` | Measured: tag about 21 px tall, corner radius 1–2 px (almost square), about 6 px side padding; its top-left sits about 14 px right of and 18 px below the tip, just past the arrow's tail. |
| `04-figjam-cursor-chat.png` | Figma (web), the FigJam file thumbnail ("Welcome to FigJam") | `632552bc-3fe5-43d3-b827-c794f1dccf77` | FigJam's cursor chat: what the person is saying in a large bubble in their colour (fully rounded, about 12 px radius), white text, and their name in a small square tag (about 10 px text) under it — the activity big, the name small. |
| `05-figma-basics-tags.png` | Figma (web), "Figma basics" file thumbnail | `6171e838-0f89-4ef4-993a-09b016838fb5` | Figma's illustrated presence: cursors with flat colour tags, square corners, a dark outline in this illustration style; tag down-right of the tip. |
| `06-figma-ios-cursors.png` | Figma (iOS), sign-in illustration | `1fa0c56b-e06f-4cfe-a996-67ee8d10aa6d` | Six cursors in six mid-saturation colours (green, blue, purple, magenta, red): the palette idea, one flat colour per person. No tags here. |

## Looked at, nothing usable

Mobbin records each app with one test account, so live multiplayer is rare.
Checked with `poppin app <app> --images` (newest version, web unless noted)
and pattern/text searches: Miro (incl. `--pattern Canvas`: boards without
other people), Mural, Pitch, Canva, Framer, Spline, Jitter (an onboarding
"eyes" bubble, not a tag), Rive, Webflow, FLORA, ClickUp (a "Click for audio"
video hint, not presence), Notion, Gamma (Mobbin's "Gamma" is an NFT site),
Freeform (iOS), Figma (iOS). FigJam, tldraw, Excalidraw, Liveblocks, Google
Docs/Slides and Lucid are not in the catalogue (`poppin find`).
`poppin sections` (marketing heroes) rejected every query and `poppin similar`
returned a server error, so marketing pages with multiplayer heroes could not
be searched.

## What it says for the Agent Cursor's pill

- Flat, solid fill in the cursor's own colour; no gradient, no glow, no rim.
- One text colour on it (Figma always white on its saturated colours; our
  pastels need near-black for contrast, so pick per colour).
- Nearly square corners on name tags (1–2 px; FigJam's chat bubble is the
  rounded exception); a soft shadow at most.
- Placed just past the arrow's tail, down-right of the tip.
- With an activity, FigJam shows it big and the name small; with a name
  only, Figma shows the name.
