package cli

// find's and get-window-state's partial-read command lines
// (desktop-parity 04).

import (
	"path/filepath"
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
	if a, err := parseFindArgs([]string{"101", "--limit", "20", "--max-chars=4000"}); err != nil || a.Limit != 20 || a.MaxChars == nil || *a.MaxChars != 4000 {
		t.Errorf("--limit, --max-chars: %+v, %v", a, err)
	}
	if a, err := parseFindArgs([]string{"101", "--full"}); err != nil || !a.Full || a.MaxChars != nil {
		t.Errorf("--full: %+v, %v", a, err)
	}
	for _, bad := range [][]string{
		{},
		{"101", "102"},
		{"101", "--role"},
		{"101", "--role", " "},
		{"101", "--first", "--count"},
		{"101", "--first=yes"},
		{"101", "--depth", "2"},
		{"101", "--limit", "0"},
		{"101", "--limit", "3", "--first"},
		{"101", "--limit", "3", "--count"},
		{"101", "--max-chars", "x"},
		{"101", "--full", "--max-chars", "10"},
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
	// The output budgets of a full read (speed 05).
	req, err = windowStateRequest([]string{"101", "--interactive", "--depth", "4", "--compact", "--max-chars", "8000", "--screenshot"}, false)
	if err != nil || !req.Interactive || req.Depth != 4 || !req.Compact || req.MaxChars == nil || *req.MaxChars != 8000 || !req.Screenshot || req.Root != "" {
		t.Errorf("budgets: %+v, %v", req, err)
	}
	req, err = windowStateRequest([]string{"101", "--max-chars", "0"}, false)
	if err != nil || req.MaxChars == nil || *req.MaxChars != 0 {
		t.Errorf("--max-chars 0 lifts the cap: %+v, %v", req, err)
	}
	if req, err = windowStateRequest([]string{"101"}, false); err != nil || req.MaxChars != nil || req.Full {
		t.Errorf("no flag leaves the helper's default: %+v, %v", req, err)
	}
	req, err = windowStateRequest([]string{"101", "--full", "--root", "4", "--depth", "2", "--screenshot"}, false)
	if err != nil || !req.Full || req.Root != "4" || req.Depth != 2 {
		t.Errorf("--full: %+v, %v", req, err)
	}
	req, err = windowStateRequest([]string{"101", "--out", "state.txt", "--max-chars", "0"}, false)
	if err != nil || req.Out == "" {
		t.Errorf("--out with --max-chars 0: %+v, %v", req, err)
	}
	req, err = windowStateRequest([]string{"101", "--out", "state.txt"}, false)
	if err != nil || !filepath.IsAbs(req.Out) || filepath.Base(req.Out) != "state.txt" {
		t.Errorf("--out resolves to an absolute path: %+v, %v", req, err)
	}
	for _, bad := range [][]string{
		{"101", "--out"},
		{"101", "--out", " "},
		{"101", "--out", "a.txt", "--max-chars", "100"},
		{"101", "--max-chars", "-1"},
		{"101", "--full", "--interactive"},
		{"101", "--full", "--max-chars", "0"},
		{"101", "--full=1"},
		{"101", "--compact=1"},
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
