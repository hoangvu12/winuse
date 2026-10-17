package protocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
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

func TestTimeoutFor(t *testing.T) {
	// The decided budgets: 10 s per request, 15 s for launch-app, 60 s for
	// a batch of Actions (winuse's own bound; Codex has no batch).
	if got := TimeoutFor("list-windows"); got != RequestTimeout {
		t.Errorf("TimeoutFor(list-windows) = %s, want the default %s", got, RequestTimeout)
	}
	if got := TimeoutFor("launch-app"); got != LaunchTimeout {
		t.Errorf("TimeoutFor(launch-app) = %s, want %s", got, LaunchTimeout)
	}
	if got := TimeoutFor("batch"); got != BatchTimeout {
		t.Errorf("TimeoutFor(batch) = %s, want %s", got, BatchTimeout)
	}
	if got := TimeoutFor("unknown-command"); got != RequestTimeout {
		t.Errorf("TimeoutFor(unknown) = %s, want the default %s", got, RequestTimeout)
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
}

func TestBatchStepKindsAreBatchableActions(t *testing.T) {
	// The list is the contract both ends of the wire share (the CLI's
	// step parser and the helper's step dispatch): every entry is an
	// Action command, and the set is exactly today's Actions.
	want := []string{"click", "type-text", "press-key", "set-value", "perform-secondary-action", "activate-window"}
	if len(BatchStepKinds) != len(want) {
		t.Fatalf("BatchStepKinds = %q, want %q", BatchStepKinds, want)
	}
	for i, kind := range want {
		if BatchStepKinds[i] != kind {
			t.Errorf("BatchStepKinds[%d] = %q, want %q", i, BatchStepKinds[i], kind)
		}
	}
}

func intPtr(v int) *int { return &v }
