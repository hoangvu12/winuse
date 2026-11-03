package helper

// select-text's offline tests (headless-max ticket 10): the classic edit
// route (EM_SETSEL with offsets and all, the RichEdit `all` read-back, a
// span past the end, the read-back and send failures), the Text pattern
// route (an Element with no edit window, no Text pattern, a short text),
// the Page route through the Browser Connection, the argument checks, the
// routing predicate and the Batch step. Everything Win32, element-side
// and CDP is faked; no desktop is touched.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
	"github.com/hoangvu12/winuse/internal/win"
)

// textRecord is what select-text's fakes answer and record.
type textRecord struct {
	native    uint64 // ElementWindow's answer
	nativeErr error
	edit      win.Control // EditControl's answer
	index     int
	span      uia.TextSpan
	err       error // SelectText's outcome (the Text pattern route)

	states   []win.EditState // EditState's answers, in order
	stateErr []error
	reads    int
	sel      [][2]int // SetEditSel calls
	selErr   error
}

// ElementWindow implements TextSelector.
func (s *headlessSpy) ElementWindow(ctx context.Context, hwnd uint64, index int) (uint64, error) {
	s.step("element-window")
	s.text.index = index
	return s.text.native, s.text.nativeErr
}

// SelectText implements TextSelector.
func (s *headlessSpy) SelectText(ctx context.Context, hwnd uint64, index int, span uia.TextSpan) error {
	s.step("text-select")
	s.text.index, s.text.span = index, span
	s.esc.pressIf("act")
	return s.text.err
}

// newTextSpy is a headlessSpy whose classic edit seams are faked: Element
// 2's own window is the Edit control 555 holding 11 characters, with the
// selection read back as asked.
func newTextSpy() *headlessSpy {
	s := newHeadlessSpy()
	s.text.native = 555
	s.text.edit = win.Control{HWND: 555, Class: "Edit", Enabled: true}
	s.text.states = []win.EditState{{Length: 11, SelKnown: true}}
	s.svc.EditControl = func(hwnd uint64) win.Control {
		s.step("edit-control")
		return s.text.edit
	}
	s.svc.EditState = func(edit uint64) (win.EditState, error) {
		s.step("edit-state")
		i := s.text.reads
		s.text.reads++
		if i < len(s.text.stateErr) && s.text.stateErr[i] != nil {
			return win.EditState{}, s.text.stateErr[i]
		}
		if i < len(s.text.states) {
			return s.text.states[i], nil
		}
		// The read-back: the selection last sent, -1 as the length.
		st := s.text.states[0]
		if n := len(s.text.sel); n > 0 {
			st.SelStart, st.SelEnd = s.text.sel[n-1][0], s.text.sel[n-1][1]
			if st.SelEnd < 0 {
				st.SelEnd = st.Length
			}
		}
		return st, nil
	}
	s.svc.SetEditSel = func(edit uint64, start, end int) error {
		s.step("em-setsel")
		s.text.sel = append(s.text.sel, [2]int{start, end})
		s.esc.pressIf("act")
		return s.text.selErr
	}
	return s
}

func selectArgs(index, start, length int) protocol.SelectTextArgs {
	return protocol.SelectTextArgs{Window: "101", ElementIndex: intPtrBatch(index), Start: start, Length: length}
}

func selectAll(index int) protocol.SelectTextArgs {
	return protocol.SelectTextArgs{Window: "101", ElementIndex: intPtrBatch(index), All: true}
}

