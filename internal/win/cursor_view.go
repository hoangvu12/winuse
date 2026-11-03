package win

// The Agent Cursor's windows (headless ticket 03): three per-pixel-alpha
// layered windows on the UI thread — the dart with its label pill, the
// click ripple, and the target's highlight — each the size of what it
// shows and updated with UpdateLayeredWindow from a 32bpp top-down
// premultiplied DIB section. They are WS_EX_LAYERED|TRANSPARENT|
// NOACTIVATE|TOOLWINDOW popups, not topmost but placed just above the
// Window the Action targets (cursor_zorder.go), shown with SW_SHOWNOACTIVATE and
// answering WM_NCHITTEST with HTTRANSPARENT: never activated, never in
// the way of a click. WDA_EXCLUDEFROMCAPTURE keeps them out of other
// software's captures where supported, best effort like the banner;
// winuse's own captures hide them through HideWhile.
//
// The arrow window is rendered only when its look (label, scale, pill
// side) changes and merely moved while the cursor travels: the pill and
// the dart are rendered into two kept buffers, and the window's pixels
// are the pill dimmed by its reveal (and scaled by its pop) with the dart
// over it, recomposed only while the reveal plays. The idle fade and the
// outline's fade use UpdateLayeredWindow's constant alpha, so the outline
// (possibly large) is rendered once per click, plus a band per frame
// while its pop runs. The ripple window is small and re-rendered per
// frame. Frames come from a SetTimer timer on the arrow window,
// dispatched to cursorWndProc by the UI thread's message loop.
//
// The label's text follows vercel-labs/agent-desktop at HEAD 9eb9b29
// (Apache-2.0; see third_party/agent-desktop/LICENSE),
// crates/windows/src/system/cursor_overlay/text.rs, modified: semibold,
// measured and drawn in the same font, centred in the pill, GDI drawing
// only the text — in Segoe UI Variable Text Semibold where installed,
// else Segoe UI Semibold, asked for by face name for a true semibold. We
// draw it with ClearType white onto black and fold the subpixels into
// grayscale coverage (cursor_raster.go putText), which reads smoother
// than GDI's grayscale antialiasing; the pill/dart split for the reveal
// and the pops follows their fade.rs and reveal.rs and the macOS
// bridge's ADShowBubble and ADHighlightShow
// (crates/macos/src/system/cursor_overlay/cursor_overlay_chrome_bridge.m).
//
// Every function here runs on the UI thread. The paint code is exercised
// only by the opt-in visual demo; the offline tests cover the pure parts
// (cursor_motion.go, cursor_layout.go, cursor_raster.go) and the
// controller with a fake view.

import (
	"errors"
	"fmt"
	"image/color"
	"math"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shcore = windows.NewLazySystemDLL("shcore.dll")
	dwmapi = windows.NewLazySystemDLL("dwmapi.dll")

	procUpdateLayeredWindow   = user32.NewProc("UpdateLayeredWindow")
	procDestroyWindow         = user32.NewProc("DestroyWindow")
	procSetTimer              = user32.NewProc("SetTimer")
	procKillTimer             = user32.NewProc("KillTimer")
	procMonitorFromPoint      = user32.NewProc("MonitorFromPoint")
	procGetMonitorInfoW       = user32.NewProc("GetMonitorInfoW")
	procCreateDIBSection      = gdi32.NewProc("CreateDIBSection")
	procGdiFlush              = gdi32.NewProc("GdiFlush")
	procGetTextExtentPoint32W = gdi32.NewProc("GetTextExtentPoint32W")
	procGetTextFaceW          = gdi32.NewProc("GetTextFaceW")
	procGetDpiForMonitor      = shcore.NewProc("GetDpiForMonitor")
	procDwmFlush              = dwmapi.NewProc("DwmFlush")
)

