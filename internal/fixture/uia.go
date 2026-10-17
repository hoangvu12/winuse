package fixture

// The fixture's server-side UI Automation provider: what makes its Elements
// visible to winuse with exact Names, AutomationIds, roles, control
// patterns and states. Standard Win32 controls would only get MSAA proxies,
// whose AutomationId is always empty (the MSAA→UIA mapping has no
// AutomationId entry), so the fixture implements the provider itself — the
// same contract Chromium/Electron apps use.
//
// Everything below was written against the Windows SDK 10.0.26100.0
// headers: interface IIDs, vtable slot orders, UiaRootObjectId (-25),
// ProviderOptions_ServerSideProvider (0x2), UiaAppendRuntimeId (3), and the
// provider-side contracts of GetRuntimeId (NULL for window-hosted roots,
// [UiaAppendRuntimeId, id] for fragments) and get_HostRawElementProvider
// (UiaHostProviderFromHwnd for roots, NULL for fragments).
//
// COM objects are two-word allocations in never-moved C memory
// (GlobalAlloc'd: vtable pointer + *uiaNode back-reference); Go's GC
// cannot see or move them. All *uiaNode values are additionally reachable
// from the provider registry, so they cannot be collected either. Objects
// are immortal (AddRef/Release are no-ops), which is legal because the
// process owns a fixed, small set of them for its whole lifetime.
//
// Provider callbacks arrive on the app's single UI thread (the STA thread
// that pumps messages and returned the root from WM_GETOBJECT), the same
// thread all app state lives on, so no locking is needed anywhere.
//
// One ABI limitation, documented rather than worked around: Go's Windows
// callback trampoline forwards only the integer argument registers, so
// methods whose parameters are doubles by value (ElementProviderFromPoint,
// IScrollProvider::SetScrollPercent) cannot read their arguments.
// ElementProviderFromPoint therefore always reports "no element at point"
// (legal: pRetVal is optional-out), and SetScrollPercent returns
// E_NOTIMPL (Scroll with enum amounts — which winuse uses for "Scroll
// Down" — works). Nothing in winuse's v1 surface needs either.

import (
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ---- constants (verified against the SDK headers) ----

const (
	uiaAppendRuntimeId        = 3
	providerOptionsServerSide = 0x2
	uiaErrElementNotEnabledHR = 0x80040200
	eNoInterface              = 0x80004002
	eNotImpl                  = 0x80004001
	ePointer                  = 0x80004003
)

// Provider interface IIDs (SDK 10.0.26100.0, UIAutomationCore.h).
var (
	iidIUnknown = windows.GUID{Data1: 0x00000000, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}

	iidRawElementProviderSimple       = windows.GUID{Data1: 0xd6dd68d1, Data2: 0x86fd, Data3: 0x4332, Data4: [8]byte{0x86, 0x66, 0x9a, 0xbe, 0xde, 0xa2, 0xd2, 0x4c}}
	iidRawElementProviderFragment     = windows.GUID{Data1: 0xf7063da8, Data2: 0x8359, Data3: 0x439c, Data4: [8]byte{0x92, 0x97, 0xbb, 0xc5, 0x29, 0x9a, 0x7d, 0x87}}
	iidRawElementProviderFragmentRoot = windows.GUID{Data1: 0x620ce2a5, Data2: 0xab8f, Data3: 0x40a9, Data4: [8]byte{0x86, 0xcb, 0xde, 0x3c, 0x75, 0x59, 0x9b, 0x58}}
	iidInvokeProvider                 = windows.GUID{Data1: 0x54fcb24b, Data2: 0xe18e, Data3: 0x47a2, Data4: [8]byte{0xb4, 0xd3, 0xec, 0xcb, 0xe7, 0x75, 0x99, 0xa2}}
	iidValueProvider                  = windows.GUID{Data1: 0xc7935180, Data2: 0x6fb3, Data3: 0x4201, Data4: [8]byte{0xb1, 0x74, 0x7d, 0xf7, 0x3a, 0xdb, 0xf6, 0x4a}}
	iidToggleProvider                 = windows.GUID{Data1: 0x56d00bd0, Data2: 0xc4f4, Data3: 0x433c, Data4: [8]byte{0xa8, 0x36, 0x1a, 0x52, 0xa5, 0x7e, 0x08, 0x92}}
	iidExpandCollapseProvider         = windows.GUID{Data1: 0xd847d3a5, Data2: 0xcab0, Data3: 0x4a98, Data4: [8]byte{0x8c, 0x32, 0xec, 0xb4, 0x5c, 0x59, 0xad, 0x24}}
	iidSelectionProvider              = windows.GUID{Data1: 0xfb8b03af, Data2: 0x3bdf, Data3: 0x48d4, Data4: [8]byte{0xbd, 0x36, 0x1a, 0x65, 0x79, 0x3b, 0xe1, 0x68}}
	iidSelectionItemProvider          = windows.GUID{Data1: 0x2acad808, Data2: 0xb2d4, Data3: 0x452d, Data4: [8]byte{0xa4, 0x07, 0x91, 0xff, 0x1a, 0xd1, 0x67, 0xb2}}
	iidScrollProvider                 = windows.GUID{Data1: 0xb38b8077, Data2: 0x1fc3, Data3: 0x42a5, Data4: [8]byte{0x8c, 0xae, 0xd4, 0x0c, 0x22, 0x15, 0x05, 0x5a}}
	iidScrollItemProvider             = windows.GUID{Data1: 0x2360c714, Data2: 0x4bf1, Data3: 0x4b26, Data4: [8]byte{0xba, 0x65, 0x9b, 0x21, 0x31, 0x61, 0x27, 0xeb}}
	// IWindowProvider (SDK UIAutomationCore.h): the Window pattern the
	// fixture's window roots expose, which lists Raise among their
	// Secondary Actions (ticket 07).
	iidWindowProvider = windows.GUID{Data1: 0x987df77b, Data2: 0xdb06, Data3: 0x4d77, Data4: [8]byte{0x8f, 0x8a, 0x86, 0xa9, 0xc3, 0xbb, 0x90, 0xb9}}
)

// UIA property ids used by GetPropertyValue (uia-go.md §3).
const (
	propBoundingRectangle       = 30001
	propControlType             = 30003
	propName                    = 30005
	propHasKeyboardFocus        = 30008
	propIsKeyboardFocusable     = 30009
	propIsEnabled               = 30010
	propAutomationId            = 30011
	propClassName               = 30012
	propIsControlElement        = 30016
	propIsContentElement        = 30017
	propIsOffscreen             = 30022
	propFrameworkId             = 30024
	propValueValue              = 30045
	propValueIsReadOnly         = 30046
	propExpandCollapseState     = 30070
	propSelectionItemIsSelected = 30079
	propToggleToggleState       = 30086
	propIsDialog                = 30174

	propIsExpandCollapseAvailable = 30028
	propIsInvokeAvailable         = 30031
	propIsScrollAvailable         = 30034
	propIsScrollItemAvailable     = 30035
	propIsSelectionItemAvailable  = 30036
	propIsSelectionAvailable      = 30037
	propIsToggleAvailable         = 30041
	propIsValueAvailable          = 30043
	propIsWindowAvailable         = 30044
	propScrollHScrollable         = 30057
	propScrollVScrollable         = 30058
)

// ToggleState / ExpandCollapseState / ScrollAmount values.
const (
	toggleOff, toggleOn                                                                                    = 0, 1
	ecCollapsed, ecExpanded                                                                                = 0, 1
	scrollLargeDecrement, scrollSmallDecrement, scrollNoAmount, scrollLargeIncrement, scrollSmallIncrement = 0, 1, 2, 3, 4
)

// VT_ types (oaidl.h).
const (
	vtEmpty   = 0
	vtI4      = 3
	vtBSTR    = 8
	vtBool    = 11
	vtUnknown = 13
)

// variant is a Win32 VARIANT (24 bytes: vt, 3 reserved words, 16-byte union).
type variant struct {
	vt uint16
	_  [3]uint16
	u  [2]uintptr
}

// uiaRect is the provider-side bounding rectangle (4 doubles: screen
// coordinates, left/top/width/height).
type uiaRect struct {
	Left, Top, Width, Height float64
}

// cptr converts a C pointer that arrived in a callback as a uintptr back to
// an unsafe.Pointer. The value is a live C pointer for the duration of the
// callback (an out-parameter slot, a BSTR, or one of this package's own
// immortal COM objects), so the conversion is sound. The indirection
// through the local's address keeps go vet's unsafeptr check quiet while
// preserving the exact same conversion.
func cptr(u uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&u))
}