func TestSelectTextClassicEdit(t *testing.T) {
	spy := newTextSpy()
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "select-text", selectArgs(2, 6, 5))
	if cmdErr != nil || out != okOutput {
		t.Fatalf("select-text: %q, %v", out, cmdErr)
	}
	want := []string{"locked", "integrity", "ready", "bounds", "cursor-travel",
		"element-window", "edit-control", "edit-state", "em-setsel", "edit-state", "cursor-click"}
	if got := spy.recorded(); !equalStrings(got, want) {
		t.Errorf("steps = %v, want %v", got, want)
	}
	assertNoV1(t, spy.recorded())
	if !reflect.DeepEqual(spy.text.sel, [][2]int{{6, 11}}) {
		t.Errorf("EM_SETSEL %v, want 6 to 11", spy.text.sel)
	}
	if got := spy.cursor.labels; len(got) != 1 || got[0] != `select text "Save"` {
		t.Errorf("labels = %q", got)
	}
	if b := spy.esc.brackets(); !equalStrings(b, []string{"arm-passive", "disarm-passive"}) {
		t.Errorf("Esc brackets = %v, want one passive scope", b)
	}

	// A length of 0 places the caret, here at the end of the text.
	spy = newTextSpy()
	if _, cmdErr := runCommand(t, CommandsWith(spy.svc), "select-text", selectArgs(2, 11, 0)); cmdErr != nil {
		t.Fatalf("caret: %v", cmdErr.Error())
	}
	if !reflect.DeepEqual(spy.text.sel, [][2]int{{11, 11}}) {
		t.Errorf("EM_SETSEL %v, want 11 to 11", spy.text.sel)
	}
}

func TestSelectTextClassicEditAll(t *testing.T) {
	spy := newTextSpy()
	if _, cmdErr := runCommand(t, CommandsWith(spy.svc), "select-text", selectAll(2)); cmdErr != nil {
		t.Fatalf("all: %v", cmdErr.Error())
	}
	if !reflect.DeepEqual(spy.text.sel, [][2]int{{0, -1}}) {
		t.Errorf("EM_SETSEL %v, want 0 to -1 (the control's own end)", spy.text.sel)
	}

	// A RichEdit counts a paragraph break as two units in its length and one
	// in EM_GETSEL: all reads back short of the length and is still ok.
	spy = newTextSpy()
	spy.text.edit.Class = "RichEditD2DPT"
	spy.text.states = []win.EditState{{Length: 12, SelKnown: true}, {SelStart: 0, SelEnd: 11, SelKnown: true}}
	if _, cmdErr := runCommand(t, CommandsWith(spy.svc), "select-text", selectAll(2)); cmdErr != nil {
		t.Errorf("RichEdit all: %v", cmdErr.Error())
	}

	// The same read-back from a plain Edit is a mismatch.
	spy = newTextSpy()
	spy.text.states = []win.EditState{{Length: 12, SelKnown: true}, {SelStart: 0, SelEnd: 11, SelKnown: true}}
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "select-text", selectAll(2))
	if cmdErr == nil || cmdErr.Msg != "the selection was delivered but the edit control selects characters 0 to 11 afterwards, not 0 to 12" ||
		!strings.Contains(cmdErr.Hint, "do not repeat it") {
		t.Errorf("Edit all mismatch: %+v", cmdErr)
	}
}

