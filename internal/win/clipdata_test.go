package win

import (
	"encoding/binary"
	"image"
	"image/color"
	"reflect"
	"strings"
	"testing"
)

func TestClipTextRoundTrip(t *testing.T) {
	for _, s := range []string{"", "hello", "xin chào 🙂", "line one\r\nline two\ttab"} {
		b, err := encodeClipText(s)
		if err != nil {
			t.Fatalf("encode %q: %v", s, err)
		}
		if len(b) < 2 || b[len(b)-1] != 0 || b[len(b)-2] != 0 {
			t.Fatalf("encode %q: missing the NUL terminator", s)
		}
		got, err := decodeClipText(b)
		if err != nil || got != s {
			t.Errorf("round trip %q = %q, %v", s, got, err)
		}
	}
}

// TestClipTextStopsAtTheNUL: a payload's allocation runs past the text.
func TestClipTextStopsAtTheNUL(t *testing.T) {
	b, _ := encodeClipText("abc")
	b = append(b, 'x', 0, 'y', 0)
	if got, err := decodeClipText(b); err != nil || got != "abc" {
		t.Errorf("decode = %q, %v; want abc", got, err)
	}
	if _, err := decodeClipText([]byte{1, 0, 2}); err == nil {
		t.Error("an odd-length payload must fail")
	}
}

func TestClipTextRefusesNUL(t *testing.T) {
	if _, err := encodeClipText("a\x00b"); err == nil {
		t.Error("text with a NUL must be refused before the clipboard is touched")
	}
}

func TestHDropRoundTrip(t *testing.T) {
	paths := []string{`C:\Users\me\a.txt`, `C:\Users\me\thư mục\b.png`}
	b, err := encodeHDrop(paths)
	if err != nil {
		t.Fatal(err)
	}
	if off := binary.LittleEndian.Uint32(b); off != dropFilesSize {
		t.Errorf("pFiles = %d, want %d", off, dropFilesSize)
	}
	if wide := binary.LittleEndian.Uint32(b[16:]); wide != 1 {
		t.Errorf("fWide = %d, want 1", wide)
	}
	got, err := decodeHDrop(b)
	if err != nil || !reflect.DeepEqual(got, paths) {
		t.Errorf("round trip = %q, %v; want %q", got, err, paths)
	}
}

func TestHDropRefusals(t *testing.T) {
	if _, err := encodeHDrop(nil); err == nil {
		t.Error("an empty list must be refused")
	}
	if _, err := encodeHDrop([]string{"a\x00b"}); err == nil {
		t.Error("a path with a NUL must be refused")
	}
	ansi := make([]byte, dropFilesSize+4)
	binary.LittleEndian.PutUint32(ansi, dropFilesSize)
	if _, err := decodeHDrop(ansi); err == nil || !strings.Contains(err.Error(), "ANSI") {
		t.Errorf("an ANSI list: err = %v, want the ANSI refusal", err)
	}
	b, _ := encodeHDrop([]string{"a"})
	if _, err := decodeHDrop(b[:len(b)-2]); err == nil {
		t.Error("a list without its end must fail")
	}
}

// dib builds a BITMAPINFOHEADER bitmap: 2x2, bits per pixel, bottom-up
// unless topDown, with px giving each pixel's B, G, R(, A) in file order.
func dib(bits int, topDown bool, rows [][]byte) []byte {
	h := len(rows)
	stride := (2*bits + 31) / 32 * 4
	out := make([]byte, bitmapInfoHeaderSize+stride*h)
	binary.LittleEndian.PutUint32(out[0:], bitmapInfoHeaderSize)
	binary.LittleEndian.PutUint32(out[4:], 2)
	height := int32(h)
	if topDown {
		height = -height
	}
	binary.LittleEndian.PutUint32(out[8:], uint32(height))
	binary.LittleEndian.PutUint16(out[12:], 1)
	binary.LittleEndian.PutUint16(out[14:], uint16(bits))
	for i, r := range rows {
		copy(out[bitmapInfoHeaderSize+i*stride:], r)
	}
	return out
}