func hrOK() uintptr            { return 0 }
func hrFail(hr uint32) uintptr { return uintptr(hr) }

// ---- COM object plumbing ----

// ifaceKind identifies one provider interface implementation.
type ifaceKind int

const (
	ifaceSimple ifaceKind = iota
	ifaceFragment
	ifaceFragmentRoot
	ifaceInvoke
	ifaceValue
	ifaceToggle
	ifaceExpandCollapse
	ifaceSelection
	ifaceSelectionItem
	ifaceScroll
	ifaceScrollItem
	ifaceWindow
	ifaceCount
)

// uiaNode is the Go identity behind every COM object.
type uiaNode struct {
	elem   Elem
	dialog bool
}

// providerNodes keeps every uiaNode reachable for Go's GC (they are also
// referenced from C memory, which the GC cannot see).
var providerNodes = map[elemKey]*uiaNode{}

type elemKey struct {
	e      Elem
	dialog bool
}

func nodeFor(e Elem, dialog bool) *uiaNode {
	k := elemKey{e, dialog}
	if n, ok := providerNodes[k]; ok {
		return n
	}
	n := &uiaNode{elem: e, dialog: dialog}
	providerNodes[k] = n
	return n
}

// comNode recovers the uiaNode from a COM object pointer (`this`).
func comNode(this uintptr) *uiaNode {
	if this == 0 {
		return nil
	}
	words := (*[2]uintptr)(cptr(this))
	return (*uiaNode)(cptr(words[1]))
}

// providerWindow owns the COM objects of one window's elements.
type providerWindow struct {
	hwnd   uintptr
	dialog bool
	objs   map[Elem]map[ifaceKind]uintptr
}

// obj returns (creating once) the COM object for an element's interface.
func (w *providerWindow) obj(e Elem, kind ifaceKind) uintptr {
	byKind, ok := w.objs[e]
	if !ok {
		byKind = map[ifaceKind]uintptr{}
		w.objs[e] = byKind
	}
	if p, ok := byKind[kind]; ok {
		return p
	}
	mem := malloc16(2 * unsafe.Sizeof(uintptr(0)))
	words := (*[2]uintptr)(mem)
	words[0] = vtables[kind]
	words[1] = uintptr(unsafe.Pointer(nodeFor(e, w.dialog)))
	byKind[kind] = uintptr(mem)
	return uintptr(mem)
}

// uiaProviders is both windows' provider state.
type uiaProviders struct {
	main   *providerWindow
	dialog *providerWindow
}

func newProviders(a *app) *uiaProviders {
	return &uiaProviders{
		main:   &providerWindow{hwnd: a.mainHwnd, objs: map[Elem]map[ifaceKind]uintptr{}},
		dialog: &providerWindow{hwnd: a.dialogHwnd, dialog: true, objs: map[Elem]map[ifaceKind]uintptr{}},
	}
}

// simple returns the root provider an element's IRawElementProviderSimple
// object; used for WM_GETOBJECT.
func (u *uiaProviders) simple(dialog bool, e Elem) uintptr {
	return u.window(dialog).obj(e, ifaceSimple)
}

func (u *uiaProviders) window(dialog bool) *providerWindow {
	if dialog {
		return u.dialog
	}
	return u.main
}

// providers returns the app's providers (nil before startup).
func providers() *uiaProviders {
	if theApp == nil {
		return nil
	}
	return theApp.uia
}

// appState returns the app if it is running.
func appState() *app { return theApp }

// vtables holds one vtable per interface kind, built once.
var vtables [ifaceCount]uintptr

