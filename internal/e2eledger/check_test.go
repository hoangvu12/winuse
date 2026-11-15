package e2eledger

import (
	"strings"
	"testing"
)

var checkRows = []Row{
	{Test: "TestA", Claim: "a", RunsOn: Runner},
	{Test: "TestB", Claim: "b", RunsOn: Runner},
	{Test: "TestUser", Claim: "u", RunsOn: UserPC, Why: "drives Notepad"},
}

// jsonRun joins go test -json lines into one stream.
func jsonRun(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

func check(t *testing.T, text string) *Report {
	t.Helper()
	events, _, err := ReadEvents(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	return Check(checkRows, events)
}

func problemsContain(t *testing.T, rep *Report, want ...string) {
	t.Helper()
	all := strings.Join(rep.Problems, "\n")
	for _, w := range want {
		if !strings.Contains(all, w) {
			t.Errorf("problems lack %q:\n%s", w, all)
		}
	}
}

func TestCheckPassesWhenEveryRunnerTestPassed(t *testing.T) {
	rep := check(t, jsonRun(
		`{"Action":"start","Package":"p"}`,
		`{"Action":"run","Package":"p","Test":"TestA"}`,
		`{"Action":"output","Package":"p","Test":"TestA","Output":"=== RUN   TestA\n"}`,
		`{"Action":"pass","Package":"p","Test":"TestA","Elapsed":1.5}`,
		`{"Action":"run","Package":"p","Test":"TestB"}`,
		`{"Action":"run","Package":"p","Test":"TestB/sub"}`,
		`{"Action":"pass","Package":"p","Test":"TestB/sub"}`,
		`{"Action":"pass","Package":"p","Test":"TestB","Elapsed":2}`,
		`{"Action":"pass","Package":"p","Elapsed":3.5}`,
	))
	if !rep.OK() {
		t.Fatalf("problems: %v", rep.Problems)
	}
	if rep.Results[0].Outcome != Passed || rep.Results[0].Elapsed != 1.5 || rep.Results[1].Outcome != Passed {
		t.Errorf("results = %+v", rep.Results)
	}
	if len(rep.Notes) != 0 {
		t.Errorf("notes = %v", rep.Notes)
	}
	if !strings.Contains(rep.Text(), "2 passed, 0 failed, 0 skipped, 0 did not run") {
		t.Errorf("text:\n%s", rep.Text())
	}
}

func TestCheckFailsADeclaredTestThatSkipped(t *testing.T) {
	rep := check(t, jsonRun(
		`{"Action":"run","Package":"p","Test":"TestA"}`,
		`{"Action":"output","Package":"p","Test":"TestA","Output":"=== RUN   TestA\n"}`,
		`{"Action":"output","Package":"p","Test":"TestA","Output":"    runner_test.go:43: end-to-end tests are opt-in only\n"}`,
		`{"Action":"output","Package":"p","Test":"TestA","Output":"--- SKIP: TestA (0.00s)\n"}`,
		`{"Action":"skip","Package":"p","Test":"TestA"}`,
		`{"Action":"pass","Package":"p","Test":"TestB"}`,
		`{"Action":"pass","Package":"p"}`,
	))
	if rep.OK() {
		t.Fatal("a skipped runner test must fail the lane")
	}
	problemsContain(t, rep, "TestA skipped, but the ledger declares it for the runner: runner_test.go:43: end-to-end tests are opt-in only")
	if len(rep.Problems) != 1 {
		t.Errorf("problems = %v, want only the skip", rep.Problems)
	}
	if rep.Results[0].Outcome != Skipped {
		t.Errorf("TestA = %+v", rep.Results[0])
	}
}

func TestCheckFailsADeclaredTestThatDidNotRun(t *testing.T) {
	rep := check(t, jsonRun(
		`{"Action":"pass","Package":"p","Test":"TestA"}`,
		`{"Action":"pass","Package":"p"}`,
	))
	problemsContain(t, rep, "TestB did not run, but the ledger declares it for the runner")
	if rep.Results[1].Outcome != NotRun {
		t.Errorf("TestB = %+v", rep.Results[1])
	}

	// No events at all (go test never started): every runner test is
	// missing.
	rep = check(t, "")
	problemsContain(t, rep, "TestA did not run", "TestB did not run")
}

func TestCheckFailsATestCutShort(t *testing.T) {
	rep := check(t, jsonRun(
		`{"Action":"pass","Package":"p","Test":"TestA"}`,
		`{"Action":"run","Package":"p","Test":"TestB"}`,
		`{"Action":"output","Package":"p","Output":"panic: test timed out after 1m0s\n"}`,
		`{"Action":"fail","Package":"p","Elapsed":60}`,
	))
	problemsContain(t, rep, "TestB did not run, but the ledger declares it for the runner: it started but never ended", "package p failed")
}

func TestCheckFailsOnAnyFailure(t *testing.T) {
	rep := check(t, jsonRun(
		`{"Action":"pass","Package":"p","Test":"TestA"}`,
		`{"Action":"run","Package":"p","Test":"TestB"}`,
		`{"Action":"output","Package":"p","Test":"TestB/leg","Output":"    click_test.go:9: exit 1, stderr: boom\n"}`,
		`{"Action":"fail","Package":"p","Test":"TestB/leg"}`,
		`{"Action":"output","Package":"p","Test":"TestB","Output":"--- FAIL: TestB (1.00s)\n"}`,
		`{"Action":"fail","Package":"p","Test":"TestB","Elapsed":1}`,
		`{"Action":"fail","Package":"p","Test":"TestUnlisted"}`,
		`{"Action":"fail","Package":"p"}`,
	))
	problemsContain(t, rep, "TestB failed", "TestB/leg failed: click_test.go:9: exit 1, stderr: boom", "TestUnlisted failed", "package p failed")
	if rep.Results[1].Outcome != Failed {
		t.Errorf("TestB = %+v", rep.Results[1])
	}
}

func TestCheckFailsABuildFailure(t *testing.T) {
	rep := check(t, jsonRun(
		`{"ImportPath":"p.test","Action":"build-output","Output":"x.go:1: undefined: y\n"}`,
		`{"ImportPath":"p.test","Action":"build-fail"}`,
		`{"Action":"start","Package":"p"}`,
		`{"Action":"output","Package":"p","Output":"FAIL\tp [build failed]\n"}`,
		`{"Action":"fail","Package":"p"}`,
	))
	problemsContain(t, rep, "package p.test did not build", "TestA did not run")
}

func TestCheckNotesWhatTheLedgerDoesNotDeclare(t *testing.T) {
	rep := check(t, jsonRun(
		`{"Action":"pass","Package":"p","Test":"TestA"}`,
		`{"Action":"pass","Package":"p","Test":"TestB"}`,
		`{"Action":"pass","Package":"p","Test":"TestUser"}`,
		`{"Action":"pass","Package":"p","Test":"TestNew"}`,
		`{"Action":"pass","Package":"p"}`,
	))
	if !rep.OK() {
		t.Fatalf("notes must not fail the lane: %v", rep.Problems)
	}
	all := strings.Join(rep.Notes, "\n")
	for _, w := range []string{"TestUser passed on the runner, though the ledger keeps it to the user's PC (drives Notepad)", "TestNew passed but has no ledger row"} {
		if !strings.Contains(all, w) {
			t.Errorf("notes lack %q:\n%s", w, all)
		}
	}
}

func TestReadEventsSkipsNonJSONLines(t *testing.T) {
	events, skipped, err := ReadEvents(strings.NewReader("\ufeff" + `{"Action":"pass","Test":"TestA"}` + "\r\ngo: downloading x\n\n{broken\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Test != "TestA" || skipped != 2 {
		t.Errorf("events = %+v, skipped = %d", events, skipped)
	}
}

func TestMarkdownListsProblemsFirst(t *testing.T) {
	rep := check(t, jsonRun(
		`{"Action":"pass","Package":"p","Test":"TestA","Elapsed":1}`,
		`{"Action":"output","Package":"p","Test":"TestB","Output":"    x_test.go:1: no WGC | here\n"}`,
		`{"Action":"skip","Package":"p","Test":"TestB"}`,
	))
	md := rep.Markdown()
	if !strings.Contains(md, "**The lane fails:**") {
		t.Errorf("markdown:\n%s", md)
	}
	b := strings.Index(md, "| `TestB` | skipped |")
	a := strings.Index(md, "| `TestA` | passed |")
	if b < 0 || a < 0 || b > a {
		t.Errorf("want TestB's skip before TestA's pass:\n%s", md)
	}
	if !strings.Contains(md, `no WGC \| here`) {
		t.Errorf("a | in a message must be escaped:\n%s", md)
	}
}
