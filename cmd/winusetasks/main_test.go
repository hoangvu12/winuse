package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hoangvu12/winuse/internal/latency"
	"github.com/hoangvu12/winuse/internal/tasksuite"
)

// These tests cover only the paths that touch nothing: the refusal, the
// usage errors and the help. `run` with --i-am-the-user would run the
// Task Suite (taking over the screen); it is user-started only and is
// never exercised by a test.

func TestRunCmdRefusesWithoutTheUserStart(t *testing.T) {
	// No flag: refused before anything runs.
	if code := runCmd(nil); code != 1 {
		t.Errorf("runCmd without --i-am-the-user = %d, want 1 (refusal)", code)
	}
	// Other flags do not smuggle a start past the refusal.
	if code := runCmd([]string{"--harness", "pi"}); code != 1 {
		t.Errorf("runCmd with flags but no user start = %d, want 1", code)
	}
}

func TestRunCmdUsageErrors(t *testing.T) {
	cases := [][]string{
		{"--bogus"},
		{"--harness"},
		{"--time-limit"},
		{"--time-limit", "not-a-duration"},
		{"--max-turns", "0"},
	}
	for _, args := range cases {
		if code := runCmd(args); code != 2 {
			t.Errorf("runCmd(%v) = %d, want 2 (usage error)", args, code)
		}
	}
}

func TestRunUnknownCommand(t *testing.T) {
	if code := run([]string{"nope"}); code != 2 {
		t.Errorf("unknown command = %d, want 2", code)
	}
	if code := run(nil); code != 2 {
		t.Errorf("no command = %d, want 2", code)
	}
}

func TestRunHelp(t *testing.T) {
	if code := run([]string{"help"}); code != 0 {
		t.Errorf("help = %d, want 0", code)
	}
}

func TestRunCmdSpeedUsageErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.exe")
	cases := [][]string{
		{"--repeats"},
		{"--repeats", "0"},
		{"--repeats", "x"},
		{"--winuse"},
		{"--b-winuse", missing},
		{"--b-skill", missing},
		{"--label", "has space", "--b-label", "B"},
		{"--label", "same", "--b-label", "SAME"},
	}
	for _, args := range cases {
		if code := runCmd(args); code != 2 {
			t.Errorf("runCmd(%v) = %d, want 2 (usage error)", args, code)
		}
	}
	// Good speed options still need the user's start.
	if code := runCmd([]string{"--repeats", "3", "--b-label", "again"}); code != 1 {
		t.Errorf("speed options without --i-am-the-user = %d, want 1 (refusal)", code)
	}
}

func TestArmsOf(t *testing.T) {
	if arms := armsOf(map[string]string{}); arms != nil {
		t.Errorf("no arm options = %v, want a plain run", arms)
	}
	one := armsOf(map[string]string{"--winuse": `C:\w\winuse.exe`})
	if len(one) != 1 || one[0].Winuse != `C:\w\winuse.exe` || one[0].Label != "" {
		t.Errorf("one arm = %+v", one)
	}
	ab := armsOf(map[string]string{"--winuse": `C:\a.exe`, "--skill": `C:\s\SKILL.md`, "--b-winuse": `C:\tree`})
	want := []tasksuite.ArmOptions{
		{Label: "A", Winuse: `C:\a.exe`, Skill: `C:\s\SKILL.md`},
		{Label: "B", Winuse: `C:\tree`, Skill: `C:\s\SKILL.md`}, // B inherits A's skill
	}
	if len(ab) != 2 || ab[0] != want[0] || ab[1] != want[1] {
		t.Errorf("A/B arms = %+v, want %+v", ab, want)
	}
	skillOnly := armsOf(map[string]string{"--b-skill": `C:\new\SKILL.md`, "--b-label": "lean"})
	if len(skillOnly) != 2 || skillOnly[0].Winuse != "" || skillOnly[1].Winuse != "" || skillOnly[1].Label != "lean" || skillOnly[1].Skill != `C:\new\SKILL.md` {
		t.Errorf("skill A/B = %+v", skillOnly)
	}
}

// latency with --i-am-the-user would replace the running helper and start
// the fixture: only its refusal and its usage errors are exercised.
func TestLatencyCmdRefusesWithoutTheUserStart(t *testing.T) {
	for _, args := range [][]string{nil, {"--huge-list", "20000", "--window", "notepad"}} {
		if code := latencyCmd(args); code != 1 {
			t.Errorf("latencyCmd(%v) = %d, want 1 (refusal)", args, code)
		}
	}
}