func TestEditSelectionOK(t *testing.T) {
	sel := func(s, e int) win.EditState { return win.EditState{SelStart: s, SelEnd: e, SelKnown: true} }
	cases := []struct {
		name       string
		start, end int
		all, rich  bool
		got        win.EditState
		want       bool
	}{
		{"exact", 2, 5, false, false, sel(2, 5), true},
		{"reversed ends", 2, 5, false, false, sel(5, 2), true},
		{"off by one", 2, 5, false, false, sel(2, 4), false},
		{"unknown past 65,535", 70000, 70010, false, false, win.EditState{}, true},
		{"all", 0, 9, true, false, sel(0, 9), true},
		{"all short in an Edit", 0, 9, true, false, sel(0, 8), false},
		{"all short in a RichEdit", 0, 9, true, true, sel(0, 8), true},
		{"all of no text in a RichEdit", 0, 0, true, true, sel(0, 0), true},
		{"all nothing selected in a RichEdit", 0, 9, true, true, sel(0, 0), false},
		{"all not from 0 in a RichEdit", 0, 9, true, true, sel(1, 9), false},
		{"a span in a RichEdit is exact", 1, 4, false, true, sel(1, 3), false},
	}
	for _, c := range cases {
		if got := editSelectionOK(c.start, c.end, c.all, c.rich, c.got); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSelectTextClassicEditFailures(t *testing.T) {
	cases := []struct {
		name     string
		args     protocol.SelectTextArgs
		setup    func(*headlessSpy)
		wantMsg  string
		wantHint string
		sent     bool // EM_SETSEL went out
		ripple   bool
	}{
		{name: "past the end", args: selectArgs(2, 8, 5),
			wantMsg:  "the selection ends past the end of the text: it would end at 13, and the text holds 11 characters; nothing was sent",
			wantHint: "offsets count UTF-16 units from 0"},
		{name: "the length cannot be read", args: selectArgs(2, 0, 1),
			setup: func(s *headlessSpy) {
				s.text.stateErr = []error{errors.New("reading the length of edit control 555 failed: timeout")}
			},
			wantMsg:  "reading the length of edit control 555 failed: timeout; nothing was sent",
			wantHint: "the edit control may have closed or be busy"},
		{name: "EM_SETSEL fails", args: selectArgs(2, 0, 1),
			setup:    func(s *headlessSpy) { s.text.selErr = errors.New("EM_SETSEL on edit control 555 failed: timeout") },
			wantMsg:  "EM_SETSEL on edit control 555 failed: timeout",
			wantHint: "the selection may have changed", sent: true},
		{name: "the read-back fails", args: selectArgs(2, 0, 1),
			setup: func(s *headlessSpy) {
				s.text.stateErr = []error{nil, errors.New("gone")}
			},
			wantMsg:  "the selection was delivered but the edit control's selection could not be read back",
			wantHint: "do not repeat it", sent: true, ripple: true},
		{name: "the read-back differs", args: selectArgs(2, 6, 5),
			setup: func(s *headlessSpy) {
				s.text.states = []win.EditState{{Length: 11, SelKnown: true}, {SelStart: 6, SelEnd: 8, SelKnown: true}}
			},
			wantMsg:  "the selection was delivered but the edit control selects characters 6 to 8 afterwards, not 6 to 11",
			wantHint: "do not repeat it", sent: true, ripple: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := newTextSpy()
			if tc.setup != nil {
				tc.setup(spy)
			}
			_, cmdErr := runCommand(t, CommandsWith(spy.svc), "select-text", tc.args)
			if cmdErr == nil || cmdErr.Msg != tc.wantMsg || !strings.Contains(cmdErr.Hint, tc.wantHint) {
				t.Fatalf("error = %+v, want %q with a hint containing %q", cmdErr, tc.wantMsg, tc.wantHint)
			}
			if sent := containsStep(spy.recorded(), "em-setsel"); sent != tc.sent {
				t.Errorf("EM_SETSEL sent = %v, want %v: %v", sent, tc.sent, spy.recorded())
			}
			if ripple := containsStep(spy.recorded(), "cursor-click"); ripple != tc.ripple {
				t.Errorf("ripple = %v, want %v", ripple, tc.ripple)
			}
			if containsStep(spy.recorded(), "text-select") {
				t.Error("a classic edit control must not take the Text pattern route")
			}
		})
	}
}

func TestSelectTextPatternRoute(t *testing.T) {
	// No window of its own (an Element drawn inside its Window), and a
	// window that is no edit control (a WPF or Chromium host): the Text
	// pattern.
	for name, setup := range map[string]func(*headlessSpy){
		"no native window": func(s *headlessSpy) { s.text.native = 0 },
		"not an edit": func(s *headlessSpy) {
			s.text.edit = win.Control{HWND: 555, Class: "HwndWrapper[App;;1]", Enabled: true}
		},
	} {
		t.Run(name, func(t *testing.T) {
			spy := newTextSpy()
			setup(spy)
			spy.refIndex = 4
			out, cmdErr := runCommand(t, CommandsWith(spy.svc), "select-text",
				protocol.SelectTextArgs{Window: "101", Ref: "name:Notes", Start: 1, Length: 3})
			if cmdErr != nil || out != okOutput {
				t.Fatalf("select-text: %q, %v", out, cmdErr)
			}
			if spy.text.index != 4 || spy.text.span != (uia.TextSpan{Start: 1, Length: 3}) {
				t.Errorf("selected %+v of element %d, want 1+3 of 4", spy.text.span, spy.text.index)
			}
			if containsStep(spy.recorded(), "em-setsel") || containsStep(spy.recorded(), "edit-state") {
				t.Errorf("the Text pattern route sent edit messages: %v", spy.recorded())
			}
			assertNoV1(t, spy.recorded())
		})
	}

	spy := newTextSpy()
	spy.text.native = 0
	if _, cmdErr := runCommand(t, CommandsWith(spy.svc), "select-text", selectAll(2)); cmdErr != nil || !spy.text.span.All {
		t.Errorf("all: %v, span %+v", cmdErr, spy.text.span)
	}
}

func TestSelectTextPatternFailures(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantMsg  string
		wantHint string
		ripple   bool
	}{
		{"no Text pattern", uia.ErrNoTextPattern, uia.ErrNoTextPattern.Error() + "; nothing was sent",
			"select-text has no headed form; to select with real keys (this brings the Window to the foreground): winuse --headed click 101 2, then winuse press-key 101 Control_L+a", false},
		{"no selection", uia.ErrNoTextSelection, uia.ErrNoTextSelection.Error() + "; nothing was sent", "winuse --headed click 101 2", false},
		{"past the end", fmt.Errorf("%w: it would end at 9, and the text holds 4 characters", uia.ErrTextRange),
			"the selection ends past the end of the text: it would end at 9, and the text holds 4 characters; nothing was sent", "or pass all", false},
		{"stale", fmt.Errorf("%w: gone", uia.ErrStale), "element 2 is not available in the cached Window State for window 101; nothing was sent", "get-window-state 101 again", false},
		{"read-back", &uia.VerifyError{Action: "select-text", Expected: "characters 0 to 3", Observed: "another selection"},
			"select-text was delivered but the Element reads another selection afterwards, not characters 0 to 3", "do not repeat it", true},
		{"write", &uia.WriteError{What: "selecting the text", HR: -2147467259}, "", "may have taken effect", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := newTextSpy()
			spy.text.native = 0
			spy.text.err = tc.err
			_, cmdErr := runCommand(t, CommandsWith(spy.svc), "select-text", selectArgs(2, 0, 3))
			if cmdErr == nil || (tc.wantMsg != "" && cmdErr.Msg != tc.wantMsg) || !strings.Contains(cmdErr.Hint, tc.wantHint) {
				t.Fatalf("error = %+v, want %q with a hint containing %q", cmdErr, tc.wantMsg, tc.wantHint)
			}
			if ripple := containsStep(spy.recorded(), "cursor-click"); ripple != tc.ripple {
				t.Errorf("ripple = %v, want %v", ripple, tc.ripple)
			}
		})
	}
}

