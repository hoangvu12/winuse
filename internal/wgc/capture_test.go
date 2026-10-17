package wgc

// The offline tests of the Windows.Graphics.Capture method (ticket 09):
// the pure parts are unit-tested — the GUID literals against the
// documented IIDs (a typo guard for hand-written struct literals), the
// SizeInt32 register packing, the frame-size cross-check, the BGRA→RGBA
// conversion, the tight crop — plus the environment knobs. The WinRT and
// Direct3D calls themselves never run here: they touch the desktop and
// the GPU and belong to the user-run e2e suite (test/e2e/wgc_test.go).

import (
	"errors"
	"image"
	"image/color"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// TestIIDsMatchDocumentedValues guards every hand-written GUID literal
// against its documented IID string (wgc.md §3, verified against the SDK
// 10.0.26100.0 headers): a mistyped byte in a GUID is the silentest
// possible failure of hand-written COM.
func TestIIDsMatchDocumentedValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  windows.GUID
		iid  string
	}{
		{"IGraphicsCaptureItemInterop", iidItemInterop, "{3628E81B-3CAC-4C60-B7F4-23CE0E0C3356}"},
		{"IGraphicsCaptureItem", iidItem, "{79C3F95B-31F7-4EC2-A464-632EF5D30760}"},
		{"IDirect3D11CaptureFramePoolStatics2", iidPoolStatics2, "{589B103F-6BBC-5DF5-A991-02E28B3B66D5}"},
		{"IGraphicsCaptureSessionStatics", iidSessionStatics, "{2224A540-5974-49AA-B232-0882536F4CB5}"},
		{"IGraphicsCaptureSession2", iidSession2, "{2C39AE40-7D2E-5044-804E-8B6799D4CF9E}"},
		{"IGraphicsCaptureSession3", iidSession3, "{F2CDD966-22AE-5EA1-9596-3A289344C3BE}"},
		{"IGraphicsCaptureSession6", iidSession6, "{D7419236-BE20-5E9F-BCD6-C4E98FD6AFDC}"},
		{"IGraphicsCaptureAccessStatics", iidAccessStatics, "{743ED370-06EC-5040-A58A-901F0F757095}"},
		{"IClosable", iidClosable, "{30D5A829-7FA4-4026-83BB-D75BAE4EA99E}"},
		{"IAsyncInfo", iidAsyncInfo, "{00000036-0000-0000-C000-000000000046}"},
		{"IDirect3DDevice", iidDirect3DDevice, "{A37624AB-8D5F-4650-9D3E-9EAE3D9BC670}"},
		{"IDXGIDevice", iidDXGIDevice, "{54EC77FA-1377-44E6-8C32-88FD5F44C84C}"},
		{"IDirect3DDxgiInterfaceAccess", iidDXGIAccess, "{A9B3D012-3DF2-4EE3-B8D1-8695F457D3C1}"},
		{"ID3D11Texture2D", iidTexture2D, "{6F15AAF2-D208-4E89-9AB4-489535D34F9C}"},
	} {
		want, err := windows.GUIDFromString(tc.iid)
		if err != nil {
			t.Fatalf("%s: parsing %s: %v", tc.name, tc.iid, err)
		}
		if tc.got != want {
			t.Errorf("%s = %v, want %s", tc.name, tc.got, tc.iid)
		}
	}
}

// TestSizeInt32RegPacksBothHalves pins the by-value struct packing: the
// width is the low 32 bits of the one register, the height the high 32.
func TestSizeInt32RegPacksBothHalves(t *testing.T) {
	got := sizeInt32{W: 800, H: 470}.reg()
	want := uintptr(800) | uintptr(470)<<32
	if got != want {
		t.Errorf("reg() = 0x%x, want 0x%x", got, want)
	}
	// Negative halves stay distinguishable too (they never occur: sizes
	// are non-negative).
	got = sizeInt32{W: 0, H: 1}.reg()
	if got != uintptr(1)<<32 {
		t.Errorf("reg() = 0x%x, want 0x%x", got, uintptr(1)<<32)
	}
}

