package win

// The insertion caret (multi-agent ticket 11, from desktop-parity 21):
// after a Headless write into a text field, the Agent Cursor draws a short
// bar in the Agent's colour where the field's caret now is, for a moment,
// then fades it. It is the cursor's fourth window beside the dart, the
// ripple and the outline: placed above the same Window, hidden from
// winuse's own captures with the rest (HideWhile), and never activated.
// The timing and the bar are pure; the drawing is in cursor_view.go.

import (
	"math"
	"time"
)

const (
	// cursorCaretDuration is how long the caret shows; its last
	// cursorCaretFade fades it out.
	cursorCaretDuration = 1200 * time.Millisecond
	cursorCaretFade     = 300 * time.Millisecond
	// cursorCaretWidth is the bar's width in logical pixels.
	cursorCaretWidth = 2
)

// caretOpacity is the caret's opacity elapsed into its showing.
func caretOpacity(elapsed time.Duration) float64 {
	left := cursorCaretDuration - elapsed
	switch {
	case left <= 0:
		return 0
	case left >= cursorCaretFade:
		return 1
	}
	return float64(left) / float64(cursorCaretFade)
}

// caretBar is the bar drawn for a caret standing at x from top to bottom:
// cursorCaretWidth logical pixels wide, centred on x. Empty for a caret
// with no height.
func caretBar(x, top, bottom int32, scale float64) Rect {
	if bottom <= top {
		return Rect{}
	}
	w := int32(math.Max(1, math.Round(cursorCaretWidth*scale)))
	left := x - w/2
	return Rect{Left: left, Top: top, Right: left + w, Bottom: bottom}
}

// startCaret shows the caret bar from now. It keeps the cursor active
// (no idle fade meanwhile) but neither shows nor moves the dart.
func (m *cursorModel) startCaret(now time.Time, bar Rect) {
	if bar.Width() <= 0 || bar.Height() <= 0 {
		return
	}
	m.caret, m.caretBar, m.caretStart = true, bar, now
	m.lastActive = now
}

// CaretAbove draws the insertion caret standing at x from top to bottom
// (screen pixels) for a moment, the windows just above anchor.
// Non-blocking, like Click.
func (c *AgentCursor) CaretAbove(anchor uint64, x, top, bottom int32) {
	if c == nil {
		return
	}
	c.post(func() { c.ui.caret(anchor, x, top, bottom) })
}

// caret starts the caret (see AgentCursor.CaretAbove).
func (u *cursorUI) caret(anchor uint64, x, top, bottom int32) {
	if u.closed {
		return
	}
	now := u.now()
	_, dpi := u.view.monitorAt(fpoint{X: float64(x), Y: float64(top)})
	u.model.startCaret(now, caretBar(x, top, bottom, cursorScale(dpi)))
	u.view.raise(anchor)
	u.render(now)
}
