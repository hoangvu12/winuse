package e2eledger

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const sample = "# Ledger\n\nSome prose.\n\n" +
	"| Test | Claim | Runs on | Why not on the runner | Last run | Result |\n" +
	"|---|---|---|---|---|---|\n" +
	"| `TestClick` | a click lands | runner | - | - | not yet run on runner |\n" +
	"| `TestClickTwice` | two clicks land | runner | | - | not yet run on runner |\n" +
	"| `TestNotepad` | Notepad types | user | drives Notepad | - | - |\n" +
	"\nMore prose.\n"

func TestParseReadsTheTable(t *testing.T) {
	rows, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %+v, want 3", rows)
	}
	want := Row{Test: "TestNotepad", Claim: "Notepad types", RunsOn: UserPC, Why: "drives Notepad", LastRun: "-", Result: "-", Line: 9}
	if rows[2] != want {
		t.Errorf("row 3 = %+v, want %+v", rows[2], want)
	}
	if got := RunnerTests(rows); strings.Join(got, ",") != "TestClick,TestClickTwice" {
		t.Errorf("RunnerTests = %v", got)
	}
}

func TestRunPatternMatchesWholeNamesOnly(t *testing.T) {
	rows, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	p := RunPattern(rows)
	if p != "^(TestClick|TestClickTwice)$" {
		t.Fatalf("RunPattern = %q", p)
	}
	re := regexp.MustCompile(p)
	for name, want := range map[string]bool{"TestClick": true, "TestClickTwice": true, "TestClickThrice": false, "TestNotepad": false, "XTestClick": false} {
		if re.MatchString(name) != want {
			t.Errorf("pattern matches %s = %v, want %v", name, !want, want)
		}
	}
	if RunPattern([]Row{{Test: "TestA", RunsOn: UserPC}}) != "" {
		t.Error("a ledger with no runner rows must give no pattern")
	}
}

func TestParseRefusesABadLedger(t *testing.T) {
	head := "| Test | Claim | Runs on | Why not on the runner | Last run | Result |\n|---|---|---|---|---|---|\n"
	for name, tc := range map[string]struct{ text, want string }{
		"no table":        {"# nothing\n", "no ledger table"},
		"other columns":   {"| Test | Claim |\n|---|---|\n| `TestA` | x |\n", "no ledger table"},
		"no rows":         {head, "no rows"},
		"a pipe in claim": {head + "| `TestA` | a | b | runner | - | - | - |\n", "cells"},
		"bad name":        {head + "| `Click` | a | runner | - | - | - |\n", "not a test function"},
		"duplicate":       {head + "| `TestA` | a | runner | - | - | - |\n| `TestA` | b | runner | - | - | - |\n", "already on line 3"},
		"no claim":        {head + "| `TestA` | - | runner | - | - | - |\n", "no claim"},
		"bad runs on":     {head + "| `TestA` | a | ci | - | - | - |\n", `runs on "ci"`},
		"user, no why":    {head + "| `TestA` | a | user | - | - | - |\n", "must say why"},
		"runner with why": {head + "| `TestA` | a | runner | needs Edge | - | - |\n", "gives a reason"},
		"two tables":      {head + "| `TestA` | a | runner | - | - | - |\n\n" + head + "| `TestB` | a | runner | - | - | - |\n", "second ledger table"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tc.text))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

// TestLedgerCoversEveryE2ETest keeps test/e2e/ledger.md and the e2e
// package in step: every top-level test in test/e2e has exactly one row,
// and every row names a test that exists. A new e2e test without a row
// fails here, in the default offline `go test ./...`, before any lane runs.
func TestLedgerCoversEveryE2ETest(t *testing.T) {
	dir := filepath.Join("..", "..", "test", "e2e")
	f, err := os.Open(filepath.Join(dir, "ledger.md"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := Parse(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	inLedger := map[string]bool{}
	for _, r := range rows {
		inLedger[r.Test] = true
	}

	files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	decl := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]*)\(t \*testing\.T\)`)
	inCode := map[string]string{}
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range decl.FindAllStringSubmatch(string(src), -1) {
			inCode[m[1]] = filepath.Base(file)
		}
	}
	if len(inCode) == 0 {
		t.Fatal("found no tests in test/e2e")
	}

	var missing, stale []string
	for name, file := range inCode {
		if !inLedger[name] {
			missing = append(missing, name+" ("+file+")")
		}
	}
	for name := range inLedger {
		if _, ok := inCode[name]; !ok {
			stale = append(stale, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	for _, m := range missing {
		t.Errorf("test/e2e/ledger.md has no row for %s: add one (runner, or user with the reason)", m)
	}
	for _, s := range stale {
		t.Errorf("test/e2e/ledger.md has a row for %s, which is not a test in test/e2e", s)
	}
}
