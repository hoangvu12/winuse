package shot

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"testing"
)

// frame builds a synthetic frame: a w×h image with a colour gradient, a
// screen rect whose top-left is at (sx, sy), anchored at (ax, ay).
func frame(w, h int, sx, sy, ax, ay int) Frame {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	return Frame{
		Image:  img,
		Rect:   image.Rect(sx, sy, sx+w, sy+h),
		Anchor: image.Point{X: ax, Y: ay},
	}
}

func TestFitScale(t *testing.T) {
	cases := []struct {
		w, h int
		want float64
	}{
		{800, 470, 1},               // small window: 1:1
		{1568, 800, 1},              // exactly the cap
		{1569, 100, 1568.0 / 1569},  // one pixel over: downscale
		{3840, 2160, 1568.0 / 3840}, // 4K window
		{900, 3200, 1568.0 / 3200},  // tall window caps on height
		{1568, 1568, 1},             // square at the cap
	}
	for _, tc := range cases {
		got := FitScale(tc.w, tc.h)
		if got != tc.want {
			t.Errorf("FitScale(%d, %d) = %v, want %v", tc.w, tc.h, got, tc.want)
		}
	}
}

func TestMappingForMatchesScaleImageDims(t *testing.T) {
	for _, phys := range [][2]int{{800, 470}, {3840, 2160}, {1571, 999}, {2000, 100}, {100, 2000}} {
		for _, scale := range []float64{1, 0.5, 1568.0 / 3840, 0.3333} {
			if scale > 1 {
				continue
			}
			f := frame(phys[0], phys[1], 100, 200, 100, 200)
			m := MappingFor(f, scale)
			img := ScaleImage(f.Image, scale)
			if m.Width != img.Rect.Dx() || m.Height != img.Rect.Dy() {
				t.Errorf("physical %v scale %v: mapping says %dx%d, ScaleImage made %dx%d",
					phys, scale, m.Width, m.Height, img.Rect.Dx(), img.Rect.Dy())
			}
			// The coverage is the frame's window-space rectangle.
			want := image.Rect(0, 0, phys[0], phys[1])
			if m.Coverage != want {
				t.Errorf("physical %v scale %v: coverage = %v, want %v", phys, scale, m.Coverage, want)
			}
		}
	}
}

func TestMappingForAnchorNotAtRectOrigin(t *testing.T) {
	// A capture widened with a popup extending left of the Window: the
	// rect starts left of the anchor, so window-space coverage is negative.
	f := frame(100, 80, 40, 50, 50, 60)
	m := MappingFor(f, 1)
	if m.Coverage != image.Rect(-10, -10, 90, 70) {
		t.Fatalf("coverage = %v, want %v", m.Coverage, image.Rect(-10, -10, 90, 70))
	}
	// Screenshot pixel (0,0) is the capture's top-left, 10 px up-left of
	// the Window's frame bounds.
	if got := m.WindowPoint(0, 0); got != image.Pt(-10, -10) {
		t.Errorf("WindowPoint(0,0) = %v, want (-10,-10)", got)
	}
}

func TestWindowPointScaleOneIsExact(t *testing.T) {
	f := frame(800, 470, 1010, 720, 1010, 720)
	m := MappingFor(f, 1)
	for _, pt := range [][2]int{{0, 0}, {1, 1}, {799, 469}, {400, 235}} {
		if got := m.WindowPoint(pt[0], pt[1]); got != image.Pt(pt[0], pt[1]) {
			t.Errorf("WindowPoint(%d, %d) = %v, want exactly it", pt[0], pt[1], got)
		}
	}
}

