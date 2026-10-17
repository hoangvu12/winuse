package win

// The in-control overlay (ticket 13): a Codex-style on-screen indicator
// raised over the Window an Action is aimed at — a topmost, layered,
// click-through, never-activating popup drawing a 4 px accent border
// around the Window and a pill banner ("winuse is in control — press Esc
// to stop") at its top centre, with GDI against a colour key (win32.md
// §9). Everything (window, state, drawing) lives on the UI thread
// (uithread.go); Show, Hide and HideWhile post jobs to it and wait
// bounded, so an Action never fails because the indicator misbehaved.
//
// Exclusion from capture is two-layered. SetWindowDisplayAffinity
// (WDA_EXCLUDEFROMCAPTURE, Win10 2004+) keeps the indicator out of other
// software's captures where the OS supports it — best effort, and
// deliberately not fallen back to WDA_MONITOR, which would paint a black
// box over captured pixels instead. Exclusion from winuse's own
// screenshots does not rely on it: it is unverified that the affinity
// also hides a layered window from GDI screen-region BitBlt (win32.md
// flags it), and pre-2004 Windows degrades the value to WDA_MONITOR, so
// every capture runs inside HideWhile — hidden, captured, restored.

import (
	"errors"
	"fmt"
	"image/color"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Window styles of the overlay (win32.md §9).
const (
	wsPopup = 0x80000000

	wsExLayered     = 0x00080000
	wsExTransparent = 0x00000020
	wsExTopmost     = 0x00000008
	// wsExToolWindow and wsExNoActivate: shared with the enumeration
	// filter (enum.go).

	lwaColorKey = 0x1
	// wdaExcludeFromCapture keeps a window out of captures entirely
	// (shown on the monitor); pre-2004 Windows degrades it to
	// wdaMonitor's black box, which is why winuse never depends on it.
	wdaExcludeFromCapture = 0x11

	swHide            = 0
	hwndTopmost       = ^uintptr(0)
	swpNoActivate     = 0x0010
	swpShowWindowFlag = 0x0040
)

// Messages the overlay's window procedure handles.
const (
	wmPaint      = 0xF
	wmEraseBkgnd = 0x14
	wmNCHitTest  = 0x84
)

var (
	procSetWindowPos             = user32.NewProc("SetWindowPos")
	procSetLayeredWindowAttrs    = user32.NewProc("SetLayeredWindowAttributes")
	procSetWindowDisplayAffinity = user32.NewProc("SetWindowDisplayAffinity")
	procGetClientRect            = user32.NewProc("GetClientRect")
	procBeginPaint               = user32.NewProc("BeginPaint")
	procEndPaint                 = user32.NewProc("EndPaint")
	procGetDpiForWindow          = user32.NewProc("GetDpiForWindow")
	procFillRect                 = user32.NewProc("FillRect")
	procDrawTextW                = user32.NewProc("DrawTextW")
	procSetTextColor             = gdi32.NewProc("SetTextColor")
	procSetBkMode                = gdi32.NewProc("SetBkMode")
	procRoundRect                = gdi32.NewProc("RoundRect")
	procCreateSolidBrush         = gdi32.NewProc("CreateSolidBrush")
	procCreatePenW               = gdi32.NewProc("CreatePenW")
	procCreateFontW              = gdi32.NewProc("CreateFontW")
)

// OverlayClassName is the overlay window's class name. The e2e tests find
// the indicator on the desktop by it.
const OverlayClassName = "winuse.overlay"

// OverlayAccent is the indicator's accent colour: the border framing the
// controlled Window (and the e2e absence check looks for it in
// screenshots). Teal, like the ChatGPT-family product colour.
var OverlayAccent = color.RGBA{R: 0x10, G: 0xA3, B: 0x7F, A: 0xFF}

// Drawing constants. The colour key (magenta) reads as fully transparent;
// the pill is dark with white text for legibility over any content.
const (
	overlayKeyColor  = 0x00FF00FF // COLORREF magenta: transparent
	overlayPillColor = 0x0024211F // COLORREF near-black: the pill banner
	overlayTextColor = 0x00FFFFFF // COLORREF white: the banner text

	overlayText        = "winuse is in control — press Esc to stop"
	overlayFontHeight  = 13 // Segoe UI height at 96 DPI
	overlayPillTop     = 12 // the pill's top below the overlay's top edge
	overlayPillPadX    = 16 // horizontal padding inside the pill
	overlayPillPadY    = 8  // vertical padding inside the pill
	overlayBorderWidth = 4  // the accent border's thickness
)

// colorref converts a colour to the Win32 COLORREF (0x00BBGGRR).
func colorref(c color.RGBA) uint32 {
	return uint32(c.R) | uint32(c.G)<<8 | uint32(c.B)<<16
}

// Overlay is the in-control indicator window. All of its state lives on
// the UI thread; Show, Hide and HideWhile post jobs there. Create one per
// helper process with NewOverlay and Close it when the helper exits.
type Overlay struct {
	thread *uiThread
	hwnd   uintptr // created on the UI thread before NewOverlay returns

	// shown, paused and rect are owned by the UI thread.
	shown  bool
	paused bool // hidden for a capture; restored after
	rect   Rect
}

// NewOverlay starts the UI thread and creates the hidden overlay window
// on it. The window is positioned off-screen and stays hidden until the
// first Show; an overlay that cannot start returns an error and Actions
// run without it.
func NewOverlay() (*Overlay, error) {
	hMod, _, _ := procGetModuleHandleW.Call(0)
	o := &Overlay{}
	t, err := startUIThread(func() error {
		if err := o.registerClass(hMod); err != nil {
			return err
		}
		return o.createWindow(hMod)
	})
	if err != nil {
		return nil, err
	}
	o.thread = t
	return o, nil
}

// Close stops the overlay's UI thread. Idempotent.
func (o *Overlay) Close() {
	if o.thread != nil {
		o.thread.close()
	}
}

// Show raises the indicator over r (the target Window's extended frame
// bounds): the window is moved there, raised to topmost and shown
// without taking focus.
func (o *Overlay) Show(r Rect) error {
	return o.thread.do(func() {
		o.shown = true
		o.rect = r
		if !o.paused {
			o.apply()
		}
	})
}

// Hide removes the indicator.
func (o *Overlay) Hide() error {
	return o.thread.do(func() {
		o.shown = false
		if !o.paused {
			procShowWindow.Call(o.hwnd, swHide)
		}
	})
}

// Do runs job on the overlay's UI thread — the thread ticket 12 installs
// the Esc low-level keyboard hook on (SetWindowsHookExW must be called
// from the pumping thread, and the hook is invoked on it while it
// pumps). The wait is bounded like every job. Jobs must stay quick and
// must never inject input or touch UI Automation (win32.md §8).
func (o *Overlay) Do(job func()) error {
	return o.thread.do(job)
}

// HideWhile runs f with the indicator hidden, restoring its previous
// visibility afterwards (only if it was visible). winuse's capture
// methods bracket themselves with it so the indicator never appears in
// winuse's own screenshots — the deterministic exclusion, independent of
// display-affinity support.
func (o *Overlay) HideWhile(f func()) {
	_ = o.thread.do(o.pause)
	defer func() { _ = o.thread.do(o.resume) }()
	f()
}

// pause hides a shown indicator for a capture. Runs on the UI thread.
func (o *Overlay) pause() {
	if o.shown && !o.paused {
		o.paused = true
		procShowWindow.Call(o.hwnd, swHide)
	}
}

// resume restores the indicator after a capture. Runs on the UI thread.
func (o *Overlay) resume() {
	if o.paused {
		o.paused = false
		if o.shown {
			o.apply()
		}
	}
}

// apply moves the window to its rect, raises it to topmost and shows it,
// without activation. Runs on the UI thread.
func (o *Overlay) apply() {
	procSetWindowPos.Call(o.hwnd, hwndTopmost,
		uintptr(int32ToU32(o.rect.Left)), uintptr(int32ToU32(o.rect.Top)),
		uintptr(int32ToU32(o.rect.Width())), uintptr(int32ToU32(o.rect.Height())),
		swpNoActivate|swpShowWindowFlag)
}

// registerClass registers the overlay window's class. Runs on the UI
// thread. A repeated registration in one process (never in practice)
// succeeds as "already exists".
func (o *Overlay) registerClass(hMod uintptr) error {
	name, _ := windows.UTF16PtrFromString(OverlayClassName)
	wc := wndclassExW{
		Size:      uint32(unsafe.Sizeof(wndclassExW{})),
		WndProc:   overlayWndProc,
		Instance:  hMod,
		ClassName: name,
		// Background brush NULL: WM_PAINT fills the whole window itself
		// (with the colour key).
	}
	r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 && !errors.Is(err, windows.ERROR_CLASS_ALREADY_EXISTS) {
		return fmt.Errorf("registering the overlay window class: %w", err)
	}
	return nil
}

// createWindow creates the hidden overlay window, off-screen and 1×1
// until the first Show, and applies the colour key and the best-effort
// capture exclusion. Runs on the UI thread.
func (o *Overlay) createWindow(hMod uintptr) error {
	cls, _ := windows.UTF16PtrFromString(OverlayClassName)
	hwnd, _, err := procCreateWindowExW.Call(
		uintptr(wsExLayered|wsExTransparent|wsExTopmost|wsExToolWindow|wsExNoActivate),
		uintptr(unsafe.Pointer(cls)), 0, uintptr(wsPopup),
		uintptr(int32ToU32(-overlayBorderWidth)), uintptr(int32ToU32(-overlayBorderWidth)), 1, 1,
		0, 0, hMod, 0)
	if hwnd == 0 {
		return fmt.Errorf("creating the overlay window: %w", err)
	}
	o.hwnd = hwnd
	// The colour key: every magenta pixel (all of the window except the
	// border and the pill) is fully transparent, and hit-testing passes
	// through transparent pixels.
	if r, _, _ := procSetLayeredWindowAttrs.Call(hwnd, uintptr(overlayKeyColor), 0, lwaColorKey); r == 0 {
		return errors.New("setting the overlay's layered window attributes failed")
	}
	// Best-effort exclusion from other software's captures (Win10 2004+).
	// winuse's own captures never rely on it; see HideWhile.
	procSetWindowDisplayAffinity.Call(hwnd, wdaExcludeFromCapture)
	return nil
}

// wndclassExW is the Win32 WNDCLASSEXW (80 bytes on amd64 and arm64).
type wndclassExW struct {
	Size, Style        uint32
	WndProc            uintptr
	ClsExtra, WndExtra int32
	Instance           uintptr
	Icon               uintptr
	Cursor             uintptr
	Background         uintptr
	MenuName           *uint16
	ClassName          *uint16
	IconSm             uintptr
}

var _ [80]byte = [unsafe.Sizeof(wndclassExW{})]byte{}

// paintStruct is the Win32 PAINTSTRUCT.
type paintStruct struct {
	Hdc         uintptr
	FErase      uint32
	RcPaint     Rect
	FRestore    uint32
	FIncUpdate  uint32
	RgbReserved [32]uint8
}

// overlayWndProc is the overlay window's procedure (created once;
// callbacks are never freed). HTTRANSPARENT plus WS_EX_TRANSPARENT make
// the window click-through even over its opaque pixels (the pill); it
// never takes focus (WS_EX_NOACTIVATE, and SWP_NOACTIVATE on every show).
var overlayWndProc = syscall.NewCallback(func(hwnd, message, wp, lp uintptr) uintptr {
	switch message {
	case wmNCHitTest:
		return htTransparent
	case wmEraseBkgnd:
		return 1 // WM_PAINT fills the whole window
	case wmPaint:
		paintOverlay(hwnd)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, message, wp, lp)
	return r
})

// paintOverlay draws the indicator: the transparent colour-key fill, the
// accent border framing the Window, and the pill banner at the top
// centre. Runs on the UI thread, from WM_PAINT.
func paintOverlay(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	var cr Rect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&cr)))
	if cr.Width() <= 0 || cr.Height() <= 0 {
		return
	}

	// The colour-key fill: everything not drawn on top of it is
	// transparent.
	fillOverlayRect(hdc, cr, overlayKeyColor)

	// The accent border around the controlled Window.
	for _, strip := range overlayBorderRects(cr) {
		fillOverlayRect(hdc, strip, colorref(OverlayAccent))
	}

	// The pill banner: measure the text, place the pill, fill it, then
	// draw the text centred in it.
	if font := createOverlayFont(dpiForWindow(hwnd)); font != 0 {
		old, _, _ := procSelectObject.Call(hdc, font)
		defer func() {
			// Deselect before deleting: GDI refuses to delete an object
			// still selected into a DC.
			procSelectObject.Call(hdc, old)
			procDeleteObject.Call(font)
		}()
	}
	procSetBkMode.Call(hdc, bkModeTransparent) // text draws over the solid pill only
	procSetTextColor.Call(hdc, uintptr(overlayTextColor))
	text, _ := windows.UTF16PtrFromString(overlayText)
	var tr Rect
	drawText(hdc, text, &tr, dtCalcRect|dtSingleLine)
	// Draw into a copy: DrawText must not move the pill rectangle itself.
	pill := overlayPillRect(cr, tr.Width(), tr.Height())
	if brush := createSolidBrush(overlayPillColor); brush != 0 {
		defer procDeleteObject.Call(brush)
		roundRectOverlay(hdc, pill, brush)
	}
	drawText(hdc, text, &pill, dtCenter|dtVCenter|dtSingleLine|dtEndEllipsis)
}