func TestSelectTextArgumentChecks(t *testing.T) {
	cases := []struct {
		name string
		args protocol.SelectTextArgs
		want string
		hint string
	}{
		{"all and a span", protocol.SelectTextArgs{Window: "101", ElementIndex: intPtrBatch(2), Start: 1, All: true}, "pass a start and a length, or all, not both", "usage: winuse select-text"},
		{"negative start", selectArgs(2, -1, 3), "start and length must be 0 or more", "usage: winuse select-text"},
		{"negative length", selectArgs(2, 0, -3), "start and length must be 0 or more", "usage: winuse select-text"},
		{"too long", selectArgs(2, 1, uia.MaxTextOffset), "start+length at most", "usage: winuse select-text"},
		{"no element", protocol.SelectTextArgs{Window: "101", Start: 1, Length: 1}, "are required", ""},
		{"index and reference", protocol.SelectTextArgs{Window: "101", ElementIndex: intPtrBatch(2), Ref: "id:x", Length: 1}, "not several", "usage: winuse select-text"},
		{"negative index", selectArgs(-1, 0, 1), "non-negative", "usage: winuse select-text"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := newTextSpy()
			_, cmdErr := runCommand(t, CommandsWith(spy.svc), "select-text", tc.args)
			if cmdErr == nil || !strings.Contains(cmdErr.Msg, tc.want) || !strings.Contains(cmdErr.Hint, tc.hint) {
				t.Fatalf("error = %+v, want it to contain %q", cmdErr, tc.want)
			}
			if got := spy.recorded(); len(got) != 0 {
				t.Errorf("a usage failure must touch nothing: %v", got)
			}
		})
	}
}

