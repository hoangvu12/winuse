package cli

// The global --headed reaches drag, hover and tab switch (headless-max
// ticket 21): without it the helper refuses their Headed routes, so the
// CLI must carry it for the agent's retry to work.

import "testing"

func TestHeadedReachesDragHoverAndTabSwitch(t *testing.T) {
	for _, headed := range []bool{false, true} {
		d, err := dragRequest([]string{"101", "@1,2", "@3,4"}, headed)
		if err != nil || d.Headed != headed || d.Window != "101" || d.ToX != 3 {
			t.Errorf("dragRequest(headed %v) = %+v, %v", headed, d, err)
		}
		h, err := hoverRequest([]string{"101", "@5,6"}, headed)
		if err != nil || h.Headed != headed || h.X != 5 {
			t.Errorf("hoverRequest(headed %v) = %+v, %v", headed, h, err)
		}
		cmd, a, err := tabRequest([]string{"switch", "101", "ABCD1234"}, headed)
		if err != nil || cmd != "tab-switch" || a.Headed != headed {
			t.Errorf("tabRequest(switch, headed %v) = %s %+v, %v", headed, cmd, a, err)
		}
	}
	// The Headless tab commands do not carry it.
	if cmd, a, err := tabRequest([]string{"new", "101"}, true); err != nil || cmd != "tab-new" || a.Headed {
		t.Errorf("tabRequest(new, headed) = %s %+v, %v", cmd, a, err)
	}
	if _, err := dragRequest(nil, true); err == nil {
		t.Error("drag with no Window must be a usage error")
	}
	if _, err := hoverRequest(nil, true); err == nil {
		t.Error("hover with no Window must be a usage error")
	}
}

func TestHeadedIsExtractedAroundDragAndHover(t *testing.T) {
	for _, line := range [][]string{
		{"--headed", "drag", "101", "@1,2", "@3,4"},
		{"drag", "101", "@1,2", "@3,4", "--headed"},
		{"hover", "--headed", "101", "@5,6"},
	} {
		rest, headed := extractHeaded(line)
		if !headed || len(rest) != len(line)-1 {
			t.Errorf("extractHeaded(%q) = %q, %v", line, rest, headed)
		}
	}
}
