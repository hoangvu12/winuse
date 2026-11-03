package fixture

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The bare control (bare.go) offline: its place in the Classic Dialog and
// what its provider answers. No window is created; the provider is called
// on a zero HWND.

func TestClassicBareControlIsLastAndInside(t *testing.T) {
	last := classicItems[len(classicItems)-1]
	if last.id != ClassicBareID || last.className != BareControlClass || last.text != BareControlName {
		t.Errorf("the last Classic Dialog item = %+v, want the bare control", last)
	}
	if last.style&wsTabStop != 0 {
		t.Errorf("the bare control is a tab stop: it would move the dialog's focus order")
	}
	_, _, _, _, items := parseTemplate(t, classicTemplate())
	cy := int16(classicTemplate()[8]) // the header's cy: after style, extended style, count, x, y and cx
	for n, it := range classicItems {
		if it.y+it.cy > cy {
			t.Errorf("item %d (id %d) ends at %d, below the dialog's %d", n, it.id, it.y+it.cy, cy)
		}
	}
	if len(items) != len(classicItems) {
		t.Errorf("%d template items, want %d", len(items), len(classicItems))
	}
}

// bstrValue reads a VT_BSTR variant's text, and false for any other type.
func bstrValue(v *variant) (string, bool) {
	if v.vt != vtBSTR || v.u[0] == 0 {
		return "", false
	}
	p := (*uint16)(cptr(v.u[0]))
	n := *(*uint32)(unsafe.Add(unsafe.Pointer(p), -4)) / 2
	return windows.UTF16ToString(unsafe.Slice(p, n+1)[:n]), true
}

func TestBareProviderProperties(t *testing.T) {
	this := uintptr(unsafe.Pointer(&[2]uintptr{bareVtable, 0}))
	for _, c := range []struct {
		prop int
		want string
	}{
		{propFrameworkId, ""}, // set and empty, the hm-09b case
		{propName, BareControlName},
		{propAutomationId, "1005"},
	} {
		var v variant
		if hr := bareGetPropertyValue(this, uintptr(c.prop), uintptr(unsafe.Pointer(&v))); hr != 0 {
			t.Fatalf("property %d: hr %#x", c.prop, hr)
		}
		if got, ok := bstrValue(&v); !ok || got != c.want {
			t.Errorf("property %d = %q (BSTR %v), want the BSTR %q", c.prop, got, ok, c.want)
		}
	}
	var v variant
	bareGetPropertyValue(this, propControlType, uintptr(unsafe.Pointer(&v)))
	if v.vt != vtI4 || *(*int32)(unsafe.Pointer(&v.u[0])) != controlTypePane {
		t.Errorf("ControlType = %+v, want Pane", v)
	}
	v = variant{vt: vtBool}
	bareGetPropertyValue(this, propIsEnabled, uintptr(unsafe.Pointer(&v)))
	if v.vt != vtEmpty {
		t.Errorf("IsEnabled = %+v, want VT_EMPTY for the host provider to answer", v)
	}
}

func TestBareProviderInterfaces(t *testing.T) {
	this := uintptr(unsafe.Pointer(&[2]uintptr{bareVtable, 0}))
	for _, iid := range []windows.GUID{iidIUnknown, iidRawElementProviderSimple} {
		var got uintptr
		if hr := bareQueryInterface(this, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&got))); hr != 0 || got != this {
			t.Errorf("QueryInterface(%v) = %#x, %#x; want S_OK and the object itself", iid, hr, got)
		}
	}
	var got uintptr
	iid := iidInvokeProvider
	if hr := bareQueryInterface(this, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&got))); hr != eNoInterface || got != 0 {
		t.Errorf("QueryInterface(IInvokeProvider) = %#x, %#x; want E_NOINTERFACE", hr, got)
	}
}
