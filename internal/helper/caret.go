package helper

// The insertion caret (multi-agent ticket 11, from desktop-parity 21):
// after a delivered Headless set-value or type-text into an Element (the
// --into classic insert and the caret splice), the Agent's own cursor
// draws a short caret where the field's caret now is, read through its
// Text pattern (uia.CaretEdgeOf). Best effort and presentation only: a
// field whose caret cannot be read gets no caret, and nothing here can
// fail the Action. Plain type-text into a focused classic edit names no
// Element, so it shows none.

import (
	"context"

	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// CaretReader reads where an Element's caret is: *uia.Client.
type CaretReader interface {
	CaretEdgeOf(ctx context.Context, hwnd uint64, index int) (uia.CaretEdge, bool)
}

var _ CaretReader = (*uia.Client)(nil)

// caretCursor is a Cursor that can draw the insertion caret: an Agent's
// cursor placed above its Window (anchoredCursor).
type caretCursor interface {
	Caret(x, top, bottom int32)
}

// showCaret draws the insertion caret of the Element behind index, best
// effort (see the file comment).
func (s Services) showCaret(ctx context.Context, w win.WindowInfo, index int) {
	if s.Cursor == nil {
		return
	}
	reader, ok := s.Actions.(CaretReader)
	if !ok {
		return
	}
	c, ok := s.cursorFor(ctx, w).(caretCursor)
	if !ok {
		return
	}
	edge, ok := reader.CaretEdgeOf(ctx, w.ID, index)
	if !ok {
		return
	}
	cursorBestEffort("drawing the caret", func() { c.Caret(edge.X, edge.Top, edge.Bottom) })
}
