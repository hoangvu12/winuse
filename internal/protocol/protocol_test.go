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
	// The decided budgets: 10 s per request, 15 s for launch-app.
	if got := TimeoutFor("list-windows"); got != RequestTimeout {
		t.Errorf("TimeoutFor(list-windows) = %s, want the default %s", got, RequestTimeout)
	}
	if got := TimeoutFor("launch-app"); got != LaunchTimeout {
		t.Errorf("TimeoutFor(launch-app) = %s, want %s", got, LaunchTimeout)
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