func TestSelectTextRefusesALockedDesktop(t *testing.T) {
	spy := newTextSpy()
	spy.svc.Locked = func() (bool, string) { spy.step("locked"); return true, "Winlogon" }
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "select-text", selectArgs(2, 0, 1))
	if cmdErr == nil || !strings.HasSuffix(cmdErr.Msg, "nothing was sent") {
		t.Fatalf("locked: %+v", cmdErr)
	}
	for _, s := range []string{"element-window", "em-setsel", "text-select", "cursor-travel"} {
		if containsStep(spy.recorded(), s) {
			t.Errorf("a locked desktop reached %q: %v", s, spy.recorded())
		}
	}
}

func TestSelectTextWithoutUIAClient(t *testing.T) {
	spy := newBatchSpy() // no TextSelector
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "select-text", selectArgs(2, 0, 1))
	if cmdErr == nil || cmdErr.Msg != "select-text is unavailable: no UIA client" {
		t.Errorf("error = %+v", cmdErr)
	}
}

// TestSelectTextRouting: select-text is Headless whenever its arguments
// parse (there is no headed form), it is a Batch step, and a Batch of it
// and a classic type-text runs in one passive scope with no banner.
func TestSelectTextRouting(t *testing.T) {
	raw := func(a protocol.SelectTextArgs) json.RawMessage { b, _ := json.Marshal(a); return b }
	if !headlessStep("select-text", raw(selectArgs(2, 0, 3)), nil) || !headlessStep("select-text", raw(selectAll(2)), nil) {
		t.Error("select-text with a span or all must be Headless")
	}
	if headlessStep("select-text", raw(selectArgs(2, -1, 3)), nil) || headlessStep("select-text", raw(protocol.SelectTextArgs{Window: "101", Length: 1}), nil) {
		t.Error("select-text arguments that do not parse are not Headless")
	}
	if takesHeaded("select-text") {
		t.Error("select-text has no headed form")
	}
	if !batchableStep("select-text") {
		t.Error("select-text must be a Batch step")
	}

	spy := newTextSpy()
	ov := &overlaySpy{share: spy.step}
	spy.svc.Overlay = ov
	steps := []protocol.Step{
		{Kind: "select-text", Args: batchStepArgs(protocol.SelectTextArgs{ElementIndex: intPtrBatch(2), Start: 6, Length: 5})},
	}
	out, cmdErr := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{Window: "101", Steps: steps})
	if cmdErr != nil || out != "Action 1 (select-text): "+okOutput {
		t.Fatalf("batch: %q, %+v", out, cmdErr)
	}
	if !reflect.DeepEqual(spy.text.sel, [][2]int{{6, 11}}) {
		t.Errorf("EM_SETSEL %v", spy.text.sel)
	}
	assertNoV1(t, spy.recorded())
	if b := spy.esc.brackets(); len(b) == 0 || b[0] != "arm-passive" {
		t.Errorf("Esc brackets = %v, want a passive Batch scope", b)
	}
}

func TestSelectTextRegistered(t *testing.T) {
	if _, ok := CommandsWith(Services{Trees: &fakeTrees{}})["select-text"]; !ok {
		t.Error("select-text is not registered")
	}
	if observationOnlyCommand("select-text") {
		t.Error("select-text acts on a Window: it must hold the action gate")
	}
}

