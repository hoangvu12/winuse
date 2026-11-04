package cli

// Offline tests for --json (desktop-parity ticket 07): the flag's place,
// the line each outcome prints, and Main end to end on commands that
// never reach the helper.

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/version"
)

func TestExtractJSON(t *testing.T) {
	cases := []struct {
		args    []string
		rest    []string
		on      bool
		command string
	}{
		{[]string{"--json", "click", "5", "3"}, []string{"click", "5", "3"}, true, "click"},
		{[]string{"--headed", "--trace", "t.jsonl", "--json", "--trace-unredacted", "click", "5", "3"},
			[]string{"--headed", "--trace", "t.jsonl", "--trace-unredacted", "click", "5", "3"}, true, "click"},
		{[]string{"--trace=t.jsonl", "--json", "list-windows"}, []string{"--trace=t.jsonl", "list-windows"}, true, "list-windows"},
		{[]string{"click", "5", "3"}, []string{"click", "5", "3"}, false, "click"},
		// After the command, --json is the command's: type-text's text here.
		{[]string{"type-text", "5", "--json"}, []string{"type-text", "5", "--json"}, false, "type-text"},
		{[]string{"--json"}, []string{}, true, ""},
	}
	for _, tc := range cases {
		rest, on, command := extractJSON(tc.args)
		if !reflect.DeepEqual(rest, tc.rest) || on != tc.on || command != tc.command {
			t.Errorf("extractJSON(%q) = %q, %v, %q; want %q, %v, %q", tc.args, rest, on, command, tc.rest, tc.on, tc.command)
		}
	}
}