// Win32 constants of the cursor windows.
const (
	wmTimer          = 0x0113
	wmMouseActivate  = 0x0021
	maNoActivate     = 3
	swShowNoActivate = 4
	swpNoMove        = 0x0002
	swpNoSize        = 0x0001

	ulwAlpha   = 0x2
	acSrcOver  = 0x0
	acSrcAlpha = 0x1

	monitorDefaultToNearest = 2
	mdtEffectiveDPI         = 0

	antialiasedQuality      = 4
	cleartypeNaturalQuality = 6
	cursorLabelQuality      = cleartypeNaturalQuality // folded to grayscale by putText
	dtNoPrefix              = 0x0800

	cursorTimerID = 1
	// cursorMaxOutlinePixels skips an outline that would need an absurd
	// window (a multi-monitor wall of 8K screens and more).
	cursorMaxOutlinePixels = 64 << 20
)

// cursorTicks maps the arrow window to its controller's tick: WM_TIMER
// reaches the shared window procedure, which looks the callback up here.
var cursorTicks sync.Map // uintptr → func()

// cursorWndProc is the cursor windows' procedure (one callback for the
// process; NewCallback slots are never freed).
var cursorWndProc = syscall.NewCallback(func(hwnd, message, wp, lp uintptr) uintptr {
	switch message {
	case wmNCHitTest:
		return htTransparent
	case wmMouseActivate:
		return maNoActivate
	case wmTimer:
		if f, ok := cursorTicks.Load(hwnd); ok {
			f.(func())()
		}
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, message, wp, lp)
	return r
})

// bitmapInfo is BITMAPINFO for a 32bpp BI_RGB DIB (the colour table is
// unused but part of the struct).
type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

// sizeL is the Win32 SIZE.
type sizeL struct{ CX, CY int32 }

// blendFunction is the Win32 BLENDFUNCTION.
type blendFunction struct {
	BlendOp, BlendFlags, SourceConstantAlpha, AlphaFormat byte
}

// monitorInfo is the Win32 MONITORINFO (40 bytes).
type monitorInfo struct {
	Size    uint32
	Monitor Rect
	Work    Rect
	Flags   uint32
}

var _ [40]byte = [unsafe.Sizeof(monitorInfo{})]byte{}

// layeredWin is one per-pixel-alpha window and its DIB section.
type layeredWin struct {
	hwnd    uintptr
	dc      uintptr // memory DC holding the DIB section
	bmp     uintptr
	origBmp uintptr // the DC's original bitmap, restored before deletion
	pix     surface // the DIB section's bits
	shown   bool
}

// newLayeredWin creates one hidden cursor window of class cls.
func newLayeredWin(hMod uintptr, cls *uint16) (*layeredWin, error) {
	hwnd, _, err := procCreateWindowExW.Call(
		uintptr(wsExLayered|wsExTransparent|wsExToolWindow|wsExNoActivate),
		uintptr(unsafe.Pointer(cls)), 0, uintptr(wsPopup),
		0, 0, 1, 1, 0, 0, hMod, 0)
	if hwnd == 0 {
		return nil, fmt.Errorf("creating an Agent Cursor window: %w", err)
	}
	dc, _, err := procCreateCompatibleDC.Call(0)
	if dc == 0 {
		procDestroyWindow.Call(hwnd)
		return nil, fmt.Errorf("creating the Agent Cursor's memory DC: %w", err)
	}
	// Best-effort exclusion from other software's captures (Win10 2004+),
	// as for the banner; winuse's own captures use HideWhile.
	if cursorExcludeFromCapture {
		procSetWindowDisplayAffinity.Call(hwnd, wdaExcludeFromCapture)
	}
	return &layeredWin{hwnd: hwnd, dc: dc}, nil
}

