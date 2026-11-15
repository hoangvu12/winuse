// Package e2eledger reads the ledger of live claims (test/e2e/ledger.md)
// and checks an e2e lane's `go test -json` output against it: every test
// the ledger declares for the hosted runner must have run and passed
// there, and no test may fail. Pure logic, offline: cmd/e2eledger wires it
// to files for the e2e workflow (.github/workflows/e2e.yml).
package e2eledger

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Where a ledger row's test runs.
const (
	// Runner: the e2e lane runs it on GitHub's hosted windows-latest
	// runner, against winuse's own fixture app only.
	Runner = "runner"
	// UserPC: it needs something the runner lacks or must not be driven
	// there (another App, a browser, the shell, toasts, a person), so only
	// the user starts it, on their own PC.
	UserPC = "user"
)

// Row is one ledger row: one e2e test and the live claim it backs.
type Row struct {
	Test    string // the top-level test function, e.g. TestClickByNameReference
	Claim   string // the claim, in plain words
	RunsOn  string // Runner or UserPC
	Why     string // why it does not run on the runner (UserPC rows only)
	LastRun string // when the result below was seen, or "-"
	Result  string // the last result, or "not yet run on runner" / "-"
	Line    int    // the row's line in the ledger, for messages
}

// header is the ledger table's header, in order. Parse refuses a table
// with other columns, so the file and this package cannot drift apart.
var header = []string{"Test", "Claim", "Runs on", "Why not on the runner", "Last run", "Result"}

var testName = regexp.MustCompile(`^Test[A-Za-z0-9_]*$`)

// Parse reads the ledger's one table (the first Markdown table whose
// header is the ledger's) and returns its rows in file order. Prose
// around the table is ignored. A row that does not parse fails the whole
// ledger: a check against a half-read ledger would pass by omission.
func Parse(r io.Reader) ([]Row, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var rows []Row
	seen := map[string]int{}
	inTable, sawHeader := false, false
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\ufeff"))
		if !strings.HasPrefix(text, "|") {
			inTable = false // the table ended; a second one is refused below
			continue
		}
		cells := splitRow(text)
		if !inTable {
			if !sameCells(cells, header) {
				continue // some other table
			}
			if sawHeader {
				return nil, fmt.Errorf("ledger line %d: a second ledger table", line)
			}
			inTable, sawHeader = true, true
			continue
		}
		if isSeparator(cells) {
			continue
		}
		if len(cells) != len(header) {
			return nil, fmt.Errorf("ledger line %d: %d cells, want %d (%s); a claim must not contain '|'", line, len(cells), len(header), strings.Join(header, ", "))
		}
		row := Row{
			Test:    strings.Trim(cells[0], "`"),
			Claim:   cells[1],
			RunsOn:  cells[2],
			Why:     cells[3],
			LastRun: cells[4],
			Result:  cells[5],
			Line:    line,
		}
		if !testName.MatchString(row.Test) {
			return nil, fmt.Errorf("ledger line %d: %q is not a test function name", line, row.Test)
		}
		if prev, ok := seen[row.Test]; ok {
			return nil, fmt.Errorf("ledger line %d: %s is already on line %d", line, row.Test, prev)
		}
		seen[row.Test] = line
		if row.Claim == "" || row.Claim == "-" {
			return nil, fmt.Errorf("ledger line %d: %s has no claim", line, row.Test)
		}
		switch row.RunsOn {
		case Runner:
			if row.Why != "" && row.Why != "-" {
				return nil, fmt.Errorf("ledger line %d: %s runs on the runner but gives a reason it does not (%q)", line, row.Test, row.Why)
			}
		case UserPC:
			if row.Why == "" || row.Why == "-" {
				return nil, fmt.Errorf("ledger line %d: %s runs on the user's PC only and must say why", line, row.Test)
			}
		default:
			return nil, fmt.Errorf("ledger line %d: %s runs on %q, want %q or %q", line, row.Test, row.RunsOn, Runner, UserPC)
		}
		rows = append(rows, row)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if !sawHeader {
		return nil, fmt.Errorf("no ledger table: want a Markdown table with the columns %s", strings.Join(header, " | "))
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("the ledger table has no rows")
	}
	return rows, nil
}

// RunnerTests returns the tests the ledger declares for the runner, in
// ledger order.
func RunnerTests(rows []Row) []string {
	var names []string
	for _, r := range rows {
		if r.RunsOn == Runner {
			names = append(names, r.Test)
		}
	}
	return names
}

// RunPattern is the `go test -run` pattern that selects exactly the
// runner tests: whole top-level names, so TestClick does not also pick
// TestClickButton. Subtests of a selected test all run.
func RunPattern(rows []Row) string {
	names := RunnerTests(rows)
	if len(names) == 0 {
		return ""
	}
	return "^(" + strings.Join(names, "|") + ")$"
}

// splitRow splits "| a | b |" into its trimmed cells.
func splitRow(text string) []string {
	text = strings.TrimPrefix(text, "|")
	text = strings.TrimSuffix(text, "|")
	parts := strings.Split(text, "|")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return parts
}

func sameCells(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// isSeparator reports a header separator row such as |---|:--|.
func isSeparator(cells []string) bool {
	for _, c := range cells {
		if strings.Trim(c, "-: ") != "" || !strings.Contains(c, "-") {
			return false
		}
	}
	return true
}