// createSolidBrush creates a solid brush of the COLORREF colour (the
// caller deletes it).
func createSolidBrush(c uint32) uintptr {
	brush, _, _ := procCreateSolidBrush.Call(uintptr(c))
	return brush
}

// fillOverlayRect fills r with a solid brush of the COLORREF colour,
// deleting the brush afterwards.
func fillOverlayRect(hdc uintptr, r Rect, c uint32) {
	brush := createSolidBrush(c)
	if brush == 0 {
		return
	}
	defer procDeleteObject.Call(brush)
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&r)), brush)
}

// roundRectOverlay fills the pill with brush as a rounded rectangle (the
// corner radius is the pill's height: a capsule).
func roundRectOverlay(hdc uintptr, r Rect, brush uintptr) {
	if r.Width() <= 0 || r.Height() <= 0 {
		return
	}
	pen, _, _ := procCreatePenW.Call(0 /*PS_SOLID*/, 1, uintptr(overlayPillColor))
	if pen == 0 {
		return
	}
	defer procDeleteObject.Call(pen)
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	defer procSelectObject.Call(hdc, oldPen)
	oldBrush, _, _ := procSelectObject.Call(hdc, brush)
	defer procSelectObject.Call(hdc, oldBrush)
	procRoundRect.Call(hdc,
		uintptr(int32ToU32(r.Left)), uintptr(int32ToU32(r.Top)),
		uintptr(int32ToU32(r.Right)), uintptr(int32ToU32(r.Bottom)),
		uintptr(int32ToU32(r.Height())), uintptr(int32ToU32(r.Height())))
}

