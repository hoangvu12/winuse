package shot

import (
	"errors"
	"fmt"
	"image"
	"strings"
)

// Frame is one full-resolution capture of a Window: the pixels plus where
// they sit. Image pixel (0,0) is the top-left of Rect, and every pixel is
// one physical screen pixel (no scaling happens at the frame level).
//
// Anchor is the Window's extended-frame-bounds top-left at capture time.
// Frames are window-anchored by convention — every capture method anchors
// to the same extended frame bounds (the GDI methods here; the
// Windows.Graphics.Capture method, whose texture origin is the extended
// frame bounds as well) — so Rect.Min−Anchor is the frame's window-space
// offset and the Mapping built from a frame is method-independent.
type Frame struct {
	Image  *image.RGBA     // full-resolution pixels, top-left origin
	Rect   image.Rectangle // screen-space rectangle the image covers
	Anchor image.Point     // extended frame bounds top-left at capture time
}

// Method is one screenshot capture method in the chain. CaptureWindow may
// fail (the chain then tries the next method); methods do not retry
// internally. CaptureWindow runs after the Window was activated and is in
// the foreground (the region method needs that; the others tolerate it).
type Method interface {
	// Name identifies the method in failure messages.
	Name() string
	// CaptureWindow captures hwnd's Window full-resolution.
	CaptureWindow(hwnd uint64) (Frame, error)
}

// ErrCapture reports that every method in the capture chain failed.
var ErrCapture = errors.New("capturing the window failed")

// Chain is the ordered capture methods, tried first to last:
// Windows.Graphics.Capture (internal/wgc, ticket 09) in front of the GDI
// methods; the chain is the whole "never depend on one method" design
// (spec, Observation details).
//
// A method that fails, returns an empty frame, or returns a near-black
// frame (the classic "nothing was rendered" failure) falls through to the
// next method. The last method's frame is returned even when near-black:
// by then every method agreed the Window looks blank, and a genuinely dark
// Window must still produce a screenshot.
type Chain struct{ Methods []Method }

// CaptureWindow captures hwnd through the chain.
func (c Chain) CaptureWindow(hwnd uint64) (Frame, error) {
	if len(c.Methods) == 0 {
		return Frame{}, errors.New("no capture methods are configured")
	}
	var failures []string
	for i, m := range c.Methods {
		f, err := m.CaptureWindow(hwnd)
		switch {
		case err != nil:
			failures = append(failures, m.Name()+": "+err.Error())
			continue
		case f.Image == nil || f.Rect.Dx() <= 0 || f.Rect.Dy() <= 0:
			failures = append(failures, m.Name()+": empty frame")
			continue
		case i < len(c.Methods)-1 && IsBlank(f.Image):
			failures = append(failures, m.Name()+": blank frame")
			continue
		}
		return f, nil
	}
	return Frame{}, fmt.Errorf("%w: %s", ErrCapture, strings.Join(failures, "; "))
}

// blankSampleStep is the sampling stride of IsBlank, in pixels, along both
// axes: fast enough for 4K frames while a frame that is black at every
// sampled pixel is black for screenshot purposes.
const blankSampleStep = 16

// IsBlank reports whether an image looks near-black (every sampled pixel
// has max(R,G,B) ≤ 2) — the signature of a capture that rendered nothing:
// a Windows.Graphics.Capture black frame, a PrintWindow refused by UIPI,
// a WDA_MONITOR window. The Windows.Graphics.Capture method's one-frame
// black retry reuses this, and the chain treats a blank non-final frame
// as a fall-through.
func IsBlank(img *image.RGBA) bool {
	if img == nil {
		return true
	}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y += blankSampleStep {
		row := img.Pix[(y-b.Min.Y)*img.Stride:]
		for x := b.Min.X; x < b.Max.X; x += blankSampleStep {
			i := (x - b.Min.X) * 4
			if row[i] > 2 || row[i+1] > 2 || row[i+2] > 2 {
				return false
			}
		}
	}
	return true
}
