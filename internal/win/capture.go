package win

// GDI screenshot capture (ticket 08): the two fallback methods of the spec's
// capture chain — the screen region (BitBlt with CAPTUREBLT, everything
// drawn over the Window included) and PrintWindow (PW_RENDERFULLCONTENT,
// works off-screen, renders only the Window's own content). Both implement
// shot.Method; Windows.Graphics.Capture (internal/wgc, ticket 09) comes
// first in front of them. Recipes: win32.md §10. All coordinates are physical pixels (the
// process is per-monitor-v2 DPI aware). No capture is ever performed from
// the CLI process — only the helper captures, after activating the Window.

import (
	"errors"
	"fmt"
	"image"
	"time"
	"unsafe"

	"github.com/hoangvu12/winuse/internal/shot"
	"golang.org/x/sys/windows"
)

// ErrGone reports a Window whose Win32 data can no longer be read: it
// closed between being resolved and being captured or clicked at.
var ErrGone = errors.New("window is gone")

var (
	gdi32 = windows.NewLazySystemDLL("gdi32.dll")

	procGetDC                  = user32.NewProc("GetDC")
	procReleaseDC              = user32.NewProc("ReleaseDC")
	procGetSystemMetrics       = user32.NewProc("GetSystemMetrics")
	procGetWindowDisplayAff    = user32.NewProc("GetWindowDisplayAffinity")
	procPrintWindow            = user32.NewProc("PrintWindow")
	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procGetDIBits              = gdi32.NewProc("GetDIBits")
)

// GDI constants (win32.md §10).
const (
	srcCopy       = 0x00CC0020
	captureBlt    = 0x40000000
	dibRGBColors  = 0
	biRGB         = 0
	pwFullContent = 2 // PW_RENDERFULLCONTENT

	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79

	// wdaNone is GetWindowDisplayAffinity's "no restriction" value; any
	// other affinity means the Window excludes itself from capture.
	wdaNone = 0
)

// printWindowBudget bounds one PrintWindow call: it renders by sending
// messages into the target app, so a hung app would otherwise block the
// request (win32.md §10). The call is abandoned after the budget.
const printWindowBudget = 1500 * time.Millisecond

// bitmapInfoHeader is the Win32 BITMAPINFOHEADER (40 bytes).
type bitmapInfoHeader struct {
	Size          uint32
	Width, Height int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

var _ [40]byte = [unsafe.Sizeof(bitmapInfoHeader{})]byte{}

// FrameBounds returns the Window's extended frame bounds in physical
// pixels: the visible extent without the drop shadow and the invisible
// resize borders — the anchor every capture method and coordinate mapping
// uses (wgc.md §5 confirms Windows.Graphics.Capture's frame origin is the
// extended frame bounds too, so the Windows.Graphics.Capture method
// plugged in unchanged).
func FrameBounds(hwnd uint64) (Rect, error) {
	var r Rect
	if err := windows.DwmGetWindowAttribute(windows.HWND(hwnd), windows.DWMWA_EXTENDED_FRAME_BOUNDS,
		unsafe.Pointer(&r), uint32(unsafe.Sizeof(r))); err != nil {
		return Rect{}, fmt.Errorf("%w (window %d): reading the extended frame bounds failed: %v", ErrGone, hwnd, err)
	}
	if r.Width() <= 0 || r.Height() <= 0 {
		return Rect{}, fmt.Errorf("%w (window %d): the Window has no visible bounds", ErrGone, hwnd)
	}
	return r, nil
}

// FrameOrigin returns the Window's extended frame bounds top-left in
// screen coordinates — the anchor a `@x,y` click adds to the mapped
// window-space point at Action time (fresh, so the click survives the
// Window moving since the screenshot).
func FrameOrigin(hwnd uint64) (image.Point, error) {
	r, err := FrameBounds(hwnd)
	if err != nil {
		return image.Point{}, err
	}
	return image.Point{X: int(r.Left), Y: int(r.Top)}, nil
}

// VirtualScreenRect returns the bounding rectangle of the whole virtual
// desktop (every monitor), in physical pixels.
func VirtualScreenRect() Rect {
	x, y := systemMetric(smXVirtualScreen), systemMetric(smYVirtualScreen)
	w, h := systemMetric(smCXVirtualScreen), systemMetric(smCYVirtualScreen)
	return Rect{Left: x, Top: y, Right: x + w, Bottom: y + h}
}

func systemMetric(index int32) int32 {
	r, _, _ := procGetSystemMetrics.Call(uintptr(int32ToU32(index)))
	return int32(r)
}

// windowDisplayAffinity returns the Window's SetWindowDisplayAffinity
// value, or 0 when it cannot be read.
func windowDisplayAffinity(hwnd uint64) uint32 {
	var aff uint32
	if r, _, _ := procGetWindowDisplayAff.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&aff))); r == 0 {
		return 0
	}
	return aff
}

