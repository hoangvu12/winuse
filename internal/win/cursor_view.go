package win

// The Agent Cursor's windows (headless ticket 03, cursor-look): four
// per-pixel-alpha layered windows on the UI thread — the arrow with its
// name tag, the optional trail, the target's highlight and the
// insertion caret — each the size of what it
// shows and updated with UpdateLayeredWindow from a 32bpp top-down
// premultiplied DIB section. They are WS_EX_LAYERED|TRANSPARENT|
// NOACTIVATE|TOOLWINDOW popups, not topmost but placed just above the
// Window the Action targets (cursor_zorder.go), shown with SW_SHOWNOACTIVATE and
// answering WM_NCHITTEST with HTTRANSPARENT: never activated, never in
// the way of a click. WDA_EXCLUDEFROMCAPTURE keeps them out of other
// software's captures where supported, best effort like the banner;
// winuse's own captures hide them through HideWhile.
//
// The arrow window's tag is rendered only when its look (name, scale,
// tag side) changes. The arrow itself is drawn exactly once per Agent and
// DPI, finer than the screen (newArrowMaster); every turned (each 3°) or
// pressed (each 0.005) arrow is a sprite sampled from it once and kept, so
// a frame is the tag copied in with a kept sprite over it — or, when
// neither the turn nor the press changed, the window is merely moved (the
// travel, the float). The idle fade and the outline's
// fade use UpdateLayeredWindow's constant alpha, so the outline (possibly
// large) is rendered once per click, plus a band per step of its pop. A
// window whose place, opacity and pixels are unchanged is not presented
// again. While anything animates, frames come at about 120 per second from
// a frame clock posting to the arrow window, with the timer resolution
// raised; otherwise from a SetTimer timer; both are dispatched to
// cursorWndProc by the UI thread's message loop.
//
// The tag's text follows vercel-labs/agent-desktop at HEAD 9eb9b29
// (Apache-2.0; see third_party/agent-desktop/LICENSE),
// crates/windows/src/system/cursor_overlay/text.rs, modified: bold,
// measured and drawn in the same font, GDI drawing only the text — in the
// embedded Figtree Bold (cursor_font.go), else Segoe UI Bold. We draw it
// with ClearType white onto black and fold the subpixels into grayscale
// coverage (cursor_raster.go putText), which reads smoother than GDI's
// grayscale antialiasing; the outline's pop follows the macOS bridge's
// ADHighlightShow
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
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shcore = windows.NewLazySystemDLL("shcore.dll")
	dwmapi = windows.NewLazySystemDLL("dwmapi.dll")
	winmm  = windows.NewLazySystemDLL("winmm.dll")

	procTimeBeginPeriod       = winmm.NewProc("timeBeginPeriod")
	procTimeEndPeriod         = winmm.NewProc("timeEndPeriod")
	procSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")
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
	dtLeft                  = 0x0000

	cursorTimerID = 1
	// wmCursorFrame is the frame clock's message to the arrow window
	// (WM_APP + 1; the UI thread's own wake is WM_APP, a thread message).
	wmCursorFrame = wmApp + 1
	// cursorMaxOutlinePixels skips an outline that would need an absurd
	// window (a multi-monitor wall of 8K screens and more).
	cursorMaxOutlinePixels = 64 << 20

	spiGetClientAreaAnimation = 0x1042
	// cursorSpriteStep is the arrow sprites' angle step: a turned arrow is
	// drawn at the nearest multiple of 3°, each made once and kept.
	cursorSpriteStep = 3 * math.Pi / 180
)

// reducedMotion reports whether Windows' "Animation effects" are off
// (Settings > Accessibility > Visual effects; SPI_GETCLIENTAREAANIMATION).
// Read per travel and click, so a change applies at once. Unknown is on.
func reducedMotion() bool {
	var on int32 = 1
	if r, _, _ := procSystemParametersInfoW.Call(spiGetClientAreaAnimation, 0, uintptr(unsafe.Pointer(&on)), 0); r == 0 {
		return false
	}
	return on == 0
}

// fastFrames counts the cursors animating at the frame rate. The first
// raises the timer resolution to 1 ms (SetTimer then fires at about the
// 8 ms asked for) and the last gives it back, as cua's overlay does, so a
// resting or hidden cursor costs nothing. Touched only on the UI thread.
var fastFrames int