// resize makes the DIB section w×h (a no-op when it already is) and
// returns false when it cannot.
func (l *layeredWin) resize(w, h int32) bool {
	if w <= 0 || h <= 0 {
		return false
	}
	if l.bmp != 0 && int(w) == l.pix.w && int(h) == l.pix.h {
		return true
	}
	bi := bitmapInfo{Header: bitmapInfoHeader{
		Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:       w,
		Height:      -h, // top-down
		Planes:      1,
		BitCount:    32,
		Compression: biRGB,
	}}
	var bits unsafe.Pointer
	bmp, _, _ := procCreateDIBSection.Call(l.dc, uintptr(unsafe.Pointer(&bi)), dibRGBColors,
		uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == nil {
		return false
	}
	prev, _, _ := procSelectObject.Call(l.dc, bmp)
	if l.bmp == 0 {
		l.origBmp = prev
	} else {
		procDeleteObject.Call(l.bmp)
	}
	l.bmp = bmp
	l.pix = surface{w: int(w), h: int(h), pix: unsafe.Slice((*uint32)(bits), int(w)*int(h))}
	return true
}

// present puts the DIB section on screen at (x, y) with the given
// constant opacity, showing the window (never activating it) if hidden.
func (l *layeredWin) present(x, y int32, opacity float64) {
	if l.bmp == 0 {
		return
	}
	procGdiFlush.Call()
	dst := point{X: x, Y: y}
	size := sizeL{CX: int32(l.pix.w), CY: int32(l.pix.h)}
	var src point
	blend := blendFunction{
		BlendOp:             acSrcOver,
		SourceConstantAlpha: byte(math.Round(clamp01(opacity) * 255)),
		AlphaFormat:         acSrcAlpha,
	}
	r, _, _ := procUpdateLayeredWindow.Call(l.hwnd, 0,
		uintptr(unsafe.Pointer(&dst)), uintptr(unsafe.Pointer(&size)),
		l.dc, uintptr(unsafe.Pointer(&src)), 0, uintptr(unsafe.Pointer(&blend)), ulwAlpha)
	if r != 0 && !l.shown {
		procShowWindow.Call(l.hwnd, swShowNoActivate)
		l.shown = true
	}
}

// hide hides the window; it reports whether it was shown.
func (l *layeredWin) hide() bool {
	if !l.shown {
		return false
	}
	procShowWindow.Call(l.hwnd, swHide)
	l.shown = false
	return true
}

// destroy destroys the window and frees its GDI objects.
func (l *layeredWin) destroy() {
	procDestroyWindow.Call(l.hwnd)
	if l.bmp != 0 {
		procSelectObject.Call(l.dc, l.origBmp)
		procDeleteObject.Call(l.bmp)
		l.bmp = 0
	}
	procDeleteDC.Call(l.dc)
	l.pix = surface{}
}

// cursorWin is the Win32 cursorView.
type cursorWin struct {
	arrow, ripple, outline *layeredWin
	caret                  *layeredWin // the insertion caret (cursor_caret.go)

	accent color.RGBA // the dart's fill, the ripple and the outline: this Agent's colour

	look      cursorLook
	lookOK    bool
	layout    cursorWindowLayout
	card      []uint32 // the look's pill, shadow and text alone
	popped    []uint32 // the pill scaled for its pop
	glyph     []uint32 // the look's dart alone
	cardOp    float64  // the pill's opacity in the window's pixels; <0: none yet
	cardPop   float64  // and its scale
	outTarget Rect
	outScale  float64
	outPop    float64 // the scale the outline's pixels are drawn at
	outPos    Rect    // the outline window's screen rectangle; empty: none
	fonts     map[int32]uintptr
	face      string        // the label's font face; "" until chosen
	interval  time.Duration // the armed timer's period; 0: none
	hidden    bool          // something was hidden since the last composed
}

// newCursorWin registers the class and creates the three hidden windows,
// drawn in accent. Runs on the UI thread.
func newCursorWin(accent color.RGBA) (*cursorWin, error) {
	hMod, _, _ := procGetModuleHandleW.Call(0)
	cls, _ := windows.UTF16PtrFromString(AgentCursorClassName)
	wc := wndclassExW{
		Size:      uint32(unsafe.Sizeof(wndclassExW{})),
		WndProc:   cursorWndProc,
		Instance:  hMod,
		ClassName: cls,
	}
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 &&
		!errors.Is(err, windows.ERROR_CLASS_ALREADY_EXISTS) {
		return nil, fmt.Errorf("registering the Agent Cursor window class: %w", err)
	}
	w := &cursorWin{accent: accent, fonts: map[int32]uintptr{}}
	var made []*layeredWin
	for _, slot := range []**layeredWin{&w.outline, &w.ripple, &w.caret, &w.arrow} {
		l, err := newLayeredWin(hMod, cls)
		if err != nil {
			for _, m := range made {
				m.destroy()
			}
			return nil, err
		}
		*slot = l
		made = append(made, l)
	}
	return w, nil
}

// setTick installs the controller's timer callback.
func (w *cursorWin) setTick(f func()) {
	cursorTicks.Store(w.arrow.hwnd, f)
}

func (w *cursorWin) monitorAt(p fpoint) (Rect, uint32) {
	x, y := roundPoint(p)
	// MonitorFromPoint takes POINT by value: 8 bytes, one register on
	// amd64 and arm64 (the only targets; see wndclassExW).
	pt := uintptr(uint32(x)) | uintptr(uint32(y))<<32
	mon, _, _ := procMonitorFromPoint.Call(pt, monitorDefaultToNearest)
	work := VirtualScreenRect()
	var dpi uint32 = 96
	if mon == 0 {
		return work, dpi
	}
	mi := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
	if r, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); r != 0 {
		work = mi.Work
	}
	if procGetDpiForMonitor.Find() == nil {
		var dx, dy uint32
		if hr, _, _ := procGetDpiForMonitor.Call(mon, mdtEffectiveDPI,
			uintptr(unsafe.Pointer(&dx)), uintptr(unsafe.Pointer(&dy))); hr == 0 && dx != 0 {
			dpi = dx
		}
	}
	return work, dpi
}

