package helper

// The in-control indicator's helper side (ticket 13): the Services
// overlay seam, the show/hide bracket every Action runs inside, and the
// capture bracket that keeps the indicator out of winuse's own
// screenshots. The window itself lives in internal/win (on the UI thread
// shared with ticket 12's Esc hook); this file is the wiring.

import (
	"github.com/hoangvu12/winuse/internal/agent"
	"log"
	"sync"

	"github.com/hoangvu12/winuse/internal/shot"
	"github.com/hoangvu12/winuse/internal/win"
)

// Overlay is the on-screen indicator that winuse is in control while an
// Action or Batch runs: click-through, never focused, and kept out of
// winuse's own screenshots. The real implementation is *win.Overlay,
// wired by the helper's Main; tests fake it to assert the brackets.
type Overlay interface {
	// Show raises the indicator over r, the target Window's bounds.
	Show(r win.Rect) error
	// Hide removes the indicator.
	Hide() error
	// HideWhile runs f with the indicator hidden, restoring its previous
	// visibility afterwards. Capture methods bracket themselves with it,
	// so winuse's own screenshots never contain the indicator — the
	// deterministic exclusion, independent of the overlay's
	// display-affinity support (win32.md flags it as unverified that
	// WDA_EXCLUDEFROMCAPTURE also hides a layered window from GDI BitBlt).
	HideWhile(f func())
}

// bracketOverlay nests Show/Hide brackets (finding 12): a Batch shows the
// indicator once for its whole run, and every step's own bracket rides
// inside it — the inner hides only decrement, and the actual hide lands
// when the outermost bracket ends, like the Esc arm scope. A step's Show
// over the same Window re-positions harmlessly (the inner Show is a
// no-op while a bracket is open). HideWhile passes straight through: a
// capture hides and restores the real indicator underneath, whatever the
// bracket count.
type bracketOverlay struct {
	inner Overlay
	mu    sync.Mutex
	shown int // open brackets; the real hide runs when it returns to 0
}

// newBracketOverlay wraps inner so Show/Hide nest.
func newBracketOverlay(inner Overlay) *bracketOverlay {
	return &bracketOverlay{inner: inner}
}

// Show implements Overlay: the first bracket raises the indicator, inner
// brackets only count (the caller still learns a Show failure).
func (o *bracketOverlay) Show(r win.Rect) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.shown == 0 {
		if err := o.inner.Show(r); err != nil {
			return err
		}
	}
	o.shown++
	return nil
}

// Hide implements Overlay: the last open bracket hides the indicator.
func (o *bracketOverlay) Hide() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.shown == 0 {
		return nil // nothing is shown; a stray hide is a no-op
	}
	o.shown--
	if o.shown == 0 {
		return o.inner.Hide()
	}
	return nil
}

// HideWhile implements Overlay: delegated to the real indicator, which
// hides for the capture and restores its own visibility afterwards.
func (o *bracketOverlay) HideWhile(f func()) {
	o.inner.HideWhile(f)
}

// showOverlay raises the indicator over the Window an Action is about to
// control. Best effort: an Action never fails because the indicator could
// not be shown (the pipeline logs and carries on).
func (s Services) showOverlay(w win.WindowInfo) {
	if s.Overlay == nil {
		return
	}
	if err := s.Overlay.Show(w.Bounds); err != nil {
		log.Printf("showing the in-control overlay over window %d: %v", w.ID, err)
	}
}

// hideOverlay removes the indicator at the end of an Action. Best effort,
// like showOverlay.
func (s Services) hideOverlay() {
	if s.Overlay == nil {
		return
	}
	if err := s.Overlay.Hide(); err != nil {
		log.Printf("hiding the in-control overlay: %v", err)
	}
}

// hiddenDuringCapture wraps one capture method so it runs with the
// indicator hidden (Overlay.HideWhile), whatever the method — the region
// BitBlt where a layered window might be composited in, PrintWindow, and
// and the Windows.Graphics.Capture method (ticket 09). The
// indicator is restored afterwards only if it was shown, so captures
// between Actions are bracketed no-ops. The Agent Cursor (ADR 0010) is
// hidden the same way, inside the indicator's bracket, when one is wired.
type hiddenDuringCapture struct {
	overlay Overlay
	cursor  Cursor
	method  shot.Method
}

// Name implements shot.Method.
func (m hiddenDuringCapture) Name() string { return m.method.Name() }

// CaptureWindow implements shot.Method.
func (m hiddenDuringCapture) CaptureWindow(hwnd uint64) (shot.Frame, error) {
	var frame shot.Frame
	var err error
	capture := func() { frame, err = m.method.CaptureWindow(hwnd) }
	if m.cursor != nil {
		inner := capture
		capture = func() { m.cursor.HideWhile(inner) }
	}
	if m.overlay != nil {
		inner := capture
		capture = func() { m.overlay.HideWhile(inner) }
	}
	capture()
	return frame, err
}

// captureMethods builds the capture chain's method list: first (the
// Windows.Graphics.Capture method in production, ticket 09), then the GDI
// fallbacks (spec, Observation details), each wrapped so it runs with the
// indicator hidden when one is wired.
func captureMethods(o Overlay, first ...shot.Method) []shot.Method {
	return captureMethodsHiding(o, nil, first...)
}

// captureMethodsHiding is captureMethods with the Agent Cursor too: each
// method runs with the indicator and the cursor hidden, whichever of the
// two is wired, so winuse's own screenshots contain neither. This is the
// full chain that --headed captures through, after activation.
func captureMethodsHiding(o Overlay, c Cursor, first ...shot.Method) []shot.Method {
	return hideDuringCapture(o, c, append(append([]shot.Method{}, first...), win.RegionMethod{}, win.PrintWindowMethod{}))
}

// headlessCaptureMethods is the headless chain (ADR 0011): first, then
// PrintWindow, hidden the same way. The screen region method is left out:
// it copies whatever is on top, so it needs the Window in the foreground.
func headlessCaptureMethods(o Overlay, c Cursor, first ...shot.Method) []shot.Method {
	return hideDuringCapture(o, c, append(append([]shot.Method{}, first...), win.PrintWindowMethod{}))
}

// hideDuringCapture wraps each method in the capture bracket when an
// indicator or a cursor is wired, and leaves them plain otherwise.
func hideDuringCapture(o Overlay, c Cursor, methods []shot.Method) []shot.Method {
	if o != nil || c != nil {
		for i := range methods {
			methods[i] = hiddenDuringCapture{overlay: o, cursor: c, method: methods[i]}
		}
	}
	return methods
}

// agentBanner is the indicator's optional side that names the agent it
// is in control for (multi-agent 06): *win.Overlay.
type agentBanner interface {
	SetAgent(label string) error
}

// SetAgent forwards to the wrapped indicator when it names agents.
func (o *bracketOverlay) SetAgent(label string) error {
	if b, ok := o.inner.(agentBanner); ok {
		return b.SetAgent(label)
	}
	return nil
}

// setOverlayAgent makes the banner name the agent that holds the desktop
// lane; the default agent (one agent, no identity) is not named. Best
// effort, like showOverlay.
func (s Services) setOverlayAgent(who agent.Identity) {
	b, ok := s.Overlay.(agentBanner)
	if !ok {
		return
	}
	label := who.Label
	if who.ID == agent.DefaultID {
		label = ""
	}
	if err := b.SetAgent(label); err != nil {
		log.Printf("naming the agent on the in-control overlay: %v", err)
	}
}