func TestWindowPointHalfScaleAimsAtFootprintCentre(t *testing.T) {
	f := frame(800, 600, 0, 0, 0, 0)
	m := MappingFor(f, 0.5)
	// Screenshot pixel x covers physical [2x, 2x+2); the click aims at the
	// centre 2x+1 — pixel-exact mapping at the boundary between two
	// screenshot pixels.
	for _, tc := range []struct{ shot, want int }{
		{0, 1}, {1, 3}, {2, 5}, {399, 799},
	} {
		if got := m.WindowPoint(tc.shot, 0); got.X != tc.want {
			t.Errorf("WindowPoint(%d, 0).X = %d, want %d", tc.shot, got.X, tc.want)
		}
	}
	// Any scale: the aim is the centre of the pixel's footprint.
	s := 1568.0 / 3840
	big := MappingFor(frame(3840, 2160, 0, 0, 0, 0), s)
	for _, shot := range []int{0, 1, 783, 1567} {
		want := int(math.Floor((float64(shot) + 0.5) / s))
		if got := big.WindowPoint(shot, 0).X; got != want {
			t.Errorf("WindowPoint(%d, 0).X = %d, want %d", shot, got, want)
		}
	}
}

func TestWindowRectRoundsOutward(t *testing.T) {
	f := frame(800, 600, 0, 0, 0, 0)
	m := MappingFor(f, 0.5)
	// 0..4 in screenshot px covers physical [0, 8); rounding outward from
	// floor(0/0.5)=0 to ceil(4/0.5)=8.
	got := m.WindowRect(0, 0, 4, 4)
	if got != image.Rect(0, 0, 8, 8) {
		t.Errorf("WindowRect(0,0,4,4) = %v, want (0,0,8,8)", got)
	}
	// An odd start rounds down, an odd end rounds up, so the crop covers
	// the full requested region.
	got = m.WindowRect(1, 1, 3, 3)
	if got != image.Rect(2, 2, 8, 8) {
		t.Errorf("WindowRect(1,1,3,3) = %v, want (2,2,8,8)", got)
	}
}

func TestContains(t *testing.T) {
	f := frame(1568, 800, 0, 0, 0, 0)
	m := MappingFor(f, 1)
	cases := []struct {
		x, y int
		want bool
	}{
		{0, 0, true}, {1567, 799, true}, {1568, 0, false}, {0, 800, false},
		{-1, 0, false}, {0, -1, false}, {100, 100, true},
	}
	for _, tc := range cases {
		if got := m.Contains(tc.x, tc.y); got != tc.want {
			t.Errorf("Contains(%d, %d) = %v, want %v", tc.x, tc.y, got, tc.want)
		}
	}
}

func TestStoreRecordsAndReplaces(t *testing.T) {
	s := NewStore()
	if _, ok := s.Lookup(101); ok {
		t.Fatal("a fresh store must have no mappings")
	}
	f1 := frame(800, 470, 0, 0, 0, 0)
	s.Record(101, MappingFor(f1, 1))
	s.Record(202, MappingFor(frame(3840, 2160, 0, 0, 0, 0), 0.5))

	m1, ok := s.Lookup(101)
	if !ok || m1.Width != 800 || m1.Scale != 1 {
		t.Fatalf("lookup 101 = %v, %v", m1, ok)
	}
	m2, ok := s.Lookup(202)
	if !ok || m2.Scale != 0.5 {
		t.Fatalf("lookup 202 = %v, %v", m2, ok)
	}

	// A new screenshot of the same Window replaces the mapping (the
	// latest screenshot's coordinate space wins).
	s.Record(101, MappingFor(frame(400, 300, 0, 0, 0, 0), 1))
	m1, _ = s.Lookup(101)
	if m1.Width != 400 {
		t.Errorf("after re-recording, lookup 101 = %dx%d, want 400x300", m1.Width, m1.Height)
	}
	// ...and other Windows are untouched.
	if _, ok := s.Lookup(202); !ok {
		t.Error("recording window 101 must not drop window 202's mapping")
	}
}

func TestScaleImageScaleOneIsExactCopy(t *testing.T) {
	src := frame(64, 48, 0, 0, 0, 0).Image
	dst := ScaleImage(src, 1)
	if dst.Rect != image.Rect(0, 0, 64, 48) {
		t.Fatalf("dst rect = %v", dst.Rect)
	}
	for y := 0; y < 48; y++ {
		for x := 0; x < 64; x++ {
			if dst.RGBAAt(x, y) != src.RGBAAt(x, y) {
				t.Fatalf("pixel (%d, %d) = %v, want %v", x, y, dst.RGBAAt(x, y), src.RGBAAt(x, y))
			}
		}
	}
}