// RegionMethod is the capture chain's workhorse: capture the Window's
// screen region — the union of its extended frame bounds with its
// overlapping owned popups (menus, tooltips, dropdowns) — with
// BitBlt|CAPTUREBLT. Layered windows are included, so everything drawn
// over the Window appears in the screenshot (spec, user story 19); the
// mouse cursor does not (it is not part of the screen DC).
//
// It skips itself when the region is not fully on the virtual desktop
// (BitBlt would clip or garbage the off-screen part — PrintWindow, the
// next method, covers that case) or when the Window excludes itself from
// capture (display affinity; the screen would show the Window's
// surroundings instead of it).
type RegionMethod struct{}

// Name implements shot.Method.
func (RegionMethod) Name() string { return "screen region" }

// CaptureWindow implements shot.Method. The Window must be in the
// foreground (the caller activates first: the spec's region capture is
// "after it is in the foreground").
func (RegionMethod) CaptureWindow(hwnd uint64) (shot.Frame, error) {
	all, err := Collect()
	if err != nil {
		return shot.Frame{}, err
	}
	var target WindowInfo
	found := false
	for _, w := range all {
		if w.ID == hwnd {
			target, found = w, true
			break
		}
	}
	if !found {
		return shot.Frame{}, fmt.Errorf("%w (window %d): the Window is no longer enumerable", ErrGone, hwnd)
	}
	if aff := windowDisplayAffinity(hwnd); aff != wdaNone {
		return shot.Frame{}, fmt.Errorf("the Window excludes itself from capture (display affinity 0x%x)", aff)
	}
	rect := PopupUnion(all, target)
	if !rectWithin(VirtualScreenRect(), rect) {
		return shot.Frame{}, errors.New("the Window is partly off-screen")
	}
	img, err := CaptureScreenRegion(rect)
	if err != nil {
		return shot.Frame{}, err
	}
	return shot.Frame{
		Image:  img,
		Rect:   imageRect(rect),
		Anchor: image.Point{X: int(target.Bounds.Left), Y: int(target.Bounds.Top)},
	}, nil
}

// PopupUnion returns the union of target's bounds with the bounds of its
// owned popups that overlap or touch it: menus, tooltips and dropdowns of
// the same App, owned directly or through an owner chain (menus that carry
// no owner link are matched by class #32768 and process). A screen-region
// capture of the union shows the popups drawn over the Window — and menus
// dropping past the Window's edge — where the plain bounds would cut them
// off. Popups that do not overlap are ignored (an owned dialog elsewhere
// on screen is its own Window, not part of this one's screenshot), and so
// are winuse's own windows (the later overlay) and cloaked popups.
//
// Pure function of the window list: offline-testable.
func PopupUnion(all []WindowInfo, target WindowInfo) Rect {
	byID := make(map[uint64]WindowInfo, len(all))
	for _, w := range all {
		byID[w.ID] = w
	}
	union := target.Bounds
	for _, w := range all {
		if w.ID == target.ID || !ownedPopupOf(byID, w, target) {
			continue
		}
		if rectsNear(w.Bounds, target.Bounds, 1) {
			union = unionRect(union, w.Bounds)
		}
	}
	return union
}

