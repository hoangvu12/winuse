# Agent Cursor look & motion: cua vs winuse

Status: research (no product change). Date 2026-11-04.

Sources: trycua/cua at `ecaf10b` (2026-11-04), cloned. Paths below are relative to that repo unless they start with `internal/` (winuse).
Tags: **[source]** read in code, **[doc]** cua docs, **[video]** frames of their GIF, **[inference]** worked out by me.

Files next to this report:
- `cua-default-arrow.svg`: cua's default arrow, rebuilt from its generator (outline, 8 glow strokes, halo, white rim; hotspot marked red). Open it in a browser.
- `cua-action-animations.gif`: cua's docs GIF of the 12 action states (`docs/public/img/cua-driver/cursor-themes/action-animations.gif`).
- `cua-presence-cursors.png`: cua Spaces' remote-presence cursor set. This is a different product surface, included for colour reference only.

---

## 1. How cua does it

### 1.1 Where the cursor lives

- **One shared Rust stack on every OS.** [source]
  - The motion maths is in `libs/cua-driver/rust/crates/cua-cursor-motion` (MIT, no dependencies). A TS port in `libs/typescript/cursor-motion` is tested against the same golden trajectories.
  - Painting and state are in `libs/cua-driver/rust/crates/cursor-overlay`: tiny-skia rasterising, the theme, the float, the halo and the badge.
  - The per-OS window plumbing is in `platform-windows/src/overlay.rs`, `platform-macos/src/cursor/overlay.rs` and `platform-linux/src/{overlay,wayland/overlay}.rs`.
  - The design was done first in a browser "motion lab" (`libs/cua-driver/tools/cursor-gallery/motion-lab/`). The Rust code is checked against it to within 0.5 pt (`cua-cursor-motion/README.md`, `tests/motion_parity.rs`).
- **Windows overlay** (`platform-windows/src/overlay.rs:1-10, 562-563, 1068-1071`) [source]:
  - **Window:** one `WS_EX_TRANSPARENT|WS_EX_LAYERED|WS_EX_NOACTIVATE|WS_EX_TOOLWINDOW` window that spans the whole virtual screen. Every session's cursor is composited into it, in insertion (z) order.
  - **Pixels:** tiny-skia draws into an RGBA pixmap, which is swizzled to BGRA and pushed with `UpdateLayeredWindowIndirect(prcDirty)`. Only the union of last frame's and this frame's cursor bounds is repainted. They report the earlier full-surface version cost about 52 ms per frame (`overlay.rs:676-688`).
  - **Timer:** `SetTimer` at 8 ms (about 125 Hz) while animating, 80 ms while sampling hover, and 250 ms when idle. `timeBeginPeriod(1)` is held only while frames are running (`overlay.rs:642-670`).
  - **Z-order:** every 80 ms the overlay is placed one slot above the pinned target: `HWND_NOTOPMOST` first, then insert after `GetWindow(target, GW_HWNDPREV)`, or `HWND_TOP` when there is no window above. With no target it does a quick TOPMOST→NOTOPMOST hop (`overlay.rs:1420-1495`). winuse's `cursor_zorder.go` already follows this.
  - **Capture exclusion:** `WDA_EXCLUDEFROMCAPTURE` is set **only for the length of cua's own desktop capture**, followed by a `DwmFlush`. The comment explains why: "a permanent affinity would also hide the cursor from screen recorders and from the Driver's own recordings, which must keep showing it" (`overlay.rs:1504-1575`).
  - **DPI:** on Windows the paint still uses `backing_scale = 1.0`, marked `TODO: thread GetDpiForWindow / per-monitor DPI` (`overlay.rs:1295`). **winuse is ahead here.**

### 1.2 Look (default theme `cua.default`)

Generator: `cursor-overlay/assets/build_default_theme.py`. Constants: `cursor-overlay/src/theme.rs`, `cursor_pose.rs`. Prose: `libs/cua-driver/docs/cursor-themes.md`.

