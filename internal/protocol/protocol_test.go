package protocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestWriteReadLineRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	req := Request{V: Version, Ver: "0.1.0", Cmd: "list-windows"}
	if err := WriteLine(&buf, &req); err != nil {
		t.Fatalf("WriteLine: %v", err)
	}
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Fatalf("WriteLine must terminate with a newline, got %q", buf.String())
	}
	var got Request
	if err := ReadLine(bufio.NewReader(&buf), &got); err != nil {
		t.Fatalf("ReadLine: %v", err)
	}
	if got.V != req.V || got.Ver != req.Ver || got.Cmd != req.Cmd {
		t.Fatalf("round trip mismatch: got %+v want %+v", got, req)
	}
}

func TestReadLineCarriageReturn(t *testing.T) {
	var resp, got Response
	resp = Response{OK: true, Out: "hello"}
	encoded, _ := json.Marshal(resp)
	line := append(encoded, '\r', '\n')
	if err := ReadLine(bufio.NewReader(bytes.NewReader(line)), &got); err != nil {
		t.Fatalf("ReadLine: %v", err)
	}
	if got != resp {
		t.Fatalf("round trip mismatch: got %+v want %+v", got, resp)
	}
}

func TestReadLineRejectsOversized(t *testing.T) {
	huge := bytes.Repeat([]byte("a"), MaxLine+10)
	huge = append(huge, '\n')
	var req Request
	err := ReadLine(bufio.NewReader(bytes.NewReader(huge)), &req)
	if !errors.Is(err, ErrLineTooLarge) {
		t.Fatalf("want ErrLineTooLarge, got %v", err)
	}
}

func TestReadLineRejectsTruncated(t *testing.T) {
	// No newline: EOF before a complete line.
	var req Request
	err := ReadLine(bufio.NewReader(strings.NewReader(`{"v":1`)), &req)
	if err == nil {
		t.Fatal("want an error for a line without a trailing newline")
	}
}

func TestResponseJSONShape(t *testing.T) {
	// The field names are the CLI-helper contract; pin them.
	b, err := json.Marshal(Response{OK: true, Out: "out", Err: "err", Hint: "hint", Exit: 3, Stale: true})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"ok": true, "out": "out", "err": "err", "hint": "hint", "exit": float64(3), "stale": true}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("field %q: got %v want %v", k, m[k], v)
		}
	}
}