func holdFastFrames(on bool, held *bool) {
	if on == *held {
		return
	}
	*held = on
	if on {
		if fastFrames++; fastFrames == 1 && procTimeBeginPeriod.Find() == nil {
			procTimeBeginPeriod.Call(1)
		}
		return
	}
	if fastFrames--; fastFrames == 0 && procTimeEndPeriod.Find() == nil {
		procTimeEndPeriod.Call(1)
	}
}

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
	case wmTimer, wmCursorFrame:
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
	// What the screen shows: present skips UpdateLayeredWindow when
	// neither the place, the opacity nor the pixels (dirty) changed.
	atX, atY int32
	atOp     float64
	dirty    bool
}

// touch records that the pixels changed since the last present.
func (l *layeredWin) touch() { l.dirty = true }

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
	l.dirty = true
	return true
}

// present puts the DIB section on screen at (x, y) with the given
// constant opacity, showing the window (never activating it) if hidden.
func (l *layeredWin) present(x, y int32, opacity float64) {
	if l.bmp == 0 {
		return
	}
	if l.shown && !l.dirty && x == l.atX && y == l.atY && opacity == l.atOp {
		return // nothing new to show
	}
	l.atX, l.atY, l.atOp, l.dirty = x, y, opacity, false
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
	arrow, trail, outline *layeredWin
	caret                 *layeredWin // the insertion caret (cursor_caret.go)

	accent color.RGBA // the arrow's fill, the outline and the caret: this Agent's colour

	look    cursorLook
	lookOK  bool
	layout  cursorWindowLayout
	base    []uint32  // the look's tag, shadow and text alone: the window less the arrow
	shown   spriteKey // the arrow in the window's pixels
	shownOK bool      // shown is valid

	// The arrow's master image and its turned and pressed sprites
	// (arrowReach-sized squares, the tip at their centre), kept per scale.
	master      arrowMaster
	masterScale float64
	sprites     map[spriteKey][]uint32
	spriteR     int32 // the sprites' reach: side 2·spriteR+1

	trailAlpha []float64 // drawTrail's scratch
	fast       bool      // this cursor holds the raised timer resolution
	// The frame clock: while the cursor animates, a goroutine posts
	// wmCursorFrame every cursorFrameInterval; posted coalesces them, so a
	// busy UI thread is never handed a backlog of frames.
	clockStop chan struct{}
	posted    atomic.Bool

	outTarget Rect
	outScale  float64
	outPop    float64 // the scale the outline's pixels are drawn at
	outPos    Rect    // the outline window's screen rectangle; empty: none
	fonts     map[int32]uintptr
	face      string        // the tag's font face; "" until chosen
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
	for _, slot := range []**layeredWin{&w.outline, &w.trail, &w.caret, &w.arrow} {
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
	cursorTicks.Store(w.arrow.hwnd, func() {
		w.posted.Store(false)
		f()
	})
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

// createLabelFont creates the tag's font face at pixel height h, bold.
func createLabelFont(face string, h int32) uintptr {
	const (
		defaultCharset = 1
		fwBold         = 700
	)
	name, err := windows.UTF16PtrFromString(face)
	if err != nil {
		return 0
	}
	f, _, _ := procCreateFontW.Call(
		uintptr(int32ToU32(-h)), 0, 0, 0, fwBold, 0, 0, 0,
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

// labelFace chooses the tag's face once, loading the embedded Figtree
// first: the first of cursorLabelFaces GDI really provides.
func (w *cursorWin) labelFace() string {
	if w.face == "" {
		loadCursorFont()
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

// font returns the tag's face at px logical pixels at scale (cached per
// pixel height).
func (w *cursorWin) font(px, scale float64) uintptr {
	h := scaled(px, scale)
	if f, ok := w.fonts[h]; ok {
		return f
	}
	f := createLabelFont(w.labelFace(), h)
	if f != 0 {
		w.fonts[h] = f
	}
	return f
}

func (w *cursorWin) measureLabel(text string, px, scale float64) int32 {
	u, err := windows.UTF16FromString(text)
	if err != nil || len(u) <= 1 {
		return 0
	}
	f := w.font(px, scale)
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

func (w *cursorWin) showCursor(tip fpoint, opacity, angle, press float64, look cursorLook) {
	if !w.lookOK || look != w.look {
		w.lookOK = w.renderArrow(look)
		w.look = look
		w.shownOK = false
	}
	if !w.lookOK {
		return
	}
	k := spriteFor(angle, press)
	if !w.shownOK || k != w.shown {
		w.composeArrow(k)
		w.shown, w.shownOK = k, true
	}
	tx, ty := roundPoint(tip)
	w.arrow.present(tx+w.layout.OriginX, ty+w.layout.OriginY, opacity)
}

// renderArrow renders look's tag (with its rim, shadow and name) into
// the kept base, and readies the arrow's master image for its scale.
func (w *cursorWin) renderArrow(look cursorLook) bool {
	w.layout = layoutCursorWindow(look)
	if !w.arrow.resize(w.layout.W, w.layout.H) {
		return false
	}
	s := &w.arrow.pix
	s.clear()
	if w.layout.Pill.Width() > 0 {
		s.drawPill(w.layout.Pill, look.Scale, w.accent)
		w.drawLine(look.Name, w.layout.Text, cursorPillFontPx, look.Scale, pillTextColor(w.accent), dtLeft)
	}
	w.base = append(w.base[:0], s.pix...)
	w.readyMaster(look.Scale)
	return true
}

// readyMaster draws the arrow's master image for scale, dropping the
// sprites of another scale. It is the only exact rasterizing of the
// arrow: once per Agent and monitor DPI.
func (w *cursorWin) readyMaster(scale float64) {
	if w.master.pix != nil && w.masterScale == scale {
		return
	}
	w.master = newArrowMaster(scale, w.accent)
	w.masterScale = scale
	w.spriteR = arrowReach(scale)
	w.sprites = map[spriteKey][]uint32{}
}

// spriteKey names one kept sprite: the arrow turned to step (multiples of
// cursorSpriteStep) and pressed to 1 + press·cursorPressStep.
type spriteKey struct{ step, press int }

// cursorPressStep is the press sprites' scale step: the click's 0.93–1.03
// squeeze is about twenty kept sprites, each made once.
const cursorPressStep = 0.005

// sprite is the arrow for k, made on first use and kept: every frame of a
// turning or pressed arrow copies one.
func (w *cursorWin) sprite(k spriteKey) []uint32 {
	if sp, ok := w.sprites[k]; ok {
		return sp
	}
	n := int(2*w.spriteR) + 1
	s := surface{w: n, h: n, pix: make([]uint32, n*n)}
	c := float64(w.spriteR)
	s.drawArrowSprite(&w.master, fpoint{X: c, Y: c}, float64(k.step)*cursorSpriteStep,
		1+float64(k.press)*cursorPressStep, Rect{Right: int32(n), Bottom: int32(n)})
	w.sprites[k] = s.pix
	return s.pix
}

// spriteFor is the sprite key of an arrow turned by angle and pressed by
// press.
func spriteFor(angle, press float64) spriteKey {
	return spriteKey{
		step:  int(math.Round(angle / cursorSpriteStep)),
		press: int(math.Round((press - 1) / cursorPressStep)),
	}
}

// composeArrow puts the window's pixels together: base (the tag) with
// k's kept sprite copied over it.
func (w *cursorWin) composeArrow(k spriteKey) {
	s := &w.arrow.pix
	if len(w.base) != len(s.pix) {
		return
	}
	copy(s.pix, w.base)
	w.arrow.touch()
	r := w.spriteR
	tip := w.layout.Tip
	ox, oy := int(tip.X)-int(r), int(tip.Y)-int(r)
	n := int(2*r) + 1
	sp := w.sprite(k)
	for y := range n {
		wy := oy + y
		if wy < 0 || wy >= s.h {
			continue
		}
		for x := range n {
			wx := ox + x
			if p := sp[y*n+x]; p != 0 && wx >= 0 && wx < s.w {
				s.pix[wy*s.w+wx] = over(s.pix[wy*s.w+wx], p)
			}
		}
	}
}

// drawLine draws one line of text into r of the arrow window's DIB, over
// the tag drawn there, at px logical pixels in c, aligned by align
// (dtCenter or dtLeft): GDI draws it white onto black (flushed so the
// bits are final before they are read), and putText composites it over
// the tag in c at the coverage GDI left.
func (w *cursorWin) drawLine(text string, r Rect, px, scale float64, c color.RGBA, align uint32) {
	u, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	f := w.font(px, scale)
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
	drawText(dc, u, &r, align|dtVCenter|dtSingleLine|dtEndEllipsis|dtNoPrefix)
	procGdiFlush.Call()
	s.putText(r, saved, c)
}

func (w *cursorWin) hideCursor() {
	if w.arrow.hide() {
		w.hidden = true
	}
}

// showTrail draws the comet trail pts (screen pixels) into a window just
// around them, at opacity.
func (w *cursorWin) showTrail(pts []fpoint, opacity, scale float64) {
	r := trailWindowRect(pts, scale)
	if r.Width() <= 0 || r.Height() <= 0 || !w.trail.resize(r.Width(), r.Height()) {
		w.hideTrail()
		return
	}
	local := make([]fpoint, len(pts))
	for i, p := range pts {
		local[i] = fpoint{X: p.X - float64(r.Left), Y: p.Y - float64(r.Top)}
	}
	s := &w.trail.pix
	s.clear()
	w.trailAlpha = s.drawTrail(local, scale, w.accent, w.trailAlpha)
	w.trail.touch()
	w.trail.present(r.Left, r.Top, opacity)
}

func (w *cursorWin) hideTrail() {
	if w.trail.hide() {
		w.hidden = true
	}
}

// reducedMotion is the package's reducedMotion (cursorView).
func (w *cursorWin) reducedMotion() bool { return reducedMotion() }

// showOutline shows the highlight around target at opacity, scaled by pop.
// The window is sized for the largest pop once per click; while the pop
// runs, each frame redraws only the band the outline moved through
// (popOutline), and once it settles the pixels stay.
func (w *cursorWin) showOutline(target Rect, opacity, pop, scale float64) {
	// The pop is redrawn in steps of 1/250 (a fraction of a pixel on any
	// element), not every frame: at 120 frames per second a band redrawn
	// each frame cost more than all the rest of the cursor.
	pop = math.Round(pop*250) / 250
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
			w.outline.touch()
			w.outPop = pop
		}
	} else if w.outPos.Width() > 0 && pop != w.outPop {
		w.outline.pix.popOutline(local(), scale, w.outPop, pop, w.accent)
		w.outline.touch()
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
// on top of its own trail, caret and outline.
func (w *cursorWin) raise(anchor uint64) {
	placeAbove(uintptr(anchor), w.outline, w.trail, w.caret, w.arrow)
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
	w.caret.touch() // a few pixels, redrawn each frame
	w.caret.present(bar.Left, bar.Top, opacity)
}

func (w *cursorWin) hideCaret() {
	if w.caret.hide() {
		w.hidden = true
	}
}

// schedule arms the next wake after d. At the frame interval (something
// animates) the frame clock runs and the timer resolution is raised; for
// the slower float and idle wakes the clock stops, the resolution is
// given back and a SetTimer timer waits. SetTimer alone cannot keep
// 120 frames per second: on Windows 11 it fires on the 15.6 ms tick even
// with the resolution raised (64 frames per second measured).
func (w *cursorWin) schedule(d time.Duration) {
	d = max(d, cursorFrameInterval)
	fast := d <= cursorFrameInterval
	holdFastFrames(fast, &w.fast)
	if fast {
		w.killTimer()
		w.startClock()
		return
	}
	w.stopClock()
	if d == w.interval {
		return // already armed with this period
	}
	ms := uintptr(d / time.Millisecond)
	if r, _, _ := procSetTimer.Call(w.arrow.hwnd, cursorTimerID, ms, 0); r != 0 {
		w.interval = d
	}
}

func (w *cursorWin) cancel() {
	holdFastFrames(false, &w.fast)
	w.stopClock()
	w.killTimer()
}

func (w *cursorWin) killTimer() {
	if w.interval != 0 {
		procKillTimer.Call(w.arrow.hwnd, cursorTimerID)
		w.interval = 0
	}
}

// startClock starts the frame clock, if it is not running: Go's timers
// are high-resolution waitable timers on Windows, so it keeps the 8 ms.
func (w *cursorWin) startClock() {
	if w.clockStop != nil {
		return
	}
	stop := make(chan struct{})
	w.clockStop = stop
	hwnd := w.arrow.hwnd
	go func() {
		t := time.NewTicker(cursorFrameInterval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if w.posted.CompareAndSwap(false, true) {
					if r, _, _ := procPostMessageW.Call(hwnd, wmCursorFrame, 0, 0); r == 0 {
						w.posted.Store(false)
					}
				}
			}
		}
	}()
}

func (w *cursorWin) stopClock() {
	if w.clockStop != nil {
		close(w.clockStop)
		w.clockStop = nil
	}
}

func (w *cursorWin) destroy() {
	w.cancel()
	cursorTicks.Delete(w.arrow.hwnd)
	for _, l := range []*layeredWin{w.arrow, w.trail, w.outline, w.caret} {
		l.destroy()
	}
	for h, f := range w.fonts {
		procDeleteObject.Call(f)
		delete(w.fonts, h)
	}
}