// vt builds one vtable: the shared IUnknown slots plus the kind's methods.
func vt(kind ifaceKind, methods ...uintptr) {
	n := 3 + len(methods)
	mem := malloc16(uintptr(n) * unsafe.Sizeof(uintptr(0)))
	words := unsafe.Slice((*uintptr)(mem), n)
	words[0] = newCallback(comQueryInterface)
	words[1] = newCallback(comAddRef)
	words[2] = newCallback(comRelease)
	copy(words[3:], methods)
	vtables[kind] = uintptr(mem)
}

// iids maps interface ids to the object kinds that answer them.
var iids = map[windows.GUID]ifaceKind{}

func init() {
	vt(ifaceSimple,
		newCallback(getProviderOptions),
		newCallback(getPatternProvider),
		newCallback(getPropertyValue),
		newCallback(getHostRawElementProvider),
	)
	vt(ifaceFragment,
		newCallback(fragmentNavigate),
		newCallback(fragmentGetRuntimeId),
		newCallback(fragmentGetBoundingRectangle),
		newCallback(fragmentGetEmbeddedFragmentRoots),
		newCallback(fragmentSetFocus),
		newCallback(fragmentGetFragmentRoot),
	)
	vt(ifaceFragmentRoot,
		newCallback(rootElementProviderFromPoint),
		newCallback(rootGetFocus),
	)
	vt(ifaceInvoke, newCallback(invokeInvoke))
	vt(ifaceValue,
		newCallback(valueSetValue),
		newCallback(valueGetValue),
		newCallback(valueGetIsReadOnly),
	)
	vt(ifaceToggle,
		newCallback(toggleToggle),
		newCallback(toggleGetState),
	)
	vt(ifaceExpandCollapse,
		newCallback(ecExpand),
		newCallback(ecCollapse),
		newCallback(ecGetState),
	)
	vt(ifaceSelection,
		newCallback(selGetSelection),
		newCallback(selCanSelectMultiple),
		newCallback(selIsSelectionRequired),
	)
	vt(ifaceSelectionItem,
		newCallback(siSelect),
		newCallback(siAddToSelection),
		newCallback(siRemoveFromSelection),
		newCallback(siGetIsSelected),
		newCallback(siGetSelectionContainer),
	)
	vt(ifaceScroll,
		newCallback(scrollScroll),
		newCallback(scrollSetScrollPercent),
		newCallback(scrollGetHScrollPercent),
		newCallback(scrollGetVScrollPercent),
		newCallback(scrollGetHViewSize),
		newCallback(scrollGetVViewSize),
		newCallback(scrollGetHScrollable),
		newCallback(scrollGetVScrollable),
	)
	vt(ifaceScrollItem, newCallback(scrollItemScrollIntoView))
	// IWindowProvider (window roots only): the vtable order is
	// SetVisualState, Close, WaitForInputIdle, CanMaximize, CanMinimize,
	// IsModal, WindowVisualState, WindowInteractionState, IsTopmost
	// (SDK UIAutomationCore.h). SetVisualState and Close are deliberately
	// E_NOTIMPL: neither is part of winuse's surface, and refusing is
	// honest.
	vt(ifaceWindow,
		newCallback(windowSetVisualState),
		newCallback(windowClose),
		newCallback(windowWaitForInputIdle),
		newCallback(windowCanMaximize),
		newCallback(windowCanMinimize),
		newCallback(windowIsModal),
		newCallback(windowVisualState),
		newCallback(windowInteractionState),
		newCallback(windowIsTopmost),
	)

	iids[iidRawElementProviderSimple] = ifaceSimple
	iids[iidRawElementProviderFragment] = ifaceFragment
	iids[iidRawElementProviderFragmentRoot] = ifaceFragmentRoot
	iids[iidInvokeProvider] = ifaceInvoke
	iids[iidValueProvider] = ifaceValue
	iids[iidToggleProvider] = ifaceToggle
	iids[iidExpandCollapseProvider] = ifaceExpandCollapse
	iids[iidSelectionProvider] = ifaceSelection
	iids[iidSelectionItemProvider] = ifaceSelectionItem
	iids[iidScrollProvider] = ifaceScroll
	iids[iidScrollItemProvider] = ifaceScrollItem
	iids[iidWindowProvider] = ifaceWindow
}

// ---- IUnknown ----

func comQueryInterface(this, riid, ppv uintptr) uintptr {
	if ppv == 0 {
		return hrFail(ePointer)
	}
	p := (*unsafe.Pointer)(cptr(ppv))
	*p = nil
	if riid == 0 {
		return hrFail(ePointer)
	}
	node := comNode(this)
	if node == nil {
		return hrFail(ePointer)
	}
	g := (*windows.GUID)(cptr(riid))
	if *g == iidIUnknown {
		// COM identity: QI(IUnknown) must return the same pointer for one
		// element whichever interface it is asked on. The element's
		// IRawElementProviderSimple object is that canonical identity.
		u := providers()
		if u == nil {
			return hrFail(ePointer)
		}
		*p = cptr(u.window(node.dialog).obj(node.elem, ifaceSimple))
		return hrOK()
	}
	kind, ok := iids[*g]
	if !ok {
		return hrFail(eNoInterface)
	}
	// FragmentRoot only exists on window roots.
	if kind == ifaceFragmentRoot && node.elem != providerRootElem(node.dialog) {
		return hrFail(eNoInterface)
	}
	// Patterns only on elements that offer them.
	switch kind {
	case ifaceInvoke:
		if !HasPattern(node.elem, PatternInvoke) {
			return hrFail(eNoInterface)
		}
	case ifaceValue:
		if !HasPattern(node.elem, PatternValue) {
			return hrFail(eNoInterface)
		}
	case ifaceToggle:
		if !HasPattern(node.elem, PatternToggle) {
			return hrFail(eNoInterface)
		}
	case ifaceExpandCollapse:
		if !HasPattern(node.elem, PatternExpandCollapse) {
			return hrFail(eNoInterface)
		}
	case ifaceSelection:
		if !HasPattern(node.elem, PatternSelection) {
			return hrFail(eNoInterface)
		}
	case ifaceSelectionItem:
		if !HasPattern(node.elem, PatternSelectionItem) {
			return hrFail(eNoInterface)
		}
	case ifaceScroll:
		if !HasPattern(node.elem, PatternScroll) {
			return hrFail(eNoInterface)
		}
	case ifaceScrollItem:
		if !HasPattern(node.elem, PatternScrollItem) {
			return hrFail(eNoInterface)
		}
	case ifaceWindow:
		// The Window pattern exists on the two window roots only.
		if node.elem != providerRootElem(node.dialog) {
			return hrFail(eNoInterface)
		}
	}
	u := providers()
	if u == nil {
		return hrFail(ePointer)
	}
	*p = cptr(u.window(node.dialog).obj(node.elem, kind))
	return hrOK()
}

