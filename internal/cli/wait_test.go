package cli

// wait's command line (desktop-parity ticket 03): the pause, the window
// wait and the element waits, beside the Page waits page_test.go covers.

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestParseWaitCommand(t *testing.T) {
	five := 5
	cases := []struct {
		args []string
		want protocol.WaitArgs
	}{
		{[]string{"500"}, protocol.WaitArgs{For: "ms", MS: 500}},
		{[]string{"window", "Notepad"}, protocol.WaitArgs{For: "window", Value: "Notepad"}},
		{[]string{"window", "Save", "As", "--timeout", "20"}, protocol.WaitArgs{For: "window", Value: "Save As", TimeoutMS: 20000}},
		{[]string{"window", "--gone", "Save As", "--timeout=2.5"}, protocol.WaitArgs{For: "window", Value: "Save As", Gone: true, TimeoutMS: 2500}},
		{[]string{"101", "text", "Saved"}, protocol.WaitArgs{Window: "101", For: "text", Value: "Saved"}},
		{[]string{"101", "5", "enabled"}, protocol.WaitArgs{Window: "101", For: "element", ElementIndex: &five, State: "enabled"}},
		{[]string{"101", "name:Save As", "Disabled", "--timeout", "3"}, protocol.WaitArgs{Window: "101", For: "element", Ref: "name:Save As", State: "disabled", TimeoutMS: 3000}},
		{[]string{"101", "id:status", "value", "Done", "now"}, protocol.WaitArgs{Window: "101", For: "element", Ref: "id:status", State: "value", Value: "Done now"}},
		{[]string{"101", "5", "value", ""}, protocol.WaitArgs{Window: "101", For: "element", ElementIndex: &five, State: "value"}},
		{[]string{"101", "5", "gone"}, protocol.WaitArgs{Window: "101", For: "element", ElementIndex: &five, State: "gone"}},
		{[]string{"notepad", "selector", "#a"}, protocol.WaitArgs{Window: "notepad", For: "selector", Value: "#a"}},
	}
	for _, c := range cases {
		got, err := parseWaitCommand(c.args)
		if err != nil {
			t.Errorf("%q: %v", c.args, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q = %+v, want %+v", c.args, got, c.want)
		}
	}
}

func TestParseWaitCommandErrors(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{nil, "wait takes a window reference"},
		{[]string{"0"}, "above 0 and at most 300000"},
		{[]string{"300001"}, "at most 300000"},
		{[]string{"500", "--timeout", "2"}, "a pause takes no --timeout"},
		{[]string{"window"}, "wait window needs an App name or a title"},
		{[]string{"window", "--gone"}, "wait window needs an App name or a title"},
		{[]string{"window", "x", "--fast"}, "wait window takes --gone and --timeout"},
		{[]string{"window", "x", "--timeout", "301"}, "at most 300"},
		{[]string{"101", "5"}, "wait 5 needs a state"},
		{[]string{"101", "5", "visible"}, `not "visible"`},
		{[]string{"101", "5", "enabled", "now"}, "takes nothing after it"},
		{[]string{"101", "5", "value"}, "needs the value to wait for"},
		{[]string{"101", "name:Save", "As", "enabled"}, `quote it, e.g. "name:Save As"`},
		{[]string{"101", "idle"}, `not "idle"`},
		{[]string{"101", "-3", "enabled"}, `not "-3"`},
	} {
		_, err := parseWaitCommand(c.args)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: %v, want %q", c.args, err, c.want)
		}
	}
}

func TestWaitRequestBudget(t *testing.T) {
	if got := waitTimeout(protocol.WaitArgs{For: "ms", MS: 120000}); got != 120*time.Second+protocol.WaitMargin {
		t.Errorf("a 120 s pause asks for %s", got)
	}
	if got := waitTimeout(protocol.WaitArgs{For: "window", Value: "x", TimeoutMS: 30000}); got != 30*time.Second+protocol.WaitMargin {
		t.Errorf("a 30 s window wait asks for %s", got)
	}
	if got := waitTimeout(protocol.WaitArgs{For: "element", State: "enabled"}); got != 0 {
		t.Errorf("a wait without --timeout asks for %s, want the command's own budget", got)
	}
}

func TestBatchWaitStepTakesAnElement(t *testing.T) {
	_, steps, err := parseBatchArgs([]string{"101", "click", "3", "then", "wait", "id:status", "value", "Done", "--timeout", "20"})
	if err != nil {
		t.Fatal(err)
	}
	if steps[1].Kind != "wait" || string(steps[1].Args) != `{"window":"","for":"element","value":"Done","timeout_ms":20000,"ref":"id:status","state":"value"}` {
		t.Errorf("wait step %s %s", steps[1].Kind, steps[1].Args)
	}
}
