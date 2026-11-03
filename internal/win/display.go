package win

// The displays (desktop-parity ticket 08): list-displays and the display
// form of screenshot, like agent-desktop's
// (crates/windows/src/system/display.rs). EnumDisplayMonitors lists the
// monitors, GetMonitorInfo gives each one's rectangle and the primary flag,
// and GetDpiForMonitor its effective DPI, the scale Windows applies. The
// helper is per-monitor DPI aware v2 (dpi.go), so every rectangle is in
// physical pixels, the coordinates move-window and the screenshot line
// use. Listing only reads; the ordering and the listing format are pure
// and tested offline.

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Display is one monitor of the desktop.
type Display struct {
	Index   int     // its place in the listing: 0 is the primary display
	Handle  uintptr // HMONITOR; valid while the display stays attached
	Bounds  Rect    // physical pixels, virtual-desktop coordinates
	DPI     uint32  // effective DPI (96 is 100%)
	Primary bool
}

// Scale is the display's effective scale: its DPI over 96.
func (d Display) Scale() float64 { return float64(d.DPI) / 96 }

var procEnumDisplayMonitors = user32.NewProc("EnumDisplayMonitors")

const monitorInfoFPrimary = 1 // MONITORINFOF_PRIMARY

// One EnumDisplayMonitors pass gathers the monitor handles into
// displayEnumState, under displayEnumMu. The callback is made once:
// windows.NewCallback slots are never freed.
var (
	displayEnumMu       sync.Mutex
	displayEnumState    []uintptr
	displayEnumCallback = windows.NewCallback(func(hmon, hdc, rect, lparam uintptr) uintptr {
		displayEnumState = append(displayEnumState, hmon)
		return 1 // go on
	})
)

// ListDisplays lists the attached displays, primary first (orderDisplays).
// A monitor whose rectangle or DPI cannot be read fails the listing, as in
// agent-desktop: a display dropped silently would shift every index after
// it.
func ListDisplays() ([]Display, error) {
	displayEnumMu.Lock()
	displayEnumState = nil
	r, _, err := procEnumDisplayMonitors.Call(0, 0, displayEnumCallback, 0)
	handles := displayEnumState
	displayEnumState = nil
	displayEnumMu.Unlock()
	if r == 0 {
		return nil, fmt.Errorf("EnumDisplayMonitors failed: %v", err)
	}
	if len(handles) == 0 {
		return nil, errors.New("no display is attached")
	}
	ds := make([]Display, 0, len(handles))
	for _, h := range handles {
		mi := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
		if r, _, err := procGetMonitorInfoW.Call(h, uintptr(unsafe.Pointer(&mi))); r == 0 {
			return nil, fmt.Errorf("reading a display's rectangle failed: %v", err)
		}
		var dx, dy uint32
		if err := procGetDpiForMonitor.Find(); err != nil {
			return nil, fmt.Errorf("reading a display's DPI failed: %v", err)
		}
		if hr, _, _ := procGetDpiForMonitor.Call(h, mdtEffectiveDPI,
			uintptr(unsafe.Pointer(&dx)), uintptr(unsafe.Pointer(&dy))); hr != 0 || dx == 0 {
			return nil, fmt.Errorf("reading a display's DPI failed (HRESULT 0x%08x)", uint32(hr))
		}
		ds = append(ds, Display{Handle: h, Bounds: mi.Monitor, DPI: dx, Primary: mi.Flags&monitorInfoFPrimary != 0})
	}
	return orderDisplays(ds), nil
}

// orderDisplays numbers the displays: the primary first (index 0, as in
// agent-desktop), then the others top to bottom and left to right, so an
// index stays put while the arrangement does.
func orderDisplays(ds []Display) []Display {
	sort.SliceStable(ds, func(i, j int) bool {
		a, b := ds[i], ds[j]
		if a.Primary != b.Primary {
			return a.Primary
		}
		if a.Bounds.Top != b.Bounds.Top {
			return a.Bounds.Top < b.Bounds.Top
		}
		return a.Bounds.Left < b.Bounds.Left
	})
	for i := range ds {
		ds[i].Index = i
	}
	return ds
}

// FormatDisplays renders list-displays: one line per display,
// "<index>\t<x>,<y> <w>x<h>\tscale <s>", with "\tprimary" on the primary
// display. The rectangle is in physical pixels; the scale has up to three
// significant digits (1, 1.25, 1.5, 1.75).
func FormatDisplays(ds []Display) string {
	var b strings.Builder
	for _, d := range ds {
		r := d.Bounds
		fmt.Fprintf(&b, "%d\t%d,%d %dx%d\tscale %s", d.Index, r.Left, r.Top, r.Width(), r.Height(),
			strconv.FormatFloat(d.Scale(), 'g', 3, 64))
		if d.Primary {
			b.WriteString("\tprimary")
		}
		b.WriteByte('\n')
	}
	return b.String()
}
