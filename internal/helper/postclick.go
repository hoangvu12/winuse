package helper

// Posted Clicks and posted wheel scrolls (headless-max ticket 12, ADR 0013
// rule 2): `click ... --post` and `scroll @x,y <dx> <dy> --post` post
// mouse messages to the classic Win32 control under the target, without
// the foreground, the real pointer or the banner. Like Posted Keys they
// are opt-in and best effort: the app may ignore them and nothing reads
// the effect back, so the output says to read the Window State.
//
// The control is found from the target Window down (Services.ChildAtPoint,
// never the desktop's topmost Window), so a covered Window is addressed,
// and only the allow-listed classic classes take the messages
// (win.PostableClass). Anything else is refused with nothing sent and the
// --headed retry. The refusals are Posted Keys': a locked desktop, an
// administrator Window, a Window blocked by a modal dialog or minimized.

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/win"
)

// The Posted Clicks' and posted scrolls' success outputs: a posted
// message has no readable outcome.
const (
	postedClickOutput  = "ok\nNote: posted; the app may ignore posted clicks, read the Window State to check\n"
	postedScrollOutput = "ok\nNote: posted; the app may ignore posted scrolls, read the Window State to check\n"
)

// postMouseFlagsError refuses `post` together with `headed` on click and
// scroll: the CLI catches it as a usage error first; the helper refuses it
// for any other client, and a Batch while it prepares its steps.
func postMouseFlagsError(cmd string) *Error {
	return Fail(cmd+": --post and --headed cannot be combined",
		"--post posts the mouse messages to the Window's control without the foreground; --headed uses the real pointer in the foreground; pass one of them")
}

// postAltRefusal refuses --modifiers alt with --post: a posted mouse
// message carries Ctrl and Shift in its key state, and nothing for Alt.
func postAltRefusal(cmd string) *Error {
	return nothingSent(Fail(cmd+" --post cannot carry Alt: a posted mouse message holds only Ctrl and Shift",
		"drop alt, or pass --headed instead of --post to hold the real Alt key"))
}

// postedRun is one posted mouse Action on a resolved Window.
type postedRun struct {
	cmd   string // "click" or "scroll", for the messages
	at    string // where, as the agent named it: "@x,y" or "element 5"
	label string // the Agent Cursor's label
	retry func(id string) string
	// screen finds the screen point to post at and the rectangle the Agent
	// Cursor travels to; it sends nothing.
	screen func(w win.WindowInfo) (sx, sy int32, rect win.Rect, rerr *Error)
	// post posts to the control at its client point (cx, cy) or the screen
	// point, and returns how many messages it posted.
	post   func(target uint64, cx, cy, sx, sy int32) (int, error)
	output string
}

// runPosted runs a posted mouse Action: the Window resolves, Posted Keys'
// refusals run, the screen point is found, the control under it is found
// from the Window down and checked against the allow-list, then, inside a
// passive Esc scope, the Agent Cursor travels and the messages are posted.
// Every refusal before the post says nothing was sent.
func (s Services) runPosted(ctx context.Context, ref string, run postedRun) (string, *Error) {
	w, rerr := resolveWindow(s, ref)
	if rerr != nil {
		return "", rerr
	}
	noteRoute(ctx, w, protocol.RoutePosted) // trace.go
	if rerr := s.headlessRefusals(w, adminMessageHint); rerr != nil {
		return "", rerr
	}
	sx, sy, rect, rerr := run.screen(w)
	if rerr != nil {
		return "", nothingSent(rerr)
	}
	target, cx, cy, class := s.ChildAtPoint(w.ID, sx, sy)
	if target == 0 || !win.PostableClass(class) {
		what := "the Window itself"
		if target != 0 && target != w.ID {
			what = "a " + strconv.Quote(class) + " control"
		}
		return "", headedRefusal(w,
			fmt.Sprintf("%s --post at %s has no posted route: under it is %s, not a classic control that takes posted mouse messages", run.cmd, run.at, what),
			run.retry, "")
	}

	esc := s.Esc
	if esc == nil {
		esc = noopEscWatch{}
	}
	defer armPassive(esc)()
	var shown bool
	rect, shown = s.cursorTravelRect(ctx, w, rect, run.label)
	if esc.Stopped() {
		return "", escStopBeforeActError()
	}
	n, err := run.post(target, cx, cy, sx, sy)
	if shown && n > 0 {
		s.cursorClick(ctx, w, rect)
	}
	if esc.Stopped() {
		return "", escStopError()
	}
	if err != nil {
		id := strconv.FormatUint(w.ID, 10)
		if n == 0 {
			return "", nothingSent(Fail(err.Error(), "the Window or its control may have closed; run winuse list-windows"))
		}
		return "", Fail(err.Error(), "the Action may have taken effect; read the Window State (winuse get-window-state "+id+") before repeating it")
	}
	return run.output, nil
}