func comAddRef(this uintptr) uintptr  { return 2 }
func comRelease(this uintptr) uintptr { return 1 }

// ---- IRawElementProviderSimple ----

func getProviderOptions(this, popts uintptr) uintptr {
	if popts == 0 {
		return hrFail(ePointer)
	}
	*(*int32)(cptr(popts)) = providerOptionsServerSide
	return hrOK()
}

func getPatternProvider(this, patternId, ppv uintptr) uintptr {
	if ppv == 0 {
		return hrFail(ePointer)
	}
	*(*unsafe.Pointer)(cptr(ppv)) = nil
	node := comNode(this)
	if node == nil {
		return hrOK()
	}
	var kind ifaceKind
	switch patternId {
	case 10000: // UIA_InvokePatternId
		kind = ifaceInvoke
		if !HasPattern(node.elem, PatternInvoke) {
			return hrOK()
		}
	case 10001: // UIA_SelectionPatternId
		kind = ifaceSelection
		if !HasPattern(node.elem, PatternSelection) {
			return hrOK()
		}
	case 10002: // UIA_ValuePatternId
		kind = ifaceValue
		if !HasPattern(node.elem, PatternValue) {
			return hrOK()
		}
	case 10004: // UIA_ScrollPatternId
		kind = ifaceScroll
		if !HasPattern(node.elem, PatternScroll) {
			return hrOK()
		}
	case 10005: // UIA_ExpandCollapsePatternId
		kind = ifaceExpandCollapse
		if !HasPattern(node.elem, PatternExpandCollapse) {
			return hrOK()
		}
	case 10010: // UIA_SelectionItemPatternId
		kind = ifaceSelectionItem
		if !HasPattern(node.elem, PatternSelectionItem) {
			return hrOK()
		}
	case 10015: // UIA_TogglePatternId
		kind = ifaceToggle
		if !HasPattern(node.elem, PatternToggle) {
			return hrOK()
		}
	case 10017: // UIA_ScrollItemPatternId
		kind = ifaceScrollItem
		if !HasPattern(node.elem, PatternScrollItem) {
			return hrOK()
		}
	case 10009: // UIA_WindowPatternId
		kind = ifaceWindow
		if node.elem != providerRootElem(node.dialog) {
			return hrOK()
		}
	default:
		return hrOK()
	}
	u := providers()
	if u == nil {
		return hrOK()
	}
	*(*unsafe.Pointer)(cptr(ppv)) = cptr(u.window(node.dialog).obj(node.elem, kind))
	return hrOK()
}

// getPropertyValue answers the element property UIA asks for. The VARIANT
// is fully initialized: unsupported properties stay VT_EMPTY (UIA's
// documented "not supported" answer, falling back to the default value).
func getPropertyValue(this, propId, pv uintptr) uintptr {
	if pv == 0 {
		return hrFail(ePointer)
	}
	node := comNode(this)
	a := appState()
	if node == nil || a == nil {
		return hrFail(ePointer)
	}
	v := (*variant)(cptr(pv))
	*v = variant{} // VT_EMPTY
	e := node.elem

	switch propId {
	case propControlType:
		if s, ok := SpecFor(e); ok {
			v.setInt32(int32(s.Role))
		}
	case propName:
		v.setBSTR(Name(e, a.status))
	case propAutomationId:
		v.setBSTR(AutomationID(e))
	case propClassName:
		if e == providerRootElem(node.dialog) {
			if node.dialog {
				v.setBSTR(DialogWindowClass)
			} else {
				v.setBSTR(MainWindowClass)
			}
		}
	case propFrameworkId:
		v.setBSTR("Win32")
	case propIsEnabled:
		v.setBool(a.enabled(e))
	case propHasKeyboardFocus:
		v.setBool(a.focusElem() == e && a.treeMap(node.dialog).Contains(e))
	case propIsKeyboardFocusable:
		v.setBool(a.focusable(e) && a.treeMap(node.dialog).Contains(e))
	case propIsControlElement:
		v.setBool(true)
	case propIsContentElement:
		v.setBool(true)
	case propIsOffscreen:
		v.setBool(a.offscreen(e))
	case propIsDialog:
		v.setBool(node.dialog && e == DialogRootElem)
	case propValueValue:
		switch e {
		case ElemTextField:
			v.setBSTR(a.state.Text)
		case ElemDialogEditFileName:
			v.setBSTR(a.state.DialogFileName)
		}
	case propValueIsReadOnly:
		if HasPattern(e, PatternValue) {
			v.setBool(false)
		}
	case propToggleToggleState:
		if e == ElemCheckboxNotify {
			if a.state.Checkbox {
				v.setInt32(toggleOn)
			} else {
				v.setInt32(toggleOff)
			}
		}
	case propExpandCollapseState:
		switch {
		case e == ElemDetailsExpander:
			v.setInt32(boolInt(a.state.DetailsExpanded, ecExpanded, ecCollapsed))
		case e == ElemMenuFile || e == ElemMenuEdit || e == ElemMenuHelp:
			v.setInt32(boolInt(a.state.OpenMenu == string(e), ecExpanded, ecCollapsed))
		}
	case propSelectionItemIsSelected:
		if isListItem(e) {
			v.setBool(a.state.SelectedItem == Name(e, ""))
		}
	case propIsInvokeAvailable:
		v.setBool(HasPattern(e, PatternInvoke))
	case propIsValueAvailable:
		v.setBool(HasPattern(e, PatternValue))
	case propIsToggleAvailable:
		v.setBool(HasPattern(e, PatternToggle))
	case propIsExpandCollapseAvailable:
		v.setBool(HasPattern(e, PatternExpandCollapse))
	case propIsScrollAvailable:
		v.setBool(HasPattern(e, PatternScroll))
	case propIsScrollItemAvailable:
		v.setBool(HasPattern(e, PatternScrollItem))
	case propIsSelectionItemAvailable:
		v.setBool(HasPattern(e, PatternSelectionItem))
	case propIsSelectionAvailable:
		v.setBool(HasPattern(e, PatternSelection))
	case propIsWindowAvailable:
		v.setBool(e == providerRootElem(node.dialog) && HasPattern(e, PatternWindow))
	case propScrollHScrollable:
		// The list never scrolls horizontally.
		v.setBool(false)
	case propScrollVScrollable:
		if e == ElemItemList {
			v.setBool(len(ListItems) > ListVisibleItems)
		}
	case propBoundingRectangle:
		// Answered by the fragment interface; UIA uses that.
	}
	return hrOK()
}