// createLabelFont creates the label font face at pixel height h, at
// FW_SEMIBOLD (which these semibold faces are).
func createLabelFont(face string, h int32) uintptr {
	const (
		defaultCharset = 1
		fwSemibold     = 600
	)
	name, err := windows.UTF16PtrFromString(face)
	if err != nil {
		return 0
	}
	f, _, _ := procCreateFontW.Call(
		uintptr(int32ToU32(-h)), 0, 0, 0, fwSemibold, 0, 0, 0,
		defaultCharset, 0, 0, cursorLabelQuality, 0, uintptr(unsafe.Pointer(name)))
	return f
}

// selectedFace is the face GDI actually selected for font f: what
// CreateFontW silently substitutes for a face that is not installed.
func (w *cursorWin) selectedFace(f uintptr) string {
	dc := w.arrow.dc
	old, _, _ := procSelectObject.Call(dc, f)
	defer procSelectObject.Call(dc, old)
	buf := make([]uint16, 64)
	if n, _, _ := procGetTextFaceW.Call(dc, uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0]))); n == 0 {
		return ""
	}
	return windows.UTF16ToString(buf)
}

// labelFace chooses the label's face once: the first of cursorLabelFaces
// GDI really provides.
func (w *cursorWin) labelFace() string {
	if w.face == "" {
		w.face = chooseLabelFace(cursorLabelFaces, func(face string) string {
			f := createLabelFont(face, 13)
			if f == 0 {
				return ""
			}
			defer procDeleteObject.Call(f)
			return w.selectedFace(f)
		})
	}
	return w.face
}

// font returns the label font at scale (cached per pixel height).
func (w *cursorWin) font(scale float64) uintptr {
	h := scaled(cursorPillFontPx, scale)
	if f, ok := w.fonts[h]; ok {
		return f
	}
	f := createLabelFont(w.labelFace(), h)
	if f != 0 {
		w.fonts[h] = f
	}
	return f
}

func (w *cursorWin) measureLabel(text string, scale float64) int32 {
	u, err := windows.UTF16FromString(text)
	if err != nil || len(u) <= 1 {
		return 0
	}
	f := w.font(scale)
	if f == 0 {
		return 0
	}
	dc := w.arrow.dc
	old, _, _ := procSelectObject.Call(dc, f)
	defer procSelectObject.Call(dc, old)
	var sz sizeL
	procGetTextExtentPoint32W.Call(dc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&sz)))
	return sz.CX
}

func (w *cursorWin) showCursor(tip fpoint, opacity, labelOp, labelPop float64, look cursorLook) {
	if !w.lookOK || look != w.look {
		w.lookOK = w.renderArrow(look)
		w.look = look
		w.cardOp = -1
	}
	if !w.lookOK {
		return
	}
	if labelOp != w.cardOp || labelPop != w.cardPop {
		w.composeArrow(labelOp, labelPop)
		w.cardOp, w.cardPop = labelOp, labelPop
	}
	tx, ty := roundPoint(tip)
	w.arrow.present(tx+w.layout.OriginX, ty+w.layout.OriginY, opacity)
}

