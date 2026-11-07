// Command e2eledger drives the e2e lane from the ledger of live claims
// (test/e2e/ledger.md): it prints the `go test -run` pattern for the
// tests the ledger declares for the hosted runner, and checks a run's
// `go test -json` output against the ledger. CI tooling, built from this
// repository — not part of the shipped winuse binary. It reads files
// only: it never runs a test and never touches the desktop.
package main

import (
	"fmt"
	"os"

	"github.com/hoangvu12/winuse/internal/e2eledger"
)

const usage = `usage: e2eledger <command> [options]

commands:
  pattern                    print the go test -run pattern that selects the
                             ledger's runner tests
  check <go-test.json>       check a run's go test -json output against the
                             ledger; exit 1 if a runner test skipped, failed
                             or did not run, or any test failed

options:
  --ledger <file>            the ledger (default test/e2e/ledger.md)
  --summary <file>           check: also append a Markdown report to file
                             (the job summary, $GITHUB_STEP_SUMMARY)
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	cmd := args[0]
	ledger := "test/e2e/ledger.md"
	summary := ""
	var rest []string
	for i := 1; i < len(args); i++ {
		switch a := args[i]; a {
		case "--ledger", "--summary":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "e2eledger: %s needs a file\n\n%s", a, usage)
				return 2
			}
			i++
			if a == "--ledger" {
				ledger = args[i]
			} else {
				summary = args[i]
			}
		default:
			rest = append(rest, a)
		}
	}
	switch cmd {
	case "pattern":
		if len(rest) != 0 {
			fmt.Fprintf(os.Stderr, "e2eledger: pattern takes no arguments\n\n%s", usage)
			return 2
		}
		return patternCmd(ledger)
	case "check":
		if len(rest) != 1 {
			fmt.Fprintf(os.Stderr, "e2eledger: check takes one go test -json file\n\n%s", usage)
			return 2
		}
		return checkCmd(ledger, rest[0], summary)
	case "help", "-h", "--help":
		fmt.Print(usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "e2eledger: unknown command %q\n\n%s", cmd, usage)
		return 2
	}
}

func readLedger(path string) ([]e2eledger.Row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rows, err := e2eledger.Parse(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return rows, nil
}

// patternCmd prints the -run pattern. A ledger with no runner rows is an
// error: an empty pattern would run every test in the package.
func patternCmd(ledger string) int {
	rows, err := readLedger(ledger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2eledger: %v\n", err)
		return 1
	}
	p := e2eledger.RunPattern(rows)
	if p == "" {
		fmt.Fprintf(os.Stderr, "e2eledger: %s declares no test for the runner\n", ledger)
		return 1
	}
	fmt.Println(p)
	return 0
}

func checkCmd(ledger, results, summary string) int {
	rows, err := readLedger(ledger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2eledger: %v\n", err)
		return 1
	}
	f, err := os.Open(results)
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2eledger: %v\n", err)
		return 1
	}
	events, skipped, err := e2eledger.ReadEvents(f)
	f.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2eledger: reading %s: %v\n", results, err)
		return 1
	}
	if skipped > 0 {
		fmt.Printf("e2eledger: %d lines of %s are not go test -json events; skipped\n", skipped, results)
	}
	rep := e2eledger.Check(rows, events)
	fmt.Print(rep.Text())
	if summary != "" {
		if err := appendFile(summary, rep.Markdown()); err != nil {
			fmt.Fprintf(os.Stderr, "e2eledger: writing the summary: %v\n", err)
		}
	}
	if !rep.OK() {
		return 1
	}
	return 0
}

func appendFile(path, text string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