// The usage errors are checked on the parser, with the user's start among
// the arguments, so a case that parsed would still never start a run.
func TestLatencyUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"--bogus"}, {"--a"}, {"--b="}, {"--window"}, {"--rounds", "0"}, {"--rounds", "101"},
		{"--per-block", "x"}, {"--warmup", "-1"}, {"--huge-list", "0"},
		{"--a-label", "same", "--b-label", "SAME"}, {"--b-label", "A"}, {"--a-label", "two words"},
		{"--no-fixture"}, {"--no-fixture", "--huge-list", "5", "--window", "notepad"},
		{"--a", "--i-am-the-user"}, // a flag is never a value
	} {
		if _, _, msg, code := parseLatency(append(args, "--i-am-the-user")); code != 2 || msg == "" {
			t.Errorf("parseLatency(%v) = %d %q, want 2 (usage error)", args, code, msg)
		}
	}
	if code := latencyCmd([]string{"--bogus"}); code != 2 {
		t.Errorf("latencyCmd(--bogus) = %d, want 2", code)
	}
}

func TestParseLatency(t *testing.T) {
	opts, me, msg, code := parseLatency([]string{"--a", "main~2", "--b=.", "--a-label", "base", "--b-label=lists",
		"--window", "notepad", "--window", "1442390", "--huge-list", "20000", "--rounds", "4", "--per-block=3", "--warmup", "0",
		"--out", `C:\r`, "--i-am-the-user"})
	if code != 0 || !me {
		t.Fatalf("code %d %q", code, msg)
	}
	if opts.A.Source != "main~2" || opts.B.Source != "." || opts.A.Label != "base" || opts.B.Label != "lists" ||
		len(opts.Windows) != 2 || opts.Windows[1] != "1442390" || opts.HugeList != 20000 || !opts.Fixture || opts.Out != `C:\r` ||
		opts.Schedule.Rounds != 4 || opts.Schedule.PerBlock != 3 || opts.Schedule.Warmup != 0 {
		t.Errorf("opts %+v", opts)
	}
	opts, me, _, code = parseLatency(nil)
	if code != 0 || me || !opts.Fixture || opts.Schedule != latency.DefaultSchedule || opts.A.Source != "" || opts.B.Source != "" {
		t.Errorf("defaults %+v", opts)
	}
	opts, _, _, code = parseLatency([]string{"--no-fixture", "--window", "notepad"})
	if code != 0 || opts.Fixture {
		t.Errorf("no fixture %+v", opts)
	}
}

func TestLatencyReportCmd(t *testing.T) {
	if code := latencyReportCmd(nil); code != 2 {
		t.Errorf("no file = %d, want 2", code)
	}
	if code := latencyReportCmd([]string{filepath.Join(t.TempDir(), "missing.json")}); code != 1 {
		t.Errorf("missing file = %d, want 1", code)
	}
	path := filepath.Join(t.TempDir(), "results.json")
	rec := `{"schema":1,"started":"2026-11-15T09:00:00Z","arms":[{"label":"A","desc":"base"},{"label":"B","desc":"head"}],
"schedule":{"rounds":1,"per_block":1,"warmup":0},"ops":[{"name":"list-windows","args":["list-windows"]}],
"samples":[{"arm":"A","op":"list-windows","round":1,"ms":40,"exit":0},{"arm":"B","op":"list-windows","round":1,"ms":38,"exit":0}]}`
	if err := os.WriteFile(path, []byte(rec), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := latencyReportCmd([]string{path}); code != 0 {
		t.Errorf("report = %d, want 0", code)
	}
}

func TestReportCmd(t *testing.T) {
	if code := reportCmd(nil); code != 2 {
		t.Errorf("report without a file = %d, want 2", code)
	}
	if code := reportCmd([]string{filepath.Join(t.TempDir(), "missing.json")}); code != 1 {
		t.Errorf("report of a missing file = %d, want 1", code)
	}
	path := filepath.Join(t.TempDir(), "results.json")
	rec := `{"schema":2,"started":"2026-11-05T03:00:00Z","harness":"claude","model":"claude-opus-5-5","repeats":1,
"arms":[{"label":"A","winuse_source":"tree"},{"label":"B","winuse_source":"tree"}],
"results":[{"Task":{"ID":"notepad-note"},"Status":"PASS","Steps":3,"Arm":"A","Repeat":1,"HarnessSeconds":40,"Usage":{"turns":3,"known":true,"cost_usd":0.4}},
{"Task":{"ID":"notepad-note"},"Status":"PASS","Steps":2,"Arm":"B","Repeat":1,"HarnessSeconds":30,"Usage":{"turns":2,"known":true,"cost_usd":0.3}}]}`
	if err := os.WriteFile(path, []byte(rec), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := reportCmd([]string{path}); code != 0 {
		t.Errorf("report = %d, want 0", code)
	}
}
