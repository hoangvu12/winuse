package helper

// Typing, keys and clicks into a Chromium or Electron Window without a
// Browser Connection (headless-max ticket 20, ADR 0013 rule 7): window
// messages sent to the top-level Chrome_WidgetWin_1 (win/chromium.go).
// Automatic, with no --post, as the user decided: the route runs after the
// Browser Connection's and the classic routes and before the ticket 21
// refusal, whenever the Window is Chromium. Text is read back through the
// UIA Value pattern of the focused field where it has one; otherwise the
// output says the text was not verified. Keys and text show the page a
// focus and blur (the activation pair), and the output says so; clicks
// need no activation. A minimized Window is refused (Chromium would
// restore itself and take the foreground), and so is a Window whose
// thread owns the foreground. WebView2 hosts are not covered.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/keys"
	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// FocusReader reads the value of the field with the keyboard focus among a
// Window's stored Elements (the Chromium route's read-back). The real
// implementation is *uia.Client; Services.Actions is type-asserted to it.
type FocusReader interface {
	FocusedValue(ctx context.Context, hwnd uint64) (value string, found bool, err error)
}

var _ FocusReader = (*uia.Client)(nil)

// The Chromium route's outputs.
const (
	chromiumTypedOutput      = "ok\nNote: typed as window messages; the page saw a focus and blur around it\n"
	chromiumUnverifiedOutput = "ok\nNote: typed as window messages, not verified: the focused field has no readable value; read the Window State to check\n"
	chromiumKeyOutput        = "ok\nNote: pressed as window messages; the page saw a focus and blur around it\n"
	chromiumClickOutput      = "ok\nNote: clicked as window messages\n"
)

// chromiumWindow reports a Chromium browser's or Electron App's top-level
// Window.
func chromiumWindow(w win.WindowInfo) bool {
	return w.Class == win.ChromiumClass
}

// chromiumRun is one Chromium route Action on a resolved Window.
type chromiumRun struct {
	label  string                  // the Agent Cursor's label
	target win.Rect                // where the cursor travels; empty: the Window
	send   func() (int, error)     // the messages; how many were sent
	done   func() (string, *Error) // the output once sent (the read-back for text)
	retry  func(id string) string  // the --headed retry
}

// runChromium runs a Chromium route Action: the Headless refusals (a
// minimized Window among them), then, in a passive Esc scope, the Agent
// Cursor and the messages. A refusal from the route itself (the Window's
// thread owns the foreground) sends nothing.
func (s Services) runChromium(ctx context.Context, w win.WindowInfo, run chromiumRun) (string, *Error) {
	noteRoute(ctx, w, protocol.RouteHeadless) // trace.go
	if rerr := s.headlessRefusals(w, adminMessageHint); rerr != nil {
		return "", rerr
	}
	esc := s.Esc
	if esc == nil {
		esc = noopEscWatch{}
	}
	defer armPassive(esc)()
	rect, shown := s.cursorTravelRect(ctx, w, run.target, run.label)
	if esc.Stopped() {
		return "", escStopBeforeActError()
	}
	if rerr := s.sameWindow(ctx, w.ID); rerr != nil {
		return "", rerr // the identity re-check (identity.go)
	}
	n, err := run.send()
	if shown && n > 0 {
		s.cursorClick(ctx, w, rect)
	}
	if esc.Stopped() {
		return "", escStopError()
	}
	if err != nil {
		id := strconv.FormatUint(w.ID, 10)
		switch {
		case errors.Is(err, win.ErrChordForeground):
			return "", headedRefusal(w,
				fmt.Sprintf("window %d's thread owns the foreground: the user is working in that browser, and winuse does not fake its activation under them", w.ID),
				run.retry, "")
		case n == 0 && !errors.Is(err, win.ErrAfterActivation):
			return "", nothingSent(Fail(err.Error(), "the Window may have closed or be hung; run winuse list-windows"))
		case ctx.Err() != nil:
			return "", inputAbortError(s, ctx, err, "while sending the messages (the part sent so far landed)")
		}
		return "", Fail(err.Error(), "the Action may have taken effect; read the Window State (winuse get-window-state "+id+") before repeating it")
	}
	return run.done()
}

// chromiumType types text into a Chromium Window: ok false when the Window
// is not Chromium (the next route decides).
func (s Services) chromiumType(ctx context.Context, w win.WindowInfo, text string) (string, *Error, bool) {
	if !chromiumWindow(w) || s.ChromiumText == nil {
		return "", nil, false
	}
	id := strconv.FormatUint(w.ID, 10)
	out, rerr := s.runChromium(ctx, w, chromiumRun{
		label: "type",
		retry: func(id string) string { return "type-text " + id + " <text>" },
		send: func() (int, error) {
			return s.ChromiumText(w.ID, text, func() bool { return s.Esc.Stopped() || ctx.Err() != nil })
		},
		done: func() (string, *Error) {
			fr, ok := s.Actions.(FocusReader)
			if !ok {
				return chromiumUnverifiedOutput, nil
			}
			switch uia.VerifyTyped(text, func() (string, bool, error) { return fr.FocusedValue(ctx, w.ID) }, s.Sleep) {
			case uia.TypedShown:
				return chromiumTypedOutput, nil
			case uia.TypedMissing:
				return "", Fail("the text was typed as window messages, but the focused field does not show it",
					"the Action was delivered; do not repeat it, read the Window State: winuse get-window-state "+id)
			}
			return chromiumUnverifiedOutput, nil
		},
	})
	return out, rerr, true
}