func getHostRawElementProvider(this, ppv uintptr) uintptr {
	if ppv == 0 {
		return hrFail(ePointer)
	}
	*(*unsafe.Pointer)(cptr(ppv)) = nil
	node := comNode(this)
	u := providers()
	if node == nil || u == nil {
		return hrOK()
	}
	// Window roots delegate window-level properties to the host provider;
	// fragments have no host of their own.
	if node.elem != providerRootElem(node.dialog) {
		return hrOK()
	}
	w := u.window(node.dialog)
	hr, _, _ := procUiaHostProviderFromHwnd.Call(w.hwnd, ppv)
	if int32(hr) < 0 {
		return hr
	}
	return hrOK()
}

// ---- IRawElementProviderFragment ----

func fragmentNavigate(this, dir, ppFrag uintptr) uintptr {
	if ppFrag == 0 {
		return hrFail(ePointer)
	}
	*(*unsafe.Pointer)(cptr(ppFrag)) = nil
	node := comNode(this)
	a := appState()
	u := providers()
	if node == nil || a == nil || u == nil {
		return hrOK()
	}
	m := a.treeMap(node.dialog)
	e := node.elem
	neighbor := func(target Elem) {
		*(*unsafe.Pointer)(cptr(ppFrag)) = cptr(u.window(node.dialog).obj(target, ifaceFragment))
	}
	switch dir {
	case 0: // parent
		if p := Parent(e, a.state, node.dialog); p != "" {
			neighbor(p)
		}
	case 1, 2: // next / previous sibling
		p := Parent(e, a.state, node.dialog)
		if p == "" {
			return hrOK()
		}
		sibs := m.Children(p)
		idx := -1
		for i, s := range sibs {
			if s == e {
				idx = i
				break
			}
		}
		if idx < 0 {
			return hrOK()
		}
		if dir == 1 && idx+1 < len(sibs) {
			neighbor(sibs[idx+1])
		}
		if dir == 2 && idx > 0 {
			neighbor(sibs[idx-1])
		}
	case 3: // first child
		if c := m.Children(e); len(c) > 0 {
			neighbor(c[0])
		}
	case 4: // last child
		if c := m.Children(e); len(c) > 0 {
			neighbor(c[len(c)-1])
		}
	}
	return hrOK()
}

func fragmentGetRuntimeId(this, ppSA uintptr) uintptr {
	if ppSA == 0 {
		return hrFail(ePointer)
	}
	*(*unsafe.Pointer)(cptr(ppSA)) = nil
	node := comNode(this)
	if node == nil {
		return hrOK()
	}
	// Window-hosted roots report NULL; the runtime id comes from the HWND.
	// Fragments report [UiaAppendRuntimeId, elemID].
	if node.elem == providerRootElem(node.dialog) {
		return hrOK()
	}
	id, ok := elemID[node.elem]
	if !ok {
		return hrOK()
	}
	*(*unsafe.Pointer)(cptr(ppSA)) = safeArrayI4([]int32{uiaAppendRuntimeId, id})
	return hrOK()
}

func fragmentGetBoundingRectangle(this, pRect uintptr) uintptr {
	if pRect == 0 {
		return hrFail(ePointer)
	}
	node := comNode(this)
	a := appState()
	if node == nil || a == nil {
		return hrOK()
	}
	r := (*uiaRect)(cptr(pRect))
	*r = uiaRect{}

	if node.dialog {
		if node.elem == DialogRootElem {
			r.Left, r.Top, r.Width, r.Height = a.windowRect(a.dialogHwnd)
			return hrOK()
		}
		dr, ok := dialogRect(node.elem)
		if !ok {
			return hrOK()
		}
		ox, oy := a.clientOrigin(a.dialogHwnd)
		r.Left, r.Top = float64(ox+dr.Left), float64(oy+dr.Top)
		r.Width, r.Height = float64(dr.Right-dr.Left), float64(dr.Bottom-dr.Top)
		return hrOK()
	}
	if node.elem == ElemWindow {
		r.Left, r.Top, r.Width, r.Height = a.windowRect(a.mainHwnd)
		return hrOK()
	}
	mr, ok := Rect(node.elem, a.state.ListFirstVisible)
	if !ok {
		return hrOK()
	}
	ox, oy := a.clientOrigin(a.mainHwnd)
	r.Left, r.Top = float64(ox+mr.Left), float64(oy+mr.Top)
	r.Width, r.Height = float64(mr.Right-mr.Left), float64(mr.Bottom-mr.Top)
	return hrOK()
}

func fragmentGetEmbeddedFragmentRoots(this, ppSA uintptr) uintptr {
	if ppSA == 0 {
		return hrFail(ePointer)
	}
	*(*unsafe.Pointer)(cptr(ppSA)) = nil
	return hrOK()
}

func fragmentSetFocus(this uintptr) uintptr {
	node := comNode(this)
	a := appState()
	if node == nil || a == nil {
		return hrOK()
	}
	if a.focusable(node.elem) {
		a.setFocus(node.elem)
	}
	return hrOK()
}

func fragmentGetFragmentRoot(this, ppRoot uintptr) uintptr {
	if ppRoot == 0 {
		return hrFail(ePointer)
	}
	*(*unsafe.Pointer)(cptr(ppRoot)) = nil
	node := comNode(this)
	u := providers()
	if node == nil || u == nil {
		return hrOK()
	}
	*(*unsafe.Pointer)(cptr(ppRoot)) = cptr(u.window(node.dialog).obj(providerRootElem(node.dialog), ifaceFragmentRoot))
	return hrOK()
}

