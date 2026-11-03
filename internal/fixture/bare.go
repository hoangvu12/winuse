package fixture

// The Classic Dialog's bare control (ClassicBareID; headless-max 09,
// hm-09b): a child window of the fixture's own class (BareControlClass)
// whose own UI Automation provider reports an empty FrameworkId. That is
// the Element kind winuse's ContextMenu takes as classic when it has its
// own window (helper.contextMenuRoute): a raw custom-class window reports
// "Win32" through the system's proxy, so only a provider of its own makes
// the FrameworkId empty (hm-09b measured both). The control answers
// WM_CONTEXTMENU with a classic popup menu (TrackPopupMenu, a `#32768`) of
// the Classic Dialog's Fill Name and Select All commands, and hands the
// chosen command to the dialog, which runs and records it
// (classicMenuCommand).
//
// The provider is one IRawElementProviderSimple per control window, in
// the same never-moved, immortal C memory as uia.go's objects (vtable
// pointer + the HWND). It answers FrameworkId (""), Name, AutomationId
// (the control ID, as the Win32 proxy would) and ControlType (Pane); the
// host provider (UiaHostProviderFromHwnd) supplies the rest, the
// NativeWindowHandle among them.

import (
	"runtime"
	"strconv"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/hoangvu12/winuse/internal/win"
)

const (
	wmContextMenu   = 0x007B
	tpmRightButton  = 0x0002
	colorWindow     = 5     // COLOR_WINDOW
	controlTypePane = 50033 // UIA_PaneControlTypeId
)

// bareVtable is the bare provider's IRawElementProviderSimple vtable.
var bareVtable uintptr

func init() {
	methods := []uintptr{
		newCallback(bareQueryInterface),
		newCallback(comAddRef),
		newCallback(comRelease),
		newCallback(getProviderOptions),
		newCallback(bareGetPatternProvider),
		newCallback(bareGetPropertyValue),
		newCallback(bareGetHostRawElementProvider),
	}
	mem := malloc16(uintptr(len(methods)) * unsafe.Sizeof(uintptr(0)))
	copy(unsafe.Slice((*uintptr)(mem), len(methods)), methods)
	bareVtable = uintptr(mem)
}

// bareProvider returns (creating once) the provider of a bare control
// window.
func (a *app) bareProvider(hwnd uintptr) uintptr {
	if p, ok := a.bareProviders[hwnd]; ok {
		return p
	}
	mem := malloc16(2 * unsafe.Sizeof(uintptr(0)))
	words := (*[2]uintptr)(mem)
	words[0] = bareVtable
	words[1] = hwnd
	if a.bareProviders == nil {
		a.bareProviders = map[uintptr]uintptr{}
	}
	a.bareProviders[hwnd] = uintptr(mem)
	return uintptr(mem)
}

// bareHwnd reads the control window back from a bare provider (`this`).
func bareHwnd(this uintptr) uintptr {
	return (*[2]uintptr)(cptr(this))[1]
}

func bareQueryInterface(this, riid, ppv uintptr) uintptr {
	if ppv == 0 || riid == 0 {
		return hrFail(ePointer)
	}
	p := (*unsafe.Pointer)(cptr(ppv))
	*p = nil
	if g := (*windows.GUID)(cptr(riid)); *g != iidIUnknown && *g != iidRawElementProviderSimple {
		return hrFail(eNoInterface)
	}
	*p = cptr(this)
	return hrOK()
}

func bareGetPatternProvider(this, patternId, ppv uintptr) uintptr {
	if ppv == 0 {
		return hrFail(ePointer)
	}
	*(*unsafe.Pointer)(cptr(ppv)) = nil
	return hrOK()
}

func bareGetPropertyValue(this, propId, pv uintptr) uintptr {
	if pv == 0 {
		return hrFail(ePointer)
	}
	v := (*variant)(cptr(pv))
	*v = variant{} // VT_EMPTY: the host provider answers
	switch propId {
	case propFrameworkId:
		v.setBSTR("") // set, and empty: not the proxy's "Win32"
	case propName:
		v.setBSTR(BareControlName)
	case propAutomationId:
		v.setBSTR(strconv.Itoa(ClassicBareID))
	case propControlType:
		v.setInt32(controlTypePane)
	}
	return hrOK()
}

func bareGetHostRawElementProvider(this, ppv uintptr) uintptr {
	if ppv == 0 {
		return hrFail(ePointer)
	}
	*(*unsafe.Pointer)(cptr(ppv)) = nil
	hr, _, _ := procUiaHostProviderFromHwnd.Call(bareHwnd(this), ppv)
	if int32(hr) < 0 {
		return hr
	}
	return hrOK()
}

// registerBareClass registers BareControlClass, painted with the window
// colour.
func registerBareClass(proc, hmod uintptr) error {
	cn := utf16Ptr(BareControlClass)
	wc := wndclassex{
		Size:       uint32(unsafe.Sizeof(wndclassex{})),
		WndProc:    proc,
		Instance:   hmod,
		Background: colorWindow + 1,
		ClassName:  cn,
	}
	r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	runtime.KeepAlive(cn)
	if r == 0 {
		return err
	}
	return nil
}

// wndBare is the bare control's window procedure: its provider, its
// context menu, and the menu's command handed to the dialog.
func (a *app) wndBare(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	switch msg {
	case wmGetobject:
		if int32(lp) == uiaRootObjectId {
			r, _, _ := procUiaReturnRawElementProvider.Call(hwnd, wp, lp, a.bareProvider(hwnd))
			return r
		}
	case wmContextMenu:
		a.bareMenu(hwnd, lp)
		return 0
	case wmCommand:
		if parent, _, _ := procGetParent.Call(hwnd); parent != 0 {
			sendMessage(parent, wmCommand, wp, 0)
		}
		return 0
	case wmDestroy:
		delete(a.bareProviders, hwnd)
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
	return r
}

// bareMenu shows the bare control's context menu: at the point of a
// right click, or near the control's top-left for the keyboard form
// (lParam -1, what winuse posts). TrackPopupMenu is modal until the menu
// closes; the chosen command arrives as WM_COMMAND to the control.
func (a *app) bareMenu(hwnd, lp uintptr) {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	appendMenu(menu, 0, ClassicMenuFillID, "&Fill Name")
	appendMenu(menu, 0, ClassicMenuSelectAllID, "Select &All")
	pt := lParamPoint(lp)
	if int32(lp) == -1 {
		var r win.Rect
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		pt = point{X: r.Left + 8, Y: r.Top + 8}
	}
	procTrackPopupMenu.Call(menu, tpmRightButton, uintptr(uint32(pt.X)), uintptr(uint32(pt.Y)), 0, hwnd, 0)
}
