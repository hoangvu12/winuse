package uia

// An Element's UIA RuntimeId (desktop-parity 17): the identity of a live
// Element while it exists. Two handles to the same Element have the same
// RuntimeId; a handle whose Element is gone fails to read it. The live
// reference resolution uses it as its staleness probe, and the store uses
// it so find, region reads and live resolutions never add the same Element
// twice.

import (
	"strconv"
	"strings"
	"unsafe"
)

// elGetRuntimeId is IUIAutomationElement::GetRuntimeId(**SAFEARRAY of int).
const elGetRuntimeId = 4

var (
	procSafeArrayGetLBound    = oleaut32.NewProc("SafeArrayGetLBound")
	procSafeArrayGetUBound    = oleaut32.NewProc("SafeArrayGetUBound")
	procSafeArrayAccessData   = oleaut32.NewProc("SafeArrayAccessData")
	procSafeArrayUnaccessData = oleaut32.NewProc("SafeArrayUnaccessData")
	procSafeArrayDestroy      = oleaut32.NewProc("SafeArrayDestroy")
)

// runtimeID reads el's RuntimeId as a string key ("42.7.13"); "" with the
// HRESULT when it cannot be read (a stale Element fails with a stale
// HRESULT, hrStale).
func runtimeID(el comPtr) (string, HRESULT) {
	var sa uintptr
	if hr := call(el, elGetRuntimeId, uintptr(unsafe.Pointer(&sa))); hr.Failed() || sa == 0 {
		return "", hr
	}
	defer procSafeArrayDestroy.Call(sa)
	var lo, hi int32
	procSafeArrayGetLBound.Call(sa, 1, uintptr(unsafe.Pointer(&lo)))
	procSafeArrayGetUBound.Call(sa, 1, uintptr(unsafe.Pointer(&hi)))
	n := int(hi - lo + 1)
	if n <= 0 {
		return "", 0
	}
	var data *int32
	if r, _, _ := procSafeArrayAccessData.Call(sa, uintptr(unsafe.Pointer(&data))); r != 0 || data == nil {
		return "", HRESULT(r)
	}
	defer procSafeArrayUnaccessData.Call(sa)
	return runtimeKey(unsafe.Slice(data, n)), 0
}

// runtimeKey joins a RuntimeId's parts into the store's key.
func runtimeKey(parts []int32) string {
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteByte('.')
		}
		b.WriteString(strconv.FormatInt(int64(p), 10))
	}
	return b.String()
}
