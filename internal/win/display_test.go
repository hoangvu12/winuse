package win

import "testing"

// TestOrderDisplaysPrimaryFirst: the primary display is index 0 wherever
// it sits; the others follow top to bottom, then left to right.
func TestOrderDisplaysPrimaryFirst(t *testing.T) {
	ds := orderDisplays([]Display{
		{Handle: 3, Bounds: Rect{Left: 1920, Top: 0, Right: 3840, Bottom: 1080}},
		{Handle: 4, Bounds: Rect{Left: -1280, Top: -300, Right: 0, Bottom: 724}},
		{Handle: 1, Bounds: Rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080}, Primary: true},
		{Handle: 2, Bounds: Rect{Left: 0, Top: 1080, Right: 1920, Bottom: 2160}},
	})
	for i, want := range []uintptr{1, 4, 3, 2} {
		if ds[i].Handle != want || ds[i].Index != i {
			t.Errorf("display %d = handle %d index %d, want handle %d", i, ds[i].Handle, ds[i].Index, want)
		}
	}
}

// TestFormatDisplays pins the listing and the DPI maths: the scale is
// the effective DPI over 96, the rectangle physical pixels, negative
// origins included.
func TestFormatDisplays(t *testing.T) {
	ds := []Display{
		{Index: 0, Bounds: Rect{Left: 0, Top: 0, Right: 2880, Bottom: 1800}, DPI: 192, Primary: true},
		{Index: 1, Bounds: Rect{Left: -1920, Top: -120, Right: 0, Bottom: 960}, DPI: 96},
		{Index: 2, Bounds: Rect{Left: 2880, Top: 0, Right: 5440, Bottom: 1440}, DPI: 120},
		{Index: 3, Bounds: Rect{Left: 5440, Top: 0, Right: 7360, Bottom: 1080}, DPI: 168},
	}
	want := "0\t0,0 2880x1800\tscale 2\tprimary\n" +
		"1\t-1920,-120 1920x1080\tscale 1\n" +
		"2\t2880,0 2560x1440\tscale 1.25\n" +
		"3\t5440,0 1920x1080\tscale 1.75\n"
	if got := FormatDisplays(ds); got != want {
		t.Errorf("FormatDisplays =\n%s\nwant\n%s", got, want)
	}
	for dpi, scale := range map[uint32]float64{96: 1, 120: 1.25, 144: 1.5, 168: 1.75, 192: 2, 288: 3} {
		if got := (Display{DPI: dpi}).Scale(); got != scale {
			t.Errorf("DPI %d: scale %v, want %v", dpi, got, scale)
		}
	}
}
