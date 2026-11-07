package e2eledger

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Event is one line of `go test -json` (cmd/test2json's TestEvent; the
// build events of Go 1.24 and later carry ImportPath instead of Package).
type Event struct {
	Action     string
	Package    string
	ImportPath string
	Test       string
	Elapsed    float64
	Output     string
}

// ReadEvents reads `go test -json` output. Lines that are not JSON (a
// toolchain message on the same stream, a BOM a shell added) are counted
// and skipped: the check judges by the events, and a run that produced
// none fails it anyway because no runner test ran.
func ReadEvents(r io.Reader) (events []Event, skipped int, err error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		text := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\ufeff"))
		if text == "" {
			continue
		}
		var e Event
		if !strings.HasPrefix(text, "{") || json.Unmarshal([]byte(text), &e) != nil {
			skipped++
			continue
		}
		events = append(events, e)
	}
	return events, skipped, sc.Err()
}

// Outcomes of one test in a run.
const (
	Passed  = "passed"
	Failed  = "failed"
	Skipped = "skipped"
	NotRun  = "did not run"
)

// Result is one runner test's outcome in a run.
type Result struct {
	Row
	Outcome string
	Elapsed float64
	Reason  string // a skip's reason, or a failure's first output line
}

// Report is the check of one run against the ledger.
type Report struct {
	Results []Result // the runner rows, in ledger order
	// Problems fail the lane: a runner test that skipped, failed or did
	// not run, any other failing test or subtest, a package or build
	// failure.
	Problems []string
	// Notes do not: a user-only test that ran anyway, a test the ledger
	// does not list.
	Notes []string
}

// OK reports whether the lane passes.
func (r *Report) OK() bool { return len(r.Problems) == 0 }

// testRun collects one test's events.
type testRun struct {
	action  string // the last of pass, fail, skip
	elapsed float64
	output  []string
}

// Check judges a run's events against the ledger.
func Check(rows []Row, events []Event) *Report {
	runs := map[string]*testRun{}
	var order []string
	rep := &Report{}
	for _, e := range events {
		pkg := e.Package
		if pkg == "" {
			pkg = e.ImportPath
		}
		switch {
		case e.Action == "build-fail":
			rep.Problems = append(rep.Problems, fmt.Sprintf("package %s did not build", pkg))
			continue
		case e.Test == "":
			if e.Action == "fail" {
				rep.Problems = append(rep.Problems, fmt.Sprintf("package %s failed (a build error, a panic or the -timeout; see its output)", pkg))
			}
			continue
		}
		tr, ok := runs[e.Test]
		if !ok {
			tr = &testRun{}
			runs[e.Test] = tr
			order = append(order, e.Test)
		}
		switch e.Action {
		case "output":
			tr.output = append(tr.output, e.Output)
		case "pass", "fail", "skip":
			tr.action = e.Action
			tr.elapsed = e.Elapsed
		}
	}

	declared := map[string]Row{}
	for _, row := range rows {
		declared[row.Test] = row
		if row.RunsOn != Runner {
			continue
		}
		res := Result{Row: row, Outcome: NotRun}
		if tr, ok := runs[row.Test]; ok {
			res.Elapsed = tr.elapsed
			switch tr.action {
			case "pass":
				res.Outcome = Passed
			case "fail":
				res.Outcome = Failed
				res.Reason = firstMessage(tr.output)
			case "skip":
				res.Outcome = Skipped
				res.Reason = firstMessage(tr.output)
			default:
				res.Reason = "it started but never ended (the run was cut short)"
			}
		}
		rep.Results = append(rep.Results, res)
		switch res.Outcome {
		case Passed:
		case Skipped:
			rep.Problems = append(rep.Problems, fmt.Sprintf("%s skipped, but the ledger declares it for the runner: %s", row.Test, orNone(res.Reason)))
		case Failed:
			rep.Problems = append(rep.Problems, fmt.Sprintf("%s failed: %s", row.Test, orNone(res.Reason)))
		default:
			why := "nothing in the go test output names it (not selected, or the run stopped before it)"
			if res.Reason != "" {
				why = res.Reason
			}
			rep.Problems = append(rep.Problems, fmt.Sprintf("%s did not run, but the ledger declares it for the runner: %s", row.Test, why))
		}
	}

	// Everything else that ran: subtests, user-only tests, unlisted tests.
	for _, name := range order {
		tr := runs[name]
		top, _, isSub := strings.Cut(name, "/")
		row, listed := declared[top]
		if !isSub && listed && row.RunsOn == Runner {
			continue // judged above
		}
		if tr.action == "fail" {
			// A failing subtest fails its parent too; it is named so the
			// lane's summary points at the leaf.
			rep.Problems = append(rep.Problems, fmt.Sprintf("%s failed: %s", name, orNone(firstMessage(tr.output))))
			continue
		}
		if isSub {
			continue
		}
		switch {
		case !listed:
			rep.Notes = append(rep.Notes, fmt.Sprintf("%s %s but has no ledger row", name, verb(tr.action)))
		case tr.action != "skip":
			rep.Notes = append(rep.Notes, fmt.Sprintf("%s %s on the runner, though the ledger keeps it to the user's PC (%s)", name, verb(tr.action), row.Why))
		}
	}
	return rep
}

