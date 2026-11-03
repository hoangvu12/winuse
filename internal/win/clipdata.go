package win

// The clipboard's data formats (desktop-parity ticket 01), pure and tested
// offline: text as CF_UNICODETEXT, a file list as CF_HDROP, and an image
// as the registered "PNG" format or a CF_DIBV5/CF_DIB bitmap, read back as
// PNG. The marshalling and its budgets follow agent-desktop's Windows
// adapter (crates/windows/src/input/clipboard/clipboard_{text,files,
// image}.rs); clipboard.go moves the bytes in and out of the clipboard.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/png"
	"strings"
	"unicode/utf16"
)

// The standard clipboard formats winuse reads and writes. "PNG" is a
// registered format: its id is looked up at run time.
const (
	cfDIB         = 8
	cfUnicodeText = 13
	cfHDrop       = 15
	cfDIBV5       = 17
)

// The budgets, agent-desktop's: a payload or a PNG file is at most
// MaxClipboardPNG bytes, an image at most 64 Mi pixels, the text a million
// UTF-16 units, a file list 1024 paths.
const (
	MaxClipboardPNG   = 64 << 20
	maxClipPayload    = MaxClipboardPNG
	maxClipPixels     = 64 << 20
	maxClipTextUnits  = 1_000_000
	maxClipPaths      = 1024
	maxClipPathUnits  = 16_384
	maxClipPathsTotal = 1_000_000
)

// decodeClipText reads CF_UNICODETEXT: UTF-16LE up to the first NUL (the
// payload's allocation may run past it).
func decodeClipText(b []byte) (string, error) {
	if len(b)%2 != 0 {
		return "", errors.New("the clipboard's text is not whole UTF-16 units")
	}
	units := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u := binary.LittleEndian.Uint16(b[i:])
		if u == 0 {
			break
		}
		units = append(units, u)
	}
	if len(units) > maxClipTextUnits {
		return "", fmt.Errorf("the clipboard's text is longer than %d characters", maxClipTextUnits)
	}
	return string(utf16.Decode(units)), nil
}

// encodeClipText writes CF_UNICODETEXT: UTF-16LE with its NUL.
func encodeClipText(s string) ([]byte, error) {
	if strings.ContainsRune(s, 0) {
		return nil, errors.New("the text contains a NUL character, which the clipboard cannot hold")
	}
	units := utf16.Encode([]rune(s))
	if len(units) > maxClipTextUnits {
		return nil, fmt.Errorf("the text is longer than %d characters", maxClipTextUnits)
	}
	out := make([]byte, 2*len(units)+2)
	for i, u := range units {
		binary.LittleEndian.PutUint16(out[2*i:], u)
	}
	return out, nil
}

// dropFilesSize is sizeof(DROPFILES): pFiles, pt.x, pt.y, fNC, fWide.
const dropFilesSize = 20

// decodeHDrop reads CF_HDROP: a DROPFILES header, then the paths as
// NUL-terminated UTF-16 strings ending in an empty one. ANSI lists
// (fWide 0) are refused, as agent-desktop does.
func decodeHDrop(b []byte) ([]string, error) {
	if len(b) < dropFilesSize {
		return nil, errors.New("the clipboard's file list is shorter than its header")
	}
	off := binary.LittleEndian.Uint32(b[0:])
	if off < dropFilesSize || int(off) > len(b) {
		return nil, errors.New("the clipboard's file list points outside itself")
	}
	if binary.LittleEndian.Uint32(b[16:]) == 0 {
		return nil, errors.New("the clipboard's file list is ANSI; only Unicode lists are read")
	}
	list := b[off:]
	if len(list)%2 != 0 {
		return nil, errors.New("the clipboard's file list is not whole UTF-16 units")
	}
	var paths []string
	var cur []uint16
	total := 0
	for i := 0; i+1 < len(list); i += 2 {
		u := binary.LittleEndian.Uint16(list[i:])
		if u != 0 {
			cur = append(cur, u)
			continue
		}
		if len(cur) == 0 {
			return paths, nil // the empty string that ends the list
		}
		if len(paths) == maxClipPaths {
			return nil, fmt.Errorf("the clipboard holds more than %d files", maxClipPaths)
		}
		total += len(cur)
		if len(cur) > maxClipPathUnits || total > maxClipPathsTotal {
			return nil, errors.New("the clipboard's file list is longer than winuse reads")
		}
		paths = append(paths, string(utf16.Decode(cur)))
		cur = cur[:0]
	}
	return nil, errors.New("the clipboard's file list has no end")
}