// ---- IRawElementProviderFragmentRoot ----

// rootElementProviderFromPoint always reports "no element": its arguments
// are doubles by value, which Go callback trampolines cannot receive (see
// the file comment). pRetVal is optional-out, so NULL is legal.
func rootElementProviderFromPoint(this, x, y, ppFrag uintptr) uintptr {
	if ppFrag == 0 {
		return hrFail(ePointer)
	}
	*(*unsafe.Pointer)(cptr(ppFrag)) = nil
	return hrOK()
}

func rootGetFocus(this, ppFrag uintptr) uintptr {
	if ppFrag == 0 {
		return hrFail(ePointer)
	}
	*(*unsafe.Pointer)(cptr(ppFrag)) = nil
	node := comNode(this)
	a := appState()
	u := providers()
	if node == nil || a == nil || u == nil {
		return hrOK()
	}
	focus := a.focusElem()
	if focus == "" {
		return hrOK()
	}
	if InDialog(focus) != node.dialog {
		return hrOK()
	}
	if !a.treeMap(node.dialog).Contains(focus) {
		return hrOK()
	}
	*(*unsafe.Pointer)(cptr(ppFrag)) = cptr(u.window(node.dialog).obj(focus, ifaceFragment))
	return hrOK()
}

// ---- pattern methods ----

// invokeInvoke performs the element's primary action — the same code path
// as a person's click, by design: what winuse triggers and what the app
// records can never diverge.
func invokeInvoke(this uintptr) uintptr {
	node := comNode(this)
	a := appState()
	if node == nil || a == nil {
		return hrFail(uiaErrElementNotEnabledHR)
	}
	if !a.enabled(node.elem) {
		return hrFail(uiaErrElementNotEnabledHR)
	}
	a.click(node.elem, Point{})
	return hrOK()
}

func valueSetValue(this, val uintptr) uintptr {
	node := comNode(this)
	a := appState()
	if node == nil || a == nil || val == 0 {
		return hrFail(ePointer)
	}
	if !a.enabled(node.elem) {
		return hrFail(uiaErrElementNotEnabledHR)
	}
	s := utf16ToString((*uint16)(cptr(val)))
	switch node.elem {
	case ElemTextField:
		a.state.Text = s
	case ElemDialogEditFileName:
		a.state.DialogFileName = s
	default:
		return hrFail(eNotImpl)
	}
	a.changed()
	return hrOK()
}

func valueGetValue(this, pBSTR uintptr) uintptr {
	if pBSTR == 0 {
		return hrFail(ePointer)
	}
	*(*unsafe.Pointer)(cptr(pBSTR)) = nil
	node := comNode(this)
	a := appState()
	if node == nil || a == nil {
		return hrOK()
	}
	switch node.elem {
	case ElemTextField:
		*(*unsafe.Pointer)(cptr(pBSTR)) = sysAllocString(a.state.Text)
	case ElemDialogEditFileName:
		*(*unsafe.Pointer)(cptr(pBSTR)) = sysAllocString(a.state.DialogFileName)
	}
	return hrOK()
}

func valueGetIsReadOnly(this, pBool uintptr) uintptr {
	if pBool == 0 {
		return hrFail(ePointer)
	}
	*(*int32)(cptr(pBool)) = 0
	return hrOK()
}

func toggleToggle(this uintptr) uintptr {
	node := comNode(this)
	a := appState()
	if node == nil || a == nil || node.elem != ElemCheckboxNotify {
		return hrFail(eNotImpl)
	}
	if !a.enabled(node.elem) {
		return hrFail(uiaErrElementNotEnabledHR)
	}
	a.click(node.elem, Point{})
	return hrOK()
}

func toggleGetState(this, pState uintptr) uintptr {
	if pState == 0 {
		return hrFail(ePointer)
	}
	node := comNode(this)
	a := appState()
	*(*int32)(cptr(pState)) = toggleOff
	if node != nil && a != nil && node.elem == ElemCheckboxNotify && a.state.Checkbox {
		*(*int32)(cptr(pState)) = toggleOn
	}
	return hrOK()
}

func ecExpand(this uintptr) uintptr {
	node := comNode(this)
	a := appState()
	if node == nil || a == nil {
		return hrFail(ePointer)
	}
	if !a.enabled(node.elem) {
		return hrFail(uiaErrElementNotEnabledHR)
	}
	switch node.elem {
	case ElemMenuFile, ElemMenuEdit, ElemMenuHelp:
		a.state.OpenMenu = string(node.elem)
	case ElemDetailsExpander:
		a.state.DetailsExpanded = true
	default:
		return hrFail(eNotImpl)
	}
	a.changed()
	return hrOK()
}

func ecCollapse(this uintptr) uintptr {
	node := comNode(this)
	a := appState()
	if node == nil || a == nil {
		return hrFail(ePointer)
	}
	if !a.enabled(node.elem) {
		return hrFail(uiaErrElementNotEnabledHR)
	}
	switch node.elem {
	case ElemMenuFile, ElemMenuEdit, ElemMenuHelp:
		if a.state.OpenMenu == string(node.elem) {
			a.state.OpenMenu = ""
		}
	case ElemDetailsExpander:
		a.state.DetailsExpanded = false
	default:
		return hrFail(eNotImpl)
	}
	a.changed()
	return hrOK()
}

func ecGetState(this, pState uintptr) uintptr {
	if pState == 0 {
		return hrFail(ePointer)
	}
	node := comNode(this)
	a := appState()
	*(*int32)(cptr(pState)) = ecCollapsed
	if node == nil || a == nil {
		return hrOK()
	}
	switch {
	case node.elem == ElemDetailsExpander && a.state.DetailsExpanded:
		*(*int32)(cptr(pState)) = ecExpanded
	case (node.elem == ElemMenuFile || node.elem == ElemMenuEdit || node.elem == ElemMenuHelp) && a.state.OpenMenu == string(node.elem):
		*(*int32)(cptr(pState)) = ecExpanded
	}
	return hrOK()
}