func TestRequestJSONShape(t *testing.T) {
	b, err := json.Marshal(Request{V: 7, Ver: "9.9.9", Cmd: "click", Args: json.RawMessage(`{"x":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"v":7,"ver":"9.9.9","cmd":"click","args":{"x":1}}`
	if string(b) != want {
		t.Fatalf("request JSON:\n got %s\nwant %s", b, want)
	}
	// Args must stay absent when unset.
	b, err = json.Marshal(Request{V: 1, Ver: "0", Cmd: "list-windows"})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("args")) {
		t.Fatalf("empty args must be omitted: %s", b)
	}
}

func TestErrorfFormatting(t *testing.T) {
	resp := Errorf(1, "boom %d", 42)
	if resp.OK || resp.Err != "boom 42" || resp.Exit != 1 {
		t.Fatalf("bad error response: %+v", resp)
	}
}

func TestMaxLineAllowsWindowStateSizedLines(t *testing.T) {
	// A Window State tree can reach a few MiB; 4 MiB must be accepted.
	big := &Response{OK: true, Out: strings.Repeat("x", 3<<20)}
	var buf bytes.Buffer
	if err := WriteLine(&buf, big); err != nil {
		t.Fatalf("WriteLine: %v", err)
	}
	var got Response
	if err := ReadLine(bufio.NewReader(&buf), &got); err != nil {
		t.Fatalf("ReadLine: %v", err)
	}
	if len(got.Out) != len(big.Out) {
		t.Fatalf("truncated: got %d bytes want %d", len(got.Out), len(big.Out))
	}
}

func TestConstants(t *testing.T) {
	if Version != 1 {
		t.Errorf("protocol version drifted: %d", Version)
	}
	if ExitEsc != 130 {
		t.Errorf("Esc exit code must stay 130 (Codex interrupt), got %d", ExitEsc)
	}
	if RequestTimeout < 9*time.Second || RequestTimeout > 11*time.Second {
		t.Errorf("request timeout should be ~10s (Codex), got %s", RequestTimeout)
	}
}

func TestCommandTimeout(t *testing.T) {
	// The decided budgets: 10 s per request (the default), 15 s for
	// launch-app, 60 s for a batch of Actions (winuse's own bound; Codex
	// has no batch). CommandTimeout is the one helper: it reports only the
	// explicit entries, and callers compose it with RequestTimeout.
	if _, ok := CommandTimeout("list-windows"); ok {
		t.Error("CommandTimeout(list-windows) must not have an explicit entry")
	}
	if d, ok := CommandTimeout("launch-app"); !ok || d != LaunchTimeout {
		t.Errorf("CommandTimeout(launch-app) = %s, %v; want %s", d, ok, LaunchTimeout)
	}
	if d, ok := CommandTimeout("batch"); !ok || d != BatchTimeout {
		t.Errorf("CommandTimeout(batch) = %s, %v; want %s", d, ok, BatchTimeout)
	}
	if LaunchTimeout != 15*time.Second {
		t.Errorf("launch-app budget must be 15s (Codex), got %s", LaunchTimeout)
	}
}

func TestLaunchArgsJSONShape(t *testing.T) {
	b, err := json.Marshal(LaunchArgs{App: "Notepad"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"app":"Notepad"}`; string(b) != want {
		t.Fatalf("launch args JSON:\n got %s\nwant %s", b, want)
	}
	var got LaunchArgs
	if err := json.Unmarshal([]byte(`{"app":"x"}`), &got); err != nil || got.App != "x" {
		t.Fatalf("unmarshal: %v %+v", err, got)
	}
}

func TestClickArgsJSONShape(t *testing.T) {
	// The wire names follow Codex's click parameters exactly (guiding
	// rule): window, element_index, click_count, mouse_button.
	b, err := json.Marshal(ClickArgs{Window: "101", ElementIndex: intPtr(3), ClickCount: 2, MouseButton: "right"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"window":"101","element_index":3,"click_count":2,"mouse_button":"right"}`; string(b) != want {
		t.Fatalf("click args JSON:\n got %s\nwant %s", b, want)
	}
	// Defaults (count 0, empty button) are omitted; a missing or null
	// element_index stays nil so required-ness is checkable.
	b, err = json.Marshal(ClickArgs{Window: "101"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"window":"101","element_index":null}`; string(b) != want {
		t.Fatalf("minimal click args JSON:\n got %s\nwant %s", b, want)
	}
	var got ClickArgs
	if err := json.Unmarshal([]byte(`{"window":"101"}`), &got); err != nil || got.ElementIndex != nil {
		t.Fatalf("unmarshal: %v %+v", err, got)
	}
	if err := json.Unmarshal([]byte(`{"window":"101","element_index":7}`), &got); err != nil || got.ElementIndex == nil || *got.ElementIndex != 7 {
		t.Fatalf("unmarshal with index: %v %+v", err, got)
	}
	// The name:/id: Element Reference form (ticket 11) is a string field
	// omitted when empty, so the three forms stay distinguishable.
	b, err = json.Marshal(ClickArgs{Window: "101", Ref: "name:OK"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"window":"101","element_index":null,"ref":"name:OK"}`; string(b) != want {
		t.Fatalf("click args with a reference:\n got %s\nwant %s", b, want)
	}
	if err := json.Unmarshal([]byte(`{"window":"101","ref":"id:box"}`), &got); err != nil || got.Ref != "id:box" {
		t.Fatalf("unmarshal with a reference: %v %+v", err, got)
	}
}

func TestActivateArgsJSONShape(t *testing.T) {
	b, err := json.Marshal(ActivateArgs{Window: "notepad"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"window":"notepad"}`; string(b) != want {
		t.Fatalf("activate args JSON:\n got %s\nwant %s", b, want)
	}
}

func TestSetValueArgsReferenceShape(t *testing.T) {
	// set-value and perform-secondary-action carry the same Element
	// Reference forms as click: element_index, or the ticket-11 ref.
	b, err := json.Marshal(SetValueArgs{Window: "101", Ref: "id:field", Value: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"window":"101","element_index":null,"ref":"id:field","value":"hello"}`; string(b) != want {
		t.Fatalf("set-value args JSON:\n got %s\nwant %s", b, want)
	}
	b, err = json.Marshal(SecondaryActionArgs{Window: "101", Ref: "name:Details", Action: "Expand"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"window":"101","element_index":null,"ref":"name:Details","action":"Expand"}`; string(b) != want {
		t.Fatalf("secondary-action args JSON:\n got %s\nwant %s", b, want)
	}
}

func TestBatchArgsJSONShape(t *testing.T) {
	// A Batch is a Window plus steps; each step is an Action's name and
	// its arguments (the Window omitted — the helper injects it).
	b, err := json.Marshal(BatchArgs{
		Window: "101",
		Steps: []Step{
			{Kind: "click", Args: json.RawMessage(`{"ref":"name:OK","click_count":2}`)},
			{Kind: "activate-window"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"window":"101","steps":[{"kind":"click","args":{"ref":"name:OK","click_count":2}},{"kind":"activate-window"}]}`
	if string(b) != want {
		t.Fatalf("batch args JSON:\n got %s\nwant %s", b, want)
	}
	var got BatchArgs
	if err := json.Unmarshal(b, &got); err != nil || got.Window != "101" || len(got.Steps) != 2 {
		t.Fatalf("unmarshal: %v %+v", err, got)
	}
	if got.Steps[0].Kind != "click" || string(got.Steps[0].Args) != `{"ref":"name:OK","click_count":2}` {
		t.Errorf("step 0 = %+v, args %s", got.Steps[0], got.Steps[0].Args)
	}
	if got.Steps[1].Kind != "activate-window" || got.Steps[1].Args != nil {
		t.Errorf("step 1 = %+v, want no arguments", got.Steps[1])
	}
	// --stop-on-error travels as stop_on_error (desktop-parity ticket 20).
	b, err = json.Marshal(BatchArgs{Window: "101", StopOnError: true})
	if err != nil || string(b) != `{"window":"101","steps":null,"stop_on_error":true}` {
		t.Errorf("stop_on_error JSON = %s, %v", b, err)
	}
}

func TestBatchStepKindsAreBatchableActions(t *testing.T) {
	// The list is the contract both ends of the wire share (the CLI's
	// step parser and the helper's step dispatch): every entry is an
	// Action command, and the set is exactly today's Actions (the ticket-10
	// pointer Actions became steps in ticket 12; browser merge ticket 07
	// adds the acting tab commands, desktop-parity ticket 20 tab-list).
	want := []string{"click", "type-text", "press-key", "set-value", "perform-secondary-action", "activate-window", "scroll", "drag", "hover",
		"tab-new", "tab-close", "tab-switch", "tab-list",
		"navigate", "back", "forward", "reload", "upload", "dialog", "wait", "eval",
		"select-text", "invoke-menu"}
	if len(BatchStepKinds) != len(want) {
		t.Fatalf("BatchStepKinds = %q, want %q", BatchStepKinds, want)
	}
	for i, kind := range want {
		if BatchStepKinds[i] != kind {
			t.Errorf("BatchStepKinds[%d] = %q, want %q", i, BatchStepKinds[i], kind)
		}
	}
}

func TestScrollArgsJSONShape(t *testing.T) {
	// The wire names follow Codex's scroll parameters exactly (guiding
	// rule): window, x, y, scrollX, scrollY.
	b, err := json.Marshal(ScrollArgs{Window: "101", X: 400, Y: 235, ScrollX: -240, ScrollY: 600})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"window":"101","x":400,"y":235,"scrollX":-240,"scrollY":600}`; string(b) != want {
		t.Fatalf("scroll args JSON:\n got %s\nwant %s", b, want)
	}
	var got ScrollArgs
	if err := json.Unmarshal([]byte(`{"window":"101","x":400,"y":235,"scrollX":-240,"scrollY":600}`), &got); err != nil || !reflect.DeepEqual(got, ScrollArgs{Window: "101", X: 400, Y: 235, ScrollX: -240, ScrollY: 600}) {
		t.Fatalf("unmarshal: %v %+v", err, got)
	}
}

func TestDragArgsJSONShape(t *testing.T) {
	// Codex's drag parameters: from_x, from_y, to_x, to_y.
	b, err := json.Marshal(DragArgs{Window: "101", FromX: 100, FromY: 50, ToX: 400, ToY: 235})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"window":"101","from_x":100,"from_y":50,"to_x":400,"to_y":235}`; string(b) != want {
		t.Fatalf("drag args JSON:\n got %s\nwant %s", b, want)
	}
	var got DragArgs
	if err := json.Unmarshal([]byte(`{"window":"101","from_x":100,"from_y":50,"to_x":400,"to_y":235}`), &got); err != nil || got.FromX != 100 || got.ToY != 235 {
		t.Fatalf("unmarshal: %v %+v", err, got)
	}
}

func TestHoverArgsJSONShape(t *testing.T) {
	// Hover is a winuse extension; its parameters follow Codex's
	// coordinate names (x, y).
	b, err := json.Marshal(HoverArgs{Window: "101", X: 200, Y: 42})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"window":"101","x":200,"y":42}`; string(b) != want {
		t.Fatalf("hover args JSON:\n got %s\nwant %s", b, want)
	}
	var got HoverArgs
	if err := json.Unmarshal([]byte(`{"window":"101","x":200,"y":42}`), &got); err != nil || got.X != 200 || got.Y != 42 {
		t.Fatalf("unmarshal: %v %+v", err, got)
	}
}

func intPtr(v int) *int { return &v }

// TestCallerTimeout covers the caller-set timeouts (desktop-parity ticket
// 14): get-window-state, launch-app, batch and wait take one, clamped to
// their caps; every other command, Actions included, ignores it.
func TestCallerTimeout(t *testing.T) {
	cases := []struct {
		cmd    string
		ms     int
		want   time.Duration
		wantOK bool
	}{
		{"get-window-state", 30000, 30 * time.Second, true},
		{"get-window-state", 600000, MaxStateTimeout, true},
		{"launch-app", 90000, 90 * time.Second, true},
		{"launch-app", 999000, MaxLaunchTimeout, true},
		{"batch", 999000, MaxBatchTimeout, true},
		{"wait", 999000, MaxWaitTimeout + WaitMargin, true},
		{"get-window-state", 0, 0, false},
		{"get-window-state", -5, 0, false},
		{"click", 30000, 0, false},
		{"type-text", 30000, 0, false},
		{"list-windows", 30000, 0, false},
	}
	for _, c := range cases {
		got, ok := CallerTimeout(&Request{Cmd: c.cmd, TimeoutMS: c.ms})
		if got != c.want || ok != c.wantOK {
			t.Errorf("CallerTimeout(%s, %d ms) = %s, %v; want %s, %v", c.cmd, c.ms, got, ok, c.want, c.wantOK)
		}
	}
	if MaxStateTimeout != 60*time.Second || MaxLaunchTimeout != 120*time.Second || MaxBatchTimeout != 300*time.Second || MaxWaitTimeout != 300*time.Second {
		t.Errorf("caps drifted: %s %s %s %s", MaxStateTimeout, MaxLaunchTimeout, MaxBatchTimeout, MaxWaitTimeout)
	}
}

// TestBudget: the caller-set timeout only widens the command's budget,
// and an Action's budget never follows it.
func TestBudget(t *testing.T) {
	cases := []struct {
		req  Request
		want time.Duration
	}{
		{Request{Cmd: "get-window-state"}, RequestTimeout},
		{Request{Cmd: "get-window-state", TimeoutMS: 45000}, 45 * time.Second},
		{Request{Cmd: "get-window-state", TimeoutMS: 3000}, RequestTimeout},
		{Request{Cmd: "get-window-state", TimeoutMS: 120000}, MaxStateTimeout},
		{Request{Cmd: "launch-app"}, LaunchTimeout},
		{Request{Cmd: "launch-app", TimeoutMS: 5000}, LaunchTimeout},
		{Request{Cmd: "launch-app", TimeoutMS: 60000}, 60 * time.Second},
		{Request{Cmd: "batch", TimeoutMS: 200000}, 200 * time.Second},
		{Request{Cmd: "wait", TimeoutMS: 11000}, BrowserTimeout},
		{Request{Cmd: "wait", TimeoutMS: 121000}, 121 * time.Second},
		{Request{Cmd: "click", TimeoutMS: 60000}, RequestTimeout},
		{Request{Cmd: "navigate", TimeoutMS: 120000}, BrowserTimeout},
	}
	for _, c := range cases {
		if got := Budget(&c.req, RequestTimeout); got != c.want {
			t.Errorf("Budget(%s, %d ms) = %s, want %s", c.req.Cmd, c.req.TimeoutMS, got, c.want)
		}
	}
	// The default passed in is the server's own (tests shrink it).
	if got := Budget(&Request{Cmd: "get-window-state"}, time.Second); got != time.Second {
		t.Errorf("Budget with a 1 s default = %s", got)
	}
}

func TestRequestTimeoutJSON(t *testing.T) {
	b, err := json.Marshal(Request{V: 1, Cmd: "batch", TimeoutMS: 120000})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"v":1,"ver":"","cmd":"batch","timeout_ms":120000}`; string(b) != want {
		t.Fatalf("request JSON:\n got %s\nwant %s", b, want)
	}
	b, _ = json.Marshal(Request{V: 1, Cmd: "click"})
	if strings.Contains(string(b), "timeout") {
		t.Fatalf("a request without --timeout must not carry one: %s", b)
	}
}