// encodeHDrop writes CF_HDROP for the paths, a Unicode list.
func encodeHDrop(paths []string) ([]byte, error) {
	if len(paths) == 0 {
		return nil, errors.New("no files were given")
	}
	if len(paths) > maxClipPaths {
		return nil, fmt.Errorf("more than %d files were given", maxClipPaths)
	}
	var units []uint16
	for _, p := range paths {
		if p == "" || strings.ContainsRune(p, 0) {
			return nil, fmt.Errorf("the path %q cannot go on the clipboard", p)
		}
		u := utf16.Encode([]rune(p))
		if len(u) > maxClipPathUnits {
			return nil, fmt.Errorf("the path %q is longer than %d characters", p, maxClipPathUnits)
		}
		units = append(append(units, u...), 0)
		if len(units) > maxClipPathsTotal {
			return nil, errors.New("the paths are longer than the clipboard's file list takes")
		}
	}
	units = append(units, 0)
	out := make([]byte, dropFilesSize+2*len(units))
	binary.LittleEndian.PutUint32(out[0:], dropFilesSize) // pFiles
	binary.LittleEndian.PutUint32(out[16:], 1)            // fWide
	for i, u := range units {
		binary.LittleEndian.PutUint16(out[dropFilesSize+2*i:], u)
	}
	return out, nil
}

// The DIB header sizes and compressions decodeDIB reads: BITMAPINFOHEADER,
// BITMAPV4HEADER and BITMAPV5HEADER, uncompressed (biRGB, capture.go) or
// with bit fields.
const (
	bitmapInfoHeaderSize = 40
	bitmapV4HeaderSize   = 108
	bitmapV5HeaderSize   = 124
	biBitfields          = 3
)

// How decodeDIB treats a 32-bit pixel's fourth byte.
const (
	alphaGuess  = iota // alpha, unless every pixel's is zero
	alphaKept          // alpha, as the header's alpha mask says
	alphaOpaque        // unused: the header's alpha mask is zero
)

// The bit-field masks decodeDIB reads: 8 bits a channel in BGRA order.
const (
	dibRedMask   = 0x00FF0000
	dibGreenMask = 0x0000FF00
	dibBlueMask  = 0x000000FF
	dibAlphaMask = 0xFF000000
)

// decodeDIB reads a CF_DIB or CF_DIBV5 bitmap of 24 or 32 bits per pixel,
// bottom-up or top-down. Bit fields must be the usual BGRA masks; other
// masks are refused rather than misread.
//
// Where the alpha byte counts: a V4 or V5 header with bit fields says so
// with its alpha mask, and a zero mask is an opaque image. Elsewhere the
// byte has no defined meaning, and a 32-bit bitmap whose alpha bytes are
// all zero is opaque: most apps leave that byte unset.
//
// The CF_DIBV5 that Windows synthesizes from a CF_BITMAP (what .NET's
// Clipboard.SetImage puts there) repeats its three masks after the V5
// header, so its pixels start 12 bytes later than the header says
// (measured on Windows 11 26200, 2026-11-03). agent-desktop misses this.
func decodeDIB(b []byte) (*image.NRGBA, error) {
	if len(b) < bitmapInfoHeaderSize {
		return nil, errors.New("the clipboard's bitmap is shorter than its header")
	}
	hdr := binary.LittleEndian.Uint32(b[0:])
	if hdr != bitmapInfoHeaderSize && hdr != bitmapV4HeaderSize && hdr != bitmapV5HeaderSize {
		return nil, fmt.Errorf("the clipboard's bitmap has an unknown header (%d bytes)", hdr)
	}
	if uint32(len(b)) < hdr {
		return nil, errors.New("the clipboard's bitmap is shorter than its header")
	}
	width := int32(binary.LittleEndian.Uint32(b[4:]))
	height := int32(binary.LittleEndian.Uint32(b[8:]))
	planes := binary.LittleEndian.Uint16(b[12:])
	bits := binary.LittleEndian.Uint16(b[14:])
	compression := binary.LittleEndian.Uint32(b[16:])
	clrUsed := binary.LittleEndian.Uint32(b[32:])
	switch {
	case planes != 1:
		return nil, errors.New("the clipboard's bitmap does not have one plane")
	case bits != 24 && bits != 32:
		return nil, fmt.Errorf("the clipboard's bitmap has %d bits per pixel; winuse reads 24 and 32", bits)
	case compression != biRGB && compression != biBitfields:
		return nil, errors.New("the clipboard's bitmap is compressed; winuse reads uncompressed bitmaps")
	case width <= 0 || height == 0:
		return nil, errors.New("the clipboard's bitmap has no pixels")
	}
	topDown := height < 0
	if topDown {
		height = -height
	}
	w, h := int(width), int(height)
	if int64(w)*int64(h) > maxClipPixels {
		return nil, errors.New("the clipboard's image is larger than winuse reads")
	}
	bpp := int(bits) / 8
	stride := (w*int(bits) + 31) / 32 * 4
	pixBytes := int64(stride) * int64(h)
	offset := int64(hdr)
	alpha := alphaGuess
	if compression == biBitfields {
		if len(b) < bitmapInfoHeaderSize+12 {
			return nil, errors.New("the clipboard's bitmap is shorter than its masks")
		}
		masks := b[bitmapInfoHeaderSize : bitmapInfoHeaderSize+12] // in the V4/V5 header, or after a BITMAPINFOHEADER
		if binary.LittleEndian.Uint32(masks[0:]) != dibRedMask || binary.LittleEndian.Uint32(masks[4:]) != dibGreenMask ||
			binary.LittleEndian.Uint32(masks[8:]) != dibBlueMask {
			return nil, errors.New("the clipboard's bitmap uses colour masks winuse does not read")
		}
		switch {
		case hdr == bitmapInfoHeaderSize:
			offset += 12
		case int64(len(b)) >= offset+12+pixBytes && bytes.Equal(b[hdr:hdr+12], masks):
			offset += 12 // the masks repeated after a V4/V5 header
		}
		if hdr > bitmapInfoHeaderSize {
			alpha = alphaOpaque
			if binary.LittleEndian.Uint32(b[52:]) == dibAlphaMask {
				alpha = alphaKept
			}
		}
	}
	offset += 4 * int64(clrUsed) // an optional palette, unused above 8 bits
	if offset+pixBytes > int64(len(b)) {
		return nil, errors.New("the clipboard's bitmap is cut short")
	}
	pix := b[offset:]
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	anyAlpha := false
	for y := 0; y < h; y++ {
		src := y
		if !topDown {
			src = h - 1 - y
		}
		row := pix[src*stride:]
		dst := img.Pix[y*img.Stride:]
		for x := 0; x < w; x++ {
			p := row[x*bpp:]
			a := byte(255)
			if bpp == 4 && alpha != alphaOpaque {
				a = p[3]
				anyAlpha = anyAlpha || a != 0
			}
			dst[4*x], dst[4*x+1], dst[4*x+2], dst[4*x+3] = p[2], p[1], p[0], a
		}
	}
	if bpp == 4 && alpha == alphaGuess && !anyAlpha {
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 255
		}
	}
	return img, nil
}

