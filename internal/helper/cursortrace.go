package helper

// The Agent Cursor's timeline in the helper log (browser-ux 03): with
// WINUSE_CURSOR_TRACE=1 in the helper's environment, each Headless Action
// logs when the cursor set off, when it landed, when the input was
// delivered (the ripple) and when the act returned, in milliseconds since
// the Action began. It is how the lag between the cursor and the Action is
// measured on a live desktop; off, it costs one environment read per
// Action.

import (
	"context"
	"log"
	"os"
	"time"
)

type cursorTraceKey struct{}

// cursorTraceOn reads the switch: the helper's environment, read per
// Action so a test can set it.
func cursorTraceOn() bool { return os.Getenv("WINUSE_CURSOR_TRACE") == "1" }

// withCursorTrace starts an Action's timeline (a no-op when tracing is
// off). what names the Action in the log lines.
func withCursorTrace(ctx context.Context, what string) context.Context {
	if !cursorTraceOn() {
		return ctx
	}
	return context.WithValue(ctx, cursorTraceKey{}, &cursorTimeline{what: what, start: time.Now()})
}

type cursorTimeline struct {
	what  string
	start time.Time
}

// cursorTrace logs one phase of the Action's timeline, if it has one.
func cursorTrace(ctx context.Context, phase string) {
	t, ok := ctx.Value(cursorTraceKey{}).(*cursorTimeline)
	if !ok {
		return
	}
	log.Printf("cursor trace: %s %s +%dms", t.what, phase, time.Since(t.start).Milliseconds())
}
