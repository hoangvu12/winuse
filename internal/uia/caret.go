package uia

// The insertion caret's place (multi-agent ticket 11, from desktop-parity
// 21): after a Headless write into a field with a Text pattern, the Agent
// Cursor draws a short caret where the field's caret now is. The place is
// read from the Text pattern: the selection's bounding rectangles
// (GetBoundingRectangles). A collapsed caret often has no rectangle, so
// the range is widened by one character, after the caret when there is
// one (the caret is that character's leading edge), else before it (the
// caret is its trailing edge). A field with no rectangle gets no caret,
// never a guessed one. Unmeasured on WPF, WinUI 3 and Chromium's frame:
// the read is best effort, and a failure only means no caret.

import (
	"context"
	"unsafe"
)

const rangeGetBoundingRectangles = 10 // IUIAutomationTextRange::GetBoundingRectangles(**SAFEARRAY of double)

// CaretEdge is the caret's place on screen, physical pixels: the x of the
// line it stands on and that line's top and bottom.
type CaretEdge struct {
	X, Top, Bottom int32
}

// caretRect is one bounding rectangle a Text pattern reports.
type caretRect struct{ left, top, width, height float64 }

// caretEdgeOf picks the caret's edge from the rectangles of the range
// read for it: the leading edge of the first when the range is the
// character after the caret (after), the trailing edge of the last when
// it is the character before it; ok is false with no usable rectangle.
func caretEdgeOf(rects []caretRect, after bool) (CaretEdge, bool) {
	if len(rects) == 0 {
		return CaretEdge{}, false
	}
	r := rects[0]
	x := r.left
	if !after {
		r = rects[len(rects)-1]
		x = r.left + r.width
	}
	if r.height <= 0 {
		return CaretEdge{}, false
	}
	return CaretEdge{X: int32(x + 0.5), Top: int32(r.top + 0.5), Bottom: int32(r.top + r.height + 0.5)}, true
}

// CaretEdgeOf reads where the caret of the Element behind index is (see
// the file comment); ok is false when it cannot be told.
func (c *Client) CaretEdgeOf(ctx context.Context, hwnd uint64, index int) (CaretEdge, bool) {
	var edge CaretEdge
	var ok bool
	_ = c.onElement(ctx, hwnd, index, func(th *thread, el comPtr) error {
		edge, ok = caretEdge(el)
		return nil
	})
	return edge, ok
}

// caretEdge reads a live Element's caret edge.
func caretEdge(el comPtr) (CaretEdge, bool) {
	pat, _ := currentPattern(el, patternText, &iidTextPattern)
	if pat == nil {
		return CaretEdge{}, false
	}
	defer release(pat)
	sel, ok := firstSelection(pat)
	if !ok {
		return CaretEdge{}, false
	}
	defer release(sel)
	// The character after the caret's end, else the one before its start.
	for _, after := range []bool{true, false} {
		var r comPtr
		if hr := call(sel, rangeClone, uintptr(unsafe.Pointer(&r))); hr.Failed() || r == nil {
			return CaretEdge{}, false
		}
		var moved int32
		if after {
			call(r, rangeMoveEndpointByRange, endpointStart, uintptr(r), endpointEnd)
			call(r, rangeMoveEndpointByUnit, endpointEnd, textUnitCharacter, 1, uintptr(unsafe.Pointer(&moved)))
		} else {
			call(r, rangeMoveEndpointByRange, endpointEnd, uintptr(r), endpointStart)
			call(r, rangeMoveEndpointByUnit, endpointStart, textUnitCharacter, ^uintptr(0), uintptr(unsafe.Pointer(&moved))) // -1
		}
		rects := boundingRects(r)
		release(r)
		if moved == 0 {
			continue
		}
		if edge, ok := caretEdgeOf(rects, after); ok {
			return edge, true
		}
	}
	return CaretEdge{}, false
}

// boundingRects reads a range's bounding rectangles (a SAFEARRAY of
// doubles, four per rectangle).
func boundingRects(r comPtr) []caretRect {
	var sa uintptr
	if hr := call(r, rangeGetBoundingRectangles, uintptr(unsafe.Pointer(&sa))); hr.Failed() || sa == 0 {
		return nil
	}
	defer procSafeArrayDestroy.Call(sa)
	var lo, hi int32
	procSafeArrayGetLBound.Call(sa, 1, uintptr(unsafe.Pointer(&lo)))
	procSafeArrayGetUBound.Call(sa, 1, uintptr(unsafe.Pointer(&hi)))
	n := int(hi - lo + 1)
	if n < 4 {
		return nil
	}
	var data *float64
	if res, _, _ := procSafeArrayAccessData.Call(sa, uintptr(unsafe.Pointer(&data))); res != 0 || data == nil {
		return nil
	}
	defer procSafeArrayUnaccessData.Call(sa)
	vals := unsafe.Slice(data, n)
	var out []caretRect
	for i := 0; i+3 < n; i += 4 {
		out = append(out, caretRect{vals[i], vals[i+1], vals[i+2], vals[i+3]})
	}
	return out
}