func TestDecodeDIBOrientationAndDepth(t *testing.T) {
	red := []byte{0, 0, 255}
	blue := []byte{255, 0, 0}
	row := func(a, b []byte) []byte { return append(append([]byte{}, a...), b...) }

	// Bottom-up 24-bit: the first row in the file is the image's last.
	img, err := decodeDIB(dib(24, false, [][]byte{row(red, red), row(blue, blue)}))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.NRGBAAt(0, 0); got != (color.NRGBA{0, 0, 255, 255}) {
		t.Errorf("bottom-up top-left = %v, want blue", got)
	}
	if got := img.NRGBAAt(1, 1); got != (color.NRGBA{255, 0, 0, 255}) {
		t.Errorf("bottom-up bottom-right = %v, want red", got)
	}

	// Top-down: the first row is the top.
	img, err = decodeDIB(dib(24, true, [][]byte{row(red, red), row(blue, blue)}))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.NRGBAAt(0, 0); got != (color.NRGBA{255, 0, 0, 255}) {
		t.Errorf("top-down top-left = %v, want red", got)
	}
}

// TestDecodeDIBUnsetAlphaIsOpaque: a 32-bit bitmap with every alpha byte
// zero comes out opaque; one with real alpha keeps it.
func TestDecodeDIBUnsetAlphaIsOpaque(t *testing.T) {
	px := []byte{10, 20, 30, 0}
	row := append(append([]byte{}, px...), px...)
	img, err := decodeDIB(dib(32, false, [][]byte{row, row}))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.NRGBAAt(0, 0); got != (color.NRGBA{30, 20, 10, 255}) {
		t.Errorf("unset alpha = %v, want opaque", got)
	}
	half := []byte{10, 20, 30, 128}
	row = append(append([]byte{}, half...), px...)
	img, err = decodeDIB(dib(32, false, [][]byte{row, row}))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.NRGBAAt(0, 0).A; got != 128 {
		t.Errorf("real alpha = %d, want 128", got)
	}
	if got := img.NRGBAAt(1, 0).A; got != 0 {
		t.Errorf("real zero alpha = %d, want 0", got)
	}
}

func TestDecodeDIBRefusals(t *testing.T) {
	ok := dib(24, false, [][]byte{make([]byte, 6), make([]byte, 6)})
	for name, mutate := range map[string]func([]byte) []byte{
		"short header":  func(b []byte) []byte { return b[:20] },
		"8 bits":        func(b []byte) []byte { binary.LittleEndian.PutUint16(b[14:], 8); return b },
		"compressed":    func(b []byte) []byte { binary.LittleEndian.PutUint32(b[16:], 1); return b },
		"zero width":    func(b []byte) []byte { binary.LittleEndian.PutUint32(b[4:], 0); return b },
		"cut short":     func(b []byte) []byte { return b[:len(b)-1] },
		"unknown size":  func(b []byte) []byte { binary.LittleEndian.PutUint32(b[0:], 64); return b },
		"two planes":    func(b []byte) []byte { binary.LittleEndian.PutUint16(b[12:], 2); return b },
		"huge declared": func(b []byte) []byte { binary.LittleEndian.PutUint32(b[4:], 1<<20); return b },
	} {
		b := mutate(append([]byte{}, ok...))
		if _, err := decodeDIB(b); err == nil {
			t.Errorf("%s: decodeDIB accepted it", name)
		}
	}
}

