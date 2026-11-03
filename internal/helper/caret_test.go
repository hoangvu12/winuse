package helper

// The insertion caret (multi-agent ticket 11): after a delivered Headless
// set-value or a caret splice, the requesting Agent's cursor draws the
// caret where the field reports it; no readable caret means no caret, and
// a failed write draws none. Cursors and the element side are faked.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/agent"
	"github.com/hoangvu12/winuse/internal/uia"
)

// caretActor is a headlessSpy that reads a caret, and splices.
type caretActor struct {
	*headlessSpy
	edge   uia.CaretEdge
	ok     bool
	reads  int
	splice error
}

func (a *caretActor) CaretEdgeOf(ctx context.Context, hwnd uint64, index int) (uia.CaretEdge, bool) {
	a.step("caret-read")
	a.reads++
	return a.edge, a.ok
}

func (a *caretActor) SpliceText(ctx context.Context, hwnd uint64, index int, text string) (uia.SpliceResult, error) {
	a.step("splice")
	return uia.SpliceResult{CaretPlaced: true}, a.splice
}

func newCaretActor(ok bool) (*caretActor, *fakeCursorSet) {
	a := &caretActor{headlessSpy: newHeadlessSpy(), edge: uia.CaretEdge{X: 120, Top: 40, Bottom: 58}, ok: ok}
	a.headlessSpy.text.native = 0 // no classic edit behind Element 2
	a.svc.Actions = a
	set := newFakeCursorSet(time.Minute)
	a.svc.Cursor = set.cursorSet
	return a, set
}

func reviewer() context.Context {
	return agent.NewContext(context.Background(), agent.Identity{ID: "name:reviewer", Label: "reviewer"})
}

func TestASetValueDrawsTheAgentsCaret(t *testing.T) {
	a, set := newCaretActor(true)
	out, cmdErr := CommandsWith(a.svc)["set-value"](reviewer(), setValueArgs("101", 2, "hello"))
	if cmdErr != nil || out != okOutput {
		t.Fatalf("set-value: %q, %v", out, cmdErr)
	}
	if len(set.made) != 1 {
		t.Fatalf("made %d cursors, want reviewer's", len(set.made))
	}
	c := set.made[0]
	if len(c.carets) != 1 || c.carets[0] != [3]int32{120, 40, 58} {
		t.Errorf("carets = %v, want one at x 120 from 40 to 58", c.carets)
	}
	if c.anchors[len(c.anchors)-1] != 101 {
		t.Errorf("anchors = %v, want the caret above window 101", c.anchors)
	}
}

func TestASpliceDrawsTheCaret(t *testing.T) {
	a, set := newCaretActor(true)
	if _, cmdErr := CommandsWith(a.svc)["type-text"](reviewer(), batchStepArgs(intoArgs(2, "hi"))); cmdErr != nil {
		t.Fatalf("type-text --into: %v", cmdErr.Error())
	}
	if len(set.made) != 1 || len(set.made[0].carets) != 1 {
		t.Errorf("carets = %+v, want one after the splice", set.made)
	}
}

func TestNoCaretWithoutAReadableCaretOrAWrite(t *testing.T) {
	// No caret to read: the Action is ok, and no caret is drawn.
	a, set := newCaretActor(false)
	if out, cmdErr := CommandsWith(a.svc)["set-value"](reviewer(), setValueArgs("101", 2, "x")); cmdErr != nil || out != okOutput {
		t.Fatalf("set-value: %q, %v", out, cmdErr)
	}
	if len(set.made) == 1 && len(set.made[0].carets) != 0 {
		t.Errorf("drew a caret with none to read: %v", set.made[0].carets)
	}
	// A failed write reads no caret.
	a, _ = newCaretActor(true)
	a.splice = errors.New("write failed")
	CommandsWith(a.svc)["type-text"](reviewer(), batchStepArgs(intoArgs(2, "hi")))
	if a.reads != 0 {
		t.Errorf("read the caret %d times after a failed write", a.reads)
	}
	// A plain Cursor (no Agent set) draws none and reads none.
	a, _ = newCaretActor(true)
	a.svc.Cursor = a.cursor
	if _, cmdErr := runCommand(t, CommandsWith(a.svc), "set-value", setValueArgs("101", 2, "x")); cmdErr != nil {
		t.Fatalf("set-value: %v", cmdErr.Error())
	}
	if a.reads != 0 {
		t.Errorf("read the caret %d times for a cursor that cannot draw one", a.reads)
	}
}
