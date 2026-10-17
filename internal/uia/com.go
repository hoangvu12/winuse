// Package uia reads accessibility trees through Windows UI Automation from
// Go, with no cgo: raw COM vtable calls (syscall.SyscallN) against the
// UIAutomationClient interfaces.
//
// All UI Automation work happens on one OS-locked, COM-initialised (MTA)
// worker thread, reached through a job queue — never from other goroutines.
// The walk is scoped to a target Window with cache requests (one chunk per
// expanded node), bounded by caps and a time limit, and uses no UIA event
// subscriptions at all (a known cause of Codex's system-wide UIA freezes).
//
// The vtable slot numbers and GUIDs below are facts from the Windows SDK
// 10.0.26100.0 UIAutomationClient.h (verified in the project's research
// notes, uia-go.md §2); the call pattern follows y3owk1n/neru (MIT)'s
// approach — no code was copied.
package uia

import (
	"fmt"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// HRESULT is a Win32 HRESULT; a negative value is a failure.
type HRESULT int32

// Failed reports whether the HRESULT marks a failure.
func (h HRESULT) Failed() bool { return h < 0 }

// String renders the HRESULT as 0x%08x, the style used in winuse errors.
func (h HRESULT) String() string { return fmt.Sprintf("0x%08x", uint32(h)) }

// comPtr is a raw COM interface pointer: it points at the object's vtable
// pointer, in non-Go memory the GC ignores. COM pointers are stored as
// comPtr (never as uintptr) so go vet stays quiet; they are only ever
// dereferenced from the worker thread that owns the apartment.
type comPtr = unsafe.Pointer

// vtable returns the function pointer at slot i of o's vtable.
// IUnknown occupies slots 0 (QueryInterface), 1 (AddRef), 2 (Release).
func vtable(o comPtr, i uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(unsafe.Pointer(o))
	return *(*uintptr)(unsafe.Add(vtbl, i*unsafe.Sizeof(uintptr(0))))
}

// call invokes vtable slot i on COM interface o. args hold the arguments
// after the implicit this pointer. The //go:uintptrescapes pragma keeps
// uintptr(unsafe.Pointer(&out)) arguments alive and un-moved for the
// duration of the syscall (same contract as syscall.Proc.Call; the pattern
// is the one safe way to pass out-params to raw vtable calls).
//
//go:uintptrescapes
func call(o comPtr, i uintptr, args ...uintptr) HRESULT {
	var buf [12]uintptr
	buf[0] = uintptr(o)
	n := copy(buf[1:], args)
	r, _, _ := syscall.SyscallN(vtable(o, i), buf[:1+n]...)
	return HRESULT(int32(r))
}

// release drops one reference on a COM interface (IUnknown::Release).
func release(o comPtr) {
	if o != nil {
		call(o, 2)
	}
}

// COM class/interface ids (SDK UIAutomationClient.h). CUIAutomation8 is the
// Win8+ class whose default interface is IUIAutomation2, which carries the
// connection/transaction timeout setters the bounded walk relies on.
var (
	clsidCUIAutomation8 = windows.GUID{Data1: 0xe22ad333, Data2: 0xb25f, Data3: 0x460c, Data4: [8]byte{0x83, 0xd0, 0x05, 0x81, 0x10, 0x73, 0x95, 0xc9}}
	iidIUIAutomation2   = windows.GUID{Data1: 0x34723aff, Data2: 0x0c9d, Data3: 0x49d0, Data4: [8]byte{0x98, 0x96, 0x7a, 0xb5, 0x2d, 0xf8, 0xcd, 0x8a}}
)

// IUIAutomation / IUIAutomation2 vtable slots (verified against SDK
// 10.0.26100.0; see uia-go.md §2). Never call slots 32–41 (event handlers).
const (
	uiaElementFromHandleBuildCache = 10 // ElementFromHandleBuildCache(hwnd, req, **el)
	uiaGetControlViewCondition     = 18 // get_ControlViewCondition(**cond)
	uiaCreateCacheRequest          = 20 // CreateCacheRequest(**req)
	uia2PutConnectionTimeout       = 61 // put_ConnectionTimeout(DWORD ms) [IUIAutomation2]
	uia2PutTransactionTimeout      = 63 // put_TransactionTimeout(DWORD ms) [IUIAutomation2]
)

// IUIAutomationElement vtable slots (cached getters are the current ones
// offset by +32).
const (
	elBuildUpdatedCache      = 9  // BuildUpdatedCache(req, **fresh)
	elGetCurrentPatternAs    = 14 // GetCurrentPatternAs(patternId, REFIID, **out)
	elGetCachedChildren      = 19 // GetCachedChildren(**arr)
	elCurrentBoundingRect    = 43 // get_CurrentBoundingRectangle(*RECT)
	elCachedControlType      = 53 // get_CachedControlType(*int32)
	elCachedName             = 55 // get_CachedName(*BSTR)
	elCachedHasKeyboardFocus = 58 // get_CachedHasKeyboardFocus(*BOOL)
	elCachedIsEnabled        = 60 // get_CachedIsEnabled(*BOOL)
	elCachedAutomationId     = 61 // get_CachedAutomationId(*BSTR)
	elCachedClassName        = 62 // get_CachedClassName(*BSTR)
	elCachedHelpText         = 63 // get_CachedHelpText(*BSTR)
	elGetClickablePoint      = 84 // GetClickablePoint(*POINT, *BOOL)
)

// IUIAutomationCacheRequest and IUIAutomationElementArray vtable slots.
const (
	reqAddProperty              = 3  // AddProperty(propertyId)
	reqPutTreeScope             = 7  // put_TreeScope(scope)
	reqPutTreeFilter            = 9  // put_TreeFilter(condition)
	reqPutAutomationElementMode = 11 // put_AutomationElementMode(mode)
	arrGetLength                = 3  // get_Length(*int32)
	arrGetElement               = 4  // GetElement(index, **el)
)

// IUIAutomationScrollItemPattern vtable slots and the pattern id used
// with GetCurrentPatternAs (uia-go.md §2).
const (
	scrollItemScrollIntoView = 3 // ScrollIntoView()
	patternScrollItem        = 10017
)

// iidScrollItemPattern is IUIAutomationScrollItemPattern's IID (SDK
// UIAutomationClient.h; uia-go.md §2).
var iidScrollItemPattern = windows.GUID{
	Data1: 0xb488300f, Data2: 0xd015, Data3: 0x4f19,
	Data4: [8]byte{0x9c, 0x29, 0xbb, 0x59, 0x5e, 0x36, 0x45, 0xef},
}

// UIA property ids cached for the tree read (SDK; uia-go.md §3). The list is
// the basic ticket-03 set; later tickets extend it (pattern availability,
// value/toggle/expand/selection state for Secondary Actions).
var treeProperties = []uint32{
	30003, // ControlType
	30005, // Name
	30008, // HasKeyboardFocus
	30010, // IsEnabled
	30011, // AutomationId
	30012, // ClassName (the root's, for Chromium/Electron detection; ticket 14)
	30013, // HelpText
}

// TreeScope_Element|Children: one cache chunk covers a node and its direct
// (control-view) children. AutomationElementMode_Full keeps the returned
// elements live, so later Actions can use them.
const (
	treeScopeElementChildren  = 3
	automationElementModeFull = 1
)

// UIA HRESULTs the walk and the actions treat specially. Signed constants
// are used because the hex values do not fit in a positive int32; tests
// pin the hex values so they cannot be mistyped.
const (
	uiaErrTimeout          = HRESULT(-0x7FECEAFB) // 0x80131505 UIA_E_TIMEOUT: a provider did not answer in time
	uiaErrNoClickablePoint = HRESULT(-0x7FFBFDFE) // 0x80040202 UIA_E_NOCLICKABLEPOINT
)

// hrStale reports whether hr means the Element behind a stored Element
// Index is gone: the documented "stale" HRESULTs (uia-go.md §2 —
// UIA_E_ELEMENTNOTAVAILABLE plus the "target process gone" RPC errors,
// which a disconnected provider also surfaces).
func hrStale(hr HRESULT) bool {
	switch uint32(hr) {
	case 0x80040201, // UIA_E_ELEMENTNOTAVAILABLE
		0x80010108, // RPC_E_DISCONNECTED
		0x800401FD, // CO_E_OBJNOTCONNECTED
		0x80010105: // RPC_E_SERVERFAULT
		return true
	}
	return false
}

var (
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	oleaut32             = windows.NewLazySystemDLL("oleaut32.dll")
	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	procSysStringLen     = oleaut32.NewProc("SysStringLen")
	procSysFreeString    = oleaut32.NewProc("SysFreeString")
)

const coinitMultithreaded = 0 // COINIT_MULTITHREADED: the MTA (no message pump)

// coInitializeEx initialises COM (MTA) on the calling thread.
func coInitializeEx() HRESULT {
	r, _, _ := procCoInitializeEx.Call(0, coinitMultithreaded)
	return HRESULT(int32(r))
}

// coUninitialize balances a successful coInitializeEx.
func coUninitialize() {
	windows.CoUninitialize()
}

// coCreateAutomation creates the IUIAutomation2 object.
func coCreateAutomation() (comPtr, HRESULT) {
	var auto comPtr
	r, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidCUIAutomation8)),
		0,
		1, // CLSCTX_INPROC_SERVER
		uintptr(unsafe.Pointer(&iidIUIAutomation2)),
		uintptr(unsafe.Pointer(&auto)),
	)
	return auto, HRESULT(int32(r))
}

// bstrToString reads a BSTR as a Go string (BSTRs may contain NULs, so the
// declared length is used, not NUL-scanning). The BSTR stays owned by the
// caller.
func bstrToString(p *uint16) string {
	if p == nil {
		return ""
	}
	n, _, _ := procSysStringLen.Call(uintptr(unsafe.Pointer(p)))
	return string(utf16.Decode(unsafe.Slice(p, n)))
}

// freeBSTR frees a BSTR owned by the caller (a getter's out-param).
func freeBSTR(p *uint16) {
	if p != nil {
		procSysFreeString.Call(uintptr(unsafe.Pointer(p)))
	}
}