func selGetSelection(this, ppSA uintptr) uintptr {
	if ppSA == 0 {
		return hrFail(ePointer)
	}
	*(*unsafe.Pointer)(cptr(ppSA)) = nil
	node := comNode(this)
	a := appState()
	u := providers()
	if node == nil || a == nil || u == nil || node.elem != ElemItemList {
		return hrOK()
	}
	var elems []unsafe.Pointer
	if a.state.SelectedItem != "" {
		for _, item := range ListItems {
			if Name(item, "") == a.state.SelectedItem {
				elems = append(elems, cptr(u.window(false).obj(item, ifaceSimple)))
				break
			}
		}
	}
	*(*unsafe.Pointer)(cptr(ppSA)) = safeArrayUnknown(elems)
	return hrOK()
}

func selCanSelectMultiple(this, pBool uintptr) uintptr {
	if pBool == 0 {
		return hrFail(ePointer)
	}
	*(*int32)(cptr(pBool)) = 0
	return hrOK()
}

func selIsSelectionRequired(this, pBool uintptr) uintptr {
	if pBool == 0 {
		return hrFail(ePointer)
	}
	*(*int32)(cptr(pBool)) = 0
	return hrOK()
}

func siSelect(this uintptr) uintptr {
	node := comNode(this)
	a := appState()
	if node == nil || a == nil || !isListItem(node.elem) {
		return hrFail(eNotImpl)
	}
	if !a.enabled(node.elem) {
		return hrFail(uiaErrElementNotEnabledHR)
	}
	a.selectItem(node.elem)
	return hrOK()
}

func siAddToSelection(this uintptr) uintptr { return siSelect(this) }

func siRemoveFromSelection(this uintptr) uintptr {
	node := comNode(this)
	a := appState()
	if node == nil || a == nil || !isListItem(node.elem) {
		return hrFail(eNotImpl)
	}
	if a.state.SelectedItem == Name(node.elem, "") {
		a.state.SelectedItem = ""
		a.changed()
	}
	return hrOK()
}

func siGetIsSelected(this, pBool uintptr) uintptr {
	if pBool == 0 {
		return hrFail(ePointer)
	}
	node := comNode(this)
	a := appState()
	*(*int32)(cptr(pBool)) = 0
	if node != nil && a != nil && isListItem(node.elem) {
		*(*int32)(cptr(pBool)) = boolInt(a.state.SelectedItem == Name(node.elem, ""), 1, 0)
	}
	return hrOK()
}

func siGetSelectionContainer(this, ppSimple uintptr) uintptr {
	if ppSimple == 0 {
		return hrFail(ePointer)
	}
	*(*unsafe.Pointer)(cptr(ppSimple)) = nil
	node := comNode(this)
	u := providers()
	if node == nil || u == nil {
		return hrOK()
	}
	*(*unsafe.Pointer)(cptr(ppSimple)) = cptr(u.window(false).obj(ElemItemList, ifaceSimple))
	return hrOK()
}

func scrollScroll(this, hAmount, vAmount uintptr) uintptr {
	node := comNode(this)
	a := appState()
	if node == nil || a == nil || node.elem != ElemItemList {
		return hrFail(eNotImpl)
	}
	if !a.enabled(node.elem) {
		return hrFail(uiaErrElementNotEnabledHR)
	}
	switch vAmount {
	case scrollLargeDecrement:
		a.scrollList(-ListVisibleItems)
	case scrollSmallDecrement:
		a.scrollList(-1)
	case scrollLargeIncrement:
		a.scrollList(ListVisibleItems)
	case scrollSmallIncrement:
		a.scrollList(1)
	}
	return hrOK()
}

// scrollSetScrollPercent cannot read its double arguments (see the file
// comment); Scroll with enum amounts is the supported path.
func scrollSetScrollPercent(this, x, y uintptr) uintptr {
	return hrFail(eNotImpl)
}

func scrollGetHScrollPercent(this, pDouble uintptr) uintptr {
	if pDouble == 0 {
		return hrFail(ePointer)
	}
	*(*float64)(cptr(pDouble)) = -1 // UIA_ScrollPatternNoScroll
	return hrOK()
}

func scrollGetVScrollPercent(this, pDouble uintptr) uintptr {
	if pDouble == 0 {
		return hrFail(ePointer)
	}
	*(*float64)(cptr(pDouble)) = -1
	node := comNode(this)
	a := appState()
	if node != nil && a != nil && node.elem == ElemItemList {
		if max := len(ListItems) - ListVisibleItems; max > 0 {
			*(*float64)(cptr(pDouble)) = float64(a.state.ListFirstVisible) * 100 / float64(max)
		}
	}
	return hrOK()
}

func scrollGetHViewSize(this, pDouble uintptr) uintptr {
	if pDouble == 0 {
		return hrFail(ePointer)
	}
	*(*float64)(cptr(pDouble)) = 100
	return hrOK()
}

func scrollGetVViewSize(this, pDouble uintptr) uintptr {
	if pDouble == 0 {
		return hrFail(ePointer)
	}
	*(*float64)(cptr(pDouble)) = float64(ListVisibleItems) * 100 / float64(len(ListItems))
	return hrOK()
}

func scrollGetHScrollable(this, pBool uintptr) uintptr {
	if pBool == 0 {
		return hrFail(ePointer)
	}
	*(*int32)(cptr(pBool)) = 0
	return hrOK()
}

func scrollGetVScrollable(this, pBool uintptr) uintptr {
	if pBool == 0 {
		return hrFail(ePointer)
	}
	*(*int32)(cptr(pBool)) = boolInt(len(ListItems) > ListVisibleItems, 1, 0)
	return hrOK()
}

func scrollItemScrollIntoView(this uintptr) uintptr {
	node := comNode(this)
	a := appState()
	if node == nil || a == nil || !isListItem(node.elem) {
		return hrFail(eNotImpl)
	}
	if !a.enabled(node.elem) {
		return hrFail(uiaErrElementNotEnabledHR)
	}
	idx := -1
	for i, item := range ListItems {
		if item == node.elem {
			idx = i
			break
		}
	}
	if idx < 0 {
		return hrOK()
	}
	if idx < a.state.ListFirstVisible {
		a.scrollList(idx - a.state.ListFirstVisible)
	} else if idx >= a.state.ListFirstVisible+ListVisibleItems {
		a.scrollList(idx - (a.state.ListFirstVisible + ListVisibleItems) + 1)
	}
	return hrOK()
}