// TestFrameSize pins the cross-check between the captured crop and the
// Window's extended frame bounds: within tolerance the frame is clipped
// to the Window (both directions), beyond it the frame is distrusted.
func TestFrameSize(t *testing.T) {
	cases := []struct {
		name                     string
		cropW, cropH, efbW, efbH int
		wantW, wantH             int
		wantOK                   bool
	}{
		{"exact match", 800, 470, 800, 470, 800, 470, true},
		{"DWM rounding, one px", 801, 470, 800, 470, 800, 470, true},
		{"crop a bit smaller", 800, 466, 800, 470, 800, 466, true},
		{"within tolerance, clipped", 804, 474, 800, 470, 800, 470, true},
		{"within tolerance, grown", 798, 468, 802, 472, 798, 468, true},
		{"beyond tolerance", 805, 470, 800, 470, 0, 0, false},
		{"way beyond", 1024, 768, 800, 470, 0, 0, false},
		{"empty crop", 0, 470, 800, 470, 0, 0, false},
		{"empty window", 800, 470, 0, 470, 0, 0, false},
	}
	for _, tc := range cases {
		w, h, ok := frameSize(tc.cropW, tc.cropH, tc.efbW, tc.efbH)
		if ok != tc.wantOK || w != tc.wantW || h != tc.wantH {
			t.Errorf("%s: frameSize(%d, %d, %d, %d) = (%d, %d, %v), want (%d, %d, %v)",
				tc.name, tc.cropW, tc.cropH, tc.efbW, tc.efbH, w, h, ok, tc.wantW, tc.wantH, tc.wantOK)
		}
	}
}

// TestRGBAFromBGRA pins the texture readback conversion: BGRA becomes
// RGBA, alpha is forced opaque, the row pitch is honoured (GPU rows are
// padded), and the crop takes only the requested pixels.
func TestRGBAFromBGRA(t *testing.T) {
	// A 2×2 image with a 12-byte row pitch (8 bytes of pixels, 4 of
	// padding): each row's pixels and padding are distinct.
	src := []byte{
		0x11, 0x22, 0x33, 0x00, 0x44, 0x55, 0x66, 0x00, 0xAA, 0xAA, 0xAA, 0xAA, // row 0
		0x77, 0x88, 0x99, 0x00, 0xAA, 0xBB, 0xCC, 0x00, 0xBB, 0xBB, 0xBB, 0xBB, // row 1
	}
	img := rgbaFromBGRA(src, 12, 2, 2)
	if img == nil {
		t.Fatal("rgbaFromBGRA returned nil")
	}
	want := [][4]uint8{
		{0x33, 0x22, 0x11, 255}, // BGRA 11 22 33 → RGBA 33 22 11, opaque
		{0x66, 0x55, 0x44, 255},
		{0x99, 0x88, 0x77, 255},
		{0xCC, 0xBB, 0xAA, 255},
	}
	for i, w := range want {
		x, y := i%2, i/2
		if got := img.RGBAAt(x, y); got != (color.RGBA{R: w[0], G: w[1], B: w[2], A: w[3]}) {
			t.Errorf("pixel (%d, %d) = %v, want %v", x, y, got, w)
		}
	}

	// Cropping to 1×2 reads only the first column of each row.
	img = rgbaFromBGRA(src, 12, 1, 2)
	if img.Rect.Dx() != 1 || img.Rect.Dy() != 2 {
		t.Fatalf("crop is %v, want 1x2", img.Rect)
	}
	if got, want := img.RGBAAt(0, 0), (color.RGBA{R: 0x33, G: 0x22, B: 0x11, A: 255}); got != want {
		t.Errorf("crop pixel (0, 0) = %v, want %v", got, want)
	}
	if got, want := img.RGBAAt(0, 1), (color.RGBA{R: 0x99, G: 0x88, B: 0x77, A: 255}); got != want {
		t.Errorf("crop pixel (0, 1) = %v, want %v", got, want)
	}

	// A pitch narrower than the row, or a short buffer, is refused rather
	// than read past its end.
	if rgbaFromBGRA(src, 4, 2, 2) != nil {
		t.Error("a row pitch narrower than w*4 must be refused")
	}
	if rgbaFromBGRA(src[:11], 12, 2, 2) != nil {
		t.Error("a buffer shorter than pitch*h must be refused")
	}
	if rgbaFromBGRA(src, 12, 0, 2) != nil {
		t.Error("an empty crop must be refused")
	}
}

