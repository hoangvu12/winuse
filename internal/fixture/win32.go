package fixture

// Raw Win32 plumbing for the fixture app: window classes, the message loop,
// GDI painting and the UIA provider entry points. Everything here follows
// the repo's no-cgo rule (raw syscalls, hand-built structs); struct layouts
// are the documented Win32 layouts for amd64/arm64 (both LP64 Windows).

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	uiaCore  = windows.NewLazySystemDLL("UIAutomationCore.dll")
	oleaut32 = windows.NewLazySystemDLL("oleaut32.dll")

	procGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
	procGlobalAlloc         = kernel32.NewProc("GlobalAlloc")
	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procAdjustWindowRectEx  = user32.NewProc("AdjustWindowRectEx")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procShowWindow          = user32.NewProc("ShowWindow")
	procUpdateWindow        = user32.NewProc("UpdateWindow")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procPostMessageW        = user32.NewProc("PostMessageW")
	procInvalidateRect      = user32.NewProc("InvalidateRect")
	procBeginPaint          = user32.NewProc("BeginPaint")
	procEndPaint            = user32.NewProc("EndPaint")
	procGetClientRect       = user32.NewProc("GetClientRect")
	procClientToScreen      = user32.NewProc("ClientToScreen")
	procGetWindowRect       = user32.NewProc("GetWindowRect")
	procSetWindowTextW      = user32.NewProc("SetWindowTextW")
	procEnableWindow        = user32.NewProc("EnableWindow")
	procSetFocus            = user32.NewProc("SetFocus")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procSetCapture          = user32.NewProc("SetCapture")
	procReleaseCapture      = user32.NewProc("ReleaseCapture")
	procSetTimer            = user32.NewProc("SetTimer")
	procKillTimer           = user32.NewProc("KillTimer")
	procTrackMouseEvent     = user32.NewProc("TrackMouseEvent")

	procCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	procCreatePen        = gdi32.NewProc("CreatePen")
	procCreateFontW      = gdi32.NewProc("CreateFontW")
	procGetStockObject   = gdi32.NewProc("GetStockObject")
	procDeleteObject     = gdi32.NewProc("DeleteObject")
	procSelectObject     = gdi32.NewProc("SelectObject")
	procFillRect         = user32.NewProc("FillRect")
	procRectangle        = gdi32.NewProc("Rectangle")
	procDrawTextW        = user32.NewProc("DrawTextW")
	procSetTextColor     = gdi32.NewProc("SetTextColor")
	procSetBkMode        = gdi32.NewProc("SetBkMode")
	procMoveToEx         = gdi32.NewProc("MoveToEx")
	procLineTo           = gdi32.NewProc("LineTo")

	procUiaReturnRawElementProvider = uiaCore.NewProc("UiaReturnRawElementProvider")
	procUiaHostProviderFromHwnd     = uiaCore.NewProc("UiaHostProviderFromHwnd")
	procSysAllocStringLen           = oleaut32.NewProc("SysAllocStringLen")
	procSafeArrayCreateVector       = oleaut32.NewProc("SafeArrayCreateVector")
	procSafeArrayPutElement         = oleaut32.NewProc("SafeArrayPutElement")
)

// Messages and styles the fixture uses.
const (
	wmDestroy          = 0x0002
	wmClose            = 0x0010
	wmGetobject        = 0x003D
	wmSetFocus         = 0x0007
	wmPaint            = 0x000F
	wmTimer            = 0x0113
	wmWindowPosChanged = 0x0046
	wmKeydown          = 0x0100
	wmChar             = 0x0102
	wmSyskeydown       = 0x0104
	wmSyskeyup         = 0x0105
	wmKeyup            = 0x0101
	wmMousemove        = 0x0200
	wmLButtonDown      = 0x0201
	wmLButtonUp        = 0x0202
	wmLButtonDblClk    = 0x0203
	wmRButtonDown      = 0x0204
	wmRButtonUp        = 0x0205
	wmMouseWheel       = 0x020A
	wmMouseHWheel      = 0x020E
	wmMouseLeave       = 0x02A3

	csDblClks = 0x0008

	swShow = 5
	swHide = 0

	wsOverlapped  = 0x00000000
	wsCaption     = 0x00C00000
	wsSysMenu     = 0x00080000
	wsMinimizeBox = 0x00020000
	wsPopup       = 0x80000000

	dtLeft        = 0x00
	dtCenter      = 0x01
	dtVCenter     = 0x04
	dtSingleLine  = 0x20
	dtEndEllipsis = 0x4000

	transparentBk = 1

	tmeLeave = 0x00000002

	gptrFlags = 0x0040 // GMEM_FIXED | GMEM_ZEROINIT (GPTR)
)

// UiaRootObjectId is the WM_GETOBJECT object id that requests the UIA root
// provider (UIAutomationCoreApi.h).
const uiaRootObjectId = -25

// point is a Win32 POINT.
type point struct{ X, Y int32 }

// msg is a Win32 MSG (48 bytes on 64-bit Windows).
type msg struct {
	Hwnd    uintptr
	Message uint32
	_       uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	_       uint32
}

// paintStruct is a Win32 PAINTSTRUCT (72 bytes; over-allocated by caller).
type paintStruct [96]byte

// wndclassex is a Win32 WNDCLASSEXW (80 bytes on 64-bit).
type wndclassex struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

// trackMouseEvent is a Win32 TRACKMOUSEEVENT.
type trackMouseEvent struct {
	Size      uint32
	Flags     uint32
	HwndTrack uintptr
	HoverTime uint32
}

// malloc16 allocates pointer-sized, zeroed, never-moved C memory (used for
// COM objects and vtables; see uia.go).
func malloc16(bytes uintptr) unsafe.Pointer {
	r, _, err := procGlobalAlloc.Call(gptrFlags, bytes)
	if r == 0 {
		panic("winusefixture: GlobalAlloc failed: " + err.Error())
	}
	return cptr(r)
}

// utf16Ptr allocates a NUL-terminated UTF-16 string pointer.
func utf16Ptr(s string) *uint16 {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		return nil
	}
	return p
}

// utf16ToString converts a NUL-terminated UTF-16 pointer to a Go string.
func utf16ToString(p *uint16) string {
	return windows.UTF16PtrToString(p)
}

// lParamPoint unpacks the packed mouse coordinates of a mouse message's
// lParam (two signed 16-bit values).
func lParamPoint(lp uintptr) point {
	return point{
		X: int32(int16(lp)),
		Y: int32(int16(lp >> 16)),
	}
}

// wheelDelta unpacks a wheel message's wParam (a signed 16-bit delta in the
// high word).
func wheelDelta(wp uintptr) int32 {
	return int32(int16(wp >> 16))
}

var newCallback = syscall.NewCallback