// ownedPopupOf reports whether w is a popup owned by target: w's owner
// chain reaches target (menus, tooltips, owned popups), or w is a menu
// window of target's process (some menu styles carry no owner link).
func ownedPopupOf(byID map[uint64]WindowInfo, w WindowInfo, target WindowInfo) bool {
	if w.OwnProcess || w.ShellWindow || w.Cloaked {
		return false
	}
	cur := w
	for cur.Owner != 0 {
		if cur.Owner == target.ID {
			return true
		}
		parent, ok := byID[cur.Owner]
		if !ok {
			break
		}
		cur = parent
	}
	return w.Class == menuWindowClass && w.PID == target.PID
}

// menuWindowClass is the Win32 class of menu popups.
const menuWindowClass = "#32768"

// rectsNear reports whether a and b overlap or come within tol pixels of
// each other (a menu whose top row starts exactly at the Window's bottom
// edge touches the Window and joins the capture region).
func rectsNear(a, b Rect, tol int32) bool {
	return a.Left < b.Right+tol && a.Right+tol > b.Left &&
		a.Top < b.Bottom+tol && a.Bottom+tol > b.Top
}

func unionRect(a, b Rect) Rect {
	return Rect{
		Left:   min32(a.Left, b.Left),
		Top:    min32(a.Top, b.Top),
		Right:  max32(a.Right, b.Right),
		Bottom: max32(a.Bottom, b.Bottom),
	}
}

func rectWithin(outer, inner Rect) bool {
	return inner.Left >= outer.Left && inner.Top >= outer.Top &&
		inner.Right <= outer.Right && inner.Bottom <= outer.Bottom
}

func min32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

func max32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

func imageRect(r Rect) image.Rectangle {
	return image.Rectangle{
		Min: image.Point{X: int(r.Left), Y: int(r.Top)},
		Max: image.Point{X: int(r.Right), Y: int(r.Bottom)},
	}
}

// PopupPresent reports whether any of target's owned popups overlaps
// or touches it — the same population PopupUnion unions. Windows's own
// capture (Windows.Graphics.Capture, ticket 09) captures only the
// Window's own window tree, so a capture method that cannot include
// popups must defer to the screen-region capture while one is open (spec,
// user story 19). Pure function of the window list: offline-testable.
func PopupPresent(all []WindowInfo, target WindowInfo) bool {
	byID := make(map[uint64]WindowInfo, len(all))
	for _, w := range all {
		byID[w.ID] = w
	}
	for _, w := range all {
		if w.ID != target.ID && ownedPopupOf(byID, w, target) && rectsNear(w.Bounds, target.Bounds, 1) {
			return true
		}
	}
	return false
}

// CaptureScreenRegion captures the screen rectangle r — physical pixels,
// virtual-desktop coordinates, any monitor — into an RGBA image (one image
// pixel per screen pixel) with BitBlt|CAPTUREBLT: layered windows drawn
// over the region (menus, tooltips, popups) are included; the cursor is
// not (win32.md §10).
func CaptureScreenRegion(r Rect) (*image.RGBA, error) {
	if err := LiveDesktopGuard(); err != nil {
		return nil, err
	}
	w, h := r.Width(), r.Height()
	if w <= 0 || h <= 0 {
		return nil, errors.New("the capture region is empty")
	}
	src, _, err := procGetDC.Call(0)
	if src == 0 {
		return nil, fmt.Errorf("GetDC failed: %v", err)
	}
	defer procReleaseDC.Call(0, src)

	mem, _, err := procCreateCompatibleDC.Call(src)
	if mem == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC failed: %v", err)
	}
	defer procDeleteDC.Call(mem)

	bmp, _, err := procCreateCompatibleBitmap.Call(src, uintptr(int32ToU32(w)), uintptr(int32ToU32(h)))
	if bmp == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap failed: %v", err)
	}
	defer procDeleteObject.Call(bmp)

	old, _, _ := procSelectObject.Call(mem, bmp)
	ok, _, err := procBitBlt.Call(
		mem, 0, 0, uintptr(int32ToU32(w)), uintptr(int32ToU32(h)),
		src, uintptr(int32ToU32(r.Left)), uintptr(int32ToU32(r.Top)),
		srcCopy|captureBlt)
	if ok == 0 {
		procSelectObject.Call(mem, old) // restore before the deferred deletes
		return nil, fmt.Errorf("BitBlt failed: %v", err)
	}
	// GetDIBits requires the bitmap to not be selected into a DC.
	procSelectObject.Call(mem, old)
	return dibToRGBA(mem, bmp, w, h)
}