// TestSubImageCropsTightly pins the frame contract: the image handed
// upward is exactly w×h, copied out of the mapped texture (not a shared
// SubImage pinning the whole mapping).
func TestSubImageCropsTightly(t *testing.T) {
	img := image4x2()
	out := subImage(img, 2, 1)
	if out.Rect.Dx() != 2 || out.Rect.Dy() != 1 {
		t.Fatalf("subImage = %v, want 2x1", out.Rect)
	}
	if &out.Pix[0] == &img.Pix[0] {
		t.Error("subImage must copy, not share, the pixels")
	}
	if got, want := out.RGBAAt(0, 0), img.RGBAAt(0, 0); got != want {
		t.Errorf("subImage pixel (0, 0) = %v, want %v", got, want)
	}
	if got, want := out.RGBAAt(1, 0), img.RGBAAt(1, 0); got != want {
		t.Errorf("subImage pixel (1, 0) = %v, want %v", got, want)
	}
	// The identity case returns the image itself.
	if subImage(img, 4, 2) != img {
		t.Error("subImage(w, h) of the full image must return it unchanged")
	}
}

func image4x2() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(10 * x), G: uint8(40 * y), B: 7, A: 255})
		}
	}
	return img
}

// TestNewReadsEnvironmentKnobs pins the helper-side switches: WINUSE_WGC
// turns the method off (the chain then starts at the GDI methods), and
// WINUSE_WGC_BUDGET overrides the capture budget (a duration or a plain
// number of seconds). Both are also the e2e suite's forced-failure
// levers.
func TestNewReadsEnvironmentKnobs(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		m := New()
		if m.disabled {
			t.Error("the method must be enabled by default")
		}
		if m.budget != DefaultBudget {
			t.Errorf("budget = %v, want %v", m.budget, DefaultBudget)
		}
	})
	t.Run("disabled", func(t *testing.T) {
		for _, v := range []string{"off", "OFF", "0", "false", "no", " disabled "} {
			t.Setenv("WINUSE_WGC", v)
			if m := New(); !m.disabled {
				t.Errorf("WINUSE_WGC=%q must disable the method", v)
			}
		}
	})
	t.Run("enabled spellings", func(t *testing.T) {
		for _, v := range []string{"", "on", "1", "yes", "bogus"} {
			t.Setenv("WINUSE_WGC", v)
			if m := New(); m.disabled {
				t.Errorf("WINUSE_WGC=%q must leave the method enabled", v)
			}
		}
	})
	t.Run("budget", func(t *testing.T) {
		t.Setenv("WINUSE_WGC_BUDGET", "250ms")
		if m := New(); m.budget != 250*time.Millisecond {
			t.Errorf("budget = %v, want 250ms", m.budget)
		}
		t.Setenv("WINUSE_WGC_BUDGET", "3")
		if m := New(); m.budget != 3*time.Second {
			t.Errorf("budget = %v, want 3s", m.budget)
		}
	})
	t.Run("budget invalid keeps default", func(t *testing.T) {
		t.Setenv("WINUSE_WGC_BUDGET", "not-a-duration")
		if m := New(); m.budget != DefaultBudget {
			t.Errorf("budget = %v, want the default %v", m.budget, DefaultBudget)
		}
	})
}

// TestCaptureWindowDisabledFallsBack pins the off-switch end of the
// chain: a disabled method answers ErrDisabled without ever starting the
// capture worker (no thread, no COM — the GDI methods answer next).
func TestCaptureWindowDisabledFallsBack(t *testing.T) {
	t.Setenv("WINUSE_WGC", "off")
	m := New()
	if _, err := m.CaptureWindow(1); !errors.Is(err, ErrDisabled) {
		t.Fatalf("CaptureWindow = %v, want ErrDisabled", err)
	}
	m.mu.Lock()
	worker := m.worker
	m.mu.Unlock()
	if worker != nil {
		t.Error("a disabled method must not start the capture worker")
	}
}

// TestNameIsChainReady pins the method name the chain reports in failure
// messages.
func TestNameIsChainReady(t *testing.T) {
	if got, want := (&Method{}).Name(), "Windows.Graphics.Capture"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}
