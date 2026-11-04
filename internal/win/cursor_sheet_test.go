package win

// The Agent Cursor's contact sheet (cursor-look): the cursor is excluded
// from screen capture, so its look is checked by rendering frames with
// the view's own code — the arrow master and its sprites, the pill and
// GDI's label, the press, the model's path — into PNG files, with no
// window created. Opt-in, never part of the default run:
//
//	WINUSE_CURSOR_SHEET=<dir> go test ./internal/win/ -run TestAgentCursorContactSheet -v -count=1
//
// It writes <dir>/cursor-sheet.png. With WINUSE_CURSOR_SHEET_CALC set to
// the README's original Calculator GIF (.scratch/cursor-look/
// winuse-calculator-before.gif) it also writes <dir>/winuse-calculator.gif:
// that recording's Calculator, its old cursor removed, clicked 7 + 2 = by
// the new cursor driven by the real model.

import (
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// sheetWin is a cursorWin on a memory DC alone: renderArrow, the label's
// GDI text and the composition run as on screen, minus the window.
func sheetWin(t *testing.T, accent color.RGBA) *cursorWin {
	t.Helper()
	dc, _, _ := procCreateCompatibleDC.Call(0)
	if dc == 0 {
		t.Fatal("CreateCompatibleDC failed")
	}
	w := &cursorWin{accent: accent, fonts: map[int32]uintptr{}, arrow: &layeredWin{dc: dc}}
	t.Cleanup(func() {
		if w.arrow.bmp != 0 {
			procSelectObject.Call(dc, w.arrow.origBmp)
			procDeleteObject.Call(w.arrow.bmp)
		}
		procDeleteDC.Call(dc)
		for _, f := range w.fonts {
			procDeleteObject.Call(f)
		}
	})
	return w
}

// frame renders one frame of the cursor window: look with its pill at
// labelOp and labelPop, the arrow turned by angle and pressed by press.
// It returns the window's pixels and where the tip sits in them.
func (w *cursorWin) frame(look cursorLook, labelOp, labelPop, angle, press float64) (*surface, fpoint) {
	if !w.lookOK || look != w.look {
		w.lookOK = w.renderArrow(look)
		w.look = look
	}
	w.composeCard(labelOp, labelPop)
	w.composeArrow(spriteFor(angle, press))
	s := &surface{w: w.arrow.pix.w, h: w.arrow.pix.h, pix: append([]uint32(nil), w.arrow.pix.pix...)}
	return s, w.layout.Tip
}

// lookAt is the controller's look for a travel to tip (cursorUI.lookFor).
func (w *cursorWin) lookAt(tip fpoint, name, label string, work Rect, scale float64) cursorLook {
	return (&cursorUI{view: w}).lookFor(tip, name, label, work, scale)
}

// paste composites the premultiplied s over img with s's (0, 0) at (x, y).
func paste(img *image.RGBA, s *surface, x, y int) {
	for j := 0; j < s.h; j++ {
		for i := 0; i < s.w; i++ {
			p := s.pix[j*s.w+i]
			if p == 0 || !image.Pt(x+i, y+j).In(img.Rect) {
				continue
			}
			o := img.PixOffset(x+i, y+j)
			inv := 255 - p>>24
			blend := func(src uint32, dst uint8) uint8 { return uint8(min(src+(uint32(dst)*inv+127)/255, 255)) }
			img.Pix[o] = blend(p>>16&0xFF, img.Pix[o])
			img.Pix[o+1] = blend(p>>8&0xFF, img.Pix[o+1])
			img.Pix[o+2] = blend(p&0xFF, img.Pix[o+2])
			img.Pix[o+3] = 0xFF
		}
	}
}

// pasteCursor puts a cursor frame on img with its tip at tip.
func pasteCursor(img *image.RGBA, s *surface, at, tip fpoint) {
	x, y := roundPoint(fpoint{X: tip.X - at.X, Y: tip.Y - at.Y})
	paste(img, s, int(x), int(y))
}

func fillRect(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	draw.Draw(img, r, &image.Uniform{C: c}, image.Point{}, draw.Src)
}

// caption writes text (white, the label's font) centred in r.
func caption(img *image.RGBA, cw *cursorWin, text string, r image.Rectangle, scale float64) {
	if !cw.arrow.resize(int32(r.Dx()), int32(r.Dy())) {
		return
	}
	cw.arrow.pix.clear()
	cw.drawLabel(text, Rect{Right: int32(r.Dx()), Bottom: int32(r.Dy())}, scale)
	paste(img, &surface{w: cw.arrow.pix.w, h: cw.arrow.pix.h, pix: cw.arrow.pix.pix}, r.Min.X, r.Min.Y)
}

var (
	sheetBg    = color.RGBA{R: 0x2A, G: 0x2D, B: 0x33, A: 0xFF}
	sheetLight = color.RGBA{R: 0xF3, G: 0xF3, B: 0xF3, A: 0xFF}
	sheetDark  = color.RGBA{R: 0x1B, G: 0x1D, B: 0x22, A: 0xFF}
	sheetApp   = color.RGBA{R: 0x20, G: 0x20, B: 0x20, A: 0xFF}
)

func TestAgentCursorContactSheet(t *testing.T) {
	dir := os.Getenv("WINUSE_CURSOR_SHEET")
	if dir == "" {
		t.Skip("contact sheet: set WINUSE_CURSOR_SHEET=<dir> to render the Agent Cursor to PNG")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const W, H = 1600, 3200
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	fillRect(img, img.Rect, sheetBg)
	cw := sheetWin(t, cursorAccent)
	def := sheetWin(t, cursorAccent)
	y := 16
	heading := func(text string) {
		caption(img, cw, text, image.Rect(0, y, W, y+30), 1.4)
		y += 38
	}
	card := func(x, w, h int, bg color.RGBA) image.Rectangle {
		r := image.Rect(x, y, x+w, y+h)
		fillRect(img, r, bg)
		return r
	}

	// 1. The arrow at 100 % and 200 % on light, dark and the accent, as
	// cua's cards show it.
	heading("Arrow: 100 % and 200 %, on light, dark and its own colour (no label)")
	x := 16
	for _, bg := range []color.RGBA{sheetLight, sheetDark, cursorAccent} {
		for _, scale := range []float64{1, 2} {
			r := card(x, 150, 150, bg)
			s, at := def.frame(cursorLook{Scale: scale}, 1, 1, 0, 1)
			pasteCursor(img, s, at, fpoint{X: float64(r.Min.X) + 60, Y: float64(r.Min.Y) + 40})
			x += 160
		}
	}
	y += 170

	// 2. Every Agent's colour: the default, then the palette in order.
	heading("Colours: the default Agent, then the palette in hand-out order (200 %), on dark and light")
	colours := append([]color.RGBA{cursorAccent}, CursorPalette...)
	for row, bg := range []color.RGBA{sheetDark, sheetLight} {
		x = 16
		for _, c := range colours {
			r := card(x, 150, 120, bg)
			s, at := sheetWin(t, c).frame(cursorLook{Scale: 2}, 1, 1, 0, 1)
			pasteCursor(img, s, at, fpoint{X: float64(r.Min.X) + 55, Y: float64(r.Min.Y) + 22})
			x += 155
		}
		y += 128
		_ = row
	}
	y += 14

	// 3. Turning: the sprites every 30°, and the turn of a rightward move.
	heading("Turning (200 %): every 30°, then 105° — the full turn of a rightward move, tip leading")
	x = 16
	angles := []float64{0, 30, 60, 90, 120, 150, 180, -150, -120, -90, -60, -30, 105}
	for _, deg := range angles {
		r := card(x, 116, 130, sheetDark)
		s, at := def.frame(cursorLook{Scale: 2}, 1, 1, deg*math.Pi/180, 1)
		pasteCursor(img, s, at, fpoint{X: float64(r.Min.X) + 58, Y: float64(r.Min.Y) + 65})
		x += 121
	}
	y += 148

	// 4. The click's press, every 1/12 of its 667 ms.
	heading("Click press (200 %): 1 → 0.93 → 1.03 → 1 over 667 ms, a frame every 56 ms, with the element outline")
	x = 16
	target := Rect{Left: 0, Top: 0, Right: 96, Bottom: 52}
	for k := 0; k <= 12; k++ {
		el := cursorPressDuration * time.Duration(k) / 12
		r := card(x, 116, 150, sheetLight)
		tgt := Rect{Left: int32(r.Min.X) + 10, Top: int32(r.Min.Y) + 20, Right: int32(r.Min.X) + 10 + target.Right, Bottom: int32(r.Min.Y) + 20 + target.Bottom}
		if op := outlineOpacity(el); op > 0 {
			o := outlineWindowRect(tgt, Rect{Right: W, Bottom: H}, 1)
			ol := &surface{w: int(o.Width()), h: int(o.Height()), pix: make([]uint32, o.Width()*o.Height())}
			ol.drawOutline(Rect{Left: tgt.Left - o.Left, Top: tgt.Top - o.Top, Right: tgt.Right - o.Left, Bottom: tgt.Bottom - o.Top}, 1, outlinePop(el), cursorAccent)
			for i := range ol.pix {
				ol.pix[i] = dimPixel(ol.pix[i], op)
			}
			paste(img, ol, int(o.Left), int(o.Top))
		}
		s, at := def.frame(cursorLook{Scale: 2}, 1, 1, 0, pressScale(el))
		pasteCursor(img, s, at, rectCentre(tgt))
		x += 121
	}
	y += 168

	// 5. The path: a 1000 px move to a button, sampled at 120 frames per
	// second (dots), the arrow every 60 ms, the label revealing for the
	// arrival; then a down-left move and a short hop.
	heading("Path (100 %): dots every frame at 120 fps, the arrow every 60 ms; label whole at the arrival")
	panel := card(16, W-32, 420, sheetApp)
	moves := []struct {
		from, to fpoint
		box      Rect
		label    string
	}{
		{fpoint{80, 330}, fpoint{1080, 330}, Rect{Left: 1040, Top: 312, Right: 1120, Bottom: 348}, `click "Save"`},
		{fpoint{1400, 60}, fpoint{1180, 360}, Rect{Left: 1150, Top: 345, Right: 1210, Bottom: 375}, ""},
		{fpoint{300, 120}, fpoint{420, 90}, Rect{Left: 400, Top: 78, Right: 440, Bottom: 102}, ""},
	}
	inPanel := func(r Rect) Rect {
		return Rect{Left: r.Left + int32(panel.Min.X), Top: r.Top + int32(panel.Min.Y), Right: r.Right + int32(panel.Min.X), Bottom: r.Bottom + int32(panel.Min.Y)}
	}
	for _, mv := range moves { // the targets first, under every frame
		box := inPanel(mv.box)
		fillRect(img, image.Rect(int(box.Left), int(box.Top), int(box.Right), int(box.Bottom)), color.RGBA{R: 0x3B, G: 0x3B, B: 0x3B, A: 0xFF})
	}
	for _, mv := range moves {
		off := func(p fpoint) fpoint { return fpoint{X: p.X + float64(panel.Min.X), Y: p.Y + float64(panel.Min.Y)} }
		box := inPanel(mv.box)
		var m cursorModel
		from := off(mv.from)
		m.startTravel(t0, Rect{Left: int32(from.X), Top: int32(from.Y), Right: int32(from.X), Bottom: int32(from.Y)}, box, 1, false)
		if mv.label != "" {
			m.revealLabel()
		}
		look := def.lookAt(rectCentre(box), "", mv.label, Rect{Right: W, Bottom: H}, 1)
		var dots []fpoint
		for el := time.Duration(0); el <= m.path.dur; el += cursorFrameInterval {
			f := m.advance(t0.Add(el))
			dots = append(dots, f.Tip)
			if last := el+cursorFrameInterval > m.path.dur; last || el%(60*time.Millisecond) < cursorFrameInterval {
				l := look
				if !last {
					l = cursorLook{Scale: 1} // the label only once, as it stands at the end
				}
				s, at := def.frame(l, f.LabelOp, f.LabelPop, f.Angle, f.Press)
				pasteCursor(img, s, at, f.Tip)
			}
		}
		dot := &surface{w: 3, h: 3, pix: make([]uint32, 9)}
		for i := range dot.pix {
			dot.pix[i] = premultiply(color.RGBA{R: 0xFF, G: 0xD0, B: 0x40, A: 0xFF}, 0.9)
		}
		for _, d := range dots {
			paste(img, dot, int(d.X)-1, int(d.Y)-1)
		}
		t.Logf("path %+v→%+v: %v, arrives at %v (%d frames at 120 fps)", mv.from, mv.to, m.path.dur, m.path.arrive, len(dots))
	}
	writePNG(t, filepath.Join(dir, "cursor-path.png"), img.SubImage(panel))
	y += 438

	// 6. Labels in each Agent's colour, at 100 % and 200 %, on light and dark.
	heading(`Label tags: flat in the Agent's colour, white or near-black text; the default Agent one line, named Agents name over action; 100 % and 200 %`)
	labels := []struct {
		c           color.RGBA
		name, label string
	}{
		{cursorAccent, "", `click "Save"`},
		{CursorPalette[0], "reviewer", `set-value "File name"`},
		{CursorPalette[3], "release-notes-writer-for-the-quarterly-report-changelog-and-the-migration-guide",
			`select "A very long option that is cut short at the pill's widest, on its own line"`},
		{CursorPalette[4], "claude", `click "Delete"`},
		{CursorPalette[2], "codex", `toggle "Remember me"`},
		{CursorPalette[5], "writer", `type "Quarterly report"`},
	}
	for _, scale := range []float64{1, 2} {
		perRow := 3
		if scale == 2 {
			perRow = 2
		}
		for _, bg := range []color.RGBA{sheetDark, sheetLight} {
			h := int(84 * scale)
			for row := 0; row < len(labels)/perRow; row++ {
				x = 16
				for _, l := range labels[row*perRow : (row+1)*perRow] {
					w := int(386 * scale)
					r := card(x, w, h, bg)
					lw := sheetWin(t, l.c)
					tip := fpoint{X: float64(r.Min.X) + 14*scale, Y: float64(r.Min.Y) + 10*scale}
					look := lw.lookAt(tip, l.name, l.label, Rect{Left: int32(r.Min.X), Top: int32(r.Min.Y), Right: int32(r.Max.X), Bottom: H}, scale)
					s, at := lw.frame(look, 1, 1, 0, 1)
					pasteCursor(img, s, at, tip)
					x += w + 8
				}
				y += h + 8
			}
		}
		if scale == 1 {
			y = referencesStrip(t, img, cw, filepath.Join(dir, "refs"), y)
		}
	}
	y += 8

	// 7. The opt-in trail, mid-travel.
	heading("Opt-in trail (WINUSE_CURSOR_TRAIL=1), mid-travel at 100 %")
	tp := card(16, 760, 180, sheetApp)
	var m cursorModel
	m.trail = true
	start := fpoint{X: float64(tp.Min.X) + 40, Y: float64(tp.Min.Y) + 150}
	dest := Rect{Left: int32(tp.Min.X) + 700, Top: int32(tp.Min.Y) + 120, Right: int32(tp.Min.X) + 740, Bottom: int32(tp.Min.Y) + 150}
	m.startTravel(t0, Rect{Left: int32(start.X), Top: int32(start.Y), Right: int32(start.X), Bottom: int32(start.Y)}, dest, 1, false)
	f := m.advance(t0.Add(m.path.dur * 45 / 100))
	tr := trailWindowRect(f.Trail, 1)
	ts := &surface{w: int(tr.Width()), h: int(tr.Height()), pix: make([]uint32, tr.Width()*tr.Height())}
	local := make([]fpoint, len(f.Trail))
	for i, p := range f.Trail {
		local[i] = fpoint{X: p.X - float64(tr.Left), Y: p.Y - float64(tr.Top)}
	}
	ts.drawTrail(local, 1, cursorAccent, nil)
	paste(img, ts, int(tr.Left), int(tr.Top))
	s, at := def.frame(cursorLook{Scale: 1}, 1, 1, f.Angle, 1)
	pasteCursor(img, s, at, f.Tip)
	y += 196

	out := img.SubImage(image.Rect(0, 0, W, min(y, H))).(*image.RGBA)
	writePNG(t, filepath.Join(dir, "cursor-sheet.png"), out)
	// The arrow alone, large, beside cua's rebuilt SVG.
	big := image.NewRGBA(image.Rect(0, 0, 336, 336))
	fillRect(big, big.Rect, sheetDark)
	bs, bat := def.frame(cursorLook{Scale: 8}, 1, 1, 0, 1)
	pasteCursor(big, bs, bat, fpoint{X: 55 * 336.0 / 128, Y: 30 * 336.0 / 128})
	writePNG(t, filepath.Join(dir, "cursor-arrow-large.png"), big)

	if calc := os.Getenv("WINUSE_CURSOR_SHEET_CALC"); calc != "" {
		renderCalculatorGIF(t, calc, filepath.Join(dir, "winuse-calculator.gif"))
	}
}

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s", path)
}

// renderCalculatorGIF re-records the README's Calculator demo with the
// new cursor: the original GIF's frames give the Calculator (the old
// cursor removed by a per-pixel median of three frames where it stood in
// different places) and its display after each click; the model drives
// the cursor exactly as the helper's Travel and Click do.
func renderCalculatorGIF(t *testing.T, src, dst string) {
	f, err := os.Open(src)
	if err != nil {
		t.Fatalf("the original Calculator GIF: %v", err)
	}
	g, err := gif.DecodeAll(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	W, H := g.Config.Width, g.Config.Height
	frames := make([]*image.RGBA, len(g.Image))
	acc := image.NewRGBA(image.Rect(0, 0, W, H))
	for i, fr := range g.Image {
		draw.Draw(acc, fr.Bounds(), fr, fr.Bounds().Min, draw.Over)
		frames[i] = image.NewRGBA(acc.Rect)
		copy(frames[i].Pix, acc.Pix)
	}
	// The display (top) shows each state; below it, the buttons without
	// a cursor.
	const displayBottom = 185
	buttons := image.NewRGBA(acc.Rect)
	for i := 0; i < len(buttons.Pix); i += 4 {
		for c := range 3 {
			v := []uint8{frames[4].Pix[i+c], frames[12].Pix[i+c], frames[40].Pix[i+c]}
			sort.Slice(v, func(a, b int) bool { return v[a] < v[b] })
			buttons.Pix[i+c] = v[1]
		}
		buttons.Pix[i+3] = 0xFF
	}
	backdrop := func(state int) *image.RGBA {
		b := image.NewRGBA(acc.Rect)
		copy(b.Pix, buttons.Pix)
		draw.Draw(b, image.Rect(0, 0, W, displayBottom), frames[state], image.Point{}, draw.Src)
		return b
	}
	states := []*image.RGBA{backdrop(4), backdrop(12), backdrop(20), backdrop(24), backdrop(32)}

	keys := []struct {
		label string
		box   Rect
	}{
		{`click "Seven"`, Rect{Left: 5, Top: 321, Right: 81, Bottom: 371}},
		{`click "Plus"`, Rect{Left: 242, Top: 426, Right: 318, Bottom: 476}},
		{`click "Two"`, Rect{Left: 84, Top: 426, Right: 160, Bottom: 476}},
		{`click "Equals"`, Rect{Left: 241, Top: 479, Right: 318, Bottom: 530}},
	}
	w := sheetWin(t, cursorAccent)
	window := Rect{Right: int32(W), Bottom: int32(H)}
	const step = 30 * time.Millisecond // about 33 frames per second
	var (
		m     cursorModel
		look  cursorLook
		out   []*image.RGBA
		state = 0
		now   = t0
	)
	shoot := func(d time.Duration) {
		for end := now.Add(d); now.Before(end); now = now.Add(step) {
			fr := m.advance(now)
			img := image.NewRGBA(acc.Rect)
			copy(img.Pix, states[state].Pix)
			if fr.Outline {
				o := outlineWindowRect(fr.OutlineRect, window, 1)
				ol := &surface{w: int(o.Width()), h: int(o.Height()), pix: make([]uint32, o.Width()*o.Height())}
				ol.drawOutline(Rect{Left: fr.OutlineRect.Left - o.Left, Top: fr.OutlineRect.Top - o.Top,
					Right: fr.OutlineRect.Right - o.Left, Bottom: fr.OutlineRect.Bottom - o.Top}, 1, fr.OutlinePop, cursorAccent)
				for i := range ol.pix {
					ol.pix[i] = dimPixel(ol.pix[i], fr.OutlineOp)
				}
				paste(img, ol, int(o.Left), int(o.Top))
			}
			if fr.Cursor {
				s, at := w.frame(look, fr.LabelOp, fr.LabelPop, fr.Angle, fr.Press)
				for i := range s.pix {
					s.pix[i] = dimPixel(s.pix[i], fr.Opacity)
				}
				pasteCursor(img, s, at, fr.Tip)
			}
			out = append(out, img)
		}
	}
	shoot(400 * time.Millisecond)
	for i, k := range keys {
		look = w.lookAt(rectCentre(k.box), "", k.label, window, 1)
		m.startTravel(now, window, k.box, 1, false)
		m.revealLabel()
		shoot(m.path.arrive)
		m.startClick(now, k.box, 1, false)
		state = i + 1
		shoot(450 * time.Millisecond)
	}
	shoot(1800 * time.Millisecond) // the float
	writeGIF(t, dst, out, step)
}

// writeGIF encodes frames with one popularity palette and, after the
// first frame, only the rectangle that changed.
func writeGIF(t *testing.T, path string, frames []*image.RGBA, step time.Duration) {
	t.Helper()
	counts := map[[3]uint8]int{}
	for _, fr := range frames {
		for i := 0; i < len(fr.Pix); i += 4 {
			counts[[3]uint8{fr.Pix[i] &^ 7, fr.Pix[i+1] &^ 7, fr.Pix[i+2] &^ 7}]++
		}
	}
	type kc struct {
		k [3]uint8
		n int
	}
	var all []kc
	for k, n := range counts {
		all = append(all, kc{k, n})
	}
	sort.Slice(all, func(a, b int) bool { return all[a].n > all[b].n })
	var pal color.Palette
	for _, e := range all {
		if len(pal) == 256 {
			break
		}
		pal = append(pal, color.RGBA{R: e.k[0] | 4, G: e.k[1] | 4, B: e.k[2] | 4, A: 0xFF})
	}
	anim := &gif.GIF{}
	var prev *image.RGBA
	for _, fr := range frames {
		r := fr.Rect
		if prev != nil {
			r = image.Rectangle{}
			for y := 0; y < fr.Rect.Dy(); y++ {
				for x := 0; x < fr.Rect.Dx(); x++ {
					o := fr.PixOffset(x, y)
					if fr.Pix[o] != prev.Pix[o] || fr.Pix[o+1] != prev.Pix[o+1] || fr.Pix[o+2] != prev.Pix[o+2] {
						r = r.Union(image.Rect(x, y, x+1, y+1))
					}
				}
			}
			if r.Empty() {
				r = image.Rect(0, 0, 1, 1)
			}
		}
		p := image.NewPaletted(r, pal)
		draw.Draw(p, r, fr, r.Min, draw.Src)
		anim.Image = append(anim.Image, p)
		anim.Delay = append(anim.Delay, int(step/(10*time.Millisecond)))
		anim.Disposal = append(anim.Disposal, gif.DisposalNone)
		prev = fr
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := gif.EncodeAll(f, anim); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s (%d frames)", path, len(frames))
}

// BenchmarkCursorFrame measures the UI thread's drawing per frame (no
// UpdateLayeredWindow): a turning arrow with its label (sprites cached),
// a pressed one, and the first frames of a new look (the master image).
func BenchmarkCursorFrame(b *testing.B) {
	t := &testing.T{}
	w := sheetWin(t, cursorAccent)
	look := w.lookAt(fpoint{X: 500, Y: 400}, "", `click "Save"`, Rect{Right: 1920, Bottom: 1080}, 1)
	w.frame(look, 1, 1, 0, 1)
	for k := -60; k <= 60; k++ {
		w.sprite(spriteKey{step: k})
	}
	b.Run("turning", func(b *testing.B) {
		for i := range b.N {
			w.composeArrow(spriteKey{step: i%60 - 30})
		}
	})
	b.Run("pressed", func(b *testing.B) {
		for i := range b.N {
			w.composeArrow(spriteFor(0, 0.93+0.1*float64(i%100)/100))
		}
	})
	b.Run("label reveal", func(b *testing.B) {
		for i := range b.N {
			w.composeCard(float64(i%100)/100, 0.94+0.06*float64(i%100)/100)
			w.composeArrow(spriteKey{})
		}
	})
	b.Run("new sprite", func(b *testing.B) {
		for i := range b.N {
			delete(w.sprites, spriteKey{step: 7})
			_ = i
			w.sprite(spriteKey{step: 7})
		}
	})
	b.Run("master at 100 %", func(b *testing.B) {
		for range b.N {
			newArrowMaster(1, cursorAccent)
		}
	})
	b.Run("outline pop band", func(b *testing.B) {
		s := &surface{w: 220, h: 120, pix: make([]uint32, 220*120)}
		tgt := Rect{Left: 60, Top: 40, Right: 160, Bottom: 80}
		s.drawOutline(tgt, 1, 1.08, cursorAccent)
		for i := range b.N {
			from := 1 + 0.08*float64(i%50)/50
			s.popOutline(tgt, 1, from, from-0.0016, cursorAccent)
		}
	})
}

// referencesStrip pastes the presence-cursor references kept in refsDir
// (.scratch/cursor-look/refs, Mobbin crops that stay in .scratch) under a
// heading at y, each scaled to one height, wrapped to the sheet's width,
// and returns the y below them. Nothing when the folder is missing.
func referencesStrip(t *testing.T, img *image.RGBA, cw *cursorWin, refsDir string, y int) int {
	paths, _ := filepath.Glob(filepath.Join(refsDir, "*.png"))
	if len(paths) == 0 {
		return y
	}
	sort.Strings(paths)
	caption(img, cw, "References, for comparison (Figma and FigJam presence tags, via Mobbin: refs/README.md; kept in .scratch only)",
		image.Rect(0, y, img.Rect.Dx(), y+30), 1.2)
	y += 36
	const h = 150
	x := 16
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		src, err := png.Decode(f)
		f.Close()
		if err != nil {
			t.Logf("reference %s: %v", p, err)
			continue
		}
		b := src.Bounds()
		w := b.Dx() * h / b.Dy()
		if x+w > img.Rect.Dx()-16 {
			x, y = 16, y+h+8
		}
		// Bilinear enough: average the source pixels under each target one.
		for j := 0; j < h; j++ {
			for i := 0; i < w; i++ {
				sx0, sx1 := b.Min.X+i*b.Dx()/w, b.Min.X+(i+1)*b.Dx()/w
				sy0, sy1 := b.Min.Y+j*b.Dy()/h, b.Min.Y+(j+1)*b.Dy()/h
				var r, g, bl, n uint32
				for sy := sy0; sy < max(sy1, sy0+1); sy++ {
					for sx := sx0; sx < max(sx1, sx0+1); sx++ {
						cr, cg, cb, _ := src.At(sx, sy).RGBA()
						r, g, bl, n = r+cr>>8, g+cg>>8, bl+cb>>8, n+1
					}
				}
				img.SetRGBA(x+i, y+j, color.RGBA{R: uint8(r / n), G: uint8(g / n), B: uint8(bl / n), A: 0xFF})
			}
		}
		x += w + 8
	}
	return y + h + 12
}