// CapturePrintWindow renders hwnd's own content with
// PrintWindow(PW_RENDERFULLCONTENT) — DirectComposition and Chromium
// content included (win32.md §10) — into a GetWindowRect-sized bitmap, then
// crops to the extended frame bounds, so the returned image covers exactly
// the visible Window and the caller's shot.Frame can anchor both Rect and
// Anchor at the extended frame bounds like every other method. The second
// return value is those extended frame bounds.
//
// Unlike a screen-region capture it works while the Window is partly
// off-screen (or excludes itself from capture); unlike it, popups and
// overlapping Windows are not part of the image — the price of the
// fallback position. A hung app is abandoned after printWindowBudget.
func CapturePrintWindow(hwnd uint64) (*image.RGBA, Rect, error) {
	if err := LiveDesktopGuard(); err != nil {
		return nil, Rect{}, err
	}
	h := windows.HWND(hwnd)
	var wr Rect
	if ok, _, _ := procGetWindowRect.Call(uintptr(h), uintptr(unsafe.Pointer(&wr))); ok == 0 {
		return nil, Rect{}, fmt.Errorf("%w (window %d): reading the Window's rect failed", ErrGone, hwnd)
	}
	w, hgt := wr.Width(), wr.Height()
	if w <= 0 || hgt <= 0 {
		return nil, Rect{}, errors.New("the Window has no size")
	}
	efb, err := FrameBounds(hwnd)
	if err != nil {
		return nil, Rect{}, err
	}

	src, _, err := procGetDC.Call(0)
	if src == 0 {
		return nil, Rect{}, fmt.Errorf("GetDC failed: %v", err)
	}
	mem, _, err := procCreateCompatibleDC.Call(src)
	if mem == 0 {
		procReleaseDC.Call(0, src)
		return nil, Rect{}, fmt.Errorf("CreateCompatibleDC failed: %v", err)
	}
	bmp, _, err := procCreateCompatibleBitmap.Call(src, uintptr(int32ToU32(w)), uintptr(int32ToU32(hgt)))
	if bmp == 0 {
		procDeleteDC.Call(mem)
		procReleaseDC.Call(0, src)
		return nil, Rect{}, fmt.Errorf("CreateCompatibleBitmap failed: %v", err)
	}
	old, _, _ := procSelectObject.Call(mem, bmp)

	// PrintWindow renders by sending messages into the target app, so it
	// can block on a hung one. On timeout the GDI objects are leaked, not
	// deleted: the abandoned call may still be drawing into them (the
	// process exit reclaims them).
	type pwResult struct {
		ok  uintptr
		err error
	}
	done := make(chan pwResult, 1)
	go func() {
		ok, _, err := procPrintWindow.Call(uintptr(h), mem, pwFullContent)
		done <- pwResult{ok, err}
	}()
	select {
	case r := <-done:
		if r.ok == 0 {
			procSelectObject.Call(mem, old)
			procDeleteObject.Call(bmp)
			procDeleteDC.Call(mem)
			procReleaseDC.Call(0, src)
			return nil, Rect{}, fmt.Errorf("PrintWindow failed: %v", r.err)
		}
		procSelectObject.Call(mem, old) // GetDIBits requires the bitmap deselected
		img, dibErr := dibToRGBA(mem, bmp, w, hgt)
		procDeleteObject.Call(bmp)
		procDeleteDC.Call(mem)
		procReleaseDC.Call(0, src)
		if dibErr != nil {
			return nil, Rect{}, dibErr
		}
		return cropToFrameBounds(img, wr, efb), efb, nil
	case <-time.After(printWindowBudget):
		// The abandoned call may still draw into mem/bmp, so those leak
		// (the process exit reclaims them); the screen DC is already only
		// ours, so it is released.
		procReleaseDC.Call(0, src)
		return nil, Rect{}, fmt.Errorf("PrintWindow did not answer within %s (the app may be hung)", printWindowBudget)
	}
}