| Property | Value |
|---|---|
| Canvas / size | Drawn on a 128×128 canvas and painted as a **42 pt** footprint (`theme.rs:18-19` `CANVAS_SIZE=128`, `DISPLAY_SIZE=42`). That is 0.328 pt per canvas unit. [source] |
| Shape | A classic pointer silhouette with **rounded corners**: a closed cubic-bezier path of 8 vertices, `DART_OUTLINE` (`build_default_theme.py:209-220`). It is rotated **20° clockwise about the hotspot (55,30)** so its left edge is vertical, i.e. **upright like the system arrow** (`UPRIGHT_DEGREES=20`). The arrow body is about 62×77 canvas units, roughly **20×25 pt**. [source] / [inference] for the pt size |
| Fill | Default Cua blue **#5EC0E8** (94,192,232) (`theme.rs:21`). A named session gets a stable fill from a 9-colour palette (below). [source] |
| Outline | **White, 5 canvas units ≈ 1.6 pt**, round joins and caps (`fill(BLUE), stroke(WHITE, 5)`). [source] |
| Glow | 8 strokes of the same path in the fill colour, widths/opacity% **44/2.0, 36/2.4, 29/3.0, 23/3.8, 18/4.8, 14/6.0, 10/7.5, 7/9.5**, layered under the body (`build_default_theme.py:256-265`). The result is a soft coloured glow following the silhouette, about 7 pt at its widest. There is **no black drop shadow**. [source] |
| Halo | A radial disc **28 pt across** (`HALO_RADIUS_PT=14`) centred on the body at **(2, 11.9) pt** from the hotspot. It is in the cursor colour at **11% peak alpha**, with stops (0→1.0, 0.45→0.55, 1→0). It is on by default (`cursor_pose.rs:128-141`). [source] |
| Palette (named sessions) | `#B284FF #F784AA #60DAAE #F4B242 #4CCCE0 #DD71EC #E85262 #B8DC36 #507EEC` (`theme.rs:23-33`). Bright, mid-saturation pastels. The index is chosen as follows: a numeric suffix `-N` gives N−1; a single letter gives a=0…; anything else uses FNV-1a (`theme.rs:52-75`). The anonymous cursor stays #5EC0E8. [source] |
| Effect colour | Effects use the fill **lifted 45% toward white** (`effects.rs:258-261`). [source] |
| Label / badge | Default `cursor.badges=minimal`: **only the session name**, and only while the human pointer hovers the agent cursor or while ≥2 agent cursors are on screen. **Nothing shows while it moves.** [doc] `verbose`: [source] (`session_badge.rs:21-31, 454-540`; `render_state.rs:45-59`) <ul><li>a pill **28 pt high**, fully rounded</li><li>centred **25 pt below** the hotspot</li><li>**Inter 11.5 pt**, 10 pt padding, max width **188 pt**, label cut at 28 chars, minimum width 55</li><li>fill is a 3-stop diagonal gradient `#5E97B2→#2B5C77→#0D1B26` mixed toward the session colour (66/52/26%) at about 93–96% alpha</li><li>1 px rim of white mixed 55% toward the session colour, alpha 190</li><li>an 8-step diffuse glow in black (y+2) plus one in the session colour</li><li>the name holds **2 s**, then fades over **0.4 s**</li></ul> |
| Action marks (`cursor.marks`, **off by default**) | Small white-outlined glyphs beside the arrow for observe, click, drag, scroll, text, key, navigate, app, transfer, record and system (`build_default_theme.py` `action_*`). Example, click: three short "spark" ticks up-left of the tip, scale 110→130→150%, opacity 0→1 by frame 4, gone by frame 20. [source]; seen in the GIF [video] |