// createOverlayFont creates the banner font at the window's DPI.
func createOverlayFont(dpi int32) uintptr {
	const (
		defaultCharset = 1
		fwSemibold     = 600
	)
	name, _ := windows.UTF16PtrFromString("Segoe UI")
	h := int32ToU32(-mulDiv32(overlayFontHeight, dpi, 96))
	f, _, _ := procCreateFontW.Call(
		uintptr(h), 0, 0, 0, fwSemibold, 0, 0, 0,
		defaultCharset, 0, 0, 0, 0, uintptr(unsafe.Pointer(name)))
	return f
}

// DrawText flags (DrawTextW docs).
const (
	dtCenter          = 0x0001
	dtVCenter         = 0x0004
	dtSingleLine      = 0x0020
	dtCalcRect        = 0x0400
	dtEndEllipsis     = 0x4000
	bkModeTransparent = 1 // SetBkMode's TRANSPARENT
)

// drawText draws text into r (or, with dtCalcRect, measures it into r).
// The length ^uintptr(0) (-1) means null-terminated.
func drawText(hdc uintptr, text *uint16, r *Rect, flags uint32) {
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(text)),
		^uintptr(0), uintptr(unsafe.Pointer(r)), uintptr(flags))
}

// dpiForWindow returns the window's DPI (96 when the API is missing,
// pre-1607 Windows, or unreadable).
func dpiForWindow(hwnd uintptr) int32 {
	if procGetDpiForWindow.Find() != nil {
		return 96
	}
	r, _, _ := procGetDpiForWindow.Call(hwnd)
	if r == 0 {
		return 96
	}
	return int32(r)
}

