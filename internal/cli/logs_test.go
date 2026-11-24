package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLogsArgs(t *testing.T) {
	a, err := parseLogsArgs([]string{"4242:1a2b3c4d", "--clear", "--max-chars=900"})
	if err != nil || a.Window != "4242:1a2b3c4d" || !a.Clear || a.MaxChars == nil || *a.MaxChars != 900 || a.JSON {
		t.Errorf("got %+v, %v", a, err)
	}
	a, err = parseLogsArgs([]string{"--out", "logs.txt", "4242"})
	if want, _ := filepath.Abs("logs.txt"); err != nil || a.Out != want || a.Window != "4242" || a.Clear {
		t.Errorf("--out: %+v, %v", a, err)
	}
	for _, bad := range [][]string{
		nil,
		{"4242", "5"},
		{"4242", "--clear=yes"},
		{"4242", "--max-chars", "-1"},
		{"4242", "--out", "a.txt", "--max-chars", "10"},
		{"4242", "--capture"},
		{"4242", "--out"},
	} {
		if _, err := parseLogsArgs(bad); err == nil {
			t.Errorf("%s was accepted", strings.Join(bad, " "))
		}
	}
}