GIF frames (`cua-action-animations.gif`, 1080×536, 60 frames at 15 fps) [video]:
- 12 cards on white, near-black (#1B1D22-ish) and Cua-blue backgrounds. The arrow reads on all three because of the white rim plus the coloured glow, including the blue-on-blue card.
- On the observe card, the "wifi" arcs above the tip pulse in and out.
- On the click card, the three sparks appear and expand, then fade.
- On the scroll card, the ↕ glyph bobs.
- In this GIF the arrow still **leans** about 20° (dart pose). The generator now makes it upright, so the GIF probably predates the upright change [inference].

### 1.3 Motion

Default path style: **`signature_arc`**. Default timing: **`steady`**.

| Property | Value |
|---|---|
| Path | The "Cua bezier" (`path.rs:126-145`). <ul><li>c1 = a + chord·`start_handle` + ⊥·d·(1−0.5·flow)</li><li>c2 = b − chord·`end_handle` + ⊥·d·(1−0.5·(1−flow))</li><li>d = `arc_size`·|ab|, flow = (`arc_flow`+1)/2</li></ul> `signature_arc` uses arc **0.16**, flow **0.15**, handles **0.3/0.3** (`spec.rs:171-182`, `params.rs:47-50`). The bend is to the **natural side**, which is **upward for horizontal moves** (`natural_side`, `path.rs:162`). The apex offset works out to about **9% of the distance, uncapped**: 0.375·(0.7125+0.7875)·0.16. [source] / [inference] for the 9% |
| Sampling | Each move is planned once as samples at **120 Hz** (`DT_MS = 1000/120`, `plan.rs:27`). A 256-step arc-length table means easing acts on distance, not on the bezier parameter (`path.rs`). Playback is interpolated by time at any frame rate. [source] |
| Easing | `signature_arc` natively uses **min-jerk** 10t³−15t⁴+6t⁵. The default `steady` timing **replaces the ease with in-out-sine** 0.5−0.5·cos(πt) "so a slow short hop does not linger at its ends" (`plan.rs:376-381`). [source] |
| Follow-through | `bump_profile`: overshoot of **1.8% of the distance, at most 8 pt**, peaking at **0.82** of the move, then settling back (`spec.rs:176-180`, `ease.rs:93-101`). It leaves the path along the end tangent (`path.rs:57-62`). [source] |
| Duration | Fitts: `(150 + 120·log2(D/W + 1))` clamped to **300–1000 ms**, then ×**1.1**. W is the target's smaller side, ≥4 pt, with a **24 pt** default box when there is no rect. `steady` then gives every move **at least 600 ms** (`relaxed` 450, `calm` 750). Examples [inference]: <ul><li>100 pt to a 40 pt button: **600 ms**</li><li>400 pt to 40 pt: **≈620 ms**</li><li>1500 pt to 24 pt: **≈955 ms**</li></ul> [source] for the formula (`spec.rs:79-107`, `style.rs:119-169`) |
| Arrival vs action | `arrival_t` is the first sample within **1 pt** of the target. "Cua Driver clicks then, and lets any follow-through or settle finish during the click" (`plan.rs:33, 742-747`; `cua-cursor-motion/README.md`). [source] |
| Rotation (`cursor.rotate=heading`, default) | The tip points along the velocity while travelling and is **upright before arrival**. It eases in over **0.15 s** and out over **0.25 s**, both stretched by (1+|turn|/π) for big turns. Hops under **40 pt** never rotate. `lean` mode instead caps the turn at 30° (15° while dragging) with tanh (`cursor_pose.rs:118-200`). The trajectory's own heading channel weights by speed ((v−40)/260, clamped) and smooths with k = 1−e^(−22·dt) (`plan.rs:680-731`). [source] |
| Float (default **on**) | A 4 s loop: sway ±1.64 pt (sin), bob ±1.97 pt (cos) centred 1.64 pt above the hotspot, tilt ±2.5° in phase with the bob. It runs at rest and while travelling (hops ≥30 pt), on its own clock. It drops to **exactly 0** before every click, drag, scroll or typing focus: it eases out in 0.2 s, holds 0 for **0.5 s** after settling, then eases back in over **0.8 s** with smoothstep (`cursor_pose.rs:24-112`; docs: "sways about 3.3 pt…, bobs about 3.9 pt…"). [source] |
| Long jumps / monitors | One overlay spans the virtual screen and moves are planned in points, so there is no special case for crossing monitors. DPI on Windows is not handled (see 1.1). [source] |
| Reduced motion | A straight, min-jerk, **120 ms** move with no float, rotation or effects. The halo stays (`plan.rs:36, 545-560`). `auto` follows the OS setting. [source] / [doc] |
| Other styles | `spring_settle` (arc 0.12, a damped 6 pt wobble), `magnetic` (40 pt capture radius, target glow), `comet_swoop` (arc 0.24, in-out-cubic, trail), `adaptive`, and `classic` (Dubins glide plus spring, k=400, the 1430 ms fixed "Codex-style" time) (`spec.rs`, `style.rs`). [source] |

### 1.4 Click and action feedback

- **Default `signature_arc`:** ripple, squish, glow and trail are all **off**. "The default style is plain" (`style.rs:79-81`). [source]
- **Built-in click animation** (theme `action_click`, 20 frames at 30 fps = **667 ms**): the arrow scales **100% → 93% (f7) → 103% (f12) → 100% (f20)**. Keyframes use bezier out (0.2,0.75) / in (0.25,1) (`build_default_theme.py:48-65, 396-419`). The three sparks only show with `cursor.marks` on. [source]
- **Opt-in ripple:** a ring of radius **8 → 52 pt**, eased out with 1−(1−k)³, width **5 → 1 pt**, alpha **0.75 → 0**, over **520 ms**, in the lifted effect colour (`effects.rs:53, 185-198`). [source]
- **Opt-in squish:** −12% scale in 50 ms, held for the 90 ms press, then a spring back over 220 ms (`effects.rs:57, 202-214`). [source]
- **Opt-in trail:** 0.18 s of path in 26 segments, width 2 → 12 pt, alpha 0.38·k², anchored **16 pt behind the tip** so the tip stays clean (`effects.rs:34-46`). [source]
- **Opt-in speed glow:** radius 30·(1+min(v·0.00024, 0.44)), alpha min(v·0.00014, 0.42). [source]
- **macOS only:** with `marks` on, a brief focus box around the clicked element. [doc]

### 1.5 Idle and multi-cursor

- **Idle hide:** after **15 s** of no activity (`AGENT_CURSOR_IDLE_TIMEOUT`), a **180 ms linear** fade. Session end hides it at once (`cua-driver-core/src/agent_cursor.rs:126-160`). [source]
- **One cursor per session**, keyed in `RenderMap`. Each has its own visual state and its own arrival waiter, all composited into the one overlay. The cursor's colour comes from the session id, and agents cannot pick one ("without accepting an agent-controlled styling argument", `theme.rs:36-40`). [source]

---

## 2. Side by side

| Property | cua (default) | winuse today |
|---|---|---|
| Shape | Upright classic arrow, rounded corners (bezier), 42 pt footprint, body ≈20×25 pt | Sharp 4-point dart from agent-desktop, 32×40 glyph box, body ≈29×33 px (`cursor_layout.go:38`) |
| Fill | Pastel #5EC0E8 | Saturated #4299FF (`cursor_raster.go:45`) |
| Outline | White 1.6 pt | Dark #111318, 3.25 px each side (`cursor_layout.go:44-51`) |
| Depth | Coloured silhouette glow (8 strokes) + 28 pt halo at 11% | Black drop shadow 34%, offset (2.5,3), blur 5 |
| Contrast on a dark app | Strong (white rim) | Weak: the dark rim and shadow vanish on dark Calculator (`docs/images/winuse-calculator.gif`) [video] |
| Palette | 9 pastels, stable by session id | 7 saturated colours, in order handed out, plus `WINUSE_CURSOR_COLOR` (`cursor_colors.go:17-25`) |
| Label | Name only, on hover or with ≥2 cursors; never while moving | Always: the action text (`click "Two"`, `agent: …`), black pill 24 px, Segoe UI Variable Semibold 12.5, right-below the tip (+20,+30), revealed 180 ms before landing |
| Path | Cubic arc, apex ≈9% of D, uncapped, bends upward, 1.8% follow-through ≤8 pt | Straight line + sine bow 5.5% of D capped at 38 px, bends downward for rightward moves (`cursor_motion.go:160-177`) |
| Ease | In-out-sine (steady); min-jerk natively | Min-jerk |
| Duration | Fitts×1.1, 300–1000 ms, **≥600 ms** | 120+60·log2(1+d/50), clamped to **150–400 ms** (≈310 ms at 400 px) (`cursor_motion.go:113-124`) |
| Acts at | Tip within 1 pt (`arrival_t`); settle continues | End of the path |
| Rotation | Tip follows the path, upright at arrival | None |
| Idle life | Float: 4 s sway/bob/tilt, zero for actions | Still |
| Click | Arrow press-scale 0.93→1.03 over 667 ms (ripple opt-in: 520 ms ring 8→52 pt) | 300 ms disc 19 px + ring to 50 px, linear fade, plus a 900 ms element outline with pop (`cursor_layout.go:78-97`) |
| Frame rate | ~125 Hz (8 ms timer + `timeBeginPeriod(1)` while animating) | ~64 Hz (10 ms `SetTimer` rounds up to the 15.6 ms tick) (`cursor_motion.go:61-65`) |
| Idle hide | 15 s, 180 ms linear fade | 30 s, 200 ms linear fade |
| Windows | One virtual-screen layered window, dirty rects | 3–4 small layered windows per Agent (arrow+pill, ripple, outline, caret); the arrow window is only **moved** while travelling |
| Z-order | Just above the target window | Same (ported, ADR 0014) |
| Capture exclusion | WDA only **during its own capture** | WDA **permanently** (`cursor_view.go:173-177`) **and** HideWhile around winuse's captures |
| DPI | Not handled on Windows (TODO) | Per-monitor DPI throughout ✔ |
| Reduced motion | 120 ms straight, no float or rotation | Not handled |
| Extra | Action marks (opt-in), theme system | Element outline, insertion caret, tab-group colour ✔ |

---

## 3. Ranked changes for winuse

Every change stays inside the existing design: the pure model in `cursor_motion.go`/`cursor_layout.go`, the pure Go SDF rasterizer in `cursor_raster.go`, and `UpdateLayeredWindow` from a DIB on the UI thread. No cgo and no new DLLs are needed. GDI+ (flat API through `syscall`) or Direct2D would also work without cgo but bring no benefit here (see #4).

### 1. Retune the motion to cua's `signature_arc` + `steady` (S, highest value)

Do it all in `cursor_motion.go`:

- **Duration:**
  - `ms = clamp(150 + 120·log2(D/W + 1), 300, 1000) · 1.1`, with D in logical px, W = min(target w, h) in logical px (≥4; 24 when there is no rect).
  - Then `max(ms, 600)`. `Travel` already gets the target `Rect`.
- **Path:** a cubic bezier, using winuse's own `fpoint`:
  - `u` = unit(ab), `n` = (u.y, −u.x) when u.x ≥ 0, else (−u.y, u.x). This bends upward for horizontal moves.
  - `d` = 0.16·|ab|, flow = 0.575.
  - c1 = a + 0.3·(b−a) + n·d·0.7125
  - c2 = b − 0.3·(b−a) + n·d·0.7875
  - Build a 64–256-entry arc-length table so the ease acts on distance.
- **Ease:** `0.5 − 0.5·cos(π·t)`, plus a follow-through bump of `over = min(0.018, 8·scale/D)` peaking at τ=0.82. Port `bump_profile` verbatim: a = max(at·10, 1.5), b = max((1−at)·10, 1.5), normalised τᵃ(1−τ)ᵇ. Overshoot past t=1 goes along the end tangent.
- **Arrival:** `Travel` should return at **arrival** (the first sample within 1 px of the target), not at `dur`. The settle keeps animating during the Action.
- **Why:** this is most of "moves really well". A 600 ms+ arc is legible, while 150–400 ms reads as a jump in the GIF.
- **Risk:** every Headless Action gets about 300 ms slower. `cursorTravelSlack` and the `Travel` bound (2× the estimate) still cover it. If throughput matters, use cua's `relaxed` floor of **450 ms**. The label reveal (`labelRevealStart`) keys off `path.dur`, so switch it to arrival. `cursor_test.go` timing expectations change.

### 2. New arrow: upright rounded pointer, white rim, coloured glow (M)

- **Geometry:**
  - Use cua's outline (MIT): the 8-vertex bezier in `cua-default-arrow.svg`, scaled by 42/128 per logical px, hotspot (55,30).
  - Or draw an equivalent of our own, to keep winuse's look distinct.
  - Flatten each cubic into about 12 segments to get a polygon (~96 points), then reuse `polygonSD`.
  - Drop `cursorDart` and `cursorDartRimGrow`.
- **Paint order** (`drawDart`):
  1. glow: `fillSoft` with strength `exp(-max(sd,0)/(3.5·scale))·0.22` in the fill colour, out to about 7 px
  2. halo: radial at (2, 11.9)·scale from the tip, r = 14·scale, alpha 0.11·(1→0.55 at 0.45→0)
  3. white rim: `sd − 1.6·scale`
  4. fill
- **Colours:**
  - Default → **#5EC0E8**.
  - Palette → cua's 9 pastels, or ours lightened by about 25%. Keep the order handout, and keep `TabGroupColor` working: hues change, so re-check `cursor_colors_test.go`.
  - `AgentCursorAccent` feeds the e2e "never in our screenshots" test, so it must follow the new colour.
- **Why:** this fixes the contrast problem seen in the Calculator GIF and gives cua's soft, friendly look.
- **Risk:**
  - Cost is low: the arrow is rendered only on a look change.
  - The window grows by about 8 px per side for the glow; `dartBounds` must include it.
  - DPI: it is already scaled.

### 3. Smoother frames: about 120 Hz while animating (S)

- Hold `timeBeginPeriod(1)` only while `cursorModel.animating()` and release it when parked. This is exactly cua's pattern, done once across all Agents' cursors.
- Then set `cursorFrameInterval` to 8 ms.
- Alternative: a `CreateWaitableTimerExW(CREATE_WAITABLE_TIMER_HIGH_RESOLUTION)` with `MsgWaitForMultipleObjects` in the UI-thread loop. This avoids the global timer change but touches `uithread.go`.
- **Risk:**
  - Power use while animating only.
  - The UI thread also runs the Esc hook, and each frame of the moved-only arrow window is just a ULW with a new `pptDst`, so CPU stays small.
  - The ripple and outline repaint per frame, so measure with `WINUSE_CURSOR_TRACE`.

### 4. Rotate the tip along the path (M)

- **Model** (pure, testable):
  - target angle = wrap(atan2(v) − tipAxis), where tipAxis = atan2(−11.9, −2).
  - Ramps: smoothstep in over 0.15 s and out over 0.25 s, both × (1+|turn|/π).
  - 0 for hops under 40 logical px and at arrival.
- **View:**
  - Rotation forces a re-render, but only during travel.
  - Cache rotated arrow sprites per look, quantised to **3°** (≤120 sprites, made lazily, about 70×70×4 B ≈ 20 KB each at 100%).
  - Each frame is then a memcpy plus ULW. The SDF is computed once per angle.
  - Pill composition stays as it is, since the pill does not rotate.
- **Why:** it is a big part of the "alive" feel in cua.
- **Risk:** memory (cap the cache, drop it on look change); 200% DPI quadruples sprite size.

### 5. Click feedback like cua's (S)

- Replace the disc+ring ripple with an **arrow press-scale** driven by `keyframe()`:
  - times (0, 0.35, 0.6, 1) over 667 ms
  - values (1, 0.93, 1.03, 1), each segment eased with `cubicBezier(0.2,0.75,0.25,1)`
  - scaled about the tip (`scalePixels` already exists)
- Optionally keep a lighter ring: r 8→52 px (×scale), eased with 1−(1−k)³, width 5→1, alpha 0.75→0, 520 ms, colour = fill lifted 45% to white.
- **Keep** winuse's element outline: cua has no equivalent on Windows, and it is useful.
- **Risk:** none of note. The ripple window becomes optional.

### 6. Idle float (S–M)

- Position-only sway and bob: dx = 1.64·sin, dy = 1.97·cos − 1.64, in logical px × scale, over a 4 s loop.
- **Skip the tilt**, so there is no re-render and each frame only moves the window.
- Exactly 0 whenever `Travel` lands or a Click, Caret or Action is in flight: hold 0.5 s, then ease in over 0.8 s with smoothstep.
- Run it at **30 Hz** while the cursor rests, until the idle fade.
- **Risk:**
  - The UI thread wakes 30×/s while any cursor rests (up to 30 s idle). Cost is small but not zero; it could be gated behind `WINUSE_CURSOR_FLOAT`.
  - It must never shift the hotspot during an Action. The `cursor_caret` and HideWhile paths need the zero-amplitude guarantee.

### 7. Restyle the label pill, keep winuse's action text (S)

- Keep the always-on action label. It is winuse's advantage, since CONTEXT.md says the label is whole when the cursor lands.
- Restyle it to cua's verbose badge:
  - 3-stop diagonal gradient of `#5E97B2/#2B5C77/#0D1B26` mixed toward the Agent colour (66/52/26%), at 93–96% alpha
  - 1 px rim of white mixed 55% toward the Agent colour, alpha 0.75
  - a soft session-coloured glow instead of the 31% black shadow
- Font 11.5–12 px, height 26–28.
- Keep the right-below placement (+20,+30): centring below the tip, as cua does, would sit on top of the element being clicked.

### 8. Capture-exclusion policy (S, but **your decision**: amends ADR 0006 / headless spec)

- Permanent `WDA_EXCLUDEFROMCAPTURE` keeps the cursor out of OBS, Snipping Tool recordings, Teams sharing and so on. The README GIF needed the demo-only switch (`cursorExcludeFromCapture`) to show it.
- CONTEXT.md only promises absence from **winuse's own** screenshots, which `HideWhile` already provides.
- cua sets the affinity only around its own capture.
- **Option:** drop the permanent affinity, or set it only inside `HideWhile`, followed by `DwmFlush`.
- **Risk:** other tools' screenshots would then show the cursor, which may be wanted ("show your team what the agent did") or not.

### 9. Reduced motion (S)

- Read `SPI_GETCLIENTAREAANIMATION`.
- When it is off: a 120 ms straight min-jerk move, no float, no rotation and no press-scale.

### 10. Later / optional (M each)

- Shorter idle hide: 15 s like cua, if 30 s feels long.
- Comet trail: 0.18 s, 26 segments, width 2→12, alpha 0.38k², anchored 16 px behind the tip. This needs the arrow window to grow, or its own trail window.
- Speed glow.

**Not recommended:** moving to one virtual-screen overlay window, as cua does. winuse's small-window, move-only design is cheaper and already per-monitor-DPI correct. cua had to add dirty rects to make the big window affordable.

### Ready-to-implement spec (defaults)

```
arrow       cua.default outline, 42/128 px per unit, upright, hotspot (55,30)
fill        #5EC0E8 default; palette #B284FF #F784AA #60DAAE #F4B242 #4CCCE0 #DD71EC #E85262 #B8DC36 #507EEC
rim         white, 1.6 px; glow in fill colour ≈7 px soft; halo r 14 px @ (2,11.9), 11% peak
path        cubic arc 0.16·D, flow 0.15, handles 0.3, bend upward for horizontal moves
ease        in-out-sine + bump(1.8% ≤ 8 px, at 0.82)
duration    max(600, 1.1·clamp(150+120·log2(D/W+1), 300, 1000)) ms
act at      tip within 1 px of target
rotation    tangent, ramps 0.15/0.25 s ×(1+|turn|/π), none under 40 px, upright at arrival
click       arrow scale 1→0.93→1.03→1 over 667 ms; optional ring 8→52 px, 520 ms, α .75→0
float       sway 1.64 / bob 1.97 px, 4 s loop, 0 for 0.5 s after any action, ease-in 0.8 s
frames      ~120 Hz while animating (timeBeginPeriod(1) scoped), 30 Hz float, parked otherwise
idle        fade 180–200 ms after 15–30 s
reduced     120 ms straight, no float/rotation/press
```
