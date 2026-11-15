package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/jev"
)

func TestParsePickArgs(t *testing.T) {
	a, err := parsePickArgs([]string{"4242", "the", "Save", "button", "--click"})
	if err != nil || a.window != "4242" || a.desc != "the Save button" || !a.click || a.setValue != nil {
		t.Errorf("click: %+v %v", a, err)
	}
	a, err = parsePickArgs([]string{"--set-value", "--literal text", "4242:1a2b", "search box", "--model=openjev-0.1"})
	if err != nil || a.window != "4242:1a2b" || a.desc != "search box" || a.setValue == nil || *a.setValue != "--literal text" || a.model != "openjev-0.1" {
		t.Errorf("set-value: %+v %v", a, err)
	}
	for _, bad := range [][]string{
		{"4242"},
		{"4242", "x", "--click", "--set-value", "v"},
		{"4242", "x", "--set-value"},
		{"4242", "x", "--first"},
		{"4242", "  "},
	} {
		if _, err := parsePickArgs(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestPickWithoutAKeyReadsAndSendsNothing(t *testing.T) {
	var out bytes.Buffer
	err := runPick(pickArgs{window: "4242", desc: "x"}, jev.Config{}, false, &out)
	if err != noKeyError || out.Len() != 0 || ExitCode(err) != 1 {
		t.Errorf("err %v, out %q", err, out.String())
	}
}

func TestPrintCandidatesShowsAtMostThreeLikelyOnes(t *testing.T) {
	var out bytes.Buffer
	printCandidates(&out, []jev.Candidate{
		{Element: jev.Element{Index: 3, Line: "button A"}, P: 0.4},
		{Element: jev.Element{Index: 4, Line: "button B"}, P: 0.3},
		{Element: jev.Element{Index: 5, Line: "button C"}, P: 0.2},
		{Element: jev.Element{Index: 6, Line: "button D"}, P: 0.1},
	})
	want := "candidates:\n  3 button A  p=0.40\n  4 button B  p=0.30\n  5 button C  p=0.20\n"
	if out.String() != want {
		t.Errorf("got %q", out.String())
	}
	out.Reset()
	printCandidates(&out, []jev.Candidate{{Element: jev.Element{Index: 3, Line: "a"}, P: 0.01}})
	if strings.TrimSpace(out.String()) != "" {
		t.Errorf("unlikely ones printed: %q", out.String())
	}
}
