package helper

// The in-control indicator's helper side (ticket 13): the Services
// overlay seam, the show/hide bracket every Action runs inside, and the
// capture bracket that keeps the indicator out of winuse's own
// screenshots. The window itself lives in internal/win (on the UI thread
// shared with ticket 12's Esc hook); this file is the wiring.

import (
	"log"

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
// the Windows.Graphics.Capture method ticket 09 will prepend. The
// indicator is restored afterwards only if it was shown, so captures
// between Actions are bracketed no-ops.
type hiddenDuringCapture struct {
	overlay Overlay
	method  shot.Method
}

// Name implements shot.Method.
func (m hiddenDuringCapture) Name() string { return m.method.Name() }

// CaptureWindow implements shot.Method.
func (m hiddenDuringCapture) CaptureWindow(hwnd uint64) (shot.Frame, error) {
	if m.overlay == nil {
		return m.method.CaptureWindow(hwnd)
	}
	var frame shot.Frame
	var err error
	m.overlay.HideWhile(func() { frame, err = m.method.CaptureWindow(hwnd) })
	return frame, err
}

// captureMethods builds the capture chain's method list: the GDI
// fallbacks (spec, Observation details; ticket 09 prepends
// Windows.Graphics.Capture in front of them), each wrapped so it runs
// with the indicator hidden when one is wired.
func captureMethods(o Overlay) []shot.Method {
	methods := []shot.Method{win.RegionMethod{}, win.PrintWindowMethod{}}
	if o != nil {
		for i := range methods {
			methods[i] = hiddenDuringCapture{overlay: o, method: methods[i]}
		}
	}
	return methods
}