// TestPageSelectText: a Page field is focused and selected over the
// Browser Connection and read back, with the Agent Cursor and no
// foreground; refusals send nothing.
func TestPageSelectText(t *testing.T) {
	r := newPageRig(t)
	out, e := r.run(t, "select-text", protocol.SelectTextArgs{Window: "301", ElementIndex: idx(8), Start: 2, Length: 5})
	if e != nil || out != okOutput {
		t.Fatalf("select-text: %q, %+v", out, e)
	}
	if got := r.sent(); !reflect.DeepEqual(got, []string{"focus", "selecttext 2 7"}) {
		t.Errorf("sent %v", got)
	}
	r.noForeground(t)
	if containsStep(r.recorded(), "element-window") || containsStep(r.recorded(), "em-setsel") {
		t.Errorf("a Page Element took a UIA route: %v", r.recorded())
	}
	if len(r.cursor.labels) != 1 || r.cursor.labels[0] != `select text "Email"` || len(r.cursor.clicks) != 1 {
		t.Errorf("cursor labels %q, ripples %d", r.cursor.labels, len(r.cursor.clicks))
	}
	if got := strings.Join(r.esc.brackets(), ","); got != "arm-passive,disarm-passive" {
		t.Errorf("Esc scope = %s", got)
	}

	r = newPageRig(t)
	if _, e := r.run(t, "select-text", protocol.SelectTextArgs{Window: "301", ElementIndex: idx(8), All: true}); e != nil {
		t.Fatalf("all: %+v", e)
	}
	if got := r.sent(); !reflect.DeepEqual(got, []string{"focus", "selecttext 0 all"}) {
		t.Errorf("all sent %v", got)
	}

	refusals := []struct {
		name  string
		field map[string]any
		args  protocol.SelectTextArgs
		want  string
	}{
		{"no text", map[string]any{"kind": ""}, protocol.SelectTextArgs{Window: "301", ElementIndex: idx(8), All: true},
			"element 8 holds no text to select (it is not a text field, a text area or editable text); nothing was sent"},
		{"no selection", map[string]any{"kind": "text", "length": 5, "selectable": false}, protocol.SelectTextArgs{Window: "301", ElementIndex: idx(8), Length: 1},
			"element 8 is an input whose type takes no text selection (such as email or number); nothing was sent"},
		{"past the end", map[string]any{"kind": "editable", "length": 3, "selectable": true}, protocol.SelectTextArgs{Window: "301", ElementIndex: idx(8), Start: 2, Length: 5},
			"the selection ends past the end of the text: it would end at 7, and the text holds 3 characters; nothing was sent"},
	}
	for _, c := range refusals {
		r := newPageRig(t)
		r.set(func() { r.textField = c.field })
		_, e := r.run(t, "select-text", c.args)
		if e == nil || e.Msg != c.want || e.Hint == "" {
			t.Errorf("%s: %+v", c.name, e)
		}
		if got := r.sent(); len(got) != 0 {
			t.Errorf("%s sent %v", c.name, got)
		}
	}

	r = newPageRig(t)
	r.set(func() { r.selection = map[string]any{"start": 0, "end": 0, "length": 14} })
	_, e = r.run(t, "select-text", protocol.SelectTextArgs{Window: "301", ElementIndex: idx(8), Start: 2, Length: 5})
	if e == nil || e.Msg != "the selection was delivered, but the field selects characters 0 to 0 afterwards, not 2 to 7" || !strings.Contains(e.Hint, "do not repeat it") {
		t.Errorf("mismatch: %+v", e)
	}
}

// TestPageSelectTextBatchStep: a select-text step on a Page Element counts
// as Headless for the Batch's scope.
func TestPageSelectTextBatchStep(t *testing.T) {
	r := newPageRig(t)
	args := batchStepArgs(protocol.SelectTextArgs{Window: "301", ElementIndex: idx(8), Start: 0, Length: 2})
	if !r.svc.stepHeadless(context.Background(), "select-text", args, &win.WindowInfo{ID: browserWindowID}) {
		t.Error("a select-text step on a Page Element must be Headless")
	}
}
