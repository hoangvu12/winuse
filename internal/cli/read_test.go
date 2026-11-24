package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseReadArgs(t *testing.T) {
	a, err := parseReadArgs([]string{"4242:1a2b3c4d", "--root", "57", "--urls", "full", "--max-chars=900"})
	if err != nil || a.Window != "4242:1a2b3c4d" || a.Root != "57" || a.URLs != "full" || a.MaxChars == nil || *a.MaxChars != 900 {
		t.Errorf("got %+v, %v", a, err)
	}
	a, err = parseReadArgs([]string{"--out", "page.md", "4242"})
	if want, _ := filepath.Abs("page.md"); err != nil || a.Out != want || a.Window != "4242" {
		t.Errorf("--out: %+v, %v", a, err)
	}
	for _, bad := range [][]string{
		nil,
		{"4242", "5"},
		{"4242", "--urls", "short"},
		{"4242", "--max-chars", "-1"},
		{"4242", "--out", "a.md", "--max-chars", "10"},
		{"4242", "--interactive"},
		{"4242", "--root"},
	} {
		if _, err := parseReadArgs(bad); err == nil {
			t.Errorf("%s was accepted", strings.Join(bad, " "))
		}
	}
}