// dibV5 builds a bottom-up 32-bit BITMAPV5HEADER bitmap with bit fields,
// the BGRA masks and alphaMask, its rows in file order; with repeat the
// three masks follow the header again, as in the CF_DIBV5 Windows
// synthesizes from a CF_BITMAP (the layout measured live on 2026-11-03:
// 124-byte header, 12 bytes of masks, then the pixels).
func dibV5(alphaMask uint32, repeat bool, rows [][]byte) []byte {
	out := make([]byte, bitmapV5HeaderSize)
	binary.LittleEndian.PutUint32(out[0:], bitmapV5HeaderSize)
	binary.LittleEndian.PutUint32(out[4:], uint32(len(rows[0])/4))
	binary.LittleEndian.PutUint32(out[8:], uint32(len(rows)))
	binary.LittleEndian.PutUint16(out[12:], 1)
	binary.LittleEndian.PutUint16(out[14:], 32)
	binary.LittleEndian.PutUint32(out[16:], biBitfields)
	binary.LittleEndian.PutUint32(out[40:], dibRedMask)
	binary.LittleEndian.PutUint32(out[44:], dibGreenMask)
	binary.LittleEndian.PutUint32(out[48:], dibBlueMask)
	binary.LittleEndian.PutUint32(out[52:], alphaMask)
	if repeat {
		out = append(out, out[40:52]...)
	}
	for _, r := range rows {
		out = append(out, r...)
	}
	return out
}

// TestDecodeSynthesizedDIBV5 pins the measured .NET bitmap: the masks
// repeated after the V5 header are skipped, and a zero alpha mask makes
// the image opaque although some alpha bytes are set.
func TestDecodeSynthesizedDIBV5(t *testing.T) {
	gray := []byte{211, 211, 211, 0}
	green := []byte{0, 200, 0, 255}
	row := func(a, b []byte) []byte { return append(append([]byte{}, a...), b...) }
	// Bottom-up: the last row in the file is the image's top, green at 0,0.
	b := dibV5(0, true, [][]byte{row(gray, gray), row(green, gray)})
	img, err := decodeDIB(b)
	if err != nil {
		t.Fatal(err)
	}
	if got := img.NRGBAAt(0, 0); got != (color.NRGBA{0, 200, 0, 255}) {
		t.Errorf("top-left = %v, want opaque green", got)
	}
	if got := img.NRGBAAt(1, 1); got != (color.NRGBA{211, 211, 211, 255}) {
		t.Errorf("bottom-right = %v, want opaque gray (the alpha mask is zero)", got)
	}

	// Without the repeat, the pixels follow the header directly, and an
	// alpha mask keeps the alpha.
	img, err = decodeDIB(dibV5(dibAlphaMask, false, [][]byte{row(gray, gray), row(green, gray)}))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.NRGBAAt(0, 0); got != (color.NRGBA{0, 200, 0, 255}) {
		t.Errorf("no repeat: top-left = %v, want green", got)
	}
	if got := img.NRGBAAt(1, 0).A; got != 0 {
		t.Errorf("an alpha mask: alpha %d, want the pixel's 0 kept", got)
	}
}

func TestDecodeDIBRefusesOtherMasks(t *testing.T) {
	b := dibV5(0, false, [][]byte{make([]byte, 8), make([]byte, 8)})
	binary.LittleEndian.PutUint32(b[40:], 0x000000FF) // red where blue goes
	if _, err := decodeDIB(b); err == nil || !strings.Contains(err.Error(), "masks") {
		t.Errorf("swapped masks: err = %v, want the mask refusal", err)
	}
}

// TestDIBRoundTrip: encodeDIB writes what decodeDIB reads back.
func TestDIBRoundTrip(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	src.SetNRGBA(0, 0, color.NRGBA{1, 2, 3, 255})
	src.SetNRGBA(2, 1, color.NRGBA{200, 100, 50, 77})
	got, err := decodeDIB(encodeDIB(src))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Pix, src.Pix) {
		t.Errorf("round trip pixels differ:\n got %v\nwant %v", got.Pix, src.Pix)
	}
}

func TestCheckPNG(t *testing.T) {
	b, err := encodePNG(image.NewNRGBA(image.Rect(0, 0, 4, 3)))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := checkPNG(b)
	if err != nil || cfg.Width != 4 || cfg.Height != 3 {
		t.Errorf("checkPNG = %+v, %v; want 4x3", cfg, err)
	}
	if _, err := checkPNG([]byte("GIF89a not a png")); err == nil {
		t.Error("a non-PNG must be refused")
	}
	if _, _, err := decodeCheckedPNG(b[:len(b)-12]); err == nil {
		t.Error("a truncated PNG must be refused")
	}
}