// chromiumPress presses a key or chord in a Chromium Window: one key
// inside the activation pair, a chord through attached key state with the
// pair (ticket 07's PostChord). A Blocked Combo, and a chord the attached
// state cannot carry, are not this route's: ok false.
func (s Services) chromiumPress(ctx context.Context, w win.WindowInfo, chord keys.Chord, force bool) (string, *Error, bool) {
	if !chromiumWindow(w) || s.ChromiumKey == nil {
		return "", nil, false
	}
	if _, blocked := keys.Blocked(chord); blocked {
		return "", nil, false // never sent as messages, --force or not
	}
	send := func() (int, error) { return s.ChromiumKey(w.ID, chord[0]) }
	if postedModifier(chord) {
		plan, err := win.PlanPostedChord(chord, w.Class)
		if err != nil || s.PostChord == nil {
			return "", nil, false
		}
		send = func() (int, error) { return s.PostChord(w.ID, w.ID, plan) }
	} else if len(chord) != 1 {
		return "", nil, false
	}
	flag := ""
	if force {
		flag = " --force"
	}
	out, rerr := s.runChromium(ctx, w, chromiumRun{
		label: cursorLabel("press "+chord.String(), ""),
		retry: func(id string) string { return "press-key " + id + " " + chord.String() + flag },
		send:  send,
		done:  func() (string, *Error) { return chromiumKeyOutput, nil },
	})
	return out, rerr, true
}

// chromiumClick clicks a Chromium Window at a point or an Element's
// centre with mouse messages and no activation: the clicks a Headless
// pattern call has no route for (double, triple, right and middle). ok is
// false when the Window is not Chromium.
func (s Services) chromiumClick(ctx context.Context, ref string, target elementTarget, button win.MouseButton, count int) (string, *Error, bool) {
	if s.ChromiumClick == nil {
		return "", nil, false
	}
	w, rerr := resolveWindow(s, ref)
	if rerr != nil || !chromiumWindow(w) {
		return "", nil, false
	}
	at := target.token()
	var sx, sy int32
	var rect win.Rect
	if target.point != nil {
		at = pointToken(target.point.X, target.point.Y)
		pt, serr := shotPoint(ctx, s, w, target.point.X, target.point.Y, "click")
		if serr != nil {
			return "", nothingSent(serr), true
		}
		sx, sy = int32(pt.X), int32(pt.Y)
		rect = win.Rect{Left: sx - 4, Top: sy - 4, Right: sx + 4, Bottom: sy + 4}
	} else {
		x, y, r, cerr := s.elementCentre(ctx, w, target)
		if cerr != nil {
			return "", nothingSent(cerr), true
		}
		sx, sy, rect = x, y, r
	}
	out, rerr := s.runChromium(ctx, w, chromiumRun{
		label:  strings.TrimPrefix(clickDescription(button, count), "a "),
		target: rect,
		retry:  func(id string) string { return "click " + id + " " + at + clickOptions(button, count) },
		send:   func() (int, error) { return s.ChromiumClick(w.ID, sx, sy, button, count) },
		done:   func() (string, *Error) { return chromiumClickOutput, nil },
	})
	return out, rerr, true
}

// chromiumStep is the Batch's scope question for the Chromium route: an
// unheaded, unposted press-key or plain type-text, or a double, triple,
// right or middle click, on a Chromium Window sends no input and takes
// no foreground.
func chromiumStep(kind string, raw json.RawMessage, w *win.WindowInfo) bool {
	if w == nil || !chromiumWindow(*w) {
		return false
	}
	var f struct {
		Headed      bool   `json:"headed"`
		Post        bool   `json:"post"`
		IntoIndex   *int   `json:"into_index"`
		IntoRef     string `json:"into_ref"`
		ClickCount  int    `json:"click_count"`
		MouseButton string `json:"mouse_button"`
	}
	if json.Unmarshal(raw, &f) != nil || f.Headed || f.Post {
		return false
	}
	switch kind {
	case "press-key":
		return true
	case "type-text":
		return f.IntoIndex == nil && f.IntoRef == ""
	case "click":
		button, ok := parseMouseButton(f.MouseButton)
		return ok && (f.ClickCount > 1 || button != win.MouseLeft)
	}
	return false
}