// renderArrow renders look's pill (with its shadow and text) and its
// dart into the two kept buffers.
func (w *cursorWin) renderArrow(look cursorLook) bool {
	w.layout = layoutCursorWindow(look)
	if !w.arrow.resize(w.layout.W, w.layout.H) {
		return false
	}
	s := &w.arrow.pix
	s.clear()
	if w.layout.Pill.Width() > 0 {
		s.drawPill(w.layout.Pill, look.Scale)
		w.drawLabel(look.Label, w.layout.Text, look.Scale)
	}
	w.card = append(w.card[:0], s.pix...)
	s.clear()
	s.drawDart(w.layout.Tip, look.Scale, w.accent, cursorRimColor)
	w.glyph = append(w.glyph[:0], s.pix...)
	return true
}

// composeArrow puts the window's pixels together: the pill at labelOp,
// scaled by labelPop about popAnchor (only the pill's box is resampled,
// and only while the pop runs), the dart over it.
func (w *cursorWin) composeArrow(labelOp, labelPop float64) {
	s := &w.arrow.pix
	if len(w.card) != len(s.pix) || len(w.glyph) != len(s.pix) {
		return
	}
	card := w.card
	if labelPop < 1 && labelOp > 0 && w.layout.Pill.Width() > 0 {
		if len(w.popped) != len(s.pix) {
			w.popped = make([]uint32, len(s.pix))
		}
		clear(w.popped)
		a := popAnchor(w.layout.Pill, w.look.Scale)
		// The pill shrinks toward the anchor, so it stays between its own
		// box (with the shadow) and the anchor.
		box := unionRect(pillShadowBounds(w.layout.Pill, w.look.Scale), Rect{
			Left: int32(math.Floor(a.X)), Top: int32(math.Floor(a.Y)),
			Right: int32(math.Ceil(a.X)) + 1, Bottom: int32(math.Ceil(a.Y)) + 1,
		})
		scalePixels(w.popped, w.card, s.w, s.h, a, labelPop, box)
		card = w.popped
	}
	for i := range s.pix {
		s.pix[i] = over(dimPixel(card[i], labelOp), w.glyph[i])
	}
}

// drawLabel draws text into r of the arrow window's DIB, over the pill
// drawn there: GDI draws it white onto black (flushed so the bits are
// final before they are read), and putText composites it over the pill
// in the text colour at the coverage GDI left. The text is centred, as
// their text.rs sets it.
func (w *cursorWin) drawLabel(text string, r Rect, scale float64) {
	u, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	f := w.font(scale)
	if f == 0 {
		return
	}
	s := &w.arrow.pix
	saved := s.takeText(r)
	if saved == nil {
		return
	}
	dc := w.arrow.dc
	old, _, _ := procSelectObject.Call(dc, f)
	defer procSelectObject.Call(dc, old)
	procSetBkMode.Call(dc, bkModeTransparent)
	procSetTextColor.Call(dc, uintptr(overlayTextColor)) // white, onto takeText's black
	drawText(dc, u, &r, dtCenter|dtVCenter|dtSingleLine|dtEndEllipsis|dtNoPrefix)
	procGdiFlush.Call()
	s.putText(r, saved, cursorTextColor)
}

func (w *cursorWin) hideCursor() {
	if w.arrow.hide() {
		w.hidden = true
	}
}

func (w *cursorWin) showRipple(at fpoint, phase, scale float64) {
	n := rippleWindowSize(scale)
	if !w.ripple.resize(n, n) {
		return
	}
	cx, cy := roundPoint(at)
	x, y := cx-n/2, cy-n/2
	s := &w.ripple.pix
	s.clear()
	s.drawRipple(fpoint{X: at.X - float64(x), Y: at.Y - float64(y)}, phase, scale, w.accent)
	w.ripple.present(x, y, 1)
}

