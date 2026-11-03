package helper

// The helper's part of a session trace (desktop-parity ticket 19): a
// traced request's response names the Window its command resolved and
// the routes it took; an untraced one carries no trace. Everything is
// faked; no desktop is touched.

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

func TestServerAnswersTheTraceNotesOnlyWhenAsked(t *testing.T) {
	notepad := win.WindowInfo{ID: 101, App: "Notepad", Title: "Untitled - Notepad"}
	cmds := map[string]Command{
		"probe": func(ctx context.Context, args json.RawMessage) (string, *Error) {
			noteRoute(ctx, notepad, protocol.RouteHeadless)
			noteRoute(ctx, win.WindowInfo{ID: 202, App: "Chrome"}, protocol.RouteCDP)
			noteRoute(ctx, notepad, protocol.RouteHeadless)
			return "", Fail("refused; nothing was sent", "")
		},
	}
	srv := New("v1", time.Minute, time.Second, cmds, discardLogger())

	resp := srv.handle(&protocol.Request{V: protocol.Version, Ver: "v1", Cmd: "probe", Trace: true})
	want := &protocol.Trace{
		Window: &protocol.TraceWindow{ID: 101, App: "Notepad", Title: "Untitled - Notepad"},
		Routes: []string{protocol.RouteHeadless, protocol.RouteCDP},
	}
	if resp.OK || !reflect.DeepEqual(resp.Trace, want) {
		t.Errorf("traced failure = %+v, trace %+v; want the first Window and each route once", resp, resp.Trace)
	}

	resp = srv.handle(&protocol.Request{V: protocol.Version, Ver: "v1", Cmd: "probe"})
	if resp.Trace != nil {
		t.Errorf("untraced response carries %+v, want no trace", resp.Trace)
	}
	raw, _ := json.Marshal(resp)
	if string(raw) != `{"ok":false,"err":"refused; nothing was sent","exit":1,"code":"INTERNAL","delivery":"not_delivered","retry":"safe"}` {
		t.Errorf("untraced response on the wire = %s, want no trace field", raw)
	}
}

// tracedRun runs one registered command under a traced request's
// context and returns its notes.
func tracedRun(t *testing.T, cmds map[string]Command, cmd string, args any) *protocol.Trace {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	ctx, notes := withTraceNotes(context.Background(), true)
	cmds[cmd](ctx, raw)
	return notes.report()
}

func TestTheActionPipelinesNoteTheirRoute(t *testing.T) {
	notepad := &protocol.TraceWindow{ID: 101, App: "Notepad", Title: "Untitled - Notepad"}

	// A v1 click through runAction (headed, headless-max 21): Headed.
	spy := &actionSpy{point: uia.Point{X: 100, Y: 200}}
	got := tracedRun(t, CommandsWith(spy.services()), "click", headedArgs(clickArgs("101", 2, 1, "left")))
	if want := (&protocol.Trace{Window: notepad, Routes: []string{protocol.RouteHeaded}}); !reflect.DeepEqual(got, want) {
		t.Errorf("click trace = %+v, want %+v", got, want)
	}

	// A Window Action through runWindowAction: Headless.
	ws := newWindowSpy()
	ws.minimized = true
	got = tracedRun(t, CommandsWith(ws.svc), "restore-window", protocol.WindowActionArgs{Window: "101"})
	if want := (&protocol.Trace{Window: notepad, Routes: []string{protocol.RouteHeadless}}); !reflect.DeepEqual(got, want) {
		t.Errorf("restore-window trace = %+v, want %+v", got, want)
	}
}

func TestATracedReadNamesItsWindow(t *testing.T) {
	cmds := CommandsWith(testServices(&fakeTrees{}))
	got := tracedRun(t, cmds, "get-window", protocol.GetWindowArgs{Window: "Notepad"})
	want := &protocol.Trace{Window: &protocol.TraceWindow{ID: 101, App: "Notepad", Title: "Untitled - Notepad"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("get-window trace = %+v, want %+v (no route: a read takes none)", got, want)
	}
	// A command that names no Window notes nothing.
	if got := tracedRun(t, cmds, "list-windows", nil); got != nil {
		t.Errorf("list-windows trace = %+v, want none", got)
	}
	// Untraced, the wrapper resolves nothing.
	calls := 0
	s := testServices(&fakeTrees{})
	s.Windows = func() ([]win.WindowInfo, error) { calls++; return fakeWindows(), nil }
	raw, _ := json.Marshal(protocol.GetWindowArgs{Window: "101"})
	CommandsWith(s)["get-window"](context.Background(), raw)
	if calls != 1 {
		t.Errorf("an untraced get-window enumerated the Windows %d times, want 1 (its own)", calls)
	}
}

func TestKeyRunRoute(t *testing.T) {
	if keyRunRoute(true) != protocol.RoutePosted || keyRunRoute(false) != protocol.RouteHeadless {
		t.Errorf("keyRunRoute: posted %q, not posted %q", keyRunRoute(true), keyRunRoute(false))
	}
}