func verb(action string) string {
	switch action {
	case "pass":
		return "passed"
	case "skip":
		return "skipped"
	case "fail":
		return "failed"
	}
	return "ran"
}

func orNone(s string) string {
	if s == "" {
		return "(no message)"
	}
	return s
}

// firstMessage is the first line a test printed itself: its t.Skip
// reason or its first t.Error/t.Fatal, without go test's own === and ---
// lines.
func firstMessage(output []string) string {
	for _, o := range output {
		for _, l := range strings.Split(o, "\n") {
			l = strings.TrimSpace(l)
			if l == "" || strings.HasPrefix(l, "=== ") || strings.HasPrefix(l, "--- ") {
				continue
			}
			return l
		}
	}
	return ""
}

// Text is the report for the job's log.
func (r *Report) Text() string {
	var b strings.Builder
	counts := map[string]int{}
	for _, res := range r.Results {
		counts[res.Outcome]++
	}
	fmt.Fprintf(&b, "e2e ledger check: %d runner tests: %d passed, %d failed, %d skipped, %d did not run\n",
		len(r.Results), counts[Passed], counts[Failed], counts[Skipped], counts[NotRun])
	for _, res := range r.Results {
		fmt.Fprintf(&b, "  %-12s %-60s %6.1fs", res.Outcome, res.Test, res.Elapsed)
		if res.Reason != "" && res.Outcome != Passed {
			fmt.Fprintf(&b, "  %s", res.Reason)
		}
		b.WriteString("\n")
	}
	if len(r.Notes) > 0 {
		b.WriteString("\nnotes:\n")
		for _, n := range r.Notes {
			fmt.Fprintf(&b, "  %s\n", n)
		}
	}
	if len(r.Problems) > 0 {
		b.WriteString("\nFAIL: the lane fails:\n")
		for _, p := range r.Problems {
			fmt.Fprintf(&b, "  %s\n", p)
		}
	} else {
		b.WriteString("\nok: every runner test in the ledger ran and passed\n")
	}
	return b.String()
}

// Markdown is the report for the job summary: the runner rows with their
// outcome, problems first, so the ledger's Last run and Result columns
// can be filled from it.
func (r *Report) Markdown() string {
	var b strings.Builder
	b.WriteString("## e2e ledger check\n\n")
	if r.OK() {
		b.WriteString("Every runner test in the ledger ran and passed.\n\n")
	} else {
		b.WriteString("**The lane fails:**\n\n")
		for _, p := range r.Problems {
			fmt.Fprintf(&b, "- %s\n", mdCell(p))
		}
		b.WriteString("\n")
	}
	results := append([]Result(nil), r.Results...)
	rank := map[string]int{Failed: 0, Skipped: 1, NotRun: 2, Passed: 3}
	sort.SliceStable(results, func(i, j int) bool { return rank[results[i].Outcome] < rank[results[j].Outcome] })
	b.WriteString("| Test | Result | Time | Message |\n|---|---|---|---|\n")
	for _, res := range results {
		msg := ""
		if res.Outcome != Passed {
			msg = mdCell(res.Reason)
		}
		fmt.Fprintf(&b, "| `%s` | %s | %.1fs | %s |\n", res.Test, res.Outcome, res.Elapsed, msg)
	}
	if len(r.Notes) > 0 {
		b.WriteString("\nNotes:\n\n")
		for _, n := range r.Notes {
			fmt.Fprintf(&b, "- %s\n", mdCell(n))
		}
	}
	return b.String()
}

func mdCell(s string) string {
	return strings.ReplaceAll(s, "|", `\|`)
}