func (w *cursorWin) hideRipple() {
	if w.ripple.hide() {
		w.hidden = true
	}
}

// showOutline shows the highlight around target at opacity, scaled by pop.
// The window is sized for the largest pop once per click; while the pop
// runs, each frame redraws only the band the outline moved through
// (popOutline), and once it settles the pixels stay.
func (w *cursorWin) showOutline(target Rect, opacity, pop, scale float64) {
	local := func() Rect {
		return Rect{
			Left: target.Left - w.outPos.Left, Top: target.Top - w.outPos.Top,
			Right: target.Right - w.outPos.Left, Bottom: target.Bottom - w.outPos.Top,
		}
	}
	if target != w.outTarget || scale != w.outScale {
		w.outTarget, w.outScale = target, scale
		w.outPos = outlineWindowRect(target, VirtualScreenRect(), scale)
		if int64(w.outPos.Width())*int64(w.outPos.Height()) > cursorMaxOutlinePixels ||
			!w.outline.resize(w.outPos.Width(), w.outPos.Height()) {
			w.outPos = Rect{}
		} else {
			s := &w.outline.pix
			s.clear()
			s.drawOutline(local(), scale, pop, w.accent)
			w.outPop = pop
		}
	} else if w.outPos.Width() > 0 && pop != w.outPop {
		w.outline.pix.popOutline(local(), scale, w.outPop, pop, w.accent)
		w.outPop = pop
	}
	if w.outPos.Width() <= 0 {
		w.hideOutline()
		return
	}
	w.outline.present(w.outPos.Left, w.outPos.Top, opacity)
}

func (w *cursorWin) hideOutline() {
	if w.outline.hide() {
		w.hidden = true
	}
}

// composed waits for the desktop compositor's next frame after a hide,
// so a capture taken right after HideWhile's hide cannot still see the
// windows. Best effort: DwmFlush fails harmlessly when composition is
// unavailable.
func (w *cursorWin) composed() {
	if !w.hidden {
		return
	}
	w.hidden = false
	if procDwmFlush.Find() == nil {
		procDwmFlush.Call()
	}
}

// raise puts the windows just above anchor (cursor_zorder.go), the arrow
// on top of its own ripple and outline.
func (w *cursorWin) raise(anchor uint64) {
	placeAbove(uintptr(anchor), w.outline, w.ripple, w.caret, w.arrow)
}

// showCaret draws the caret's bar, a fully rounded rectangle in the
// Agent's colour, the window the bar's size.
func (w *cursorWin) showCaret(bar Rect, opacity float64) {
	if bar.Width() <= 0 || bar.Height() <= 0 || !w.caret.resize(bar.Width(), bar.Height()) {
		w.hideCaret()
		return
	}
	s := &w.caret.pix
	s.clear()
	bw, bh := float64(bar.Width()), float64(bar.Height())
	s.fillSDF(Rect{Right: bar.Width(), Bottom: bar.Height()}, w.accent, 1, func(x, y float64) float64 {
		return roundRectSD(0, 0, bw, bh, bw/2, x, y)
	})
	w.caret.present(bar.Left, bar.Top, opacity)
}

func (w *cursorWin) hideCaret() {
	if w.caret.hide() {
		w.hidden = true
	}
}

func (w *cursorWin) schedule(d time.Duration) {
	d = max(d, cursorFrameInterval)
	if d == w.interval {
		return // already armed with this period
	}
	ms := uintptr(d / time.Millisecond)
	if r, _, _ := procSetTimer.Call(w.arrow.hwnd, cursorTimerID, ms, 0); r != 0 {
		w.interval = d
	}
}

func (w *cursorWin) cancel() {
	if w.interval != 0 {
		procKillTimer.Call(w.arrow.hwnd, cursorTimerID)
		w.interval = 0
	}
}

func (w *cursorWin) destroy() {
	w.cancel()
	cursorTicks.Delete(w.arrow.hwnd)
	for _, l := range []*layeredWin{w.arrow, w.ripple, w.outline, w.caret} {
		l.destroy()
	}
	for h, f := range w.fonts {
		procDeleteObject.Call(f)
		delete(w.fonts, h)
	}
}
