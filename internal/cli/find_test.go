package cli

// find's and get-window-state's partial-read command lines
// (desktop-parity 04).

import (
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestParseFindArgs(t *testing.T) {
	a, err := parseFindArgs([]string{"--role", "button", "101", "--name=Save As", "--text", "ready", "--state", "enabled", "--first"})
	want := protocol.FindArgs{Window: "101", Role: "button", Name: "Save As", Text: "ready", State: "enabled", First: true}
	if err != nil || a != want {
		t.Errorf("find: %+v, %v; want %+v", a, err, want)
	}
	if a, err := parseFindArgs([]string{"101", "--count"}); err != nil || !a.Count {
		t.Errorf("--count: %+v, %v", a, err)
	}
	for _, bad := range [][]string{
		{},
		{"101", "102"},
		{"101", "--role"},
		{"101", "--role", " "},
		{"101", "--first", "--count"},
		{"101", "--first=yes"},
		{"101", "--depth", "2"},
	} {
		if _, err := parseFindArgs(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
	if d, ok := protocol.MaxTimeout("find"); !ok || d != protocol.MaxStateTimeout {
		t.Errorf("find's --timeout cap = %v, %v", d, ok)
	}
}

func TestWindowStateShapeFlags(t *testing.T) {
	req, err := windowStateRequest([]string{"101", "--root", "name:Save As", "--depth=2", "--interactive"}, false)
	if err != nil || req.Window != "101" || req.Root != "name:Save As" || req.Depth != 2 || !req.Interactive || req.Skeleton {
		t.Errorf("--root: %+v, %v", req, err)
	}
	req, err = windowStateRequest([]string{"--skeleton", "101"}, false)
	if err != nil || !req.Skeleton || req.Root != "" {
		t.Errorf("--skeleton: %+v, %v", req, err)
	}
	for _, bad := range [][]string{
		{"101", "--root"},
		{"101", "--depth", "0"},
		{"101", "--depth", "x"},
		{"101", "--skeleton=1"},
		{"101", "--bogus"},
	} {
		if _, err := windowStateRequest(bad, false); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}
