package tasksuite

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSpreadOf(t *testing.T) {
	if s := SpreadOf(nil); s.N != 0 {
		t.Errorf("empty spread = %+v", s)
	}
	if s := SpreadOf([]float64{41, 18, 15}); s != (Spread{N: 3, Median: 18, Min: 15, Max: 41}) {
		t.Errorf("odd spread = %+v", s)
	}
	if s := SpreadOf([]float64{4, 1, 3, 2}); s.Median != 2.5 || s.Min != 1 || s.Max != 4 {
		t.Errorf("even spread = %+v", s)
	}
}

// fakeRun is one measured run, as runTask fills it from the Harness's
// output and the trace.
func fakeRun(task, arm string, rep int, status Status, wall float64, turns int, in, out int64, cost float64, obs, act, wait int64) TaskResult {
	return TaskResult{
		Task:           Task{ID: task, Category: "ordinary"},
		Status:         status,
		Steps:          turns,
		Seconds:        wall + 5,
		Arm:            arm,
		Repeat:         rep,
		HarnessSeconds: wall,
		Usage:          Usage{Turns: turns, InputTokens: in, OutputTokens: out, CostUSD: cost, APIMS: int64(wall * 700), Known: true},
		Trace: TraceSummary{
			Commands: turns, Actions: turns / 2, MS: obs + act + wait,
			Kinds: map[string]KindTotal{
				KindObserve: {Commands: turns / 2, MS: obs},
				KindAct:     {Commands: turns / 2, MS: act},
				KindWait:    {Commands: 1, MS: wait},
			},
		},
		TraceFound: true,
	}
}

func abResults() []TaskResult {
	skip := func(arm string) TaskResult {
		return TaskResult{Task: Task{ID: "vscode-edit", Category: "electron"}, Status: Skip, Steps: -1, Arm: arm, Repeat: 1,
			Reason: "requires code, which was not found on PATH", Usage: Usage{Turns: -1, CostUSD: -1}}
	}
	return []TaskResult{
		fakeRun("notepad-note", "main", 1, Pass, 99, 19, 410_000, 3_100, 1.62, 6_100, 21_000, 900),
		fakeRun("notepad-note", "waits", 1, Pass, 80, 15, 350_000, 2_900, 1.31, 5_900, 12_400, 800),
		fakeRun("notepad-note", "waits", 2, Pass, 84, 17, 371_000, 3_000, 1.40, 6_000, 12_900, 700),
		fakeRun("notepad-note", "main", 2, Pass, 92, 15, 380_000, 2_800, 1.48, 5_800, 19_800, 1_100),
		fakeRun("notepad-note", "main", 3, Fail, 140, 41, 900_000, 6_000, 3.20, 12_000, 44_000, 2_000),
		fakeRun("notepad-note", "waits", 3, Pass, 77, 14, 330_000, 2_700, 1.22, 5_700, 11_100, 600),
		skip("main"),
		skip("waits"),
	}
}

func TestSummarizeMediansOverRepeats(t *testing.T) {
	stats := Summarize(abResults())
	if len(stats) != 4 {
		t.Fatalf("groups = %d, want 4 (2 tasks × 2 arms)", len(stats))
	}
	main := stats[0]
	if main.Task != "notepad-note" || main.Arm != "main" || main.Runs != 3 || main.Passed != 2 || main.Measured != 3 {
		t.Errorf("main group = %+v", main)
	}
	if main.Wall != (Spread{N: 3, Median: 99, Min: 92, Max: 140}) {
		t.Errorf("wall = %+v", main.Wall)
	}
	if main.Turns.Median != 19 || main.Turns.Min != 15 || main.Turns.Max != 41 {
		t.Errorf("turns = %+v", main.Turns)
	}
	if main.Kinds[KindAct].Median != 21.0 {
		t.Errorf("act seconds median = %v, want 21", main.Kinds[KindAct].Median)
	}
	if !stats[2].Skipped || stats[2].Task != "vscode-edit" {
		t.Errorf("vscode-edit group = %+v", stats[2])
	}
}

func TestFormatSummary(t *testing.T) {
	sum := FormatSummary(abResults())
	t.Logf("\n%s", sum)
	for _, want := range []string{
		"Speed, arm main", "Speed, arm waits",
		"notepad-note", "2/3", "3/3",
		"1m39s [1m32s-2m20s]", // main's wall: median [min-max]
		"19 [15-41]",          // main's turns
		"410k",                // main's input tokens, median
		"$1.62",
		"6.1/21.0/1.1", // observe/act/wait seconds, medians
		"skipped: vscode-edit",
	} {
		if !strings.Contains(sum, want) {
			t.Errorf("summary missing %q:\n%s", want, sum)
		}
	}
}

func TestFormatComparison(t *testing.T) {
	cmp := FormatComparison(abResults(), "main", "waits")
	t.Logf("\n%s", cmp)
	for _, want := range []string{
		"A = main, B = waits",
		"notepad-note  2/3     3/3",
		"1m39s -> 1m20s (-19%)*", // B's median is below A's min: starred
		"19 -> 15 (-21%)",        // inside A's range: not starred
		"all tasks",
		"skipped: vscode-edit",
	} {
		if !strings.Contains(cmp, want) {
			t.Errorf("comparison missing %q:\n%s", want, cmp)
		}
	}
	if strings.Contains(cmp, "19 -> 15 (-21%)*") {
		t.Errorf("a change inside A's range must not be starred:\n%s", cmp)
	}
}