// The line each outcome prints.
func TestJSONLineForEachOutcome(t *testing.T) {
	cases := []struct {
		name   string
		code   int
		stdout string
		stderr string
		resp   *protocol.Response
		want   jsonLine
	}{
		{
			name: "success", stdout: "clicked element 3\n",
			resp: &protocol.Response{OK: true, Out: "clicked element 3\n"},
			want: jsonLine{OK: true, Command: "click", Out: "clicked element 3\n"},
		},
		{
			name: "helper failure", code: 1,
			stderr: "element 3 is not available in the cached Window State for window 5; nothing was sent\nhint: run winuse get-window-state 5 again\n",
			resp: &protocol.Response{Err: "element 3 is not available in the cached Window State for window 5; nothing was sent",
				Hint: "run winuse get-window-state 5 again", Exit: 1,
				Code: protocol.CodeStaleRef, Delivery: protocol.DeliveryNotDelivered, Retry: protocol.RetrySafe},
			want: jsonLine{Command: "click", Error: &jsonError{Code: protocol.CodeStaleRef,
				Message: "element 3 is not available in the cached Window State for window 5; nothing was sent",
				Hint:    "run winuse get-window-state 5 again", Delivery: protocol.DeliveryNotDelivered, Retry: protocol.RetrySafe}},
		},
		{
			name: "older helper without a code", code: 1,
			resp: &protocol.Response{Err: "the pattern call failed", Hint: "the Action may have taken effect; read the Window State", Exit: 1},
			want: jsonLine{Command: "click", Error: &jsonError{Code: protocol.CodeInternal, Message: "the pattern call failed",
				Hint: "the Action may have taken effect; read the Window State", Delivery: protocol.DeliveryUncertain, Retry: protocol.RetryUnsafe}},
		},
		{
			name: "Esc stop keeps the steps that ran", code: protocol.ExitEsc, stdout: "Action 1 (click): ok\n",
			resp: &protocol.Response{Out: "Action 1 (click): ok\n", Err: "batch stopped at Action 2 (click): winuse was stopped by the user with the physical Escape key",
				Hint: "the interrupted Action may have partly taken effect", Exit: protocol.ExitEsc,
				Code: protocol.CodeStopped, Delivery: protocol.DeliveryUncertain, Retry: protocol.RetryUnsafe},
			want: jsonLine{Command: "click", Out: "Action 1 (click): ok\n", Error: &jsonError{Code: protocol.CodeStopped,
				Message: "batch stopped at Action 2 (click): winuse was stopped by the user with the physical Escape key",
				Hint:    "the interrupted Action may have partly taken effect", Delivery: protocol.DeliveryUncertain, Retry: protocol.RetryUnsafe}},
		},
		{
			name: "usage error", code: 2,
			stderr: "winuse: click: the element index must be a non-negative integer\nusage: winuse click <window> <element>\n",
			want: jsonLine{Command: "click", Error: &jsonError{Code: protocol.CodeUsage,
				Message: "winuse: click: the element index must be a non-negative integer",
				Hint:    "usage: winuse click <window> <element>", Delivery: protocol.DeliveryNotDelivered, Retry: protocol.RetrySafe}},
		},
		{
			name: "request timeout", code: 1, stderr: "winuse request timed out: click\n",
			want: jsonLine{Command: "click", Error: &jsonError{Code: protocol.CodeTimeout, Message: "winuse request timed out: click",
				Delivery: protocol.DeliveryUncertain, Retry: protocol.RetryUnsafe}},
		},
		{
			name: "locked desktop", code: 1,
			stderr: "Windows desktop is locked; unlock before using winuse\nhint: stop and ask the user to unlock\n",
			want: jsonLine{Command: "click", Error: &jsonError{Code: protocol.CodeDesktopLocked,
				Message: "Windows desktop is locked; unlock before using winuse", Hint: "stop and ask the user to unlock"}},
		},
		{
			name: "a helper success then a local failure", code: 1, stderr: "winuse: writing out.png: access denied\n",
			resp: &protocol.Response{OK: true},
			want: jsonLine{Command: "click", Error: &jsonError{Code: protocol.CodeInternal, Message: "winuse: writing out.png: access denied"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := jsonLineFor("click", tc.code, tc.stdout, tc.stderr, tc.resp)
			if !reflect.DeepEqual(got, tc.want) {
				g, _ := json.Marshal(got)
				w, _ := json.Marshal(tc.want)
				t.Errorf("line\n got %s\nwant %s", g, w)
			}
		})
	}
}

func TestFinishRecordsTheResponse(t *testing.T) {
	lastResponse = nil
	resp := &protocol.Response{Err: "no open Window with ID 42", Exit: 1, Code: protocol.CodeNotFound}
	captureStderr(t, func() { finish(resp) })
	if lastResponse != resp {
		t.Errorf("finish did not record its response for --json")
	}
}

// Main end to end: one JSON line on stdout, the exit code unchanged.
func TestMainJSONPrintsOneLine(t *testing.T) {
	code, out := runMain(t, "--json", "version")
	if code != 0 {
		t.Fatalf("exit %d, output %q", code, out)
	}
	var line jsonLine
	if err := json.Unmarshal([]byte(out), &line); err != nil || strings.Count(out, "\n") != 1 {
		t.Fatalf("output %q is not one JSON line: %v", out, err)
	}
	if want := (jsonLine{OK: true, Command: "version", Out: version.Version + "\n"}); !reflect.DeepEqual(line, want) {
		t.Errorf("line %+v, want %+v", line, want)
	}

	code, out = runMain(t, "--json", "clickx")
	if code != 2 {
		t.Fatalf("exit %d, want 2; output %q", code, out)
	}
	if err := json.Unmarshal([]byte(out), &line); err != nil || strings.Count(out, "\n") != 1 {
		t.Fatalf("output %q is not one JSON line: %v", out, err)
	}
	if line.OK || line.Command != "clickx" || line.Error == nil || line.Error.Code != protocol.CodeUsage ||
		line.Error.Message != `winuse: unknown command "clickx"; did you mean click?` ||
		!strings.HasPrefix(line.Error.Hint, "usage: winuse click <window>") || line.Error.Delivery != protocol.DeliveryNotDelivered {
		t.Errorf("line %s", out)
	}
}

// Without --json the text is what it was: the same bytes as the command
// prints on its own.
func TestMainWithoutJSONPrintsText(t *testing.T) {
	_, plain := runMain(t, "clickx")
	if strings.HasPrefix(plain, "{") || !strings.HasPrefix(plain, `winuse: unknown command "clickx"`) {
		t.Errorf("plain output changed: %q", plain)
	}
	_, out := runMain(t, "version")
	if out != version.Version+"\n" {
		t.Errorf("version printed %q", out)
	}
}