func TestScaleImageAverages(t *testing.T) {
	// A 2:1 downscale where each destination pixel covers one black and
	// one white source pixel: the average is the midpoint.
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		src.SetRGBA(0, y, color.RGBA{0, 0, 0, 255})
		src.SetRGBA(1, y, color.RGBA{255, 255, 255, 255})
	}
	dst := ScaleImage(src, 0.5)
	if dst.Rect != image.Rect(0, 0, 1, 1) {
		t.Fatalf("dst rect = %v, want 1x1", dst.Rect)
	}
	c := dst.RGBAAt(0, 0)
	if c.R != 128 || c.G != 128 || c.B != 128 || c.A != 255 {
		t.Errorf("pixel = %v, want the average (128,128,128,255)", c)
	}
}

func TestScaleImageUniformColourStaysUniform(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 999, 777))
	for i := 0; i < len(src.Pix); i += 4 {
		src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3] = 200, 100, 50, 0 // alpha undefined
	}
	dst := ScaleImage(src, 1568.0/2000)
	wantH := int(math.Round(777 * (1568.0 / 2000)))
	if dst.Rect.Dy() != wantH {
		t.Errorf("dst height = %d, want %d", dst.Rect.Dy(), wantH)
	}
	for y := 0; y < dst.Rect.Dy(); y++ {
		for x := 0; x < dst.Rect.Dx(); x++ {
			c := dst.RGBAAt(x, y)
			if c.R != 200 || c.G != 100 || c.B != 50 || c.A != 255 {
				t.Fatalf("pixel (%d, %d) = %v, want (200,100,50,255)", x, y, c)
			}
		}
	}
}

func TestScaleImageSplitPixelContributesToBothSides(t *testing.T) {
	// 3 px shrunk by 2/3: dst pixels cover [0,1.5) and [1.5,3); the middle
	// (white) pixel contributes half a pixel to each destination, so each
	// is (1.0·black + 0.5·white)/1.5 = 85.
	src := image.NewRGBA(image.Rect(0, 0, 3, 1))
	src.SetRGBA(0, 0, color.RGBA{0, 0, 0, 255})
	src.SetRGBA(1, 0, color.RGBA{255, 255, 255, 255})
	src.SetRGBA(2, 0, color.RGBA{0, 0, 0, 255})
	dst := ScaleImage(src, 2.0/3.0)
	if dst.Rect.Dx() != 2 {
		t.Fatalf("dst width = %d, want 2", dst.Rect.Dx())
	}
	for x := 0; x < 2; x++ {
		if c := dst.RGBAAt(x, 0); c.R != 85 {
			t.Errorf("dst(%d,0) = %v, want 85 (the split white pixel shared by both)", x, c)
		}
	}
}

func TestScaleImageDimsMatchRounding(t *testing.T) {
	for _, phys := range [][2]int{{3840, 2160}, {1571, 997}, {1, 1}, {3, 7}} {
		for _, scale := range []float64{1, 0.5, 1568.0 / 3840, 0.7} {
			dst := ScaleImage(frame(phys[0], phys[1], 0, 0, 0, 0).Image, scale)
			wantW, wantH := scaledSize(phys[0], scale), scaledSize(phys[1], scale)
			if dst.Rect.Dx() != wantW || dst.Rect.Dy() != wantH {
				t.Errorf("physical %v scale %v: got %dx%d, want %dx%d", phys, scale,
					dst.Rect.Dx(), dst.Rect.Dy(), wantW, wantH)
			}
		}
	}
}

func TestIsBlank(t *testing.T) {
	black := image.NewRGBA(image.Rect(0, 0, 64, 64))
	if !IsBlank(black) {
		t.Error("an all-black image is blank")
	}
	nearBlack := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for i := 0; i < len(nearBlack.Pix); i += 4 {
		nearBlack.Pix[i], nearBlack.Pix[i+1], nearBlack.Pix[i+2] = 2, 2, 2
	}
	if !IsBlank(nearBlack) {
		t.Error("max RGB 2 counts as blank (the nothing-rendered signature)")
	}
	// A bright pixel at a sampled position breaks blankness.
	content := image.NewRGBA(image.Rect(0, 0, 64, 64))
	content.SetRGBA(0, 0, color.RGBA{R: 3, A: 255})
	if IsBlank(content) {
		t.Error("a non-black image is not blank")
	}
	if !IsBlank(nil) {
		t.Error("nil is blank")
	}
}

