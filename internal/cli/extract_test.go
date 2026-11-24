package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseExtractArgs(t *testing.T) {
	a, err := parseExtractArgs([]string{"--root", "57", "4242:1a2b3c4d", "--as=table", "--limit", "200", "--max-chars", "0"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Window != "4242:1a2b3c4d" || a.Root != "57" || a.As != "table" || a.Limit != 200 || a.MaxChars == nil || *a.MaxChars != 0 || a.Out != "" {
		t.Errorf("parsed %+v", a)
	}
	a, err = parseExtractArgs([]string{"4242", "--out", "rows.json"})
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(a.Out) || filepath.Base(a.Out) != "rows.json" {
		t.Errorf("--out %q is not made absolute", a.Out)
	}
}

func TestParseExtractArgsRefusals(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{nil, "extract takes one window reference"},
		{[]string{"4242", "5"}, "extract takes one window reference"},
		{[]string{"4242", "--depth", "2"}, "unknown flag --depth"},
		{[]string{"4242", "--as", "grid"}, "--as takes list, table or jsonld"},
		{[]string{"4242", "--limit", "0"}, "--limit needs a positive number"},
		{[]string{"4242", "--max-chars", "-1"}, "--max-chars needs a number"},
		{[]string{"4242", "--out", "x.json", "--max-chars", "100"}, "pass --out or --max-chars, not both"},
		{[]string{"4242", "--root"}, "--root needs a value"},
		{[]string{"4242", "--root", "1", "--root", "2"}, "--root is given twice"},
	}
	for _, c := range cases {
		_, err := parseExtractArgs(c.args)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: %v, want %q", c.args, err, c.want)
		}
	}
}
