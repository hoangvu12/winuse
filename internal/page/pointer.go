package page

// Hover and drag on a Page (browser merge ticket 06): trusted mouse
// events into the Tab's session, each raced against dialogs like a click
// (Input.send). The ideas follow agent-browser's sequences (a move before
// the press, interpolated moves while the button is down); the code is
// winuse's own.
//
// A drag first asks the browser to hand HTML5 drags over to CDP
// (Input.setInterceptDrags). Without it, a drag that starts an HTML5
// drag-and-drop enters the operating system's drag loop, which follows the
// real pointer and not CDP's events. With it, the browser reports
// Input.dragIntercepted with the drag's data, and the drop is finished
// with Input.dispatchDragEvent at the target: no real pointer, no
// foreground. A drag that starts no HTML5 drag (a slider, a canvas, a
// sortable list built on mouse events) stays plain mouse events.

import (
	"context"
	"encoding/json"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp"
)

// HoverEvents is a hover at p: one move there with no button down.
func HoverEvents(p Point) []map[string]any {
	return []map[string]any{{"type": "mouseMoved", "x": p.X, "y": p.Y, "button": "none", "buttons": 0}}
}

// Hover moves the Page's mouse to p and leaves it there.
func (in *Input) Hover(ctx context.Context, p Point) error {
	for _, ev := range HoverEvents(p) {
		if err := in.send(ctx, "Input.dispatchMouseEvent", ev); err != nil {
			return err
		}
	}
	return nil
}

// DragSteps is how many moves a drag makes between its press and its
// release.
const DragSteps = 10

// dragPace is the pause between two moves of a drag: pages that start a
// drag on a timer or after a distance see a person's speed.
var dragPace = 15 * time.Millisecond

// DragEvents is a left-button drag from one point to another: a move to
// the start, the press, steps moves along the line (button held), and the
// release at the end.
func DragEvents(from, to Point, steps int) []map[string]any {
	if steps < 1 {
		steps = 1
	}
	evs := []map[string]any{
		{"type": "mouseMoved", "x": from.X, "y": from.Y, "button": "none", "buttons": 0},
		{"type": "mousePressed", "x": from.X, "y": from.Y, "button": "left", "buttons": 1, "clickCount": 1},
	}
	for i := 1; i <= steps; i++ {
		f := float64(i) / float64(steps)
		evs = append(evs, map[string]any{"type": "mouseMoved", "x": from.X + (to.X-from.X)*f, "y": from.Y + (to.Y-from.Y)*f, "button": "left", "buttons": 1})
	}
	return append(evs, map[string]any{"type": "mouseReleased", "x": to.X, "y": to.Y, "button": "left", "buttons": 0, "clickCount": 1})
}

// DragKind says how a drag ended: with plain mouse events, or as an HTML5
// drag-and-drop dropped through Input.dispatchDragEvent.
type DragKind string

const (
	DragMouse DragKind = "mouse"
	DragHTML5 DragKind = "html5"
)

// Drag drags from one point to another. HTML5 drags are intercepted and
// dropped at to through CDP's drag events (dragEnter, dragOver, drop),
// then the button is released; any other drag is the mouse events alone.
func (in *Input) Drag(ctx context.Context, from, to Point) (DragKind, error) {
	sub := in.c.Subscribe(in.session, "Input.dragIntercepted")
	defer sub.Close()
	intercept := func(on bool) {
		in.c.CallSession(context.WithoutCancel(ctx), in.session, "Input.setInterceptDrags", map[string]any{"enabled": on})
	}
	intercept(true)
	defer intercept(false)

	evs := DragEvents(from, to, DragSteps)
	release := evs[len(evs)-1]
	drop := func(got cdp.Event) (DragKind, error) {
		var p struct {
			Data json.RawMessage `json:"data"`
		}
		json.Unmarshal(got.Params, &p)
		if len(p.Data) == 0 {
			p.Data = json.RawMessage(`{"items":[],"dragOperationsMask":1}`)
		}
		for _, typ := range []string{"dragEnter", "dragOver", "drop"} {
			if err := in.send(ctx, "Input.dispatchDragEvent", map[string]any{"type": typ, "x": to.X, "y": to.Y, "data": p.Data}); err != nil {
				return DragHTML5, err
			}
		}
		return DragHTML5, in.send(ctx, "Input.dispatchMouseEvent", release)
	}
	for i, ev := range evs[:len(evs)-1] {
		if i > 1 && dragPace > 0 {
			select {
			case <-time.After(dragPace):
			case <-ctx.Done():
				return DragMouse, ctx.Err()
			}
		}
		if err := in.send(ctx, "Input.dispatchMouseEvent", ev); err != nil {
			return DragMouse, err
		}
		select {
		case got := <-sub.Events():
			return drop(got)
		default:
		}
	}
	// The last move may start the drag; its event can trail the move's
	// answer.
	select {
	case got := <-sub.Events():
		return drop(got)
	case <-time.After(dragSettle):
	}
	return DragMouse, in.send(ctx, "Input.dispatchMouseEvent", release)
}

// dragSettle is how long a drag waits after its last move for the browser
// to report an HTML5 drag.
var dragSettle = 50 * time.Millisecond