// cropToFrameBounds crops a GetWindowRect-sized PrintWindow image to the
// extended frame bounds (the invisible resize borders must not show in
// the screenshot), returning a fresh (0,0)-origin image.
func cropToFrameBounds(img *image.RGBA, wr, efb Rect) *image.RGBA {
	left := max32(efb.Left, wr.Left)
	top := max32(efb.Top, wr.Top)
	right := min32(efb.Right, wr.Right)
	bottom := min32(efb.Bottom, wr.Bottom)
	if right <= left || bottom <= top {
		return img // bounds inconsistent (never seen in practice): keep it all
	}
	crop := image.Rect(int(left-wr.Left), int(top-wr.Top), int(right-wr.Left), int(bottom-wr.Top))
	sub := img.SubImage(crop).(*image.RGBA)
	out := image.NewRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
	for y := 0; y < crop.Dy(); y++ {
		srcRow := sub.Pix[y*sub.Stride : y*sub.Stride+crop.Dx()*4]
		copy(out.Pix[y*out.Stride:y*out.Stride+crop.Dx()*4], srcRow)
	}
	return out
}

// PrintWindowMethod is the capture chain's last resort: render the
// Window's own content with PrintWindow(PW_RENDERFULLCONTENT). It reaches
// Windows that are partly off-screen and Windows that exclude themselves
// from screen capture; it cannot include popups or overlapping Windows.
type PrintWindowMethod struct{}

// Name implements shot.Method.
func (PrintWindowMethod) Name() string { return "PrintWindow" }

// CaptureWindow implements shot.Method.
func (PrintWindowMethod) CaptureWindow(hwnd uint64) (shot.Frame, error) {
	img, efb, err := CapturePrintWindow(hwnd)
	if err != nil {
		return shot.Frame{}, err
	}
	return shot.Frame{
		Image:  img,
		Rect:   imageRect(efb),
		Anchor: image.Point{X: int(efb.Left), Y: int(efb.Top)},
	}, nil
}

// dibToRGBA reads a GDI bitmap back as a top-down RGBA image: GetDIBits
// into the image's own pixel buffer (32-bpp BI_RGB), then BGRA→RGBA with
// opaque alpha (the alpha channel is undefined in GDI captures).
func dibToRGBA(dc, bmp uintptr, w, h int32) (*image.RGBA, error) {
	bi := bitmapInfoHeader{
		Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:       w,
		Height:      -h, // top-down
		Planes:      1,
		BitCount:    32,
		Compression: biRGB,
	}
	img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	ok, _, err := procGetDIBits.Call(
		dc, bmp, 0, uintptr(int32ToU32(h)),
		uintptr(unsafe.Pointer(&img.Pix[0])),
		uintptr(unsafe.Pointer(&bi)), dibRGBColors)
	if ok == 0 || int32(ok) != h {
		return nil, fmt.Errorf("GetDIBits failed: %v (copied %d of %d rows)", err, int32(ok), h)
	}
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+2] = img.Pix[i+2], img.Pix[i] // BGRA → RGBA
		img.Pix[i+3] = 255
	}
	return img, nil
}