// fakeMethod is a chain method with a scripted outcome.
type fakeMethod struct {
	name  string
	frame Frame
	err   error
	blank bool
	calls int
}

func (m *fakeMethod) Name() string { return m.name }

func (m *fakeMethod) CaptureWindow(hwnd uint64) (Frame, error) {
	m.calls++
	if m.err != nil {
		return Frame{}, m.err
	}
	if m.blank {
		return Frame{Image: image.NewRGBA(image.Rect(0, 0, 64, 64)), Rect: image.Rect(0, 0, 64, 64)}, nil
	}
	return m.frame, nil
}

func TestChainFirstMethodWins(t *testing.T) {
	m1 := &fakeMethod{name: "first", frame: frame(800, 470, 0, 0, 0, 0)}
	m2 := &fakeMethod{name: "second", frame: frame(800, 470, 0, 0, 0, 0)}
	c := Chain{Methods: []Method{m1, m2}}
	f, err := c.CaptureWindow(101)
	if err != nil || f.Image == nil {
		t.Fatalf("capture: %v", err)
	}
	if m2.calls != 0 {
		t.Error("a successful first method must stop the chain")
	}
}

func TestChainFallsThroughOnBlankAndError(t *testing.T) {
	blank := &fakeMethod{name: "blank", blank: true}
	erring := &fakeMethod{name: "error", err: fmt.Errorf("boom")}
	good := &fakeMethod{name: "good", frame: frame(100, 100, 0, 0, 0, 0)}
	c := Chain{Methods: []Method{blank, erring, good}}
	f, err := c.CaptureWindow(101)
	if err != nil {
		t.Fatalf("the chain must fall through to the good method: %v", err)
	}
	if f.Image == nil || f.Rect.Dx() != 100 {
		t.Errorf("frame = %+v", f)
	}
	if blank.calls != 1 || erring.calls != 1 || good.calls != 1 {
		t.Error("each method must have been tried exactly once")
	}
}

func TestChainAcceptsBlankFromLastMethod(t *testing.T) {
	// A genuinely dark Window: both methods report blank, and the last
	// method's frame is still returned.
	blank1 := &fakeMethod{name: "one", blank: true}
	blank2 := &fakeMethod{name: "two", blank: true}
	c := Chain{Methods: []Method{blank1, blank2}}
	f, err := c.CaptureWindow(101)
	if err != nil {
		t.Fatalf("a blank last-method frame must be accepted: %v", err)
	}
	if f.Image == nil {
		t.Error("the frame must carry the blank image")
	}
}

func TestChainAllFailedListsEveryMethod(t *testing.T) {
	m1 := &fakeMethod{name: "region", err: fmt.Errorf("the Window is partly off-screen")}
	m2 := &fakeMethod{name: "PrintWindow", err: fmt.Errorf("timed out")}
	c := Chain{Methods: []Method{m1, m2}}
	_, err := c.CaptureWindow(101)
	if err == nil {
		t.Fatal("all methods failing must be an error")
	}
	want := "capturing the window failed: region: the Window is partly off-screen; PrintWindow: timed out"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestChainEmptyFrameFallsThrough(t *testing.T) {
	empty := &fakeMethod{name: "empty", frame: Frame{Image: image.NewRGBA(image.Rect(0, 0, 0, 0))}}
	good := &fakeMethod{name: "good", frame: frame(10, 10, 0, 0, 0, 0)}
	f, err := Chain{Methods: []Method{empty, good}}.CaptureWindow(1)
	if err != nil || f.Image == nil {
		t.Fatalf("empty frames must fall through: %v", err)
	}
}

func TestChainNoMethods(t *testing.T) {
	if _, err := (Chain{}).CaptureWindow(1); err == nil {
		t.Fatal("a chain without methods must fail")
	}
}