func mulDiv32(v, mul, div int32) int32 {
	return v * mul / div
}

// overlayBorderRects returns the four accent strips along a client area's
// edges: the indicator frames the Window it is raised over. Pure function
// of the client rectangle.
func overlayBorderRects(cr Rect) [4]Rect {
	w, h := cr.Width(), cr.Height()
	if w <= 0 || h <= 0 {
		return [4]Rect{}
	}
	return [4]Rect{
		{Left: cr.Left, Top: cr.Top, Right: cr.Left + w, Bottom: cr.Top + overlayBorderWidth},
		{Left: cr.Left, Top: cr.Top + h - overlayBorderWidth, Right: cr.Left + w, Bottom: cr.Top + h},
		{Left: cr.Left, Top: cr.Top, Right: cr.Left + overlayBorderWidth, Bottom: cr.Top + h},
		{Left: cr.Left + w - overlayBorderWidth, Top: cr.Top, Right: cr.Left + w, Bottom: cr.Top + h},
	}
}

// overlayPillRect returns the pill banner's rectangle inside cr for text
// measured at (textW, textH): horizontally centred at the top, clipped to
// the client area so a very narrow Window gets a clipped, ellipsized
// banner rather than an overflow. Pure function of its inputs.
func overlayPillRect(cr Rect, textW, textH int32) Rect {
	w, h := textW+2*overlayPillPadX, textH+2*overlayPillPadY
	if w > cr.Width() {
		w = cr.Width()
	}
	if h > cr.Height() {
		h = cr.Height()
	}
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	left := cr.Left + (cr.Width()-w)/2
	if left < cr.Left {
		left = cr.Left
	}
	top := cr.Top + overlayPillTop
	if bottom := cr.Top + cr.Height(); top+h > bottom {
		top = bottom - h
	}
	if top < cr.Top {
		top = cr.Top
	}
	return Rect{Left: left, Top: top, Right: left + w, Bottom: top + h}
}