// postClick runs `click ... --post`: at a point, the point maps through
// the latest screenshot; on an Element, the centre of its live bounds is
// the point. Any button and count, with Ctrl and Shift as MK_* key state
// (desktop-parity ticket 06).
func (s Services) postClick(ctx context.Context, a protocol.ClickArgs, target elementTarget, button win.MouseButton, count int, mods win.Modifiers) (string, *Error) {
	if a.Headed {
		return "", postMouseFlagsError("click")
	}
	if mods.Alt {
		return "", postAltRefusal("click")
	}
	at := ""
	run := postedRun{
		cmd:    "click",
		label:  "post " + strings.TrimPrefix(clickDescription(button, count), "a "), // "post click", "post double click"
		output: postedClickOutput,
		post: func(t uint64, cx, cy, _, _ int32) (int, error) {
			return s.PostClick(t, cx, cy, button, count, mods)
		},
	}
	if target.point != nil {
		x, y := target.point.X, target.point.Y
		at = pointToken(x, y)
		run.screen = func(w win.WindowInfo) (int32, int32, win.Rect, *Error) {
			pt, serr := shotPoint(ctx, s, w, x, y, "click")
			if serr != nil {
				return 0, 0, win.Rect{}, serr
			}
			return int32(pt.X), int32(pt.Y), win.Rect{Left: int32(pt.X) - 4, Top: int32(pt.Y) - 4, Right: int32(pt.X) + 4, Bottom: int32(pt.Y) + 4}, nil
		}
	} else {
		at = target.token()
		run.screen = func(w win.WindowInfo) (int32, int32, win.Rect, *Error) {
			return s.elementCentre(ctx, w, target)
		}
	}
	run.at = at
	run.retry = func(id string) string { return "click " + id + " " + at + clickOptions(button, count) }
	return s.runPosted(ctx, a.Window, run)
}

// elementCentre is the centre of the live bounds of the Element behind
// target: where a posted click on an Element lands. It sends nothing.
func (s Services) elementCentre(ctx context.Context, w win.WindowInfo, target elementTarget) (int32, int32, win.Rect, *Error) {
	actor, ok := s.Actions.(HeadlessActor)
	if !ok {
		return 0, 0, win.Rect{}, Fail("click --post on an Element is unavailable: no UIA client", "")
	}
	index, rerr := target.resolve(ctx, s, w)
	if rerr != nil {
		return 0, 0, win.Rect{}, rerr
	}
	l, t, r, b, _, err := actor.ElementBounds(ctx, w.ID, index)
	if err != nil {
		return 0, 0, win.Rect{}, elementError(err, w, index)
	}
	if r <= l || b <= t {
		return 0, 0, win.Rect{}, Fail(fmt.Sprintf("element %d has no area on screen to post a click at", index),
			"scroll it into view (winuse scroll "+strconv.FormatUint(w.ID, 10)+" "+target.token()+" into-view), then retry")
	}
	rect := win.Rect{Left: l, Top: t, Right: r, Bottom: b}
	return (l + r) / 2, (t + b) / 2, rect, nil
}

// postScroll runs `scroll @x,y <dx> <dy> --post`: posted WM_MOUSEWHEEL
// (vertical) and WM_MOUSEHWHEEL (horizontal) at the point, which the wheel
// messages carry in screen coordinates.
func (s Services) postScroll(ctx context.Context, a protocol.ScrollArgs, mods win.Modifiers) (string, *Error) {
	if a.Headed {
		return "", postMouseFlagsError("scroll")
	}
	if mods.Alt {
		return "", postAltRefusal("scroll")
	}
	x, y, dx, dy := a.X, a.Y, a.ScrollX, a.ScrollY
	at := pointToken(x, y)
	return s.runPosted(ctx, a.Window, postedRun{
		cmd:    "scroll",
		at:     at,
		label:  "post " + pointScrollVerb(dx, dy),
		output: postedScrollOutput,
		retry:  func(id string) string { return fmt.Sprintf("scroll %s %s %d %d", id, at, dx, dy) },
		screen: func(w win.WindowInfo) (int32, int32, win.Rect, *Error) {
			pt, serr := shotPoint(ctx, s, w, x, y, "scroll")
			if serr != nil {
				return 0, 0, win.Rect{}, serr
			}
			return int32(pt.X), int32(pt.Y), win.Rect{Left: int32(pt.X) - 4, Top: int32(pt.Y) - 4, Right: int32(pt.X) + 4, Bottom: int32(pt.Y) + 4}, nil
		},
		post: func(t uint64, _, _, sx, sy int32) (int, error) {
			return s.PostWheel(t, sx, sy, int32(dx), int32(dy), mods)
		},
	})
}

// postedStep is the Batch's scope question for a posted click or scroll:
// it takes no foreground, banner or real input, like a posted key, so it
// counts as Headless. With `headed` the step is refused while the Batch
// prepares (postsHeaded).
func postedStep(kind string, args json.RawMessage) bool {
	switch kind {
	case "click", "scroll":
		var f struct {
			Post   bool `json:"post"`
			Headed bool `json:"headed"`
		}
		return json.Unmarshal(args, &f) == nil && f.Post && !f.Headed
	}
	return false
}
