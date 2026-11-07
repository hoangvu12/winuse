package cli

// The global --no-wait flag (reliability-council 06): placed like
// --headed, before the command or anywhere after it outside literal text,
// and carried on the request.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestExtractNoWaitPositions(t *testing.T) {
	cases := []struct {
		name string
		args string
		want string
		on   bool
	}{
		{"absent", "click 101 3", "click 101 3", false},
		{"before the command", "--no-wait click 101 3", "click 101 3", true},
		{"at the end", "click 101 3 --no-wait", "click 101 3", true},
		{"among other global flags", "--headed click 101 3 --no-wait --json", "--headed click 101 3", true},
		{"set-value before the value", "set-value 101 --no-wait 3 hello", "set-value 101 3 hello", true},
		{"a batch step", "batch 101 click 3 --no-wait then press-key Return", "batch 101 click 3 then press-key Return", true},
		// Literal text is never eaten.
		{"type-text text", "type-text 101 --no-wait", "type-text 101 --no-wait", false},
		{"set-value value", "set-value 101 3 --no-wait", "set-value 101 3 --no-wait", false},
	}
	for _, tc := range cases {
		args, _, _ := extractJSON(hoistGlobalFlags(split(tc.args)))
		rest, on := extractNoWait(args)
		if got := strings.Join(rest, " "); got != tc.want || on != tc.on {
			t.Errorf("%s: %q, %v; want %q, %v", tc.name, got, on, tc.want, tc.on)
		}
	}
	// --headed comes out after it, wherever the two stand.
	rest, on := extractNoWait(split("--no-wait --headed click 101 3"))
	if rest, headed := extractHeaded(rest); strings.Join(rest, " ") != "click 101 3" || !on || !headed {
		t.Errorf("--no-wait --headed: %q, no-wait %v, headed %v", rest, on, headed)
	}
}

func TestNoWaitOnTheWire(t *testing.T) {
	raw, err := json.Marshal(protocol.Request{Cmd: "click", NoWait: true})
	if err != nil || !strings.Contains(string(raw), `"no_wait":true`) {
		t.Errorf("request = %s, %v; want no_wait", raw, err)
	}
	if raw, _ := json.Marshal(protocol.Request{Cmd: "click"}); strings.Contains(string(raw), "no_wait") {
		t.Errorf("request = %s: no_wait only when set", raw)
	}
}

func TestHelpNamesNoWait(t *testing.T) {
	if !strings.Contains(Usage, "--no-wait") || !strings.Contains(globalHelp(), "--no-wait") {
		t.Error("the global options must name --no-wait")
	}
}