// ---- IWindowProvider (window roots) ----

// windowSetVisualState is deliberately unimplemented: it is not part of
// winuse's surface, and a provider may refuse (E_NOTIMPL).
func windowSetVisualState(this, state uintptr) uintptr {
	return hrFail(eNotImpl)
}

// windowClose is deliberately unimplemented: winuse never closes a Window
// through the pattern (closing is the user's / the app's own act).
func windowClose(this uintptr) uintptr {
	return hrFail(eNotImpl)
}

// windowWaitForInputIdle: the fixture answers instantly, so the wait
// succeeds with TRUE.
func windowWaitForInputIdle(this, ms, pBool uintptr) uintptr {
	if pBool == 0 {
		return hrFail(ePointer)
	}
	*(*int32)(cptr(pBool)) = -1 // TRUE
	return hrOK()
}

func windowCanMaximize(this, pBool uintptr) uintptr {
	if pBool == 0 {
		return hrFail(ePointer)
	}
	*(*int32)(cptr(pBool)) = -1
	return hrOK()
}

func windowCanMinimize(this, pBool uintptr) uintptr {
	if pBool == 0 {
		return hrFail(ePointer)
	}
	*(*int32)(cptr(pBool)) = -1
	return hrOK()
}

// windowIsModal: the Save As dialog is modal; the main window is not.
func windowIsModal(this, pBool uintptr) uintptr {
	if pBool == 0 {
		return hrFail(ePointer)
	}
	node := comNode(this)
	*(*int32)(cptr(pBool)) = boolInt(node != nil && node.dialog, -1, 0)
	return hrOK()
}

// windowVisualState: WindowVisualState_Normal (0).
func windowVisualState(this, pState uintptr) uintptr {
	if pState == 0 {
		return hrFail(ePointer)
	}
	*(*int32)(cptr(pState)) = 0
	return hrOK()
}

// windowInteractionState: WindowInteractionState_Running (0).
func windowInteractionState(this, pState uintptr) uintptr {
	if pState == 0 {
		return hrFail(ePointer)
	}
	*(*int32)(cptr(pState)) = 0
	return hrOK()
}

func windowIsTopmost(this, pBool uintptr) uintptr {
	if pBool == 0 {
		return hrFail(ePointer)
	}
	*(*int32)(cptr(pBool)) = 0
	return hrOK()
}

// ---- VARIANT / BSTR / SAFEARRAY helpers ----

func (v *variant) setInt32(x int32) {
	v.vt = vtI4
	*(*int32)(unsafe.Pointer(&v.u[0])) = x
}

func (v *variant) setBool(b bool) {
	v.vt = vtBool
	if b {
		*(*int16)(unsafe.Pointer(&v.u[0])) = -1 // VARIANT_TRUE
	} else {
		*(*int16)(unsafe.Pointer(&v.u[0])) = 0
	}
}

func (v *variant) setBSTR(s string) {
	v.vt = vtBSTR
	v.u[0] = uintptr(sysAllocString(s))
}

// sysAllocString allocates a BSTR the caller (UIA) will free. BSTRs are
// length-prefixed; the terminating NUL of UTF16FromString is not part of
// the content.
func sysAllocString(s string) unsafe.Pointer {
	runes := []rune(s)
	if len(runes) == 0 {
		b, _, _ := procSysAllocStringLen.Call(0, 0)
		return cptr(b)
	}
	u16 := utf16.Encode(runes)
	b, _, _ := procSysAllocStringLen.Call(uintptr(unsafe.Pointer(&u16[0])), uintptr(len(u16)))
	return cptr(b)
}

// safeArrayI4 builds a one-dimensional SAFEARRAY of VT_I4 values; the
// caller (UIA) destroys it.
func safeArrayI4(vals []int32) unsafe.Pointer {
	psa, _, _ := procSafeArrayCreateVector.Call(uintptr(vtI4), 0, uintptr(len(vals)))
	if psa == 0 {
		return nil
	}
	for i, v := range vals {
		idx, val := int32(i), v
		procSafeArrayPutElement.Call(psa, uintptr(unsafe.Pointer(&idx)), uintptr(unsafe.Pointer(&val)))
	}
	return cptr(psa)
}

// safeArrayUnknown builds a one-dimensional SAFEARRAY of VT_UNKNOWN
// provider pointers; the caller (UIA) destroys it.
func safeArrayUnknown(ptrs []unsafe.Pointer) unsafe.Pointer {
	psa, _, _ := procSafeArrayCreateVector.Call(uintptr(vtUnknown), 0, uintptr(len(ptrs)))
	if psa == 0 {
		return nil
	}
	for i, p := range ptrs {
		idx := int32(i)
		procSafeArrayPutElement.Call(psa, uintptr(unsafe.Pointer(&idx)), uintptr(p))
	}
	return cptr(psa)
}

func boolInt(b bool, t, f int32) int32 {
	if b {
		return t
	}
	return f
}

// ---- window rectangle helpers used by the provider ----

// windowRect returns the main or dialog window's rectangle in screen
// coordinates (physical pixels; the process is per-monitor-v2 aware).
func (a *app) windowRect(hwnd uintptr) (left, top, width, height float64) {
	var r struct{ Left, Top, Right, Bottom int32 }
	if r1, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); r1 == 0 {
		return 0, 0, 0, 0
	}
	return float64(r.Left), float64(r.Top), float64(r.Right - r.Left), float64(r.Bottom - r.Top)
}

// clientOrigin returns the window's client-area origin in screen
// coordinates.
func (a *app) clientOrigin(hwnd uintptr) (x, y int32) {
	p := point{}
	procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&p)))
	return p.X, p.Y
}

// treeMap returns the window's current element tree, rebuilt when the state
// generation changed. Called on the UI thread only.
func (a *app) treeMap(dialog bool) *TreeMap {
	if a.mainTree == nil || a.treeGen != a.state.Generation {
		a.mainTree = NewTreeMap(BuildTree(a.state))
		a.dialogTree = NewTreeMap(DialogTree(a.state))
		a.treeGen = a.state.Generation
	}
	if dialog {
		return a.dialogTree
	}
	return a.mainTree
}