// encodeDIB writes img as a CF_DIB: a BITMAPINFOHEADER and 32-bit BGRA
// rows, bottom-up.
func encodeDIB(img image.Image) []byte {
	r := img.Bounds()
	w, h := r.Dx(), r.Dy()
	out := make([]byte, bitmapInfoHeaderSize+4*w*h)
	binary.LittleEndian.PutUint32(out[0:], bitmapInfoHeaderSize)
	binary.LittleEndian.PutUint32(out[4:], uint32(w))
	binary.LittleEndian.PutUint32(out[8:], uint32(h))
	binary.LittleEndian.PutUint16(out[12:], 1)
	binary.LittleEndian.PutUint16(out[14:], 32)
	binary.LittleEndian.PutUint32(out[20:], uint32(4*w*h))
	nrgba, ok := img.(*image.NRGBA)
	if !ok {
		nrgba = image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				nrgba.Set(x, y, img.At(r.Min.X+x, r.Min.Y+y))
			}
		}
	}
	for y := 0; y < h; y++ {
		src := nrgba.Pix[y*nrgba.Stride:]
		dst := out[bitmapInfoHeaderSize+(h-1-y)*4*w:]
		for x := 0; x < w; x++ {
			dst[4*x], dst[4*x+1], dst[4*x+2], dst[4*x+3] = src[4*x+2], src[4*x+1], src[4*x], src[4*x+3]
		}
	}
	return out
}

// checkPNG validates PNG bytes (an --image file, or the clipboard's "PNG"
// format) and returns the size. The header is read first, so an image over
// the pixel budget is refused before it is decoded.
func checkPNG(b []byte) (image.Config, error) {
	if len(b) > MaxClipboardPNG {
		return image.Config{}, fmt.Errorf("the PNG is larger than %d MiB", MaxClipboardPNG>>20)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return image.Config{}, fmt.Errorf("not a PNG image: %v", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > maxClipPixels {
		return image.Config{}, errors.New("the PNG is larger than winuse puts on the clipboard")
	}
	return cfg, nil
}

// decodeCheckedPNG decodes PNG bytes after checkPNG.
func decodeCheckedPNG(b []byte) (image.Image, image.Config, error) {
	cfg, err := checkPNG(b)
	if err != nil {
		return nil, cfg, err
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, cfg, fmt.Errorf("not a PNG image: %v", err)
	}
	return img, cfg, nil
}

// encodePNG encodes img as PNG.
func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