func TestChange(t *testing.T) {
	cases := []struct {
		a, b float64
		want string
	}{{100, 80, "(-20%)"}, {80, 100, "(+25%)"}, {0, 0, "(0%)"}, {0, 3, "(new)"}}
	for _, c := range cases {
		if got := change(c.a, c.b); got != c.want {
			t.Errorf("change(%v, %v) = %q, want %q", c.a, c.b, got, c.want)
		}
	}
}

func TestFormatTableNamesRunsWithRepeats(t *testing.T) {
	table := FormatTable(abResults())
	for _, want := range []string{"Run", "main r1", "waits r3"} {
		if !strings.Contains(table, want) {
			t.Errorf("table missing %q:\n%s", want, table)
		}
	}
	// A plain run keeps the old columns.
	plain := FormatTable([]TaskResult{{Task: Task{ID: "x", Category: "ordinary"}, Status: Pass, Steps: 3, Seconds: 10}})
	if strings.Contains(plain, "Run") {
		t.Errorf("a plain run's table has a Run column:\n%s", plain)
	}
}

// TestRecordedRunReport goes the whole offline way: a fake Harness
// stream and a fake trace per run, parsed as runTask parses them, written
// as a results file, read back and summarized as `winusetasks report`
// does.
func TestRecordedRunReport(t *testing.T) {
	stream := readTestdata(t, "claude-stream.jsonl")
	traceRaw := readTestdata(t, "trace.jsonl")
	dir := t.TempDir()
	var results []TaskResult
	for i, arm := range []string{"A", "B", "B", "A"} {
		outcome, err := ParseClaudeResult(stream)
		if err != nil {
			t.Fatal(err)
		}
		trace := filepath.Join(dir, fmt.Sprintf("trace-%d.jsonl", i))
		if err := os.WriteFile(trace, []byte(traceRaw), 0o644); err != nil {
			t.Fatal(err)
		}
		sum, found, err := ReadTraceFiles(trace)
		if err != nil {
			t.Fatal(err)
		}
		results = append(results, TaskResult{
			Task: Task{ID: "notepad-note", Category: "ordinary"}, Status: Pass, Steps: outcome.Steps,
			Seconds: 50, Arm: arm, Repeat: 1 + i/2, HarnessSeconds: 41 + float64(i),
			Usage: outcome.Usage, Trace: sum, TraceFound: found,
		})
	}
	rec := RunRecord{Schema: RecordSchema, Started: time.Date(2026, 11, 5, 3, 0, 0, 0, time.UTC), Harness: "claude", Model: "claude-opus-5-5",
		Repeats: 2, Arms: []ArmRecord{{Label: "A", WinuseSource: "tree"}, {Label: "B", WinuseSource: `C:\b\winuse.exe`}}, Results: results}
	raw, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "results.json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	back, err := LoadRunRecord(path)
	if err != nil {
		t.Fatalf("LoadRunRecord: %v", err)
	}
	if back.Schema != RecordSchema || len(back.Results) != 4 || back.Results[0].Trace.Kind(KindAct).MS != 6070 {
		t.Fatalf("round trip lost data: %+v", back.Results[0])
	}
	sum := FormatSummary(back.Results)
	cmp := FormatComparison(back.Results, "A", "B")
	t.Logf("\n%s\n%s", sum, cmp)
	for _, want := range []string{"3 ", "53k", "125", "$0.41", "28.5s", "8.8s", "1.7/6.1/0.9", "12/8"} {
		if !strings.Contains(sum, want) {
			t.Errorf("summary missing %q:\n%s", want, sum)
		}
	}
	if !strings.Contains(cmp, "A = A, B = B") {
		t.Errorf("comparison:\n%s", cmp)
	}
}

func TestLoadRunRecordSchema1(t *testing.T) {
	// The results files before speed metrics: no schema, no arms, no
	// repeats. They still load, as one arm with one run per task.
	old := `{"started":"2026-11-03T10:00:00Z","harness":"claude","model":"claude-opus-5-5","winuse":"0.1.0",
"results":[{"Task":{"ID":"notepad-note","Category":"ordinary"},"Status":"PASS","Steps":19,"Seconds":99,"Reason":"","ScratchDir":"C:\\x"}]}`
	path := filepath.Join(t.TempDir(), "results.json")
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	rec, err := LoadRunRecord(path)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Repeats != 1 || len(rec.Results) != 1 || rec.Results[0].Steps != 19 {
		t.Errorf("schema-1 record = %+v", rec)
	}
	st := Summarize(rec.Results)[0]
	if st.Turns.Median != 19 || st.Wall.Median != 99 {
		t.Errorf("a schema-1 run's turns/wall = %v/%v, want its steps and time (19/99)", st.Turns.Median, st.Wall.Median)
	}
	if sum := FormatSummary(rec.Results); !strings.Contains(sum, "notepad-note") {
		t.Errorf("summary of a schema-1 file:\n%s", sum)
	}
}
